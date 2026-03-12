package cli

import (
	"encoding/json"
	"fmt"

	"github.com/constructspace/loom/internal/core"
	lsync "github.com/constructspace/loom/internal/sync"
	"github.com/spf13/cobra"
)

func newSendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "send [remote]",
		Short: "Push local changes to a hub",
		Long:  "Send local operations and objects to a LoomHub remote.",
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

			token, err := remotes.GetAuthToken(remote.Name)
			if err != nil {
				return err
			}

			client := lsync.NewClient(remote.URL, token)

			// Get active stream
			stream, err := v.ActiveStream()
			if err != nil {
				return fmt.Errorf("get active stream: %w", err)
			}

			// Get local head
			localHead, err := v.OpReader.Head()
			if err != nil {
				return fmt.Errorf("read local head: %w", err)
			}

			if localHead <= remote.PushSeq {
				fmt.Println("Everything up to date.")
				return nil
			}

			// Negotiate with hub
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

			if !negResp.NeedsPush {
				fmt.Println("Everything up to date.")
				return nil
			}

			// Determine what to send
			fromSeq := remote.PushSeq
			if commonSeq, ok := negResp.CommonSeqs[stream.ID]; ok && commonSeq > fromSeq {
				fromSeq = commonSeq
			}

			// Read operations to send
			ops, err := v.OpReader.ReadByStream(stream.ID, fromSeq, localHead)
			if err != nil {
				return fmt.Errorf("read operations: %w", err)
			}

			if len(ops) == 0 {
				fmt.Println("Everything up to date.")
				return nil
			}

			// Convert to wire format and collect object refs
			wireOps := make([]lsync.OperationWire, len(ops))
			objectRefs := make(map[string]bool)
			for i, op := range ops {
				metaJSON, _ := json.Marshal(op.Meta)
				wireOps[i] = lsync.OperationWire{
					ID:        op.ID,
					Seq:       op.Seq,
					StreamID:  op.StreamID,
					SpaceID:   op.SpaceID,
					EntityID:  op.EntityID,
					Type:      string(op.Type),
					Path:      op.Path,
					ObjectRef: op.ObjectRef,
					ParentSeq: op.ParentSeq,
					Author:    op.Author,
					Timestamp: op.Timestamp,
					Meta:      json.RawMessage(metaJSON),
				}
				if op.Delta != nil {
					wireOps[i].Delta = json.RawMessage(op.Delta)
				}
				if op.ObjectRef != "" {
					objectRefs[op.ObjectRef] = true
				}
			}

			// Read objects to send
			var objects []lsync.ObjectData
			for hash := range objectRefs {
				content, err := v.Store.Read(hash)
				if err != nil {
					return fmt.Errorf("read object %s: %w", hash[:12], err)
				}
				objects = append(objects, lsync.ObjectData{
					Hash:    hash,
					Content: content,
				})
			}

			// Push
			fmt.Printf("Sending %d operations, %d objects...\n", len(wireOps), len(objects))
			pushResp, err := client.Push(&lsync.PushRequest{
				ProjectID:  v.Config.Project.Name,
				StreamID:   stream.ID,
				FromSeq:    fromSeq,
				Operations: wireOps,
				Objects:    objects,
			})
			if err != nil {
				return err
			}

			if !pushResp.OK {
				return fmt.Errorf("push rejected: %s", pushResp.Error)
			}

			// Update local push_seq
			if err := remotes.UpdatePushSeq(remote.Name, localHead); err != nil {
				return fmt.Errorf("update push seq: %w", err)
			}

			fmt.Printf("Sent %d operations to %s (server head: %d)\n", pushResp.Applied, remote.Name, pushResp.ServerHead)
			return nil
		},
	}
}
