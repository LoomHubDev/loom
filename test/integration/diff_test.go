package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/constructspace/loom/internal/core"
)

func TestCLI_DiffShowsChanges(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644)

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	stream, err := vault.ActiveStream()
	if err != nil {
		vault.Close()
		t.Fatalf("active stream: %v", err)
	}

	_, err = vault.Checkpoints.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "v1",
		Author:   "test",
		Source:   core.SourceManual,
	})
	if err != nil {
		vault.Close()
		t.Fatalf("create checkpoint: %v", err)
	}

	newContent := []byte("package main\n\nfunc main() { println(\"hello\") }\n")
	hash, err := vault.Store.Write(newContent, "text/plain")
	if err != nil {
		vault.Close()
		t.Fatalf("store write: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "main.go"), newContent, 0644)
	_, err = vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      core.OpModify,
		Path:      "main.go",
		ObjectRef: hash,
		Author:    "test",
	})
	if err != nil {
		vault.Close()
		t.Fatalf("write op: %v", err)
	}

	// Release lock before CLI opens the vault
	vault.Close()

	out := runCLI(t, "-p", dir, "diff")
	if !strings.Contains(out, "main.go") {
		t.Errorf("diff output missing 'main.go': %s", out)
	}
}

func TestCLI_DiffBetweenCheckpoints(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644)

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	stream, err := vault.ActiveStream()
	if err != nil {
		vault.Close()
		t.Fatalf("active stream: %v", err)
	}

	cp1, err := vault.Checkpoints.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "v1",
		Author:   "test",
		Source:   core.SourceManual,
	})
	if err != nil {
		vault.Close()
		t.Fatalf("create checkpoint v1: %v", err)
	}

	newContent := []byte("package main\n\nfunc main() { println(\"hello\") }\n")
	hash, err := vault.Store.Write(newContent, "text/plain")
	if err != nil {
		vault.Close()
		t.Fatalf("store write: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "main.go"), newContent, 0644)
	_, err = vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      core.OpModify,
		Path:      "main.go",
		ObjectRef: hash,
		Author:    "test",
	})
	if err != nil {
		vault.Close()
		t.Fatalf("write op: %v", err)
	}

	cp2, err := vault.Checkpoints.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "v2",
		Author:   "test",
		Source:   core.SourceManual,
	})
	if err != nil {
		vault.Close()
		t.Fatalf("create checkpoint v2: %v", err)
	}

	// Release lock before CLI opens the vault
	vault.Close()

	out := runCLI(t, "-p", dir, "diff", cp1.ID, cp2.ID)
	if !strings.Contains(out, "main.go") {
		t.Errorf("diff output missing 'main.go': %s", out)
	}
}

func TestCLI_DiffJSON(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644)

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	stream, err := vault.ActiveStream()
	if err != nil {
		vault.Close()
		t.Fatalf("active stream: %v", err)
	}

	_, err = vault.Checkpoints.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "v1",
		Author:   "test",
		Source:   core.SourceManual,
	})
	if err != nil {
		vault.Close()
		t.Fatalf("create checkpoint: %v", err)
	}

	newContent := []byte("package main\n\nfunc main() { println(\"hello\") }\n")
	hash, err := vault.Store.Write(newContent, "text/plain")
	if err != nil {
		vault.Close()
		t.Fatalf("store write: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "main.go"), newContent, 0644)
	_, err = vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      core.OpModify,
		Path:      "main.go",
		ObjectRef: hash,
		Author:    "test",
	})
	if err != nil {
		vault.Close()
		t.Fatalf("write op: %v", err)
	}

	// Release lock before CLI opens the vault
	vault.Close()

	out := runCLI(t, "-p", dir, "diff", "--format", "json")
	var result interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Errorf("diff --format json output is not valid JSON: %v\nOutput: %s", err, out)
	}
}

func TestCLI_ShowCheckpoint(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	stream, err := vault.ActiveStream()
	if err != nil {
		vault.Close()
		t.Fatalf("active stream: %v", err)
	}

	cp, err := vault.Checkpoints.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "auth complete",
		Author:   "testuser",
		Source:   core.SourceManual,
	})
	if err != nil {
		vault.Close()
		t.Fatalf("create checkpoint: %v", err)
	}

	// Release lock before CLI opens the vault
	vault.Close()

	out := runCLI(t, "-p", dir, "show", cp.ID)

	if !strings.Contains(out, "auth complete") {
		t.Errorf("show output missing title 'auth complete': %s", out)
	}
	if !strings.Contains(out, cp.ID) {
		t.Errorf("show output missing checkpoint ID %s: %s", cp.ID, out)
	}
	if !strings.Contains(out, "testuser") {
		t.Errorf("show output missing author 'testuser': %s", out)
	}
}

