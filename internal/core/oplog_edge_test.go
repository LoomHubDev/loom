package core

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupOplogTestify(t *testing.T) (*OpWriter, *OpReader, *StreamManager, *Stream) {
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

	return NewOpWriter(db, store), NewOpReader(db), sm, stream
}

func setupPersistentOplogTestify(t *testing.T) (string, *OpWriter, *OpReader, *StreamManager, *Stream) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := storage.InitDB(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	sm := NewStreamManager(db)
	stream, err := sm.Create("main")
	require.NoError(t, err)

	return dir, NewOpWriter(db, store), NewOpReader(db), sm, stream
}

func TestOpWriter_WriteAssignsIDAndTimestamp(t *testing.T) {
	writer, _, _, stream := setupOplogTestify(t)

	op := Operation{
		StreamID: stream.ID,
		SpaceID:  "code",
		EntityID: "main.go",
		Type:     OpCreate,
		Path:     "main.go",
		Author:   "test",
	}

	result, err := writer.Write(op)
	require.NoError(t, err)
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, int64(1), result.Seq)
	assert.NotEmpty(t, result.Timestamp)
}

func TestOpWriter_WritePreservesCustomTimestamp(t *testing.T) {
	writer, _, _, stream := setupOplogTestify(t)

	op := Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      OpCreate,
		Path:      "main.go",
		Author:    "test",
		Timestamp: "2020-01-01T00:00:00Z",
	}

	result, err := writer.Write(op)
	require.NoError(t, err)
	assert.Equal(t, "2020-01-01T00:00:00Z", result.Timestamp)
}

func TestOpWriter_WriteWithDelta(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	delta := []byte(`{"op":"replace","path":"/name","value":"new"}`)
	op := Operation{
		StreamID: stream.ID,
		SpaceID:  "code",
		EntityID: "config.json",
		Type:     OpModify,
		Path:     "config.json",
		Author:   "test",
		Delta:    delta,
	}

	_, err := writer.Write(op)
	require.NoError(t, err)

	ops, err := reader.ReadByEntity("config.json")
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, delta, ops[0].Delta)
}

func TestOpWriter_WriteWithObjectRef(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	op := Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      OpCreate,
		Path:      "main.go",
		Author:    "test",
		ObjectRef: "sha256abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234",
	}

	_, err := writer.Write(op)
	require.NoError(t, err)

	ops, err := reader.ReadByEntity("main.go")
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, "sha256abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234", ops[0].ObjectRef)
}

func TestOpWriter_WriteWithMeta(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	op := Operation{
		StreamID: stream.ID,
		SpaceID:  "code",
		EntityID: "main.go",
		Type:     OpCreate,
		Path:     "main.go",
		Author:   "test",
		Meta: OpMeta{
			Size:        2048,
			ContentType: "text/x-go",
			Source:      "editor",
			AgentID:     "agent-001",
			Labels:      map[string]string{"reviewed": "true"},
		},
	}

	_, err := writer.Write(op)
	require.NoError(t, err)

	ops, err := reader.ReadByEntity("main.go")
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, int64(2048), ops[0].Meta.Size)
	assert.Equal(t, "text/x-go", ops[0].Meta.ContentType)
	assert.Equal(t, "editor", ops[0].Meta.Source)
	assert.Equal(t, "agent-001", ops[0].Meta.AgentID)
}

func TestOpWriter_WriteDeleteUpdatesEntityStatus(t *testing.T) {
	writer, _, _, stream := setupOplogTestify(t)

	// Create then delete
	writer.Write(Operation{
		StreamID: stream.ID, SpaceID: "code", EntityID: "temp.go",
		Type: OpCreate, Path: "temp.go", Author: "test",
	})
	_, err := writer.Write(Operation{
		StreamID: stream.ID, SpaceID: "code", EntityID: "temp.go",
		Type: OpDelete, Path: "temp.go", Author: "test",
	})
	require.NoError(t, err)

	var status string
	err = writer.db.QueryRow("SELECT status FROM entities WHERE id = 'temp.go' AND space_id = 'code'").Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "deleted", status)
}

