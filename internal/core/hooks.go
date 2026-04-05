package core

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// HookEvent is the name of a lifecycle hook.
type HookEvent string

const (
	HookPreCheckpoint  HookEvent = "pre-checkpoint"
	HookPostCheckpoint HookEvent = "post-checkpoint"
	HookPreRestore     HookEvent = "pre-restore"
	HookPostRestore    HookEvent = "post-restore"
	HookPrePush        HookEvent = "pre-push"
	HookPostPush       HookEvent = "post-push"
	HookPrePull        HookEvent = "pre-pull"
	HookPostPull       HookEvent = "post-pull"
)

// HookRunner executes lifecycle hook scripts found in the .loom/hooks/ directory.
type HookRunner struct {
	hooksDir    string
	projectPath string
}

// NewHookRunner returns a HookRunner that looks for scripts in loomPath/hooks/.
func NewHookRunner(loomPath string) *HookRunner {
	return &HookRunner{
		hooksDir:    filepath.Join(loomPath, "hooks"),
		projectPath: filepath.Dir(loomPath),
	}
}

// Run executes the hook script for the given event, if it exists and is executable.
// env is merged with the process environment; LOOM_EVENT and LOOM_PROJECT are
// always set automatically.
// Returns nil when no hook file exists (not an error).
// Returns an error (blocking the caller) when the script exits non-zero.
func (r *HookRunner) Run(event HookEvent, env map[string]string) error {
	script := filepath.Join(r.hooksDir, string(event))

	info, err := os.Stat(script)
	if err != nil {
		// File doesn't exist — not an error.
		return nil
	}

	// Check executable bit (owner execute).
	if info.Mode()&0111 == 0 {
		// Not executable — skip silently.
		return nil
	}

	cmd := exec.Command(script)

	// Build environment: inherit + automatic vars + caller-supplied vars.
	environment := os.Environ()
	environment = append(environment,
		"LOOM_EVENT="+string(event),
		"LOOM_PROJECT="+r.projectPath,
	)
	for k, v := range env {
		environment = append(environment, k+"="+v)
	}
	cmd.Env = environment
	cmd.Dir = r.projectPath

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("hook %s failed: %s", event, msg)
	}

	return nil
}
