package diff

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDiffEnv(t *testing.T) (*DiffEngine, *core.OpWriter, *core.CheckpointEngine, *core.Stream) {
	t.Helper()
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
	engine := NewDiffEngine(db, reader, store)
	cpEngine := core.NewCheckpointEngine(db, reader)

	return engine, writer, cpEngine, stream
}

func TestDiffEngine_EmptyRange(t *testing.T) {
	engine, _, _, _ := setupDiffEnv(t)

	result, err := engine.Diff(ParseRef("0"), ParseRef("0"), DiffOptions{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.FromSeq)
	assert.Equal(t, int64(0), result.ToSeq)
	assert.Empty(t, result.Spaces)
	assert.Equal(t, 0, result.Summary.SpacesChanged)
	assert.Equal(t, 0, result.Summary.TotalOperations)
}

func TestDiffEngine_CreateOps(t *testing.T) {
	engine, writer, _, stream := setupDiffEnv(t)

	// Write content to object store and get hash.
	hash, err := engine.store.Write([]byte("hello world\n"), "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "file1",
		Type:      core.OpCreate,
		Path:      "src/main.go",
		ObjectRef: hash,
		Author:    "test",
	})
	require.NoError(t, err)

	result, err := engine.Diff(ParseRef("0"), ParseRef("HEAD"), DiffOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Summary.SpacesChanged)
	assert.Equal(t, 1, result.Summary.EntitiesCreated)
	assert.Equal(t, 1, result.Summary.TotalOperations)

	require.Len(t, result.Spaces, 1)
	assert.Equal(t, "code", result.Spaces[0].SpaceID)
	require.Len(t, result.Spaces[0].Entities, 1)

	entity := result.Spaces[0].Entities[0]
	assert.Equal(t, "file1", entity.EntityID)
	assert.Equal(t, "src/main.go", entity.Path)
	assert.Equal(t, core.ChangeCreated, entity.Change)
	// Created entity should have hunks showing additions.
	assert.NotEmpty(t, entity.Hunks)
}

func TestDiffEngine_ModifyOps(t *testing.T) {
	engine, writer, _, stream := setupDiffEnv(t)

	// Create entity first.
	hash1, err := engine.store.Write([]byte("line 1\nline 2\n"), "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "file1",
		Type:      core.OpCreate,
		Path:      "src/main.go",
		ObjectRef: hash1,
		Author:    "test",
	})
	require.NoError(t, err)

	// Get seq after create (this is our "from" point).
	fromSeq, err := engine.reader.Head()
	require.NoError(t, err)

	// Modify entity.
	hash2, err := engine.store.Write([]byte("line 1\nline 2 modified\nline 3 added\n"), "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "file1",
		Type:      core.OpModify,
		Path:      "src/main.go",
		ObjectRef: hash2,
		Author:    "test",
	})
	require.NoError(t, err)

	result, err := engine.Diff(
		ParseRef(fmt.Sprintf("%d", fromSeq)),
		ParseRef("HEAD"),
		DiffOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Summary.SpacesChanged)
	assert.Equal(t, 1, result.Summary.EntitiesModified)

	require.Len(t, result.Spaces, 1)
	require.Len(t, result.Spaces[0].Entities, 1)

	entity := result.Spaces[0].Entities[0]
	assert.Equal(t, core.ChangeModified, entity.Change)
	assert.NotEmpty(t, entity.Hunks, "modified entity should have hunks")

	// Verify hunks contain both additions and deletions.
	var hasAdd, hasDelete bool
	for _, hunk := range entity.Hunks {
		for _, line := range hunk.Lines {
			if line.Type == "add" {
				hasAdd = true
			}
			if line.Type == "delete" {
				hasDelete = true
			}
		}
	}
	assert.True(t, hasAdd, "should have added lines")
	assert.True(t, hasDelete, "should have deleted lines")
}

