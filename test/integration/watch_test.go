package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/watch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatch_EndToEnd_CreateModifyDelete(t *testing.T) {
	dir := t.TempDir()

	// Create sample project files
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0644))

	// Init vault
	vault, err := core.InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	// Record head before watching
	headBefore, err := vault.OpReader.Head()
	require.NoError(t, err)

	// Start watcher in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher, err := watch.NewWatcher(vault, watch.WatcherConfig{
		DebounceMs:     50,
		AutoCheckpoint: false,
	})
	require.NoError(t, err)

	watchErr := make(chan error, 1)
	go func() {
		watchErr <- watcher.Start(ctx)
	}()

	// Wait for watcher startup
	time.Sleep(100 * time.Millisecond)

	// Create handler.go
	handlerPath := filepath.Join(dir, "handler.go")
	require.NoError(t, os.WriteFile(handlerPath, []byte("package main\n\nfunc handler() {}\n"), 0644))
	time.Sleep(200 * time.Millisecond)

	// Modify handler.go
	require.NoError(t, os.WriteFile(handlerPath, []byte("package main\n\nfunc handler() { return }\n"), 0644))
	time.Sleep(200 * time.Millisecond)

	// Delete handler.go
	require.NoError(t, os.Remove(handlerPath))
	time.Sleep(200 * time.Millisecond)

	// Stop watcher
	cancel()
	select {
	case err := <-watchErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop in time")
	}

	// Verify ops were recorded
	headAfter, err := vault.OpReader.Head()
	require.NoError(t, err)
	assert.Greater(t, headAfter, headBefore, "expected ops to be recorded")

	// Read ops in range and verify at least 2 ops
	ops, err := vault.OpReader.ReadRange(headBefore+1, headAfter)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(ops), 2, "expected at least 2 ops recorded")
}

func TestWatch_EndToEnd_AutoCheckpoint(t *testing.T) {
	dir := t.TempDir()

	// Create sample project files (go.mod triggers "code" space detection)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0644))

	// Init vault
	vault, err := core.InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	// Get active stream
	stream, err := vault.ActiveStream()
	require.NoError(t, err)

	// Start watcher in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher, err := watch.NewWatcher(vault, watch.WatcherConfig{
		DebounceMs:      50,
		AutoCheckpoint:  true,
		IntervalOps:     3,
		IntervalSeconds: 9999,
	})
	require.NoError(t, err)

	watchErr := make(chan error, 1)
	go func() {
		watchErr <- watcher.Start(ctx)
	}()

	// Wait for watcher startup
	time.Sleep(100 * time.Millisecond)

	// Create 5 files in a loop with 150ms between each
	for i := 0; i < 5; i++ {
		fileName := fmt.Sprintf("file_%d.go", i)
		filePath := filepath.Join(dir, fileName)
		content := fmt.Sprintf("package main\n\n// file %d\n", i)
		require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))
		time.Sleep(150 * time.Millisecond)
	}

	// Wait for auto-checkpoint evaluation
	time.Sleep(500 * time.Millisecond)

	// Stop watcher
	cancel()
	select {
	case err := <-watchErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop in time")
	}

	// List checkpoints and count auto ones
	checkpoints, err := vault.Checkpoints.List(stream.ID, 100)
	require.NoError(t, err)

	autoCount := 0
	for _, cp := range checkpoints {
		if cp.Source == core.SourceAuto {
			autoCount++
		}
	}

	assert.Greater(t, autoCount, 0, "expected at least one auto-checkpoint to be created")
}
