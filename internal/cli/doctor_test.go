package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/constructspace/loom/internal/core"
	"github.com/stretchr/testify/require"
)

func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	require.NoError(t, err)
	return buf.String()
}

func TestDoctor_HealthyVault(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	_, err := core.InitVault(dir)
	require.NoError(t, err)

	out := runCmd(t, "--project", dir, "doctor")
	if !strings.Contains(out, "All checks passed") {
		t.Fatalf("expected 'All checks passed' in output, got:\n%s", out)
	}
}

func TestDoctor_VerifiesObjectRefs(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	// Write a file so init picks it up and creates ops with ObjectRefs
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\ngo 1.21\n"), 0644))

	vault, err := core.InitVault(dir)
	require.NoError(t, err)
	require.NoError(t, vault.Close())

	out := runCmd(t, "--project", dir, "doctor")
	if !strings.Contains(out, "All checks passed") {
		t.Fatalf("expected 'All checks passed' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Object references:") {
		t.Fatalf("expected object reference check in output, got:\n%s", out)
	}
}
