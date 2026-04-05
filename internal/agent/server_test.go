package agent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/core"
	loom "github.com/constructspace/loom/pkg/loom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAgentTest(t *testing.T) (*loom.Client, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()

	// Create some files so the vault has entities to track.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644))

	// Initialize and close the vault so the project is set up.
	v, err := core.InitVault(dir)
	require.NoError(t, err)
	require.NoError(t, v.Close())

	// Open via the loom SDK client.
	client, err := loom.Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })

	srv := NewAgentServer(client, 0)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close() })

	return client, ts
}

func TestAgentServer_Status(t *testing.T) {
	_, ts := setupAgentTest(t)

	resp, err := http.Get(ts.URL + "/api/v1/status")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Contains(t, body, "project")
	assert.Contains(t, body, "stream")
}

func TestAgentServer_Checkpoint(t *testing.T) {
	_, ts := setupAgentTest(t)

	payload := map[string]any{
		"title":   "test checkpoint",
		"summary": "created via agent API",
		"tags":    map[string]string{"env": "test"},
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/api/v1/checkpoint", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var cp map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cp))
	assert.NotEmpty(t, cp["id"])
	assert.Equal(t, "test checkpoint", cp["title"])
}

func TestAgentServer_Log(t *testing.T) {
	client, ts := setupAgentTest(t)

	// Create a couple of checkpoints.
	_, err := client.Checkpoint("first")
	require.NoError(t, err)
	_, err = client.Checkpoint("second")
	require.NoError(t, err)

	resp, err := http.Get(ts.URL + "/api/v1/log?limit=5")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var cps []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cps))
	assert.GreaterOrEqual(t, len(cps), 2)
}

func TestAgentServer_Search(t *testing.T) {
	client, ts := setupAgentTest(t)

	_, err := client.Checkpoint("auth fix for login")
	require.NoError(t, err)

	resp, err := http.Get(ts.URL + "/api/v1/search?q=auth")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var results []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&results))
	assert.GreaterOrEqual(t, len(results), 1)
}

func TestAgentServer_Diff(t *testing.T) {
	_, ts := setupAgentTest(t)

	resp, err := http.Get(ts.URL + "/api/v1/diff?from=0&to=HEAD")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result, "from_seq")
	assert.Contains(t, result, "to_seq")
	assert.Contains(t, result, "summary")
}

func TestAgentServer_Rollback(t *testing.T) {
	client, ts := setupAgentTest(t)

	cp, err := client.Checkpoint("rollback target")
	require.NoError(t, err)

	payload := map[string]string{
		"checkpoint_id": cp.ID,
		"scope":         "full",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/api/v1/rollback", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "ok", result["status"])
}

func TestAgentServer_CheckpointMissingTitle(t *testing.T) {
	_, ts := setupAgentTest(t)

	payload := map[string]any{"summary": "no title"}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/api/v1/checkpoint", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result, "error")
}

func TestAgentServer_Record(t *testing.T) {
	_, ts := setupAgentTest(t)

	content := []byte("package main\nfunc added() {}\n")
	encoded := base64.StdEncoding.EncodeToString(content)

	payload := map[string]string{
		"space_id": "code",
		"path":     "src/main.go",
		"content":  encoded,
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/api/v1/record", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, true, result["ok"])
}

func TestAgentServer_Explain(t *testing.T) {
	client, ts := setupAgentTest(t)

	// Record a change so there is something to explain.
	err := client.RecordChange("code", "main.go", []byte("package main\n"))
	require.NoError(t, err)

	payload := map[string]string{"from": "0", "to": "HEAD"}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/api/v1/explain", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Contains(t, result, "explanation")
	assert.NotEmpty(t, result["explanation"])
}

func TestAgentServer_Tools(t *testing.T) {
	_, ts := setupAgentTest(t)

	resp, err := http.Get(ts.URL + "/api/v1/tools")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var tools []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tools))
	assert.NotEmpty(t, tools)

	// Verify it's a proper array with named tools.
	for _, tool := range tools {
		assert.Contains(t, tool, "name")
		assert.Contains(t, tool, "description")
	}
}
