package storage

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitDB_CreatesAllTables(t *testing.T) {
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer db.Close()

	tables := []string{"operations", "checkpoints", "streams", "entities", "objects", "remotes", "metadata", "schema_version"}
	for _, table := range tables {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "table %s should exist", table)
	}
}

func TestInitDB_CreatesFTSTable(t *testing.T) {
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='checkpoints_fts'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestInitDB_SetsSeqCounter(t *testing.T) {
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer db.Close()

	var val string
	err = db.QueryRow("SELECT value FROM metadata WHERE key = 'seq_counter'").Scan(&val)
	require.NoError(t, err)
	assert.Equal(t, "0", val)
}

func TestInitDB_SetsSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer db.Close()

	var version int
	err = db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, 1, version)
}

func TestInitDB_MigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// First init
	db1, err := InitDB(path)
	require.NoError(t, err)
	db1.Close()

	// Open and re-migrate should not fail
	db2, err := OpenDB(path)
	require.NoError(t, err)
	defer db2.Close()

	// Tables should still work
	var count int
	err = db2.QueryRow("SELECT COUNT(*) FROM operations").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestInitDB_CreatesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "test.db")

	db, err := InitDB(path)
	require.NoError(t, err)
	defer db.Close()

	assert.FileExists(t, path)
}

func TestOpenDB_NonexistentPath(t *testing.T) {
	_, err := OpenDB("/nonexistent/path/to/db.db")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database not found")
}

func TestOpenDB_ExistingDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db1, err := InitDB(path)
	require.NoError(t, err)
	db1.Close()

	db2, err := OpenDB(path)
	require.NoError(t, err)
	defer db2.Close()

	// Should be able to query
	var val string
	err = db2.QueryRow("SELECT value FROM metadata WHERE key = 'seq_counter'").Scan(&val)
	require.NoError(t, err)
	assert.Equal(t, "0", val)
}

func TestNextSeq_Sequential(t *testing.T) {
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer db.Close()

	for i := int64(1); i <= 10; i++ {
		seq, err := NextSeq(db)
		require.NoError(t, err)
		assert.Equal(t, i, seq)
	}
}

func TestNextSeq_ConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer db.Close()

	var mu sync.Mutex
	var wg sync.WaitGroup
	count := 50
	var seqs []int64

	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seq, err := NextSeq(db)
			if err != nil {
				return // SQLite busy under contention is acceptable
			}
			mu.Lock()
			seqs = append(seqs, seq)
			mu.Unlock()
		}()
	}
	wg.Wait()

	// At least some should succeed
	assert.NotEmpty(t, seqs, "at least some NextSeq calls should succeed")

	// All successful seqs should be unique
	seen := make(map[int64]bool)
	for _, seq := range seqs {
		assert.False(t, seen[seq], "duplicate seq: %d", seq)
		assert.Greater(t, seq, int64(0), "seq should be positive")
		seen[seq] = true
	}
}

func TestInitDB_PragmasApplied(t *testing.T) {
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer db.Close()

	// Check WAL mode
	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	require.NoError(t, err)
	assert.Equal(t, "wal", journalMode)

	// Check foreign keys
	var fk int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	require.NoError(t, err)
	assert.Equal(t, 1, fk)
}

func TestInitDB_CreatesIndexes(t *testing.T) {
	dir := t.TempDir()
	db, err := InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer db.Close()

	indexes := []string{
		"idx_operations_seq",
		"idx_operations_stream",
		"idx_operations_space",
		"idx_operations_entity",
		"idx_operations_author",
		"idx_operations_timestamp",
		"idx_checkpoints_stream",
		"idx_checkpoints_source",
		"idx_checkpoints_timestamp",
		"idx_entities_space",
		"idx_entities_path",
	}

	for _, idx := range indexes {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "index %s should exist", idx)
	}
}
