# 06 — Systems: Operation Log

## Overview

The operation log is the foundation of Loom. Every change in every space is recorded as an operation — an append-only, immutable entry in the log. State is never stored directly; it's derived by replaying operations.

This is fundamentally different from Git, which stores snapshots (full tree state at each commit). Loom stores deltas (what changed), which enables:

- Continuous auto-versioning without storage explosion
- Fine-grained history (every save, not just commits)
- Efficient sync (send only new ops, not full snapshots)
- Time-travel to any point, not just named checkpoints

## Architecture

```
File Change
  │
  ▼
Space Adapter → Operation
  │
  ▼
┌─────────────────────────────────┐
│         Operation Writer        │
│  ┌───────────────────────────┐  │
│  │ 1. Validate operation     │  │
│  │ 2. Assign sequence number │  │
│  │ 3. Write to SQLite        │  │
│  │ 4. Store object (if new)  │  │
│  │ 5. Update entity state    │  │
│  │ 6. Notify subscribers     │  │
│  └───────────────────────────┘  │
└─────────────────────────────────┘
  │
  ▼
Subscribers:
  ├─ AutoCheckpoint evaluator
  ├─ Sync client (if connected)
  └─ Watch event stream (for UI)
```

## OpWriter

The OpWriter is a single-goroutine writer that serializes all writes to the operation log.

```go
type OpWriter struct {
    db       *sql.DB
    store    *ObjectStore
    seqMu    sync.Mutex
    seq      int64
    subs     []chan Operation
}

func NewOpWriter(db *sql.DB, store *ObjectStore) *OpWriter {
    // Load current sequence counter from metadata
    var seq int64
    db.QueryRow("SELECT CAST(value AS INTEGER) FROM metadata WHERE key = 'seq_counter'").Scan(&seq)

    return &OpWriter{
        db:    db,
        store: store,
        seq:   seq,
    }
}

func (w *OpWriter) Write(op Operation) (Operation, error) {
    w.seqMu.Lock()
    defer w.seqMu.Unlock()

    // Assign sequence number
    w.seq++
    op.Seq = w.seq
    op.ID = ulid.Make().String()

    // Write in a transaction
    tx, err := w.db.Begin()
    if err != nil {
        return op, err
    }
    defer tx.Rollback()

    // Insert operation
    _, err = tx.Exec(`
        INSERT INTO operations (id, seq, stream_id, space_id, entity_id, type, path, delta, object_ref, parent_seq, author, timestamp, meta)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        op.ID, op.Seq, op.StreamID, op.SpaceID, op.EntityID,
        op.Type, op.Path, op.Delta, op.ObjectRef, op.ParentSeq,
        op.Author, op.Timestamp, marshalJSON(op.Meta),
    )
    if err != nil {
        return op, err
    }

    // Update sequence counter
    tx.Exec("UPDATE metadata SET value = ? WHERE key = 'seq_counter'", w.seq)

    // Update stream head
    tx.Exec("UPDATE streams SET head_seq = ?, updated_at = ? WHERE id = ?",
        op.Seq, op.Timestamp, op.StreamID)

    // Upsert entity state
    tx.Exec(`
        INSERT INTO entities (id, space_id, path, kind, object_ref, size, mod_time, status, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, 'active', datetime('now'))
        ON CONFLICT(id, space_id) DO UPDATE SET
            path = excluded.path,
            object_ref = excluded.object_ref,
            size = excluded.size,
            mod_time = excluded.mod_time,
            status = CASE WHEN excluded.status = 'deleted' THEN 'deleted' ELSE 'active' END,
            updated_at = datetime('now')`,
        op.EntityID, op.SpaceID, op.Path, op.Meta.ContentType,
        op.ObjectRef, op.Meta.Size, op.Timestamp)

    if err := tx.Commit(); err != nil {
        return op, err
    }

    // Notify subscribers
    for _, sub := range w.subs {
        select {
        case sub <- op:
        default: // Don't block if subscriber is slow
        }
    }

    return op, nil
}
```

## OpReader

Reads operations from the log with various query patterns.

```go
type OpReader struct {
    db *sql.DB
}

// ReadRange returns ops between two sequence numbers (exclusive start, inclusive end)
func (r *OpReader) ReadRange(fromSeq, toSeq int64) ([]Operation, error) {
    rows, err := r.db.Query(
        "SELECT * FROM operations WHERE seq > ? AND seq <= ? ORDER BY seq ASC",
        fromSeq, toSeq,
    )
    // ... scan rows into []Operation
}

