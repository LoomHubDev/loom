package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/constructspace/loom/internal/core"
	lsync "github.com/constructspace/loom/internal/sync"
)

func TestSendSplitsOperationsIntoBatches(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "" })

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}
	stream, err := vault.ActiveStream()
	if err != nil {
		t.Fatalf("active stream: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, err := vault.OpWriter.Write(core.Operation{
			StreamID: stream.ID,
			SpaceID:  "code",
			EntityID: "main.go",
			Type:     core.OpModify,
			Path:     "main.go",
			Author:   "test",
			Meta:     core.OpMeta{Size: int64(16 + i)},
		}); err != nil {
			t.Fatalf("write op %d: %v", i, err)
		}
	}

	remotes := core.NewRemoteStore(vault.DB)

	var mu sync.Mutex
	var pushOpCounts []int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alice/repo/api/v1/negotiate":
			var req lsync.NegotiateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode negotiate: %v", err)
			}
			json.NewEncoder(w).Encode(lsync.NegotiateResponse{
				CommonSeqs: map[string]int64{req.Streams[0].StreamID: 0},
				ServerSeqs: map[string]int64{req.Streams[0].StreamID: 0},
				NeedsPush:  true,
			})
		case "/alice/repo/api/v1/push":
			var req lsync.PushRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode push: %v", err)
			}
			mu.Lock()
			pushOpCounts = append(pushOpCounts, len(req.Operations))
			mu.Unlock()
			json.NewEncoder(w).Encode(lsync.PushResponse{OK: true, Applied: len(req.Operations), ServerHead: req.Operations[len(req.Operations)-1].Seq})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := remotes.Add("origin", server.URL+"/alice/repo", true); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	if err := remotes.SetAuthToken("origin", "token"); err != nil {
		t.Fatalf("set auth token: %v", err)
	}
	if err := vault.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	if err := os.Setenv("LOOM_SEND_BATCH_SIZE", "2"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("LOOM_SEND_BATCH_SIZE") })

	var out bytes.Buffer
	cmd := newSendCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"origin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("send failed: %v\n%s", err, out.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(pushOpCounts) < 2 {
		t.Fatalf("expected multiple push requests, got %v", pushOpCounts)
	}
	for _, count := range pushOpCounts {
		if count > 2 {
			t.Fatalf("expected each push to respect batch size 2, got %v", pushOpCounts)
		}
	}
}
