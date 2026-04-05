package diff

import (
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ParseRef tests ---

func TestParseRef_Head(t *testing.T) {
	for _, s := range []string{"HEAD", "head"} {
		ref := ParseRef(s)
		assert.Equal(t, "head", ref.Type, "input: %s", s)
		assert.Equal(t, "", ref.Value, "input: %s", s)
	}
}

func TestParseRef_Relative(t *testing.T) {
	ref := ParseRef("HEAD~3")
	assert.Equal(t, "relative", ref.Type)
	assert.Equal(t, "HEAD~3", ref.Value)
}

func TestParseRef_Seq(t *testing.T) {
	ref := ParseRef("42")
	assert.Equal(t, "seq", ref.Type)
	assert.Equal(t, "42", ref.Value)
}

func TestParseRef_CheckpointID(t *testing.T) {
	ref := ParseRef("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	assert.Equal(t, "checkpoint", ref.Type)
	assert.Equal(t, "01ARZ3NDEKTSV4RRFFQ69G5FAV", ref.Value)
}

// --- DiffRef.String tests ---

func TestDiffRef_String(t *testing.T) {
	tests := []struct {
		ref  DiffRef
		want string
	}{
		{DiffRef{Type: "head", Value: ""}, "HEAD"},
		{DiffRef{Type: "relative", Value: "HEAD~3"}, "HEAD~3"},
		{DiffRef{Type: "seq", Value: "42"}, "42"},
		{DiffRef{Type: "checkpoint", Value: "01ARZ3NDEKTSV4RRFFQ69G5FAV"}, "01ARZ3NDEKTSV4RRFFQ69G5FAV"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, tc.ref.String())
	}
}

// --- ResolveRef tests ---

func TestResolveRef_Head(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	sm := core.NewStreamManager(db)
	stream, err := sm.Create("main")
	require.NoError(t, err)

	writer := core.NewOpWriter(db, store)
	reader := core.NewOpReader(db)

	op, err := writer.Write(core.Operation{
		StreamID: stream.ID,
		SpaceID:  "space1",
		EntityID: "entity1",
		Type:     core.OpCreate,
		Path:     "file.txt",
		Author:   "test",
	})
	require.NoError(t, err)

	seq, err := ResolveRef(ParseRef("HEAD"), db, reader)
	require.NoError(t, err)
	assert.Equal(t, op.Seq, seq)
}

func TestResolveRef_Seq(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	reader := core.NewOpReader(db)

	seq, err := ResolveRef(ParseRef("42"), db, reader)
	require.NoError(t, err)
	assert.Equal(t, int64(42), seq)
}

func TestResolveRef_Checkpoint(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	sm := core.NewStreamManager(db)
	stream, err := sm.Create("main")
	require.NoError(t, err)

	writer := core.NewOpWriter(db, store)
	reader := core.NewOpReader(db)
	engine := core.NewCheckpointEngine(db, reader)

	_, err = writer.Write(core.Operation{
		StreamID: stream.ID,
		SpaceID:  "space1",
		EntityID: "entity1",
		Type:     core.OpCreate,
		Path:     "file.txt",
		Author:   "test",
	})
	require.NoError(t, err)

	cp, err := engine.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "cp1",
		Author:   "test",
		Source:   core.SourceManual,
	})
	require.NoError(t, err)

	seq, err := ResolveRef(ParseRef(cp.ID), db, reader)
	require.NoError(t, err)
	assert.Equal(t, cp.Seq, seq)
}

func TestResolveRef_Relative(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	sm := core.NewStreamManager(db)
	stream, err := sm.Create("main")
	require.NoError(t, err)

	writer := core.NewOpWriter(db, store)
	reader := core.NewOpReader(db)
	engine := core.NewCheckpointEngine(db, reader)

	var cps []*core.Checkpoint
	for i := 0; i < 3; i++ {
		_, err := writer.Write(core.Operation{
			StreamID: stream.ID,
			SpaceID:  "space1",
			EntityID: core.NewID(),
			Type:     core.OpCreate,
			Path:     "file.txt",
			Author:   "test",
		})
		require.NoError(t, err)

		cp, err := engine.Create(core.CheckpointInput{
			StreamID: stream.ID,
			Title:    "cp",
			Author:   "test",
			Source:   core.SourceManual,
		})
		require.NoError(t, err)
		cps = append(cps, cp)
	}

	// HEAD~1: OFFSET 1 → 2nd-most-recent = cps[1]
	seq, err := ResolveRef(ParseRef("HEAD~1"), db, reader)
	require.NoError(t, err)
	assert.Equal(t, cps[1].Seq, seq)
}

func TestResolveRef_RelativeZero(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	sm := core.NewStreamManager(db)
	stream, err := sm.Create("main")
	require.NoError(t, err)

	writer := core.NewOpWriter(db, store)
	reader := core.NewOpReader(db)
	engine := core.NewCheckpointEngine(db, reader)

	var latestCP *core.Checkpoint
	for i := 0; i < 3; i++ {
		_, err := writer.Write(core.Operation{
			StreamID: stream.ID,
			SpaceID:  "space1",
			EntityID: core.NewID(),
			Type:     core.OpCreate,
			Path:     "file.txt",
			Author:   "test",
		})
		require.NoError(t, err)

		cp, err := engine.Create(core.CheckpointInput{
			StreamID: stream.ID,
			Title:    "cp",
			Author:   "test",
			Source:   core.SourceManual,
		})
		require.NoError(t, err)
		latestCP = cp
	}

	// HEAD~0: OFFSET 0 → latest checkpoint
	seq, err := ResolveRef(ParseRef("HEAD~0"), db, reader)
	require.NoError(t, err)
	assert.Equal(t, latestCP.Seq, seq)
}
