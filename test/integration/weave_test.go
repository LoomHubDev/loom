package integration

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/constructspace/loom/internal/core"
)

func TestCLI_WeaveAppliesNonConflictingSourceChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), []byte("package main\n"))
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"))

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	mainStream, err := vault.Streams.GetByName("main")
	if err != nil {
		t.Fatalf("get main stream: %v", err)
	}

	featureStream, err := vault.Streams.Fork("main", "feature")
	if err != nil {
		t.Fatalf("fork feature: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: mainStream.ID,
		SpaceID:  "code",
		EntityID: "main.go",
		Type:     core.OpModify,
		Path:     "main.go",
		Author:   "test",
	}); err != nil {
		t.Fatalf("write main op: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: featureStream.ID,
		SpaceID:  "code",
		EntityID: "feature.txt",
		Type:     core.OpCreate,
		Path:     "feature.txt",
		Author:   "test",
	}); err != nil {
		t.Fatalf("write feature op: %v", err)
	}

	if err := vault.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	out := runCLI(t, "-p", dir, "weave", "feature")
	if !strings.Contains(out, "Weave complete") {
		t.Fatalf("expected weave completion output, got:\n%s", out)
	}

	reopened, err := core.OpenVault(dir)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	defer reopened.Close()

	mainStream, err = reopened.Streams.GetByName("main")
	if err != nil {
		t.Fatalf("reload main stream: %v", err)
	}

	ops, err := reopened.OpReader.ReadByStream(mainStream.ID, 0, mainStream.HeadSeq)
	if err != nil {
		t.Fatalf("read main stream ops: %v", err)
	}
	if countEntityOps(ops, "feature.txt") == 0 {
		t.Fatalf("expected woven feature.txt operation in main stream")
	}

	featureStream, err = reopened.Streams.GetByName("feature")
	if err != nil {
		t.Fatalf("reload feature stream: %v", err)
	}
	if featureStream.Status != "woven" {
		t.Fatalf("expected feature stream to be marked woven, got %q", featureStream.Status)
	}

	checkpoints, err := reopened.Checkpoints.List(mainStream.ID, 10)
	if err != nil {
		t.Fatalf("list checkpoints: %v", err)
	}
	if !hasCheckpointTitle(checkpoints, "Weave feature into main") {
		t.Fatalf("expected weave checkpoint, got %#v", checkpoints)
	}
}

func TestCLI_WeaveDetectsConflictingEntityChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), []byte("package main\n"))
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"))

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	mainStream, err := vault.Streams.GetByName("main")
	if err != nil {
		t.Fatalf("get main stream: %v", err)
	}

	featureStream, err := vault.Streams.Fork("main", "feature")
	if err != nil {
		t.Fatalf("fork feature: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: mainStream.ID,
		SpaceID:  "code",
		EntityID: "shared.txt",
		Type:     core.OpCreate,
		Path:     "shared.txt",
		Author:   "test",
	}); err != nil {
		t.Fatalf("write main op: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: featureStream.ID,
		SpaceID:  "code",
		EntityID: "shared.txt",
		Type:     core.OpCreate,
		Path:     "shared.txt",
		Author:   "test",
	}); err != nil {
		t.Fatalf("write feature op: %v", err)
	}

	if err := vault.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	_, err = executeCLI(dir, "weave", "feature")
	if err == nil {
		t.Fatalf("expected weave conflict")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if !strings.Contains(err.Error(), "shared.txt") {
		t.Fatalf("expected conflicting entity in error, got %v", err)
	}

	reopened, err := core.OpenVault(dir)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	defer reopened.Close()

	mainStream, err = reopened.Streams.GetByName("main")
	if err != nil {
		t.Fatalf("reload main stream: %v", err)
	}

	ops, err := reopened.OpReader.ReadByStream(mainStream.ID, 0, mainStream.HeadSeq)
	if err != nil {
		t.Fatalf("read main stream ops: %v", err)
	}
	if countEntityOps(ops, "shared.txt") != 1 {
		t.Fatalf("expected conflict to leave only target-side shared.txt op in main stream")
	}

	featureStream, err = reopened.Streams.GetByName("feature")
	if err != nil {
		t.Fatalf("reload feature stream: %v", err)
	}
	if featureStream.Status != "active" {
		t.Fatalf("expected conflicting feature stream to remain active, got %q", featureStream.Status)
	}
}

