# 08 — Development Roadmap

## Phase Overview

| Phase | Focus | Milestone |
|-------|-------|-----------|
| 1 | Foundation | Core engine works: init, write ops, checkpoint, log |
| 2 | File watching + Auto-versioning | Continuous versioning daemon |
| 3 | Diff + Restore | Compare and restore any point |
| 4 | Streams + Merge | Branching with intelligent merge |
| 5 | Agent API | LLM-native versioning interface |
| 6 | Sync (Client/Server) | Push/pull to remotes |
| 7 | Polish + Release | v0.1.0 public release |

## Phase 1: Foundation

Build the core engine. Everything else depends on this.

### Deliverables

- [ ] Go module setup (`go.mod`, project structure, Makefile)
- [ ] Core types (Operation, Checkpoint, Stream, Entity, Object)
- [ ] SQLite storage layer (schema, migrations, pragmas)
- [ ] Object store (SHA-256 content-addressed blobs)
- [ ] Operation log (write, read, query)
- [ ] Checkpoint engine (create manual checkpoints)
- [ ] Stream manager (create `main`, basic CRUD)
- [ ] Space adapter interface
- [ ] Code adapter (Git-aware)
- [ ] Docs adapter (filesystem)
- [ ] CLI: `loom init`
- [ ] CLI: `loom status`
- [ ] CLI: `loom checkpoint <title>`
- [ ] CLI: `loom log`
- [ ] Tests: core, storage, adapters

### Exit Criteria

```bash
cd my-project
loom init            # Creates .loom/ with SQLite + config
loom status          # Shows detected spaces and entity count
loom checkpoint "v1" # Creates a checkpoint
loom log             # Shows the checkpoint
```

## Phase 2: File Watching + Auto-Versioning

The "Google Docs" experience: changes are versioned automatically.

### Deliverables

- [ ] File watcher (fsnotify-based)
- [ ] Change debouncer (500ms default)
- [ ] Ignore system (.loomignore + built-in patterns)
- [ ] Watch → adapter → operation pipeline
- [ ] Auto-checkpoint engine (threshold + time-based)
- [ ] CLI: `loom watch` (foreground daemon)
- [ ] CLI: `loom watch --daemon` (background)
- [ ] Config: `[watch]` and `[checkpoint]` sections
- [ ] Design adapter (JSON structural tracking)
- [ ] Notes adapter (text tracking)
- [ ] Tests: watcher, debouncer, auto-checkpoint

### Exit Criteria

```bash
loom watch &                 # Start daemon
echo "hello" > docs/new.md   # File created
# Loom auto-creates operation
# After threshold, auto-checkpoint created

loom log                     # Shows auto-checkpoints
```

## Phase 3: Diff + Restore

See what changed. Go back in time.

### Deliverables

- [ ] Diff engine (orchestration layer)
- [ ] Text diff (Myers algorithm)
- [ ] Structured diff (JSON patch)
- [ ] Binary diff (fingerprint comparison)
- [ ] Diff formatting (terminal with color, JSON, patch)
- [ ] Ref resolution (checkpoint IDs, HEAD, HEAD~N)
- [ ] Restore engine (full, per-space, per-entity)
- [ ] Guard checkpoint (auto-created before restore)
- [ ] Restore checkpoint (auto-created after restore)
- [ ] CLI: `loom diff`
- [ ] CLI: `loom diff <from> <to>`
- [ ] CLI: `loom show <checkpoint-id>`
- [ ] CLI: `loom restore <checkpoint-id>`
- [ ] Tests: diff, restore, round-trip

### Exit Criteria

```bash
loom diff                    # Shows changes since last checkpoint
loom diff HEAD~2 HEAD        # Compare two checkpoints
loom restore <id>            # Restore project state
loom log                     # Shows guard + restore checkpoints
```

## Phase 4: Streams + Merge

Branching without the pain.

### Deliverables

