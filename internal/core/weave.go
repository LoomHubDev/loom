package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/constructspace/loom/internal/merge"
	"github.com/constructspace/loom/internal/storage"
)

var ErrWeaveConflict = errors.New("weave conflict")

type WeaveStrategy string

const (
	WeaveStrategyAuto   WeaveStrategy = "auto"
	WeaveStrategyOurs   WeaveStrategy = "ours"
	WeaveStrategyTheirs WeaveStrategy = "theirs"
)

type WeaveOptions struct {
	SourceName string
	TargetName string
	Strategy   WeaveStrategy
	DryRun     bool
	Author     string
}

type WeaveConflict struct {
	EntityID string
}

type WeaveConflictError struct {
	Source    string
	Target    string
	Conflicts []WeaveConflict
}

func (e *WeaveConflictError) Error() string {
	entities := make([]string, 0, len(e.Conflicts))
	for _, conflict := range e.Conflicts {
		entities = append(entities, conflict.EntityID)
	}
	return fmt.Sprintf("%s: %s into %s for entities: %s", ErrWeaveConflict, e.Source, e.Target, strings.Join(entities, ", "))
}

type WeaveResult struct {
	Source         *Stream
	Target         *Stream
	Applied        int
	SourceEntities int
	ConflictCount  int
	Conflicts      []WeaveConflict
	Checkpoint     *Checkpoint
}

type WeaveEngine struct {
	streams     *StreamManager
	reader      *OpReader
	writer      *OpWriter
	checkpoints *CheckpointEngine
	store       *storage.ObjectStore
}

func NewWeaveEngine(streams *StreamManager, reader *OpReader, writer *OpWriter, checkpoints *CheckpointEngine, store *storage.ObjectStore) *WeaveEngine {
	return &WeaveEngine{
		streams:     streams,
		reader:      reader,
		writer:      writer,
		checkpoints: checkpoints,
		store:       store,
	}
}

