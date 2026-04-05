package adapter

import (
	"os"
	"path/filepath"
	"testing"
)

// --- CodeAdapter ---

func TestCodeAdapter_Detect(t *testing.T) {
	tests := []struct {
		name   string
		marker string
		isDir  bool
	}{
		{"go.mod", "go.mod", false},
		{"package.json", "package.json", false},
		{".git dir", ".git", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.isDir {
				if err := os.Mkdir(filepath.Join(dir, tc.marker), 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(filepath.Join(dir, tc.marker), []byte(""), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			a := &CodeAdapter{}
			if !a.Detect(dir) {
				t.Errorf("CodeAdapter.Detect should return true for %s", tc.marker)
			}
		})
	}
}

func TestCodeAdapter_DetectFalse(t *testing.T) {
	dir := t.TempDir()
	a := &CodeAdapter{}
	if a.Detect(dir) {
		t.Error("CodeAdapter.Detect should return false for empty dir")
	}
}

func TestCodeAdapter_IsBinary(t *testing.T) {
	a := &CodeAdapter{}

	binaries := []string{"image.png", "photo.jpg", "icon.ico", "lib.wasm", "app.exe"}
	for _, f := range binaries {
		if !a.IsBinary(f) {
			t.Errorf("expected %s to be binary", f)
		}
	}

	textFiles := []string{"main.go", "index.js", "README.md", "Makefile"}
	for _, f := range textFiles {
		if a.IsBinary(f) {
			t.Errorf("expected %s to not be binary", f)
		}
	}
}

func TestCodeAdapter_Diff(t *testing.T) {
	a := &CodeAdapter{}

	old := []byte("line1\nline2\nline3\n")
	new := []byte("line1\nmodified\nline3\n")

	hunks := a.Diff(old, new, "main.go", 3)
	if len(hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}

	// Verify the hunk contains the change.
	found := false
	for _, h := range hunks {
		for _, l := range h.Lines {
			if l.Type == "add" || l.Type == "delete" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected hunks to contain add or delete lines")
	}
}

func TestCodeAdapter_DiffBinary(t *testing.T) {
	a := &CodeAdapter{}
	hunks := a.Diff([]byte("old"), []byte("new"), "image.png", 3)
	if hunks != nil {
		t.Error("expected nil hunks for binary file")
	}
}

// --- DocsAdapter ---

func TestDocsAdapter_Detect(t *testing.T) {
	for _, dirName := range []string{"docs", "doc", "documentation", "wiki"} {
		t.Run(dirName, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, dirName), 0o755); err != nil {
				t.Fatal(err)
			}

			a := &DocsAdapter{}
			if !a.Detect(dir) {
				t.Errorf("DocsAdapter.Detect should return true for %s/", dirName)
			}
		})
	}
}

func TestDocsAdapter_DetectFalse(t *testing.T) {
	dir := t.TempDir()
	a := &DocsAdapter{}
	if a.Detect(dir) {
		t.Error("DocsAdapter.Detect should return false for empty dir")
	}
}

func TestDocsAdapter_Extensions(t *testing.T) {
	a := &DocsAdapter{}
	exts := a.Extensions()

	if len(exts) == 0 {
		t.Fatal("expected docs adapter to have extensions")
	}

	expected := map[string]bool{".md": true, ".txt": true, ".rst": true, ".html": true}
	for ext := range expected {
		found := false
		for _, e := range exts {
			if e == ext {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected extension %s in docs adapter", ext)
		}
	}
}

func TestDocsAdapter_IsBinary(t *testing.T) {
	a := &DocsAdapter{}
	if a.IsBinary("anything.png") {
		t.Error("DocsAdapter.IsBinary should always return false")
	}
}

// --- FilesystemAdapter ---

func TestFilesystemAdapter_Detect(t *testing.T) {
	a := &FilesystemAdapter{}

	// Should return false even with markers present.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.Detect(dir) {
		t.Error("FilesystemAdapter.Detect should always return false")
	}
}

func TestFilesystemAdapter_IsBinary(t *testing.T) {
	a := &FilesystemAdapter{}
	if !a.IsBinary("file.zip") {
		t.Error("expected .zip to be binary")
	}
	if a.IsBinary("file.txt") {
		t.Error("expected .txt to not be binary")
	}
}

func TestFilesystemAdapter_Diff(t *testing.T) {
	a := &FilesystemAdapter{}

	// Text diff works.
	hunks := a.Diff([]byte("a\n"), []byte("b\n"), "file.txt", 3)
	if len(hunks) == 0 {
		t.Error("expected hunks for text diff")
	}

	// Binary returns nil.
	hunks = a.Diff([]byte("a"), []byte("b"), "file.png", 3)
	if hunks != nil {
		t.Error("expected nil hunks for binary diff")
	}
}

// --- Registry ---

func TestRegistry_GetBuiltins(t *testing.T) {
	r := NewRegistry()

	for _, id := range []string{"code", "docs", "filesystem"} {
		if a := r.Get(id); a == nil {
			t.Errorf("expected built-in adapter %q to be registered", id)
		}
	}
}

func TestRegistry_ForSpace_Fallback(t *testing.T) {
	r := NewRegistry()

	a := r.ForSpace("nonexistent")
	if a == nil {
		t.Fatal("ForSpace should return a fallback adapter")
	}
	if a.ID() != "filesystem" {
		t.Errorf("expected filesystem fallback, got %q", a.ID())
	}

	// Known adapter works.
	a = r.ForSpace("code")
	if a == nil || a.ID() != "code" {
		t.Error("ForSpace(\"code\") should return code adapter")
	}
}

func TestRegistry_DetectSpaces(t *testing.T) {
	dir := t.TempDir()

	// Create .git dir and docs/ dir.
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	spaces := r.DetectSpaces(dir)

	if _, ok := spaces["code"]; !ok {
		t.Error("expected code adapter to be detected")
	}
	if _, ok := spaces["docs"]; !ok {
		t.Error("expected docs adapter to be detected")
	}
	if _, ok := spaces["filesystem"]; ok {
		t.Error("filesystem adapter should never be auto-detected")
	}
}