- [ ] Stream creation (fork from current)
- [ ] Stream switching
- [ ] Stream listing and info
- [ ] Fork point detection
- [ ] Three-way text merge
- [ ] Structural merge (JSON)
- [ ] Merge engine (Tier 1 + Tier 2 auto-merge)
- [ ] LLM merge integration (Tier 3)
- [ ] Merge policy configuration
- [ ] CLI: `loom stream create/switch/list/info`
- [ ] CLI: `loom merge <stream>`
- [ ] Tests: merge scenarios, conflict resolution

### Exit Criteria

```bash
loom stream create feature/auth    # Fork a stream
# ... make changes ...
loom stream switch main            # Switch back
loom merge feature/auth            # Merge without conflicts
```

## Phase 5: Agent API

Make Loom a first-class tool for AI agents.

### Deliverables

- [ ] Go SDK (`pkg/loom/client.go`)
- [ ] HTTP API server (agent-server)
- [ ] All agent endpoints (checkpoint, rollback, diff, log, status, explain, search)
- [ ] LLM tool definitions (JSON schema for function calling)
- [ ] Agent authentication (local tokens)
- [ ] SSE event stream (for real-time agent notifications)
- [ ] CLI: `loom agent-server`
- [ ] Tests: SDK, API endpoints

### Exit Criteria

```bash
loom agent-server --port 7890 &

# From an agent:
curl -X POST localhost:7890/api/v1/checkpoint \
  -d '{"title": "before refactor"}'
# Returns checkpoint JSON

curl localhost:7890/api/v1/status
# Returns project status JSON
```

## Phase 6: Sync (Client/Server)

Push and pull to remotes.

### Deliverables

- [ ] Loom server binary (`cmd/loom-server`)
- [ ] Server API (negotiate, push, pull, project info)
- [ ] Server storage (SQLite, optionally Postgres)
- [ ] Server authentication (JWT tokens)
- [ ] Sync client (push, pull, negotiate)
- [ ] Remote management (add, remove, list)
- [ ] Sync log (track what's been synced)
- [ ] CLI: `loom remote add/remove/list`
- [ ] CLI: `loom push`
- [ ] CLI: `loom pull`
- [ ] Docker image for server
- [ ] Tests: sync round-trip, server API

### Exit Criteria

```bash
# Start server
docker run -p 8080:8080 loom-server

# Client
loom remote add origin http://localhost:8080/project/my-app
loom push                    # Pushes ops + objects
loom pull                    # Pulls remote ops
```

## Phase 7: Polish + Release

Prepare for v0.1.0 public release.

### Deliverables

- [ ] Error messages and UX polish
- [ ] `loom doctor` (integrity checks)
- [ ] `loom export` / `loom import` (backup/restore)
- [ ] `loom compact` (operation log compaction)
- [ ] `.loomignore` documentation
- [ ] GoReleaser config (cross-platform builds)
- [ ] CI/CD pipeline (GitHub Actions)
- [ ] README.md
- [ ] Website / landing page
- [ ] Homebrew formula
- [ ] Performance benchmarks
- [ ] Security audit
- [ ] License headers

### Exit Criteria

```bash
# Install via Homebrew
brew install loom

# Full workflow
loom init
loom watch &
# ... work ...
loom checkpoint "ready for review"
loom push
loom diff HEAD~5 HEAD
loom agent-server &
```

## Future (Post v0.1.0)

| Feature | Phase |
|---------|-------|
| CRDT-based merge (Automerge integration) | v0.2 |
| Real-time collaboration (WebSocket sync) | v0.2 |
| Construct space integration (visual UI) | v0.2 |
| Semantic diff (AST-aware code diff) | v0.3 |
| Operation log compaction (background) | v0.3 |
| Plugin system (custom adapters as Go plugins) | v0.3 |
| Git bridge (import/export Git history) | v0.4 |
| Cloud hosting (managed Loom server) | v0.5 |
| Multi-project versioning | v0.5 |
| Encryption at rest | v0.5 |
| Signed operations (cryptographic signatures) | v0.6 |

## Timeline

Phase 1-3 are the critical path. Once diffing and restore work, Loom is usable as a personal tool. Phases 4-6 enable team collaboration. Phase 7 makes it releasable.

No time estimates — focus on shipping each phase completely before moving to the next.
