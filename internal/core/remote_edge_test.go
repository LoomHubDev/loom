package core

import (
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRemoteTestify(t *testing.T) *RemoteStore {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewRemoteStore(db)
}

func TestRemoteStore_AddDuplicateName(t *testing.T) {
	store := setupRemoteTestify(t)

	err := store.Add("origin", "https://example.com/a/b", true)
	require.NoError(t, err)

	err = store.Add("origin", "https://example.com/c/d", false)
	assert.Error(t, err, "duplicate name should fail")
}

func TestRemoteStore_AddSwitchesDefault(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("first", "https://first.com/a/b", true)
	store.Add("second", "https://second.com/a/b", true) // This should clear first's default

	first, err := store.Get("first")
	require.NoError(t, err)
	assert.False(t, first.IsDefault, "first should no longer be default")

	second, err := store.Get("second")
	require.NoError(t, err)
	assert.True(t, second.IsDefault, "second should be default")
}

func TestRemoteStore_AddMultipleNonDefault(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("a", "https://a.com/x/y", false)
	store.Add("b", "https://b.com/x/y", false)
	store.Add("c", "https://c.com/x/y", false)

	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list, 3)

	for _, r := range list {
		assert.False(t, r.IsDefault)
	}
}

func TestRemoteStore_DefaultFallbackToFirst(t *testing.T) {
	store := setupRemoteTestify(t)

	// Add non-default remotes
	store.Add("backup", "https://backup.com/a/b", false)
	store.Add("staging", "https://staging.com/a/b", false)

	r, err := store.Default()
	require.NoError(t, err)
	// Should fall back to first one
	assert.NotEmpty(t, r.Name)
}

func TestRemoteStore_DefaultNoRemotes(t *testing.T) {
	store := setupRemoteTestify(t)

	_, err := store.Default()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no remotes configured")
}

func TestRemoteStore_GetNonexistent(t *testing.T) {
	store := setupRemoteTestify(t)

	_, err := store.Get("nonexistent")
	assert.Error(t, err)
}

func TestRemoteStore_RemoveNonexistent(t *testing.T) {
	store := setupRemoteTestify(t)

	err := store.Remove("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRemoteStore_RemoveReducesList(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("origin", "https://origin.com/a/b", true)
	store.Add("backup", "https://backup.com/a/b", false)

	err := store.Remove("origin")
	require.NoError(t, err)

	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "backup", list[0].Name)
}

func TestRemoteStore_ListEmpty(t *testing.T) {
	store := setupRemoteTestify(t)

	list, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestRemoteStore_ListOrdered(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("charlie", "https://c.com/a/b", false)
	store.Add("alpha", "https://a.com/a/b", false)
	store.Add("bravo", "https://b.com/a/b", false)

	list, err := store.List()
	require.NoError(t, err)
	require.Len(t, list, 3)

	// Ordered by name
	assert.Equal(t, "alpha", list[0].Name)
	assert.Equal(t, "bravo", list[1].Name)
	assert.Equal(t, "charlie", list[2].Name)
}

func TestRemoteStore_UpdatePushSeqSetsLastPush(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("origin", "https://example.com/a/b", true)

	err := store.UpdatePushSeq("origin", 100)
	require.NoError(t, err)

	r, err := store.Get("origin")
	require.NoError(t, err)
	assert.Equal(t, int64(100), r.PushSeq)
	assert.NotEmpty(t, r.LastPush)
}

func TestRemoteStore_UpdatePullSeqSetsLastPull(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("origin", "https://example.com/a/b", true)

	err := store.UpdatePullSeq("origin", 50)
	require.NoError(t, err)

	r, err := store.Get("origin")
	require.NoError(t, err)
	assert.Equal(t, int64(50), r.PullSeq)
	assert.NotEmpty(t, r.LastPull)
}

func TestRemoteStore_UpdateSeqsMultipleTimes(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("origin", "https://example.com/a/b", true)

	store.UpdatePushSeq("origin", 10)
	store.UpdatePushSeq("origin", 20)
	store.UpdatePushSeq("origin", 30)

	r, _ := store.Get("origin")
	assert.Equal(t, int64(30), r.PushSeq)
}

func TestRemoteStore_AuthTokenSetAndGet(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("origin", "https://example.com/a/b", true)

	err := store.SetAuthToken("origin", "my-secret-token")
	require.NoError(t, err)

	token, err := store.GetAuthToken("origin")
	require.NoError(t, err)
	assert.Equal(t, "my-secret-token", token)
}

func TestRemoteStore_AuthTokenUpdate(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("origin", "https://example.com/a/b", true)

	store.SetAuthToken("origin", "token-v1")
	store.SetAuthToken("origin", "token-v2")
	store.SetAuthToken("origin", "token-v3")

	token, _ := store.GetAuthToken("origin")
	assert.Equal(t, "token-v3", token)
}

func TestRemoteStore_AuthTokenNotFound(t *testing.T) {
	store := setupRemoteTestify(t)

	_, err := store.GetAuthToken("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no auth token")
}

func TestRemoteStore_AuthTokenPerRemote(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("origin", "https://origin.com/a/b", true)
	store.Add("backup", "https://backup.com/a/b", false)

	store.SetAuthToken("origin", "origin-token")
	store.SetAuthToken("backup", "backup-token")

	tok1, _ := store.GetAuthToken("origin")
	tok2, _ := store.GetAuthToken("backup")

	assert.Equal(t, "origin-token", tok1)
	assert.Equal(t, "backup-token", tok2)
}

func TestRemoteStore_GetPreservesAllFields(t *testing.T) {
	store := setupRemoteTestify(t)

	err := store.Add("origin", "https://hub.example.com/owner/project", true)
	require.NoError(t, err)

	store.UpdatePushSeq("origin", 42)
	store.UpdatePullSeq("origin", 37)

	r, err := store.Get("origin")
	require.NoError(t, err)

	assert.Equal(t, "origin", r.Name)
	assert.Equal(t, "https://hub.example.com/owner/project", r.URL)
	assert.True(t, r.IsDefault)
	assert.Equal(t, int64(42), r.PushSeq)
	assert.Equal(t, int64(37), r.PullSeq)
	assert.NotEmpty(t, r.LastPush)
	assert.NotEmpty(t, r.LastPull)
	assert.NotEmpty(t, r.CreatedAt)
}

func TestRemoteStore_InitialSeqsAreZero(t *testing.T) {
	store := setupRemoteTestify(t)

	store.Add("origin", "https://example.com/a/b", true)

	r, _ := store.Get("origin")
	assert.Equal(t, int64(0), r.PushSeq)
	assert.Equal(t, int64(0), r.PullSeq)
	assert.Empty(t, r.LastPush)
	assert.Empty(t, r.LastPull)
}
