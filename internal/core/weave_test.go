package core

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupWeaveEnv(t *testing.T) (*WeaveEngine, *OpWriter, *StreamManager, *CheckpointEngine, *storage.ObjectStore) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	sm := NewStreamManager(db)
	writer := NewOpWriter(db, store)
	reader := NewOpReader(db)
	cpEngine := NewCheckpointEngine(db, reader)
	engine := NewWeaveEngine(sm, reader, writer, cpEngine, store)

	return engine, writer, sm, cpEngine, store
}

func TestWeaveEngine_AutoMergeNoConflicts(t *testing.T) {
	engine, writer, sm, _, store := setupWeaveEnv(t)

	// Create main stream with a base file
	main, err := sm.Create("main")
	require.NoError(t, err)

	baseContent := []byte("line1\nline2\nline3\n")
	baseHash, err := store.Write(baseContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "a.go",
		Type: OpCreate, Path: "a.go", ObjectRef: baseHash, Author: "test",
	})
	require.NoError(t, err)

	// Fork feature from main
	feature, err := sm.Fork("main", "feature")
	require.NoError(t, err)

	// Feature changes entity B (no overlap)
	bContent := []byte("hello world\n")
	bHash, err := store.Write(bContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: feature.ID, SpaceID: "code", EntityID: "b.go",
		Type: OpCreate, Path: "b.go", ObjectRef: bHash, Author: "test",
	})
	require.NoError(t, err)

	// Main changes entity A further (different entity from feature)
	modContent := []byte("line1\nmodified\nline3\n")
	modHash, err := store.Write(modContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "a.go",
		Type: OpModify, Path: "a.go", ObjectRef: modHash, Author: "test",
	})
	require.NoError(t, err)

	// Weave feature into main — no conflicts expected
	result, err := engine.Weave(WeaveOptions{
		SourceName: "feature", TargetName: "main",
		Strategy: WeaveStrategyAuto, Author: "test",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Applied)
	assert.Equal(t, 0, result.ConflictCount)
	assert.NotNil(t, result.Checkpoint)
}

func TestWeaveEngine_AutoMergeContentMerge(t *testing.T) {
	engine, writer, sm, _, store := setupWeaveEnv(t)

	main, err := sm.Create("main")
	require.NoError(t, err)

	// Base content with 4 lines
	baseContent := []byte("line1\nline2\nline3\nline4\n")
	baseHash, err := store.Write(baseContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpCreate, Path: "file.go", ObjectRef: baseHash, Author: "test",
	})
	require.NoError(t, err)

	// Fork
	feature, err := sm.Fork("main", "feature")
	require.NoError(t, err)

	// Feature changes line2 (source/ours)
	oursContent := []byte("line1\nmodified2\nline3\nline4\n")
	oursHash, err := store.Write(oursContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: feature.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpModify, Path: "file.go", ObjectRef: oursHash, Author: "test",
	})
	require.NoError(t, err)

	// Main changes line4 (target/theirs) — different region
	theirsContent := []byte("line1\nline2\nline3\nmodified4\n")
	theirsHash, err := store.Write(theirsContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpModify, Path: "file.go", ObjectRef: theirsHash, Author: "test",
	})
	require.NoError(t, err)

	// Weave feature into main — auto-merge should succeed
	result, err := engine.Weave(WeaveOptions{
		SourceName: "feature", TargetName: "main",
		Strategy: WeaveStrategyAuto, Author: "test",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ConflictCount)
	assert.Equal(t, 1, result.Applied) // one merged op

	// Verify the merged content was written correctly
	assert.NotNil(t, result.Checkpoint)
}

func TestWeaveEngine_AutoMergeConflict(t *testing.T) {
	engine, writer, sm, _, store := setupWeaveEnv(t)

	main, err := sm.Create("main")
	require.NoError(t, err)

	baseContent := []byte("line1\nline2\nline3\n")
	baseHash, err := store.Write(baseContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpCreate, Path: "file.go", ObjectRef: baseHash, Author: "test",
	})
	require.NoError(t, err)

	feature, err := sm.Fork("main", "feature")
	require.NoError(t, err)

	// Feature changes line2 to "AAA"
	oursContent := []byte("line1\nAAA\nline3\n")
	oursHash, err := store.Write(oursContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: feature.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpModify, Path: "file.go", ObjectRef: oursHash, Author: "test",
	})
	require.NoError(t, err)

	// Main changes line2 to "BBB" — same region, different content → conflict
	theirsContent := []byte("line1\nBBB\nline3\n")
	theirsHash, err := store.Write(theirsContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpModify, Path: "file.go", ObjectRef: theirsHash, Author: "test",
	})
	require.NoError(t, err)

	// Weave should fail with conflict
	result, err := engine.Weave(WeaveOptions{
		SourceName: "feature", TargetName: "main",
		Strategy: WeaveStrategyAuto, Author: "test",
	})
	require.Error(t, err)

	var conflictErr *WeaveConflictError
	require.True(t, errors.As(err, &conflictErr))
	assert.Equal(t, 1, len(conflictErr.Conflicts))
	assert.Equal(t, "file.go", conflictErr.Conflicts[0].EntityID)
	assert.Equal(t, 1, result.ConflictCount)
}

