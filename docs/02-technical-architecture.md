# 02 — Technical Architecture

## Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│                      CLI / Agent API                      │
│  (loom init, loom checkpoint, loom diff, loom push, ...)  │
├───────────────────────┬──────────────────────────────────┤
│   Space Adapters      │        Agent SDK                  │
│  ┌──────────────────┐ │  ┌────────────────────────────┐  │
│  │ code (git-aware) │ │  │ HTTP/gRPC API for agents   │  │
│  │ docs (filesystem)│ │  │ checkpoint, rollback, diff  │  │
│  │ design (struct)  │ │  │ explain, query, restore     │  │
│  │ data (schema)    │ │  └────────────────────────────┘  │
│  │ config (kv)      │ │                                  │
│  │ notes (text)     │ │                                  │
│  └──────────────────┘ │                                  │
├───────────────────────┴──────────────────────────────────┤
│                     Core Engine                           │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Operation Log (append-only, ordered)                │ │
│  │  ├─ OperationWriter (write ops, assign sequence)    │ │
│  │  ├─ OperationReader (replay, query, filter)         │ │
│  │  └─ Compaction (background, merge old ops)          │ │
│  ├─────────────────────────────────────────────────────┤ │
│  │ Stream Manager                                      │ │
│  │  ├─ StreamRegistry (create, fork, list, delete)     │ │
│  │  ├─ StreamHead (track current position per stream)  │ │
│  │  └─ StreamMerge (converge streams)                  │ │
│  ├─────────────────────────────────────────────────────┤ │
│  │ Checkpoint Engine                                   │ │
│  │  ├─ AutoCheckpoint (debounced, on-change)           │ │
│  │  ├─ ManualCheckpoint (named, user-created)          │ │
│  │  └─ CheckpointIndex (lookup, search, filter)        │ │
│  ├─────────────────────────────────────────────────────┤ │
│  │ Diff Engine                                         │ │
│  │  ├─ TextDiff (line + word level)                    │ │
│  │  ├─ StructuredDiff (JSON, YAML, TOML)              │ │
│  │  ├─ BinaryDiff (fingerprint + metadata)             │ │
│  │  └─ SemanticDiff (adapter-delegated)                │ │
│  ├─────────────────────────────────────────────────────┤ │
│  │ Merge Engine                                        │ │
│  │  ├─ AutoMerge (non-conflicting changes)             │ │
│  │  ├─ LLMMerge (semantic conflict resolution)         │ │
│  │  └─ MergePolicy (strategy per space/entity type)    │ │
│  └─────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────┤
│                       Storage                             │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ SQLite (embedded)                                   │ │
│  │  ├─ operations table (the log)                      │ │
│  │  ├─ checkpoints table                               │ │
│  │  ├─ streams table                                   │ │
│  │  ├─ entities table (tracked items)                  │ │
│  │  └─ metadata table (config, refs)                   │ │
│  ├─────────────────────────────────────────────────────┤ │
│  │ Object Store (content-addressed)                    │ │
│  │  ├─ Blobs (file content, design data, binary)       │ │
│  │  ├─ SHA-256 addressing                              │ │
│  │  └─ Compression (zstd for large objects)            │ │
│  └─────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────┤
│                    Sync Protocol                          │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Client                                              │ │
│  │  ├─ Push (send ops + objects to remote)              │ │
│  │  ├─ Pull (receive ops + objects from remote)         │ │
│  │  └─ Negotiate (find common ancestor, delta sync)    │ │
│  ├─────────────────────────────────────────────────────┤ │
│  │ Server                                              │ │
│  │  ├─ HTTP API (receive push, serve pull)              │ │
│  │  ├─ Project Registry (multi-project hosting)         │ │
│  │  ├─ Auth (tokens, permissions)                      │ │
│  │  └─ Storage Backend (SQLite or Postgres)             │ │
│  └─────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
```

## Data Flow

### Writing (Auto-Versioning)

```
File Change Detected (fsnotify)
  │
  ▼
Space Adapter normalizes the change
  │
  ▼
Operation created:
  { space: "code", entity: "src/main.go", type: "modify", delta: [...] }
  │
  ▼
OperationWriter assigns sequence number, writes to SQLite
  │
  ▼
Content stored in Object Store (if new/modified blob)
  │
  ▼
AutoCheckpoint evaluates:
  - Enough ops since last checkpoint?
  - Significant change detected?
  - Time threshold reached?
  │
  ▼ (if yes)
