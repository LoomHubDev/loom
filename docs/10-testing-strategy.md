# 10 — Testing Strategy

## Philosophy

- Test behavior, not implementation
- Every exported function has tests
- Integration tests for user-facing workflows
- Table-driven tests for exhaustive edge cases
- No mocks for SQLite — use real temporary databases
- Tests must be fast (entire suite < 30 seconds)

## Test Structure

```
internal/
  core/
    operation_test.go           # Unit tests for operation types
    oplog_test.go               # Unit tests for op reader/writer
    checkpoint_test.go          # Unit tests for checkpoint engine
    stream_test.go              # Unit tests for stream manager
    vault_test.go               # Unit tests for vault init/open
  storage/
    sqlite_test.go              # Database init, migrations, pragmas
    objectstore_test.go         # Blob write/read/dedup/gc
    schema_test.go              # Schema validation
  adapter/
    code/
      code_test.go              # Code adapter detection, normalization
      git_test.go               # Git integration
    docs/
      docs_test.go              # Docs adapter
    design/
      design_test.go            # Design adapter
  diff/
    engine_test.go              # Diff orchestration
    text_test.go                # Myers text diff
    structured_test.go          # JSON patch diff
  merge/
    engine_test.go              # Merge orchestration
    threeway_test.go            # Three-way merge algorithm
  sync/
    client_test.go              # Sync client
    protocol_test.go            # Wire protocol serialization
  server/
    handlers_test.go            # Server API handlers
  agent/
    api_test.go                 # Agent API handlers
  cli/
    init_test.go                # CLI command tests
    checkpoint_test.go
    log_test.go
    diff_test.go
    restore_test.go
  testutil/
    db.go                       # Test database helper
    vault.go                    # Test vault helper
    fixtures.go                 # Test fixture management
test/
  integration/
    init_checkpoint_test.go     # Init → add → checkpoint → log
    watch_auto_test.go          # Watch → modify files → auto checkpoint
    diff_restore_test.go        # Checkpoint → modify → diff → restore
    stream_merge_test.go        # Fork → modify → merge
    sync_test.go                # Push → pull round-trip
    agent_test.go               # Agent API workflow
  fixtures/
    sample-project/             # A minimal project for testing
      main.go
      docs/
        readme.md
      design/
        mockup.json
```

## Unit Test Patterns

### Table-Driven Tests

```go
func TestHashContent(t *testing.T) {
    tests := []struct {
        name    string
        content []byte
        want    string
    }{
        {
            name:    "empty content",
            content: []byte{},
            want:    "e3b0c44298fc1c14...", // Known SHA-256 of "blob:0\x00"
        },
        {
            name:    "simple text",
            content: []byte("hello world"),
            want:    "...", // Precomputed
        },
        {
            name:    "binary content",
            content: []byte{0x00, 0xFF, 0x42},
            want:    "...",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := storage.HashContent(tt.content)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

### Operation Log Tests

```go
func TestOpWriter_SequenceIncrement(t *testing.T) {
    db := testutil.NewTestDB(t)
    store := testutil.NewTestObjectStore(t)
    writer := core.NewOpWriter(db, store)
    stream := testutil.CreateStream(t, db, "main")

    // Write 3 operations
    for i := 0; i < 3; i++ {
        op := testutil.MakeOp(stream.ID, "code", fmt.Sprintf("file%d.go", i))
        result, err := writer.Write(op)
        assert.NoError(t, err)
        assert.Equal(t, int64(i+1), result.Seq)
    }

    // Verify head
    reader := core.NewOpReader(db)
    head, _ := reader.Head()
    assert.Equal(t, int64(3), head)
}

