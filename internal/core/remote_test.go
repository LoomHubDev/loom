package core

import (
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/storage"
)

func setupTestDB(t *testing.T) *RemoteStore {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRemoteStore(db)
}

func TestRemoteAddAndGet(t *testing.T) {
	store := setupTestDB(t)

	err := store.Add("origin", "https://hub.example.com/alice/myproject", true)
	if err != nil {
		t.Fatal(err)
	}

	r, err := store.Get("origin")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "origin" {
		t.Errorf("expected name=origin, got %s", r.Name)
	}
	if r.URL != "https://hub.example.com/alice/myproject" {
		t.Errorf("expected url match, got %s", r.URL)
	}
	if !r.IsDefault {
		t.Error("expected default=true")
	}
}

func TestRemoteList(t *testing.T) {
	store := setupTestDB(t)

	store.Add("origin", "https://hub1.example.com/a/b", true)
	store.Add("backup", "https://hub2.example.com/a/b", false)

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 remotes, got %d", len(list))
	}
}

func TestRemoteDefault(t *testing.T) {
	store := setupTestDB(t)

	store.Add("backup", "https://hub2.example.com/a/b", false)
	store.Add("origin", "https://hub1.example.com/a/b", true)

	r, err := store.Default()
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "origin" {
		t.Errorf("expected default=origin, got %s", r.Name)
	}
}

func TestRemoteDefaultFallback(t *testing.T) {
	store := setupTestDB(t)

	store.Add("backup", "https://hub2.example.com/a/b", false)

	r, err := store.Default()
	if err != nil {
		t.Fatal(err)
	}
	// Should fall back to first
	if r.Name != "backup" {
		t.Errorf("expected fallback=backup, got %s", r.Name)
	}
}

func TestRemoteRemove(t *testing.T) {
	store := setupTestDB(t)

	store.Add("origin", "https://hub.example.com/a/b", true)
	err := store.Remove("origin")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Get("origin")
	if err == nil {
		t.Error("expected error after remove")
	}
}

func TestRemoteRemoveNotFound(t *testing.T) {
	store := setupTestDB(t)

	err := store.Remove("nonexistent")
	if err == nil {
		t.Error("expected error for missing remote")
	}
}

func TestRemoteUpdateSeqs(t *testing.T) {
	store := setupTestDB(t)

	store.Add("origin", "https://hub.example.com/a/b", true)

	store.UpdatePushSeq("origin", 42)
	store.UpdatePullSeq("origin", 37)

	r, _ := store.Get("origin")
	if r.PushSeq != 42 {
		t.Errorf("expected push_seq=42, got %d", r.PushSeq)
	}
	if r.PullSeq != 37 {
		t.Errorf("expected pull_seq=37, got %d", r.PullSeq)
	}
	if r.LastPush == "" {
		t.Error("expected last_push to be set")
	}
	if r.LastPull == "" {
		t.Error("expected last_pull to be set")
	}
}

func TestRemoteAuthToken(t *testing.T) {
	store := setupTestDB(t)

	store.Add("origin", "https://hub.example.com/a/b", true)

	err := store.SetAuthToken("origin", "test-token-123")
	if err != nil {
		t.Fatal(err)
	}

	token, err := store.GetAuthToken("origin")
	if err != nil {
		t.Fatal(err)
	}
	if token != "test-token-123" {
		t.Errorf("expected test-token-123, got %s", token)
	}

	// Update token
	store.SetAuthToken("origin", "new-token")
	token, _ = store.GetAuthToken("origin")
	if token != "new-token" {
		t.Errorf("expected new-token, got %s", token)
	}
}

func TestRemoteAuthTokenNotFound(t *testing.T) {
	store := setupTestDB(t)

	_, err := store.GetAuthToken("nonexistent")
	if err == nil {
		t.Error("expected error for missing token")
	}
}

func TestRemoteNoRemotes(t *testing.T) {
	store := setupTestDB(t)

	_, err := store.Default()
	if err == nil {
		t.Error("expected error when no remotes configured")
	}
}