// ReadByEntity returns all ops for a specific entity
func (r *OpReader) ReadByEntity(entityID string) ([]Operation, error) {
    rows, err := r.db.Query(
        "SELECT * FROM operations WHERE entity_id = ? ORDER BY seq ASC",
        entityID,
    )
    // ...
}

// ReadBySpace returns ops for a space within a sequence range
func (r *OpReader) ReadBySpace(spaceID string, fromSeq, toSeq int64) ([]Operation, error) {
    rows, err := r.db.Query(
        "SELECT * FROM operations WHERE space_id = ? AND seq > ? AND seq <= ? ORDER BY seq ASC",
        spaceID, fromSeq, toSeq,
    )
    // ...
}

// ReadSince returns ops since a timestamp
func (r *OpReader) ReadSince(since string) ([]Operation, error) {
    rows, err := r.db.Query(
        "SELECT * FROM operations WHERE timestamp >= ? ORDER BY seq ASC",
        since,
    )
    // ...
}

// Head returns the latest sequence number
func (r *OpReader) Head() (int64, error) {
    var seq int64
    err := r.db.QueryRow("SELECT CAST(value AS INTEGER) FROM metadata WHERE key = 'seq_counter'").Scan(&seq)
    return seq, err
}
```

## Sequence Number Assignment

Sequence numbers are the backbone of ordering. Rules:

1. **Monotonically increasing** — every new op gets seq = previous + 1
2. **Per-project, not per-stream** — all streams share one sequence space
3. **Never reused** — even after compaction, old numbers aren't recycled
4. **Assigned by the writer** — the writer holds a mutex on the counter
5. **Atomic** — sequence assignment + op write happen in one transaction

Why not per-stream sequences? Cross-stream queries (like "all changes since yesterday") need a global ordering. Per-stream sequences would require a merge step.

## Operation Batching

Multiple related changes can be written as a batch:

```go
func (w *OpWriter) WriteBatch(ops []Operation) error {
    w.seqMu.Lock()
    defer w.seqMu.Unlock()

    tx, _ := w.db.Begin()
    defer tx.Rollback()

    for i := range ops {
        w.seq++
        ops[i].Seq = w.seq
        ops[i].ID = ulid.Make().String()
        // Insert each op...
    }

    tx.Exec("UPDATE metadata SET value = ? WHERE key = 'seq_counter'", w.seq)
    return tx.Commit()
}
```

Batching is used by:
- File watcher (debounced changes produce multiple ops)
- Sync pull (received ops written as a batch)
- Restore (restore operations written atomically)

## Compaction

Over time, the operation log grows. Compaction reduces size by merging old ops:

### Strategy

```
Before compaction:
  seq 1: create  file.go  (content: v1)
  seq 2: modify  file.go  (delta: v1→v2)
  seq 3: modify  file.go  (delta: v2→v3)
  seq 4: modify  file.go  (delta: v3→v4)

After compaction (keep last N ops per entity):
  seq 1: create  file.go  (content: v3) [rewritten]
  seq 4: modify  file.go  (delta: v3→v4) [kept]
```

### Rules

1. Never compact ops newer than the oldest active checkpoint
2. Never compact ops that haven't been synced to all remotes
3. Compact at most to the last checkpoint per entity
4. Run in the background, never block writes
5. Preserve all checkpoint boundaries

### When

- Manually: `loom compact`
- Automatically: when the database exceeds a size threshold (configurable)
- On push: optionally compact after successful sync

## Replay

State at any point is derived by replaying ops from the beginning (or from the nearest checkpoint):

```go
func (r *OpReader) ReplayTo(seq int64, space string) (map[string]EntityState, error) {
    // Find the nearest checkpoint before this sequence
    var checkpointSeq int64
    var checkpointSpaces string
    r.db.QueryRow(
        "SELECT seq, spaces FROM checkpoints WHERE seq <= ? ORDER BY seq DESC LIMIT 1",
        seq,
    ).Scan(&checkpointSeq, &checkpointSpaces)

    // Start from checkpoint state (or empty if no checkpoint)
    state := parseCheckpointSpaces(checkpointSpaces, space)

    // Apply ops from checkpoint to target
    ops, _ := r.ReadBySpace(space, checkpointSeq, seq)
    for _, op := range ops {
        state = applyOp(state, op)
    }

    return state, nil
}
```

## Subscriptions

Components that need to react to new operations subscribe via channels:

```go
func (w *OpWriter) Subscribe() <-chan Operation {
    ch := make(chan Operation, 100)
    w.subs = append(w.subs, ch)
    return ch
}
```

Subscribers:
- **AutoCheckpoint** — evaluates whether to create a checkpoint after each op
- **SyncClient** — queues new ops for push (if connected to remote)
- **WatchServer** — streams events to UI or agent API via SSE/WebSocket
