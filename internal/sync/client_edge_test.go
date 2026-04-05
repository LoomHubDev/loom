package sync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- URL Parsing ---

func TestNewClient_FullURL(t *testing.T) {
	c := NewClient("https://hub.example.com/alice/myproject", "token123")
	assert.Equal(t, "https://hub.example.com", c.baseURL)
	assert.Equal(t, "alice", c.owner)
	assert.Equal(t, "myproject", c.loom)
	assert.Equal(t, "token123", c.token)
}

func TestNewClient_TrailingSlash(t *testing.T) {
	c := NewClient("https://hub.example.com/alice/myproject/", "tok")
	assert.Equal(t, "https://hub.example.com", c.baseURL)
	assert.Equal(t, "alice", c.owner)
	assert.Equal(t, "myproject", c.loom)
}

func TestNewClient_ShortURL(t *testing.T) {
	c := NewClient("https://hub.example.com", "tok")
	assert.Equal(t, "https://hub.example.com", c.baseURL)
	assert.Empty(t, c.owner)
	assert.Empty(t, c.loom)
}

func TestNewClient_EmptyToken(t *testing.T) {
	c := NewClient("https://hub.example.com/o/l", "")
	assert.Empty(t, c.token)
}

func TestNewClient_SyncURL(t *testing.T) {
	c := NewClient("https://hub.example.com/alice/proj", "tok")
	url := c.syncURL("negotiate")
	assert.Equal(t, "https://hub.example.com/alice/proj/api/v1/negotiate", url)
}

func TestNewClient_HttpClientTimeout(t *testing.T) {
	c := NewClient("https://hub.example.com/o/l", "tok")
	assert.NotNil(t, c.httpClient)
	assert.NotZero(t, c.httpClient.Timeout)
}

// --- Negotiate ---

func TestNegotiate_SuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer mytoken", r.Header.Get("Authorization"))

		json.NewEncoder(w).Encode(NegotiateResponse{
			CommonSeqs: map[string]int64{"s1": 5},
			ServerSeqs: map[string]int64{"s1": 10, "s2": 3},
			NeedsPush:  true,
			NeedsPull:  true,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "mytoken")
	resp, err := c.Negotiate(&NegotiateRequest{
		ProjectID: "proj",
		Streams:   []StreamSyncState{{StreamID: "s1", Name: "main", HeadSeq: 5}},
	})
	require.NoError(t, err)
	assert.True(t, resp.NeedsPush)
	assert.True(t, resp.NeedsPull)
	assert.Equal(t, int64(5), resp.CommonSeqs["s1"])
	assert.Equal(t, int64(10), resp.ServerSeqs["s1"])
}

func TestNegotiate_NoAuthHeader_WhenNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		json.NewEncoder(w).Encode(NegotiateResponse{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "")
	_, err := c.Negotiate(&NegotiateRequest{})
	assert.NoError(t, err)
}

func TestNegotiate_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "internal server error"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	_, err := c.Negotiate(&NegotiateRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// --- Push ---

func TestPush_SuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/push")

		var req PushRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "proj", req.ProjectID)
		assert.Len(t, req.Operations, 1)
		assert.Len(t, req.Objects, 1)

		json.NewEncoder(w).Encode(PushResponse{
			OK:         true,
			Applied:    1,
			ServerHead: 20,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	resp, err := c.Push(&PushRequest{
		ProjectID: "proj",
		StreamID:  "s1",
		FromSeq:   10,
		Operations: []OperationWire{
			{ID: "op1", Seq: 11, Type: "create", Path: "new.go"},
		},
		Objects: []ObjectData{
			{Hash: "abc", Content: []byte("content")},
		},
	})
	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, 1, resp.Applied)
	assert.Equal(t, int64(20), resp.ServerHead)
}

func TestPush_ConflictError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"message": "conflict: pull first"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	_, err := c.Push(&PushRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "409")
	assert.Contains(t, err.Error(), "conflict")
}

