package cli

import (
	"encoding/json"
	"fmt"

	"github.com/constructspace/loom/internal/core"
	lsync "github.com/constructspace/loom/internal/sync"
	"github.com/spf13/cobra"
)

func newReceiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "receive [remote]",
		Short: "Pull changes from a hub",
		Long:  "Receive operations and objects from a LoomHub remote.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer v.Close()

			remotes := core.NewRemoteStore(v.DB)

			var remote *core.Remote
			if len(args) > 0 {
				remote, err = remotes.Get(args[0])
			} else {
				remote, err = remotes.Default()
			}
			if err != nil {
				return err
			}

			token, _ := remotes.GetAuthToken(remote.Name) // token optional for public looms
			client := lsync.NewClient(remote.URL, token)

			// Get active stream
			stream, err := v.ActiveStream()
			if err != nil {
				return fmt.Errorf("get active stream: %w", err)
			}

			// Negotiate
			localHead, err := v.OpReader.Head()
			if err != nil {
				return fmt.Errorf("read local head: %w", err)
			}

			fmt.Printf("Negotiating with %s...\n", remote.Name)
			negResp, err := client.Negotiate(&lsync.NegotiateRequest{
				ProjectID: v.Config.Project.Name,
				Streams: []lsync.StreamSyncState{
					{
						StreamID: stream.ID,
						Name:     stream.Name,
						HeadSeq:  localHead,
					},
				},
			})
			if err != nil {
				return err
			}

			if !negResp.NeedsPull {
				fmt.Println("Already up to date.")
				return nil
			}

			// Determine pull range
			fromSeq := remote.PullSeq
			if commonSeq, ok := negResp.CommonSeqs[stream.ID]; ok && commonSeq > fromSeq {
				fromSeq = commonSeq
			}

			// Pull
			fmt.Printf("Receiving from %s (from seq %d)...\n", remote.Name, fromSeq)
			pullResp, err := client.Pull(&lsync.PullRequest{
				ProjectID: v.Config.Project.Name,
				StreamID:  stream.ID,
				FromSeq:   fromSeq,
			})
			if err != nil {
				return err
			}

			if len(pullResp.Operations) == 0 {
				fmt.Println("Already up to date.")
				return nil
			}

			// Store received objects
			for _, obj := range pullResp.Objects {
				if !v.Store.Exists(obj.Hash) {
					if _, err := v.Store.Write(obj.Content, ""); err != nil {
						return fmt.Errorf("store object %s: %w", obj.Hash[:12], err)
					}
				}
			}

			// Convert wire operations to core operations and write them
			coreOps := make([]core.Operation, len(pullResp.Operations))
			for i, op := range pullResp.Operations {
				var meta core.OpMeta
				if len(op.Meta) > 0 {
					json.Unmarshal(op.Meta, &meta)
				}
				coreOps[i] = core.Operation{
					ID:        op.ID,
					Seq:       op.Seq,
					StreamID:  op.StreamID,
					SpaceID:   op.SpaceID,
					EntityID:  op.EntityID,
					Type:      core.OpType(op.Type),
					Path:      op.Path,
					ObjectRef: op.ObjectRef,
					ParentSeq: op.ParentSeq,
					Author:    op.Author,
					Timestamp: op.Timestamp,
					Meta:      meta,
				}
				if op.Delta != nil {
					coreOps[i].Delta = []byte(op.Delta)
				}
			}

			// Write operations locally using WriteBatch
			written, err := v.OpWriter.WriteBatch(coreOps)
			if err != nil {
				return fmt.Errorf("write operations: %w", err)
			}

			// Update pull_seq
			var maxSeq int64
			for _, op := range written {
				if op.Seq > maxSeq {
					maxSeq = op.Seq
				}
			}
			if err := remotes.UpdatePullSeq(remote.Name, maxSeq); err != nil {
				return fmt.Errorf("update pull seq: %w", err)
			}

			fmt.Printf("Received %d operations from %s (head: %d)\n", len(written), remote.Name, maxSeq)
			return nil
		},
	}
}