func TestOpWriter_WriteMoveType(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	op := Operation{
		StreamID: stream.ID,
		SpaceID:  "code",
		EntityID: "old.go",
		Type:     OpMove,
		Path:     "new/old.go",
		Author:   "test",
		Meta:     OpMeta{OldPath: "old.go"},
	}

	_, err := writer.Write(op)
	require.NoError(t, err)

	ops, err := reader.ReadByEntity("old.go")
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, OpMove, ops[0].Type)
	assert.Equal(t, "old.go", ops[0].Meta.OldPath)
}

func TestOpWriter_WriteRenameType(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	_, err := writer.Write(Operation{
		StreamID: stream.ID,
		SpaceID:  "code",
		EntityID: "handler.go",
		Type:     OpRename,
		Path:     "handler_v2.go",
		Author:   "test",
		Meta:     OpMeta{OldPath: "handler.go"},
	})
	require.NoError(t, err)

	ops, err := reader.ReadByEntity("handler.go")
	require.NoError(t, err)
	assert.Len(t, ops, 1)
	assert.Equal(t, OpRename, ops[0].Type)
}

func TestOpWriter_WriteUpdatesStreamHead(t *testing.T) {
	writer, _, sm, stream := setupOplogTestify(t)

	for i := 0; i < 3; i++ {
		writer.Write(Operation{
			StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
			Type: OpModify, Path: "f.go", Author: "test",
		})
	}

	updated, err := sm.GetByID(stream.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), updated.HeadSeq)
}

func TestOpWriter_ConcurrentWrites(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	var wg sync.WaitGroup
	count := 20

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			writer.Write(Operation{
				StreamID: stream.ID,
				SpaceID:  "code",
				EntityID: "concurrent.go",
				Type:     OpModify,
				Path:     "concurrent.go",
				Author:   "test",
			})
		}(i)
	}
	wg.Wait()

	head, err := reader.Head()
	require.NoError(t, err)
	assert.Equal(t, int64(count), head)
}

func TestOpWriter_WriteRejectsUnknownStream(t *testing.T) {
	writer, reader, _, _ := setupOplogTestify(t)

	_, err := writer.Write(Operation{
		StreamID: "missing-stream",
		SpaceID:  "code",
		EntityID: "ghost.go",
		Type:     OpCreate,
		Path:     "ghost.go",
		Author:   "test",
	})
	require.Error(t, err)

	head, err := reader.Head()
	require.NoError(t, err)
	assert.Equal(t, int64(0), head)

	var count int
	err = writer.db.QueryRow("SELECT COUNT(*) FROM operations").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestOpWriter_ImportBatchRollsBackOnFailure(t *testing.T) {
	writer, reader, sm, stream := setupOplogTestify(t)

	_, err := writer.ImportBatch([]Operation{
		{
			ID:        "remote-op-5",
			Seq:       5,
			StreamID:  stream.ID,
			SpaceID:   "code",
			EntityID:  "remote.go",
			Type:      OpCreate,
			Path:      "remote.go",
			Author:    "remote",
			Timestamp: "2026-04-04T03:00:00Z",
		},
		{
			ID:        "remote-op-0",
			Seq:       0,
			StreamID:  stream.ID,
			SpaceID:   "code",
			EntityID:  "broken.go",
			Type:      OpCreate,
			Path:      "broken.go",
			Author:    "remote",
			Timestamp: "2026-04-04T03:00:01Z",
		},
	})
	require.Error(t, err)

	head, err := reader.Head()
	require.NoError(t, err)
	assert.Equal(t, int64(0), head)

	var opCount int
	err = writer.db.QueryRow("SELECT COUNT(*) FROM operations").Scan(&opCount)
	require.NoError(t, err)
	assert.Equal(t, 0, opCount)

	var entityCount int
	err = writer.db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&entityCount)
	require.NoError(t, err)
	assert.Equal(t, 0, entityCount)

	updated, err := sm.GetByID(stream.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updated.HeadSeq)
}

