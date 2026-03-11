# 06 — Systems: Checkpoints

## Overview

Checkpoints are named points on a stream's timeline. They capture the full project state across all spaces at a specific operation sequence.

Think of them as:
- Git commits, but **optional** — the history exists without them
- Google Docs versions, but **explicit** — you can name and tag them
- Save points in a game — you can always restore to them

## Types

| Source | Created By | Example |
|--------|-----------|---------|
| `manual` | User | `loom checkpoint "auth system complete"` |
| `auto` | Loom daemon | Every 50 ops or 5 minutes of activity |
| `agent` | AI agent | `loom.checkpoint("before refactor")` |
| `workflow` | Automation/hooks | Pre-release, pre-deploy |
| `guard` | Loom (safety) | Before restore, before risky operations |
| `restore` | Loom | After any restore operation |

## Creation Flow

```
Trigger (manual / auto / agent / hook)
  │
  ▼
Gather current state from all tracked spaces:
  ├─ code: Git HEAD + working tree status
  ├─ docs: File hashes + modification times
  ├─ design: Structured entity snapshot
  └─ ... (each space via its adapter)
  │
  ▼
Build SpaceState[] array
  │
  ▼
Write Checkpoint to SQLite:
  { id, stream_id, seq, title, summary, spaces, source }
  │
  ▼
Update checkpoint FTS index
  │
  ▼
Notify subscribers
```

### Implementation

```go
type CheckpointEngine struct {
    db       *sql.DB
    reader   *OpReader
    registry *AdapterRegistry
}

type CheckpointInput struct {
    StreamID string
    Title    string
    Summary  string
    Source   CheckpointSource
    Tags     map[string]string
    Author   string
}

func (ce *CheckpointEngine) Create(input CheckpointInput) (*Checkpoint, error) {
    // Get current stream head
    stream, _ := ce.getStream(input.StreamID)
    currentSeq := stream.HeadSeq

    // Get previous checkpoint for this stream
    var parentID string
    var parentSeq int64
    ce.db.QueryRow(
        "SELECT id, seq FROM checkpoints WHERE stream_id = ? ORDER BY seq DESC LIMIT 1",
        input.StreamID,
    ).Scan(&parentID, &parentSeq)

    // Gather state from each space
    spaces := ce.gatherSpaceStates(parentSeq, currentSeq)

    checkpoint := &Checkpoint{
        ID:        ulid.Make().String(),
        StreamID:  input.StreamID,
        Seq:       currentSeq,
        Title:     input.Title,
        Summary:   input.Summary,
        Author:    input.Author,
        Timestamp: time.Now().UTC().Format(time.RFC3339),
        Source:    input.Source,
        Spaces:    spaces,
        Tags:      input.Tags,
        ParentID:  parentID,
    }

    // Write to database
    _, err := ce.db.Exec(`
        INSERT INTO checkpoints (id, stream_id, seq, title, summary, author, timestamp, source, spaces, tags, parent_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        checkpoint.ID, checkpoint.StreamID, checkpoint.Seq,
        checkpoint.Title, checkpoint.Summary, checkpoint.Author,
        checkpoint.Timestamp, checkpoint.Source,
        marshalJSON(checkpoint.Spaces), marshalJSON(checkpoint.Tags),
        checkpoint.ParentID,
    )

    return checkpoint, err
}
```

### Gathering Space States

```go
func (ce *CheckpointEngine) gatherSpaceStates(fromSeq, toSeq int64) []SpaceState {
    var states []SpaceState

    for _, space := range ce.registry.ListSpaces() {
        adapter := ce.registry.GetAdapter(space.ID)

        // Count operations for this space since last checkpoint
        ops, _ := ce.reader.ReadBySpace(space.ID, fromSeq, toSeq)

        if len(ops) == 0 {
            states = append(states, SpaceState{
                SpaceID: space.ID,
                Adapter: space.Adapter,
                Status:  SpaceUnchanged,
            })
            continue
        }

        // Get entity states from the adapter
        entities, _ := adapter.GetEntityStates(ops)

        // Build summary
        summary := buildSummary(ops)

        // Get adapter-specific refs (e.g., git HEAD)
        refs := adapter.GetRefs()

        states = append(states, SpaceState{
            SpaceID:  space.ID,
            Adapter:  space.Adapter,
            Status:   SpaceChanged,
            Summary:  summary,
            Entities: entities,
            Refs:     refs,
        })
    }

    return states
}
```

## Auto-Checkpointing

The auto-checkpoint system runs as a goroutine, evaluating criteria after each operation:

```go
type AutoCheckpointer struct {
    engine    *CheckpointEngine
    opStream  <-chan Operation
    config    AutoCheckpointConfig
    lastSeq   int64
    lastTime  time.Time
    opCount   int
}

type AutoCheckpointConfig struct {
    Enabled            bool
    IntervalOps        int           // Create checkpoint every N ops
    IntervalDuration   time.Duration // Or every N duration of activity
    OnSignificantChange bool        // Detect large changes
    SignificantThreshold int        // Lines changed to trigger
}

func (ac *AutoCheckpointer) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case op := <-ac.opStream:
            ac.opCount++

            // Check: enough operations?
            if ac.config.IntervalOps > 0 && ac.opCount >= ac.config.IntervalOps {
                ac.createAutoCheckpoint(op)
                continue
            }

            // Check: enough time?
            if ac.config.IntervalDuration > 0 && time.Since(ac.lastTime) >= ac.config.IntervalDuration {
                ac.createAutoCheckpoint(op)
                continue
            }

            // Check: significant change?
            if ac.config.OnSignificantChange && ac.isSignificant(op) {
                ac.createAutoCheckpoint(op)
                continue
            }
        }
    }
}

