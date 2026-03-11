# 03 — Project Setup

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.25+ | Language runtime |
| Git | 2.40+ | Code space adapter reads Git state |
| SQLite | 3.40+ | Bundled via `modernc.org/sqlite` (no system install needed) |

## Repository Structure

```
loom/
  go.mod                        # Module: github.com/AzuraCast/loom (or your org)
  go.sum
  LICENSE                       # MIT
  Makefile                      # Build, test, lint, release targets
  .goreleaser.yml               # Cross-platform release builds
  cmd/
    loom/main.go                # CLI binary
    loom-server/main.go         # Server binary
  internal/                     # Private packages (Go convention)
  pkg/                          # Public SDK packages
  test/                         # Integration tests and fixtures
  docs/                         # This documentation
```

## Quick Start

### Build from Source

```bash
# Clone
git clone https://github.com/org/loom.git
cd loom

# Build CLI
go build -o bin/loom ./cmd/loom

# Build Server
go build -o bin/loom-server ./cmd/loom-server

# Or build both
make build
```

### Install

```bash
# Via go install
go install github.com/org/loom/cmd/loom@latest

# Or download binary from releases
curl -fsSL https://get.loom.dev | sh
```

### First Use

```bash
# Initialize a project
cd my-project
loom init

# Loom detects existing spaces:
#   ✓ code (Git repository detected)
#   ✓ docs (docs/ directory found)
#   + notes (notes/ directory found)

# Start auto-versioning
loom watch

# Create a named checkpoint
loom checkpoint "initial setup"

# View timeline
loom log

# View changes since last checkpoint
loom diff
```

## Development Setup

### Clone and Build

```bash
git clone https://github.com/org/loom.git
cd loom
go mod download
make build
```

### Run Tests

```bash
# All tests
make test

# Unit tests only
go test ./internal/...

# Integration tests
go test ./test/integration/...

# With coverage
make test-coverage

# Single package
go test ./internal/core/ -v
```

### Run Linter

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run
make lint
```

### Makefile Targets

```makefile
build:           # Build both binaries to bin/
test:            # Run all tests
test-unit:       # Run unit tests only
test-integration:# Run integration tests
test-coverage:   # Run tests with coverage report
lint:            # Run golangci-lint
fmt:             # Format code (gofmt + goimports)
clean:           # Remove build artifacts
release:         # Build release binaries for all platforms
install:         # Install to $GOPATH/bin
```

## Configuration

### Project Config (.loom/config.toml)

Created by `loom init`. Lives at `.loom/config.toml` inside the project.

```toml
[project]
name = "my-app"
version = 1                     # Config schema version

[author]
name = "flakerimi"
email = "flakerim@example.com"

[spaces]
code = { adapter = "git", path = "." }
docs = { adapter = "filesystem", path = "docs/" }
design = { adapter = "design", path = "design/" }
notes = { adapter = "filesystem", path = "notes/" }

[watch]
enabled = true
debounce_ms = 500               # Coalesce changes within this window
ignore = [
  ".git",
  "node_modules",
  "dist",
  "build",
  ".loom",
  "*.tmp",
  "*.swp",
]

[checkpoint]
auto = true
interval_ops = 50               # Auto-checkpoint every N operations
interval_seconds = 300          # Or every N seconds of changes
on_significant_change = true    # Checkpoint on large diffs

[remote]
# Added by `loom remote add`
# origin = "https://loom.example.com/project/my-app"

[agent]
enabled = false                 # Start agent API on `loom watch`
port = 7890
```

### Global Config (~/.config/loom/config.toml)

User-level defaults.

```toml
[author]
name = "flakerimi"
email = "flakerim@example.com"

[defaults]
auto_watch = true
auto_checkpoint = true

[aliases]
co = "checkpoint"
st = "status"
```

### Ignore File (.loomignore)

Same syntax as `.gitignore`. Controls which files the watcher ignores.

```
# Dependencies
node_modules/
vendor/

# Build output
dist/
build/
bin/

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store
Thumbs.db

# Secrets
.env
*.key
*.pem
```

## Dependencies

### Core Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `modernc.org/sqlite` | SQLite driver (pure Go, no CGo) |
| `github.com/fsnotify/fsnotify` | Cross-platform file watching |
| `github.com/klauspost/compress/zstd` | Blob compression |
| `github.com/BurntSushi/toml` | Config parsing |
| `github.com/go-chi/chi/v5` | HTTP router (server + agent API) |
| `github.com/sergi/go-diff` | Text diffing (Myers algorithm) |
| `github.com/google/uuid` | UUID generation |
| `github.com/rs/zerolog` | Structured logging (or stdlib slog) |

### Dev Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/stretchr/testify` | Test assertions |
| `github.com/golangci/golangci-lint` | Linting |
| `github.com/goreleaser/goreleaser` | Release automation |

### Zero CGo Policy

All dependencies are pure Go. No CGo. This ensures:
- Cross-compilation works (`GOOS=linux GOARCH=amd64 go build`)
- No system library dependencies
- Single static binary
- Fast CI builds

## IDE Setup

### VS Code

Recommended extensions:
- `golang.go` — official Go extension
- `EditorConfig` — consistent formatting

`.vscode/settings.json`:
```json
{
  "go.lintTool": "golangci-lint",
  "go.lintFlags": ["--fast"],
  "go.testFlags": ["-v"],
  "editor.formatOnSave": true,
  "[go]": {
    "editor.defaultFormatter": "golang.go"
  }
}
```

### GoLand / IntelliJ

- Enable `golangci-lint` integration
- Set `File Watchers` for `gofmt` on save
- Mark `internal/` as project sources root

## Release Process

### Versioning

Loom follows semver: `v0.1.0`, `v0.2.0`, ..., `v1.0.0`.

Pre-1.0: breaking changes are expected between minor versions.

### Building Releases

```bash
# Tag a release
git tag v0.1.0
git push origin v0.1.0

# GoReleaser handles the rest (via CI)
goreleaser release
```

### Release Matrix

| OS | Architecture | Binary |
|----|-------------|--------|
| macOS | arm64 | `loom-darwin-arm64` |
| macOS | amd64 | `loom-darwin-amd64` |
| Linux | amd64 | `loom-linux-amd64` |
| Linux | arm64 | `loom-linux-arm64` |
| Windows | amd64 | `loom-windows-amd64.exe` |
