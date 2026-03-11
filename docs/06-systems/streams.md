# 06 — Systems: Streams

## Overview

Streams replace Git branches. A stream is a named, live timeline of operations that auto-versions continuously.

Key differences from Git branches:

| Git Branch | Loom Stream |
|------------|-------------|
| Points to a single commit | Points to a sequence position in the op log |
| Static until you commit | Updates continuously as changes happen |
| Requires explicit merge ceremony | Auto-converges via merge engine |
| Diverges permanently until merged | Can stay in sync via continuous merge |
| One branch per working directory | Multiple streams can track the same files |

## Data Model

```go
type Stream struct {
    ID        string  // ULID
    Name      string  // "main", "feature/auth", "agent/refactor-42"
    HeadSeq   int64   // Latest operation sequence
    CreatedAt string
    UpdatedAt string
    ParentID  string  // Parent stream (if forked)
    ForkSeq   int64   // Sequence where this stream diverged
    Status    string  // active, merged, archived
}
```

## Stream Lifecycle

```
Create Stream
  │
  ▼
Active (receiving operations)
  │
  ├─ Fork → New Stream (starts at current HeadSeq)
  │
  ├─ Merge → Operations from another stream converged into this one
  │
  ├─ Archive → Stream frozen, no new operations
  │
  └─ Delete → Stream metadata removed (ops remain in log)
```

## Default Stream

Every project starts with a `main` stream. It's created during `loom init`:

```go
func createMainStream(db *sql.DB) error {
    id := ulid.Make().String()
    _, err := db.Exec(`
        INSERT INTO streams (id, name, head_seq, status)
        VALUES (?, 'main', 0, 'active')`, id)
    return err
}
```

## Forking

Forking creates a new stream that starts at the current head of an existing stream:

```go
func (sm *StreamManager) Fork(parentName, newName string) (*Stream, error) {
    parent, err := sm.GetByName(parentName)
    if err != nil {
        return nil, fmt.Errorf("parent stream %q not found", parentName)
    }

    stream := &Stream{
        ID:        ulid.Make().String(),
        Name:      newName,
        HeadSeq:   parent.HeadSeq,
        ParentID:  parent.ID,
        ForkSeq:   parent.HeadSeq,
        Status:    "active",
    }

    _, err = sm.db.Exec(`
        INSERT INTO streams (id, name, head_seq, parent_id, fork_seq, status)
        VALUES (?, ?, ?, ?, ?, 'active')`,
        stream.ID, stream.Name, stream.HeadSeq, stream.ParentID, stream.ForkSeq)

    return stream, err
}
```

After forking, operations written to the new stream get their own sequence numbers but reference the new stream ID. The parent stream continues independently.

## Stream Routing

When the user writes operations, they go to the active stream:

```go
func (sm *StreamManager) ActiveStream() (*Stream, error) {
    name, err := sm.getActiveStreamName()
    if err != nil {
        return nil, err
    }
    return sm.GetByName(name)
}

// The active stream is stored in metadata
func (sm *StreamManager) Switch(name string) error {
    _, err := sm.GetByName(name) // Verify it exists
    if err != nil {
        return err
    }
    sm.db.Exec("UPDATE metadata SET value = ? WHERE key = 'active_stream'", name)
    return nil
}
```

## Merging Streams

When two streams need to converge:

```
main:       A─B─C─────F─G
                 \     ↑
feature:          D─E──┘ (merge D,E into main)
```

### Merge Process

1. Find the fork point (where the streams diverged)
2. Collect ops from the fork point to the source stream head
3. For each op, check if it conflicts with ops in the target stream
4. Non-conflicting ops are applied directly
5. Conflicting ops go through the merge engine (auto or LLM)
6. A merge checkpoint is created recording the convergence

```go
func (sm *StreamManager) Merge(sourceName, targetName string) (*MergeResult, error) {
    source, _ := sm.GetByName(sourceName)
    target, _ := sm.GetByName(targetName)

    // Find the common ancestor (fork point)
    forkSeq := sm.findForkPoint(source, target)

    // Get ops from fork to each stream's head
    sourceOps, _ := sm.reader.ReadRange(forkSeq, source.HeadSeq)
    targetOps, _ := sm.reader.ReadRange(forkSeq, target.HeadSeq)

    // Categorize by entity
    sourceByEntity := groupByEntity(sourceOps)
    targetByEntity := groupByEntity(targetOps)

    // Merge per entity
    var merged []Operation
    var conflicts []MergeConflict

    for entityID, sOps := range sourceByEntity {
        tOps, exists := targetByEntity[entityID]
        if !exists {
            // No conflict — apply source ops to target
            merged = append(merged, sOps...)
            continue
        }

        // Both streams modified the same entity — use merge engine
        result, err := sm.mergeEngine.MergeEntity(entityID, sOps, tOps, forkSeq)
        if err != nil {
            return nil, err
        }

        if result.OK {
            merged = append(merged, result.Operations...)
        } else {
            conflicts = append(conflicts, result.Conflicts...)
        }
    }

    if len(conflicts) > 0 {
        return &MergeResult{OK: false, Conflicts: conflicts}, nil
    }

    // Write merged ops to target stream
    sm.writer.WriteBatch(merged)

    // Create merge checkpoint
    sm.checkpointEngine.Create(CheckpointInput{
        StreamID: target.ID,
        Title:    fmt.Sprintf("Merge %s into %s", sourceName, targetName),
        Source:   SourceWorkflow,
    })

    // Mark source as merged
    sm.db.Exec("UPDATE streams SET status = 'merged' WHERE id = ?", source.ID)

    return &MergeResult{OK: true, Operations: merged}, nil
}
```

## Stream Naming Conventions

```
main                    # Primary stream
feature/auth            # Feature work
fix/login-bug           # Bug fix
agent/refactor-42       # Created by AI agent (includes agent run ID)
release/v1.0            # Release preparation
experiment/new-ui       # Experimental work
```

Naming rules:
- Lowercase
- Alphanumeric + hyphens + slashes
- Slashes for namespacing (one level)
- Max 100 characters
- `main` is reserved as the default

## Stream Cleanup

Old streams can be archived or deleted:

```bash
# Archive a merged stream (keeps metadata, no new ops)
loom stream archive feature/auth

# Delete a stream (removes metadata, ops stay in log)
loom stream delete experiment/failed

# List all streams
loom stream list

# Show stream info
loom stream info feature/auth
```

## CLI Commands

```bash
# Create a new stream (fork from current)
loom stream create feature/auth

# Switch active stream
loom stream switch feature/auth

# List streams
loom stream list

# Merge a stream into the current stream
loom merge feature/auth

# Show stream info
loom stream info main

# Archive / delete
loom stream archive feature/auth
loom stream delete feature/auth
```
