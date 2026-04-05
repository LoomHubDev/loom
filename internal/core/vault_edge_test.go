package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitVault_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	// No files at all

	vault, err := InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	assert.DirExists(t, filepath.Join(dir, ".loom"))
	assert.FileExists(t, filepath.Join(dir, ".loom", "config.toml"))
	assert.FileExists(t, filepath.Join(dir, ".loom", "loom.db"))

	// No spaces detected
	assert.Empty(t, vault.Config.Spaces)
}

func TestInitVault_CreatesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	vault, err := InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	assert.DirExists(t, filepath.Join(dir, ".loom", "objects"))
	assert.DirExists(t, filepath.Join(dir, ".loom", "locks"))
	assert.DirExists(t, filepath.Join(dir, ".loom", "hooks"))
}

func TestInitVault_DefaultConfig(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	vault, err := InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	assert.Equal(t, 1, vault.Config.Project.Version)
	assert.True(t, vault.Config.Watch.Enabled)
	assert.Equal(t, 500, vault.Config.Watch.DebounceMs)
	assert.Contains(t, vault.Config.Watch.Ignore, ".git")
	assert.Contains(t, vault.Config.Watch.Ignore, "node_modules")
	assert.Contains(t, vault.Config.Watch.Ignore, ".loom")
	assert.True(t, vault.Config.CPoint.Auto)
	assert.Equal(t, 50, vault.Config.CPoint.IntervalOps)
	assert.Equal(t, 300, vault.Config.CPoint.IntervalSeconds)
	assert.True(t, vault.Config.CPoint.OnSignificantChange)
}

func TestInitVault_ProjectNameFromDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	vault, err := InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	assert.Equal(t, filepath.Base(dir), vault.Config.Project.Name)
}

func TestInitVault_WithNameOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	vault, err := InitVault(dir, WithName("My Awesome Project"))
	require.NoError(t, err)
	defer vault.Close()

	assert.Equal(t, "My Awesome Project", vault.Config.Project.Name)
}

func TestInitVault_DoubleInitFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	v1, err := InitVault(dir)
	require.NoError(t, err)
	v1.Close()

	_, err = InitVault(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already initialized")
}

func TestOpenVault_NonLoomDirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := OpenVault(dir)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNotInit)
}

func TestOpenVault_PreservesConfig(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
	os.WriteFile(filepath.Join(dir, "docs", "readme.md"), []byte("# Hi"), 0644)

	v1, err := InitVault(dir, WithName("TestProject"))
	require.NoError(t, err)
	v1.Close()

	v2, err := OpenVault(dir)
	require.NoError(t, err)
	defer v2.Close()

	assert.Equal(t, "TestProject", v2.Config.Project.Name)
	assert.Equal(t, 1, v2.Config.Project.Version)
	_, hasCode := v2.Config.Spaces["code"]
	assert.True(t, hasCode)
}

func TestOpenVault_FromNestedDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c", "d")
	os.MkdirAll(nested, 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	v1, err := InitVault(dir)
	require.NoError(t, err)
	v1.Close()

	v2, err := OpenVault(nested)
	require.NoError(t, err)
	defer v2.Close()

	assert.Equal(t, dir, v2.ProjectPath)
}

func TestVault_Close_Idempotent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	vault, err := InitVault(dir)
	require.NoError(t, err)

	// Close multiple times should not panic
	err = vault.Close()
	assert.NoError(t, err)
}

func TestVault_Close_NilDB(t *testing.T) {
	vault := &Vault{}
	err := vault.Close()
	assert.NoError(t, err)
}

func TestVault_ActiveStream_ReturnsMainByDefault(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	vault, err := InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	stream, err := vault.ActiveStream()
	require.NoError(t, err)
	assert.Equal(t, "main", stream.Name)
}

func TestVault_EntityCount_NoEntities(t *testing.T) {
	dir := t.TempDir()
	// No files to track

	vault, err := InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	counts, err := vault.EntityCount()
	require.NoError(t, err)
	assert.Empty(t, counts)
}

