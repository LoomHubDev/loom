package storage

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStoreTestify(t *testing.T) *ObjectStore {
	t.Helper()
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)
	return store
}

func TestObjectStore_Root(t *testing.T) {
	store := newTestStoreTestify(t)
	assert.NotEmpty(t, store.Root())
	assert.True(t, strings.HasSuffix(store.Root(), "objects"))
}

func TestObjectStore_WriteEmptyContent(t *testing.T) {
	store := newTestStoreTestify(t)

	hash, err := store.Write([]byte{}, "application/octet-stream")
	require.NoError(t, err)
	assert.Len(t, hash, 64)

	read, err := store.Read(hash)
	require.NoError(t, err)
	assert.Empty(t, read)
}

func TestObjectStore_WriteAndReadSmall(t *testing.T) {
	store := newTestStoreTestify(t)

	content := []byte("small content")
	hash, err := store.Write(content, "text/plain")
	require.NoError(t, err)

	read, err := store.Read(hash)
	require.NoError(t, err)
	assert.Equal(t, content, read)
	assert.False(t, store.IsCompressed(hash))
}

func TestObjectStore_CompressionThreshold_BelowThreshold(t *testing.T) {
	store := newTestStoreTestify(t)

	// Exactly at threshold (4096 bytes) - should NOT be compressed
	content := make([]byte, 4096)
	for i := range content {
		content[i] = byte(i % 26) + 'a'
	}

	hash, err := store.Write(content, "text/plain")
	require.NoError(t, err)
	assert.False(t, store.IsCompressed(hash))

	read, err := store.Read(hash)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(content, read))
}

func TestObjectStore_CompressionThreshold_AboveThreshold(t *testing.T) {
	store := newTestStoreTestify(t)

	// 4097 bytes - should be compressed
	content := make([]byte, 4097)
	for i := range content {
		content[i] = byte(i % 26) + 'a'
	}

	hash, err := store.Write(content, "text/plain")
	require.NoError(t, err)
	assert.True(t, store.IsCompressed(hash))

	read, err := store.Read(hash)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(content, read))
}

func TestObjectStore_Deduplication_SameHash(t *testing.T) {
	store := newTestStoreTestify(t)

	content := []byte("deduplicate me")
	h1, err := store.Write(content, "text/plain")
	require.NoError(t, err)

	h2, err := store.Write(content, "text/plain")
	require.NoError(t, err)

	assert.Equal(t, h1, h2, "same content should produce same hash")
}

func TestObjectStore_Deduplication_DifferentContentType(t *testing.T) {
	store := newTestStoreTestify(t)

	content := []byte("same bytes")
	h1, _ := store.Write(content, "text/plain")
	h2, _ := store.Write(content, "application/json")

	// Content-addressed: same bytes = same hash regardless of content type
	assert.Equal(t, h1, h2)
}

func TestObjectStore_DifferentContent_DifferentHash(t *testing.T) {
	store := newTestStoreTestify(t)

	h1, _ := store.Write([]byte("content A"), "text/plain")
	h2, _ := store.Write([]byte("content B"), "text/plain")

	assert.NotEqual(t, h1, h2)
}

func TestObjectStore_ExistsTrue(t *testing.T) {
	store := newTestStoreTestify(t)

	hash, _ := store.Write([]byte("exists"), "text/plain")
	assert.True(t, store.Exists(hash))
}

func TestObjectStore_ExistsFalse(t *testing.T) {
	store := newTestStoreTestify(t)
	assert.False(t, store.Exists("0000000000000000000000000000000000000000000000000000000000000000"))
}

func TestObjectStore_ReadMissingHash(t *testing.T) {
	store := newTestStoreTestify(t)

	_, err := store.Read("0000000000000000000000000000000000000000000000000000000000000000")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read object")
}

func TestObjectStore_IsCompressed_NonexistentHash(t *testing.T) {
	store := newTestStoreTestify(t)
	assert.False(t, store.IsCompressed("0000000000000000000000000000000000000000000000000000000000000000"))
}

func TestObjectStore_LargeCompressedRoundtrip(t *testing.T) {
	store := newTestStoreTestify(t)

	// Highly compressible content (repeating pattern)
	content := []byte(strings.Repeat("ABCDEFGH", 10000))
	hash, err := store.Write(content, "text/plain")
	require.NoError(t, err)

	assert.True(t, store.IsCompressed(hash))

	read, err := store.Read(hash)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(content, read))
}

func TestObjectStore_BinaryContentRoundtrip(t *testing.T) {
	store := newTestStoreTestify(t)

	// Binary content with null bytes
	content := make([]byte, 256)
	for i := range content {
		content[i] = byte(i)
	}

	hash, err := store.Write(content, "application/octet-stream")
	require.NoError(t, err)

	read, err := store.Read(hash)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(content, read))
}

func TestObjectStore_ConcurrentWrites(t *testing.T) {
	store := newTestStoreTestify(t)

	var mu sync.Mutex
	var wg sync.WaitGroup
	var successCount int

	for i := range 20 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			content := []byte(strings.Repeat("x", idx+1))
			hash, err := store.Write(content, "text/plain")
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
				assert.NotEmpty(t, hash)
			}
			// SQLite BUSY under high contention is acceptable
		}(i)
	}
	wg.Wait()

	// At least some writes should succeed
	assert.Greater(t, successCount, 0, "at least some concurrent writes should succeed")
}

func TestObjectStore_ConcurrentReads(t *testing.T) {
	store := newTestStoreTestify(t)

	content := []byte("concurrent read content")
	hash, err := store.Write(content, "text/plain")
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			read, err := store.Read(hash)
			assert.NoError(t, err)
			assert.True(t, bytes.Equal(content, read))
		}()
	}
	wg.Wait()
}

func TestObjectStore_WriteMultipleTypes(t *testing.T) {
	store := newTestStoreTestify(t)

	files := []struct {
		content     []byte
		contentType string
	}{
		{[]byte("package main"), "text/x-go"},
		{[]byte(`{"key":"value"}`), "application/json"},
		{[]byte("<html></html>"), "text/html"},
		{[]byte{0x89, 0x50, 0x4E, 0x47}, "image/png"}, // PNG magic bytes
	}

	for _, f := range files {
		hash, err := store.Write(f.content, f.contentType)
		require.NoError(t, err)
		assert.True(t, store.Exists(hash))

		read, err := store.Read(hash)
		require.NoError(t, err)
		assert.Equal(t, f.content, read)
	}
}

func TestObjectStore_WriteNilContent(t *testing.T) {
	store := newTestStoreTestify(t)

	hash, err := store.Write(nil, "text/plain")
	require.NoError(t, err)
	assert.Len(t, hash, 64)

	read, err := store.Read(hash)
	require.NoError(t, err)
	assert.Empty(t, read)
}
