package core

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/storage"
)

func setupBenchEnv(b *testing.B) (*OpWriter, *OpReader, *Stream) {
	b.Helper()
	dir := b.TempDir()
	db, _ := storage.InitDB(filepath.Join(dir, "bench.db"))
	b.Cleanup(func() { db.Close() })
	store, _ := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	sm := NewStreamManager(db)
	stream, _ := sm.Create("main")
	writer := NewOpWriter(db, store)
	reader := NewOpReader(db)
	return writer, reader, stream
}

func BenchmarkOpWriter_Write(b *testing.B) {
	writer, _, stream := setupBenchEnv(b)
	op := Operation{
		StreamID: stream.ID,
		SpaceID:  "code",
		EntityID: "bench.go",
		Type:     OpModify,
		Path:     "bench.go",
		Author:   "bench",
		Meta:     OpMeta{Size: 100},
	}

	b.ResetTimer()
	for b.Loop() {
		writer.Write(op)
	}
}

func BenchmarkOpWriter_WriteBatch(b *testing.B) {
	writer, _, stream := setupBenchEnv(b)

	ops := make([]Operation, 10)
	for i := range ops {
		ops[i] = Operation{
			StreamID: stream.ID,
			SpaceID:  "code",
			EntityID: fmt.Sprintf("file%d.go", i),
			Type:     OpCreate,
			Path:     fmt.Sprintf("file%d.go", i),
			Author:   "bench",
		}
	}

	b.ResetTimer()
	for b.Loop() {
		writer.WriteBatch(ops)
	}
}

func BenchmarkOpReader_ReadRange(b *testing.B) {
	writer, reader, stream := setupBenchEnv(b)

	// Prepopulate 1000 ops
	for i := 0; i < 1000; i++ {
		writer.Write(Operation{
			StreamID: stream.ID, SpaceID: "code",
			EntityID: fmt.Sprintf("file%d.go", i%100), Type: OpModify,
			Path: fmt.Sprintf("file%d.go", i%100), Author: "bench",
		})
	}

	b.ResetTimer()
	for b.Loop() {
		reader.ReadRange(0, 1000)
	}
}

func BenchmarkCheckpoint_Create(b *testing.B) {
	dir := b.TempDir()
	db, _ := storage.InitDB(filepath.Join(dir, "bench.db"))
	b.Cleanup(func() { db.Close() })
	store, _ := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	sm := NewStreamManager(db)
	stream, _ := sm.Create("main")
	writer := NewOpWriter(db, store)
	reader := NewOpReader(db)
	engine := NewCheckpointEngine(db, reader)

	// Write some ops
	for i := 0; i < 50; i++ {
		writer.Write(Operation{
			StreamID: stream.ID, SpaceID: "code",
			EntityID: fmt.Sprintf("f%d.go", i), Type: OpCreate,
			Path: fmt.Sprintf("f%d.go", i), Author: "bench",
		})
	}

	b.ResetTimer()
	for b.Loop() {
		engine.Create(CheckpointInput{
			StreamID: stream.ID, Title: "bench", Author: "bench", Source: SourceAuto,
		})
	}
}
