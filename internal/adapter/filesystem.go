package adapter

import (
	"path/filepath"
	"strings"

	"github.com/constructspace/loom/internal/diff"
)

// FilesystemAdapter is a generic fallback adapter for spaces that do not match
// any specialized adapter (design, notes, data, config, etc.).
type FilesystemAdapter struct{}

// ID returns the adapter identifier.
func (a *FilesystemAdapter) ID() string { return "filesystem" }

// Name returns a human-friendly name.
func (a *FilesystemAdapter) Name() string { return "Filesystem" }

// Detect always returns false. The filesystem adapter is used as a fallback
// and is never auto-detected.
func (a *FilesystemAdapter) Detect(_ string) bool { return false }

// Extensions returns nil — the filesystem adapter handles all file types.
func (a *FilesystemAdapter) Extensions() []string { return nil }

// Diff computes a text diff for text files. Returns nil for binary files.
func (a *FilesystemAdapter) Diff(oldContent, newContent []byte, path string, contextLines int) []diff.DiffHunk {
	if a.IsBinary(path) {
		return nil
	}
	return diff.TextDiff(oldContent, newContent, contextLines)
}

// IsBinary returns true if the path has a known binary file extension.
func (a *FilesystemAdapter) IsBinary(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return binaryExtensions[ext]
}
