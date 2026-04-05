package merge

import (
	"encoding/json"
	"fmt"

	"github.com/constructspace/loom/internal/storage"
)

// MergeEngine performs content-level merges using the object store.
type MergeEngine struct {
	store *storage.ObjectStore
}

// NewMergeEngine creates a MergeEngine backed by the given object store.
func NewMergeEngine(store *storage.ObjectStore) *MergeEngine {
	return &MergeEngine{store: store}
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
