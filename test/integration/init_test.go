package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/constructspace/loom/internal/cli"
	"github.com/constructspace/loom/internal/core"
)

func TestCLI_InitStatusCheckpointLog(t *testing.T) {
	dir := t.TempDir()

	// Create sample project
	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "docs", "readme.md"), []byte("# Test\n"), 0644)

	// Test init
	out := runCLI(t, "init", dir)
	if !strings.Contains(out, "Initialized Loom") {
		t.Errorf("init output missing 'Initialized Loom': %s", out)
	}
	if !strings.Contains(out, "code") {
		t.Errorf("init output missing 'code' space: %s", out)
	}
	if !strings.Contains(out, "docs") {
		t.Errorf("init output missing 'docs' space: %s", out)
	}

	// Test status
	out = runCLI(t, "-p", dir, "status")
	if !strings.Contains(out, "Stream:") {
		t.Errorf("status output missing 'Stream:': %s", out)
	}

	// Test checkpoint
	out = runCLI(t, "-p", dir, "checkpoint", "initial setup")
	if !strings.Contains(out, "Checkpoint created") {
		t.Errorf("checkpoint output missing 'Checkpoint created': %s", out)
	}

	// Test log
	out = runCLI(t, "-p", dir, "log")
	if !strings.Contains(out, "initial setup") {
		t.Errorf("log output missing 'initial setup': %s", out)
	}
}

func TestCLI_StreamOperations(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	runCLI(t, "init", dir)

	// Create stream
	out := runCLI(t, "-p", dir, "stream", "create", "feature/test")
	if !strings.Contains(out, "Created stream") {
		t.Errorf("stream create output: %s", out)
	}

	// List streams
	out = runCLI(t, "-p", dir, "stream", "list")
	if !strings.Contains(out, "main") {
		t.Errorf("stream list missing main: %s", out)
	}
	if !strings.Contains(out, "feature/test") {
		t.Errorf("stream list missing feature/test: %s", out)
	}

	// Switch stream
	out = runCLI(t, "-p", dir, "stream", "switch", "feature/test")
	if !strings.Contains(out, "Switched to") {
		t.Errorf("stream switch output: %s", out)
	}

	// Info
	out = runCLI(t, "-p", dir, "stream", "info", "feature/test")
	if !strings.Contains(out, "feature/test") {
		t.Errorf("stream info output: %s", out)
	}
}

func TestFullWorkflow_InitCheckpointModifyCheckpoint(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	// Init via core (not CLI) for direct access
	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	stream, _ := vault.ActiveStream()

	// First checkpoint
	cp1, err := vault.Checkpoints.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "v1",
		Author:   "test",
		Source:   core.SourceManual,
	})
	if err != nil {
		t.Fatalf("checkpoint 1: %v", err)
	}

	// Simulate file modification via op
	vault.OpWriter.Write(core.Operation{
		StreamID: stream.ID,
		SpaceID:  "code",
		EntityID: "main.go",
		Type:     core.OpModify,
		Path:     "main.go",
		Author:   "test",
	})

	// Second checkpoint
	cp2, err := vault.Checkpoints.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "v2 - added handler",
		Author:   "test",
		Source:   core.SourceManual,
	})
	if err != nil {
		t.Fatalf("checkpoint 2: %v", err)
	}

	// Verify parent chain
	if cp2.ParentID != cp1.ID {
		t.Errorf("expected cp2 parent = cp1 (%s), got %s", cp1.ID, cp2.ParentID)
	}

	// Verify log
	all, _ := vault.Checkpoints.List(stream.ID, 10)
	if len(all) != 2 {
		t.Errorf("expected 2 checkpoints, got %d", len(all))
	}

	vault.Close()
}

func TestCLI_LogSearchRespectsLimit(t *testing.T) {
	dir := t.TempDir()

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	defer vault.Close()

	stream, err := vault.ActiveStream()
	if err != nil {
		t.Fatalf("active stream: %v", err)
	}

	for i := 1; i <= 3; i++ {
		if _, err := vault.Checkpoints.Create(core.CheckpointInput{
			StreamID: stream.ID,
			Title:    "searchable checkpoint",
			Summary:  "match",
			Author:   "test",
			Source:   core.SourceManual,
		}); err != nil {
			t.Fatalf("checkpoint %d: %v", i, err)
		}
		if _, err := vault.OpWriter.Write(core.Operation{
			StreamID: stream.ID,
			SpaceID:  "code",
			EntityID: "file.txt",
			Type:     core.OpModify,
			Path:     "file.txt",
			Author:   "test",
		}); err != nil {
			t.Fatalf("write op %d: %v", i, err)
		}
	}

	out := runCLI(t, "-p", dir, "log", "--search", "searchable", "-n", "2")
	if strings.Count(out, "searchable checkpoint") != 2 {
		t.Fatalf("expected 2 matching checkpoints, got output:\n%s", out)
	}
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cli %v failed: %v\nOutput: %s", args, err, buf.String())
	}
	return buf.String()
}
