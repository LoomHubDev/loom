# Loom Phases 2-7 — Unified Implementation Spec

## Status

- **Phase 1 (Foundation)**: COMPLETE — vault, ops, checkpoints, streams, storage, CLI, 271 tests
- **Phase 4 (Streams)**: PARTIAL — WeaveEngine exists with Tier 1 conflict detection, `ours`/`theirs` strategy, fork-point detection. Stream CRUD + CLI done.
- **Phase 6 (Sync Client)**: PARTIAL — HTTP client (negotiate/push/pull), send/receive CLI with batching, ImportBatch for remote ops.

Everything else: not started.

---

## Phase 2: File Watching + Auto-Versioning

### Goal
Continuous auto-versioning: file changes are captured as operations automatically.

### New Packages

#### `internal/watch/watcher.go`
- `Watcher` struct: wraps fsnotify, routes events to adapters
- `New(vault *Vault, opts WatchOptions) *Watcher`
- `Start(ctx context.Context) error` — blocks, watching for events
- `Stop()` — graceful shutdown
- Routes file events to correct space based on path matching against `Config.Spaces`

#### `internal/watch/debounce.go`
- `Debouncer` struct: coalesces rapid changes per-file
- Configurable window (default 500ms from `config.toml` `watch.debounce_ms`)
- Emits one consolidated event per file per window

#### `internal/watch/filter.go`
- `Filter` struct: checks paths against ignore rules
- Sources: `Config.Watch.Ignore` list (already in config), `.loomignore` file (gitignore syntax)
- Uses `filepath.Match` for glob patterns

#### `internal/watch/autocheckpoint.go`
- `AutoCheckpointer` struct: evaluates checkpoint criteria
- Triggers on: op count threshold (`checkpoint.interval_ops`), time threshold (`checkpoint.interval_seconds`), significant change (multiple spaces or deletes)
- Creates checkpoint with `Source: SourceAuto`

### Adapter System — `internal/adapter/`

#### `internal/adapter/adapter.go`
```go
type SpaceAdapter interface {
    ID() string
    Name() string
    NormalizeChange(event FileEvent) ([]Operation, error)
    Diff(oldContent, newContent []byte, path string) (*DiffOutput, error)
}
```

Minimal interface — just what's needed. No over-abstraction.

#### `internal/adapter/registry.go`
- `AdapterRegistry` struct with `map[string]SpaceAdapter`
- `Register(adapter)`, `Get(id)`, `ForPath(relPath) SpaceAdapter`

#### Built-in Adapters
- `internal/adapter/code.go` — CodeAdapter: text files, detects binary vs text
- `internal/adapter/docs.go` — DocsAdapter: markdown/text files in docs dirs
- `internal/adapter/filesystem.go` — FilesystemAdapter: generic fallback for design/notes/data spaces

All adapters share the same core logic: read file, create Operation with correct OpType, store content in ObjectStore. The differentiation is primarily in `Diff()`.

### CLI
- `loom watch` — foreground daemon (Start watcher + auto-checkpoint)
- `loom watch --daemon` — background (uses `os/exec` to re-launch detached)

### Dependencies
- `github.com/fsnotify/fsnotify` — file system notifications

---

## Phase 3: Diff + Restore

### Goal
Compare any two points in history. Restore to any checkpoint.

### New Package: `internal/diff/`

#### `internal/diff/engine.go`
- `DiffEngine` struct: orchestrates diffs between two refs
- `New(db, reader, store, registry) *DiffEngine`
- `Diff(from, to DiffRef, opts DiffOptions) (*DiffResult, error)`
- Groups ops by space, delegates entity-level diffing to adapters

#### `internal/diff/ref.go`
- `DiffRef` type with `Type` (checkpoint, seq, head, relative) and `Value`
- `ParseRef(s string) DiffRef` — parses "HEAD", "HEAD~3", checkpoint IDs, raw seq numbers
- `resolveRef(ref DiffRef) (int64, error)` — converts to sequence number

#### `internal/diff/text.go`
- `TextDiff(old, new []byte, context int) []DiffHunk`
- Myers diff algorithm implementation (or use `github.com/sergi/go-diff`)
- Produces hunks with line numbers, insertions, deletions

#### `internal/diff/structured.go`
- `StructuredDiff(old, new []byte) ([]JSONPatch, error)`
- JSON patch (RFC 6902) computation for structured files
- Uses `github.com/wI2L/jsondiff` or manual tree walk

#### `internal/diff/binary.go`
- `BinaryDiff(oldHash, newHash string, oldSize, newSize int64) string`
- Metadata-only comparison (size, hash)

#### `internal/diff/format.go`
- `TerminalFormatter` — colored unified diff output
- `JSONFormatter` — structured JSON output (for agent API)

