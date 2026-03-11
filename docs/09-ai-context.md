# 09 — AI Context Guide

## Purpose

This document helps AI coding assistants work effectively on the Loom codebase. Read this alongside the project `CLAUDE.md` for rules and conventions.

## Architecture Reminders

### The Golden Rule

> **Operations are append-only. State is derived, never stored directly.**

If you're about to store state in a separate table or file — stop. The operation log IS the state. Derive what you need by replaying ops or querying the log.

### Data Flow

```
File Change → Space Adapter → Operation → OpWriter → SQLite
                                                      ↓
                                            Subscribers:
                                              AutoCheckpoint
                                              SyncClient
                                              AgentEventStream
```

Never bypass the OpWriter. All changes flow through it for consistent sequencing.

### Layer Boundaries

```
CLI / Agent API (user-facing)
     ↓ calls
Core Engine (operation log, checkpoints, streams, diff, merge)
     ↓ uses
Storage (SQLite + Object Store)
     ↓ stores
Disk (.loom/)
```

Rules:
- CLI never touches SQLite directly — always goes through core engine
- Core engine never touches the filesystem directly — always goes through storage
- Space adapters read the project filesystem but write operations through the engine

## Project Structure

```
cmd/loom/main.go              CLI entry point
cmd/loom-server/main.go       Server entry point
internal/core/                 Core engine (the heart)
internal/storage/              SQLite + Object Store
internal/adapter/              Space adapters
internal/diff/                 Diff engine
internal/merge/                Merge engine
internal/sync/                 Client/server sync
internal/server/               HTTP server
internal/agent/                Agent API
internal/watch/                File watcher
internal/cli/                  CLI commands (cobra)
pkg/loom/                      Public Go SDK
```

## Common Patterns

### Adding a New CLI Command

1. Create `internal/cli/new_command.go`
2. Register in `internal/cli/root.go` with `rootCmd.AddCommand(newCmd)`
3. Use cobra conventions: `RunE` not `Run`, return errors
4. All output through `fmt.Fprintf(cmd.OutOrStdout(), ...)`
5. Write tests in `internal/cli/new_command_test.go`

```go
var newCmd = &cobra.Command{
    Use:   "new-command [args]",
    Short: "One-line description",
    Long:  "Longer description if needed",
    RunE: func(cmd *cobra.Command, args []string) error {
        vault, err := core.OpenVault(".")
        if err != nil {
            return err
        }
        // ... implementation
        return nil
    },
}
```

### Adding a New Space Adapter

1. Create `internal/adapter/myspace/myspace.go`
2. Implement the `SpaceAdapter` interface fully
3. Register in `internal/adapter/registry.go`
4. Add auto-detection logic in `Detect()`
5. Write tests in `internal/adapter/myspace/myspace_test.go`

```go
type MySpaceAdapter struct {
    config SpaceConfig
}

func (a *MySpaceAdapter) ID() string   { return "myspace" }
func (a *MySpaceAdapter) Name() string { return "My Space" }
// ... implement all SpaceAdapter methods
```

### Adding a New Operation Type

1. Add the constant to `OpType` in `internal/core/operation.go`
2. Handle it in `OpWriter.Write()` if it needs special logic
3. Handle it in each space adapter's `NormalizeChange()`
4. Handle it in the diff engine
5. Update tests

### Working with SQLite

Always use transactions for multi-statement writes:

```go
tx, err := db.Begin()
if err != nil {
    return err
}
defer tx.Rollback()

// ... do work ...

return tx.Commit()
```

Use prepared statements for repeated queries:

```go
stmt, _ := db.Prepare("SELECT * FROM operations WHERE entity_id = ? AND seq > ?")
defer stmt.Close()
```

Never use string concatenation for SQL — always use `?` placeholders.

### Error Handling

Wrap errors with context:

```go
if err != nil {
    return fmt.Errorf("write operation %s: %w", op.ID, err)
}
```

Use `errors.Is()` and `errors.As()` for checking:

```go
if errors.Is(err, ErrNotFound) {
    // handle not found
}
```

Define sentinel errors in the package that owns the concept:

```go
var (
    ErrNotFound    = errors.New("not found")
    ErrNotInit     = errors.New("not a loom project (run 'loom init')")
    ErrLocked      = errors.New("project is locked by another process")
)
```

## Testing Patterns

### Unit Test Template

```go
func TestOpWriter_Write(t *testing.T) {
    // Setup
    db := testutil.NewTestDB(t)
    store := testutil.NewTestObjectStore(t)
    writer := core.NewOpWriter(db, store)

    // Create test stream
    testutil.CreateStream(t, db, "main")

    // Execute
    op := core.Operation{
        StreamID: "main-id",
        SpaceID:  "code",
        EntityID: "src/main.go",
        Type:     core.OpCreate,
        Path:     "src/main.go",
        Author:   "test",
    }

    result, err := writer.Write(op)

    // Assert
    assert.NoError(t, err)
    assert.Equal(t, int64(1), result.Seq)
    assert.NotEmpty(t, result.ID)
}
```

### Test Helpers

Create helpers in `internal/testutil/`:

