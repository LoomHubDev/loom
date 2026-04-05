package core

import (
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
)

// NewID generates a new ULID.
func NewID() string {
	return ulid.Make().String()
}

// Now returns the current time in RFC3339 format.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// --- Operation Types ---

type OpType string

const (
	OpCreate OpType = "create"
	OpModify OpType = "modify"
	OpDelete OpType = "delete"
	OpMove   OpType = "move"
	OpRename OpType = "rename"
)

type Operation struct {
	ID        string `json:"id"`
	Seq       int64  `json:"seq"`
	StreamID  string `json:"stream_id"`
	SpaceID   string `json:"space_id"`
	EntityID  string `json:"entity_id"`
	Type      OpType `json:"type"`
	Path      string `json:"path"`
	Delta     []byte `json:"delta,omitempty"`
	ObjectRef string `json:"object_ref,omitempty"`
	ParentSeq int64  `json:"parent_seq"`
	Author    string `json:"author"`
	Timestamp string `json:"timestamp"`
	Meta      OpMeta `json:"meta"`
}

type OpMeta struct {
	OldPath     string            `json:"old_path,omitempty"`
	Size        int64             `json:"size,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Checksum    string            `json:"checksum,omitempty"`
	Source      string            `json:"source,omitempty"`
	AgentID     string            `json:"agent_id,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// --- Checkpoint Types ---

type CheckpointSource string

const (
	SourceManual   CheckpointSource = "manual"
	SourceAuto     CheckpointSource = "auto"
	SourceAgent    CheckpointSource = "agent"
	SourceWorkflow CheckpointSource = "workflow"
	SourceGuard    CheckpointSource = "guard"
	SourceRestore  CheckpointSource = "restore"
	SourceWeave    CheckpointSource = "weave"
)

type Checkpoint struct {
	ID        string            `json:"id"`
	StreamID  string            `json:"stream_id"`
	Seq       int64             `json:"seq"`
	Title     string            `json:"title"`
	Summary   string            `json:"summary"`
	Author    string            `json:"author"`
	Timestamp string            `json:"timestamp"`
	Source    CheckpointSource  `json:"source"`
	Spaces    []SpaceState      `json:"spaces"`
	Tags      map[string]string `json:"tags,omitempty"`
	ParentID  string            `json:"parent_id,omitempty"`
}

type SpaceState struct {
	SpaceID  string            `json:"space_id"`
	Adapter  string            `json:"adapter"`
	Status   SpaceStatus       `json:"status"`
	Summary  SpaceSummary      `json:"summary"`
	Entities []EntityState     `json:"entities,omitempty"`
	Refs     map[string]string `json:"refs,omitempty"`
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
	Insertions       int `json:"insertions,omitempty"`
	Deletions        int `json:"deletions,omitempty"`
}

// --- Entity Types ---

type EntityState struct {
	ID        string            `json:"id"`
	SpaceID   string            `json:"space_id"`
	Kind      string            `json:"kind"`
	Path      string            `json:"path"`
	Change    ChangeType        `json:"change"`
	ObjectRef string            `json:"object_ref,omitempty"`
	Size      int64             `json:"size"`
	ModTime   string            `json:"mod_time,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

type ChangeType string

const (
	ChangeCreated  ChangeType = "created"
	ChangeModified ChangeType = "modified"
	ChangeDeleted  ChangeType = "deleted"
	ChangeMoved    ChangeType = "moved"
	ChangeNone     ChangeType = "none"
)

// --- Stream Types ---

type Stream struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	HeadSeq   int64  `json:"head_seq"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	ParentID  string `json:"parent_id,omitempty"`
	ForkSeq   int64  `json:"fork_seq,omitempty"`
	Status    string `json:"status"`
}

// --- Space Types ---

type Space struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Adapter string `json:"adapter"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

// --- Config ---

type ProjectConfig struct {
	Project ProjectInfo            `toml:"project" json:"project"`
	Author  AuthorInfo             `toml:"author" json:"author"`
	Spaces  map[string]SpaceConfig `toml:"spaces" json:"spaces"`
	Watch   WatchConfig            `toml:"watch" json:"watch"`
	CPoint  CheckpointConfig       `toml:"checkpoint" json:"checkpoint"`
}

type ProjectInfo struct {
	Name    string `toml:"name" json:"name"`
	Version int    `toml:"version" json:"version"`
}

type AuthorInfo struct {
	Name  string `toml:"name" json:"name"`
	Email string `toml:"email" json:"email"`
}

type SpaceConfig struct {
	Adapter string `toml:"adapter" json:"adapter"`
	Path    string `toml:"path" json:"path"`
}

type WatchConfig struct {
	Enabled    bool     `toml:"enabled" json:"enabled"`
	DebounceMs int     `toml:"debounce_ms" json:"debounce_ms"`
	Ignore     []string `toml:"ignore" json:"ignore"`
}

type CheckpointConfig struct {
	Auto              bool `toml:"auto" json:"auto"`
	IntervalOps       int  `toml:"interval_ops" json:"interval_ops"`
	IntervalSeconds   int  `toml:"interval_seconds" json:"interval_seconds"`
	OnSignificantChange bool `toml:"on_significant_change" json:"on_significant_change"`
}

// --- Helpers ---

func MarshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