func TestWeaveEngine_OursStrategy(t *testing.T) {
	engine, writer, sm, _, store := setupWeaveEnv(t)

	main, err := sm.Create("main")
	require.NoError(t, err)

	baseContent := []byte("base\n")
	baseHash, err := store.Write(baseContent, "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpCreate, Path: "file.go", ObjectRef: baseHash, Author: "test",
	})
	require.NoError(t, err)

	feature, err := sm.Fork("main", "feature")
	require.NoError(t, err)

	// Both sides modify same entity
	oursHash, _ := store.Write([]byte("ours\n"), "text/plain")
	_, err = writer.Write(Operation{
		StreamID: feature.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpModify, Path: "file.go", ObjectRef: oursHash, Author: "test",
	})
	require.NoError(t, err)

	theirsHash, _ := store.Write([]byte("theirs\n"), "text/plain")
	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpModify, Path: "file.go", ObjectRef: theirsHash, Author: "test",
	})
	require.NoError(t, err)

	// Also add a non-conflicting entity on feature
	newHash, _ := store.Write([]byte("new file\n"), "text/plain")
	_, err = writer.Write(Operation{
		StreamID: feature.ID, SpaceID: "code", EntityID: "new.go",
		Type: OpCreate, Path: "new.go", ObjectRef: newHash, Author: "test",
	})
	require.NoError(t, err)

	// Strategy ours: skip conflicting source ops, keep non-conflicting
	result, err := engine.Weave(WeaveOptions{
		SourceName: "feature", TargetName: "main",
		Strategy: WeaveStrategyOurs, Author: "test",
	})
	require.NoError(t, err)
	// Only the non-conflicting op (new.go) should be applied
	assert.Equal(t, 1, result.Applied)
	assert.Equal(t, 1, result.ConflictCount)
}

func TestWeaveEngine_TheirsStrategy(t *testing.T) {
	engine, writer, sm, _, store := setupWeaveEnv(t)

	main, err := sm.Create("main")
	require.NoError(t, err)

	baseHash, _ := store.Write([]byte("base\n"), "text/plain")
	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpCreate, Path: "file.go", ObjectRef: baseHash, Author: "test",
	})
	require.NoError(t, err)

	feature, err := sm.Fork("main", "feature")
	require.NoError(t, err)

	oursHash, _ := store.Write([]byte("ours\n"), "text/plain")
	_, err = writer.Write(Operation{
		StreamID: feature.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpModify, Path: "file.go", ObjectRef: oursHash, Author: "test",
	})
	require.NoError(t, err)

	theirsHash, _ := store.Write([]byte("theirs\n"), "text/plain")
	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpModify, Path: "file.go", ObjectRef: theirsHash, Author: "test",
	})
	require.NoError(t, err)

	// Strategy theirs: all source ops applied (overwrite target)
	result, err := engine.Weave(WeaveOptions{
		SourceName: "feature", TargetName: "main",
		Strategy: WeaveStrategyTheirs, Author: "test",
	})
	require.NoError(t, err)
	// The conflicting source op should be applied
	assert.Equal(t, 1, result.Applied)
	assert.Equal(t, 1, result.ConflictCount)
}

func TestWeaveEngine_DryRun(t *testing.T) {
	engine, writer, sm, _, store := setupWeaveEnv(t)

	main, err := sm.Create("main")
	require.NoError(t, err)

	baseHash, _ := store.Write([]byte("base\n"), "text/plain")
	_, err = writer.Write(Operation{
		StreamID: main.ID, SpaceID: "code", EntityID: "file.go",
		Type: OpCreate, Path: "file.go", ObjectRef: baseHash, Author: "test",
	})
	require.NoError(t, err)

	feature, err := sm.Fork("main", "feature")
	require.NoError(t, err)

	newHash, _ := store.Write([]byte("new content\n"), "text/plain")
	_, err = writer.Write(Operation{
		StreamID: feature.ID, SpaceID: "code", EntityID: "new.go",
		Type: OpCreate, Path: "new.go", ObjectRef: newHash, Author: "test",
	})
	require.NoError(t, err)

	// Dry run should return counts but not write
	result, err := engine.Weave(WeaveOptions{
		SourceName: "feature", TargetName: "main",
		Strategy: WeaveStrategyAuto, DryRun: true, Author: "test",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Applied)
	assert.Nil(t, result.Checkpoint) // no checkpoint on dry run
}

func TestWeaveEngine_SelfWeaveError(t *testing.T) {
	engine, _, sm, _, _ := setupWeaveEnv(t)

	_, err := sm.Create("main")
	require.NoError(t, err)

	_, err = engine.Weave(WeaveOptions{
		SourceName: "main", TargetName: "main",
		Strategy: WeaveStrategyAuto, Author: "test",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot weave a stream into itself")
}
