package adapter

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/constructspace/loom/internal/diff"
)

// binaryExtensions is the shared set of extensions considered binary.
var binaryExtensions = map[string]bool{
	".png":   true,
	".jpg":   true,
	".jpeg":  true,
	".gif":   true,
	".ico":   true,
	".bmp":   true,
	".webp":  true,
	".wasm":  true,
	".exe":   true,
	".dll":   true,
	".so":    true,
	".dylib": true,
	".zip":   true,
	".tar":   true,
	".gz":    true,
	".bz2":   true,
	".xz":    true,
	".pdf":   true,
	".woff":  true,
	".woff2": true,
	".ttf":   true,
	".otf":   true,
	".eot":   true,
	".mp3":   true,
	".mp4":   true,
	".wav":   true,
	".avi":   true,
	".mov":   true,
}

// codeMarkers are files/directories whose presence indicates a code project.
var codeMarkers = []string{
	".git",
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"Makefile",
	"CMakeLists.txt",
	"pom.xml",
}

// CodeAdapter handles code spaces.
type CodeAdapter struct{}

// ID returns the adapter identifier.
func (a *CodeAdapter) ID() string { return "code" }

// Name returns a human-friendly name.
func (a *CodeAdapter) Name() string { return "Code" }

// Detect returns true if projectPath contains any code project markers.
func (a *CodeAdapter) Detect(projectPath string) bool {
	for _, marker := range codeMarkers {
		path := filepath.Join(projectPath, marker)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// Extensions returns nil — the code adapter handles all files in a code space.
func (a *CodeAdapter) Extensions() []string { return nil }

// Diff computes a text diff between old and new content. Returns nil for binary files.
func (a *CodeAdapter) Diff(oldContent, newContent []byte, path string, contextLines int) []diff.DiffHunk {
	if a.IsBinary(path) {
		return nil
	}
	return diff.TextDiff(oldContent, newContent, contextLines)
}

// IsBinary returns true if the path has a known binary file extension.
func (a *CodeAdapter) IsBinary(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return binaryExtensions[ext]
}
