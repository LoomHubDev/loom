package merge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockLLMServer creates a test server that returns the given response body with the given status.
func newMockLLMServer(t *testing.T, status int, responseBody any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(responseBody)
	}))
}

// openAIWrap wraps text content in an OpenAI-compatible response structure.
func openAIWrap(content string) map[string]any {
	return map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": content,
				},
			},
		},
	}
}

func newTestLLMConfig(endpoint string) LLMConfig {
	return LLMConfig{
		Enabled:            true,
		Endpoint:           endpoint,
		APIKey:             "test-key",
		Model:              "test-model",
		AutoApplyThreshold: 0.9,
		MaxFileSize:        100000,
	}
}

// Test 1: Valid resolve with high confidence.
func TestLLMMerger_Resolve(t *testing.T) {
	llmJSON := `{"content": "merged content\n", "explanation": "combined both changes", "confidence": 0.95}`
	srv := newMockLLMServer(t, http.StatusOK, openAIWrap(llmJSON))
	defer srv.Close()

	merger := NewLLMMerger(newTestLLMConfig(srv.URL))

	base := []byte("line1\nline2\n")
	ours := []byte("line1\nours\n")
	theirs := []byte("line1\ntheirs\n")
	conflicts := []Conflict{
		{BaseStart: 1, BaseLines: []string{"line2\n"}, OurLines: []string{"ours\n"}, TheirLines: []string{"theirs\n"}},
	}

	result, err := merger.Resolve("test-entity", base, ours, theirs, conflicts)
	require.NoError(t, err)
	assert.Equal(t, "merged content\n", result.Content)
	assert.Equal(t, "combined both changes", result.Explanation)
	assert.InDelta(t, 0.95, result.Confidence, 0.001)
}

