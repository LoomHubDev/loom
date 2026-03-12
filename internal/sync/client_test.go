package sync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClientParsesURL(t *testing.T) {
	c := NewClient("https://hub.example.com/alice/myproject", "tok123")
	if c.baseURL != "https://hub.example.com" {
		t.Errorf("expected baseURL=https://hub.example.com, got %s", c.baseURL)
	}
	if c.owner != "alice" {
		t.Errorf("expected owner=alice, got %s", c.owner)
	}
	if c.loom != "myproject" {
		t.Errorf("expected loom=myproject, got %s", c.loom)
	}
	if c.token != "tok123" {
		t.Errorf("expected token=tok123, got %s", c.token)
	}
}

func TestNegotiate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alice/proj/api/v1/negotiate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer testtoken" {
			t.Error("expected auth header")
		}

		var req NegotiateRequest
		json.NewDecoder(r.Body).Decode(&req)

		json.NewEncoder(w).Encode(NegotiateResponse{
			CommonSeqs: map[string]int64{"s1": 5},
			ServerSeqs: map[string]int64{"s1": 10},
			NeedsPush:  false,
			NeedsPull:  true,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/alice/proj", "testtoken")
	resp, err := c.Negotiate(&NegotiateRequest{
		ProjectID: "proj",
		Streams:   []StreamSyncState{{StreamID: "s1", Name: "main", HeadSeq: 5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.NeedsPush {
		t.Error("expected needs_push=false")
	}
	if !resp.NeedsPull {
		t.Error("expected needs_pull=true")
	}
	if resp.ServerSeqs["s1"] != 10 {
		t.Errorf("expected server_seq=10, got %d", resp.ServerSeqs["s1"])
	}
}

func TestPush(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bob/repo/api/v1/push" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req PushRequest
		json.NewDecoder(r.Body).Decode(&req)

		if len(req.Operations) != 2 {
			t.Errorf("expected 2 ops, got %d", len(req.Operations))
		}

		json.NewEncoder(w).Encode(PushResponse{
			OK:         true,
			Applied:    2,
			ServerHead: 15,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/bob/repo", "tok")
	resp, err := c.Push(&PushRequest{
		ProjectID: "repo",
		StreamID:  "s1",
		FromSeq:   10,
		Operations: []OperationWire{
			{ID: "op1", Seq: 11, Type: "create", Path: "a.txt"},
			{ID: "op2", Seq: 12, Type: "modify", Path: "b.txt"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if resp.Applied != 2 {
		t.Errorf("expected applied=2, got %d", resp.Applied)
	}
}

func TestPull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(PullResponse{
			Operations: []OperationWire{
				{ID: "op1", Seq: 6, Type: "create", Path: "new.txt", ObjectRef: "abc123"},
			},
			Objects: []ObjectData{
				{Hash: "abc123", Content: []byte("hello")},
			},
			ServerHead: 6,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/a/b", "tok")
	resp, err := c.Pull(&PullRequest{ProjectID: "b", StreamID: "s1", FromSeq: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Operations) != 1 {
		t.Fatalf("expected 1 op, got %d", len(resp.Operations))
	}
	if len(resp.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(resp.Objects))
	}
	if string(resp.Objects[0].Content) != "hello" {
		t.Errorf("expected content=hello, got %s", string(resp.Objects[0].Content))
	}
}

func TestErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "bad token"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/a/b", "badtoken")
	_, err := c.Negotiate(&NegotiateRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "hub error 401: bad token" {
		t.Errorf("unexpected error: %s", err.Error())
	}
}

func TestLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "alice" || body["password"] != "secret" {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"message": "invalid credentials"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "jwt-token-xyz"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/a/b", "")
	token, err := c.Login("alice", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if token != "jwt-token-xyz" {
		t.Errorf("expected jwt-token-xyz, got %s", token)
	}
}