func TestCLI_WeaveTheirsStrategyAppliesConflictingSourceChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), []byte("package main\n"))
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"))

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	mainStream, err := vault.Streams.GetByName("main")
	if err != nil {
		t.Fatalf("get main stream: %v", err)
	}

	featureStream, err := vault.Streams.Fork("main", "feature")
	if err != nil {
		t.Fatalf("fork feature: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: mainStream.ID,
		SpaceID:  "code",
		EntityID: "shared.txt",
		Type:     core.OpCreate,
		Path:     "shared.txt",
		Author:   "test",
	}); err != nil {
		t.Fatalf("write main op: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: featureStream.ID,
		SpaceID:  "code",
		EntityID: "shared.txt",
		Type:     core.OpModify,
		Path:     "shared.txt",
		Author:   "test",
	}); err != nil {
		t.Fatalf("write feature op: %v", err)
	}

	if err := vault.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	out := runCLI(t, "-p", dir, "weave", "feature", "--strategy", "theirs")
	if !strings.Contains(out, "Weave complete") {
		t.Fatalf("expected weave completion output, got:\n%s", out)
	}

	reopened, err := core.OpenVault(dir)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	defer reopened.Close()

	mainStream, err = reopened.Streams.GetByName("main")
	if err != nil {
		t.Fatalf("reload main stream: %v", err)
	}

	ops, err := reopened.OpReader.ReadByStream(mainStream.ID, 0, mainStream.HeadSeq)
	if err != nil {
		t.Fatalf("read main stream ops: %v", err)
	}
	if countEntityOps(ops, "shared.txt") < 2 {
		t.Fatalf("expected theirs strategy to append source-side shared.txt change to main stream")
	}
}

func TestCLI_WeaveDryRunDoesNotMutateStreams(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), []byte("package main\n"))
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"))

	vault, err := core.InitVault(dir)
	if err != nil {
		t.Fatalf("init vault: %v", err)
	}

	mainStream, err := vault.Streams.GetByName("main")
	if err != nil {
		t.Fatalf("get main stream: %v", err)
	}

	featureStream, err := vault.Streams.Fork("main", "feature")
	if err != nil {
		t.Fatalf("fork feature: %v", err)
	}

	if _, err := vault.OpWriter.Write(core.Operation{
		StreamID: featureStream.ID,
		SpaceID:  "code",
		EntityID: "feature.txt",
		Type:     core.OpCreate,
		Path:     "feature.txt",
		Author:   "test",
	}); err != nil {
		t.Fatalf("write feature op: %v", err)
	}

	if err := vault.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	out := runCLI(t, "-p", dir, "weave", "feature", "--dry-run")
	if !strings.Contains(out, "Dry run") {
		t.Fatalf("expected dry-run output, got:\n%s", out)
	}

	reopened, err := core.OpenVault(dir)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	defer reopened.Close()

	mainStream, err = reopened.Streams.GetByName("main")
	if err != nil {
		t.Fatalf("reload main stream: %v", err)
	}

	ops, err := reopened.OpReader.ReadByStream(mainStream.ID, 0, mainStream.HeadSeq)
	if err != nil {
		t.Fatalf("read main stream ops: %v", err)
	}
	if countEntityOps(ops, "feature.txt") != 0 {
		t.Fatalf("expected dry-run to avoid mutating main stream")
	}

	featureStream, err = reopened.Streams.GetByName("feature")
	if err != nil {
		t.Fatalf("reload feature stream: %v", err)
	}
	if featureStream.Status != "active" {
		t.Fatalf("expected dry-run to leave feature active, got %q", featureStream.Status)
	}

	checkpoints, err := reopened.Checkpoints.List(mainStream.ID, 10)
	if err != nil {
		t.Fatalf("list checkpoints: %v", err)
	}
	if hasCheckpointTitle(checkpoints, "Weave feature into main") {
		t.Fatalf("expected dry-run to avoid weave checkpoint")
	}
}

func countEntityOps(ops []core.Operation, entityID string) int {
	count := 0
	for _, op := range ops {
		if op.EntityID == entityID {
			count++
		}
	}
	return count
}

func hasCheckpointTitle(checkpoints []core.Checkpoint, title string) bool {
	for _, cp := range checkpoints {
		if cp.Title == title {
			return true
		}
	}
	return false
}