func (w *WeaveEngine) Weave(opts WeaveOptions) (*WeaveResult, error) {
	source, err := w.streams.GetByName(opts.SourceName)
	if err != nil {
		return nil, fmt.Errorf("source stream: %w", err)
	}
	target, err := w.streams.GetByName(opts.TargetName)
	if err != nil {
		return nil, fmt.Errorf("target stream: %w", err)
	}
	if source.ID == target.ID {
		return nil, fmt.Errorf("cannot weave a stream into itself")
	}

	forkSeq, err := w.findCommonSeq(source, target)
	if err != nil {
		return nil, err
	}

	sourceOps, err := w.reader.ReadByStream(source.ID, forkSeq, source.HeadSeq)
	if err != nil {
		return nil, fmt.Errorf("read source operations: %w", err)
	}
	targetOps, err := w.reader.ReadByStream(target.ID, forkSeq, target.HeadSeq)
	if err != nil {
		return nil, fmt.Errorf("read target operations: %w", err)
	}

	targetEntities := make(map[string]bool)
	for _, op := range targetOps {
		targetEntities[op.EntityID] = true
	}

	sourceEntities := make(map[string]bool)
	conflictEntities := make(map[string]bool)
	var conflicts []WeaveConflict
	for _, op := range sourceOps {
		sourceEntities[op.EntityID] = true
		if targetEntities[op.EntityID] && !conflictEntities[op.EntityID] {
			conflictEntities[op.EntityID] = true
			conflicts = append(conflicts, WeaveConflict{EntityID: op.EntityID})
		}
	}

	// When strategy is auto and conflicts exist, attempt content-level merge.
	var mergedOps []Operation
	if len(conflicts) > 0 && opts.Strategy == WeaveStrategyAuto {
		me := merge.NewMergeEngine(w.store)
		var remaining []WeaveConflict

		// Group source and target ops by entityID for quick lookup.
		sourceByEntity := groupOpsByEntity(sourceOps)
		targetByEntity := groupOpsByEntity(targetOps)

		for _, c := range conflicts {
			baseRef := w.getBaseObjectRef(c.EntityID, forkSeq)
			oursRef := lastObjectRef(sourceByEntity[c.EntityID])
			theirsRef := lastObjectRef(targetByEntity[c.EntityID])

			// Content merge requires at least the two changed sides to have content.
			// Without ObjectRefs we cannot perform a meaningful merge.
			if oursRef == "" || theirsRef == "" {
				remaining = append(remaining, c)
				continue
			}

			result, err := me.MergeEntity(baseRef, oursRef, theirsRef)
			if err != nil || !result.OK {
				remaining = append(remaining, c)
				continue
			}

			// Store merged content and create a modify op.
			mergedHash, err := w.store.Write(result.Content, "text/plain")
			if err != nil {
				remaining = append(remaining, c)
				continue
			}

			// Use the last source op as template for the merged op.
			srcOps := sourceByEntity[c.EntityID]
			lastOp := srcOps[len(srcOps)-1]
			mergedOps = append(mergedOps, Operation{
				StreamID:  target.ID,
				SpaceID:   lastOp.SpaceID,
				EntityID:  c.EntityID,
				Type:      OpModify,
				Path:      lastOp.Path,
				ObjectRef: mergedHash,
				Author:    opts.Author,
				Meta: OpMeta{
					Size:   int64(len(result.Content)),
					Source: "merge",
				},
			})
		}

		if len(remaining) > 0 {
			return &WeaveResult{
				Source:         source,
				Target:         target,
				SourceEntities: len(sourceEntities),
				ConflictCount:  len(remaining),
				Conflicts:      remaining,
			}, &WeaveConflictError{Source: source.Name, Target: target.Name, Conflicts: remaining}
		}

		// All conflicts resolved via content merge; clear conflict entities so
		// the original source ops for those entities are skipped (the merged ops
		// replace them).
		conflicts = remaining
	}

	var pending []Operation
	for _, op := range sourceOps {
		if conflictEntities[op.EntityID] && opts.Strategy == WeaveStrategyOurs {
			continue
		}
		// Skip source ops for entities that were resolved via content merge
		// (mergedOps already contain the combined result).
		if len(mergedOps) > 0 && conflictEntities[op.EntityID] {
			continue
		}
		pending = append(pending, Operation{
			StreamID:  target.ID,
			SpaceID:   op.SpaceID,
			EntityID:  op.EntityID,
			Type:      op.Type,
			Path:      op.Path,
			Delta:     op.Delta,
			ObjectRef: op.ObjectRef,
			Author:    opts.Author,
			Meta:      op.Meta,
		})
	}
	// Append merged ops for auto-resolved conflicts.
	pending = append(pending, mergedOps...)

	result := &WeaveResult{
		Source:         source,
		Target:         target,
		Applied:        len(pending),
		SourceEntities: len(sourceEntities),
		ConflictCount:  len(conflicts),
		Conflicts:      conflicts,
	}
	if opts.DryRun {
		return result, nil
	}

	if len(pending) > 0 {
		if _, err := w.writer.WriteBatch(pending); err != nil {
			return nil, fmt.Errorf("apply weave operations: %w", err)
		}
	}

	checkpoint, err := w.checkpoints.Create(CheckpointInput{
		StreamID: target.ID,
		Title:    fmt.Sprintf("Weave %s into %s", source.Name, target.Name),
		Summary:  fmt.Sprintf("%d operations woven from %s into %s", len(pending), source.Name, target.Name),
		Author:   opts.Author,
		Source:   SourceWeave,
		Tags: map[string]string{
			"source_stream": source.Name,
			"target_stream": target.Name,
			"strategy":      string(opts.Strategy),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create weave checkpoint: %w", err)
	}
	result.Checkpoint = checkpoint

	if err := w.streams.SetStatus(source.ID, "woven"); err != nil {
		return nil, fmt.Errorf("mark source stream woven: %w", err)
	}

	return result, nil
}

func (w *WeaveEngine) findCommonSeq(source, target *Stream) (int64, error) {
	sourceAncestors, err := w.ancestorSeqs(source)
	if err != nil {
		return 0, err
	}

	current := target
	sharedUntil := target.HeadSeq
	for {
		if sourceShared, ok := sourceAncestors[current.ID]; ok {
			if sourceShared < sharedUntil {
				return sourceShared, nil
			}
			return sharedUntil, nil
		}
		if current.ParentID == "" {
			break
		}
		parent, err := w.streams.GetByID(current.ParentID)
		if err != nil {
			return 0, fmt.Errorf("walk target ancestors: %w", err)
		}
		if sharedUntil == 0 || current.ForkSeq < sharedUntil {
			sharedUntil = current.ForkSeq
		}
		current = parent
	}

	return 0, fmt.Errorf("streams %q and %q do not share a common ancestor", source.Name, target.Name)
}

func (w *WeaveEngine) ancestorSeqs(stream *Stream) (map[string]int64, error) {
	ancestors := map[string]int64{
		stream.ID: stream.HeadSeq,
	}
	current := stream
	sharedUntil := stream.HeadSeq
	for current.ParentID != "" {
		parent, err := w.streams.GetByID(current.ParentID)
		if err != nil {
			return nil, fmt.Errorf("walk source ancestors: %w", err)
		}
		if sharedUntil == 0 || current.ForkSeq < sharedUntil {
			sharedUntil = current.ForkSeq
		}
		ancestors[parent.ID] = sharedUntil
		current = parent
	}
	return ancestors, nil
}

// getBaseObjectRef returns the last ObjectRef for an entity at or before the given seq.
func (w *WeaveEngine) getBaseObjectRef(entityID string, beforeSeq int64) string {
	ops, err := w.reader.ReadByEntity(entityID)
	if err != nil {
		return ""
	}
	var ref string
	for _, op := range ops {
		if op.Seq > beforeSeq {
			break
		}
		if op.ObjectRef != "" {
			ref = op.ObjectRef
		}
	}
	return ref
}

// lastObjectRef returns the ObjectRef from the last operation with a non-empty ref.
func lastObjectRef(ops []Operation) string {
	for i := len(ops) - 1; i >= 0; i-- {
		if ops[i].ObjectRef != "" {
			return ops[i].ObjectRef
		}
	}
	return ""
}

// groupOpsByEntity groups operations by their EntityID.
func groupOpsByEntity(ops []Operation) map[string][]Operation {
	m := make(map[string][]Operation)
	for _, op := range ops {
		m[op.EntityID] = append(m[op.EntityID], op)
	}
	return m
}
