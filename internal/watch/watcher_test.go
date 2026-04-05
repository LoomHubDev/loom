package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/constructspace/loom/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupWatcherEnv creates a temp project directory with a go.mod and main.go,
// initialises a loom vault, and returns the vault + project dir.
func setupWatcherEnv(t *testing.T) (*core.Vault, string) {
	t.Helper()

	dir := t.TempDir()

	// Create minimal project files so space detection picks up "code".
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644))

	vault, err := core.InitVault(dir)
	require.NoError(t, err)
	t.Cleanup(func() { vault.Close() })

	return vault, dir
}

func TestWatcher_DetectsFileCreate(t *testing.T) {
	vault, dir := setupWatcherEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	headBefore, _ := vault.OpReader.Head()

	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()
	time.Sleep(200 * time.Millisecond) // let watcher start

	// Create a new file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main\n"), 0644))
	time.Sleep(500 * time.Millisecond) // debounce + processing

	cancel()
	<-done

	headAfter, _ := vault.OpReader.Head()
	assert.Greater(t, headAfter, headBefore, "head should advance after file create")
}

func TestWatcher_DetectsFileModify(t *testing.T) {
	vault, dir := setupWatcherEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	headBefore, _ := vault.OpReader.Head()

	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)

	// Modify existing file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644))
	time.Sleep(500 * time.Millisecond)

	cancel()
	<-done

	headAfter, _ := vault.OpReader.Head()
	assert.Greater(t, headAfter, headBefore, "head should advance after file modify")
}

func TestWatcher_DetectsFileDelete(t *testing.T) {
	vault, dir := setupWatcherEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	headBefore, _ := vault.OpReader.Head()

	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)

	// Remove a file.
	require.NoError(t, os.Remove(filepath.Join(dir, "main.go")))
	time.Sleep(500 * time.Millisecond)

	cancel()
	<-done

	headAfter, _ := vault.OpReader.Head()
	assert.Greater(t, headAfter, headBefore, "head should advance after file delete")
}

func TestWatcher_IgnoresLoomDir(t *testing.T) {
	vault, dir := setupWatcherEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	headBefore, _ := vault.OpReader.Head()

	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()
	time.Sleep(200 * time.Millisecond)

	// Write inside .loom/ — should be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".loom", "ignored.txt"), []byte("test"), 0644))
	time.Sleep(500 * time.Millisecond)

	cancel()
	<-done

	headAfter, _ := vault.OpReader.Head()
	assert.Equal(t, headBefore, headAfter, "head should NOT advance for .loom writes")
}

func TestWatcher_StopsCleanly(t *testing.T) {
	vault, _ := setupWatcherEnv(t)

	ctx, cancel := context.WithCancel(context.Background())

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err, "Start should return nil on cancellation")
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop within 2 seconds")
	}
}
