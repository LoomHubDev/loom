package core

import (
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCheckpointTestify(t *testing.T) (*CheckpointEngine, *OpWriter, *OpReader, *StreamManager, *Stream) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	sm := NewStreamManager(db)
	stream, err := sm.Create("main")
	require.NoError(t, err)

	writer := NewOpWriter(db, store)
	reader := NewOpReader(db)
	engine := NewCheckpointEngine(db, reader)

	return engine, writer, reader, sm, stream
}

func TestCheckpointEngine_CreateWithSummaryAndTags(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	tags := map[string]string{"release": "v1.0", "env": "staging"}
	cp, err := engine.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "release checkpoint",
		Summary:  "All features merged and tested",
		Author:   "deployer",
		Source:   SourceWorkflow,
		Tags:     tags,
	})
	require.NoError(t, err)
	assert.Equal(t, "release checkpoint", cp.Title)
	assert.Equal(t, "All features merged and tested", cp.Summary)
	assert.Equal(t, "deployer", cp.Author)
	assert.Equal(t, SourceWorkflow, cp.Source)
	assert.Equal(t, "v1.0", cp.Tags["release"])
	assert.Equal(t, "staging", cp.Tags["env"])
}

func TestCheckpointEngine_CreateInvalidStream(t *testing.T) {
	engine, _, _, _, _ := setupCheckpointTestify(t)

	_, err := engine.Create(CheckpointInput{
		StreamID: "nonexistent-stream-id",
		Title:    "should fail",
		Author:   "test",
		Source:   SourceManual,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get stream head")
}

func TestCheckpointEngine_GetNonExistent(t *testing.T) {
	engine, _, _, _, _ := setupCheckpointTestify(t)

	_, err := engine.Get("nonexistent-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get checkpoint")
}

func TestCheckpointEngine_ListEmpty(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	cps, err := engine.List(stream.ID, 10)
	require.NoError(t, err)
	assert.Empty(t, cps)
}

func TestCheckpointEngine_ListWithLimit(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	for i := 0; i < 5; i++ {
		_, err := engine.Create(CheckpointInput{
			StreamID: stream.ID,
			Title:    "cp",
			Author:   "test",
			Source:   SourceManual,
		})
		require.NoError(t, err)
	}

	cps, err := engine.List(stream.ID, 3)
	require.NoError(t, err)
	assert.Len(t, cps, 3)
}

func TestCheckpointEngine_ListAll(t *testing.T) {
	engine, _, _, sm, stream1 := setupCheckpointTestify(t)

	stream2, err := sm.Create("dev")
	require.NoError(t, err)

	engine.Create(CheckpointInput{StreamID: stream1.ID, Title: "main-cp", Author: "test", Source: SourceManual})
	engine.Create(CheckpointInput{StreamID: stream2.ID, Title: "dev-cp", Author: "test", Source: SourceManual})

	all, err := engine.ListAll(10)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestCheckpointEngine_ListAllWithLimit(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	for i := 0; i < 5; i++ {
		engine.Create(CheckpointInput{StreamID: stream.ID, Title: "cp", Author: "test", Source: SourceManual})
	}

	all, err := engine.ListAll(2)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestCheckpointEngine_ListAllEmpty(t *testing.T) {
	engine, _, _, _, _ := setupCheckpointTestify(t)

	all, err := engine.ListAll(10)
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestCheckpointEngine_SearchNoResults(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	engine.Create(CheckpointInput{StreamID: stream.ID, Title: "auth system", Author: "test", Source: SourceManual})

	results, err := engine.Search("database")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestCheckpointEngine_SearchBySummary(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	engine.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "refactor",
		Summary:  "Improved authentication flow with OAuth",
		Author:   "test",
		Source:   SourceManual,
	})
	engine.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "bugfix",
		Summary:  "Fixed database connection pooling",
		Author:   "test",
		Source:   SourceManual,
	})

	results, err := engine.Search("OAuth")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "refactor", results[0].Title)
}

func TestCheckpointEngine_SearchWithLimit(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	for i := 0; i < 4; i++ {
		_, err := engine.Create(CheckpointInput{
			StreamID: stream.ID,
			Title:    "auth",
			Summary:  "searchable",
			Author:   "test",
			Source:   SourceManual,
		})
		require.NoError(t, err)
	}

	results, err := engine.Search("auth", 2)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestCheckpointEngine_LatestSeq(t *testing.T) {
	engine, writer, _, _, stream := setupCheckpointTestify(t)

	// No checkpoints yet
	seq := engine.LatestSeq(stream.ID)
	assert.Equal(t, int64(0), seq)

	// Write ops and checkpoint
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "a.go", Type: OpCreate, Path: "a.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "b.go", Type: OpCreate, Path: "b.go", Author: "test"})

	engine.Create(CheckpointInput{StreamID: stream.ID, Title: "cp1", Author: "test", Source: SourceManual})
	seq = engine.LatestSeq(stream.ID)
	assert.Equal(t, int64(2), seq)

	// More ops and another checkpoint
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "c.go", Type: OpCreate, Path: "c.go", Author: "test"})
	engine.Create(CheckpointInput{StreamID: stream.ID, Title: "cp2", Author: "test", Source: SourceManual})
	seq = engine.LatestSeq(stream.ID)
	assert.Equal(t, int64(3), seq)
}

func TestCheckpointEngine_LatestSeq_WrongStream(t *testing.T) {
	engine, _, _, _, _ := setupCheckpointTestify(t)

	seq := engine.LatestSeq("nonexistent-stream")
	assert.Equal(t, int64(0), seq)
}