func TestVault_EntityCount_MultipleSpaces(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
	os.WriteFile(filepath.Join(dir, "docs", "readme.md"), []byte("# Hi"), 0644)
	os.WriteFile(filepath.Join(dir, "docs", "guide.md"), []byte("# Guide"), 0644)

	vault, err := InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	counts, err := vault.EntityCount()
	require.NoError(t, err)

	assert.Greater(t, counts["code"], 0)
	assert.Equal(t, 2, counts["docs"])
}

// --- detectSpaces ---

func TestDetectSpaces_GitRepo(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)

	spaces := detectSpaces(dir)
	assert.Contains(t, spaces, "code")
	assert.Equal(t, "git", spaces["code"].Adapter)
	assert.Equal(t, ".", spaces["code"].Path)
}

func TestDetectSpaces_GoProject(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	spaces := detectSpaces(dir)
	assert.Contains(t, spaces, "code")
	assert.Equal(t, "filesystem", spaces["code"].Adapter)
}

func TestDetectSpaces_NodeProject(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)

	spaces := detectSpaces(dir)
	assert.Contains(t, spaces, "code")
}

func TestDetectSpaces_RustProject(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]"), 0644)

	spaces := detectSpaces(dir)
	assert.Contains(t, spaces, "code")
}

func TestDetectSpaces_PythonProject(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]"), 0644)

	spaces := detectSpaces(dir)
	assert.Contains(t, spaces, "code")
}

func TestDetectSpaces_DocsDirectory(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"docs", "doc", "documentation"} {
		subDir := filepath.Join(dir, name)
		os.MkdirAll(subDir, 0755)
		spaces := detectSpaces(dir)
		assert.Contains(t, spaces, "docs", "should detect %s as docs space", name)
		os.RemoveAll(subDir) // clean for next iteration
	}
}

func TestDetectSpaces_DesignDirectory(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "design"), 0755)

	spaces := detectSpaces(dir)
	assert.Contains(t, spaces, "design")
	assert.Equal(t, "design", spaces["design"].Adapter)
}

func TestDetectSpaces_NotesDirectory(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "notes"), 0755)

	spaces := detectSpaces(dir)
	assert.Contains(t, spaces, "notes")
}

func TestDetectSpaces_EmptyProject(t *testing.T) {
	dir := t.TempDir()

	spaces := detectSpaces(dir)
	assert.Empty(t, spaces)
}

func TestDetectSpaces_GitPrioritizedOverFilesystem(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	spaces := detectSpaces(dir)
	// .git takes priority — adapter should be "git"
	assert.Equal(t, "git", spaces["code"].Adapter)
}

// --- detectMIME ---

func TestDetectMIME_KnownExtensions(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "text/x-go"},
		{"app.js", "text/javascript"},
		{"index.ts", "text/typescript"},
		{"script.py", "text/x-python"},
		{"lib.rs", "text/x-rust"},
		{"README.md", "text/markdown"},
		{"config.json", "application/json"},
		{"config.yaml", "text/yaml"},
		{"config.yml", "text/yaml"},
		{"settings.toml", "text/toml"},
		{"index.html", "text/html"},
		{"style.css", "text/css"},
		{"schema.sql", "text/x-sql"},
		{"notes.txt", "text/plain"},
		{"logo.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"icon.svg", "image/svg+xml"},
		{"anim.gif", "image/gif"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, detectMIME(tt.path))
		})
	}
}

func TestDetectMIME_UnknownExtension(t *testing.T) {
	assert.Equal(t, "application/octet-stream", detectMIME("file.xyz"))
	assert.Equal(t, "application/octet-stream", detectMIME("file.wasm"))
	assert.Equal(t, "application/octet-stream", detectMIME("file.bin"))
}

func TestDetectMIME_NoExtension(t *testing.T) {
	assert.Equal(t, "application/octet-stream", detectMIME("Makefile"))
	assert.Equal(t, "application/octet-stream", detectMIME("Dockerfile"))
}

func TestDetectMIME_CaseInsensitive(t *testing.T) {
	assert.Equal(t, "text/x-go", detectMIME("main.GO"))
	assert.Equal(t, "text/javascript", detectMIME("app.JS"))
	assert.Equal(t, "image/png", detectMIME("logo.PNG"))
}

