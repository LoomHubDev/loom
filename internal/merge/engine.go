package merge

import (
	"encoding/json"
	"fmt"

	"github.com/constructspace/loom/internal/storage"
)

// MergeEngine performs content-level merges using the object store.
type MergeEngine struct {
	store *storage.ObjectStore
	llm   *LLMMerger // optional LLM-based conflict resolver (Tier 3)
}

// NewMergeEngine creates a MergeEngine backed by the given object store.
func NewMergeEngine(store *storage.ObjectStore) *MergeEngine {
	return &MergeEngine{store: store}
}

// NewMergeEngineWithLLM creates a MergeEngine with LLM-assisted conflict resolution.
// When conventional merge produces conflicts, the engine will attempt LLM resolution
// before reporting failure.
func NewMergeEngineWithLLM(store *storage.ObjectStore, llmConfig LLMConfig) *MergeEngine {
	return &MergeEngine{
		store: store,
		llm:   NewLLMMerger(llmConfig),
	}
}

// MergeEntity reads base, ours, and theirs from the object store and merges them.
// An empty ref string is treated as empty content (useful for newly created entities).
func (e *MergeEngine) MergeEntity(baseRef, oursRef, theirsRef string) (*MergeResult, error) {
	base, err := e.readRef(baseRef)
	if err != nil {
		return nil, fmt.Errorf("read base %q: %w", baseRef, err)
	}
	ours, err := e.readRef(oursRef)
	if err != nil {
		return nil, fmt.Errorf("read ours %q: %w", oursRef, err)
	}
	theirs, err := e.readRef(theirsRef)
	if err != nil {
		return nil, fmt.Errorf("read theirs %q: %w", theirsRef, err)
	}

	return MergeEntityContent(base, ours, theirs), nil
}

// MergeEntityContent performs a content-level merge on raw byte content.
// It tries structured (JSON) merge first and falls back to text merge.
func MergeEntityContent(base, ours, theirs []byte) *MergeResult {
	if isJSON(base) && isJSON(ours) && isJSON(theirs) {
		return StructuredMerge(base, ours, theirs)
	}
	return ThreeWayMerge(base, ours, theirs)
}

// ResolveConflicts attempts to resolve merge conflicts using the LLM if available.
// When the LLM is configured and the confidence meets the auto-apply threshold,
// the returned MergeResult will have OK=true. If the LLM provides a result with
// lower confidence, OK remains false but Content is set to the LLM suggestion.
// If the LLM is not configured or fails, the original conflict result is returned.
func (e *MergeEngine) ResolveConflicts(entityID string, base, ours, theirs []byte, result *MergeResult) *MergeResult {
	if e.llm == nil {
		return result
	}
	if result.OK || len(result.Conflicts) == 0 {
		return result
	}

	llmResult, err := e.llm.Resolve(entityID, base, ours, theirs, result.Conflicts)
	if err != nil {
		// LLM failed; return the original conflict result.
		return result
	}

	merged := &MergeResult{
		Content:   []byte(llmResult.Content),
		Conflicts: result.Conflicts,
	}

	if llmResult.Confidence >= e.llm.config.AutoApplyThreshold {
		merged.OK = true
		merged.Conflicts = nil
	}
	// If confidence is below threshold, OK stays false — caller can inspect the suggestion.

	return merged
}

// readRef reads content from the object store. Empty ref returns empty content.
func (e *MergeEngine) readRef(ref string) ([]byte, error) {
	if ref == "" {
		return []byte{}, nil
	}
	data, err := e.store.Read(ref)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// isJSON returns true if the content parses as a JSON object.
func isJSON(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	var m map[string]any
	return json.Unmarshal(data, &m) == nil
}
