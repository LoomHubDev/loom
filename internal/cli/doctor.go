package cli

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/constructspace/loom/internal/core"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run integrity checks on the vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Loom Doctor")

			failed := false

			// 1. DB Schema
			var schemaVersion int
			err = vault.DB.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&schemaVersion)
			if err != nil {
				fmt.Fprintf(out, "  [FAIL] Schema version table missing or unreadable: %v\n", err)
				failed = true
			} else if schemaVersion != 1 {
				fmt.Fprintf(out, "  [FAIL] Schema version is %d, expected 1\n", schemaVersion)
				failed = true
			} else {
				fmt.Fprintf(out, "  [OK] Schema version: %d\n", schemaVersion)
			}

			// 2. Seq counter
			var seqCounter int64
			err = vault.DB.QueryRow("SELECT CAST(value AS INTEGER) FROM metadata WHERE key = 'seq_counter'").Scan(&seqCounter)
			if err != nil {
				fmt.Fprintf(out, "  [FAIL] Cannot read seq_counter: %v\n", err)
				failed = true
			} else {
				var maxSeq int64
				row := vault.DB.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM operations")
				row.Scan(&maxSeq)
				if seqCounter != maxSeq {
					fmt.Fprintf(out, "  [FAIL] seq_counter is %d but max(seq) in operations is %d\n", seqCounter, maxSeq)
					failed = true
				} else {
					fmt.Fprintf(out, "  [OK] Sequence counter: %d\n", seqCounter)
				}
			}

			// 3. Stream heads
			rows, err := vault.DB.Query("SELECT id, name, head_seq FROM streams")
			if err != nil {
				fmt.Fprintf(out, "  [FAIL] Cannot query streams: %v\n", err)
				failed = true
			} else {
				streamsFailed := false
				streamCount := 0
				for rows.Next() {
					streamCount++
					var id, name string
					var headSeq int64
					if err := rows.Scan(&id, &name, &headSeq); err != nil {
						continue
					}
					var maxOpSeq int64
					vault.DB.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM operations WHERE stream_id = ?", id).Scan(&maxOpSeq)
					if headSeq != maxOpSeq {
						fmt.Fprintf(out, "  [FAIL] Stream %q head_seq is %d but max op seq is %d\n", name, headSeq, maxOpSeq)
						failed = true
						streamsFailed = true
					}
				}
				rows.Close()
				if !streamsFailed {
					fmt.Fprintf(out, "  [OK] Stream heads consistent (%d streams)\n", streamCount)
				}
			}

			// 4. Object refs
			refRows, err := vault.DB.Query("SELECT object_ref FROM operations WHERE object_ref IS NOT NULL AND object_ref != ''")
			if err != nil {
				fmt.Fprintf(out, "  [FAIL] Cannot query object refs: %v\n", err)
				failed = true
			} else {
				total := 0
				valid := 0
				for refRows.Next() {
					var ref string
					if err := refRows.Scan(&ref); err != nil {
						continue
					}
					total++
					if vault.Store.Exists(ref) {
						valid++
					}
				}
				refRows.Close()
				if valid != total {
					fmt.Fprintf(out, "  [FAIL] Object references: %d/%d valid\n", valid, total)
					failed = true
				} else {
					fmt.Fprintf(out, "  [OK] Object references: %d/%d valid\n", valid, total)
				}
			}

			// 5. Orphan objects
			var orphanCount int
			err = vault.DB.QueryRow(`
				SELECT COUNT(*) FROM objects
				WHERE hash NOT IN (
					SELECT object_ref FROM operations WHERE object_ref IS NOT NULL AND object_ref != ''
				)`).Scan(&orphanCount)
			if err != nil && err != sql.ErrNoRows {
				fmt.Fprintf(out, "  [WARN] Cannot check orphan objects: %v\n", err)
			} else if orphanCount > 0 {
				fmt.Fprintf(out, "  [WARN] %d orphan objects (not referenced by any operation)\n", orphanCount)
			}

			fmt.Fprintln(out)
			if failed {
				fmt.Fprintln(out, "Some checks failed.")
				os.Exit(1)
			}
			fmt.Fprintln(out, "All checks passed.")
			return nil
		},
	}
}
