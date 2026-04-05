package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRestoreEnv(t *testing.T) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "docs", "readme.md"), []byte("# Hello\n"), 0644)

	vault, err := InitVault(dir)
	require.NoError(t, err)
	t.Cleanup(func() { vault.Close() })
	return vault, dir
}

func TestRestoreEngine_FullRestore(t *testing.T) {
	vault, dir := setupRestoreEnv(t)

	stream, err := vault.ActiveStream()
	require.NoError(t, err)

	// Create checkpoint "v1" with initial state
	cpV1, err := vault.Checkpoints.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "v1",
		Author:   "test",
		Source:   SourceManual,
	})
	require.NoError(t, err)

	// Modify main.go: write new content to disk and object store, then op
	newContent := []byte("package main\nfunc main() { fmt.Println(\"modified\") }\n")
	hash, err := vault.Store.Write(newContent, "text/x-go")
	require.NoError(t, err)
	os.WriteFile(filepath.Join(dir, "main.go"), newContent, 0644)
	_, err = vault.OpWriter.Write(Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      OpModify,
		Path:      "main.go",
		ObjectRef: hash,
		Author:    "test",
		Meta:      OpMeta{Size: int64(len(newContent))},
	})
	require.NoError(t, err)

	// Create checkpoint "v2"
	_, err = vault.Checkpoints.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "v2",
		Author:   "test",
		Source:   SourceManual,
	})
	require.NoError(t, err)

	// Restore to v1
	engine := NewRestoreEngine(vault)
	restoreCP, err := engine.Restore(cpV1.ID, RestoreScope{Full: true})
	require.NoError(t, err)

	// File should have original content
	content, err := os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, "package main\nfunc main() {}\n", string(content))

	// Restore checkpoint should exist
	assert.Equal(t, SourceRestore, restoreCP.Source)
	assert.Contains(t, restoreCP.Title, "Restored to v1")
	assert.Equal(t, cpV1.ID, restoreCP.Tags["restored_from"])

	// Verify guard checkpoint was also created
	cps, err := vault.Checkpoints.List(stream.ID, 10)
	require.NoError(t, err)

	var hasGuard bool
	for _, cp := range cps {
		if cp.Source == SourceGuard {
			hasGuard = true
			assert.Contains(t, cp.Title, "Guard before restore to v1")
			assert.Equal(t, cpV1.ID, cp.Tags["restore_target"])
		}
	}
	assert.True(t, hasGuard, "guard checkpoint should exist")
}

func TestRestoreEngine_RestoreCreatesGuardCheckpoint(t *testing.T) {
	vault, _ := setupRestoreEnv(t)

	stream, err := vault.ActiveStream()
	require.NoError(t, err)

	cp, err := vault.Checkpoints.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "target",
		Author:   "test",
		Source:   SourceManual,
	})
	require.NoError(t, err)

	engine := NewRestoreEngine(vault)
	_, err = engine.Restore(cp.ID, RestoreScope{Full: true})
	require.NoError(t, err)

	// Find guard checkpoint
	cps, err := vault.Checkpoints.List(stream.ID, 10)
	require.NoError(t, err)

	var guardFound bool
	for _, c := range cps {
		if c.Source == SourceGuard {
			guardFound = true
			assert.Equal(t, cp.ID, c.Tags["restore_target"])
		}
	}
	assert.True(t, guardFound, "guard checkpoint should be created before restore")
}

func TestRestoreEngine_RestoreCreatesRestoreCheckpoint(t *testing.T) {
	vault, _ := setupRestoreEnv(t)

	stream, err := vault.ActiveStream()
	require.NoError(t, err)

	cp, err := vault.Checkpoints.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "baseline",
		Author:   "test",
		Source:   SourceManual,
	})
	require.NoError(t, err)

	engine := NewRestoreEngine(vault)
	restoreCP, err := engine.Restore(cp.ID, RestoreScope{Full: true})
	require.NoError(t, err)

	assert.Equal(t, SourceRestore, restoreCP.Source)
	assert.Contains(t, restoreCP.Title, "Restored to baseline")
	assert.Equal(t, cp.ID, restoreCP.Tags["restored_from"])
}

func TestRestoreEngine_RestoreByEntity(t *testing.T) {
	vault, dir := setupRestoreEnv(t)

	stream, err := vault.ActiveStream()
	require.NoError(t, err)

	// Checkpoint with initial state
	cpV1, err := vault.Checkpoints.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "v1",
		Author:   "test",
		Source:   SourceManual,
	})
	require.NoError(t, err)

	// Modify main.go
	newMainContent := []byte("package main // changed\n")
	hashMain, err := vault.Store.Write(newMainContent, "text/x-go")
	require.NoError(t, err)
	os.WriteFile(filepath.Join(dir, "main.go"), newMainContent, 0644)
	_, err = vault.OpWriter.Write(Operation{
		StreamID: stream.ID, SpaceID: "code", EntityID: "main.go",
		Type: OpModify, Path: "main.go", ObjectRef: hashMain, Author: "test",
		Meta: OpMeta{Size: int64(len(newMainContent))},
	})
	require.NoError(t, err)

	// Modify go.mod
	newModContent := []byte("module test\n\ngo 1.25\n")
	hashMod, err := vault.Store.Write(newModContent, "text/plain")
	require.NoError(t, err)
	os.WriteFile(filepath.Join(dir, "go.mod"), newModContent, 0644)
	_, err = vault.OpWriter.Write(Operation{
		StreamID: stream.ID, SpaceID: "code", EntityID: "go.mod",
		Type: OpModify, Path: "go.mod", ObjectRef: hashMod, Author: "test",
		Meta: OpMeta{Size: int64(len(newModContent))},
	})
	require.NoError(t, err)

	// Restore only main.go
	engine := NewRestoreEngine(vault)
	_, err = engine.Restore(cpV1.ID, RestoreScope{EntityID: "main.go"})
	require.NoError(t, err)

	// main.go should have original content
	content, err := os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, "package main\nfunc main() {}\n", string(content))

	// go.mod should still have modified content
	modContent, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	assert.Equal(t, "module test\n\ngo 1.25\n", string(modContent))
}

func TestRestoreEngine_RestoreNonexistentCheckpoint(t *testing.T) {
	vault, _ := setupRestoreEnv(t)

	engine := NewRestoreEngine(vault)
	_, err := engine.Restore("nonexistent-id-12345", RestoreScope{Full: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get target checkpoint")
}

func TestRestoreEngine_RestoreDeletedFile(t *testing.T) {
	vault, dir := setupRestoreEnv(t)

	stream, err := vault.ActiveStream()
	require.NoError(t, err)

	// Checkpoint with initial state (main.go exists)
	cpV1, err := vault.Checkpoints.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "v1",
		Author:   "test",
		Source:   SourceManual,
	})
	require.NoError(t, err)

	// Delete main.go: remove from disk and write delete op
	os.Remove(filepath.Join(dir, "main.go"))
	_, err = vault.OpWriter.Write(Operation{
		StreamID: stream.ID, SpaceID: "code", EntityID: "main.go",
		Type: OpDelete, Path: "main.go", Author: "test",
	})
	require.NoError(t, err)

	// Verify file is gone
	_, err = os.Stat(filepath.Join(dir, "main.go"))
	require.True(t, os.IsNotExist(err), "main.go should be deleted")

	// Restore to v1
	engine := NewRestoreEngine(vault)
	_, err = engine.Restore(cpV1.ID, RestoreScope{Full: true})
	require.NoError(t, err)

	// File should exist again with original content
	content, err := os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, "package main\nfunc main() {}\n", string(content))
}