func TestOpWriter_BatchWrite(t *testing.T) {
    db := testutil.NewTestDB(t)
    store := testutil.NewTestObjectStore(t)
    writer := core.NewOpWriter(db, store)
    stream := testutil.CreateStream(t, db, "main")

    ops := []core.Operation{
        testutil.MakeOp(stream.ID, "code", "a.go"),
        testutil.MakeOp(stream.ID, "docs", "readme.md"),
        testutil.MakeOp(stream.ID, "code", "b.go"),
    }

    err := writer.WriteBatch(ops)
    assert.NoError(t, err)

    reader := core.NewOpReader(db)
    all, _ := reader.ReadRange(0, 100)
    assert.Len(t, all, 3)
    assert.Equal(t, int64(1), all[0].Seq)
    assert.Equal(t, int64(2), all[1].Seq)
    assert.Equal(t, int64(3), all[2].Seq)
}
```

### Object Store Tests

```go
func TestObjectStore_Deduplication(t *testing.T) {
    store := testutil.NewTestObjectStore(t)

    content := []byte("hello world")

    hash1, err := store.Write(content)
    assert.NoError(t, err)

    hash2, err := store.Write(content)
    assert.NoError(t, err)

    assert.Equal(t, hash1, hash2)

    // Only one file should exist
    entries := countObjectFiles(store.Root())
    assert.Equal(t, 1, entries)
}

func TestObjectStore_Compression(t *testing.T) {
    store := testutil.NewTestObjectStore(t)

    // Small content — stored raw
    small := []byte("hello")
    smallHash, _ := store.Write(small)
    assert.False(t, store.IsCompressed(smallHash))

    // Large content — stored compressed
    large := make([]byte, 10000)
    for i := range large {
        large[i] = byte(i % 256)
    }
    largeHash, _ := store.Write(large)
    assert.True(t, store.IsCompressed(largeHash))

    // Read back — should be identical
    readBack, _ := store.Read(largeHash)
    assert.Equal(t, large, readBack)
}
```

### Diff Tests

```go
func TestTextDiff_SimpleChange(t *testing.T) {
    old := []byte("line1\nline2\nline3\n")
    new := []byte("line1\nmodified\nline3\n")

    result, err := diff.TextDiff(old, new, "test.go")
    assert.NoError(t, err)
    assert.Equal(t, "text", result.Type)
    assert.Len(t, result.Hunks, 1)
    assert.Contains(t, result.Hunks[0].Content, "-line2")
    assert.Contains(t, result.Hunks[0].Content, "+modified")
}

func TestStructuredDiff_JSONPatch(t *testing.T) {
    old := []byte(`{"name": "old", "count": 1}`)
    new := []byte(`{"name": "new", "count": 1, "active": true}`)

    result, err := diff.StructuredDiff(old, new, "data.json")
    assert.NoError(t, err)
    assert.Equal(t, "structured", result.Type)
    // Should have replace for "name" and add for "active"
}
```

### Merge Tests

```go
func TestThreeWayMerge_NoConflict(t *testing.T) {
    base := []byte("line1\nline2\nline3\n")
    ours := []byte("line1\nmodified-ours\nline3\n")   // Changed line 2
    theirs := []byte("line1\nline2\nmodified-theirs\n") // Changed line 3

    result, conflicts := merge.ThreeWay(base, ours, theirs)
    assert.Empty(t, conflicts)
    assert.Equal(t, "line1\nmodified-ours\nmodified-theirs\n", string(result))
}

