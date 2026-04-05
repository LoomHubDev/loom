package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	lsync "github.com/constructspace/loom/internal/sync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupHubTest creates a test server and returns it along with an auth token for "alice".
func setupHubTest(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	hub, err := NewHubServer(dir)
	require.NoError(t, err)
	t.Cleanup(func() {
		ts := httptest.NewServer(hub.Handler()) // need ts first to close
		_ = ts
		hub.Close()
	})

	require.NoError(t, hub.users.CreateUser("alice", "secret"))
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(func() { ts.Close() })

	// Login to get token.
	token := loginHelper(t, ts, "alice", "secret")
	return ts, token
}

func loginHelper(t *testing.T, ts *httptest.Server, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NotEmpty(t, result.Token)
	return result.Token
}

func authPost(t *testing.T, ts *httptest.Server, token, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", ts.URL+path, bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestHubServer_LoginSuccess(t *testing.T) {
	dir := t.TempDir()
	hub, err := NewHubServer(dir)
	require.NoError(t, err)
	defer hub.Close()

	require.NoError(t, hub.users.CreateUser("alice", "secret"))
	ts := httptest.NewServer(hub.Handler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "secret"})
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result.Token)

	// Validate the token round-trips.
	username, err := hub.users.ValidateToken(result.Token)
	require.NoError(t, err)
	assert.Equal(t, "alice", username)
}

func TestHubServer_LoginBadCredentials(t *testing.T) {
	dir := t.TempDir()
	hub, err := NewHubServer(dir)
	require.NoError(t, err)
	defer hub.Close()

	require.NoError(t, hub.users.CreateUser("alice", "secret"))
	ts := httptest.NewServer(hub.Handler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "wrong"})
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHubServer_NegotiateEmpty(t *testing.T) {
	ts, token := setupHubTest(t)

	req := lsync.NegotiateRequest{
		ProjectID: "test-project",
		Streams: []lsync.StreamSyncState{
			{StreamID: "stream-1", Name: "main", HeadSeq: 0},
		},
	}

	resp := authPost(t, ts, token, "/alice/myproject/api/v1/negotiate", req)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result lsync.NegotiateResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	assert.Equal(t, int64(0), result.ServerSeqs["stream-1"])
	assert.Equal(t, int64(0), result.CommonSeqs["stream-1"])
	assert.False(t, result.NeedsPush)
	assert.False(t, result.NeedsPull)
}

func TestHubServer_PushAndPull(t *testing.T) {
	ts, token := setupHubTest(t)

	// Push some operations.
	pushReq := lsync.PushRequest{
		ProjectID: "test-project",
		StreamID:  "stream-1",
		FromSeq:   0,
		Operations: []lsync.OperationWire{
			{
				ID:        "op-1",
				Seq:       1,
				StreamID:  "stream-1",
				SpaceID:   "space-1",
				EntityID:  "entity-1",
				Type:      "create",
				Path:      "/hello.txt",
				ObjectRef: "",
				ParentSeq: 0,
				Author:    "alice",
				Timestamp: "2025-01-01T00:00:00Z",
			},
			{
				ID:        "op-2",
				Seq:       2,
				StreamID:  "stream-1",
				SpaceID:   "space-1",
				EntityID:  "entity-1",
				Type:      "update",
				Path:      "/hello.txt",
				ObjectRef: "",
				ParentSeq: 1,
				Author:    "alice",
				Timestamp: "2025-01-01T00:01:00Z",
			},
		},
	}

	pushResp := authPost(t, ts, token, "/alice/myproject/api/v1/push", pushReq)
	defer pushResp.Body.Close()
	require.Equal(t, http.StatusOK, pushResp.StatusCode)

	var pushResult lsync.PushResponse
	require.NoError(t, json.NewDecoder(pushResp.Body).Decode(&pushResult))
	assert.True(t, pushResult.OK)
	assert.Equal(t, 2, pushResult.Applied)
	assert.Equal(t, int64(2), pushResult.ServerHead)

	// Pull them back from seq 0.
	pullReq := lsync.PullRequest{
		ProjectID: "test-project",
		StreamID:  "stream-1",
		FromSeq:   0,
	}

	pullResp := authPost(t, ts, token, "/alice/myproject/api/v1/pull", pullReq)
	defer pullResp.Body.Close()
	require.Equal(t, http.StatusOK, pullResp.StatusCode)

	var pullResult lsync.PullResponse
	require.NoError(t, json.NewDecoder(pullResp.Body).Decode(&pullResult))
	assert.Len(t, pullResult.Operations, 2)
	assert.Equal(t, "op-1", pullResult.Operations[0].ID)
	assert.Equal(t, "op-2", pullResult.Operations[1].ID)
	assert.Equal(t, int64(2), pullResult.ServerHead)

	// Pull from seq 1 should only return the second op.
	pullReq2 := lsync.PullRequest{
		ProjectID: "test-project",
		StreamID:  "stream-1",
		FromSeq:   1,
	}
	pullResp2 := authPost(t, ts, token, "/alice/myproject/api/v1/pull", pullReq2)
	defer pullResp2.Body.Close()
	require.Equal(t, http.StatusOK, pullResp2.StatusCode)

	var pullResult2 lsync.PullResponse
	require.NoError(t, json.NewDecoder(pullResp2.Body).Decode(&pullResult2))
	assert.Len(t, pullResult2.Operations, 1)
	assert.Equal(t, "op-2", pullResult2.Operations[0].ID)
}

