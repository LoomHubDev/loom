package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/constructspace/loom/internal/core"
	"github.com/stretchr/testify/require"
)

func runSpaceCmd(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())
	return buf.String()
}

func runSpaceCmdErr(t *testing.T, args ...string) error {
	t.Helper()
	var buf bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestSpace_List(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	// Create a docs directory so init detects it as a space
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0755))
	// Create a go.mod so init detects code space
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644))

	v, err := core.InitVault(dir)
	require.NoError(t, err)
	require.NoError(t, v.Close())

	out := runSpaceCmd(t, "--project", dir, "space", "list")
	if !strings.Contains(out, "code") {
		t.Fatalf("expected 'code' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "docs") {
		t.Fatalf("expected 'docs' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Spaces:") {
		t.Fatalf("expected 'Spaces:' header in output, got:\n%s", out)
	}
}

func TestSpace_Add(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644))

	v, err := core.InitVault(dir)
	require.NoError(t, err)
	require.NoError(t, v.Close())

	// Create the notes directory
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "notes"), 0755))

	out := runSpaceCmd(t, "--project", dir, "space", "add", "notes", "notes")
	if !strings.Contains(out, "Added space notes at notes") {
		t.Fatalf("expected confirmation message, got:\n%s", out)
	}

	// Verify config was updated
	configPath := filepath.Join(dir, ".loom", "config.toml")
	var cfg core.ProjectConfig
	_, err = toml.DecodeFile(configPath, &cfg)
	require.NoError(t, err)

	sc, ok := cfg.Spaces["notes"]
	if !ok {
		t.Fatalf("expected 'notes' space in config, got spaces: %v", cfg.Spaces)
	}
	if sc.Path != "notes" {
		t.Fatalf("expected path 'notes', got %q", sc.Path)
	}
	if sc.Adapter != "filesystem" {
		t.Fatalf("expected adapter 'filesystem', got %q", sc.Adapter)
	}
}

func TestSpace_AddDuplicate(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644))

	v, err := core.InitVault(dir)
	require.NoError(t, err)
	require.NoError(t, v.Close())

	err = runSpaceCmdErr(t, "--project", dir, "space", "add", "code", ".")
	if err == nil {
		t.Fatal("expected error when adding duplicate space, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got: %v", err)
	}
}

func TestSpace_Remove(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	// Create docs dir so init detects 2 spaces
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644))

	v, err := core.InitVault(dir)
	require.NoError(t, err)
	// Verify we have at least 2 spaces
	require.GreaterOrEqual(t, len(v.Config.Spaces), 2, "expected at least 2 spaces for remove test")
	require.NoError(t, v.Close())

	out := runSpaceCmd(t, "--project", dir, "space", "remove", "docs")
	if !strings.Contains(out, "Removed space docs") {
		t.Fatalf("expected removal confirmation, got:\n%s", out)
	}

	// Verify config was updated
	configPath := filepath.Join(dir, ".loom", "config.toml")
	var cfg core.ProjectConfig
	_, err = toml.DecodeFile(configPath, &cfg)
	require.NoError(t, err)

	if _, ok := cfg.Spaces["docs"]; ok {
		t.Fatalf("expected 'docs' space to be removed from config")
	}
}

func TestSpace_RemoveLastFails(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	// Only go.mod, no docs dir → only "code" space detected
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644))

	v, err := core.InitVault(dir)
	require.NoError(t, err)
	// Ensure exactly 1 space
	require.Equal(t, 1, len(v.Config.Spaces), "expected exactly 1 space for last-space test")
	require.NoError(t, v.Close())

	err = runSpaceCmdErr(t, "--project", dir, "space", "remove", "code")
	if err == nil {
		t.Fatal("expected error when removing last space, got nil")
	}
	if !strings.Contains(err.Error(), "cannot remove the last space") {
		t.Fatalf("expected 'cannot remove the last space' error, got: %v", err)
	}
}
