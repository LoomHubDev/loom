package watch

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAutoCheckpointEnv(t *testing.T) (*core.CheckpointEngine, *core.OpWriter, *core.OpReader, *core.Stream) {
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
	sm.SetActive("main")
	writer := core.NewOpWriter(db, store)
	reader := core.NewOpReader(db)
	engine := core.NewCheckpointEngine(db, reader)
	return engine, writer, reader, stream
}

// writeOp is a helper to write a single modify op to the stream.
func writeOp(t *testing.T, writer *core.OpWriter, streamID, entity string) {
	t.Helper()
	_, err := writer.Write(core.Operation{
		StreamID: streamID,
		SpaceID:  "code",
		EntityID: entity,
		Type:     core.OpModify,
		Path:     entity,
		Author:   "test",
	})
	require.NoError(t, err)
}

func TestAutoCheckpointer_TriggersOnOpCount(t *testing.T) {
	engine, writer, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     3,
		IntervalSeconds: -1, // disable time threshold
	})

	writeOp(t, writer, stream.ID, "a.go")
	writeOp(t, writer, stream.ID, "b.go")
	writeOp(t, writer, stream.ID, "c.go")

	cp := ac.MaybeCheckpoint(stream.ID, "tester")
	require.NotNil(t, cp, "expected checkpoint to be created at op threshold")
	assert.Equal(t, core.SourceAuto, cp.Source)
}

func TestAutoCheckpointer_DoesNotTriggerBelowThreshold(t *testing.T) {
	engine, writer, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     5,
		IntervalSeconds: -1, // disable time threshold
	})

	writeOp(t, writer, stream.ID, "a.go")
	writeOp(t, writer, stream.ID, "b.go")

	cp := ac.MaybeCheckpoint(stream.ID, "tester")
	assert.Nil(t, cp, "expected no checkpoint below op threshold")
}

func TestAutoCheckpointer_TriggersOnTimeInterval(t *testing.T) {
	engine, writer, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     0,              // disable op threshold
		IntervalSeconds: 0,              // trigger immediately when there are ops
	})
	// Simulate that last checkpoint was 1 hour ago.
	ac.LastCheckpoint = time.Now().Add(-1 * time.Hour)

	writeOp(t, writer, stream.ID, "a.go")

	cp := ac.MaybeCheckpoint(stream.ID, "tester")
	require.NotNil(t, cp, "expected checkpoint to be created after time threshold")
	assert.Equal(t, core.SourceAuto, cp.Source)
}

func TestAutoCheckpointer_NoOpsNoCheckpoint(t *testing.T) {
	engine, _, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     1,
		IntervalSeconds: 0,
	})
	// Set last checkpoint far in the past so time threshold would fire if there were ops.
	ac.LastCheckpoint = time.Now().Add(-1 * time.Hour)

	cp := ac.MaybeCheckpoint(stream.ID, "tester")
	assert.Nil(t, cp, "expected no checkpoint when there are no pending ops")
}

func TestAutoCheckpointer_ResetsAfterCheckpoint(t *testing.T) {
	engine, writer, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     2,
		IntervalSeconds: -1, // disable time threshold
	})

	// Write 2 ops — should trigger.
	writeOp(t, writer, stream.ID, "a.go")
	writeOp(t, writer, stream.ID, "b.go")

	cp := ac.MaybeCheckpoint(stream.ID, "tester")
	require.NotNil(t, cp, "expected first checkpoint at threshold 2")

	// Write 1 more op — should NOT trigger (only 1 op since last checkpoint).
	writeOp(t, writer, stream.ID, "c.go")

	cp2 := ac.MaybeCheckpoint(stream.ID, "tester")
	assert.Nil(t, cp2, "expected no second checkpoint with only 1 pending op after reset")
}
