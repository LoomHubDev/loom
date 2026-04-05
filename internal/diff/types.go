package diff

import "github.com/constructspace/loom/internal/core"

// DiffResult is the top-level diff output.
type DiffResult struct {
	FromSeq int64       `json:"from_seq"`
	ToSeq   int64       `json:"to_seq"`
	FromRef string      `json:"from_ref"`
	ToRef   string      `json:"to_ref"`
	Spaces  []SpaceDiff `json:"spaces"`
	Summary DiffSummary `json:"summary"`
}

// DiffSummary summarizes a diff across all spaces.
type DiffSummary struct {
	SpacesChanged    int `json:"spaces_changed"`
	EntitiesCreated  int `json:"entities_created"`
	EntitiesModified int `json:"entities_modified"`
	EntitiesDeleted  int `json:"entities_deleted"`
	TotalOperations  int `json:"total_operations"`
}

// SpaceDiff shows changes within a single space.
type SpaceDiff struct {
	SpaceID  string            `json:"space_id"`
	Entities []EntityDiff      `json:"entities"`
	Summary  core.SpaceSummary `json:"summary"`
}

// EntityDiff shows changes to a single entity.
type EntityDiff struct {
	EntityID string          `json:"entity_id"`
	Path     string          `json:"path"`
	Change   core.ChangeType `json:"change"`
	Hunks    []DiffHunk      `json:"hunks,omitempty"`
	Summary  string          `json:"summary,omitempty"`
}

// DiffHunk represents a contiguous block of changes.
type DiffHunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}

// DiffLine is a single line in a hunk.
type DiffLine struct {
	Type    string `json:"type"` // "add", "delete", "context"
	Content string `json:"content"`
}

// DiffOptions controls diff behavior.
type DiffOptions struct {
	SpaceID  string `json:"space_id,omitempty"`
	EntityID string `json:"entity_id,omitempty"`
	Format   string `json:"format,omitempty"`  // "text", "json"
	Context  int    `json:"context,omitempty"` // lines of context
	Summary  bool   `json:"summary,omitempty"` // summary only
}
