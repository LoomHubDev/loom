package merge

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMergeEnv(t *testing.T) (*MergeEngine, *storage.ObjectStore) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)
	engine := NewMergeEngine(store)
	return engine, store
}

func storeContent(t *testing.T, store *storage.ObjectStore, content []byte) string {
	t.Helper()
	hash, err := store.Write(content, "application/octet-stream")
	require.NoError(t, err)
	return hash
}

// Test 1: Text merge with no conflict — different regions changed.
func TestMergeEngine_TextMergeNoConflict(t *testing.T) {
	engine, store := setupMergeEnv(t)

	base := []byte("line1\nline2\nline3\nline4\nline5\n")
	ours := []byte("line1\nchanged2\nline3\nline4\nline5\n")   // changed line 2
	theirs := []byte("line1\nline2\nline3\nline4\nchanged5\n") // changed line 5

	baseRef := storeContent(t, store, base)
	oursRef := storeContent(t, store, ours)
	theirsRef := storeContent(t, store, theirs)

	result, err := engine.MergeEntity(baseRef, oursRef, theirsRef)
	require.NoError(t, err)
	require.True(t, result.OK, "expected no conflicts")

	assert.Contains(t, string(result.Content), "changed2")
	assert.Contains(t, string(result.Content), "changed5")
	assert.Empty(t, result.Conflicts)
}

// Test 2: Text merge with conflict — same line changed differently.
func TestMergeEngine_TextMergeConflict(t *testing.T) {
	engine, store := setupMergeEnv(t)

	base := []byte("line1\nline2\nline3\n")
	ours := []byte("line1\nours2\nline3\n")
	theirs := []byte("line1\ntheirs2\nline3\n")

	baseRef := storeContent(t, store, base)
	oursRef := storeContent(t, store, ours)
	theirsRef := storeContent(t, store, theirs)

	result, err := engine.MergeEntity(baseRef, oursRef, theirsRef)
	require.NoError(t, err)
	require.False(t, result.OK, "expected conflict")
	require.NotEmpty(t, result.Conflicts)
}

// Test 3: Structured JSON merge — non-overlapping key changes.
func TestMergeEngine_StructuredMergeNoConflict(t *testing.T) {
	engine, store := setupMergeEnv(t)

	base := []byte(`{"a":1,"b":2}`)
	ours := []byte(`{"a":10,"b":2}`)  // changed "a"
	theirs := []byte(`{"a":1,"b":20}`) // changed "b"

	baseRef := storeContent(t, store, base)
	oursRef := storeContent(t, store, ours)
	theirsRef := storeContent(t, store, theirs)

	result, err := engine.MergeEntity(baseRef, oursRef, theirsRef)
	require.NoError(t, err)
	require.True(t, result.OK, "expected no conflicts")

	var merged map[string]any
	err = json.Unmarshal(result.Content, &merged)
	require.NoError(t, err)
	assert.Equal(t, float64(10), merged["a"])
	assert.Equal(t, float64(20), merged["b"])
}

// Test 4: Structured JSON merge — same key changed differently → conflict.
func TestMergeEngine_StructuredMergeConflict(t *testing.T) {
	engine, store := setupMergeEnv(t)

	base := []byte(`{"a":1,"b":2}`)
	ours := []byte(`{"a":10,"b":2}`)
	theirs := []byte(`{"a":99,"b":2}`)

	baseRef := storeContent(t, store, base)
	oursRef := storeContent(t, store, ours)
	theirsRef := storeContent(t, store, theirs)

	result, err := engine.MergeEntity(baseRef, oursRef, theirsRef)
	require.NoError(t, err)
	require.False(t, result.OK, "expected conflict on key 'a'")
	require.NotEmpty(t, result.Conflicts)
}