func TestPush_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "invalid token"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "bad")
	_, err := c.Push(&PushRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// --- Pull ---

func TestPull_SuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/pull")

		json.NewEncoder(w).Encode(PullResponse{
			Operations: []OperationWire{
				{ID: "op1", Seq: 6, Type: "create", Path: "a.go", ObjectRef: "hash1"},
				{ID: "op2", Seq: 7, Type: "modify", Path: "b.go"},
			},
			Objects: []ObjectData{
				{Hash: "hash1", Content: []byte("file content")},
			},
			ServerHead: 7,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	resp, err := c.Pull(&PullRequest{ProjectID: "p", StreamID: "s1", FromSeq: 5})
	require.NoError(t, err)
	assert.Len(t, resp.Operations, 2)
	assert.Len(t, resp.Objects, 1)
	assert.Equal(t, int64(7), resp.ServerHead)
	assert.Equal(t, "a.go", resp.Operations[0].Path)
	assert.Equal(t, "hash1", resp.Operations[0].ObjectRef)
}

func TestPull_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(PullResponse{
			Operations: []OperationWire{},
			Objects:    []ObjectData{},
			ServerHead: 5,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	resp, err := c.Pull(&PullRequest{ProjectID: "p", StreamID: "s1", FromSeq: 5})
	require.NoError(t, err)
	assert.Empty(t, resp.Operations)
	assert.Empty(t, resp.Objects)
	assert.Equal(t, int64(5), resp.ServerHead)
}

func TestPull_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	_, err := c.Pull(&PullRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

// --- Login ---

func TestLogin_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/auth/login", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "alice", body["username"])
		assert.Equal(t, "secret123", body["password"])

		json.NewEncoder(w).Encode(map[string]string{"token": "jwt-abc-xyz"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "")
	token, err := c.Login("alice", "secret123")
	require.NoError(t, err)
	assert.Equal(t, "jwt-abc-xyz", token)
}

func TestLogin_InvalidCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "invalid credentials"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "")
	_, err := c.Login("wrong", "wrong")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid credentials")
}

func TestLogin_ServerDown(t *testing.T) {
	// Use a server that's already closed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := NewClient(srv.URL+"/o/l", "")
	_, err := c.Login("user", "pass")
	assert.Error(t, err)
}

// --- Error handling ---

func TestReadError_JSONMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "bad request: missing field"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	_, err := c.Negotiate(&NegotiateRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad request: missing field")
}

func TestReadError_PlainTextBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("nginx: bad gateway"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	_, err := c.Negotiate(&NegotiateRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "502")
	assert.Contains(t, err.Error(), "nginx: bad gateway")
}

func TestReadError_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	_, err := c.Negotiate(&NegotiateRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestReadError_JSONWithErrorField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "not_found",
			"message": "project not found",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")
	_, err := c.Negotiate(&NegotiateRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "project not found")
}

// --- Wire types ---

func TestOperationWire_JSONRoundtrip(t *testing.T) {
	op := OperationWire{
		ID:        "op-1",
		Seq:       42,
		StreamID:  "s1",
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      "create",
		Path:      "src/main.go",
		Delta:     json.RawMessage(`{"op":"add"}`),
		ObjectRef: "hash123",
		ParentSeq: 41,
		Author:    "alice",
		Timestamp: "2024-01-01T00:00:00Z",
		Meta:      json.RawMessage(`{"size":100}`),
	}

	data, err := json.Marshal(op)
	require.NoError(t, err)

	var decoded OperationWire
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, op.ID, decoded.ID)
	assert.Equal(t, op.Seq, decoded.Seq)
	assert.Equal(t, op.Type, decoded.Type)
	assert.Equal(t, op.ObjectRef, decoded.ObjectRef)
}

func TestNegotiateRequest_JSONRoundtrip(t *testing.T) {
	req := NegotiateRequest{
		ProjectID: "my-project",
		Streams: []StreamSyncState{
			{StreamID: "s1", Name: "main", HeadSeq: 100},
			{StreamID: "s2", Name: "dev", HeadSeq: 50},
		},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded NegotiateRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "my-project", decoded.ProjectID)
	assert.Len(t, decoded.Streams, 2)
	assert.Equal(t, int64(100), decoded.Streams[0].HeadSeq)
}

func TestPushRequest_WithObjects(t *testing.T) {
	req := PushRequest{
		ProjectID: "p",
		StreamID:  "s1",
		FromSeq:   10,
		Operations: []OperationWire{
			{ID: "op1", Type: "create", ObjectRef: "hash1"},
		},
		Objects: []ObjectData{
			{Hash: "hash1", Content: []byte("file content here")},
		},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded PushRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Len(t, decoded.Objects, 1)
	assert.Equal(t, "hash1", decoded.Objects[0].Hash)
	assert.Equal(t, []byte("file content here"), decoded.Objects[0].Content)
}

func TestPushResponse_WithError(t *testing.T) {
	resp := PushResponse{
		OK:    false,
		Error: "conflict: base seq mismatch",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded PushResponse
	json.Unmarshal(data, &decoded)
	assert.False(t, decoded.OK)
	assert.Equal(t, "conflict: base seq mismatch", decoded.Error)
}

// --- Multiple API calls in sequence ---

func TestClient_NegotiateThenPushThenPull(t *testing.T) {
	callOrder := []string{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/o/l/api/v1/negotiate":
			callOrder = append(callOrder, "negotiate")
			json.NewEncoder(w).Encode(NegotiateResponse{NeedsPush: true})
		case r.URL.Path == "/o/l/api/v1/push":
			callOrder = append(callOrder, "push")
			json.NewEncoder(w).Encode(PushResponse{OK: true, Applied: 1, ServerHead: 15})
		case r.URL.Path == "/o/l/api/v1/pull":
			callOrder = append(callOrder, "pull")
			json.NewEncoder(w).Encode(PullResponse{ServerHead: 15})
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/o/l", "tok")

	_, err := c.Negotiate(&NegotiateRequest{})
	require.NoError(t, err)

	_, err = c.Push(&PushRequest{})
	require.NoError(t, err)

	_, err = c.Pull(&PullRequest{})
	require.NoError(t, err)

	assert.Equal(t, []string{"negotiate", "push", "pull"}, callOrder)
}