func TestCLI_RestoreFullRoundtrip(t *testing.T) {
	dir := t.TempDir()

	originalContent := []byte("original content\n")
	os.WriteFile(filepath.Join(dir, "main.go"), originalContent, 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644)

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}
	defer vault.Close()

	stream, err := vault.ActiveStream()
	if err != nil {
		t.Fatalf("active stream: %v", err)
	}

	// Store original content and write op so restore can find it
	origHash, err := vault.Store.Write(originalContent, "text/plain")
	if err != nil {
		t.Fatalf("store original content: %v", err)
	}
	_, err = vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      core.OpCreate,
		Path:      "main.go",
		ObjectRef: origHash,
		Author:    "test",
	})
	if err != nil {
		t.Fatalf("write create op: %v", err)
	}

	cp1, err := vault.Checkpoints.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "v1",
		Author:   "test",
		Source:   core.SourceManual,
	})
	if err != nil {
		t.Fatalf("create checkpoint v1: %v", err)
	}

	// Modify main.go on disk and write op
	modifiedContent := []byte("modified content\n")
	newHash, err := vault.Store.Write(modifiedContent, "text/plain")
	if err != nil {
		t.Fatalf("store modified content: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "main.go"), modifiedContent, 0644)
	_, err = vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      core.OpModify,
		Path:      "main.go",
		ObjectRef: newHash,
		Author:    "test",
	})
	if err != nil {
		t.Fatalf("write modify op: %v", err)
	}

	// Must close vault before CLI opens it
	vault.Close()

	out := runCLI(t, "-p", dir, "restore", cp1.ID)

	diskContent, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go after restore: %v", err)
	}
	if string(diskContent) != string(originalContent) {
		t.Errorf("expected 'original content', got: %s", string(diskContent))
	}
	if !strings.Contains(strings.ToLower(out), "restored") && !strings.Contains(strings.ToLower(out), "guard") {
		t.Errorf("restore output missing 'Restored' or 'Guard': %s", out)
	}
}

func TestCLI_RestoreByEntity(t *testing.T) {
	dir := t.TempDir()

	mainOriginal := []byte("original main\n")
	readmeOriginal := []byte("original readme\n")

	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), mainOriginal, 0644)
	os.WriteFile(filepath.Join(dir, "docs", "readme.md"), readmeOriginal, 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644)

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}
	defer vault.Close()

	stream, err := vault.ActiveStream()
	if err != nil {
		t.Fatalf("active stream: %v", err)
	}

	// Store original content and write ops
	mainHash, err := vault.Store.Write(mainOriginal, "text/plain")
	if err != nil {
		t.Fatalf("store main.go: %v", err)
	}
	readmeHash, err := vault.Store.Write(readmeOriginal, "text/plain")
	if err != nil {
		t.Fatalf("store readme: %v", err)
	}

	_, err = vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      core.OpCreate,
		Path:      "main.go",
		ObjectRef: mainHash,
		Author:    "test",
	})
	if err != nil {
		t.Fatalf("write create op main.go: %v", err)
	}
	_, err = vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "docs",
		EntityID:  "readme.md",
		Type:      core.OpCreate,
		Path:      "readme.md",
		ObjectRef: readmeHash,
		Author:    "test",
	})
	if err != nil {
		t.Fatalf("write create op readme: %v", err)
	}

	cp1, err := vault.Checkpoints.Create(core.CheckpointInput{
		StreamID: stream.ID,
		Title:    "v1",
		Author:   "test",
		Source:   core.SourceManual,
	})
	if err != nil {
		t.Fatalf("create checkpoint v1: %v", err)
	}

	// Modify both files
	mainModified := []byte("modified main\n")
	readmeModified := []byte("modified readme\n")

	mainModHash, err := vault.Store.Write(mainModified, "text/plain")
	if err != nil {
		t.Fatalf("store modified main.go: %v", err)
	}
	readmeModHash, err := vault.Store.Write(readmeModified, "text/plain")
	if err != nil {
		t.Fatalf("store modified readme: %v", err)
	}

	os.WriteFile(filepath.Join(dir, "main.go"), mainModified, 0644)
	os.WriteFile(filepath.Join(dir, "docs", "readme.md"), readmeModified, 0644)

	_, err = vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      core.OpModify,
		Path:      "main.go",
		ObjectRef: mainModHash,
		Author:    "test",
	})
	if err != nil {
		t.Fatalf("write modify op main.go: %v", err)
	}
	_, err = vault.OpWriter.Write(core.Operation{
		StreamID:  stream.ID,
		SpaceID:   "docs",
		EntityID:  "readme.md",
		Type:      core.OpModify,
		Path:      "readme.md",
		ObjectRef: readmeModHash,
		Author:    "test",
	})
	if err != nil {
		t.Fatalf("write modify op readme: %v", err)
	}

	// Must close vault before CLI opens it
	vault.Close()

	runCLI(t, "-p", dir, "restore", cp1.ID, "--entity", "main.go")

	// main.go should be restored to original
	mainDisk, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if string(mainDisk) != string(mainOriginal) {
		t.Errorf("main.go: expected %q, got %q", string(mainOriginal), string(mainDisk))
	}

	// docs/readme.md should still have modified content
	readmeDisk, err := os.ReadFile(filepath.Join(dir, "docs", "readme.md"))
	if err != nil {
		t.Fatalf("read readme.md: %v", err)
	}
	if string(readmeDisk) != string(readmeModified) {
		t.Errorf("readme.md: expected %q (unmodified), got %q", string(readmeModified), string(readmeDisk))
	}
}
