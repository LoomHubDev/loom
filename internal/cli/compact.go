package cli

import (
	"fmt"
	"os"

	"github.com/constructspace/loom/internal/core"
	"github.com/spf13/cobra"
)

func newCompactCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "compact",
		Short: "Compact the operation log by removing superseded operations",
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			// 1. Find oldest checkpoint seq across all streams
			var oldestCheckpointSeq int64
			err = vault.DB.QueryRow("SELECT COALESCE(MIN(seq), 0) FROM checkpoints").Scan(&oldestCheckpointSeq)
			if err != nil {
				return fmt.Errorf("query checkpoints: %w", err)
			}

			if oldestCheckpointSeq == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No checkpoints found; nothing safe to compact.")
				return nil
			}

			// Measure DB size before
			dbInfo, err := os.Stat(vault.LoomPath + "/loom.db")
			if err != nil {
				return fmt.Errorf("stat db: %w", err)
			}
			sizeBefore := dbInfo.Size()

			// 2. For each (stream_id, entity_id) pair, find operations before the oldest checkpoint
			// that are superseded (there is a newer op with an object_ref for the same entity).
			// Keep only the latest op per entity before the checkpoint; delete the rest.
			//
			// Strategy:
			//   - Collect IDs of the "latest op before oldest checkpoint" per (stream_id, entity_id)
			//     that has an object_ref (i.e. represents a full snapshot).
			//   - Delete all *other* ops for that entity before the checkpoint.

			// Find entities that have at least 2 ops before oldestCheckpointSeq with an object_ref.
			candidateRows, err := vault.DB.Query(`
				SELECT stream_id, entity_id, MAX(seq) AS keep_seq
				FROM operations
				WHERE seq < ? AND object_ref IS NOT NULL AND object_ref != ''
				GROUP BY stream_id, entity_id
				HAVING COUNT(*) > 1
			`, oldestCheckpointSeq)
			if err != nil {
				return fmt.Errorf("query compaction candidates: %w", err)
			}

			type candidate struct {
				streamID string
				entityID string
				keepSeq  int64
			}
			var candidates []candidate
			for candidateRows.Next() {
				var c candidate
				if err := candidateRows.Scan(&c.streamID, &c.entityID, &c.keepSeq); err != nil {
					candidateRows.Close()
					return fmt.Errorf("scan candidate: %w", err)
				}
				candidates = append(candidates, c)
			}
			candidateRows.Close()

			if len(candidates) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Compacted: removed 0 operations, freed 0 bytes")
				return nil
			}

			// 3. Delete superseded ops
			tx, err := vault.DB.Begin()
			if err != nil {
				return fmt.Errorf("begin tx: %w", err)
			}
			defer tx.Rollback()

			totalRemoved := int64(0)
			for _, c := range candidates {
				result, err := tx.Exec(`
					DELETE FROM operations
					WHERE stream_id = ? AND entity_id = ? AND seq < ? AND seq < ?
					  AND object_ref IS NOT NULL AND object_ref != ''
				`, c.streamID, c.entityID, c.keepSeq, oldestCheckpointSeq)
				if err != nil {
					return fmt.Errorf("delete ops for %s/%s: %w", c.streamID, c.entityID, err)
				}
				n, _ := result.RowsAffected()
				totalRemoved += n
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit: %w", err)
			}

			// 4. VACUUM
			if _, err := vault.DB.Exec("VACUUM"); err != nil {
				return fmt.Errorf("vacuum: %w", err)
			}

			// Measure size after
			dbInfoAfter, _ := os.Stat(vault.LoomPath + "/loom.db")
			sizeAfter := int64(0)
			if dbInfoAfter != nil {
				sizeAfter = dbInfoAfter.Size()
			}
			freed := sizeBefore - sizeAfter

			fmt.Fprintf(cmd.OutOrStdout(), "Compacted: removed %d operations, freed %d bytes\n", totalRemoved, freed)
			return nil
		},
	}
}
