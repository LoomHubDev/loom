package cli

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/constructspace/loom/internal/core"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <archive-path>",
		Short: "Import a loom export archive into the current project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			archivePath := args[0]

			// 1. Verify archive exists
			if _, err := os.Stat(archivePath); err != nil {
				return fmt.Errorf("archive not found: %w", err)
			}

			// 2. Resolve target project dir
			absProject, err := filepath.Abs(projectDir)
			if err != nil {
				return fmt.Errorf("resolve project dir: %w", err)
			}

			loomTarget := filepath.Join(absProject, core.LoomDir)
			if _, err := os.Stat(loomTarget); err == nil {
				return fmt.Errorf(".loom directory already exists at %s; remove it before importing", loomTarget)
			}

			// 3. Extract tar.gz
			f, err := os.Open(archivePath)
			if err != nil {
				return fmt.Errorf("open archive: %w", err)
			}
			defer f.Close()

			gr, err := gzip.NewReader(f)
			if err != nil {
				return fmt.Errorf("read gzip: %w", err)
			}
			defer gr.Close()

			tr := tar.NewReader(gr)
			fileCount := 0

			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					return fmt.Errorf("read tar entry: %w", err)
				}

				// Security: strip any absolute or traversal paths
				clean := filepath.Clean(hdr.Name)
				if filepath.IsAbs(clean) {
					continue
				}

				target := filepath.Join(absProject, clean)

				if hdr.FileInfo().IsDir() {
					if err := os.MkdirAll(target, 0755); err != nil {
						return fmt.Errorf("create dir %s: %w", target, err)
					}
					continue
				}

				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					return fmt.Errorf("create parent dir: %w", err)
				}

				outFile, err := os.Create(target)
				if err != nil {
					return fmt.Errorf("create file %s: %w", target, err)
				}
				_, copyErr := io.Copy(outFile, tr)
				outFile.Close()
				if copyErr != nil {
					return fmt.Errorf("write file %s: %w", target, copyErr)
				}
				fileCount++
			}

			// 4. Verify vault opens correctly
			vault, err := core.OpenVault(absProject)
			if err != nil {
				return fmt.Errorf("extracted vault is invalid: %w", err)
			}
			vault.Close()

			// 5. Report
			fmt.Fprintf(cmd.OutOrStdout(), "Imported %d files. Vault OK.\n", fileCount)
			return nil
		},
	}
}
