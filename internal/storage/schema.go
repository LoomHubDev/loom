package storage

const schemaV1 = `
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS operations (
    id         TEXT PRIMARY KEY,
    seq        INTEGER NOT NULL UNIQUE,
    stream_id  TEXT NOT NULL,
    space_id   TEXT NOT NULL,
    entity_id  TEXT NOT NULL,
    type       TEXT NOT NULL,
    path       TEXT NOT NULL,
    delta      BLOB,
    object_ref TEXT,
    parent_seq INTEGER,
    author     TEXT NOT NULL,
    timestamp  TEXT NOT NULL,
    meta       TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_operations_seq ON operations(seq);
CREATE INDEX IF NOT EXISTS idx_operations_stream ON operations(stream_id, seq);
CREATE INDEX IF NOT EXISTS idx_operations_space ON operations(space_id, seq);
CREATE INDEX IF NOT EXISTS idx_operations_entity ON operations(entity_id, seq);
CREATE INDEX IF NOT EXISTS idx_operations_author ON operations(author, seq);
CREATE INDEX IF NOT EXISTS idx_operations_timestamp ON operations(timestamp);

CREATE TABLE IF NOT EXISTS checkpoints (
    id         TEXT PRIMARY KEY,
    stream_id  TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    title      TEXT NOT NULL,
    summary    TEXT,
    author     TEXT NOT NULL,
    timestamp  TEXT NOT NULL,
    source     TEXT NOT NULL,
    spaces     TEXT NOT NULL,
    tags       TEXT,
    parent_id  TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_checkpoints_stream ON checkpoints(stream_id, seq);
CREATE INDEX IF NOT EXISTS idx_checkpoints_source ON checkpoints(source);
CREATE INDEX IF NOT EXISTS idx_checkpoints_timestamp ON checkpoints(timestamp);

CREATE VIRTUAL TABLE IF NOT EXISTS checkpoints_fts USING fts5(
    id UNINDEXED,
    title,
    summary,
    content='checkpoints',
    content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS checkpoints_ai AFTER INSERT ON checkpoints BEGIN
    INSERT INTO checkpoints_fts(rowid, id, title, summary)
    VALUES (new.rowid, new.id, new.title, COALESCE(new.summary, ''));
END;

CREATE TABLE IF NOT EXISTS streams (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    head_seq   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    parent_id  TEXT,
    fork_seq   INTEGER,
    status     TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE IF NOT EXISTS entities (
    id         TEXT NOT NULL,
    space_id   TEXT NOT NULL,
    path       TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'file',
    object_ref TEXT,
    size       INTEGER,
    mod_time   TEXT,
    status     TEXT NOT NULL DEFAULT 'active',
    meta       TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (id, space_id)
);

CREATE INDEX IF NOT EXISTS idx_entities_space ON entities(space_id);
CREATE INDEX IF NOT EXISTS idx_entities_path ON entities(space_id, path);

CREATE TABLE IF NOT EXISTS objects (
    hash         TEXT PRIMARY KEY,
    size         INTEGER NOT NULL,
    compressed   INTEGER NOT NULL DEFAULT 0,
    content_type TEXT,
    ref_count    INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS remotes (
    name       TEXT PRIMARY KEY,
    url        TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    last_push  TEXT,
    last_pull  TEXT,
    push_seq   INTEGER NOT NULL DEFAULT 0,
    pull_seq   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS metadata (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO metadata (key, value) VALUES ('seq_counter', '0');
INSERT OR IGNORE INTO metadata (key, value) VALUES ('schema_version', '1');
`
