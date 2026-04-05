package adapter

import (
	"os"
	"path/filepath"

	"github.com/constructspace/loom/internal/diff"
)

// docsDirectories are directory names that indicate a documentation space.
var docsDirectories = []string{
	"docs",
	"doc",
	"documentation",
	"wiki",
}

// docsExtensions are file extensions handled by the documentation adapter.
var docsExtensions = []string{
	".md",
	".mdx",
	".txt",
	".rst",
	".adoc",
	".tex",
	".html",
}

// DocsAdapter handles documentation spaces.
type DocsAdapter struct{}

// ID returns the adapter identifier.
func (a *DocsAdapter) ID() string { return "docs" }

// Name returns a human-friendly name.
func (a *DocsAdapter) Name() string { return "Documentation" }

// Detect returns true if projectPath contains a documentation directory.
func (a *DocsAdapter) Detect(projectPath string) bool {
	for _, dir := range docsDirectories {
		path := filepath.Join(projectPath, dir)
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// Extensions returns the file extensions this adapter handles.
func (a *DocsAdapter) Extensions() []string {
	return docsExtensions
}

// Diff computes a text diff between old and new content.
func (a *DocsAdapter) Diff(oldContent, newContent []byte, path string, contextLines int) []diff.DiffHunk {
	return diff.TextDiff(oldContent, newContent, contextLines)
}

// IsBinary always returns false — documentation files are text.
func (a *DocsAdapter) IsBinary(_ string) bool { return false }
