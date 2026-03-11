# 05 — Storage Schema

## Overview

Loom uses two storage systems:

1. **SQLite** — the operation log, checkpoints, streams, entities, and metadata. This is the structured data that needs indexing and querying.
2. **Object Store** — content-addressed file storage for blobs (file contents, design data, binary assets). Flat files on disk, addressed by SHA-256.

## SQLite Database

Located at `.loom/loom.db`. Uses WAL mode for concurrent read/write.

### Initialization

```go
func InitDB(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, err
    }

    // Performance pragmas
    pragmas := []string{
        "PRAGMA journal_mode=WAL",
        "PRAGMA synchronous=NORMAL",
        "PRAGMA cache_size=-64000",     // 64MB cache
        "PRAGMA foreign_keys=ON",
        "PRAGMA busy_timeout=5000",
        "PRAGMA wal_autocheckpoint=1000",
    }

    for _, p := range pragmas {
        if _, err := db.Exec(p); err != nil {
            return nil, fmt.Errorf("pragma %s: %w", p, err)
        }
    }

    if err := migrate(db); err != nil {
        return nil, fmt.Errorf("migrate: %w", err)
    }

    return db, nil
}
```

### Schema

```sql
-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ============================================================
-- OPERATIONS (the core — append-only log)
-- ============================================================

CREATE TABLE IF NOT EXISTS operations (
    id         TEXT PRIMARY KEY,              -- ULID
    seq        INTEGER NOT NULL UNIQUE,       -- Monotonic sequence number
    stream_id  TEXT NOT NULL,                 -- FK to streams
    space_id   TEXT NOT NULL,                 -- Space identifier
    entity_id  TEXT NOT NULL,                 -- Entity identifier (path-based)
    type       TEXT NOT NULL,                 -- create, modify, delete, move, rename, meta
    path       TEXT NOT NULL,                 -- Path within space
    delta      BLOB,                          -- Change payload (diff, patch, null)
    object_ref TEXT,                          -- SHA-256 of content (if applicable)
    parent_seq INTEGER,                       -- Previous op sequence for ordering
    author     TEXT NOT NULL,                 -- Author identifier
    timestamp  TEXT NOT NULL,                 -- RFC 3339
    meta       TEXT,                          -- JSON: OpMeta
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Sequential reads (most common query: replay ops in order)
CREATE INDEX IF NOT EXISTS idx_operations_seq ON operations(seq);

-- Filter by stream
CREATE INDEX IF NOT EXISTS idx_operations_stream ON operations(stream_id, seq);

-- Filter by space
CREATE INDEX IF NOT EXISTS idx_operations_space ON operations(space_id, seq);

-- Filter by entity
CREATE INDEX IF NOT EXISTS idx_operations_entity ON operations(entity_id, seq);

-- Filter by author
CREATE INDEX IF NOT EXISTS idx_operations_author ON operations(author, seq);

-- Time-range queries
CREATE INDEX IF NOT EXISTS idx_operations_timestamp ON operations(timestamp);

-- ============================================================
-- CHECKPOINTS
-- ============================================================

CREATE TABLE IF NOT EXISTS checkpoints (
    id         TEXT PRIMARY KEY,              -- ULID
    stream_id  TEXT NOT NULL,                 -- FK to streams
    seq        INTEGER NOT NULL,              -- Op sequence this checkpoint is at
    title      TEXT NOT NULL,                 -- Human-readable name
    summary    TEXT,                          -- Description
    author     TEXT NOT NULL,                 -- Who created it
    timestamp  TEXT NOT NULL,                 -- RFC 3339
    source     TEXT NOT NULL,                 -- manual, auto, agent, workflow, guard, restore
    spaces     TEXT NOT NULL,                 -- JSON: []SpaceState
    tags       TEXT,                          -- JSON: map[string]string
    parent_id  TEXT,                          -- Previous checkpoint ID
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_checkpoints_stream ON checkpoints(stream_id, seq);
CREATE INDEX IF NOT EXISTS idx_checkpoints_source ON checkpoints(source);
CREATE INDEX IF NOT EXISTS idx_checkpoints_timestamp ON checkpoints(timestamp);

-- Full-text search on checkpoint titles and summaries
CREATE VIRTUAL TABLE IF NOT EXISTS checkpoints_fts USING fts5(
    id UNINDEXED,
    title,
    summary,
    content='checkpoints',
    content_rowid='rowid'
);

-- Triggers to keep FTS in sync
CREATE TRIGGER IF NOT EXISTS checkpoints_ai AFTER INSERT ON checkpoints BEGIN
    INSERT INTO checkpoints_fts(rowid, id, title, summary)
    VALUES (new.rowid, new.id, new.title, new.summary);
END;

CREATE TRIGGER IF NOT EXISTS checkpoints_ad AFTER DELETE ON checkpoints BEGIN
    INSERT INTO checkpoints_fts(checkpoints_fts, rowid, id, title, summary)
    VALUES ('delete', old.rowid, old.id, old.title, old.summary);
END;

-- ============================================================
-- STREAMS
-- ============================================================

CREATE TABLE IF NOT EXISTS streams (
    id         TEXT PRIMARY KEY,              -- ULID
    name       TEXT NOT NULL UNIQUE,          -- Human-readable name
    head_seq   INTEGER NOT NULL DEFAULT 0,    -- Latest op sequence
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    parent_id  TEXT,                          -- Parent stream (if forked)
    fork_seq   INTEGER,                       -- Sequence where forked
    status     TEXT NOT NULL DEFAULT 'active' -- active, merged, archived
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_streams_name ON streams(name);

-- ============================================================
-- ENTITIES (tracked items across all spaces)
-- ============================================================

CREATE TABLE IF NOT EXISTS entities (
    id         TEXT NOT NULL,                 -- Entity identifier
    space_id   TEXT NOT NULL,                 -- Space
    path       TEXT NOT NULL,                 -- Current path
    kind       TEXT NOT NULL,                 -- file, document, component, schema, ...
    object_ref TEXT,                          -- Current content hash
    size       INTEGER,                       -- Current size in bytes
    mod_time   TEXT,                          -- Last modification time
    status     TEXT NOT NULL DEFAULT 'active', -- active, deleted
    meta       TEXT,                          -- JSON metadata
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (id, space_id)
);

CREATE INDEX IF NOT EXISTS idx_entities_space ON entities(space_id);
CREATE INDEX IF NOT EXISTS idx_entities_path ON entities(space_id, path);

-- ============================================================
-- OBJECTS (index for the content-addressed store)
-- ============================================================

CREATE TABLE IF NOT EXISTS objects (
    hash         TEXT PRIMARY KEY,            -- SHA-256 hex
    size         INTEGER NOT NULL,            -- Uncompressed size
    compressed   INTEGER NOT NULL DEFAULT 0,  -- 1 if stored compressed
    content_type TEXT,                        -- MIME type
    ref_count    INTEGER NOT NULL DEFAULT 1,  -- Number of ops referencing this
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ============================================================
-- REMOTES
-- ============================================================

CREATE TABLE IF NOT EXISTS remotes (
    name       TEXT PRIMARY KEY,              -- Alias (e.g., "origin")
    url        TEXT NOT NULL,                 -- Server URL
    is_default INTEGER NOT NULL DEFAULT 0,    -- Default push/pull target
    last_push  TEXT,                          -- Timestamp of last push
    last_pull  TEXT,                          -- Timestamp of last pull
    push_seq   INTEGER NOT NULL DEFAULT 0,    -- Last pushed sequence
    pull_seq   INTEGER NOT NULL DEFAULT 0,    -- Last pulled sequence
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ============================================================
-- METADATA (key-value store for misc project data)
-- ============================================================

CREATE TABLE IF NOT EXISTS metadata (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Store current sequence counter
-- INSERT INTO metadata (key, value) VALUES ('seq_counter', '0');

-- ============================================================
-- SYNC LOG (track what's been synced with each remote)
-- ============================================================

CREATE TABLE IF NOT EXISTS sync_log (
    id         TEXT PRIMARY KEY,              -- ULID
    remote     TEXT NOT NULL,                 -- Remote name
    direction  TEXT NOT NULL,                 -- "push" or "pull"
    from_seq   INTEGER NOT NULL,              -- Start of synced range
    to_seq     INTEGER NOT NULL,              -- End of synced range
    ops_count  INTEGER NOT NULL,              -- Number of ops synced
    timestamp  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_sync_log_remote ON sync_log(remote, timestamp);
```

