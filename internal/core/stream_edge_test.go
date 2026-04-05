package core

import (
	"path/filepath"
	"testing"

	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupStreamTestify(t *testing.T) *StreamManager {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewStreamManager(db)
}

func TestStreamManager_CreatePopulatesAllFields(t *testing.T) {
	sm := setupStreamTestify(t)

	s, err := sm.Create("main")
	require.NoError(t, err)

	assert.NotEmpty(t, s.ID)
	assert.Len(t, s.ID, 26) // ULID length
	assert.Equal(t, "main", s.Name)
	assert.Equal(t, int64(0), s.HeadSeq)
	assert.NotEmpty(t, s.CreatedAt)
	assert.NotEmpty(t, s.UpdatedAt)
	assert.Equal(t, "active", s.Status)
	assert.Empty(t, s.ParentID)
	assert.Equal(t, int64(0), s.ForkSeq)
}

func TestStreamManager_CreateDuplicateName(t *testing.T) {
	sm := setupStreamTestify(t)

	_, err := sm.Create("main")
	require.NoError(t, err)

	_, err = sm.Create("main")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create stream")
}

func TestStreamManager_GetByID(t *testing.T) {
	sm := setupStreamTestify(t)

	created, err := sm.Create("test-stream")
	require.NoError(t, err)

	got, err := sm.GetByID(created.ID)
	require.NoError(t, err)

	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "test-stream", got.Name)
	assert.Equal(t, created.HeadSeq, got.HeadSeq)
	assert.Equal(t, created.Status, got.Status)
}

func TestStreamManager_GetByIDNotFound(t *testing.T) {
	sm := setupStreamTestify(t)

	_, err := sm.GetByID("nonexistent-id")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrStreamNotFound)
}

func TestStreamManager_GetByNameNotFound(t *testing.T) {
	sm := setupStreamTestify(t)

	_, err := sm.GetByName("nonexistent")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrStreamNotFound)
}

func TestStreamManager_ForkSetsParentFields(t *testing.T) {
	sm := setupStreamTestify(t)

	parent, err := sm.Create("main")
	require.NoError(t, err)

	forked, err := sm.Fork("main", "feature/auth")
	require.NoError(t, err)

	assert.NotEmpty(t, forked.ID)
	assert.NotEqual(t, parent.ID, forked.ID)
	assert.Equal(t, "feature/auth", forked.Name)
	assert.Equal(t, parent.ID, forked.ParentID)
	assert.Equal(t, parent.HeadSeq, forked.ForkSeq)
	assert.Equal(t, parent.HeadSeq, forked.HeadSeq)
	assert.Equal(t, "active", forked.Status)
}

func TestStreamManager_ForkNonexistentParent(t *testing.T) {
	sm := setupStreamTestify(t)

	_, err := sm.Fork("nonexistent", "child")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parent stream")
}

func TestStreamManager_ForkDuplicateName(t *testing.T) {
	sm := setupStreamTestify(t)

	sm.Create("main")
	sm.Create("existing")

	_, err := sm.Fork("main", "existing")
	assert.Error(t, err)
}

func TestStreamManager_ForkPreservesParentHeadSeq(t *testing.T) {
	sm := setupStreamTestify(t)

	// Create parent and get a non-zero head via direct DB manipulation
	parent, err := sm.Create("main")
	require.NoError(t, err)

	// Fork from main with 0 head
	forked, err := sm.Fork("main", "branch")
	require.NoError(t, err)

	assert.Equal(t, parent.HeadSeq, forked.ForkSeq)
}

func TestStreamManager_ListEmpty(t *testing.T) {
	sm := setupStreamTestify(t)

	streams, err := sm.List()
	require.NoError(t, err)
	assert.Empty(t, streams)
}