func TestThreeWayMerge_Conflict(t *testing.T) {
    base := []byte("line1\nline2\nline3\n")
    ours := []byte("line1\nours-changed\nline3\n")
    theirs := []byte("line1\ntheirs-changed\nline3\n")

    _, conflicts := merge.ThreeWay(base, ours, theirs)
    assert.Len(t, conflicts, 1)
    assert.Equal(t, 1, conflicts[0].BaseLine)
}
```

## Integration Test Patterns

```go
func TestFullWorkflow_InitCheckpointDiffRestore(t *testing.T) {
    // Setup project
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
    os.MkdirAll(filepath.Join(dir, "docs"), 0755)
    os.WriteFile(filepath.Join(dir, "docs/readme.md"), []byte("# Hello"), 0644)

    // Init
    vault, err := core.InitVault(dir)
    require.NoError(t, err)
    defer vault.Close()

    // First checkpoint
    cp1, _ := vault.CheckpointEngine().Create(core.CheckpointInput{
        StreamID: vault.ActiveStream().ID,
        Title:    "initial",
        Source:   core.SourceManual,
        Author:   "test",
    })

    // Modify a file
    os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}"), 0644)

    // Record the change
    vault.RecordFileChange("code", "main.go")

    // Second checkpoint
    cp2, _ := vault.CheckpointEngine().Create(core.CheckpointInput{
        StreamID: vault.ActiveStream().ID,
        Title:    "added main func",
        Source:   core.SourceManual,
        Author:   "test",
    })

    // Diff between checkpoints
    result, _ := vault.DiffEngine().Diff(
        core.DiffRef{Type: "checkpoint", Value: cp1.ID},
        core.DiffRef{Type: "checkpoint", Value: cp2.ID},
        core.DiffOptions{},
    )
    assert.Equal(t, 1, result.Summary.SpacesChanged)
    assert.Equal(t, 1, result.Summary.EntitiesModified)

    // Restore to first checkpoint
    vault.CheckpointEngine().Restore(cp1.ID, core.RestoreScope{Full: true})

    // Verify file is restored
    content, _ := os.ReadFile(filepath.Join(dir, "main.go"))
    assert.Equal(t, "package main", string(content))

    // Log should show guard + restore checkpoints
    cps, _ := vault.CheckpointEngine().List(vault.ActiveStream().ID, 10)
    assert.GreaterOrEqual(t, len(cps), 4) // initial + main func + guard + restore
}
```

## Test Fixtures

### Sample Project

```
test/fixtures/sample-project/
  main.go                       # package main
  go.mod                        # module example.com/test
  docs/
    readme.md                   # # Test Project
    guide.md                    # ## Getting Started
  design/
    mockup.json                 # { "pages": [...] }
  notes/
    todo.md                     # - [x] Setup project
```

### Fixture Helper

```go
func CopySampleProject(t *testing.T) string {
    t.Helper()
    dir := t.TempDir()
    // Copy test/fixtures/sample-project/ to dir
    copyDir("test/fixtures/sample-project", dir)
    return dir
}
```

## Coverage Targets

| Package | Target |
|---------|--------|
| `internal/core` | 90%+ |
| `internal/storage` | 85%+ |
| `internal/adapter` | 80%+ |
| `internal/diff` | 90%+ |
| `internal/merge` | 90%+ |
| `internal/sync` | 80%+ |
| `internal/cli` | 70%+ |
| `pkg/loom` | 85%+ |
| Overall | 80%+ |

## Running Tests

```bash
# All tests
go test ./...

# With verbose output
go test ./... -v

# With coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Single package
go test ./internal/core/ -v

# Single test
go test ./internal/core/ -run TestOpWriter_Write -v

# Race detection
go test ./... -race

# Benchmark tests
go test ./internal/storage/ -bench=. -benchmem
```

## Benchmark Tests

```go
func BenchmarkOpWriter_Write(b *testing.B) {
    db := testutil.NewBenchDB(b)
    store := testutil.NewBenchObjectStore(b)
    writer := core.NewOpWriter(db, store)
    stream := testutil.CreateStream(b, db, "main")

    op := testutil.MakeOp(stream.ID, "code", "test.go")

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        writer.Write(op)
    }
}

func BenchmarkObjectStore_Write(b *testing.B) {
    store := testutil.NewBenchObjectStore(b)
    content := make([]byte, 4096) // 4KB file

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // Different content each time to avoid dedup
        binary.LittleEndian.PutUint64(content, uint64(i))
        store.Write(content)
    }
}

func BenchmarkTextDiff(b *testing.B) {
    old := testutil.LoadFixture("large-file-before.go")
    new := testutil.LoadFixture("large-file-after.go")

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        diff.TextDiff(old, new, "test.go")
    }
}
```

## CI Pipeline

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - run: go test ./... -race -coverprofile=coverage.out
      - run: go tool cover -func=coverage.out | tail -1
      - uses: codecov/codecov-action@v4
        with:
          file: coverage.out

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest

  build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        goos: [linux, darwin, windows]
        goarch: [amd64, arm64]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - run: GOOS=${{ matrix.goos }} GOARCH=${{ matrix.goarch }} go build ./cmd/loom/
```