func TestDiffEngine_DeleteOps(t *testing.T) {
	engine, writer, _, stream := setupDiffEnv(t)

	// Create entity first.
	hash1, err := engine.store.Write([]byte("content to delete\n"), "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "file1",
		Type:      core.OpCreate,
		Path:      "src/main.go",
		ObjectRef: hash1,
		Author:    "test",
	})
	require.NoError(t, err)

	fromSeq, err := engine.reader.Head()
	require.NoError(t, err)

	// Delete entity.
	_, err = writer.Write(core.Operation{
		StreamID: stream.ID,
		SpaceID:  "code",
		EntityID: "file1",
		Type:     core.OpDelete,
		Path:     "src/main.go",
		Author:   "test",
	})
	require.NoError(t, err)

	result, err := engine.Diff(
		ParseRef(fmt.Sprintf("%d", fromSeq)),
		ParseRef("HEAD"),
		DiffOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Summary.EntitiesDeleted)
	require.Len(t, result.Spaces, 1)
	require.Len(t, result.Spaces[0].Entities, 1)

	entity := result.Spaces[0].Entities[0]
	assert.Equal(t, core.ChangeDeleted, entity.Change)
	// Deleted entity should have hunks showing deletions.
	assert.NotEmpty(t, entity.Hunks)
}

func TestDiffEngine_MultipleSpaces(t *testing.T) {
	engine, writer, _, stream := setupDiffEnv(t)

	hash1, err := engine.store.Write([]byte("code content\n"), "text/plain")
	require.NoError(t, err)
	hash2, err := engine.store.Write([]byte("doc content\n"), "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "file1",
		Type:      core.OpCreate,
		Path:      "src/main.go",
		ObjectRef: hash1,
		Author:    "test",
	})
	require.NoError(t, err)

	_, err = writer.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "docs",
		EntityID:  "doc1",
		Type:      core.OpCreate,
		Path:      "docs/readme.md",
		ObjectRef: hash2,
		Author:    "test",
	})
	require.NoError(t, err)

	result, err := engine.Diff(ParseRef("0"), ParseRef("HEAD"), DiffOptions{})
	require.NoError(t, err)

	assert.Equal(t, 2, result.Summary.SpacesChanged)
	assert.Equal(t, 2, result.Summary.EntitiesCreated)
	assert.Len(t, result.Spaces, 2)

	// Spaces should be sorted alphabetically.
	assert.Equal(t, "code", result.Spaces[0].SpaceID)
	assert.Equal(t, "docs", result.Spaces[1].SpaceID)
}

func TestDiffEngine_FilterBySpace(t *testing.T) {
	engine, writer, _, stream := setupDiffEnv(t)

	hash1, err := engine.store.Write([]byte("code content\n"), "text/plain")
	require.NoError(t, err)
	hash2, err := engine.store.Write([]byte("doc content\n"), "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "file1",
		Type:      core.OpCreate,
		Path:      "src/main.go",
		ObjectRef: hash1,
		Author:    "test",
	})
	require.NoError(t, err)

	_, err = writer.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "docs",
		EntityID:  "doc1",
		Type:      core.OpCreate,
		Path:      "docs/readme.md",
		ObjectRef: hash2,
		Author:    "test",
	})
	require.NoError(t, err)

	result, err := engine.Diff(ParseRef("0"), ParseRef("HEAD"), DiffOptions{SpaceID: "code"})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Summary.SpacesChanged)
	require.Len(t, result.Spaces, 1)
	assert.Equal(t, "code", result.Spaces[0].SpaceID)
}

