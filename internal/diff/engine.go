package diff

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/storage"
)

// DiffEngine computes diffs between two points in the oplog.
type DiffEngine struct {
	db     *sql.DB
	reader *core.OpReader
	store  *storage.ObjectStore
}

// NewDiffEngine creates a new DiffEngine.
func NewDiffEngine(db *sql.DB, reader *core.OpReader, store *storage.ObjectStore) *DiffEngine {
	return &DiffEngine{db: db, reader: reader, store: store}
}

// Diff computes the diff between two refs.
func (e *DiffEngine) Diff(from, to DiffRef, opts DiffOptions) (*DiffResult, error) {
	fromSeq, err := ResolveRef(from, e.db, e.reader)
	if err != nil {
		return nil, fmt.Errorf("resolve from ref: %w", err)
	}
	toSeq, err := ResolveRef(to, e.db, e.reader)
	if err != nil {
		return nil, fmt.Errorf("resolve to ref: %w", err)
	}

	ops, err := e.reader.ReadRange(fromSeq, toSeq)
	if err != nil {
		return nil, fmt.Errorf("read range: %w", err)
	}

	// Group ops by spaceID.
	spaceOps := make(map[string][]core.Operation)
	for _, op := range ops {
		spaceOps[op.SpaceID] = append(spaceOps[op.SpaceID], op)
	}

	result := &DiffResult{
		FromSeq: fromSeq,
		ToSeq:   toSeq,
		FromRef: from.String(),
		ToRef:   to.String(),
	}

	// Sort space IDs for deterministic output.
	spaceIDs := make([]string, 0, len(spaceOps))
	for id := range spaceOps {
		spaceIDs = append(spaceIDs, id)
	}
	sort.Strings(spaceIDs)

	for _, spaceID := range spaceIDs {
		sops := spaceOps[spaceID]

		// Filter by space if requested.
		if opts.SpaceID != "" && opts.SpaceID != spaceID {
			continue
		}

		spaceDiff, err := e.buildSpaceDiff(spaceID, sops, fromSeq, opts)
		if err != nil {
			return nil, fmt.Errorf("build space diff %s: %w", spaceID, err)
		}
		result.Spaces = append(result.Spaces, spaceDiff)

		result.Summary.EntitiesCreated += spaceDiff.Summary.EntitiesCreated
		result.Summary.EntitiesModified += spaceDiff.Summary.EntitiesModified
		result.Summary.EntitiesDeleted += spaceDiff.Summary.EntitiesDeleted
	}

	result.Summary.SpacesChanged = len(result.Spaces)
	result.Summary.TotalOperations = len(ops)

	return result, nil
}

// buildSpaceDiff builds the diff for a single space.
func (e *DiffEngine) buildSpaceDiff(spaceID string, ops []core.Operation, fromSeq int64, opts DiffOptions) (SpaceDiff, error) {
	// Group ops by entityID.
	entityOps := make(map[string][]core.Operation)
	for _, op := range ops {
		entityOps[op.EntityID] = append(entityOps[op.EntityID], op)
	}

	// Sort entity IDs for deterministic output.
	entityIDs := make([]string, 0, len(entityOps))
	for id := range entityOps {
		entityIDs = append(entityIDs, id)
	}
	sort.Strings(entityIDs)

	sd := SpaceDiff{SpaceID: spaceID}

	for _, entityID := range entityIDs {
		eops := entityOps[entityID]

		// Filter by entity if requested.
		if opts.EntityID != "" && opts.EntityID != entityID {
			continue
		}

		ed, err := e.buildEntityDiff(entityID, eops, fromSeq, opts)
		if err != nil {
			return sd, fmt.Errorf("build entity diff %s: %w", entityID, err)
		}
		sd.Entities = append(sd.Entities, ed)

		switch ed.Change {
		case core.ChangeCreated:
			sd.Summary.EntitiesCreated++
		case core.ChangeModified:
			sd.Summary.EntitiesModified++
		case core.ChangeDeleted:
			sd.Summary.EntitiesDeleted++
		}
	}

	return sd, nil
}

// buildEntityDiff builds the diff for a single entity.
func (e *DiffEngine) buildEntityDiff(entityID string, ops []core.Operation, fromSeq int64, opts DiffOptions) (EntityDiff, error) {
	// Sort ops by seq to ensure correct ordering.
	sort.Slice(ops, func(i, j int) bool { return ops[i].Seq < ops[j].Seq })

	lastOp := ops[len(ops)-1]
	change := opTypeToChangeType(lastOp.Type)

	// Use the path from the last op.
	path := lastOp.Path

	ed := EntityDiff{
		EntityID: entityID,
		Path:     path,
		Change:   change,
		Summary:  fmt.Sprintf("%s %s", change, path),
	}

	// If summary-only, skip content diffing.
	if opts.Summary {
		return ed, nil
	}

	// Find new state: last op in range with an ObjectRef.
	var newRef string
	for i := len(ops) - 1; i >= 0; i-- {
		if ops[i].ObjectRef != "" {
			newRef = ops[i].ObjectRef
			break
		}
	}

	// Get old and new content.
	var oldContent, newContent []byte

	switch change {
	case core.ChangeCreated:
		// No old content.
		if newRef != "" {
			var err error
			newContent, err = e.store.Read(newRef)
			if err != nil {
				return ed, fmt.Errorf("read new content: %w", err)
			}
		}
	case core.ChangeDeleted:
		// No new content. Get old from before the range.
		oldRef, err := e.getEntityObjectRef(entityID, fromSeq)
		if err != nil {
			return ed, fmt.Errorf("get old ref for delete: %w", err)
		}
		if oldRef != "" {
			oldContent, err = e.store.Read(oldRef)
			if err != nil {
				return ed, fmt.Errorf("read old content: %w", err)
			}
		}
	default:
		// Modified: get both old and new.
		oldRef, err := e.getEntityObjectRef(entityID, fromSeq)
		if err != nil {
			return ed, fmt.Errorf("get old ref: %w", err)
		}
		if oldRef != "" {
			oldContent, err = e.store.Read(oldRef)
			if err != nil {
				return ed, fmt.Errorf("read old content: %w", err)
			}
		}
		if newRef != "" {
			newContent, err = e.store.Read(newRef)
			if err != nil {
				return ed, fmt.Errorf("read new content: %w", err)
			}
		}
	}

	// Only compute text diff if we have content to compare.
	if oldContent != nil || newContent != nil {
		contextLines := opts.Context
		if contextLines <= 0 {
			contextLines = 3
		}
		ed.Hunks = TextDiff(oldContent, newContent, contextLines)
	}

	return ed, nil
}

// getEntityObjectRef returns the ObjectRef for an entity at or before the given seq.
func (e *DiffEngine) getEntityObjectRef(entityID string, atSeq int64) (string, error) {
	var ref string
	err := e.db.QueryRow(
		"SELECT object_ref FROM operations WHERE entity_id = ? AND seq <= ? AND object_ref != '' ORDER BY seq DESC LIMIT 1",
		entityID, atSeq,
	).Scan(&ref)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query entity ref at seq %d: %w", atSeq, err)
	}
	return ref, nil
}

// opTypeToChangeType maps an operation type to a change type.
func opTypeToChangeType(opType core.OpType) core.ChangeType {
	switch opType {
	case core.OpCreate:
		return core.ChangeCreated
	case core.OpDelete:
		return core.ChangeDeleted
	case core.OpMove, core.OpRename:
		return core.ChangeMoved
	default:
		return core.ChangeModified
	}
}
