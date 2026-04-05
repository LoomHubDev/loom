package adapter

import "github.com/constructspace/loom/internal/diff"

// SpaceAdapter is the interface that all space adapters must implement.
// Adapters detect space types, know what file extensions to handle,
// produce diffs, and identify binary files.
type SpaceAdapter interface {
	// ID returns the adapter identifier (e.g., "code", "docs", "filesystem").
	ID() string
	// Name returns a human-friendly name.
	Name() string
	// Detect returns true if this adapter can handle the given directory.
	Detect(projectPath string) bool
	// Extensions returns file extensions this adapter handles (empty = all files).
	Extensions() []string
	// Diff computes a diff between old and new content.
	Diff(oldContent, newContent []byte, path string, contextLines int) []diff.DiffHunk
	// IsBinary returns true if the given path is a binary file.
	IsBinary(path string) bool
}