func TestHubServer_PushRequiresAuth(t *testing.T) {
	dir := t.TempDir()
	hub, err := NewHubServer(dir)
	require.NoError(t, err)
	defer hub.Close()

	ts := httptest.NewServer(hub.Handler())
	defer ts.Close()

	pushReq := lsync.PushRequest{
		ProjectID:  "test-project",
		StreamID:   "stream-1",
		FromSeq:    0,
		Operations: []lsync.OperationWire{},
	}
	data, _ := json.Marshal(pushReq)

	// No auth header at all.
	resp, err := http.Post(ts.URL+"/alice/myproject/api/v1/push", "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Bad token.
	req, _ := http.NewRequest("POST", ts.URL+"/alice/myproject/api/v1/push", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp2.StatusCode)
}

func TestHubServer_NegotiateAfterPush(t *testing.T) {
	ts, token := setupHubTest(t)

	// Push 3 ops.
	pushReq := lsync.PushRequest{
		ProjectID: "test-project",
		StreamID:  "stream-1",
		FromSeq:   0,
		Operations: []lsync.OperationWire{
			{ID: "op-1", Seq: 1, StreamID: "stream-1", SpaceID: "s1", EntityID: "e1", Type: "create", Path: "/a.txt", Author: "alice", Timestamp: "2025-01-01T00:00:00Z"},
			{ID: "op-2", Seq: 2, StreamID: "stream-1", SpaceID: "s1", EntityID: "e1", Type: "update", Path: "/a.txt", Author: "alice", Timestamp: "2025-01-01T00:01:00Z"},
			{ID: "op-3", Seq: 3, StreamID: "stream-1", SpaceID: "s1", EntityID: "e1", Type: "update", Path: "/a.txt", Author: "alice", Timestamp: "2025-01-01T00:02:00Z"},
		},
	}
	pushResp := authPost(t, ts, token, "/alice/myproject/api/v1/push", pushReq)
	pushResp.Body.Close()

	// Negotiate: client has seq 1, server has seq 3 → needs_pull.
	negReq := lsync.NegotiateRequest{
		ProjectID: "test-project",
		Streams: []lsync.StreamSyncState{
			{StreamID: "stream-1", Name: "main", HeadSeq: 1},
		},
	}
	negResp := authPost(t, ts, token, "/alice/myproject/api/v1/negotiate", negReq)
	defer negResp.Body.Close()
	require.Equal(t, http.StatusOK, negResp.StatusCode)

	var result lsync.NegotiateResponse
	require.NoError(t, json.NewDecoder(negResp.Body).Decode(&result))

	assert.Equal(t, int64(3), result.ServerSeqs["stream-1"])
	assert.Equal(t, int64(1), result.CommonSeqs["stream-1"])
	assert.False(t, result.NeedsPush)
	assert.True(t, result.NeedsPull)
}
