# 04 — Data Models

## Conventions

- All IDs are ULIDs (time-sortable, lexicographically ordered) unless otherwise noted
- Timestamps are RFC 3339 / ISO 8601 (`2026-03-11T10:15:30.000Z`)
- Hashes are lowercase hex SHA-256 (64 characters)
- Sequence numbers are monotonically increasing `int64`
- All structs have JSON tags for serialization
- Nullable fields use pointer types (`*string`, `*int64`)
- Maps use `string` keys for JSON compatibility

## Entity Relationship

```
Project
  └── has many Streams
        └── has many Operations (ordered by sequence)
              └── references Entities (by space + path)
              └── may reference Objects (by hash)
        └── has many Checkpoints (point to an operation sequence)

Space (registered in config)
  └── has one Adapter
  └── tracks many Entities

Remote
  └── syncs Streams (operations + objects)
```

## Core Types

### Operation

The atomic unit of change. Every modification in the project becomes one or more operations.

```go
type Operation struct {
    ID        string    `json:"id"`         // ULID
    Seq       int64     `json:"seq"`        // Monotonic sequence number (per project)
    StreamID  string    `json:"stream_id"`  // Which stream this belongs to
    SpaceID   string    `json:"space_id"`   // Which space (code, docs, design, ...)
    EntityID  string    `json:"entity_id"`  // Unique entity identifier (usually path)
    Type      OpType    `json:"type"`       // Operation type
    Path      string    `json:"path"`       // Path within the space
    Delta     []byte    `json:"delta"`      // The change payload (format depends on adapter)
    ObjectRef string    `json:"object_ref"` // SHA-256 of the new content (if applicable)
    ParentSeq int64     `json:"parent_seq"` // Previous operation sequence (for ordering)
    Author    string    `json:"author"`     // Author identifier
    Timestamp string    `json:"timestamp"`  // RFC 3339
    Meta      OpMeta    `json:"meta"`       // Additional metadata
}

type OpType string

const (
    OpCreate  OpType = "create"   // New entity
    OpModify  OpType = "modify"   // Content changed
    OpDelete  OpType = "delete"   // Entity removed
    OpMove    OpType = "move"     // Entity relocated (path changed)
    OpRename  OpType = "rename"   // Entity renamed (same location)
    OpMeta    OpType = "meta"     // Metadata-only change (permissions, etc.)
)

type OpMeta struct {
    OldPath     string            `json:"old_path,omitempty"`     // For move/rename
    Size        int64             `json:"size,omitempty"`         // Content size in bytes
    ContentType string            `json:"content_type,omitempty"` // MIME type
    Checksum    string            `json:"checksum,omitempty"`     // Content hash before change
    Source      string            `json:"source,omitempty"`       // "user", "agent", "auto", "watch"
    AgentID     string            `json:"agent_id,omitempty"`     // If created by an agent
    Labels      map[string]string `json:"labels,omitempty"`       // Arbitrary key-value labels
}
```

### Delta Formats

The `Delta` field format depends on the operation type and space adapter:

| Space | OpCreate | OpModify | OpDelete |
|-------|----------|----------|----------|
| code | nil (content in ObjectRef) | text diff (unified) | nil |
| docs | nil (content in ObjectRef) | text diff (unified) | nil |
| design | JSON snapshot | JSON patch (RFC 6902) | nil |
| data | JSON snapshot | JSON patch (RFC 6902) | nil |
| config | nil (content in ObjectRef) | text diff or JSON patch | nil |
| binary | nil (content in ObjectRef) | nil (full replace via ObjectRef) | nil |

For text files, the delta is a unified diff:
```
@@ -42,1 +42,1 @@
-    password := req.FormValue("password")
+    passphrase := req.FormValue("passphrase")
```

For structured files (JSON, YAML), the delta is a JSON patch:
```json
[
  { "op": "replace", "path": "/auth/method", "value": "oauth2" },
  { "op": "add", "path": "/auth/providers/-", "value": "github" }
]
```

### Checkpoint

A named point on a stream. Captures the state across all spaces at a specific operation sequence.

```go
type Checkpoint struct {
    ID        string            `json:"id"`          // ULID
    StreamID  string            `json:"stream_id"`   // Which stream
    Seq       int64             `json:"seq"`          // Operation sequence this checkpoint is at
    Title     string            `json:"title"`        // Human-readable name
    Summary   string            `json:"summary"`      // Description of changes
    Author    string            `json:"author"`       // Who created it
    Timestamp string            `json:"timestamp"`    // RFC 3339
    Source    CheckpointSource  `json:"source"`       // How it was created
    Spaces    []SpaceState      `json:"spaces"`       // State of each space at this point
    Tags      map[string]string `json:"tags"`         // Arbitrary metadata
    ParentID  string            `json:"parent_id"`    // Previous checkpoint ID (for chain)
}

type CheckpointSource string

const (
    SourceManual    CheckpointSource = "manual"    // User created
    SourceAuto      CheckpointSource = "auto"      // Auto-checkpoint (timer/threshold)
    SourceAgent     CheckpointSource = "agent"     // Created by AI agent
    SourceWorkflow  CheckpointSource = "workflow"  // Created by automation/hook
    SourceGuard     CheckpointSource = "guard"     // Before risky operation
    SourceRestore   CheckpointSource = "restore"   // After a restore operation
)
```

