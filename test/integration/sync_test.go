package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/constructspace/loom/internal/cli"
	"github.com/constructspace/loom/internal/core"
	lsync "github.com/constructspace/loom/internal/sync"
)

func TestCLI_SendUsesActiveStreamStateInsteadOfRemoteGlobalPushSeq(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), []byte("package main\n"))
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"))

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	mainStream, err := vault.Streams.GetByName("main")
	if err != nil {
		t.Fatalf("get main stream: %v", err)
	}

	featureStream, err := vault.Streams.Fork("main", "feature")
	if err != nil {
		t.Fatalf("fork feature stream: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: featureStream.ID,
		SpaceID:  "code",
		EntityID: "feature.txt",
		Type:     core.OpCreate,
		Path:     "feature.txt",
		Author:   "test",
		Meta:     core.OpMeta{Size: 7},
	}); err != nil {
		t.Fatalf("write feature op: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: mainStream.ID,
		SpaceID:  "code",
		EntityID: "main.go",
		Type:     core.OpModify,
		Path:     "main.go",
		Author:   "test",
		Meta:     core.OpMeta{Size: 14},
	}); err != nil {
		t.Fatalf("write main op: %v", err)
	}

	remotes := core.NewRemoteStore(vault.DB)

	var mu sync.Mutex
	pushesByStream := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alice/repo/api/v1/negotiate":
			var req lsync.NegotiateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode negotiate: %v", err)
			}
			resp := lsync.NegotiateResponse{
				CommonSeqs: map[string]int64{req.Streams[0].StreamID: 0},
				ServerSeqs: map[string]int64{req.Streams[0].StreamID: 0},
				NeedsPush:  true,
			}
			writeJSON(t, w, resp)
		case "/alice/repo/api/v1/push":
			var req lsync.PushRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode push: %v", err)
			}
			mu.Lock()
			pushesByStream[req.StreamID]++
			mu.Unlock()
			writeJSON(t, w, lsync.PushResponse{OK: true, Applied: len(req.Operations), ServerHead: 999})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := remotes.Add("origin", server.URL+"/alice/repo", true); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	if err := remotes.SetAuthToken("origin", "test-token"); err != nil {
		t.Fatalf("set auth token: %v", err)
	}
	if err := vault.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	runCLI(t, "-p", dir, "send", "origin")
	runCLI(t, "-p", dir, "stream", "switch", "feature")
	runCLI(t, "-p", dir, "send", "origin")

	mu.Lock()
	defer mu.Unlock()

	if pushesByStream[mainStream.ID] == 0 {
		t.Fatalf("expected a push for main stream")
	}
	if pushesByStream[featureStream.ID] == 0 {
		t.Fatalf("expected a push for feature stream; send incorrectly treated it as already synced")
	}
}

