package loom

import (
	"fmt"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/diff"
)

// Client is the public API for embedding Loom in Go programs and AI agents.
type Client struct {
	vault *core.Vault
}

// CheckpointOption is a functional option for creating checkpoints.
type CheckpointOption func(*core.CheckpointInput)

// StatusResult holds the current project status.
type StatusResult struct {
	Project        string           `json:"project"`
	Stream         string           `json:"stream"`
	HeadSeq        int64            `json:"head_seq"`
	LastCheckpoint *core.Checkpoint `json:"last_checkpoint,omitempty"`
	PendingOps     int              `json:"pending_ops"`
	Spaces         map[string]int   `json:"spaces"`
}

// Open opens a Loom project at the given path.
// The path must point to an initialized Loom project (contains a .loom directory).
func Open(projectPath string) (*Client, error) {
	vault, err := core.OpenVault(projectPath)
	if err != nil {
		return nil, fmt.Errorf("open vault: %w", err)
	}
	return &Client{vault: vault}, nil
}

// Close releases all resources held by the client.
func (c *Client) Close() error {
	return c.vault.Close()
}

// Checkpoint creates a new checkpoint at the current stream head.
// The source is always set to SourceAgent and the default author is "agent".
// Use WithAuthor, WithSummary, and WithTag to customize the checkpoint.
func (c *Client) Checkpoint(title string, opts ...CheckpointOption) (*core.Checkpoint, error) {
	stream, err := c.vault.ActiveStream()
	if err != nil {
		return nil, fmt.Errorf("get active stream: %w", err)
	}

	input := core.CheckpointInput{
		StreamID: stream.ID,
		Title:    title,
		Author:   "agent",
		Source:   core.SourceAgent,
	}
	for _, opt := range opts {
		opt(&input)
	}

	cp, err := c.vault.Checkpoints.Create(input)
	if err != nil {
		return nil, fmt.Errorf("create checkpoint: %w", err)
	}
	return cp, nil
}

// Rollback restores the project to the state at the given checkpoint (full restore).
func (c *Client) Rollback(checkpointID string) error {
	engine := core.NewRestoreEngine(c.vault)
	_, err := engine.Restore(checkpointID, core.RestoreScope{Full: true})
	if err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	return nil
}

// RollbackEntity restores a single entity to its state at the given checkpoint.
func (c *Client) RollbackEntity(checkpointID, entityID string) error {
	engine := core.NewRestoreEngine(c.vault)
	_, err := engine.Restore(checkpointID, core.RestoreScope{EntityID: entityID})
	if err != nil {
		return fmt.Errorf("rollback entity: %w", err)
	}
	return nil
}

// Diff computes the diff between two refs (e.g. "0", "HEAD", checkpoint IDs, or sequences).
func (c *Client) Diff(from, to string) (*diff.DiffResult, error) {
	engine := diff.NewDiffEngine(c.vault.DB, c.vault.OpReader, c.vault.Store)
	fromRef := diff.ParseRef(from)
	toRef := diff.ParseRef(to)
	result, err := engine.Diff(fromRef, toRef, diff.DiffOptions{})
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	return result, nil
}

// DiffSummary returns a human-readable summary of the diff between two refs.
func (c *Client) DiffSummary(from, to string) (string, error) {
	result, err := c.Diff(from, to)
	if err != nil {
		return "", err
	}
	return diff.FormatTerminal(result, false), nil
}

// Log returns the most recent checkpoints for the active stream.
func (c *Client) Log(limit int) ([]core.Checkpoint, error) {
	stream, err := c.vault.ActiveStream()
	if err != nil {
		return nil, fmt.Errorf("get active stream: %w", err)
	}
	checkpoints, err := c.vault.Checkpoints.List(stream.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}
	return checkpoints, nil
}