### SpaceState

The state of a single space at a checkpoint.

```go
type SpaceState struct {
    SpaceID   string            `json:"space_id"`    // Space identifier
    Adapter   string            `json:"adapter"`     // Adapter type used
    Status    SpaceStatus       `json:"status"`      // Changed or unchanged since last checkpoint
    Summary   SpaceSummary      `json:"summary"`     // Change summary
    Entities  []EntityState     `json:"entities"`    // Individual entity states
    Refs      map[string]string `json:"refs"`        // Adapter-specific refs (e.g., git HEAD)
}

type SpaceStatus string

const (
    SpaceChanged   SpaceStatus = "changed"
    SpaceUnchanged SpaceStatus = "unchanged"
)

type SpaceSummary struct {
    EntitiesCreated  int `json:"entities_created"`
    EntitiesModified int `json:"entities_modified"`
    EntitiesDeleted  int `json:"entities_deleted"`
    Insertions       int `json:"insertions,omitempty"`  // Lines added (text spaces)
    Deletions        int `json:"deletions,omitempty"`   // Lines removed (text spaces)
}
```

### EntityState

The state of a single entity (file, document, design node, etc.) at a checkpoint.

```go
type EntityState struct {
    ID        string       `json:"id"`          // Entity identifier (usually path)
    SpaceID   string       `json:"space_id"`    // Which space
    Kind      string       `json:"kind"`        // Entity kind (file, document, component, schema, ...)
    Path      string       `json:"path"`        // Path within the space
    Change    ChangeType   `json:"change"`      // What changed
    ObjectRef string       `json:"object_ref"`  // SHA-256 of current content
    Size      int64        `json:"size"`        // Content size in bytes
    ModTime   string       `json:"mod_time"`    // Last modification time
    Meta      map[string]string `json:"meta"`   // Entity-specific metadata
}

type ChangeType string

const (
    ChangeCreated  ChangeType = "created"
    ChangeModified ChangeType = "modified"
    ChangeDeleted  ChangeType = "deleted"
    ChangeMoved    ChangeType = "moved"
    ChangeNone     ChangeType = "none"
)
```

### Stream

A named, live timeline of operations.

```go
type Stream struct {
    ID        string  `json:"id"`          // ULID
    Name      string  `json:"name"`        // Human-readable (e.g., "main", "feature/auth")
    HeadSeq   int64   `json:"head_seq"`    // Latest operation sequence number
    CreatedAt string  `json:"created_at"`  // RFC 3339
    UpdatedAt string  `json:"updated_at"`  // RFC 3339
    ParentID  string  `json:"parent_id"`   // Parent stream (if forked)
    ForkSeq   int64   `json:"fork_seq"`    // Sequence number where this stream forked
    Status    string  `json:"status"`      // "active", "merged", "archived"
}
```

### Object

Content-addressed blob in the object store.

```go
type Object struct {
    Hash        string `json:"hash"`         // SHA-256 hex
    Size        int64  `json:"size"`         // Uncompressed size in bytes
    Compressed  bool   `json:"compressed"`   // Whether stored compressed (zstd)
    ContentType string `json:"content_type"` // MIME type
    CreatedAt   string `json:"created_at"`   // RFC 3339
}
```

Objects are stored on disk at `.loom/objects/<first-2-hex>/<remaining-62-hex>`.

Hash computation:
```go
func HashContent(content []byte) string {
    h := sha256.New()
    h.Write([]byte("blob:"))
    h.Write([]byte(strconv.Itoa(len(content))))
    h.Write([]byte{0}) // null separator
    h.Write(content)
    return hex.EncodeToString(h.Sum(nil))
}
```

### Space

A registered content domain.

```go
type Space struct {
    ID          string   `json:"id"`           // Unique identifier (e.g., "code", "docs")
    Name        string   `json:"name"`         // Display name
    Adapter     string   `json:"adapter"`      // Adapter type (git, filesystem, design, ...)
    Path        string   `json:"path"`         // Root path within the project
    Enabled     bool     `json:"enabled"`      // Whether tracking is active
    IgnoreRules []string `json:"ignore_rules"` // Space-specific ignore patterns
}
```

### Remote

A server endpoint for syncing streams.