func TestCLI_SendConflictPreservesPushState(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), []byte("package main\n"))
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"))

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	stream, err := vault.ActiveStream()
	if err != nil {
		t.Fatalf("active stream: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: stream.ID,
		SpaceID:  "code",
		EntityID: "main.go",
		Type:     core.OpModify,
		Path:     "main.go",
		Author:   "test",
		Meta:     core.OpMeta{Size: 14},
	}); err != nil {
		t.Fatalf("write main op: %v", err)
	}

	remotes := core.NewRemoteStore(vault.DB)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alice/repo/api/v1/negotiate":
			writeJSON(t, w, lsync.NegotiateResponse{
				CommonSeqs: map[string]int64{stream.ID: 0},
				ServerSeqs: map[string]int64{stream.ID: 0},
				NeedsPush:  true,
			})
		case "/alice/repo/api/v1/push":
			w.WriteHeader(http.StatusConflict)
			writeJSON(t, w, map[string]string{"message": "conflict: pull first"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := remotes.Add("origin", server.URL+"/alice/repo", true); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	if err := remotes.SetAuthToken("origin", "test-token"); err != nil {
		t.Fatalf("set auth token: %v", err)
	}
	if err := vault.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	_, err = executeCLI(dir, "send", "origin")
	if err == nil {
		t.Fatalf("expected send to fail on remote conflict")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if !strings.Contains(err.Error(), "pull first") {
		t.Fatalf("expected pull-first guidance, got %v", err)
	}

	reopened, err := core.OpenVault(dir)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	defer reopened.Close()

	pushSeq, err := core.NewRemoteStore(reopened.DB).GetStreamPushSeq("origin", stream.ID)
	if err != nil {
		t.Fatalf("read stream push seq: %v", err)
	}
	if pushSeq != 0 {
		t.Fatalf("expected failed send to leave push seq at 0, got %d", pushSeq)
	}

	remote, err := core.NewRemoteStore(reopened.DB).Get("origin")
	if err != nil {
		t.Fatalf("get remote: %v", err)
	}
	if remote.PushSeq != 0 {
		t.Fatalf("expected failed send to leave remote push seq at 0, got %d", remote.PushSeq)
	}
}

func TestCLI_ReceivePreservesRemoteOperationIdentityAndProgress(t *testing.T) {
	dir := t.TempDir()

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	stream, err := vault.ActiveStream()
	if err != nil {
		t.Fatalf("active stream: %v", err)
	}

	remoteStreamID := stream.ID
	remotes := core.NewRemoteStore(vault.DB)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alice/repo/api/v1/negotiate":
			writeJSON(t, w, lsync.NegotiateResponse{
				CommonSeqs: map[string]int64{remoteStreamID: 0},
				ServerSeqs: map[string]int64{remoteStreamID: 5},
				NeedsPull:  true,
			})
		case "/alice/repo/api/v1/pull":
			writeJSON(t, w, lsync.PullResponse{
				Operations: []lsync.OperationWire{
					{
						ID:       "remote-op-5",
						Seq:      5,
						StreamID: remoteStreamID,
						SpaceID:  "code",
						EntityID: "remote.txt",
						Type:     string(core.OpCreate),
						Path:     "remote.txt",
						Author:   "remote-user",
						Timestamp: "2026-04-04T03:00:00Z",
					},
				},
				ServerHead: 5,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := remotes.Add("origin", server.URL+"/alice/repo", true); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	if err := remotes.SetAuthToken("origin", "test-token"); err != nil {
		t.Fatalf("set auth token: %v", err)
	}
	if err := vault.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	runCLI(t, "-p", dir, "receive", "origin")

	reopened, err := core.OpenVault(dir)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	defer reopened.Close()

	var seq int64
	err = reopened.DB.QueryRow("SELECT seq FROM operations WHERE id = ?", "remote-op-5").Scan(&seq)
	if err != nil {
		t.Fatalf("expected received operation to preserve remote id: %v", err)
	}
	if seq != 5 {
		t.Fatalf("expected received operation seq 5, got %d", seq)
	}

	remote, err := core.NewRemoteStore(reopened.DB).Get("origin")
	if err != nil {
		t.Fatalf("get remote: %v", err)
	}
	if remote.PullSeq != 5 {
		t.Fatalf("expected pull seq to track remote server head 5, got %d", remote.PullSeq)
	}
}

func TestCLI_ReceiveRejectsMismatchedObjectHash(t *testing.T) {
	dir := t.TempDir()

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	stream, err := vault.ActiveStream()
	if err != nil {
		t.Fatalf("active stream: %v", err)
	}

	badHash := strings.Repeat("a", 64)
	remotes := core.NewRemoteStore(vault.DB)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alice/repo/api/v1/negotiate":
			writeJSON(t, w, lsync.NegotiateResponse{
				CommonSeqs: map[string]int64{stream.ID: 0},
				ServerSeqs: map[string]int64{stream.ID: 1},
				NeedsPull:  true,
			})
		case "/alice/repo/api/v1/pull":
			writeJSON(t, w, lsync.PullResponse{
				Operations: []lsync.OperationWire{
					{
						ID:        "remote-op-bad",
						Seq:       1,
						StreamID:  stream.ID,
						SpaceID:   "code",
						EntityID:  "remote.txt",
						Type:      string(core.OpCreate),
						Path:      "remote.txt",
						ObjectRef: badHash,
						Author:    "remote-user",
						Timestamp: "2026-04-04T03:05:00Z",
					},
				},
				Objects: []lsync.ObjectData{
					{Hash: badHash, Content: []byte("this does not match the declared hash")},
				},
				ServerHead: 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := remotes.Add("origin", server.URL+"/alice/repo", true); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	if err := remotes.SetAuthToken("origin", "test-token"); err != nil {
		t.Fatalf("set auth token: %v", err)
	}
	if err := vault.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	_, err = executeCLI(dir, "receive", "origin")
	if err == nil {
		t.Fatalf("expected receive to reject mismatched object hash")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch error, got %v", err)
	}
}

func executeCLI(projectDir string, args ...string) (string, error) {
	var buf bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	fullArgs := append([]string{"-p", projectDir}, args...)
	cmd.SetArgs(fullArgs)
	err := cmd.Execute()
	return buf.String(), err
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}
