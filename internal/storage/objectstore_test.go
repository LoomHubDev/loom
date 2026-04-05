package storage

import (
	"bytes"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *ObjectStore {
	t.Helper()
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store, err := NewObjectStore(filepath.Join(dir, "objects"), db)
	if err != nil {
		t.Fatalf("new object store: %v", err)
	}
	return store
}

func TestObjectStore_WriteRead(t *testing.T) {
	store := newTestStore(t)

	content := []byte("hello world")
	hash, err := store.Write(content, "text/plain")
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	if len(hash) != 64 {
		t.Errorf("expected 64 char hash, got %d", len(hash))
	}

	read, err := store.Read(hash)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(content, read) {
		t.Errorf("content mismatch: %q != %q", content, read)
	}
}

func TestObjectStore_Deduplication(t *testing.T) {
	store := newTestStore(t)

	content := []byte("duplicate content")
	hash1, _ := store.Write(content, "text/plain")
	hash2, _ := store.Write(content, "text/plain")

	if hash1 != hash2 {
		t.Errorf("hashes differ: %s != %s", hash1, hash2)
	}
}

func TestObjectStore_Compression(t *testing.T) {
	store := newTestStore(t)

	// Small content — not compressed
	small := []byte("hi")
	smallHash, _ := store.Write(small, "text/plain")
	if store.IsCompressed(smallHash) {
		t.Error("small content should not be compressed")
	}

	// Large content — compressed
	large := make([]byte, 10000)
	for i := range large {
		large[i] = byte(i % 26) + 'a'
	}
	largeHash, _ := store.Write(large, "text/plain")
	if !store.IsCompressed(largeHash) {
		t.Error("large content should be compressed")
	}

	// Read back should match original
	read, err := store.Read(largeHash)
	if err != nil {
		t.Fatalf("read large: %v", err)
	}
	if !bytes.Equal(large, read) {
		t.Error("large content mismatch after compression roundtrip")
	}
}

func TestObjectStore_Exists(t *testing.T) {
	store := newTestStore(t)

	content := []byte("test exists")
	hash, _ := store.Write(content, "text/plain")

	if !store.Exists(hash) {
		t.Error("object should exist")
	}

	if store.Exists("0000000000000000000000000000000000000000000000000000000000000000") {
		t.Error("nonexistent hash should not exist")
	}
}

func TestObjectStore_ReadNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Read("0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Error("expected error for missing object")
	}
}

func TestObjectStore_ReadRejectsInvalidHashLength(t *testing.T) {
	store := newTestStore(t)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Read should return an error, not panic: %v", r)
		}
	}()

	_, err := store.Read("short")
	if err == nil {
		t.Fatal("expected invalid hash length error")
	}
}