### Migration Strategy

Migrations are numbered SQL files applied in order:

```go
var migrations = []string{
    migrationV1, // Initial schema (above)
    // migrationV2, // Future schema changes
}

func migrate(db *sql.DB) error {
    var current int
    db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&current)

    for i := current; i < len(migrations); i++ {
        tx, _ := db.Begin()
        tx.Exec(migrations[i])
        tx.Exec("INSERT INTO schema_version (version) VALUES (?)", i+1)
        tx.Commit()
    }
    return nil
}
```

### Common Queries

```sql
-- Get operations for a stream in order
SELECT * FROM operations WHERE stream_id = ? ORDER BY seq ASC;

-- Get operations between two sequence numbers
SELECT * FROM operations WHERE seq > ? AND seq <= ? ORDER BY seq ASC;

-- Get operations for a specific entity
SELECT * FROM operations WHERE entity_id = ? ORDER BY seq ASC;

-- Get operations by space since last checkpoint
SELECT * FROM operations
WHERE space_id = ? AND seq > (
    SELECT COALESCE(MAX(seq), 0) FROM checkpoints WHERE stream_id = ?
)
ORDER BY seq ASC;

-- Get latest checkpoint for a stream
SELECT * FROM checkpoints WHERE stream_id = ? ORDER BY seq DESC LIMIT 1;

-- Search checkpoints by title/summary
SELECT c.* FROM checkpoints c
JOIN checkpoints_fts f ON c.id = f.id
WHERE checkpoints_fts MATCH ?
ORDER BY c.seq DESC;

-- Get next sequence number (atomic)
UPDATE metadata SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)
WHERE key = 'seq_counter'
RETURNING CAST(value AS INTEGER);

-- Count operations per space since a checkpoint
SELECT space_id, COUNT(*) as op_count
FROM operations
WHERE seq > ? AND stream_id = ?
GROUP BY space_id;
```