func TestStreamManager_ListMultiple(t *testing.T) {
	sm := setupStreamTestify(t)

	sm.Create("main")
	sm.Create("dev")
	sm.Create("staging")
	sm.Fork("main", "feature/x")

	streams, err := sm.List()
	require.NoError(t, err)
	assert.Len(t, streams, 4)

	// Verify all names present
	names := make(map[string]bool)
	for _, s := range streams {
		names[s.Name] = true
	}
	assert.True(t, names["main"])
	assert.True(t, names["dev"])
	assert.True(t, names["staging"])
	assert.True(t, names["feature/x"])
}

func TestStreamManager_ListOrderByUpdatedDesc(t *testing.T) {
	sm := setupStreamTestify(t)

	sm.Create("alpha")
	sm.Create("beta")
	sm.Create("gamma")

	streams, err := sm.List()
	require.NoError(t, err)
	require.Len(t, streams, 3)

	// Most recently created (updated) should be first
	// Since they're created almost simultaneously, just verify ordering exists
	for i := 1; i < len(streams); i++ {
		assert.True(t, streams[i-1].UpdatedAt >= streams[i].UpdatedAt,
			"streams should be ordered by updated_at DESC")
	}
}

func TestStreamManager_SetActiveNonexistent(t *testing.T) {
	sm := setupStreamTestify(t)

	err := sm.SetActive("nonexistent")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrStreamNotFound)
}

func TestStreamManager_SetActiveSwitch(t *testing.T) {
	sm := setupStreamTestify(t)

	sm.Create("main")
	sm.Create("dev")
	sm.Create("staging")

	// Initial default
	name, err := sm.ActiveName()
	require.NoError(t, err)
	assert.Equal(t, "main", name)

	// Switch to dev
	err = sm.SetActive("dev")
	require.NoError(t, err)
	name, _ = sm.ActiveName()
	assert.Equal(t, "dev", name)

	// Switch to staging
	err = sm.SetActive("staging")
	require.NoError(t, err)
	name, _ = sm.ActiveName()
	assert.Equal(t, "staging", name)

	// Switch back to main
	err = sm.SetActive("main")
	require.NoError(t, err)
	name, _ = sm.ActiveName()
	assert.Equal(t, "main", name)
}

func TestStreamManager_ActiveNameDefaultsToMain(t *testing.T) {
	sm := setupStreamTestify(t)

	// No active set yet, should default to "main"
	name, err := sm.ActiveName()
	require.NoError(t, err)
	assert.Equal(t, "main", name)
}

func TestStreamManager_ForkChain(t *testing.T) {
	sm := setupStreamTestify(t)

	sm.Create("main")
	fork1, err := sm.Fork("main", "feature")
	require.NoError(t, err)

	fork2, err := sm.Fork("feature", "feature-sub")
	require.NoError(t, err)

	// Verify chain
	main, _ := sm.GetByName("main")
	assert.Empty(t, main.ParentID)
	assert.Equal(t, main.ID, fork1.ParentID)
	assert.Equal(t, fork1.ID, fork2.ParentID)
}

func TestStreamManager_StreamNamesWithSpecialChars(t *testing.T) {
	sm := setupStreamTestify(t)

	names := []string{
		"feature/auth-flow",
		"bugfix/issue-123",
		"release/v1.0.0",
		"user/alice/experiment",
	}

	for _, name := range names {
		s, err := sm.Create(name)
		require.NoError(t, err, "create %s", name)
		assert.Equal(t, name, s.Name)

		got, err := sm.GetByName(name)
		require.NoError(t, err, "get %s", name)
		assert.Equal(t, name, got.Name)
	}
}

func TestStreamManager_GetByIDAndGetByNameConsistent(t *testing.T) {
	sm := setupStreamTestify(t)

	created, err := sm.Create("test")
	require.NoError(t, err)

	byName, err := sm.GetByName("test")
	require.NoError(t, err)

	byID, err := sm.GetByID(created.ID)
	require.NoError(t, err)

	assert.Equal(t, byName.ID, byID.ID)
	assert.Equal(t, byName.Name, byID.Name)
	assert.Equal(t, byName.HeadSeq, byID.HeadSeq)
	assert.Equal(t, byName.Status, byID.Status)
}