### Types
```go
type DiffResult struct {
    FromSeq int64
    ToSeq   int64
    Spaces  []SpaceDiff
    Summary DiffSummary
}

type SpaceDiff struct {
    SpaceID  string
    Entities []EntityDiff
    Summary  SpaceSummary
}

type EntityDiff struct {
    EntityID string
    Path     string
    Change   ChangeType
    Hunks    []DiffHunk   // for text
    Patches  []JSONPatch  // for structured
    Summary  string       // for binary
}

type DiffHunk struct {
    OldStart int
    OldLines int
    NewStart int
    NewLines int
    Lines    []DiffLine
}

type DiffLine struct {
    Type    string // "add", "delete", "context"
    Content string
}
```

### Restore — `internal/core/restore.go`
- `RestoreEngine` struct
- `Restore(checkpointID string, scope RestoreScope) error`
- `RestoreScope`: Full, PerSpace(spaceID), PerEntity(entityID)
- Before restore: auto-create guard checkpoint (`Source: SourceGuard`)
- After restore: create restore checkpoint (`Source: SourceRestore`)
- Reads entity states at target checkpoint, writes file content from ObjectStore

### CLI
- `loom diff` — changes since last checkpoint
- `loom diff <ref-a> <ref-b>` — between two refs
- `loom diff --space code`, `--entity path`, `--format json`, `--summary`, `-C N`
- `loom show <checkpoint-id>` — show checkpoint details + diff from parent
- `loom restore <checkpoint-id>` — restore with guard checkpoint
- `loom restore <checkpoint-id> --entity src/main.go` — partial restore

### Dependencies
- `github.com/sergi/go-diff` — Myers diff algorithm

---

## Phase 4: Streams + Merge (Completion)

### Current State
WeaveEngine handles Tier 1 (different entities) and simple ours/theirs strategies. Missing: actual content merging.

### What to Add

#### `internal/merge/engine.go`
- `MergeEngine` struct: wraps WeaveEngine for content-level merging
- `MergeEntity(entityID string, base, ours, theirs []byte) (*MergeResult, error)`
- Delegates to text or structured merge based on content type

#### `internal/merge/text.go`
- `ThreeWayMerge(base, ours, theirs []byte) ([]byte, []Conflict, error)`
- Uses diff3 algorithm: compute diffs from base to each side, combine non-overlapping
- Conflict = both sides changed the same line range differently

#### `internal/merge/structured.go`
- `StructuredMerge(base, ours, theirs []byte) ([]byte, []Conflict, error)`
- JSON field-level merge: compute patches from base, check for path conflicts
- Non-conflicting paths: apply both patches
- Conflicting paths: return as Conflict

#### `internal/merge/llm.go` (Tier 3 — optional)
- `LLMMerger` struct with LLM endpoint config
- `Resolve(ctx MergeContext) (*LLMResult, error)`
- Builds prompt with base/ours/theirs + conflict description
- Auto-apply if confidence >= threshold
- Skip if LLM unavailable — fall through to manual

#### `internal/merge/policy.go`
- `MergePolicy` struct: strategy per space, LLM config
- Read from `config.toml` `[merge]` section
- Strategies: `auto`, `auto+llm`, `ours`, `theirs`

### Update WeaveEngine
- When conflicts detected and strategy is `auto`: attempt content merge via MergeEngine
- Only escalate to error if content merge fails

### CLI Update
- `loom weave <stream>` — already exists, enhance with merge output
- `loom weave --dry-run` — already exists
- `loom weave --strategy auto|ours|theirs` — already exists
- Add: `loom weave --accept-all` — accept all LLM suggestions

### Dependencies
- `github.com/sergi/go-diff` (shared with Phase 3)

---

## Phase 5: Agent API

### Goal
HTTP API + Go SDK for AI agents to version, diff, rollback, and explain changes.

### Go SDK — `pkg/loom/client.go`
```go
type Client struct {
    vault *core.Vault
}

func Open(projectPath string) (*Client, error)
func (c *Client) Checkpoint(title string, opts ...CheckpointOption) (*Checkpoint, error)
func (c *Client) Rollback(checkpointID string) error
func (c *Client) RollbackEntity(checkpointID, entityID string) error
func (c *Client) Diff(from, to string) (*DiffResult, error)
func (c *Client) DiffSummary(from, to string) (string, error)
func (c *Client) Log(limit int) ([]Checkpoint, error)
func (c *Client) Status() (*StatusResult, error)
func (c *Client) Search(query string) ([]Checkpoint, error)
func (c *Client) Close() error
```

Thin wrapper over core.Vault — adds convenience, agent-friendly defaults (Source: SourceAgent).

### HTTP Agent Server — `internal/agent/`

#### `internal/agent/server.go`
- `AgentServer` struct wrapping vault + HTTP router
- `New(vault *Vault, port int) *AgentServer`
- `Start(ctx context.Context) error`
- Uses `net/http` + `chi` router

