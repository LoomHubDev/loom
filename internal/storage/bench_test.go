package storage

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkHashContent_Small(b *testing.B) {
	content := []byte("hello world")
	b.ResetTimer()
	for b.Loop() {
		HashContent(content)
	}
}

func BenchmarkHashContent_4KB(b *testing.B) {
	content := make([]byte, 4096)
	for i := range content {
		content[i] = byte(i % 256)
	}
	b.ResetTimer()
	for b.Loop() {
		HashContent(content)
	}
}

func BenchmarkHashContent_1MB(b *testing.B) {
	content := make([]byte, 1<<20)
	for i := range content {
		content[i] = byte(i % 256)
	}
	b.ResetTimer()
	for b.Loop() {
		HashContent(content)
	}
}

func BenchmarkObjectStore_WriteSmall(b *testing.B) {
	dir := b.TempDir()
	db, _ := InitDB(filepath.Join(dir, "bench.db"))
	defer db.Close()
	store, _ := NewObjectStore(filepath.Join(dir, "objects"), db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content := []byte(fmt.Sprintf("content-%d", i))
		store.Write(content, "text/plain")
	}
}

func BenchmarkObjectStore_WriteLarge(b *testing.B) {
	dir := b.TempDir()
	db, _ := InitDB(filepath.Join(dir, "bench.db"))
	defer db.Close()
	store, _ := NewObjectStore(filepath.Join(dir, "objects"), db)

	base := make([]byte, 10000)
	for i := range base {
		base[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content := make([]byte, len(base))
		copy(content, base)
		content[0] = byte(i) // unique content
		store.Write(content, "text/plain")
	}
}

func BenchmarkObjectStore_Read(b *testing.B) {
	dir := b.TempDir()
	db, _ := InitDB(filepath.Join(dir, "bench.db"))
	defer db.Close()
	store, _ := NewObjectStore(filepath.Join(dir, "objects"), db)

	hash, _ := store.Write([]byte("benchmark read content"), "text/plain")

	b.ResetTimer()
	for b.Loop() {
		store.Read(hash)
	}
}

func BenchmarkNextSeq(b *testing.B) {
	dir := b.TempDir()
	db, _ := InitDB(filepath.Join(dir, "bench.db"))
	defer db.Close()

	b.ResetTimer()
	for b.Loop() {
		NextSeq(db)
	}
}