// Test 5: Structured JSON merge — both sides add different keys.
func TestMergeEngine_StructuredMergeAddKeys(t *testing.T) {
	engine, store := setupMergeEnv(t)

	base := []byte(`{"a":1,"b":2}`)
	ours := []byte(`{"a":1,"b":2,"c":3}`)  // added "c"
	theirs := []byte(`{"a":1,"b":2,"d":4}`) // added "d"

	baseRef := storeContent(t, store, base)
	oursRef := storeContent(t, store, ours)
	theirsRef := storeContent(t, store, theirs)

	result, err := engine.MergeEntity(baseRef, oursRef, theirsRef)
	require.NoError(t, err)
	require.True(t, result.OK, "expected no conflicts")

	var merged map[string]any
	err = json.Unmarshal(result.Content, &merged)
	require.NoError(t, err)
	assert.Equal(t, float64(1), merged["a"])
	assert.Equal(t, float64(2), merged["b"])
	assert.Equal(t, float64(3), merged["c"])
	assert.Equal(t, float64(4), merged["d"])
}

// Test 6: Non-JSON content falls back to text merge.
func TestMergeEngine_FallbackToTextForInvalidJSON(t *testing.T) {
	engine, store := setupMergeEnv(t)

	base := []byte("hello world\ngoodbye world\n")
	ours := []byte("hello universe\ngoodbye world\n")
	theirs := []byte("hello world\ngoodbye universe\n")

	baseRef := storeContent(t, store, base)
	oursRef := storeContent(t, store, ours)
	theirsRef := storeContent(t, store, theirs)

	result, err := engine.MergeEntity(baseRef, oursRef, theirsRef)
	require.NoError(t, err)
	require.True(t, result.OK, "expected no conflicts")

	assert.Contains(t, string(result.Content), "hello universe")
	assert.Contains(t, string(result.Content), "goodbye universe")
}

// Test 7: Empty base ref is treated as empty content.
func TestMergeEngine_EmptyBaseRef(t *testing.T) {
	engine, store := setupMergeEnv(t)

	ours := []byte("new content from ours\n")
	theirs := []byte("new content from ours\n") // same content so no conflict

	oursRef := storeContent(t, store, ours)
	theirsRef := storeContent(t, store, theirs)

	// Empty string as baseRef → empty content
	result, err := engine.MergeEntity("", oursRef, theirsRef)
	require.NoError(t, err)
	require.True(t, result.OK, "expected no conflicts when both add identical content")
	assert.Equal(t, ours, result.Content)
}

// Test 8: Default policy returns auto for unknown spaces.
func TestPolicy_DefaultStrategy(t *testing.T) {
	p := NewDefaultPolicy()
	assert.Equal(t, StrategyAuto, p.StrategyFor("unknown-space"))
	assert.Equal(t, StrategyAuto, p.StrategyFor(""))
}

// Test 9: Space-specific strategy override.
func TestPolicy_SpaceOverride(t *testing.T) {
	p := NewDefaultPolicy()
	p.SpaceStrategies["config"] = StrategyOurs
	p.SpaceStrategies["data"] = StrategyTheirs

	assert.Equal(t, StrategyOurs, p.StrategyFor("config"))
	assert.Equal(t, StrategyTheirs, p.StrategyFor("data"))
	assert.Equal(t, StrategyAuto, p.StrategyFor("other"))
}

// Test 10: Structured merge — ours deletes a key that theirs didn't change.
func TestStructuredMerge_DeleteKey(t *testing.T) {
	base := []byte(`{"a":1,"b":2,"c":3}`)
	ours := []byte(`{"a":1,"c":3}`)        // deleted "b"
	theirs := []byte(`{"a":1,"b":2,"c":3}`) // no change

	result := StructuredMerge(base, ours, theirs)

	require.True(t, result.OK, "expected no conflicts")

	var merged map[string]any
	err := json.Unmarshal(result.Content, &merged)
	require.NoError(t, err)
	assert.Equal(t, float64(1), merged["a"])
	assert.Equal(t, float64(3), merged["c"])
	_, hasB := merged["b"]
	assert.False(t, hasB, "key 'b' should be deleted")
}
