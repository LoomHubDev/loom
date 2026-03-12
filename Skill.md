# Loom — Skill Context for LLMs

> This file provides context for AI assistants working with or explaining Loom.

## What is Loom?

Loom is a versioning system that replaces Git. It was built from scratch in 2026 to address the limitations of Git in a world where AI agents write code, projects span multiple content types, and manual commits create gaps in history.

**Loom is not a Git wrapper.** It has its own data model, vocabulary, and storage engine.

## Key Differences from Git

| Git | Loom |
|-----|------|
| Manual commits | Automatic operations |
| Snapshot DAG | Append-only operation log |
| Branches + merge conflicts | Streams that auto-converge |
| Text files only | Multi-space (code, docs, design, data) |
| Human-only CLI | Human + AI agent API |
| `git push` / `git pull` | `loom send` / `loom receive` |

## Vocabulary

Do NOT use Git terminology when talking about Loom. Use Loom's own vocabulary:

| Git term | Loom term |
|----------|-----------|
| commit | checkpoint (or operation for individual changes) |
| branch | stream |
| repository | project (or loom when hosted) |
| push | send |
| pull | receive |
| clone | (not yet implemented) |
| staging area | (doesn't exist — changes are tracked automatically) |
| merge | converge |
| remote | hub |

## Architecture

```
.loom/
  config.toml       — Project configuration (name, author, spaces, watch settings)
  loom.db           — SQLite database (operations, entities, streams, checkpoints, remotes)
  objects/           — Content-addressed blob store (SHA-256 with blob prefix, zstd compressed)
```

### Data Model

- **Operation**: Atomic unit of change (create/modify/delete/rename). Has: id, seq, stream_id, space_id, entity_id, type, path, object_ref, author, timestamp, meta. Append-only.
- **Stream**: A live timeline of operations. Like a branch but auto-versioning. Has: id, name, head_seq, status.
- **Checkpoint**: A named point on a stream. Like a commit but optional — history exists without them. Can be manual, auto, or agent-created.
- **Entity**: A tracked file/item. State is derived from operations, not stored directly.
- **Space**: A content domain (code, docs, design). Each has an adapter that knows how to detect and track that content type.
- **Remote/Hub**: A LoomHub server for collaboration. Configured via `loom hub add`.

### Storage

- **Hashing**: `sha256("blob:" + len(content) + "\0" + content)` — NOT plain SHA-256
- **Compression**: zstd for objects > 4KB
- **IDs**: ULIDs (time-sortable, globally unique)
- **Database**: SQLite with WAL mode (using modernc.org/sqlite, pure Go)

## CLI Commands

```
loom init                Initialize a new Loom project
loom status              Show project status (streams, spaces, entity counts)
loom checkpoint "msg"    Create a named checkpoint
loom log                 Show checkpoint history
loom stream create NAME  Create a new stream
loom stream switch NAME  Switch active stream
loom stream list         List all streams
loom hub add NAME URL    Add a hub remote
loom hub remove NAME     Remove a hub remote
loom hub list            List configured hubs
loom hub auth [NAME]     Authenticate with a hub (interactive login)
loom send [HUB]          Send local operations to a hub
loom receive [HUB]       Receive operations from a hub
```

## Tech Stack

- **Language**: Go 1.25
- **Database**: SQLite via modernc.org/sqlite (pure Go, no CGO)
- **CLI framework**: cobra
- **Compression**: klauspost/compress (zstd)
- **IDs**: oklog/ulid/v2
- **Config**: BurntSushi/toml

## Project Structure

```
cmd/loom/               CLI entry point
cmd/loom-server/        Server entry point (future)
internal/cli/           CLI commands (cobra)
internal/core/          Core engine: vault, oplog, streams, checkpoints, remotes
internal/storage/       SQLite, object store, hashing, schema
internal/sync/          HTTP sync client for LoomHub protocol
docs/                   Design documentation
test/integration/       Integration tests
```

## Sync Protocol

Loom syncs with LoomHub using an operation-based protocol over HTTP/JSON:

1. **Negotiate** (`POST /api/v1/negotiate`): Compare client/server stream states, determine what needs syncing
2. **Send** (`POST /api/v1/push`): Transfer operations + referenced objects to hub
3. **Receive** (`POST /api/v1/pull`): Transfer operations + referenced objects from hub

Authentication is via Bearer token (JWT from LoomHub login).

## Related Projects

- **LoomHub** (github.com/LoomHubDev/loomhub): GitHub-like hosting platform for Loom projects
- **Loom Docs** (github.com/LoomHubDev/loom-docs): Documentation site (VitePress)