func TestOpWriter_WriteBatchRejectsUnknownStream(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	_, err := writer.WriteBatch([]Operation{
		{
			StreamID: stream.ID,
			SpaceID:  "code",
			EntityID: "real.go",
			Type:     OpCreate,
			Path:     "real.go",
			Author:   "test",
		},
		{
			StreamID: "missing-stream",
			SpaceID:  "code",
			EntityID: "ghost.go",
			Type:     OpCreate,
			Path:     "ghost.go",
			Author:   "test",
		},
	})
	require.Error(t, err)

	head, err := reader.Head()
	require.NoError(t, err)
	assert.Equal(t, int64(0), head)

	var count int
	err = writer.db.QueryRow("SELECT COUNT(*) FROM operations").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestOpWriter_ImportBatchRejectsUnknownStream(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	_, err := writer.ImportBatch([]Operation{
		{
			ID:        "remote-op-10",
			Seq:       10,
			StreamID:  stream.ID,
			SpaceID:   "code",
			EntityID:  "real.go",
			Type:      OpCreate,
			Path:      "real.go",
			Author:    "remote",
			Timestamp: "2026-04-04T03:00:00Z",
		},
		{
			ID:        "remote-op-11",
			Seq:       11,
			StreamID:  "missing-stream",
			SpaceID:   "code",
			EntityID:  "ghost.go",
			Type:      OpCreate,
			Path:      "ghost.go",
			Author:    "remote",
			Timestamp: "2026-04-04T03:00:01Z",
		},
	})
	require.Error(t, err)

	head, err := reader.Head()
	require.NoError(t, err)
	assert.Equal(t, int64(0), head)

	var count int
	err = writer.db.QueryRow("SELECT COUNT(*) FROM operations").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestOpWriter_WriteBatchPersistsAcrossReopen(t *testing.T) {
	dir, writer, reader, _, stream := setupPersistentOplogTestify(t)

	written, err := writer.WriteBatch([]Operation{
		{
			StreamID: stream.ID,
			SpaceID:  "code",
			EntityID: "alpha.go",
			Type:     OpCreate,
			Path:     "alpha.go",
			Author:   "test",
			Meta:     OpMeta{Size: 11},
		},
		{
			StreamID: stream.ID,
			SpaceID:  "docs",
			EntityID: "guide.md",
			Type:     OpCreate,
			Path:     "guide.md",
			Author:   "test",
			Meta:     OpMeta{Size: 22},
		},
	})
	require.NoError(t, err)
	require.Len(t, written, 2)

	head, err := reader.Head()
	require.NoError(t, err)
	assert.Equal(t, int64(2), head)

	require.NoError(t, writer.db.Close())

	db, err := storage.OpenDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer db.Close()

	reopenedReader := NewOpReader(db)
	reopenedStreams := NewStreamManager(db)

	reopenedHead, err := reopenedReader.Head()
	require.NoError(t, err)
	assert.Equal(t, int64(2), reopenedHead)

	ops, err := reopenedReader.ReadRange(0, reopenedHead)
	require.NoError(t, err)
	require.Len(t, ops, 2)
	assert.Equal(t, "alpha.go", ops[0].Path)
	assert.Equal(t, "guide.md", ops[1].Path)

	reopenedStream, err := reopenedStreams.GetByID(stream.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), reopenedStream.HeadSeq)

	var size int64
	err = db.QueryRow("SELECT size FROM entities WHERE id = 'guide.md' AND space_id = 'docs'").Scan(&size)
	require.NoError(t, err)
	assert.Equal(t, int64(22), size)
}

func TestOpReader_ReadRangeEmpty(t *testing.T) {
	_, reader, _, _ := setupOplogTestify(t)

	ops, err := reader.ReadRange(0, 10)
	require.NoError(t, err)
	assert.Empty(t, ops)
}

func TestOpReader_ReadRangeFullSpan(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	for i := 0; i < 5; i++ {
		writer.Write(Operation{
			StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
			Type: OpModify, Path: "f.go", Author: "test",
		})
	}

	ops, err := reader.ReadRange(0, 5)
	require.NoError(t, err)
	assert.Len(t, ops, 5)
}

func TestOpReader_ReadRangeBeyondHead(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	writer.Write(Operation{
		StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
		Type: OpCreate, Path: "f.go", Author: "test",
	})

	ops, err := reader.ReadRange(0, 1000)
	require.NoError(t, err)
	assert.Len(t, ops, 1)
}

func TestOpReader_ReadByStream(t *testing.T) {
	writer, reader, sm, stream1 := setupOplogTestify(t)

	stream2, err := sm.Create("dev")
	require.NoError(t, err)

	writer.Write(Operation{StreamID: stream1.ID, SpaceID: "code", EntityID: "a.go", Type: OpCreate, Path: "a.go", Author: "test"})
	writer.Write(Operation{StreamID: stream2.ID, SpaceID: "code", EntityID: "b.go", Type: OpCreate, Path: "b.go", Author: "test"})
	writer.Write(Operation{StreamID: stream1.ID, SpaceID: "code", EntityID: "c.go", Type: OpCreate, Path: "c.go", Author: "test"})
	writer.Write(Operation{StreamID: stream2.ID, SpaceID: "code", EntityID: "d.go", Type: OpCreate, Path: "d.go", Author: "test"})

	ops1, err := reader.ReadByStream(stream1.ID, 0, 100)
	require.NoError(t, err)
	assert.Len(t, ops1, 2)

	ops2, err := reader.ReadByStream(stream2.ID, 0, 100)
	require.NoError(t, err)
	assert.Len(t, ops2, 2)
}

func TestOpReader_ReadByStreamEmpty(t *testing.T) {
	_, reader, _, stream := setupOplogTestify(t)

	ops, err := reader.ReadByStream(stream.ID, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, ops)
}

func TestOpReader_ReadByStreamWithRange(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	for i := 0; i < 5; i++ {
		writer.Write(Operation{
			StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
			Type: OpModify, Path: "f.go", Author: "test",
		})
	}

	// Only get ops with seq 3-4
	ops, err := reader.ReadByStream(stream.ID, 2, 4)
	require.NoError(t, err)
	assert.Len(t, ops, 2)
	assert.Equal(t, int64(3), ops[0].Seq)
	assert.Equal(t, int64(4), ops[1].Seq)
}

func TestOpReader_ReadByEntityNotFound(t *testing.T) {
	_, reader, _, _ := setupOplogTestify(t)

	ops, err := reader.ReadByEntity("nonexistent.go")
	require.NoError(t, err)
	assert.Empty(t, ops)
}

func TestOpReader_ReadByEntityOrdered(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "main.go", Type: OpCreate, Path: "main.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "other.go", Type: OpCreate, Path: "other.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "main.go", Type: OpModify, Path: "main.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "main.go", Type: OpModify, Path: "main.go", Author: "test"})

	ops, err := reader.ReadByEntity("main.go")
	require.NoError(t, err)
	assert.Len(t, ops, 3)
	// Verify ascending order
	for i := 1; i < len(ops); i++ {
		assert.True(t, ops[i].Seq > ops[i-1].Seq)
	}
}

func TestOpReader_CountBySpaceEmpty(t *testing.T) {
	_, reader, _, stream := setupOplogTestify(t)

	counts, err := reader.CountBySpace(stream.ID, 0)
	require.NoError(t, err)
	assert.Empty(t, counts)
}

func TestOpReader_CountBySpaceMoveAndRenameAreModified(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "a.go", Type: OpMove, Path: "new/a.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "b.go", Type: OpRename, Path: "b_v2.go", Author: "test"})

	counts, err := reader.CountBySpace(stream.ID, 0)
	require.NoError(t, err)

	code := counts["code"]
	require.NotNil(t, code)
	// Move and rename are counted as modified
	assert.Equal(t, 2, code.Modified)
	assert.Equal(t, 0, code.Created)
	assert.Equal(t, 0, code.Deleted)
}

func TestOpReader_CountBySpaceSinceSeq(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "a.go", Type: OpCreate, Path: "a.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "b.go", Type: OpCreate, Path: "b.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "c.go", Type: OpCreate, Path: "c.go", Author: "test"})

	// Count only after seq 2
	counts, err := reader.CountBySpace(stream.ID, 2)
	require.NoError(t, err)

	code := counts["code"]
	require.NotNil(t, code)
	assert.Equal(t, 1, code.Created, "only ops after seq 2 should be counted")
}

