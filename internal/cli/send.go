package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/constructspace/loom/internal/core"
	lsync "github.com/constructspace/loom/internal/sync"
	"github.com/spf13/cobra"
)

const defaultSendBatchSize = 256

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

			localHead := stream.HeadSeq

			pushSeq, err := remotes.GetStreamPushSeq(remote.Name, stream.ID)
			if err != nil {
				return fmt.Errorf("read stream push seq: %w", err)
			}

			if localHead <= pushSeq {
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
			fromSeq := pushSeq
			if fromSeq == 0 {
				if commonSeq, ok := negResp.CommonSeqs[stream.ID]; ok && commonSeq > fromSeq {
					fromSeq = commonSeq
				}
			}

			if fromSeq > localHead {
				fromSeq = localHead
			}

			if commonSeq, ok := negResp.CommonSeqs[stream.ID]; ok && commonSeq < fromSeq {
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

			batchSize := configuredSendBatchSize()
			fmt.Printf("Sending %d operations in batches of %d...\n", len(ops), batchSize)

			sentObjects := make(map[string]bool)
			totalApplied := 0
			serverHead := fromSeq

			for start := 0; start < len(ops); start += batchSize {
				end := start + batchSize
				if end > len(ops) {
					end = len(ops)
				}

				batch := ops[start:end]
				wireOps := make([]lsync.OperationWire, len(batch))
				objectRefs := make(map[string]bool)

				for i, op := range batch {
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
					if op.ObjectRef != "" && !sentObjects[op.ObjectRef] {
						objectRefs[op.ObjectRef] = true
					}
				}

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
					sentObjects[hash] = true
				}

				batchFromSeq := fromSeq
				if start > 0 {
					batchFromSeq = ops[start-1].Seq
				}

				pushResp, err := client.Push(&lsync.PushRequest{
					ProjectID:  v.Config.Project.Name,
					StreamID:   stream.ID,
					FromSeq:    batchFromSeq,
					Operations: wireOps,
					Objects:    objects,
				})
				if err != nil {
					return err
				}
				if !pushResp.OK {
					return fmt.Errorf("push rejected: %s", pushResp.Error)
				}

				totalApplied += pushResp.Applied
				serverHead = pushResp.ServerHead
			}

			// Update local push_seq
			if err := remotes.UpdateStreamPushSeq(remote.Name, stream.ID, localHead); err != nil {
				return fmt.Errorf("update push seq: %w", err)
			}

			fmt.Printf("Sent %d operations to %s (server head: %d)\n", totalApplied, remote.Name, serverHead)
			return nil
		},
	}
}

func configuredSendBatchSize() int {
	if raw := os.Getenv("LOOM_SEND_BATCH_SIZE"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return defaultSendBatchSize
}
