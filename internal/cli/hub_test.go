package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/constructspace/loom/internal/core"
	"github.com/stretchr/testify/require"
)

func runHubCmd(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())
	return buf.String()
}

func TestHubStatus_ShowsRemote(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	v, err := core.InitVault(dir)
	require.NoError(t, err)
	require.NoError(t, v.Close())

	// Add a remote
	runHubCmd(t, "--project", dir, "hub", "add", "origin", "https://loomhub.dev/alice/project")

	out := runHubCmd(t, "--project", dir, "hub", "status")
	if !strings.Contains(out, "origin") {
		t.Fatalf("expected 'origin' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "https://loomhub.dev/alice/project") {
		t.Fatalf("expected URL in output, got:\n%s", out)
	}
}

func TestHubStatus_NoRemotes(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	v, err := core.InitVault(dir)
	require.NoError(t, err)
	require.NoError(t, v.Close())

	out := runHubCmd(t, "--project", dir, "hub", "status")
	if !strings.Contains(out, "No remotes configured") {
		t.Fatalf("expected 'No remotes configured' in output, got:\n%s", out)
	}
}

func TestHubStatus_SpecificRemote(t *testing.T) {
	dir := t.TempDir()
	projectDir = dir
	t.Cleanup(func() { projectDir = "." })

	v, err := core.InitVault(dir)
	require.NoError(t, err)
	require.NoError(t, v.Close())

	runHubCmd(t, "--project", dir, "hub", "add", "origin", "https://loomhub.dev/alice/project")
	runHubCmd(t, "--project", dir, "hub", "add", "backup", "https://loomhub.dev/alice/backup")

	out := runHubCmd(t, "--project", dir, "hub", "status", "origin")
	if !strings.Contains(out, "origin") {
		t.Fatalf("expected 'origin' in output, got:\n%s", out)
	}
	if strings.Contains(out, "backup") {
		t.Fatalf("did not expect 'backup' in output, got:\n%s", out)
	}
}