func TestDiffEngine_SummaryOnly(t *testing.T) {
	engine, writer, _, stream := setupDiffEnv(t)

	hash, err := engine.store.Write([]byte("content\n"), "text/plain")
	require.NoError(t, err)

	_, err = writer.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "file1",
		Type:      core.OpCreate,
		Path:      "src/main.go",
		ObjectRef: hash,
		Author:    "test",
	})
	require.NoError(t, err)

	result, err := engine.Diff(ParseRef("0"), ParseRef("HEAD"), DiffOptions{Summary: true})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Summary.EntitiesCreated)
	require.Len(t, result.Spaces, 1)
	require.Len(t, result.Spaces[0].Entities, 1)

	// Summary mode should not produce hunks.
	assert.Empty(t, result.Spaces[0].Entities[0].Hunks)
}

func TestFormatTerminal_BasicOutput(t *testing.T) {
	result := &DiffResult{
		FromSeq: 0,
		ToSeq:   2,
		FromRef: "0",
		ToRef:   "HEAD",
		Spaces: []SpaceDiff{
			{
				SpaceID: "code",
				Entities: []EntityDiff{
					{
						EntityID: "file1",
						Path:     "src/main.go",
						Change:   core.ChangeCreated,
						Summary:  "created src/main.go",
					},
					{
						EntityID: "file2",
						Path:     "src/util.go",
						Change:   core.ChangeModified,
						Hunks: []DiffHunk{
							{
								OldStart: 1, OldLines: 2,
								NewStart: 1, NewLines: 3,
								Lines: []DiffLine{
									{Type: "context", Content: "line 1\n"},
									{Type: "delete", Content: "line 2\n"},
									{Type: "add", Content: "line 2 modified\n"},
									{Type: "add", Content: "line 3 added\n"},
								},
							},
						},
					},
				},
				Summary: core.SpaceSummary{EntitiesCreated: 1, EntitiesModified: 1},
			},
		},
		Summary: DiffSummary{
			SpacesChanged:    1,
			EntitiesCreated:  1,
			EntitiesModified: 1,
			TotalOperations:  2,
		},
	}

	output := FormatTerminal(result, false)

	assert.Contains(t, output, "diff 0..HEAD")
	assert.Contains(t, output, "1 space(s) changed")
	assert.Contains(t, output, "code")
	assert.Contains(t, output, "+ src/main.go")
	assert.Contains(t, output, "M src/util.go")
	assert.Contains(t, output, "@@ -1,2 +1,3 @@")
	assert.Contains(t, output, "+line 2 modified")
	assert.Contains(t, output, "-line 2")

	// Test with color.
	colorOutput := FormatTerminal(result, true)
	assert.Contains(t, colorOutput, "\033[32m")
	assert.Contains(t, colorOutput, "\033[31m")
}

func TestFormatJSON_ValidJSON(t *testing.T) {
	result := &DiffResult{
		FromSeq: 0,
		ToSeq:   1,
		FromRef: "0",
		ToRef:   "HEAD",
		Spaces: []SpaceDiff{
			{
				SpaceID: "code",
				Entities: []EntityDiff{
					{
						EntityID: "file1",
						Path:     "src/main.go",
						Change:   core.ChangeCreated,
						Summary:  "created src/main.go",
					},
				},
				Summary: core.SpaceSummary{EntitiesCreated: 1},
			},
		},
		Summary: DiffSummary{
			SpacesChanged:   1,
			EntitiesCreated: 1,
			TotalOperations: 1,
		},
	}

	output := FormatJSON(result)

	// Verify it's valid JSON.
	var parsed map[string]any
	err := json.Unmarshal([]byte(output), &parsed)
	require.NoError(t, err)

	// Verify key fields.
	assert.Equal(t, float64(0), parsed["from_seq"])
	assert.Equal(t, float64(1), parsed["to_seq"])
	assert.Equal(t, "0", parsed["from_ref"])
	assert.Equal(t, "HEAD", parsed["to_ref"])

	spaces, ok := parsed["spaces"].([]any)
	require.True(t, ok)
	assert.Len(t, spaces, 1)

	// Verify nil result returns empty object.
	assert.Equal(t, "{}", FormatJSON(nil))
}

