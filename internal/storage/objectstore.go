package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

const compressionThreshold = 4096 // 4KB

// ObjectStore manages content-addressed blob storage.
type ObjectStore struct {
	root string
	db   *sql.DB
	enc  *zstd.Encoder
	dec  *zstd.Decoder
}

// NewObjectStore creates an ObjectStore at the given root directory.
func NewObjectStore(root string, db *sql.DB) (*ObjectStore, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("create object store: %w", err)
	}

	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("create zstd encoder: %w", err)
	}

	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}

	return &ObjectStore{root: root, db: db, enc: enc, dec: dec}, nil
}

// Root returns the object store root directory.
func (s *ObjectStore) Root() string {
	return s.root
}

// Write stores content and returns its hash. Deduplicates automatically.
func (s *ObjectStore) Write(content []byte, contentType string) (string, error) {
	hash := HashContent(content)
	objPath := s.objectPath(hash)

	// Deduplication check
	if _, err := os.Stat(objPath); err == nil {
		// Already exists — increment ref count
		s.db.Exec("UPDATE objects SET ref_count = ref_count + 1 WHERE hash = ?", hash)
		return hash, nil
	}

	if err := os.MkdirAll(filepath.Dir(objPath), 0755); err != nil {
		return "", fmt.Errorf("create object dir: %w", err)
	}

	// Compress if above threshold
	var data []byte
	compressed := false
	if len(content) > compressionThreshold {
		data = s.enc.EncodeAll(content, nil)
		compressed = true
	} else {
		data = content
	}

	// Atomic write: temp file then rename
	tmpPath := objPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0444); err != nil {
		return "", fmt.Errorf("write object: %w", err)
	}
	if err := os.Rename(tmpPath, objPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("rename object: %w", err)
	}

	// Record in index
	compInt := 0
	if compressed {
		compInt = 1
	}
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO objects (hash, size, compressed, content_type, ref_count) VALUES (?, ?, ?, ?, 1)",
		hash, len(content), compInt, contentType,
	)
	if err != nil {
		return "", fmt.Errorf("index object: %w", err)
	}

	return hash, nil
}

// Read retrieves content by hash.
func (s *ObjectStore) Read(hash string) ([]byte, error) {
	objPath := s.objectPath(hash)
	data, err := os.ReadFile(objPath)
	if err != nil {
		return nil, fmt.Errorf("read object %s: %w", hash[:12], err)
	}

	// Check if compressed
	var compressed int
	err = s.db.QueryRow("SELECT compressed FROM objects WHERE hash = ?", hash).Scan(&compressed)
	if err != nil {
		// If not in index, try decompressing anyway
		if result, decErr := s.dec.DecodeAll(data, nil); decErr == nil {
			return result, nil
		}
		return data, nil
	}

	if compressed == 1 {
		result, err := s.dec.DecodeAll(data, nil)
		if err != nil {
			return nil, fmt.Errorf("decompress object %s: %w", hash[:12], err)
		}
		return result, nil
	}

	return data, nil
}

// Exists checks if an object exists in the store.
func (s *ObjectStore) Exists(hash string) bool {
	_, err := os.Stat(s.objectPath(hash))
	return err == nil
}

// IsCompressed returns whether an object is stored compressed.
func (s *ObjectStore) IsCompressed(hash string) bool {
	var compressed int
	err := s.db.QueryRow("SELECT compressed FROM objects WHERE hash = ?", hash).Scan(&compressed)
	if err != nil {
		return false
	}
	return compressed == 1
}

func (s *ObjectStore) objectPath(hash string) string {
	return filepath.Join(s.root, hash[:2], hash)
}