func TestDetectMIME_PathWithDirs(t *testing.T) {
	assert.Equal(t, "text/x-go", detectMIME("src/pkg/main.go"))
	assert.Equal(t, "application/json", detectMIME("/root/config/settings.json"))
}

// --- scanDirectory ---

func TestScanDirectory_IgnoresPatterns(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "temp.tmp"), []byte("temp"), 0644)
	os.WriteFile(filepath.Join(dir, "file.swp"), []byte("swap"), 0644)

	entities := scanDirectory(dir, "code", []string{"*.tmp", "*.swp"}, nil)

	paths := make(map[string]bool)
	for _, e := range entities {
		paths[e.Path] = true
	}
	assert.True(t, paths["main.go"])
	assert.False(t, paths["temp.tmp"])
	assert.False(t, paths["file.swp"])
}

func TestScanDirectory_IgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0755)
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("config"), 0644)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "index.js"), []byte("module"), 0644)

	entities := scanDirectory(dir, "code", []string{".git", "node_modules"}, nil)

	for _, e := range entities {
		assert.NotContains(t, e.Path, ".git")
		assert.NotContains(t, e.Path, "node_modules")
	}
	assert.Len(t, entities, 1)
	assert.Equal(t, "main.go", entities[0].Path)
}

func TestScanDirectory_ExcludesSubspaces(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "docs", "readme.md"), []byte("# Hi"), 0644)

	entities := scanDirectory(dir, "code", nil, []string{"docs"})

	paths := make(map[string]bool)
	for _, e := range entities {
		paths[e.Path] = true
	}
	assert.True(t, paths["main.go"])
	assert.False(t, paths["docs/readme.md"])
}

func TestScanDirectory_EntityFields(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	entities := scanDirectory(dir, "code", nil, nil)
	require.Len(t, entities, 1)

	e := entities[0]
	assert.Equal(t, "main.go", e.ID)
	assert.Equal(t, "code", e.SpaceID)
	assert.Equal(t, "file", e.Kind)
	assert.Equal(t, "main.go", e.Path)
	assert.Equal(t, int64(12), e.Size) // "package main" = 12 bytes
	assert.NotEmpty(t, e.ModTime)
	assert.Equal(t, "text/x-go", e.Meta["content_type"])
}

func TestScanDirectory_Empty(t *testing.T) {
	dir := t.TempDir()
	entities := scanDirectory(dir, "code", nil, nil)
	assert.Empty(t, entities)
}

func TestScanDirectory_Nested(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b", "c"), 0755)
	os.WriteFile(filepath.Join(dir, "root.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "a", "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(dir, "a", "b", "b.go"), []byte("package b"), 0644)
	os.WriteFile(filepath.Join(dir, "a", "b", "c", "c.go"), []byte("package c"), 0644)

	entities := scanDirectory(dir, "code", nil, nil)
	assert.Len(t, entities, 4)

	paths := make(map[string]bool)
	for _, e := range entities {
		paths[e.Path] = true
	}
	assert.True(t, paths["root.go"])
	assert.True(t, paths[filepath.Join("a", "a.go")])
	assert.True(t, paths[filepath.Join("a", "b", "b.go")])
	assert.True(t, paths[filepath.Join("a", "b", "c", "c.go")])
}

// --- findLoomDir ---

func TestFindLoomDir_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := findLoomDir(dir)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNotInit)
}

func TestFindLoomDir_InCurrentDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".loom"), 0755)

	found, err := findLoomDir(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".loom"), found)
}

func TestFindLoomDir_WalksUp(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".loom"), 0755)
	nested := filepath.Join(dir, "a", "b", "c")
	os.MkdirAll(nested, 0755)

	found, err := findLoomDir(nested)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".loom"), found)
}

// --- WithName option ---

func TestWithName_SetsName(t *testing.T) {
	var opts initOptions
	WithName("custom")(&opts)
	assert.Equal(t, "custom", opts.name)
}

func TestWithName_EmptyString(t *testing.T) {
	var opts initOptions
	WithName("")(&opts)
	assert.Empty(t, opts.name)
}
