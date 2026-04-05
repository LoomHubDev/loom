package loom

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestClient initializes a Loom vault in a temp dir and opens a Client.
func setupTestClient(t *testing.T) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)

	// InitVault must run before Open, since Open requires an existing .loom directory.
	v, err := core.InitVault(dir)
	require.NoError(t, err)
	v.Close()

	client, err := Open(dir)
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })
	return client, dir
}

func TestClient_OpenAndClose(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)

	v, err := core.InitVault(dir)
	require.NoError(t, err)
	v.Close()

	client, err := Open(dir)
	require.NoError(t, err)
	assert.NotNil(t, client)

	err = client.Close()
	assert.NoError(t, err)
}

func TestClient_Checkpoint(t *testing.T) {
	client, _ := setupTestClient(t)

	cp, err := client.Checkpoint("initial state")
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, "initial state", cp.Title)
	assert.Equal(t, core.SourceAgent, cp.Source)
	assert.Equal(t, "agent", cp.Author)
}

func TestClient_CheckpointWithOptions(t *testing.T) {
	client, _ := setupTestClient(t)

	cp, err := client.Checkpoint("feature work",
		WithSummary("implement feature X"),
		WithTag("ticket", "PROJ-42"),
		WithAuthor("bot-v2"),
	)
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, "implement feature X", cp.Summary)
	assert.Equal(t, "PROJ-42", cp.Tags["ticket"])
	assert.Equal(t, "bot-v2", cp.Author)
}

func TestClient_Log(t *testing.T) {
	client, _ := setupTestClient(t)

	_, err := client.Checkpoint("first")
	require.NoError(t, err)
	_, err = client.Checkpoint("second")
	require.NoError(t, err)
	_, err = client.Checkpoint("third")
	require.NoError(t, err)

	checkpoints, err := client.Log(10)
	require.NoError(t, err)
	assert.Len(t, checkpoints, 3)
}

func TestClient_Search(t *testing.T) {
	client, _ := setupTestClient(t)

	_, err := client.Checkpoint("auth fix", WithSummary("fix authentication bug"))
	require.NoError(t, err)
	_, err = client.Checkpoint("unrelated refactor")
	require.NoError(t, err)

	results, err := client.Search("auth")
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, "auth fix", results[0].Title)
}

func TestClient_Status(t *testing.T) {
	client, _ := setupTestClient(t)

	status, err := client.Status()
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.NotEmpty(t, status.Project)
	assert.NotEmpty(t, status.Stream)
	assert.NotNil(t, status.Spaces)
}

func TestClient_Diff(t *testing.T) {
	client, dir := setupTestClient(t)

	// Write a new file so we have something to diff
	os.WriteFile(filepath.Join(dir, "handler.go"), []byte("package main\nfunc handler() {}\n"), 0644)

	// Write an operation directly via the vault so there is something to diff
	stream, err := client.vault.ActiveStream()
	require.NoError(t, err)

	content := []byte("package main\nfunc handler() {}\n")
	hash, err := client.vault.Store.Write(content, "text/x-go")
	require.NoError(t, err)

	_, err = client.vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "handler.go",
		Type:      core.OpCreate,
		Path:      "handler.go",
		ObjectRef: hash,
		Author:    "test",
	})
	require.NoError(t, err)

	result, err := client.Diff("0", "HEAD")
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestClient_Rollback(t *testing.T) {
	client, dir := setupTestClient(t)

	// Write initial content and record it
	initialContent := []byte("package main\nfunc v1() {}\n")
	os.WriteFile(filepath.Join(dir, "main.go"), initialContent, 0644)

	stream, err := client.vault.ActiveStream()
	require.NoError(t, err)

	hash, err := client.vault.Store.Write(initialContent, "text/x-go")
	require.NoError(t, err)

	_, err = client.vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      core.OpModify,
		Path:      "main.go",
		ObjectRef: hash,
		Author:    "test",
	})
	require.NoError(t, err)

	// Checkpoint current state
	cp, err := client.Checkpoint("v1 state")
	require.NoError(t, err)
	require.NotNil(t, cp)

	// Modify the file and record a new op
	modifiedContent := []byte("package main\nfunc v2() {}\n")
	os.WriteFile(filepath.Join(dir, "main.go"), modifiedContent, 0644)

	modHash, err := client.vault.Store.Write(modifiedContent, "text/x-go")
	require.NoError(t, err)

	_, err = client.vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      core.OpModify,
		Path:      "main.go",
		ObjectRef: modHash,
		Author:    "test",
	})
	require.NoError(t, err)

	// Rollback to v1
	err = client.Rollback(cp.ID)
	require.NoError(t, err)

	// File should be restored to initial content
	restored, err := os.ReadFile(filepath.Join(dir, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, initialContent, restored)
}

func TestClient_OpenNonexistent(t *testing.T) {
	_, err := Open("/nonexistent/path/to/project")
	assert.Error(t, err)
}

func TestClient_RecordChange(t *testing.T) {
	client, _ := setupTestClient(t)

	content := []byte("package main\nfunc recorded() {}\n")
	err := client.RecordChange("code", "recorded.go", content)
	require.NoError(t, err)

	// Verify the op was written by diffing from 0 to HEAD.
	result, err := client.Diff("0", "HEAD")
	require.NoError(t, err)
	require.NotNil(t, result)

	found := false
	for _, space := range result.Spaces {
		for _, e := range space.Entities {
			if e.Path == "recorded.go" {
				found = true
			}
		}
	}
	assert.True(t, found, "expected recorded.go in diff result")
}

func TestClient_Explain(t *testing.T) {
	client, _ := setupTestClient(t)

	// Write an op via the vault directly (same package access).
	stream, err := client.vault.ActiveStream()
	require.NoError(t, err)

	hash, err := client.vault.Store.Write([]byte("package main\n"), "text/x-go")
	require.NoError(t, err)

	_, err = client.vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "explain_test.go",
		Type:      core.OpCreate,
		Path:      "explain_test.go",
		ObjectRef: hash,
		Author:    "test",
	})
	require.NoError(t, err)

	explanation, err := client.Explain("0", "HEAD")
	require.NoError(t, err)
	assert.NotEmpty(t, explanation)
	assert.NotEqual(t, "No changes detected.", explanation)
}