```go
// NewTestDB creates a temporary SQLite database with schema
func NewTestDB(t *testing.T) *sql.DB {
    t.Helper()
    dir := t.TempDir()
    db, err := storage.InitDB(filepath.Join(dir, "test.db"))
    require.NoError(t, err)
    t.Cleanup(func() { db.Close() })
    return db
}

// NewTestObjectStore creates a temporary object store
func NewTestObjectStore(t *testing.T) *storage.ObjectStore {
    t.Helper()
    return storage.NewObjectStore(filepath.Join(t.TempDir(), "objects"))
}

// NewTestVault creates a full temporary vault
func NewTestVault(t *testing.T) *core.Vault {
    t.Helper()
    dir := t.TempDir()
    vault, err := core.InitVault(dir)
    require.NoError(t, err)
    t.Cleanup(func() { vault.Close() })
    return vault
}
```

### Integration Test Template

```go
func TestInitAndCheckpoint(t *testing.T) {
    // Create a temp project directory
    dir := t.TempDir()

    // Add some files
    os.MkdirAll(filepath.Join(dir, "docs"), 0755)
    os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
    os.WriteFile(filepath.Join(dir, "docs/readme.md"), []byte("# Hello"), 0644)

    // Init loom
    vault, err := core.InitVault(dir)
    require.NoError(t, err)

    // Should detect code and docs spaces
    spaces := vault.Spaces()
    assert.Contains(t, spaceIDs(spaces), "code")
    assert.Contains(t, spaceIDs(spaces), "docs")

    // Create checkpoint
    cp, err := vault.CheckpointEngine().Create(core.CheckpointInput{
        StreamID: vault.ActiveStream().ID,
        Title:    "initial",
        Source:   core.SourceManual,
        Author:   "test",
    })
    require.NoError(t, err)
    assert.Equal(t, "initial", cp.Title)

    // Log should show it
    cps, _ := vault.CheckpointEngine().List(vault.ActiveStream().ID, 10)
    assert.Len(t, cps, 1)
}
```

## File Naming

| Item | Pattern | Example |
|------|---------|---------|
| Core component | `internal/core/foo.go` | `operation.go` |
| Core test | `internal/core/foo_test.go` | `operation_test.go` |
| Adapter | `internal/adapter/space/space.go` | `adapter/code/code.go` |
| CLI command | `internal/cli/foo.go` | `cli/checkpoint.go` |
| Storage | `internal/storage/foo.go` | `storage/sqlite.go` |
| SDK | `pkg/loom/foo.go` | `pkg/loom/client.go` |
| Test helper | `internal/testutil/foo.go` | `testutil/db.go` |
| Integration test | `test/integration/foo_test.go` | `test/integration/init_test.go` |

## Go Conventions

| Convention | Enforcement |
|-----------|------------|
| `gofmt` on all files | CI check |
| No global mutable state | Code review |
| Errors wrapped with `fmt.Errorf("context: %w", err)` | Linter |
| No `panic` except in init-time programmer errors | Code review |
| Interfaces in the consumer package, not the provider | Convention |
| Context propagation via `context.Context` first param | Convention |
| Table-driven tests for exhaustive coverage | Convention |

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Storing state outside the operation log | Derive it by replaying ops |
| Writing to SQLite without a transaction | Always use `tx` for multi-write |
| Using `string` for hashes in comparisons | Use the `Hash` type alias |
| Forgetting to update stream head after writing ops | OpWriter handles this |
| Reading files without going through the adapter | Use `adapter.ReadEntity()` |
| Hardcoding `.loom` path | Use `vault.LoomDir()` |
| Not closing database in tests | Use `t.Cleanup()` |
| Using `time.Now()` in core logic | Pass time as parameter |
| Putting business logic in CLI handlers | CLI handlers call core engine |
| Using `log` package | Use `slog` for structured logging |

## Key Files Quick Reference

| File | Purpose |
|------|---------|
| `internal/core/operation.go` | Operation types and creation |
| `internal/core/oplog.go` | Operation log (write, read, query) |
| `internal/core/checkpoint.go` | Checkpoint creation and management |
| `internal/core/stream.go` | Stream lifecycle |
| `internal/core/vault.go` | Vault initialization and discovery |
| `internal/storage/sqlite.go` | Database connection and setup |
| `internal/storage/schema.go` | SQLite schema and migrations |
| `internal/storage/objectstore.go` | Content-addressed blob store |
| `internal/adapter/adapter.go` | SpaceAdapter interface |
| `internal/adapter/registry.go` | Adapter registration |
| `internal/diff/engine.go` | Diff orchestration |
| `internal/merge/engine.go` | Merge orchestration |
| `internal/cli/root.go` | CLI root command and setup |
| `pkg/loom/client.go` | Public Go SDK |

## Cross-References

- Architecture: `02-technical-architecture.md`
- Data models: `04-data-models.md`
- Storage schema: `05-storage-schema.md`
- Operation log details: `06-systems/operation-log.md`
- Streams: `06-systems/streams.md`
- Checkpoints: `06-systems/checkpoints.md`
- Adapters: `06-systems/adapters.md`
- Diff: `06-systems/diff.md`
- Merge: `06-systems/merge.md`
- Sync: `06-systems/sync.md`
- Agent API: `07-agent-api.md`
- Roadmap: `08-development-roadmap.md`
- Testing: `10-testing-strategy.md`
