# Loom

**Versioning for 2026+.** Continuous, intelligent, multi-space.

Loom is a ground-up reinvention of version control. It replaces the manual commit-branch-merge ceremony of Git with continuous, automatic versioning. Code, docs, design, data — everything gets versioned in one unified timeline.

Built with Go. Open source. MIT license.

## Why Loom

Git was designed in 2005 for the Linux kernel. Twenty years later, the world looks different:

- **Projects are more than code.** Docs, design files, data, configs, AI conversations — all need versioning.
- **AI agents write code now.** They need a versioning API, not a CLI designed for humans typing in terminals.
- **Merge conflicts kill productivity.** They're a 2005 solution to a 2005 problem.
- **Manual commits create gaps.** The time between commits is a black hole — work is lost, context disappears.

| Concept | Git | Loom |
|---------|-----|------|
| Unit of change | Commit (manual) | Operation (automatic) |
| History | Snapshot DAG | Append-only operation log |
| Branching | Branches + merge ceremony | Streams that auto-converge |
| Content types | Text files | Any content via space adapters |
| Scope | Single repo | Multi-space project (code + docs + design + ...) |
| Audience | Humans | Humans + AI agents + automation |
| Auto-save | None | Continuous auto-checkpointing |

## Install

**Download a binary** from the [latest release](https://github.com/LoomHubDev/loom/releases/latest).

```bash
# macOS (Apple Silicon)
curl -L https://github.com/LoomHubDev/loom/releases/latest/download/loom_0.1.0_darwin_arm64.tar.gz | tar xz
sudo mv loom /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/LoomHubDev/loom/releases/latest/download/loom_0.1.0_darwin_amd64.tar.gz | tar xz
sudo mv loom /usr/local/bin/

# Linux (amd64)
curl -L https://github.com/LoomHubDev/loom/releases/latest/download/loom_0.1.0_linux_amd64.tar.gz | tar xz
sudo mv loom /usr/local/bin/
```

**Or install via Go:**

```bash
go install github.com/constructspace/loom/cmd/loom@latest
```

## Quick Start

```bash
# Initialize a project
loom init

# Check status
loom status

# Create a checkpoint (like a commit, but optional — history exists without them)
loom checkpoint "before refactor"

# View history
loom log

# Work with streams (like branches, but they auto-converge)
loom stream create feature/auth
loom stream switch feature/auth
loom stream list
```

## Sync with LoomHub

Loom has built-in support for syncing with [LoomHub](https://github.com/LoomHubDev/loomhub), a GitHub-like hosting platform for Loom projects.

```bash
# Add a remote
loom hub add origin https://hub.example.com/alice/myproject

# Authenticate
loom hub auth

# Push changes
loom send

# Pull changes
loom receive
```

## Core Concepts

### Operations
The atomic unit of change. Not a snapshot — just what changed. Operations are append-only and automatically recorded as you work.

### Streams
Live, auto-versioning timelines. Like branches, but they version themselves continuously. Multiple streams can run in parallel and auto-converge without merge conflicts.

### Checkpoints
Named points on a stream. Like Git commits, but optional — history exists with or without them. Can be created manually, automatically (every N operations), or by AI agents.

### Spaces
A project can have multiple spaces: code, docs, design, data. Each space has an adapter that knows how to detect, track, and diff that content type. All spaces share a single unified timeline.

## Architecture

```
.loom/
  config.toml        # Project configuration
  loom.db            # SQLite database (operations, entities, streams, checkpoints)
  objects/           # Content-addressed blob store (SHA-256, zstd compressed)
```

- **Storage**: SQLite + content-addressed object store
- **Hashing**: `sha256("blob:" + len + "\0" + content)`
- **Compression**: zstd for objects > 4KB
- **IDs**: ULIDs (time-sortable, globally unique)

## CLI Reference

```
loom init              Initialize a new project
loom status            Show project status
loom checkpoint        Create a named checkpoint
loom log               Show checkpoint history
loom stream            Manage streams (create, switch, list, merge)
loom hub add           Add a hub remote
loom hub remove        Remove a hub remote
loom hub list          List hub remotes
loom hub auth          Authenticate with a hub
loom send              Push local changes to a hub
loom receive           Pull changes from a hub
```

## Documentation

Full documentation: [LoomHubDev/loom-docs](https://github.com/LoomHubDev/loom-docs)

## License

MIT
