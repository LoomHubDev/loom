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

func newExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export [output-path]",
		Short: "Export the .loom directory as a tar.gz archive",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			loomPath := vault.LoomPath
			projectName := filepath.Base(vault.ProjectPath)
			vault.Close()

			// Determine output path
			outPath := projectName + "-loom-export.tar.gz"
			if len(args) > 0 {
				outPath = args[0]
			}

			outFile, err := os.Create(outPath)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer outFile.Close()

			gw := gzip.NewWriter(outFile)
			defer gw.Close()
			tw := tar.NewWriter(gw)
			defer tw.Close()

			fileCount := 0
			var totalBytes int64

			baseDir := filepath.Dir(loomPath)
			err = filepath.Walk(loomPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				rel, err := filepath.Rel(baseDir, path)
				if err != nil {
					return err
				}

				hdr, err := tar.FileInfoHeader(info, "")
				if err != nil {
					return err
				}
				hdr.Name = rel
				if info.IsDir() {
					hdr.Name += "/"
				}

				if err := tw.WriteHeader(hdr); err != nil {
					return err
				}

				if !info.IsDir() {
					f, err := os.Open(path)
					if err != nil {
						return err
					}
					defer f.Close()
					n, err := io.Copy(tw, f)
					if err != nil {
						return err
					}
					totalBytes += n
					fileCount++
				}

				return nil
			})
			if err != nil {
				return fmt.Errorf("walk .loom directory: %w", err)
			}

			// Flush writers before reporting size
			if err := tw.Close(); err != nil {
				return fmt.Errorf("close tar: %w", err)
			}
			if err := gw.Close(); err != nil {
				return fmt.Errorf("close gzip: %w", err)
			}

			outInfo, _ := os.Stat(outPath)
			archiveSize := int64(0)
			if outInfo != nil {
				archiveSize = outInfo.Size()
			}

			absOut, _ := filepath.Abs(outPath)
			fmt.Fprintf(cmd.OutOrStdout(), "Exported to %s (%d files, %d bytes)\n", absOut, fileCount, archiveSize)
			return nil
		},
	}
}