#### `internal/agent/handlers.go`
Endpoints:
| Method | Path | Handler |
|--------|------|---------|
| POST | `/api/v1/checkpoint` | Create checkpoint |
| POST | `/api/v1/rollback` | Rollback to checkpoint |
| GET | `/api/v1/diff` | Get diff between refs |
| GET | `/api/v1/log` | Get checkpoint log |
| GET | `/api/v1/status` | Get project status |
| GET | `/api/v1/search` | Search checkpoints |
| POST | `/api/v1/record` | Record a file change |

All return JSON. Auth via local Bearer token stored in `.loom/agent-token`.

#### `internal/agent/schema.go`
- LLM tool definitions as JSON (for function calling)
- `ToolDefinitions() []byte` — returns JSON array of tool schemas

### CLI
- `loom agent-server --port 7890` — start standalone agent API
- Integrate with `loom watch --agent-api --agent-port 7890` (Phase 2 daemon)

### Dependencies
- `github.com/go-chi/chi/v5` — HTTP router

---

## Phase 6: Sync Server (Completion)

### Current State
Client done: negotiate, push, pull, send/receive CLI with batching. Server is a stub.

### New Package: `internal/server/`

#### `internal/server/server.go`
- `HubServer` struct: db, object store, router
- `New(config ServerConfig) *HubServer`
- `Start(addr string) error`
- Multi-project: each project gets its own SQLite DB in a data directory

#### `internal/server/handlers.go`
Endpoints (match client protocol exactly):
| Method | Path | Handler |
|--------|------|---------|
| POST | `/:owner/:loom/api/v1/negotiate` | Compare stream states |
| POST | `/:owner/:loom/api/v1/push` | Receive ops + objects |
| POST | `/:owner/:loom/api/v1/pull` | Send ops + objects |
| GET | `/:owner/:loom/api/v1/info` | Project metadata |
| POST | `/api/v1/auth/login` | Authenticate, return JWT |

#### `internal/server/auth.go`
- JWT token generation and validation
- `authMiddleware` — validates Bearer token on all routes except login
- Simple user store in server SQLite (username + bcrypt hash)

#### `internal/server/store.go`
- `ProjectStore` — manages per-project databases
- `GetOrCreateProject(owner, name string) (*ProjectDB, error)`
- Each project: own SQLite DB + object store directory
- Reuses `internal/storage` schema

### `cmd/loom-server/main.go`
- Cobra CLI: `loom-server serve --port 3000 --data /var/loom`
- `loom-server user add <username>` — create user
- `loom-server project create <owner>/<name>` — create project

### Dependencies
- `github.com/go-chi/chi/v5` (shared with Phase 5)
- `github.com/golang-jwt/jwt/v5` — JWT tokens
- `golang.org/x/crypto/bcrypt` — password hashing

---

## Phase 7: Polish + Release

### `loom doctor` — Integrity Checks
- `internal/cli/doctor.go`
- Verify: DB schema version, seq counter consistency, orphan objects, missing object refs, stream head consistency
- Report: OK / warnings / errors

### `loom export` / `loom import`
- `internal/cli/export.go`, `import.go`
- Export: tar.gz of `.loom/` directory (DB + objects)
- Import: extract and validate

### `loom compact`
- `internal/core/compact.go`
- Merge old operations into summary ops (reduce log size)
- Keep all checkpoints, compact ops between them

### CI/CD
- `.github/workflows/test.yml` — `go test ./...` on push
- `.github/workflows/release.yml` — GoReleaser on tag

### GoReleaser
- `.goreleaser.yaml` already exists — verify/update for both `loom` and `loom-server` binaries

### Homebrew
- `HomebrewFormula/loom.rb` — generated by GoReleaser

---

## Implementation Order

Strictly sequential per the roadmap. Each phase ships completely before the next begins.

| Order | Phase | Estimated Files | Key Dependencies |
|-------|-------|----------------|-----------------|
| 1 | Phase 2 (Watch) | ~8 new files | fsnotify |
| 2 | Phase 3 (Diff + Restore) | ~10 new files | go-diff |
| 3 | Phase 4 (Merge completion) | ~5 new files | go-diff (shared) |
| 4 | Phase 5 (Agent API) | ~6 new files | chi |
| 5 | Phase 6 (Server) | ~5 new files | chi, jwt, bcrypt |
| 6 | Phase 7 (Polish) | ~6 new files | goreleaser |

Total: ~40 new files across all phases.

## Testing Strategy

Every phase includes tests:
- Unit tests for each new package (testify assert/require)
- Integration tests for CLI commands
- For sync: mock HTTP server tests (already established pattern)
- For merge: table-driven tests with known conflict scenarios
- For watch: short debounce window tests with temp dirs
- For agent API: httptest server tests

## Non-Goals (Explicitly Deferred)

- CRDT-based merge (v2)
- Real-time WebSocket sync (v2)
- Semantic/AST-aware diff (v3)
- Plugin system for custom adapters (v3)
- Git bridge (v4)
- Encryption at rest (v5)
