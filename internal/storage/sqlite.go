package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// InitDB creates and initializes a SQLite database at the given path.
func InitDB(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := configurePragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// OpenDB opens an existing SQLite database.
func OpenDB(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("database not found: %s", path)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := configurePragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func configurePragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-64000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA wal_autocheckpoint=1000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("pragma %s: %w", p, err)
		}
	}
	return nil
}

func migrate(db *sql.DB) error {
	// Check if schema_version table exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_version'").Scan(&count)
	if err != nil {
		return fmt.Errorf("check schema: %w", err)
	}

	if count == 0 {
		// Fresh database — apply schema
		if _, err := db.Exec(schemaV1); err != nil {
			return fmt.Errorf("apply schema v1: %w", err)
		}
		if _, err := db.Exec("INSERT INTO schema_version (version) VALUES (1)"); err != nil {
			return fmt.Errorf("record schema version: %w", err)
		}
	}

	return nil
}

// NextSeq atomically increments and returns the next sequence number.
func NextSeq(db *sql.DB) (int64, error) {
	var seq int64
	err := db.QueryRow(`
		UPDATE metadata SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)
		WHERE key = 'seq_counter'
		RETURNING CAST(value AS INTEGER)
	`).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("next seq: %w", err)
	}
	return seq, nil
}