// Test 2: Disabled LLM returns error.
func TestLLMMerger_Disabled(t *testing.T) {
	config := LLMConfig{Enabled: false}
	merger := NewLLMMerger(config)

	_, err := merger.Resolve("entity", []byte("base"), []byte("ours"), []byte("theirs"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM merge disabled")
}

// Test 3: File too large returns error.
func TestLLMMerger_FileTooLarge(t *testing.T) {
	config := LLMConfig{
		Enabled:     true,
		MaxFileSize: 10,
	}
	merger := NewLLMMerger(config)

	largeBase := make([]byte, 100)
	_, err := merger.Resolve("entity", largeBase, []byte("ours"), []byte("theirs"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file too large")
}

// Test 4: API returns 500 error.
func TestLLMMerger_APIError(t *testing.T) {
	srv := newMockLLMServer(t, http.StatusInternalServerError, map[string]string{"error": "internal"})
	defer srv.Close()

	merger := NewLLMMerger(newTestLLMConfig(srv.URL))

	_, err := merger.Resolve("entity", []byte("base"), []byte("ours"), []byte("theirs"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API returned status 500")
}

// Test 5: Invalid JSON from LLM.
func TestLLMMerger_InvalidJSON(t *testing.T) {
	srv := newMockLLMServer(t, http.StatusOK, openAIWrap("this is not json at all"))
	defer srv.Close()

	merger := NewLLMMerger(newTestLLMConfig(srv.URL))

	_, err := merger.Resolve("entity", []byte("base"), []byte("ours"), []byte("theirs"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse LLM response")
}

// Test 6: Low confidence is still parsed correctly.
func TestLLMMerger_LowConfidence(t *testing.T) {
	llmJSON := `{"content": "maybe merged", "explanation": "not sure", "confidence": 0.5}`
	srv := newMockLLMServer(t, http.StatusOK, openAIWrap(llmJSON))
	defer srv.Close()

	merger := NewLLMMerger(newTestLLMConfig(srv.URL))

	result, err := merger.Resolve("entity", []byte("base"), []byte("ours"), []byte("theirs"), nil)
	require.NoError(t, err)
	assert.Equal(t, "maybe merged", result.Content)
	assert.InDelta(t, 0.5, result.Confidence, 0.001)
}

// Test 7: Build prompt contains all content.
func TestLLMMerger_BuildPrompt(t *testing.T) {
	merger := NewLLMMerger(LLMConfig{})
	base := []byte("base content")
	ours := []byte("ours content")
	theirs := []byte("theirs content")
	conflicts := []Conflict{
		{BaseStart: 0, BaseLines: []string{"base content"}, OurLines: []string{"ours content"}, TheirLines: []string{"theirs content"}},
	}

	prompt := merger.buildMergePrompt("my-entity", base, ours, theirs, conflicts)

	assert.Contains(t, prompt, "my-entity")
	assert.Contains(t, prompt, "base content")
	assert.Contains(t, prompt, "ours content")
	assert.Contains(t, prompt, "theirs content")
	assert.Contains(t, prompt, "Conflict 1")
}

// Test 8: MergeEngine with LLM fallback resolves conflicts.
func TestMergeEngine_WithLLMFallback(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	// The LLM will produce a high-confidence merge.
	mergedContent := "line1\nours-and-theirs\n"
	llmResult := LLMResult{
		Content:     mergedContent,
		Explanation: "merged both",
		Confidence:  0.95,
	}
	llmJSON, err2 := json.Marshal(llmResult)
	require.NoError(t, err2)
	srv := newMockLLMServer(t, http.StatusOK, openAIWrap(string(llmJSON)))
	defer srv.Close()

	engine := NewMergeEngineWithLLM(store, newTestLLMConfig(srv.URL))

	// Create conflicting content.
	base := []byte("line1\nline2\n")
	ours := []byte("line1\nours2\n")
	theirs := []byte("line1\ntheirs2\n")

	// First, conventional merge should produce a conflict.
	conventional := MergeEntityContent(base, ours, theirs)
	require.False(t, conventional.OK, "conventional merge should have conflicts")

	// Now resolve via the engine's LLM fallback.
	resolved := engine.ResolveConflicts("test-entity", base, ours, theirs, conventional)
	require.True(t, resolved.OK, "LLM should have resolved the conflict")
	assert.Equal(t, mergedContent, string(resolved.Content))
	assert.Empty(t, resolved.Conflicts)
}

// Test 9: MergeEngine without LLM returns original conflicts.
func TestMergeEngine_WithoutLLM(t *testing.T) {
	engine := NewMergeEngine(nil) // nil store is fine; we won't use it.

	base := []byte("line1\nline2\n")
	ours := []byte("line1\nours2\n")
	theirs := []byte("line1\ntheirs2\n")

	conventional := MergeEntityContent(base, ours, theirs)
	require.False(t, conventional.OK)

	// Without LLM, ResolveConflicts should return the original.
	result := engine.ResolveConflicts("entity", base, ours, theirs, conventional)
	assert.False(t, result.OK)
	assert.NotEmpty(t, result.Conflicts)
}

// Test 10: LLM below threshold keeps OK=false but provides content.
func TestMergeEngine_LLMBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	// LLM returns confidence below the 0.9 threshold.
	llmJSON := `{"content": "low confidence merge", "explanation": "unsure", "confidence": 0.6}`
	srv := newMockLLMServer(t, http.StatusOK, openAIWrap(llmJSON))
	defer srv.Close()

	engine := NewMergeEngineWithLLM(store, newTestLLMConfig(srv.URL))

	base := []byte("line1\nline2\n")
	ours := []byte("line1\nours2\n")
	theirs := []byte("line1\ntheirs2\n")

	conventional := MergeEntityContent(base, ours, theirs)
	require.False(t, conventional.OK)

	resolved := engine.ResolveConflicts("entity", base, ours, theirs, conventional)
	assert.False(t, resolved.OK, "below threshold should not auto-apply")
	assert.Equal(t, "low confidence merge", string(resolved.Content))
	assert.NotEmpty(t, resolved.Conflicts, "conflicts should be preserved for caller review")
}

// Test 11: Parse response with JSON code fence.
func TestParseResponse_CodeFence(t *testing.T) {
	response := "Here is the result:\n```json\n{\"content\": \"merged\", \"explanation\": \"done\", \"confidence\": 0.85}\n```\n"
	result, err := parseResponse(response)
	require.NoError(t, err)
	assert.Equal(t, "merged", result.Content)
	assert.InDelta(t, 0.85, result.Confidence, 0.001)
}

// Test 12: Parse response with plain JSON.
func TestParseResponse_PlainJSON(t *testing.T) {
	response := `{"content": "plain", "explanation": "just json", "confidence": 0.9}`
	result, err := parseResponse(response)
	require.NoError(t, err)
	assert.Equal(t, "plain", result.Content)
}

// Test 13: Parse response with out-of-range confidence.
func TestParseResponse_InvalidConfidence(t *testing.T) {
	response := `{"content": "x", "explanation": "y", "confidence": 1.5}`
	_, err := parseResponse(response)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

// Test 14: Anthropic response format is handled.
func TestLLMMerger_AnthropicFormat(t *testing.T) {
	llmJSON := `{"content": "anthropic merged", "explanation": "via anthropic", "confidence": 0.92}`
	anthropicResp := map[string]any{
		"content": []map[string]any{
			{"text": llmJSON},
		},
	}
	srv := newMockLLMServer(t, http.StatusOK, anthropicResp)
	defer srv.Close()

	merger := NewLLMMerger(newTestLLMConfig(srv.URL))

	result, err := merger.Resolve("entity", []byte("base"), []byte("ours"), []byte("theirs"), nil)
	require.NoError(t, err)
	assert.Equal(t, "anthropic merged", result.Content)
	assert.InDelta(t, 0.92, result.Confidence, 0.001)
}

// Test 15: StrategyAutoLLM exists and has correct value.
func TestStrategyAutoLLM(t *testing.T) {
	assert.Equal(t, Strategy("auto+llm"), StrategyAutoLLM)
}
