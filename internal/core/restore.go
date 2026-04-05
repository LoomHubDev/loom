package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// RestoreScope controls the scope of a restore operation.
type RestoreScope struct {
	Full     bool
	SpaceID  string // restore only this space
	EntityID string // restore only this entity
}

// entityState captures the state of an entity at a point in time.
type entityState struct {
	SpaceID   string
	EntityID  string
	Path      string
	ObjectRef string
	Type      OpType
}

// RestoreEngine handles restoring project state to a previous checkpoint.
type RestoreEngine struct {
	vault *Vault
}

// NewRestoreEngine creates a new RestoreEngine.
func NewRestoreEngine(vault *Vault) *RestoreEngine {
	return &RestoreEngine{vault: vault}
}

// Restore restores the project to the state at the given checkpoint.
// It creates a guard checkpoint before restoring, applies file changes,
// writes restore operations, and creates a restore checkpoint.
func (re *RestoreEngine) Restore(checkpointID string, scope RestoreScope) (*Checkpoint, error) {
	// 1. Get target checkpoint
	target, err := re.vault.Checkpoints.Get(checkpointID)
	if err != nil {
		return nil, fmt.Errorf("get target checkpoint: %w", err)
	}

	// 2. Get active stream
	stream, err := re.vault.ActiveStream()
	if err != nil {
		return nil, fmt.Errorf("get active stream: %w", err)
	}

	// 3. Create guard checkpoint
	author := re.vault.Config.Author.Name
	_, err = re.vault.Checkpoints.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    fmt.Sprintf("Guard before restore to %s", target.Title),
		Author:   author,
		Source:   SourceGuard,
		Tags:     map[string]string{"restore_target": checkpointID},
	})
	if err != nil {
		return nil, fmt.Errorf("create guard checkpoint: %w", err)
	}

	// 4. Get entity states at the target checkpoint's seq
	targetStates, err := re.entityStatesAt(stream.ID, target.Seq)
	if err != nil {
		return nil, fmt.Errorf("get target entity states: %w", err)
	}

	// 5. Get current entity states (at current head)
	head, err := re.vault.OpReader.Head()
	if err != nil {
		return nil, fmt.Errorf("get current head: %w", err)
	}
	currentStates, err := re.entityStatesAt(stream.ID, head)
	if err != nil {
		return nil, fmt.Errorf("get current entity states: %w", err)
	}

	// 6. Restore entities that existed at the target checkpoint
	for key, ts := range targetStates {
		// Filter by scope
		if !re.matchesScope(scope, ts) {
			continue
		}

		// Skip entities that were deleted at the target checkpoint
		if ts.Type == OpDelete {
			continue
		}

		// Read content from object store
		if ts.ObjectRef == "" {
			continue
		}

		content, err := re.vault.Store.Read(ts.ObjectRef)
		if err != nil {
			return nil, fmt.Errorf("read object %s for entity %s: %w", ts.ObjectRef, ts.EntityID, err)
		}

		// Determine file path using space config
		filePath, err := re.resolveFilePath(ts.SpaceID, ts.Path)
		if err != nil {
			return nil, fmt.Errorf("resolve path for entity %s: %w", ts.EntityID, err)
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return nil, fmt.Errorf("create dir for %s: %w", filePath, err)
		}

		// Write content to disk
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			return nil, fmt.Errorf("write file %s: %w", filePath, err)
		}

		// Determine op type based on current state
		opType := OpModify
		if cs, exists := currentStates[key]; !exists || cs.Type == OpDelete {
			opType = OpCreate
		}

		// Write restore operation
		_, err = re.vault.OpWriter.Write(Operation{
			StreamID:  stream.ID,
			SpaceID:   ts.SpaceID,
			EntityID:  ts.EntityID,
			Type:      opType,
			Path:      ts.Path,
			ObjectRef: ts.ObjectRef,
			Author:    author,
			Meta: OpMeta{
				Size:   int64(len(content)),
				Source: "restore",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("write restore op for %s: %w", ts.EntityID, err)
		}
	}

	// 7. Delete entities that were created after the target checkpoint
	for key, cs := range currentStates {
		// Filter by scope
		if !re.matchesScope(scope, cs) {
			continue
		}

		// Skip deleted entities
		if cs.Type == OpDelete {
			continue
		}

		// If entity didn't exist at target or was deleted at target, remove it
		ts, existedAtTarget := targetStates[key]
		if !existedAtTarget || ts.Type == OpDelete {
			filePath, err := re.resolveFilePath(cs.SpaceID, cs.Path)
			if err != nil {
				continue
			}

			// Remove from disk (ignore error if already gone)
			os.Remove(filePath)

			// Write delete operation
			_, err = re.vault.OpWriter.Write(Operation{
				StreamID: stream.ID,
				SpaceID:  cs.SpaceID,
				EntityID: cs.EntityID,
				Type:     OpDelete,
				Path:     cs.Path,
				Author:   author,
				Meta: OpMeta{
					Source: "restore",
				},
			})
			if err != nil {
				return nil, fmt.Errorf("write delete op for %s: %w", cs.EntityID, err)
			}
		}
	}

	// 8. Create restore checkpoint
	restoreCP, err := re.vault.Checkpoints.Create(CheckpointInput{
		StreamID: stream.ID,
		Title:    fmt.Sprintf("Restored to %s", target.Title),
		Author:   author,
		Source:   SourceRestore,
		Tags:     map[string]string{"restored_from": checkpointID},
	})
	if err != nil {
		return nil, fmt.Errorf("create restore checkpoint: %w", err)
	}

	return restoreCP, nil
}

// entityStatesAt builds a map of entity states at a given sequence number.
// The key is "spaceID:entityID". Only operations for the given stream are considered.
func (re *RestoreEngine) entityStatesAt(streamID string, atSeq int64) (map[string]entityState, error) {
	ops, err := re.vault.OpReader.ReadRange(0, atSeq)
	if err != nil {
		return nil, fmt.Errorf("read ops up to seq %d: %w", atSeq, err)
	}

	states := make(map[string]entityState)
	for _, op := range ops {
		if op.StreamID != streamID {
			continue
		}
		key := op.SpaceID + ":" + op.EntityID
		states[key] = entityState{
			SpaceID:   op.SpaceID,
			EntityID:  op.EntityID,
			Path:      op.Path,
			ObjectRef: op.ObjectRef,
			Type:      op.Type,
		}
	}
	return states, nil
}

// matchesScope returns true if the entity matches the restore scope.
func (re *RestoreEngine) matchesScope(scope RestoreScope, es entityState) bool {
	if scope.Full {
		return true
	}
	if scope.EntityID != "" {
		return es.EntityID == scope.EntityID && (scope.SpaceID == "" || es.SpaceID == scope.SpaceID)
	}
	if scope.SpaceID != "" {
		return es.SpaceID == scope.SpaceID
	}
	// Default: full restore when no scope fields are set
	return true
}

// resolveFilePath resolves the full filesystem path for an entity.
func (re *RestoreEngine) resolveFilePath(spaceID, entityPath string) (string, error) {
	spaceCfg, ok := re.vault.Config.Spaces[spaceID]
	if !ok {
		return "", fmt.Errorf("unknown space: %s", spaceID)
	}
	return filepath.Join(re.vault.ProjectPath, spaceCfg.Path, entityPath), nil
}
