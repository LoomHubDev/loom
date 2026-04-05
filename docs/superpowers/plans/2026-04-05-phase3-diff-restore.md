# Phase 3: Diff + Restore — Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Compare any two points in history and restore to any checkpoint, with guard checkpoints for safety.

**Architecture:** A DiffEngine resolves refs (HEAD, HEAD~N, checkpoint IDs) to sequence numbers, reads ops between them, groups by space/entity, and delegates to text/structured/binary diffing. A RestoreEngine reads entity content at a checkpoint from the ObjectStore and writes it back to disk, creating guard/restore checkpoints automatically.

**Tech Stack:** go-diff (Myers algorithm), existing SQLite oplog + ObjectStore, cobra CLI

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/diff/types.go` | DiffResult, SpaceDiff, EntityDiff, DiffHunk, DiffLine, DiffRef types |
| `internal/diff/ref.go` | ParseRef, resolveRef — convert "HEAD", "HEAD~3", checkpoint IDs to seq numbers |
| `internal/diff/text.go` | Myers text diff producing hunks |
| `internal/diff/engine.go` | DiffEngine orchestration — resolve refs, read ops, group, diff entities |
| `internal/diff/format.go` | TerminalFormatter (colored) and JSONFormatter |
| `internal/diff/diff_test.go` | Tests for all diff components |
| `internal/core/restore.go` | RestoreEngine — restore files from checkpoint with guard/restore checkpoints |
| `internal/core/restore_test.go` | Tests for restore |
| `internal/cli/diff.go` | `loom diff` command |
| `internal/cli/show.go` | `loom show` command |
| `internal/cli/restore.go` | `loom restore` command |
| `test/integration/diff_test.go` | End-to-end diff + restore integration tests |

---

### Task 1: Diff Types + Ref Resolution

**Files:**
- Create: `internal/diff/types.go`
- Create: `internal/diff/ref.go`
- Create: `internal/diff/ref_test.go`

### Task 2: Text Diff (Myers Algorithm)

**Files:**
- Create: `internal/diff/text.go`
- Create: `internal/diff/text_test.go`

### Task 3: Diff Engine + Formatters

**Files:**
- Create: `internal/diff/engine.go`
- Create: `internal/diff/format.go`
- Create: `internal/diff/engine_test.go`

### Task 4: Restore Engine

**Files:**
- Create: `internal/core/restore.go`
- Create: `internal/core/restore_test.go`

### Task 5: CLI Commands (diff, show, restore)

**Files:**
- Create: `internal/cli/diff.go`
- Create: `internal/cli/show.go`
- Create: `internal/cli/restore.go`
- Modify: `internal/cli/root.go`

### Task 6: Integration Tests

**Files:**
- Create: `test/integration/diff_test.go`
