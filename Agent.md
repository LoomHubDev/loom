# Loom — Agent Development Guide

> This file helps AI coding agents work on the Loom codebase effectively.

## Golden Rule

**Operations are append-only. State is derived, never stored directly.**

If you're about to store state in a separate table — stop. The operation log IS the state. Derive what you need by replaying ops or querying the log.

## Layer Boundaries

```
CLI (internal/cli/)           ← user-facing commands
  ↓ calls
Core Engine (internal/core/)  ← operations, streams, checkpoints, remotes
  ↓ uses
Storage (internal/storage/)   ← SQLite + object store
  ↓ stores
Disk (.loom/)                 ← database + objects
```

Rules:
- CLI never touches SQLite directly — always goes through core engine
- Core engine never touches the filesystem directly — goes through storage
- All changes flow through `OpWriter` for consistent sequencing

## How to Add a CLI Command

1. Create `internal/cli/mycommand.go`
2. Define a function `func newMyCommandCmd() *cobra.Command { ... }`
3. Use `cobra.Command` with `RunE` (not `Run`) — return errors
4. Register in `internal/cli/root.go`: `rootCmd.AddCommand(newMyCommandCmd())`
5. Open the vault with `core.OpenVault(projectDir)` and defer `Close()`

```go
func newMyCommandCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "mycommand",
        Short: "What it does",
        RunE: func(cmd *cobra.Command, args []string) error {
            v, err := core.OpenVault(projectDir)
            if err != nil {
                return err
            }
            defer v.Close()
            // ... use v.OpWriter, v.OpReader, v.Streams, v.Store, etc.
            return nil
        },
    }
}
```

## Key Types

```go
// internal/core/types.go
type Operation struct {
    ID, StreamID, SpaceID, EntityID string
    Seq       int64
    Type      OpType  // "create", "modify", "delete", "move", "rename"
    Path      string
    Delta     []byte
    ObjectRef string  // hash of content in object store
    ParentSeq int64
    Author    string
    Timestamp string
    Meta      OpMeta
}

type Stream struct {
    ID, Name, Status string
    HeadSeq          int64
}

type Checkpoint struct {
    ID, StreamID, Title, Summary, Author string
    Seq       int64
    Source    CheckpointSource  // "manual", "auto", "agent"
    Spaces   []SpaceState
    Tags     map[string]string
}
```

## Key Interfaces

```go
// internal/core/vault.go — the main entry point
type Vault struct {
    ProjectPath string
    LoomPath    string
    Config      *ProjectConfig
    DB          *sql.DB
    Store       *storage.ObjectStore
    Streams     *StreamManager
    OpWriter    *OpWriter
    OpReader    *OpReader
    Checkpoints *CheckpointEngine
}

// internal/core/oplog.go
OpWriter.Write(op Operation) (Operation, error)       // single op
OpWriter.WriteBatch(ops []Operation) ([]Operation, error) // atomic batch
OpReader.Head() (int64, error)                         // current seq
OpReader.ReadRange(from, to int64) ([]Operation, error)
OpReader.ReadByStream(streamID string, from, to int64) ([]Operation, error)

// internal/storage/objectstore.go
Store.Write(content []byte, contentType string) (hash string, error)
Store.Read(hash string) ([]byte, error)
Store.Exists(hash string) bool
```

## Object Hashing

```go
// internal/storage/hash.go
// Format: sha256("blob:" + len + "\0" + content)
func HashContent(content []byte) string {
    h := sha256.New()
    h.Write([]byte("blob:"))
    h.Write([]byte(strconv.Itoa(len(content))))
    h.Write([]byte{0})
    h.Write(content)
    return hex.EncodeToString(h.Sum(nil))
}
```

This is NOT plain SHA-256. Always use `storage.HashContent()`.

## Database Schema (key tables)

- `operations` — append-only operation log (id, seq, stream_id, space_id, entity_id, type, path, object_ref, author, timestamp, meta)
- `streams` — stream definitions (id, name, head_seq, status)
- `entities` — derived entity state (id, space_id, path, object_ref, size, status)
- `checkpoints` — named points on streams
- `objects` — object metadata index (hash, size, compressed)
- `remotes` — hub remote configs (name, url, push_seq, pull_seq)
- `metadata` — key-value store (seq_counter, schema_version, auth tokens)

## Testing

Run all tests:
```bash
go test ./...
```

Test helpers are in `internal/testutil/`. Use `t.TempDir()` for test directories.
Tests use real SQLite databases, not mocks.

## Common Mistakes to Avoid

- Don't store state outside the operation log — derive it
- Don't bypass `OpWriter` — it handles sequencing and entity state
- Don't use plain SHA-256 — use `storage.HashContent()`
- Don't use string concatenation in SQL — use `?` placeholders
- Don't forget to close the vault (`defer v.Close()`)
- Don't use Git terminology in user-facing strings — use Loom vocabulary (send not push, receive not pull, stream not branch, checkpoint not commit)