// Status returns the current project status including stream info, pending ops, and space counts.
func (c *Client) Status() (*StatusResult, error) {
	stream, err := c.vault.ActiveStream()
	if err != nil {
		return nil, fmt.Errorf("get active stream: %w", err)
	}

	head, err := c.vault.OpReader.Head()
	if err != nil {
		return nil, fmt.Errorf("get head: %w", err)
	}

	spaces, err := c.vault.EntityCount()
	if err != nil {
		return nil, fmt.Errorf("entity count: %w", err)
	}

	// Find pending ops since last checkpoint
	lastCheckpointSeq := c.vault.Checkpoints.LatestSeq(stream.ID)
	pendingOps := int(head - lastCheckpointSeq)
	if pendingOps < 0 {
		pendingOps = 0
	}

	result := &StatusResult{
		Project:    c.vault.Config.Project.Name,
		Stream:     stream.Name,
		HeadSeq:    head,
		PendingOps: pendingOps,
		Spaces:     spaces,
	}

	// Attach last checkpoint if any
	checkpoints, err := c.vault.Checkpoints.List(stream.ID, 1)
	if err == nil && len(checkpoints) > 0 {
		result.LastCheckpoint = &checkpoints[0]
	}

	return result, nil
}

// Search performs full-text search across checkpoint titles and summaries.
func (c *Client) Search(query string) ([]core.Checkpoint, error) {
	results, err := c.vault.Checkpoints.Search(query)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return results, nil
}

// WithSummary sets the summary on a checkpoint.
func WithSummary(s string) CheckpointOption {
	return func(input *core.CheckpointInput) {
		input.Summary = s
	}
}

// WithTag adds a key/value tag to a checkpoint.
func WithTag(key, value string) CheckpointOption {
	return func(input *core.CheckpointInput) {
		if input.Tags == nil {
			input.Tags = make(map[string]string)
		}
		input.Tags[key] = value
	}
}

// WithAuthor overrides the default author ("agent") on a checkpoint.
func WithAuthor(author string) CheckpointOption {
	return func(input *core.CheckpointInput) {
		input.Author = author
	}
}

// RecordChange records a file change made by an agent.
func (c *Client) RecordChange(spaceID, path string, content []byte) error {
	stream, err := c.vault.ActiveStream()
	if err != nil {
		return err
	}
	hash, err := c.vault.Store.Write(content, "")
	if err != nil {
		return fmt.Errorf("store content: %w", err)
	}
	_, err = c.vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   spaceID,
		EntityID:  path,
		Type:      core.OpModify,
		Path:      path,
		ObjectRef: hash,
		Author:    "agent",
		Meta: core.OpMeta{
			Size:   int64(len(content)),
			Source: "agent",
		},
	})
	return err
}

// Explain returns a human-readable summary of changes between two refs.
func (c *Client) Explain(from, to string) (string, error) {
	result, err := c.Diff(from, to)
	if err != nil {
		return "", err
	}
	return buildExplanation(result), nil
}

// buildExplanation builds a natural language summary from a DiffResult.
func buildExplanation(result *diff.DiffResult) string {
	if len(result.Spaces) == 0 {
		return "No changes detected."
	}

	var parts []string
	for _, space := range result.Spaces {
		if len(space.Entities) == 0 {
			continue
		}

		created := make([]string, 0)
		modified := make([]string, 0)
		deleted := make([]string, 0)

		for _, e := range space.Entities {
			switch e.Change {
			case core.ChangeCreated:
				created = append(created, e.Path)
			case core.ChangeModified:
				modified = append(modified, e.Path)
			case core.ChangeDeleted:
				deleted = append(deleted, e.Path)
			}
		}

		var spaceParts []string
		if len(created) > 0 {
			noun := "file"
			if len(created) > 1 {
				noun = "files"
			}
			spaceParts = append(spaceParts, fmt.Sprintf("%d %s created in %s space: %s",
				len(created), noun, space.SpaceID, joinPaths(created)))
		}
		if len(modified) > 0 {
			noun := "file"
			if len(modified) > 1 {
				noun = "files"
			}
			spaceParts = append(spaceParts, fmt.Sprintf("%d %s modified in %s space: %s",
				len(modified), noun, space.SpaceID, joinPaths(modified)))
		}
		if len(deleted) > 0 {
			noun := "file"
			if len(deleted) > 1 {
				noun = "files"
			}
			spaceParts = append(spaceParts, fmt.Sprintf("%d %s deleted in %s space: %s",
				len(deleted), noun, space.SpaceID, joinPaths(deleted)))
		}
		parts = append(parts, spaceParts...)
	}

	if len(parts) == 0 {
		return "No changes detected."
	}

	out := ""
	for i, p := range parts {
		if i == 0 {
			out = p
		} else {
			out += ". " + p
		}
	}
	out += "."
	return out
}

// joinPaths joins file paths with commas.
func joinPaths(paths []string) string {
	result := ""
	for i, p := range paths {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
