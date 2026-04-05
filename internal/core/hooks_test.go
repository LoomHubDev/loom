package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeHookRunner(t *testing.T) (*HookRunner, string) {
	t.Helper()
	tmp := t.TempDir()
	loomPath := filepath.Join(tmp, ".loom")
	hooksDir := filepath.Join(loomPath, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	return NewHookRunner(loomPath), hooksDir
}

func TestHookRunner_NoHookFile(t *testing.T) {
	runner, _ := makeHookRunner(t)
	if err := runner.Run(HookPreCheckpoint, nil); err != nil {
		t.Fatalf("expected nil when no hook file, got: %v", err)
	}
}

func TestHookRunner_SuccessfulHook(t *testing.T) {
	runner, hooksDir := makeHookRunner(t)

	script := filepath.Join(hooksDir, string(HookPreCheckpoint))
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := runner.Run(HookPreCheckpoint, nil); err != nil {
		t.Fatalf("expected nil for successful hook, got: %v", err)
	}
}

func TestHookRunner_FailedHook(t *testing.T) {
	runner, hooksDir := makeHookRunner(t)

	script := filepath.Join(hooksDir, string(HookPreCheckpoint))
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'something went wrong' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	err := runner.Run(HookPreCheckpoint, nil)
	if err == nil {
		t.Fatal("expected error for failing hook, got nil")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Fatalf("expected stderr in error, got: %v", err)
	}
}

func TestHookRunner_EnvironmentVars(t *testing.T) {
	runner, hooksDir := makeHookRunner(t)

	outFile := filepath.Join(hooksDir, "env_out.txt")
	script := filepath.Join(hooksDir, string(HookPostCheckpoint))
	content := "#!/bin/sh\necho \"$LOOM_EVENT\" > " + outFile + "\n"
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}

	if err := runner.Run(HookPostCheckpoint, map[string]string{"LOOM_CHECKPOINT_ID": "abc123"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("expected output file to be written: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if got != string(HookPostCheckpoint) {
		t.Fatalf("expected LOOM_EVENT=%q, got %q", string(HookPostCheckpoint), got)
	}
}

func TestHookRunner_NonExecutableIgnored(t *testing.T) {
	runner, hooksDir := makeHookRunner(t)

	script := filepath.Join(hooksDir, string(HookPreRestore))
	// Write without execute permission.
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runner.Run(HookPreRestore, nil); err != nil {
		t.Fatalf("expected nil for non-executable hook, got: %v", err)
	}
}