## Object Store

### Directory Layout

```
.loom/objects/
  ab/
    abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567
  cd/
    cdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
```

First 2 hex characters of the SHA-256 hash become the directory name. The full hash is the filename. This prevents too many files in a single directory (max 256 subdirectories).

### Write Flow

```go
func (s *ObjectStore) Write(content []byte) (string, error) {
    hash := HashContent(content)

    dir := filepath.Join(s.root, hash[:2])
    path := filepath.Join(dir, hash)

    // Deduplication: if object exists, skip write
    if _, err := os.Stat(path); err == nil {
        s.incrementRefCount(hash)
        return hash, nil
    }

    os.MkdirAll(dir, 0755)

    // Compress if larger than threshold (4KB)
    var data []byte
    compressed := false
    if len(content) > 4096 {
        data = zstdCompress(content)
        compressed = true
    } else {
        data = content
    }

    // Atomic write: write to temp file, then rename
    tmp := path + ".tmp"
    os.WriteFile(tmp, data, 0444) // Read-only
    os.Rename(tmp, path)

    // Record in SQLite index
    s.db.Exec(`INSERT INTO objects (hash, size, compressed, ref_count) VALUES (?, ?, ?, 1)`,
        hash, len(content), boolToInt(compressed))

    return hash, nil
}
```

### Read Flow

```go
func (s *ObjectStore) Read(hash string) ([]byte, error) {
    path := filepath.Join(s.root, hash[:2], hash)
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("object %s not found: %w", hash[:12], err)
    }

    // Check if compressed
    var compressed bool
    s.db.QueryRow("SELECT compressed FROM objects WHERE hash = ?", hash).Scan(&compressed)

    if compressed {
        return zstdDecompress(data)
    }
    return data, nil
}
```

### Garbage Collection

Objects whose `ref_count` drops to 0 can be garbage collected:

```go
func (s *ObjectStore) GC() (int, error) {
    rows, _ := s.db.Query("SELECT hash FROM objects WHERE ref_count <= 0")
    var count int
    for rows.Next() {
        var hash string
        rows.Scan(&hash)
        os.Remove(filepath.Join(s.root, hash[:2], hash))
        s.db.Exec("DELETE FROM objects WHERE hash = ?", hash)
        count++
    }
    return count, nil
}
```

### Compression Strategy

| Content Size | Strategy |
|-------------|----------|
| < 4 KB | Store raw (compression overhead not worth it) |
| 4 KB – 10 MB | Zstandard compression (fast, good ratio) |
| > 10 MB | Zstandard compression + chunked storage (future) |

## Lock Management

```
.loom/locks/
  writer.lock     # Held by the process writing operations
```

File-based locking prevents concurrent writes from multiple processes (e.g., daemon + CLI).

```go
func (l *Lock) Acquire() error {
    f, err := os.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
    if err != nil {
        return fmt.Errorf("lock held by another process")
    }
    fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
    f.Close()
    return nil
}

func (l *Lock) Release() error {
    return os.Remove(l.path)
}
```

## Backup and Recovery

### Export

```bash
# Export full project history as a portable archive
loom export --output project-backup.loom

# The archive contains:
# - loom.db (SQLite)
# - objects/ (all blobs)
# - config.toml
```

### Import

```bash
# Import from a backup
loom import project-backup.loom
```

### Integrity Check

```bash
# Verify database integrity and object store consistency
loom doctor

# Checks:
# - SQLite integrity_check
# - Object store: all referenced hashes exist
# - Object store: all stored objects are referenced
# - Sequence numbers: no gaps, monotonic
# - Stream heads: point to valid operations
```