Checkpoint created with snapshot reference
```

### Reading (Diff/Log/Restore)

```
User: loom diff --space code checkpoint-a checkpoint-b
  │
  ▼
CheckpointIndex resolves both checkpoint IDs
  │
  ▼
OperationReader retrieves ops between checkpoints
  │
  ▼
Diff Engine groups ops by entity
  │
  ▼
Space Adapter renders diff per entity type:
  - code: line-level text diff
  - design: structural node diff
  - data: JSON patch
  │
  ▼
Formatted output to terminal or structured JSON for agents
```

### Syncing (Push/Pull)

```
loom push
  │
  ▼
Client reads local stream head
  │
  ▼
Client sends "negotiate" request to server:
  { stream: "main", localHead: "seq-1234" }
  │
  ▼
Server responds with common ancestor:
  { commonAncestor: "seq-1100", serverHead: "seq-1180" }
  │
  ▼
Client sends ops seq-1100 to seq-1234 + referenced objects
  │
  ▼
Server applies ops (no conflicts — append-only log)
  │
  ▼
Server responds with ops seq-1100 to seq-1180 (server-only ops)
  │
  ▼
Client applies server ops (merge via auto-convergence)
  │
  ▼
Both sides now have ops seq-1000 to seq-1234 + seq-1100 to seq-1180
Stream heads updated
```

## Directory Structure (.loom vault)

```
project/
  .loom/
    loom.db                     # SQLite database (ops, checkpoints, streams, entities)
    config.toml                 # Project config (spaces, remotes, author)
    objects/                    # Content-addressed object store
      ab/
        abcdef0123456789...     # SHA-256 addressed blobs
    locks/                      # File locks for concurrent access
      writer.lock
    hooks/                      # Pre/post hooks
      pre-checkpoint
      post-checkpoint
      pre-push
      post-pull
```

### Why SQLite for the Operation Log

The operation log is the heart of Loom. It needs:
- Fast sequential writes (append-only)
- Fast range reads (replay ops between two points)
- Indexed queries (by space, entity, author, time)
- Transactional consistency (atomic multi-op writes)
- Embedded (no external database server)

SQLite handles all of these. It's the most deployed database in the world, battle-tested, and Go has excellent drivers (`modernc.org/sqlite` — pure Go, no CGo).

### Why Separate Object Store

Binary content (images, design files, large datasets) shouldn't live in SQLite. The object store:
- Stores content by SHA-256 hash (deduplication)
- Supports compression (zstd)
- Files are immutable — write once, never modify
- Can be garbage-collected when no ops reference them

## Technology Stack

| Component | Technology | Why |
|-----------|-----------|-----|
| Language | Go 1.25 | Team expertise, fast compile, single binary, great concurrency |
| Database | SQLite via `modernc.org/sqlite` | Embedded, pure Go, no CGo dependency |
| CLI Framework | `cobra` | Industry standard for Go CLIs |
| File Watching | `fsnotify` | Cross-platform file system notifications |
| Hashing | `crypto/sha256` (stdlib) | Content addressing |
| Compression | `github.com/klauspost/compress/zstd` | Fast compression for blobs |
| HTTP Server | `net/http` + `chi` router | Lightweight, stdlib-compatible |
| Logging | `log/slog` (stdlib) | Structured logging, Go 1.21+ |
| Testing | `testing` (stdlib) + `testify` | Standard Go testing |
| Config | `toml` | Human-readable, better than YAML for config |

## Module Layout

```
loom/
  cmd/
    loom/                       # CLI binary entry point
      main.go
    loom-server/                # Server binary entry point
      main.go
  internal/
    core/
      operation.go              # Operation types and creation
      oplog.go                  # Operation log (write, read, query)
      checkpoint.go             # Checkpoint creation and indexing
      stream.go                 # Stream management
      entity.go                 # Entity tracking and state
    storage/
      sqlite.go                 # SQLite connection and migrations
      schema.go                 # Database schema (SQL)
      objectstore.go            # Content-addressed blob storage
      hash.go                   # SHA-256 hashing utilities
    adapter/
      adapter.go                # SpaceAdapter interface
      registry.go               # Adapter registration and lookup
      code/
        code.go                 # Code space adapter (Git-aware)
        git.go                  # Git integration (read HEAD, diff, status)
      docs/
        docs.go                 # Docs space adapter (filesystem)
      design/
        design.go               # Design space adapter (structured)
      data/
        data.go                 # Data space adapter (JSON/YAML schemas)
      config/
        config.go               # Config space adapter
      notes/
        notes.go                # Notes space adapter
    diff/
      engine.go                 # Diff orchestration
      text.go                   # Line/word text diff (Myers algorithm)
      structured.go             # Structured diff (JSON patch)
      binary.go                 # Binary fingerprint diff
      formatter.go              # Output formatting (terminal, JSON)
    merge/
      engine.go                 # Merge orchestration
      auto.go                   # Automatic non-conflicting merge
      llm.go                    # LLM-assisted semantic merge
      policy.go                 # Merge strategy per space/type
    sync/
      client.go                 # Sync client (push/pull)
      protocol.go               # Wire protocol types
      negotiate.go              # Ancestry negotiation
    server/
      server.go                 # HTTP server
      handlers.go               # API route handlers
      auth.go                   # Token-based auth
      store.go                  # Server-side storage
    agent/
      api.go                    # Agent-facing API (structured)
      handlers.go               # Agent command handlers
      schema.go                 # API schema for LLM tool use
    watch/
      watcher.go                # File system watcher
      debounce.go               # Change debouncing
      filter.go                 # Ignore patterns (.loomignore)
    cli/
      root.go                   # Root cobra command
      init.go                   # loom init
      status.go                 # loom status
      log.go                    # loom log
      diff.go                   # loom diff
      checkpoint.go             # loom checkpoint
      restore.go                # loom restore
      stream.go                 # loom stream
      push.go                   # loom push
      pull.go                   # loom pull
      remote.go                 # loom remote
      space.go                  # loom space
      watch.go                  # loom watch (start auto-versioning daemon)
      agent.go                  # loom agent (start agent API server)
  pkg/
    loom/
      client.go                 # Public Go SDK for embedding Loom
      types.go                  # Public types
  test/
    integration/
      init_test.go
      checkpoint_test.go
      stream_test.go
      sync_test.go
      agent_test.go
    fixtures/
      sample-project/