```go
type Remote struct {
    Name    string `json:"name"`     // Alias (e.g., "origin")
    URL     string `json:"url"`      // Server URL
    Token   string `json:"-"`        // Auth token (not serialized to JSON)
    Default bool   `json:"default"`  // Is this the default push/pull target
}
```

### DiffResult

The output of a diff operation between two points.

```go
type DiffResult struct {
    FromSeq    int64         `json:"from_seq"`     // Starting sequence
    ToSeq      int64         `json:"to_seq"`       // Ending sequence
    FromRef    string        `json:"from_ref"`     // Checkpoint ID or "HEAD"
    ToRef      string        `json:"to_ref"`       // Checkpoint ID or "HEAD"
    Spaces     []SpaceDiff   `json:"spaces"`       // Per-space diffs
    Summary    DiffSummary   `json:"summary"`      // Aggregate summary
}

type SpaceDiff struct {
    SpaceID   string       `json:"space_id"`
    Entities  []EntityDiff `json:"entities"`
    Summary   SpaceSummary `json:"summary"`
}

type EntityDiff struct {
    EntityID    string     `json:"entity_id"`
    Path        string     `json:"path"`
    Change      ChangeType `json:"change"`
    Hunks       []DiffHunk `json:"hunks,omitempty"`        // For text diffs
    Patches     []byte     `json:"patches,omitempty"`      // For structured diffs (JSON patch)
    OldObjectRef string    `json:"old_object_ref,omitempty"`
    NewObjectRef string    `json:"new_object_ref,omitempty"`
}

type DiffHunk struct {
    OldStart int    `json:"old_start"`
    OldLines int    `json:"old_lines"`
    NewStart int    `json:"new_start"`
    NewLines int    `json:"new_lines"`
    Content  string `json:"content"`
}

type DiffSummary struct {
    SpacesChanged    int `json:"spaces_changed"`
    EntitiesCreated  int `json:"entities_created"`
    EntitiesModified int `json:"entities_modified"`
    EntitiesDeleted  int `json:"entities_deleted"`
    TotalOperations  int `json:"total_operations"`
}
```

### MergeResult

The output of merging two streams.

```go
type MergeResult struct {
    OK           bool             `json:"ok"`            // Whether merge succeeded
    Strategy     string           `json:"strategy"`      // "auto", "llm", "manual"
    Operations   []Operation      `json:"operations"`    // Resulting merged operations
    Suggestions  []MergeSuggestion `json:"suggestions"`  // LLM suggestions (if any)
    Conflicts    []MergeConflict  `json:"conflicts"`     // Only if OK=false and no LLM
}

type MergeSuggestion struct {
    EntityID    string `json:"entity_id"`
    Description string `json:"description"` // LLM explanation of what it resolved
    Confidence  float64 `json:"confidence"` // 0.0 to 1.0
    Applied     bool   `json:"applied"`     // Whether auto-applied (high confidence)
}

type MergeConflict struct {
    EntityID  string `json:"entity_id"`
    SpaceID   string `json:"space_id"`
    OursOp    Operation `json:"ours"`
    TheirsOp  Operation `json:"theirs"`
    BaseRef   string    `json:"base_ref"` // Common ancestor object
}
```

## Enums

```go
// All enum types are string-based for JSON serialization and readability

type OpType string       // create, modify, delete, move, rename, meta
type ChangeType string   // created, modified, deleted, moved, none
type SpaceStatus string  // changed, unchanged
type CheckpointSource string // manual, auto, agent, workflow, guard, restore
```

## SQLite Schema

See `05-storage-schema.md` for the full SQLite schema definition.

## Serialization

### JSON

All types serialize to JSON for:
- Agent API responses
- Sync protocol wire format
- Export/import

### Binary (Future)

For performance-critical paths (sync protocol, large operation batches), a binary format (Protocol Buffers or MessagePack) may be introduced. JSON remains the primary format for v1.

## ID Generation

### ULID (Primary IDs)

ULIDs are used for Operations, Checkpoints, Streams:
- Time-sortable (lexicographic order = chronological order)
- 128-bit, Crockford Base32 encoded (26 characters)
- Monotonic within same millisecond
- Example: `01ARZ3NDEKTSV4RRFFQ69G5FAV`

```go
import "github.com/oklog/ulid/v2"

func NewID() string {
    return ulid.Make().String()
}
```

### SHA-256 (Content Hashes)

Used for object store content addressing:
- 256-bit, lowercase hex (64 characters)
- Deterministic from content
- Example: `e3b0c44298fc1c149afbf4c8996fb924...`

### Sequence Numbers (Operation Ordering)

`int64`, monotonically increasing per project:
- Starts at 1
- Never reused
- Gaps are allowed (e.g., after compaction)
- Used for range queries and sync negotiation
