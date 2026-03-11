# 07 — Agent API

## Overview

Loom is LLM-native. AI agents are first-class citizens that can version, diff, rollback, and explain changes through a structured API. The agent API is both a Go SDK (for agents running in-process) and an HTTP API (for external agents).

## Why Agent-First

Traditional version control was built for humans typing commands. AI agents need:

1. **Structured input/output** — JSON, not terminal text
2. **Checkpointing** — save state before risky operations, rollback if something breaks
3. **Context** — understand what changed, when, and why
4. **Atomicity** — group related changes across spaces into one checkpoint
5. **Explainability** — natural language summaries of changes

## Go SDK

For agents running in the same process or as a Go library:

```go
package loom

type Client struct {
    vault *Vault
}

// Open an existing Loom project
func Open(projectPath string) (*Client, error) {
    vault, err := OpenVault(projectPath)
    if err != nil {
        return nil, err
    }
    return &Client{vault: vault}, nil
}

// Checkpoint creates a named checkpoint
func (c *Client) Checkpoint(title string, opts ...CheckpointOption) (*Checkpoint, error) {
    input := CheckpointInput{
        StreamID: c.vault.ActiveStream().ID,
        Title:    title,
        Source:   SourceAgent,
        Author:   "agent",
    }
    for _, opt := range opts {
        opt(&input)
    }
    return c.vault.CheckpointEngine().Create(input)
}

// Rollback restores to a previous checkpoint
func (c *Client) Rollback(checkpointID string) error {
    return c.vault.CheckpointEngine().Restore(checkpointID, RestoreScope{Full: true})
}

// RollbackEntity restores a single entity
func (c *Client) RollbackEntity(checkpointID, entityID string) error {
    return c.vault.CheckpointEngine().Restore(checkpointID, RestoreScope{EntityID: entityID})
}

// Diff returns changes between two points
func (c *Client) Diff(from, to string) (*DiffResult, error) {
    return c.vault.DiffEngine().Diff(
        parseRef(from),
        parseRef(to),
        DiffOptions{Format: "json"},
    )
}

// DiffSummary returns a human-readable summary of changes
func (c *Client) DiffSummary(from, to string) (string, error) {
    result, err := c.Diff(from, to)
    if err != nil {
        return "", err
    }
    return formatSummary(result), nil
}

// Log returns recent checkpoints
func (c *Client) Log(limit int) ([]Checkpoint, error) {
    return c.vault.CheckpointEngine().List(c.vault.ActiveStream().ID, limit)
}

// Status returns current project status
func (c *Client) Status() (*StatusResult, error) {
    return c.vault.Status()
}

// Explain uses an LLM to explain what changed (requires LLM config)
func (c *Client) Explain(from, to string) (string, error) {
    diff, err := c.Diff(from, to)
    if err != nil {
        return "", err
    }
    return c.vault.Agent().Explain(diff)
}

// Search finds checkpoints matching a query
func (c *Client) Search(query string) ([]Checkpoint, error) {
    return c.vault.CheckpointEngine().Search(query)
}

// Write records a change (used when agent modifies files directly)
func (c *Client) RecordChange(space, path string, content []byte) error {
    // ... creates operation and stores object
}
```

### Usage Example

```go
// An AI agent using Loom for safe refactoring
func refactorAuth(project string) error {
    lm, err := loom.Open(project)
    if err != nil {
        return err
    }

    // Checkpoint before starting
    cp, _ := lm.Checkpoint("before auth refactor",
        loom.WithTag("agent", "refactor-v2"),
        loom.WithTag("scope", "auth"),
    )

    // Do the refactoring work...
    err = performRefactoring()
    if err != nil {
        // Something went wrong — rollback
        lm.Rollback(cp.ID)
        return fmt.Errorf("refactor failed, rolled back: %w", err)
    }

    // Verify the result
    diff, _ := lm.DiffSummary(cp.ID, "HEAD")
    fmt.Println("Changes made:", diff)

    // Checkpoint after success
    lm.Checkpoint("auth refactor complete",
        loom.WithSummary(diff),
    )

    return nil
}
```

## HTTP API

For external agents (Python, JS, any language):

### Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/v1/checkpoint` | Create checkpoint |
| POST | `/api/v1/rollback` | Rollback to checkpoint |
| GET | `/api/v1/diff` | Get diff between refs |
| GET | `/api/v1/log` | Get checkpoint log |
| GET | `/api/v1/status` | Get project status |
| POST | `/api/v1/explain` | LLM-explain a diff |
| GET | `/api/v1/search` | Search checkpoints |
| POST | `/api/v1/record` | Record a file change |

### Create Checkpoint

```http
POST /api/v1/checkpoint
Content-Type: application/json
Authorization: Bearer <token>

{
  "title": "before auth refactor",
  "summary": "Saving state before refactoring the auth module",
  "tags": {
    "agent": "refactor-v2",
    "scope": "auth"
  }
}
```

Response:
```json
{
  "id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "stream_id": "01ARZ3NDEKTSV4RRFFQ69G5FAX",
  "seq": 1234,
  "title": "before auth refactor",
  "timestamp": "2026-03-11T10:15:30.000Z",
  "source": "agent",
  "spaces": [
    {
      "space_id": "code",
      "status": "changed",
      "summary": { "entities_modified": 5, "insertions": 42, "deletions": 13 }
    }
  ]
}
```

### Rollback

```http
POST /api/v1/rollback
Content-Type: application/json

{
  "checkpoint_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "scope": "full"
}
```

Or partial rollback:
```json
{
  "checkpoint_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "scope": "entity",
  "entity_id": "src/auth/login.go"
}
```

### Diff

```http
GET /api/v1/diff?from=HEAD~1&to=HEAD&space=code
```

Response:
```json
{
  "from_seq": 1200,
  "to_seq": 1234,
  "spaces": [
    {
      "space_id": "code",
      "entities": [
        {
          "entity_id": "src/auth/login.go",
          "path": "src/auth/login.go",
          "change": "modified",
          "hunks": [
            {
              "old_start": 40,
              "old_lines": 3,
              "new_start": 40,
              "new_lines": 3,
              "content": "@@ -40,3 +40,3 @@\n-    password := req.FormValue(\"password\")\n+    passphrase := req.FormValue(\"passphrase\")"
            }
          ]
        }
      ]
    }
  ],
  "summary": {
    "spaces_changed": 1,
    "entities_modified": 3,
    "total_operations": 15
  }
}
```

### Status

```http
GET /api/v1/status
```

Response:
```json
{
  "project": "my-app",
  "stream": "main",
  "head_seq": 1234,
  "last_checkpoint": {
    "id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
    "title": "auth refactor complete",
    "seq": 1230,
    "timestamp": "2026-03-11T10:15:30.000Z"
  },
  "pending_ops": 4,
  "spaces": {
    "code": { "entities": 142, "changed_since_checkpoint": 3 },
    "docs": { "entities": 28, "changed_since_checkpoint": 0 },
    "design": { "entities": 15, "changed_since_checkpoint": 1 }
  }
}
```

### Explain

```http
POST /api/v1/explain
Content-Type: application/json

{
  "from": "HEAD~1",
  "to": "HEAD"
}
```

Response:
```json
{
  "explanation": "The auth module was refactored to use passphrases instead of passwords. 3 files were modified in the code space: login.go (renamed variables), auth_test.go (updated test cases), and config.go (added passphrase validation rules). No changes in docs or design.",
  "summary": "Auth: password → passphrase migration",
  "spaces_affected": ["code"],
  "entities_affected": ["src/auth/login.go", "src/auth/auth_test.go", "src/auth/config.go"]
}
```

## LLM Tool Definition

For agents using tool-use / function-calling:

```json
{
  "tools": [
    {
      "name": "loom_checkpoint",
      "description": "Create a named version checkpoint. Use before risky operations or after completing a unit of work.",
      "parameters": {
        "type": "object",
        "properties": {
          "title": {
            "type": "string",
            "description": "Short descriptive title for the checkpoint"
          },
          "summary": {
            "type": "string",
            "description": "Longer description of what changed"
          },
          "tags": {
            "type": "object",
            "description": "Key-value metadata tags"
          }
        },
        "required": ["title"]
      }
    },
    {
      "name": "loom_rollback",
      "description": "Rollback to a previous checkpoint. Creates a guard checkpoint first.",
      "parameters": {
        "type": "object",
        "properties": {
          "checkpoint_id": {
            "type": "string",
            "description": "The checkpoint ID to rollback to"
          },
          "scope": {
            "type": "string",
            "enum": ["full", "space", "entity"],
            "description": "Scope of rollback"
          },
          "space_id": {
            "type": "string",
            "description": "Space ID (required if scope is 'space')"
          },
          "entity_id": {
            "type": "string",
            "description": "Entity path (required if scope is 'entity')"
          }
        },
        "required": ["checkpoint_id"]
      }
    },
    {
      "name": "loom_diff",
      "description": "Get the diff between two points in history. Use to understand what changed.",
      "parameters": {
        "type": "object",
        "properties": {
          "from": { "type": "string", "description": "Start ref (checkpoint ID, HEAD, HEAD~N)" },
          "to": { "type": "string", "description": "End ref (default: HEAD)" },
          "space": { "type": "string", "description": "Filter to one space" },
          "summary_only": { "type": "boolean", "description": "Only return summary, not full diff" }
        },
        "required": ["from"]
      }
    },
    {
      "name": "loom_status",
      "description": "Get current project versioning status.",
      "parameters": { "type": "object", "properties": {} }
    },
    {
      "name": "loom_log",
      "description": "Get recent checkpoints. Use to find a checkpoint to rollback to.",
      "parameters": {
        "type": "object",
        "properties": {
          "limit": { "type": "integer", "description": "Max checkpoints to return (default: 10)" },
          "space": { "type": "string", "description": "Filter by space" },
          "source": { "type": "string", "description": "Filter by source (manual, auto, agent)" }
        }
      }
    },
    {
      "name": "loom_search",
      "description": "Search checkpoints by title or summary.",
      "parameters": {
        "type": "object",
        "properties": {
          "query": { "type": "string", "description": "Search query" }
        },
        "required": ["query"]
      }
    }
  ]
}
```

## Agent API Server

The agent API runs as part of the `loom watch` daemon or standalone:

```bash
# Start with watch daemon
loom watch --agent-api --agent-port 7890

# Or standalone
loom agent-server --port 7890

# Agent connects to localhost:7890
```

```go
func (s *AgentServer) Start(port int) error {
    r := chi.NewRouter()

    r.Post("/api/v1/checkpoint", s.handleCheckpoint)
    r.Post("/api/v1/rollback", s.handleRollback)
    r.Get("/api/v1/diff", s.handleDiff)
    r.Get("/api/v1/log", s.handleLog)
    r.Get("/api/v1/status", s.handleStatus)
    r.Post("/api/v1/explain", s.handleExplain)
    r.Get("/api/v1/search", s.handleSearch)
    r.Post("/api/v1/record", s.handleRecord)

    return http.ListenAndServe(fmt.Sprintf(":%d", port), r)
}
```

## Agent Workflows

### Safe Refactoring

```
1. agent calls loom_checkpoint("before refactor")
2. agent modifies files
3. agent calls loom_diff(from=checkpoint_id, to="HEAD")
4. agent verifies changes look correct
5. agent calls loom_checkpoint("refactor complete")
6. if tests fail → agent calls loom_rollback(checkpoint_id)
```

### Exploratory Coding

```
1. agent calls loom_checkpoint("exploration start")
2. agent tries approach A → doesn't work
3. agent calls loom_rollback(checkpoint_id)
4. agent tries approach B → works
5. agent calls loom_checkpoint("approach B implemented")
```

### Understanding Context

```
1. agent calls loom_log(limit=5) → sees recent history
2. agent calls loom_diff(from="HEAD~3", to="HEAD") → sees recent changes
3. agent calls loom_search("auth") → finds auth-related checkpoints
4. agent now has full context of recent project evolution
```

### Multi-Agent Coordination

```
Agent A:
  1. loom_checkpoint("agent-a: starting task-123")
  2. ... does work ...
  3. loom_checkpoint("agent-a: completed task-123")

Agent B:
  1. loom_log() → sees Agent A's checkpoints
  2. loom_diff(from="agent-a-start", to="agent-a-end") → reviews A's work
  3. loom_checkpoint("agent-b: building on task-123")
```