func (ac *AutoCheckpointer) createAutoCheckpoint(lastOp Operation) {
    ac.engine.Create(CheckpointInput{
        StreamID: lastOp.StreamID,
        Title:    ac.generateTitle(),
        Summary:  ac.generateSummary(),
        Source:   SourceAuto,
        Author:   "loom-auto",
    })
    ac.opCount = 0
    ac.lastTime = time.Now()
    ac.lastSeq = lastOp.Seq
}

func (ac *AutoCheckpointer) generateTitle() string {
    // Generate a meaningful title from recent operations
    // e.g., "Auto: modified 3 files in code, 1 in docs"
    // Future: use LLM to generate a natural summary
    return fmt.Sprintf("Auto checkpoint at %s", time.Now().Format("15:04"))
}
```

## Restore

Restoring from a checkpoint means replaying the project state to match that checkpoint's point in time.

### Restore Flow

```
User: loom restore <checkpoint-id> [--space code] [--entity src/main.go]
  │
  ▼
1. Create a guard checkpoint (save current state first)
  │
  ▼
2. Determine restore scope:
   ├─ Full project: all spaces
   ├─ Single space: one space only
   └─ Single entity: one file/item
  │
  ▼
3. For each entity being restored:
   a. Get the entity's ObjectRef at the target checkpoint
   b. Read the object content from the object store
   c. Write the content back to the working directory
   d. Create a "restore" operation in the log
  │
  ▼
4. Create a restore checkpoint:
   "Restored to: [original checkpoint title]"
  │
  ▼
5. Report results
```

### Implementation

```go
func (ce *CheckpointEngine) Restore(checkpointID string, scope RestoreScope) error {
    target, err := ce.Get(checkpointID)
    if err != nil {
        return fmt.Errorf("checkpoint not found: %w", err)
    }

    // 1. Guard checkpoint
    ce.Create(CheckpointInput{
        StreamID: target.StreamID,
        Title:    fmt.Sprintf("Guard before restore to %s", target.Title),
        Source:   SourceGuard,
    })

    // 2. Determine what to restore
    entities := ce.resolveRestoreEntities(target, scope)

    // 3. Restore each entity
    for _, entity := range entities {
        adapter := ce.registry.GetAdapter(entity.SpaceID)

        // Read content from object store
        content, _ := ce.objectStore.Read(entity.ObjectRef)

        // Write to working directory via adapter
        adapter.WriteEntity(entity.Path, content)

        // Record restore operation
        ce.writer.Write(Operation{
            StreamID: target.StreamID,
            SpaceID:  entity.SpaceID,
            EntityID: entity.ID,
            Type:     OpModify,
            Path:     entity.Path,
            ObjectRef: entity.ObjectRef,
            Meta: OpMeta{
                Source: "restore",
                Labels: map[string]string{
                    "restored_from": checkpointID,
                },
            },
        })
    }

    // 4. Restore checkpoint
    ce.Create(CheckpointInput{
        StreamID: target.StreamID,
        Title:    fmt.Sprintf("Restored to: %s", target.Title),
        Source:   SourceRestore,
        Tags:     map[string]string{"restored_from": checkpointID},
    })

    return nil
}

type RestoreScope struct {
    Full     bool     // Restore everything
    SpaceID  string   // Restore one space
    EntityID string   // Restore one entity
}
```

## Checkpoint Querying

```go
// Get a checkpoint by ID
func (ce *CheckpointEngine) Get(id string) (*Checkpoint, error)

// List checkpoints for a stream
func (ce *CheckpointEngine) List(streamID string, limit int) ([]Checkpoint, error)

// Search checkpoints by title/summary (full-text)
func (ce *CheckpointEngine) Search(query string) ([]Checkpoint, error)

// Get checkpoints by source type
func (ce *CheckpointEngine) ListBySource(source CheckpointSource) ([]Checkpoint, error)

// Get the checkpoint chain (parent → child)
func (ce *CheckpointEngine) Chain(checkpointID string) ([]Checkpoint, error)
```

## CLI Commands

```bash
# Create manual checkpoint
loom checkpoint "auth system complete"
loom checkpoint "before redesign" --tag release=v2.0

# List checkpoints
loom log                              # All checkpoints on current stream
loom log --space code                 # Filter by space
loom log --source auto                # Filter by source
loom log --since "2026-03-01"         # Filter by date
loom log --search "auth"              # Full-text search

# Show checkpoint details
loom show <checkpoint-id>

# Diff between checkpoints
loom diff <checkpoint-a> <checkpoint-b>

# Restore
loom restore <checkpoint-id>                    # Full project
loom restore <checkpoint-id> --space code       # One space
loom restore <checkpoint-id> --entity src/main.go  # One file

# Auto-checkpoint config
# Configured in .loom/config.toml [checkpoint] section
```

## Retention

Old auto-checkpoints can be pruned to save space:

```toml
# .loom/config.toml
[checkpoint.retention]
auto_max_age_days = 30     # Delete auto checkpoints older than 30 days
auto_max_count = 1000      # Keep at most 1000 auto checkpoints
manual_max_age_days = 365  # Keep manual checkpoints for a year
agent_max_age_days = 90    # Keep agent checkpoints for 90 days
```

Pruning never deletes:
- Manual checkpoints within retention
- The most recent checkpoint per stream
- Checkpoints referenced by sync history
- Guard/restore checkpoints (audit trail)