```

## Concurrency Model

### File Watcher (Auto-Versioning Daemon)

```
Main Process
  ├─ Watcher goroutine (fsnotify events)
  │    └─ Debouncer (coalesce rapid changes, 500ms window)
  │         └─ Adapter goroutine pool (normalize changes in parallel)
  │              └─ OpWriter (serial, holds write lock on SQLite)
  ├─ AutoCheckpoint goroutine (evaluates checkpoint criteria)
  └─ CLI handler (accepts user commands concurrently)
```

### Write Safety

SQLite in WAL mode allows concurrent reads during writes. A single writer goroutine serializes all operation writes. This prevents write contention and ensures strict ordering.

```go
// The OpWriter channel serializes writes
opChan := make(chan Operation, 1000)

go func() {
    for op := range opChan {
        db.WriteOperation(op) // Serial writes
    }
}()
```

### Agent API Concurrency

The agent API server handles requests concurrently. Read operations (diff, log, query) can run in parallel. Write operations (checkpoint, rollback) are serialized through the same OpWriter channel.

## Performance Targets

| Metric | Target | Notes |
|--------|--------|-------|
| Operation write | < 1ms | Single SQLite insert in WAL mode |
| Checkpoint creation | < 50ms | Snapshot + index update |
| Diff (two checkpoints) | < 200ms | For typical project (~1000 ops between) |
| Log query | < 50ms | Indexed SQLite query |
| Restore | < 2s | Full project restore from checkpoint |
| Push (1000 ops) | < 5s | Network-dependent |
| Pull (1000 ops) | < 5s | Network-dependent |
| File watch latency | < 1s | Time from file save to operation logged |
| Binary size | < 20MB | Single Go binary |
| Memory (daemon) | < 50MB | File watcher + debouncer |

## Security Model

### Local

- `.loom/loom.db` has the same permissions as the project directory
- No secrets stored in the database (auth tokens in OS keychain or config)
- Object store files are read-only after creation

### Remote

- Token-based authentication (JWT or API key)
- TLS required for all remote communication
- Operations are signed with author identity (future: cryptographic signing)
- Server validates operation sequence integrity on receive

## Extensibility Points

1. **Space Adapters** — Implement the `SpaceAdapter` interface to add new content types
2. **Merge Policies** — Register custom merge strategies per space or entity type
3. **Hooks** — Shell scripts executed pre/post checkpoint, push, pull
4. **Agent Tools** — Extend the agent API with custom tools via the plugin interface
5. **Storage Backends** — Server can use SQLite (default) or Postgres for large deployments