func TestOpWriter_WriteBatchEmpty(t *testing.T) {
	writer, _, _, _ := setupOplogTestify(t)

	result, err := writer.WriteBatch([]Operation{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestOpWriter_WriteBatchAssignsSequentialSeqs(t *testing.T) {
	writer, _, _, stream := setupOplogTestify(t)

	ops := []Operation{
		{StreamID: stream.ID, SpaceID: "code", EntityID: "a.go", Type: OpCreate, Path: "a.go", Author: "test"},
		{StreamID: stream.ID, SpaceID: "code", EntityID: "b.go", Type: OpCreate, Path: "b.go", Author: "test"},
		{StreamID: stream.ID, SpaceID: "code", EntityID: "c.go", Type: OpCreate, Path: "c.go", Author: "test"},
	}

	result, err := writer.WriteBatch(ops)
	require.NoError(t, err)
	require.Len(t, result, 3)

	assert.Equal(t, int64(1), result[0].Seq)
	assert.Equal(t, int64(2), result[1].Seq)
	assert.Equal(t, int64(3), result[2].Seq)

	// Each should have unique IDs
	ids := make(map[string]bool)
	for _, op := range result {
		assert.NotEmpty(t, op.ID)
		assert.False(t, ids[op.ID], "duplicate ID in batch")
		ids[op.ID] = true
	}
}

func TestOpWriter_WriteBatchPreservesCustomTimestamp(t *testing.T) {
	writer, _, _, stream := setupOplogTestify(t)

	ops := []Operation{
		{StreamID: stream.ID, SpaceID: "code", EntityID: "a.go", Type: OpCreate, Path: "a.go", Author: "test", Timestamp: "2020-06-15T12:00:00Z"},
		{StreamID: stream.ID, SpaceID: "code", EntityID: "b.go", Type: OpCreate, Path: "b.go", Author: "test"},
	}

	result, err := writer.WriteBatch(ops)
	require.NoError(t, err)

	assert.Equal(t, "2020-06-15T12:00:00Z", result[0].Timestamp)
	assert.NotEmpty(t, result[1].Timestamp)
	assert.NotEqual(t, "2020-06-15T12:00:00Z", result[1].Timestamp)
}

func TestOpWriter_WriteBatchMixedSpaces(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	ops := []Operation{
		{StreamID: stream.ID, SpaceID: "code", EntityID: "a.go", Type: OpCreate, Path: "a.go", Author: "test"},
		{StreamID: stream.ID, SpaceID: "docs", EntityID: "readme.md", Type: OpCreate, Path: "readme.md", Author: "test"},
		{StreamID: stream.ID, SpaceID: "code", EntityID: "b.go", Type: OpModify, Path: "b.go", Author: "test"},
		{StreamID: stream.ID, SpaceID: "design", EntityID: "mock.fig", Type: OpCreate, Path: "mock.fig", Author: "test"},
	}

	_, err := writer.WriteBatch(ops)
	require.NoError(t, err)

	codeOps, _ := reader.ReadBySpace("code", 0, 100)
	assert.Len(t, codeOps, 2)

	docsOps, _ := reader.ReadBySpace("docs", 0, 100)
	assert.Len(t, docsOps, 1)

	designOps, _ := reader.ReadBySpace("design", 0, 100)
	assert.Len(t, designOps, 1)
}

func TestOpReader_ReadBySpaceWithRange(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "a.go", Type: OpCreate, Path: "a.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "docs", EntityID: "r.md", Type: OpCreate, Path: "r.md", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "b.go", Type: OpCreate, Path: "b.go", Author: "test"})
	writer.Write(Operation{StreamID: stream.ID, SpaceID: "code", EntityID: "c.go", Type: OpCreate, Path: "c.go", Author: "test"})

	// Only code ops after seq 2
	ops, err := reader.ReadBySpace("code", 2, 100)
	require.NoError(t, err)
	assert.Len(t, ops, 2) // b.go and c.go
}

func TestOpReader_HeadAfterBatch(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	ops := make([]Operation, 10)
	for i := range ops {
		ops[i] = Operation{
			StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
			Type: OpModify, Path: "f.go", Author: "test",
		}
	}

	_, err := writer.WriteBatch(ops)
	require.NoError(t, err)

	head, err := reader.Head()
	require.NoError(t, err)
	assert.Equal(t, int64(10), head)
}

func TestOpWriter_WriteAllOpTypes(t *testing.T) {
	writer, reader, _, stream := setupOplogTestify(t)

	types := []OpType{OpCreate, OpModify, OpDelete, OpMove, OpRename}
	for _, opType := range types {
		_, err := writer.Write(Operation{
			StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
			Type: opType, Path: "f.go", Author: "test",
		})
		require.NoError(t, err, "write op type %s", opType)
	}

	ops, err := reader.ReadByEntity("f.go")
	require.NoError(t, err)
	assert.Len(t, ops, len(types))
}