func TestCheckpointEngine_MultipleSpacesInBreakdown(t *testing.T) {
	engine, writer, _, _, stream := setupCheckpointTestify(t)

	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "main.go", Type: OpCreate, Path: "main.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "docs", EntityID: "readme.md", Type: OpCreate, Path: "readme.md", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "design", EntityID: "mock.fig", Type: OpCreate, Path: "mock.fig", Author: "test"})

	cp, err := engine.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "multi-space",
		Author:   "test",
		Source:   SourceManual,
	})
	require.NoError(t, err)
	assert.Len(t, cp.Spaces, 3)

	spaceIDs := make(map[string]bool)
	for _, s := range cp.Spaces {
		spaceIDs[s.SpaceID] = true
		assert.Equal(t, SpaceChanged, s.Status)
	}
	assert.True(t, spaceIDs["code"])
	assert.True(t, spaceIDs["docs"])
	assert.True(t, spaceIDs["design"])
}

func TestCheckpointEngine_ParentChain_ThreeDeep(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	cp1, err := engine.Create(CheckpointInput{StreamID: stream.ID, Title: "first", Author: "test", Source: SourceManual})
	require.NoError(t, err)
	assert.Empty(t, cp1.ParentID, "first checkpoint should have no parent")

	cp2, err := engine.Create(CheckpointInput{StreamID: stream.ID, Title: "second", Author: "test", Source: SourceManual})
	require.NoError(t, err)
	assert.Equal(t, cp1.ID, cp2.ParentID)

	cp3, err := engine.Create(CheckpointInput{StreamID: stream.ID, Title: "third", Author: "test", Source: SourceManual})
	require.NoError(t, err)
	assert.Equal(t, cp2.ID, cp3.ParentID)
}

func TestCheckpointEngine_GetPreservesAllFields(t *testing.T) {
	engine, writer, _, _, stream := setupCheckpointTestify(t)

	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "a.go", Type: OpCreate, Path: "a.go", Author: "test"})

	created, err := engine.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    "full checkpoint",
		Summary:  "detailed summary",
		Author:   "alice",
		Source:   SourceAgent,
		Tags:     map[string]string{"ci": "true"},
	})
	require.NoError(t, err)

	got, err := engine.Get(created.ID)
	require.NoError(t, err)

	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, created.StreamID, got.StreamID)
	assert.Equal(t, created.Seq, got.Seq)
	assert.Equal(t, "full checkpoint", got.Title)
	assert.Equal(t, "detailed summary", got.Summary)
	assert.Equal(t, "alice", got.Author)
	assert.Equal(t, SourceAgent, got.Source)
	assert.Equal(t, "true", got.Tags["ci"])
	assert.NotEmpty(t, got.Timestamp)
}

func TestCheckpointEngine_AllSources(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	sources := []CheckpointSource{SourceManual, SourceAuto, SourceAgent, SourceWorkflow, SourceGuard, SourceRestore}
	for _, src := range sources {
		cp, err := engine.Create(CheckpointInput{
			StreamID: stream.ID,
			Title:    "cp-" + string(src),
			Author:   "test",
			Source:   src,
		})
		require.NoError(t, err, "source %s", src)
		assert.Equal(t, src, cp.Source)
	}

	cps, err := engine.List(stream.ID, 100)
	require.NoError(t, err)
	assert.Len(t, cps, len(sources))
}

func TestCheckpointEngine_NoOpsBetweenCheckpoints(t *testing.T) {
	engine, _, _, _, stream := setupCheckpointTestify(t)

	// Two checkpoints with no ops between them
	cp1, err := engine.Create(CheckpointInput{StreamID: stream.ID, Title: "empty1", Author: "test", Source: SourceManual})
	require.NoError(t, err)

	cp2, err := engine.Create(CheckpointInput{StreamID: stream.ID, Title: "empty2", Author: "test", Source: SourceManual})
	require.NoError(t, err)

	assert.Equal(t, cp1.Seq, cp2.Seq, "seq should be same when no ops between")
	assert.Empty(t, cp2.Spaces, "no space changes expected")
}

func TestCheckpointEngine_ListOrderDescBySeq(t *testing.T) {
	engine, writer, _, _, stream := setupCheckpointTestify(t)

	// Create ops between checkpoints to ensure different seqs
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "a.go", Type: OpCreate, Path: "a.go", Author: "test"})
	engine.Create(CheckpointInput{StreamID: stream.ID, Title: "alpha", Author: "test", Source: SourceManual})

	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "b.go", Type: OpCreate, Path: "b.go", Author: "test"})
	engine.Create(CheckpointInput{StreamID: stream.ID, Title: "beta", Author: "test", Source: SourceManual})

	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "c.go", Type: OpCreate, Path: "c.go", Author: "test"})
	engine.Create(CheckpointInput{StreamID: stream.ID, Title: "gamma", Author: "test", Source: SourceManual})

	cps, err := engine.List(stream.ID, 10)
	require.NoError(t, err)
	require.Len(t, cps, 3)

	// Should be newest first (desc by seq)
	assert.Equal(t, "gamma", cps[0].Title)
	assert.Equal(t, "beta", cps[1].Title)
	assert.Equal(t, "alpha", cps[2].Title)
	assert.True(t, cps[0].Seq > cps[1].Seq)
	assert.True(t, cps[1].Seq > cps[2].Seq)
}
