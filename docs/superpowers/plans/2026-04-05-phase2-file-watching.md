# Phase 2: File Watching + Auto-Versioning — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Continuous auto-versioning — file changes are captured as operations automatically, with auto-checkpointing when thresholds are met.

**Architecture:** A file watcher (fsnotify) detects changes, a debouncer coalesces rapid edits (500ms), a filter applies ignore rules, and an adapter normalizes events into operations written to the oplog. An auto-checkpoint goroutine monitors thresholds and creates checkpoints. The `loom watch` CLI command orchestrates everything.

**Tech Stack:** fsnotify (file watching), existing SQLite oplog + ObjectStore, cobra CLI

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/watch/filter.go` | Ignore-rule matching (glob patterns from config + .loomignore) |
| `internal/watch/filter_test.go` | Tests for filter |
| `internal/watch/debounce.go` | Coalesces rapid per-file events into single events |
| `internal/watch/debounce_test.go` | Tests for debouncer |
| `internal/watch/autocheckpoint.go` | Monitors op count/time thresholds, creates auto-checkpoints |
| `internal/watch/autocheckpoint_test.go` | Tests for auto-checkpoint |
| `internal/watch/watcher.go` | Orchestrates fsnotify → filter → debounce → normalize → write |
| `internal/watch/watcher_test.go` | Integration tests for watcher |
| `internal/cli/watch.go` | `loom watch` command |
| `test/integration/watch_test.go` | End-to-end integration test |

---

### Task 1: Filter — Ignore Rule Matching

**Files:**
- Create: `internal/watch/filter.go`
- Create: `internal/watch/filter_test.go`

The filter determines whether a file event should be ignored. It checks glob patterns from `Config.Watch.Ignore` and an optional `.loomignore` file.

- [ ] **Step 1: Write the failing tests**

```go
// internal/watch/filter_test.go
package watch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilter_IgnoresConfiguredPatterns(t *testing.T) {
	f := NewFilter([]string{".git", "node_modules", "*.tmp", "*.swp"}, "")

	assert.True(t, f.ShouldIgnore(".git"))
	assert.True(t, f.ShouldIgnore("node_modules"))
	assert.True(t, f.ShouldIgnore("temp.tmp"))
	assert.True(t, f.ShouldIgnore("file.swp"))
	assert.False(t, f.ShouldIgnore("main.go"))
	assert.False(t, f.ShouldIgnore("docs/readme.md"))
}

func TestFilter_IgnoresNestedPaths(t *testing.T) {
	f := NewFilter([]string{".git", "node_modules"}, "")

	assert.True(t, f.ShouldIgnore(".git/config"))
	assert.True(t, f.ShouldIgnore(".git/objects/ab/cd1234"))
	assert.True(t, f.ShouldIgnore("node_modules/express/index.js"))
	assert.False(t, f.ShouldIgnore("src/main.go"))
}

func TestFilter_LoomignoreFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".loomignore"), []byte("build/\n*.log\n# comment\n\n"), 0644)

	f := NewFilter([]string{".git"}, dir)

	assert.True(t, f.ShouldIgnore("build/output.js"))
	assert.True(t, f.ShouldIgnore("server.log"))
	assert.True(t, f.ShouldIgnore(".git"))
	assert.False(t, f.ShouldIgnore("main.go"))
}

func TestFilter_NoLoomignoreFile(t *testing.T) {
	dir := t.TempDir()
	f := NewFilter([]string{"*.tmp"}, dir)

	assert.True(t, f.ShouldIgnore("file.tmp"))
	assert.False(t, f.ShouldIgnore("main.go"))
}

func TestFilter_EmptyRules(t *testing.T) {
	f := NewFilter(nil, "")
	assert.False(t, f.ShouldIgnore("anything.go"))
}

func TestFilter_AlwaysIgnoresLoomDir(t *testing.T) {
	f := NewFilter(nil, "")
	assert.True(t, f.ShouldIgnore(".loom"))
	assert.True(t, f.ShouldIgnore(".loom/loom.db"))
}

func TestFilter_RouteToSpace(t *testing.T) {
	spaces := map[string]string{
		"docs":   "docs",
		"design": "design",
		"code":   ".",
	}
	f := NewFilter(nil, "")

	assert.Equal(t, "docs", f.RouteToSpace("docs/readme.md", spaces))
	assert.Equal(t, "design", f.RouteToSpace("design/mock.json", spaces))
	assert.Equal(t, "code", f.RouteToSpace("src/main.go", spaces))
	assert.Equal(t, "code", f.RouteToSpace("main.go", spaces))
}

func TestFilter_RouteToSpace_LongestMatchWins(t *testing.T) {
	spaces := map[string]string{
		"docs": "docs",
		"code": ".",
	}
	f := NewFilter(nil, "")

	// docs/readme.md should match "docs" not "." (code)
	assert.Equal(t, "docs", f.RouteToSpace("docs/readme.md", spaces))
}

func TestFilter_RouteToSpace_NoMatch(t *testing.T) {
	f := NewFilter(nil, "")
	assert.Equal(t, "", f.RouteToSpace("file.go", nil))
	assert.Equal(t, "", f.RouteToSpace("file.go", map[string]string{}))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./internal/watch/... -v -count=1`
Expected: Compilation error — package `watch` doesn't exist yet

- [ ] **Step 3: Implement Filter**

```go
// internal/watch/filter.go
package watch

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Filter determines whether file events should be ignored.
type Filter struct {
	rules []string
}

// NewFilter creates a filter from config ignore rules and an optional .loomignore file.
// projectPath can be empty if no .loomignore should be loaded.
func NewFilter(configRules []string, projectPath string) *Filter {
	rules := make([]string, 0, len(configRules)+10)
	rules = append(rules, ".loom") // Always ignore .loom directory
	rules = append(rules, configRules...)

	if projectPath != "" {
		if fileRules, err := readLoomignore(filepath.Join(projectPath, ".loomignore")); err == nil {
			rules = append(rules, fileRules...)
		}
	}

	return &Filter{rules: rules}
}

// ShouldIgnore returns true if the given relative path should be ignored.
func (f *Filter) ShouldIgnore(relPath string) bool {
	// Check each path component and the filename against rules
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for _, rule := range f.rules {
		// Check against the full path
		if matched, _ := filepath.Match(rule, relPath); matched {
			return true
		}
		// Check each path component (for directory rules like ".git", "node_modules")
		for _, part := range parts {
			if matched, _ := filepath.Match(rule, part); matched {
				return true
			}
		}
	}
	return false
}

// RouteToSpace determines which space a file belongs to based on its relative path.
// spaces maps spaceID -> space path (e.g. "docs" -> "docs", "code" -> ".").
// Returns empty string if no space matches.
func (f *Filter) RouteToSpace(relPath string, spaces map[string]string) string {
	if len(spaces) == 0 {
		return ""
	}

	bestMatch := ""
	bestLen := -1

	for spaceID, spacePath := range spaces {
		if spacePath == "." {
			// Root space — matches everything, but is lowest priority
			if bestLen < 0 {
				bestMatch = spaceID
				bestLen = 0
			}
			continue
		}
		// Check if relPath starts with spacePath
		prefix := filepath.ToSlash(spacePath) + "/"
		if strings.HasPrefix(filepath.ToSlash(relPath), prefix) || filepath.ToSlash(relPath) == filepath.ToSlash(spacePath) {
			if len(spacePath) > bestLen {
				bestMatch = spaceID
				bestLen = len(spacePath)
			}
		}
	}

	return bestMatch
}

func readLoomignore(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rules []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip trailing slash for directory patterns
		line = strings.TrimSuffix(line, "/")
		rules = append(rules, line)
	}
	return rules, scanner.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./internal/watch/... -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/flakerim/LoomProject/loom
git add internal/watch/filter.go internal/watch/filter_test.go
git commit -m "feat(watch): add file event filter with ignore rules and space routing"
```

---

### Task 2: Debouncer — Coalesce Rapid Events

**Files:**
- Create: `internal/watch/debounce.go`
- Create: `internal/watch/debounce_test.go`

The debouncer collects events per-file and emits one consolidated event per file after a quiet period.

- [ ] **Step 1: Write the failing tests**

```go
// internal/watch/debounce_test.go
package watch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebouncer_CoalescesRapidEvents(t *testing.T) {
	window := 50 * time.Millisecond
	d := NewDebouncer(window)
	out := d.Start()

	// Send 5 rapid events for the same file
	for i := 0; i < 5; i++ {
		d.Send(FileEvent{Path: "main.go", Op: OpModify})
	}

	// Wait for debounce window
	select {
	case evt := <-out:
		assert.Equal(t, "main.go", evt.Path)
		assert.Equal(t, OpModify, evt.Op)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected debounced event")
	}

	// No second event
	select {
	case evt := <-out:
		t.Fatalf("unexpected extra event: %v", evt)
	case <-time.After(100 * time.Millisecond):
		// Good — no extra events
	}

	d.Stop()
}

func TestDebouncer_DifferentFilesEmitSeparately(t *testing.T) {
	window := 50 * time.Millisecond
	d := NewDebouncer(window)
	out := d.Start()

	d.Send(FileEvent{Path: "a.go", Op: OpModify})
	d.Send(FileEvent{Path: "b.go", Op: OpModify})

	received := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case evt := <-out:
			received[evt.Path] = true
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("expected 2 events, got %d", i)
		}
	}

	assert.True(t, received["a.go"])
	assert.True(t, received["b.go"])

	d.Stop()
}

func TestDebouncer_CreateThenModifyBecomesCreate(t *testing.T) {
	window := 50 * time.Millisecond
	d := NewDebouncer(window)
	out := d.Start()

	d.Send(FileEvent{Path: "new.go", Op: OpCreate})
	d.Send(FileEvent{Path: "new.go", Op: OpModify})

	select {
	case evt := <-out:
		assert.Equal(t, "new.go", evt.Path)
		assert.Equal(t, OpCreate, evt.Op, "create+modify should coalesce to create")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected debounced event")
	}

	d.Stop()
}

func TestDebouncer_CreateThenDeleteCancels(t *testing.T) {
	window := 50 * time.Millisecond
	d := NewDebouncer(window)
	out := d.Start()

	d.Send(FileEvent{Path: "temp.go", Op: OpCreate})
	d.Send(FileEvent{Path: "temp.go", Op: OpDelete})

	// Should produce no event (created then immediately deleted)
	select {
	case evt := <-out:
		t.Fatalf("expected no event for create+delete, got: %v", evt)
	case <-time.After(150 * time.Millisecond):
		// Good
	}

	d.Stop()
}

func TestDebouncer_DeleteEmits(t *testing.T) {
	window := 50 * time.Millisecond
	d := NewDebouncer(window)
	out := d.Start()

	d.Send(FileEvent{Path: "old.go", Op: OpDelete})

	select {
	case evt := <-out:
		assert.Equal(t, "old.go", evt.Path)
		assert.Equal(t, OpDelete, evt.Op)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected delete event")
	}

	d.Stop()
}

func TestDebouncer_StopDrainsCleanly(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)
	_ = d.Start()
	d.Stop()
	// Should not panic or hang
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./internal/watch/... -v -count=1 -run Debounce`
Expected: Compilation error — `NewDebouncer`, `FileEvent`, etc. not defined

- [ ] **Step 3: Implement Debouncer and FileEvent types**

```go
// internal/watch/debounce.go
package watch

import (
	"sync"
	"time"
)

// OpKind represents the type of file operation.
type OpKind string

const (
	OpCreate OpKind = "create"
	OpModify OpKind = "modify"
	OpDelete OpKind = "delete"
	OpRename OpKind = "rename"
)

// FileEvent represents a debounced file system event.
type FileEvent struct {
	Path    string
	Op      OpKind
	OldPath string // For rename events
}

type pendingEvent struct {
	event FileEvent
	timer *time.Timer
}

// Debouncer coalesces rapid file events per-path into single events.
type Debouncer struct {
	window  time.Duration
	mu      sync.Mutex
	pending map[string]*pendingEvent
	out     chan FileEvent
	in      chan FileEvent
	done    chan struct{}
}

// NewDebouncer creates a debouncer with the given quiet window.
func NewDebouncer(window time.Duration) *Debouncer {
	return &Debouncer{
		window:  window,
		pending: make(map[string]*pendingEvent),
		out:     make(chan FileEvent, 100),
		in:      make(chan FileEvent, 100),
		done:    make(chan struct{}),
	}
}

// Start begins processing events and returns the output channel.
func (d *Debouncer) Start() <-chan FileEvent {
	go d.run()
	return d.out
}

// Send submits a file event for debouncing.
func (d *Debouncer) Send(evt FileEvent) {
	select {
	case d.in <- evt:
	case <-d.done:
	}
}

// Stop shuts down the debouncer.
func (d *Debouncer) Stop() {
	close(d.done)
	d.mu.Lock()
	for _, p := range d.pending {
		p.timer.Stop()
	}
	d.pending = make(map[string]*pendingEvent)
	d.mu.Unlock()
}

func (d *Debouncer) run() {
	for {
		select {
		case evt := <-d.in:
			d.handleEvent(evt)
		case <-d.done:
			return
		}
	}
}

func (d *Debouncer) handleEvent(evt FileEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if p, exists := d.pending[evt.Path]; exists {
		p.timer.Stop()
		// Coalesce: merge the operation types
		p.event.Op = coalesceOp(p.event.Op, evt.Op)
		if p.event.Op == "" {
			// create+delete cancels out — remove pending
			delete(d.pending, evt.Path)
			return
		}
		p.timer = time.AfterFunc(d.window, func() { d.emit(evt.Path) })
	} else {
		pe := &pendingEvent{event: evt}
		pe.timer = time.AfterFunc(d.window, func() { d.emit(evt.Path) })
		d.pending[evt.Path] = pe
	}
}

func (d *Debouncer) emit(path string) {
	d.mu.Lock()
	p, exists := d.pending[path]
	if exists {
		delete(d.pending, path)
	}
	d.mu.Unlock()

	if exists {
		select {
		case d.out <- p.event:
		case <-d.done:
		}
	}
}

// coalesceOp merges two operations on the same file.
// Returns empty string if they cancel out.
func coalesceOp(prev, next OpKind) OpKind {
	switch {
	case prev == OpCreate && next == OpDelete:
		return "" // cancel
	case prev == OpCreate && next == OpModify:
		return OpCreate // still a create
	case prev == OpModify && next == OpDelete:
		return OpDelete
	case prev == OpDelete && next == OpCreate:
		return OpCreate // re-created
	default:
		return next
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./internal/watch/... -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/flakerim/LoomProject/loom
git add internal/watch/debounce.go internal/watch/debounce_test.go
git commit -m "feat(watch): add event debouncer with op coalescing"
```

---

### Task 3: AutoCheckpointer — Threshold-Based Checkpoints

**Files:**
- Create: `internal/watch/autocheckpoint.go`
- Create: `internal/watch/autocheckpoint_test.go`

Monitors operation count and time thresholds, creating checkpoints automatically.

- [ ] **Step 1: Write the failing tests**

```go
// internal/watch/autocheckpoint_test.go
package watch

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAutoCheckpointEnv(t *testing.T) (*core.CheckpointEngine, *core.OpWriter, *core.OpReader, *core.Stream) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.InitDB(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := storage.NewObjectStore(filepath.Join(dir, "objects"), db)
	require.NoError(t, err)

	sm := core.NewStreamManager(db)
	stream, err := sm.Create("main")
	require.NoError(t, err)
	sm.SetActive("main")

	writer := core.NewOpWriter(db, store)
	reader := core.NewOpReader(db)
	engine := core.NewCheckpointEngine(db, reader)

	return engine, writer, reader, stream
}

func TestAutoCheckpointer_TriggersOnOpCount(t *testing.T) {
	engine, writer, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     3,
		IntervalSeconds: 9999, // effectively disabled
	})

	// Write 3 ops
	for i := 0; i < 3; i++ {
		writer.Write(core.Operation{
			StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
			Type: core.OpCreate, Path: "f.go", Author: "test",
		})
	}

	created := ac.MaybeCheckpoint(stream.ID, "test")
	require.NotNil(t, created, "should create checkpoint after reaching op threshold")
	assert.Equal(t, core.SourceAuto, created.Source)
	assert.Contains(t, created.Title, "Auto")
}

func TestAutoCheckpointer_DoesNotTriggerBelowThreshold(t *testing.T) {
	engine, writer, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     5,
		IntervalSeconds: 9999,
	})

	// Write only 2 ops
	for i := 0; i < 2; i++ {
		writer.Write(core.Operation{
			StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
			Type: core.OpCreate, Path: "f.go", Author: "test",
		})
	}

	created := ac.MaybeCheckpoint(stream.ID, "test")
	assert.Nil(t, created, "should not create checkpoint below threshold")
}

func TestAutoCheckpointer_TriggersOnTimeInterval(t *testing.T) {
	engine, writer, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     9999,
		IntervalSeconds: 0, // always triggers on time
	})

	// Write at least 1 op
	writer.Write(core.Operation{
		StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
		Type: core.OpCreate, Path: "f.go", Author: "test",
	})

	// Set last checkpoint time to the past
	ac.lastCheckpoint = time.Now().Add(-1 * time.Hour)

	created := ac.MaybeCheckpoint(stream.ID, "test")
	require.NotNil(t, created)
}

func TestAutoCheckpointer_NoOpsNoCheckpoint(t *testing.T) {
	engine, _, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     1,
		IntervalSeconds: 0,
	})
	ac.lastCheckpoint = time.Now().Add(-1 * time.Hour)

	created := ac.MaybeCheckpoint(stream.ID, "test")
	assert.Nil(t, created, "should not create checkpoint when no ops pending")
}

func TestAutoCheckpointer_ResetsAfterCheckpoint(t *testing.T) {
	engine, writer, _, stream := setupAutoCheckpointEnv(t)

	ac := NewAutoCheckpointer(engine, AutoCheckpointConfig{
		IntervalOps:     2,
		IntervalSeconds: 9999,
	})

	// Write 2 ops, trigger checkpoint
	for i := 0; i < 2; i++ {
		writer.Write(core.Operation{
			StreamID: stream.ID, SpaceID: "code", EntityID: "f.go",
			Type: core.OpCreate, Path: "f.go", Author: "test",
		})
	}
	cp := ac.MaybeCheckpoint(stream.ID, "test")
	require.NotNil(t, cp)

	// 1 more op — should NOT trigger yet
	writer.Write(core.Operation{
		StreamID: stream.ID, SpaceID: "code", EntityID: "g.go",
		Type: core.OpCreate, Path: "g.go", Author: "test",
	})
	cp2 := ac.MaybeCheckpoint(stream.ID, "test")
	assert.Nil(t, cp2, "should not trigger again until threshold reached")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./internal/watch/... -v -count=1 -run AutoCheckpoint`
Expected: Compilation error — `NewAutoCheckpointer` not defined

- [ ] **Step 3: Implement AutoCheckpointer**

```go
// internal/watch/autocheckpoint.go
package watch

import (
	"fmt"
	"time"

	"github.com/constructspace/loom/internal/core"
)

// AutoCheckpointConfig holds thresholds for auto-checkpoint creation.
type AutoCheckpointConfig struct {
	IntervalOps     int // Create checkpoint after this many ops
	IntervalSeconds int // Create checkpoint after this many seconds
}

// AutoCheckpointer evaluates whether an auto-checkpoint should be created.
type AutoCheckpointer struct {
	engine         *core.CheckpointEngine
	config         AutoCheckpointConfig
	lastCheckpoint time.Time
}

// NewAutoCheckpointer creates a new auto-checkpoint evaluator.
func NewAutoCheckpointer(engine *core.CheckpointEngine, config AutoCheckpointConfig) *AutoCheckpointer {
	return &AutoCheckpointer{
		engine:         engine,
		config:         config,
		lastCheckpoint: time.Now(),
	}
}

// MaybeCheckpoint evaluates thresholds and creates a checkpoint if warranted.
// Returns the created checkpoint, or nil if no checkpoint was needed.
func (ac *AutoCheckpointer) MaybeCheckpoint(streamID, author string) *core.Checkpoint {
	lastSeq := ac.engine.LatestSeq(streamID)

	// Get stream head to count ops since last checkpoint
	var headSeq int64
	// We use LatestSeq as our reference — ops after that seq are pending
	pendingOps := ac.countPendingOps(streamID, lastSeq)

	if pendingOps == 0 {
		return nil
	}

	triggered := false
	reason := ""

	// Check op count threshold
	if ac.config.IntervalOps > 0 && pendingOps >= ac.config.IntervalOps {
		triggered = true
		reason = fmt.Sprintf("%d operations", pendingOps)
	}

	// Check time threshold
	if ac.config.IntervalSeconds >= 0 && !triggered {
		elapsed := time.Since(ac.lastCheckpoint)
		if elapsed >= time.Duration(ac.config.IntervalSeconds)*time.Second {
			triggered = true
			reason = fmt.Sprintf("%.0fs elapsed", elapsed.Seconds())
		}
	}

	if !triggered {
		return nil
	}

	_ = headSeq
	cp, err := ac.engine.Create(core.CheckpointInput{
		StreamID: streamID,
		Title:    fmt.Sprintf("Auto-checkpoint (%s)", reason),
		Author:   author,
		Source:   core.SourceAuto,
	})
	if err != nil {
		return nil
	}

	ac.lastCheckpoint = time.Now()
	return cp
}

func (ac *AutoCheckpointer) countPendingOps(streamID string, sinceSeq int64) int {
	// Use the checkpoint engine's underlying reader via LatestSeq
	// We need to count ops after sinceSeq for this stream
	// Access through the engine's exported method
	counts, err := ac.engine.CountOpsSince(streamID, sinceSeq)
	if err != nil {
		return 0
	}
	return counts
}
```

Wait — `CheckpointEngine` doesn't have `CountOpsSince`. We need to add a helper or use what's available. The engine has access to the OpReader. Let me adjust — we'll use a simpler approach: pass the OpReader to AutoCheckpointer.

**Revised implementation:**

```go
// internal/watch/autocheckpoint.go
package watch

import (
	"fmt"
	"time"

	"github.com/constructspace/loom/internal/core"
)

// AutoCheckpointConfig holds thresholds for auto-checkpoint creation.
type AutoCheckpointConfig struct {
	IntervalOps     int
	IntervalSeconds int
}

// AutoCheckpointer evaluates whether an auto-checkpoint should be created.
type AutoCheckpointer struct {
	engine         *core.CheckpointEngine
	reader         *core.OpReader
	config         AutoCheckpointConfig
	lastCheckpoint time.Time
}

// NewAutoCheckpointer creates a new auto-checkpoint evaluator.
func NewAutoCheckpointer(engine *core.CheckpointEngine, config AutoCheckpointConfig) *AutoCheckpointer {
	return &AutoCheckpointer{
		engine:         engine,
		config:         config,
		lastCheckpoint: time.Now(),
	}
}

// MaybeCheckpoint evaluates thresholds and creates a checkpoint if warranted.
// Returns the created checkpoint, or nil if no checkpoint was needed.
func (ac *AutoCheckpointer) MaybeCheckpoint(streamID, author string) *core.Checkpoint {
	lastCPSeq := ac.engine.LatestSeq(streamID)

	// Count pending ops by checking space op counts since last checkpoint
	spaceCounts, err := ac.engine.CountSinceCheckpoint(streamID)
	if err != nil {
		return nil
	}

	pendingOps := 0
	for _, c := range spaceCounts {
		pendingOps += c.Created + c.Modified + c.Deleted
	}

	if pendingOps == 0 {
		return nil
	}

	triggered := false
	reason := ""

	if ac.config.IntervalOps > 0 && pendingOps >= ac.config.IntervalOps {
		triggered = true
		reason = fmt.Sprintf("%d operations", pendingOps)
	}

	if ac.config.IntervalSeconds >= 0 && !triggered {
		elapsed := time.Since(ac.lastCheckpoint)
		if elapsed >= time.Duration(ac.config.IntervalSeconds)*time.Second {
			triggered = true
			reason = fmt.Sprintf("%.0fs elapsed", elapsed.Seconds())
		}
	}

	if !triggered {
		return nil
	}

	_ = lastCPSeq
	cp, err := ac.engine.Create(core.CheckpointInput{
		StreamID: streamID,
		Title:    fmt.Sprintf("Auto-checkpoint (%s)", reason),
		Author:   author,
		Source:   core.SourceAuto,
	})
	if err != nil {
		return nil
	}

	ac.lastCheckpoint = time.Now()
	return cp
}
```

We also need to add `CountSinceCheckpoint` to `CheckpointEngine`. This is a small helper:

**Add to `internal/core/checkpoint.go`:**

```go
// CountSinceCheckpoint returns operation counts per space since the latest checkpoint.
func (ce *CheckpointEngine) CountSinceCheckpoint(streamID string) (map[string]*SpaceOpCounts, error) {
	lastSeq := ce.LatestSeq(streamID)
	return ce.reader.CountBySpace(streamID, lastSeq)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./internal/watch/... -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/flakerim/LoomProject/loom
git add internal/watch/autocheckpoint.go internal/watch/autocheckpoint_test.go internal/core/checkpoint.go
git commit -m "feat(watch): add auto-checkpoint engine with op count and time thresholds"
```

---

### Task 4: Watcher — Orchestrate fsnotify → Filter → Debounce → OpWriter

**Files:**
- Create: `internal/watch/watcher.go`
- Create: `internal/watch/watcher_test.go`
- Modify: `go.mod` (add fsnotify dependency)

- [ ] **Step 1: Add fsnotify dependency**

```bash
cd /Users/flakerim/LoomProject/loom && go get github.com/fsnotify/fsnotify@latest && go mod tidy
```

- [ ] **Step 2: Write the failing tests**

```go
// internal/watch/watcher_test.go
package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupWatcherEnv(t *testing.T) (*core.Vault, string) {
	t.Helper()
	dir := t.TempDir()

	// Create a project structure
	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)

	vault, err := core.InitVault(dir)
	require.NoError(t, err)
	t.Cleanup(func() { vault.Close() })

	return vault, dir
}

func TestWatcher_DetectsFileCreate(t *testing.T) {
	vault, dir := setupWatcherEnv(t)

	stream, err := vault.ActiveStream()
	require.NoError(t, err)
	initialHead := stream.HeadSeq

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	go w.Start(ctx)
	time.Sleep(100 * time.Millisecond) // Let watcher start

	// Create a new file
	os.WriteFile(filepath.Join(dir, "new_file.go"), []byte("package main\n"), 0644)

	// Wait for debounce + processing
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Check that an operation was written
	head, _ := vault.OpReader.Head()
	assert.Greater(t, head, initialHead, "new operation should be written")
}

func TestWatcher_DetectsFileModify(t *testing.T) {
	vault, dir := setupWatcherEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	headBefore, _ := vault.OpReader.Head()

	go w.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Modify existing file
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)

	time.Sleep(300 * time.Millisecond)
	cancel()

	headAfter, _ := vault.OpReader.Head()
	assert.Greater(t, headAfter, headBefore)
}

func TestWatcher_DetectsFileDelete(t *testing.T) {
	vault, dir := setupWatcherEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	headBefore, _ := vault.OpReader.Head()

	go w.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	os.Remove(filepath.Join(dir, "main.go"))

	time.Sleep(300 * time.Millisecond)
	cancel()

	headAfter, _ := vault.OpReader.Head()
	assert.Greater(t, headAfter, headBefore)
}

func TestWatcher_IgnoresLoomDir(t *testing.T) {
	vault, dir := setupWatcherEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	headBefore, _ := vault.OpReader.Head()

	go w.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Write inside .loom — should be ignored
	os.WriteFile(filepath.Join(dir, ".loom", "temp"), []byte("ignored"), 0644)

	time.Sleep(300 * time.Millisecond)
	cancel()

	headAfter, _ := vault.OpReader.Head()
	assert.Equal(t, headBefore, headAfter, "changes inside .loom should be ignored")
}

func TestWatcher_StopsCleanly(t *testing.T) {
	vault, _ := setupWatcherEnv(t)

	ctx, cancel := context.WithCancel(context.Background())

	w, err := NewWatcher(vault, WatcherConfig{DebounceMs: 50})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		w.Start(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good — stopped cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop within timeout")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./internal/watch/... -v -count=1 -run Watcher`
Expected: Compilation error — `NewWatcher`, `WatcherConfig` not defined

- [ ] **Step 4: Implement Watcher**

```go
// internal/watch/watcher.go
package watch

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/constructspace/loom/internal/core"
	"github.com/fsnotify/fsnotify"
)

// WatcherConfig configures the watcher behavior.
type WatcherConfig struct {
	DebounceMs      int  // Debounce window in milliseconds (default 500)
	AutoCheckpoint  bool // Enable auto-checkpointing (default true)
	IntervalOps     int  // Auto-checkpoint op threshold
	IntervalSeconds int  // Auto-checkpoint time threshold
}

// Watcher observes file changes and writes operations to the vault.
type Watcher struct {
	vault      *core.Vault
	config     WatcherConfig
	filter     *Filter
	debouncer  *Debouncer
	autoCP     *AutoCheckpointer
	fsWatcher  *fsnotify.Watcher
	spaceMap   map[string]string // spaceID -> path
}

// NewWatcher creates a file watcher for the given vault.
func NewWatcher(vault *core.Vault, config WatcherConfig) (*Watcher, error) {
	if config.DebounceMs <= 0 {
		config.DebounceMs = 500
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Build space path map
	spaceMap := make(map[string]string)
	for spaceID, spaceCfg := range vault.Config.Spaces {
		spaceMap[spaceID] = spaceCfg.Path
	}

	filter := NewFilter(vault.Config.Watch.Ignore, vault.ProjectPath)
	debouncer := NewDebouncer(time.Duration(config.DebounceMs) * time.Millisecond)

	var autoCP *AutoCheckpointer
	if config.AutoCheckpoint {
		intervalOps := config.IntervalOps
		if intervalOps == 0 {
			intervalOps = vault.Config.CPoint.IntervalOps
		}
		intervalSecs := config.IntervalSeconds
		if intervalSecs == 0 {
			intervalSecs = vault.Config.CPoint.IntervalSeconds
		}
		autoCP = NewAutoCheckpointer(vault.Checkpoints, AutoCheckpointConfig{
			IntervalOps:     intervalOps,
			IntervalSeconds: intervalSecs,
		})
	}

	return &Watcher{
		vault:     vault,
		config:    config,
		filter:    filter,
		debouncer: debouncer,
		autoCP:    autoCP,
		fsWatcher: fsw,
		spaceMap:  spaceMap,
	}, nil
}

// Start begins watching the project directory. Blocks until ctx is cancelled.
func (w *Watcher) Start(ctx context.Context) error {
	defer w.fsWatcher.Close()

	// Add project directory recursively
	if err := w.addDirRecursive(w.vault.ProjectPath); err != nil {
		return err
	}

	debounced := w.debouncer.Start()

	for {
		select {
		case <-ctx.Done():
			w.debouncer.Stop()
			return nil

		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return nil
			}
			w.handleFSEvent(event)

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return nil
			}
			slog.Error("watcher error", "err", err)

		case evt := <-debounced:
			w.processEvent(evt)
		}
	}
}

func (w *Watcher) handleFSEvent(event fsnotify.Event) {
	relPath, err := filepath.Rel(w.vault.ProjectPath, event.Name)
	if err != nil {
		return
	}
	relPath = filepath.ToSlash(relPath)

	if w.filter.ShouldIgnore(relPath) {
		return
	}

	var op OpKind
	switch {
	case event.Has(fsnotify.Create):
		op = OpCreate
		// Watch new directories
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			w.fsWatcher.Add(event.Name)
			return // Don't emit events for directories
		}
	case event.Has(fsnotify.Write):
		op = OpModify
	case event.Has(fsnotify.Remove):
		op = OpDelete
	case event.Has(fsnotify.Rename):
		op = OpDelete // Rename = delete old path (new path gets a Create)
	default:
		return
	}

	// Skip directories
	if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
		return
	}

	w.debouncer.Send(FileEvent{Path: relPath, Op: op})
}

func (w *Watcher) processEvent(evt FileEvent) {
	stream, err := w.vault.ActiveStream()
	if err != nil {
		slog.Error("get active stream", "err", err)
		return
	}

	spaceID := w.filter.RouteToSpace(evt.Path, w.spaceMap)
	if spaceID == "" {
		return
	}

	// For the entity path, strip the space prefix if the space isn't root
	entityPath := evt.Path
	if spacePath, ok := w.spaceMap[spaceID]; ok && spacePath != "." {
		entityPath = strings.TrimPrefix(evt.Path, filepath.ToSlash(spacePath)+"/")
	}

	op := core.Operation{
		StreamID: stream.ID,
		SpaceID:  spaceID,
		EntityID: entityPath,
		Type:     toCoreOpType(evt.Op),
		Path:     entityPath,
		Author:   w.vault.Config.Author.Name,
		Meta: core.OpMeta{
			Source: "watch",
		},
	}

	// For create/modify, store the content
	if evt.Op == OpCreate || evt.Op == OpModify {
		fullPath := filepath.Join(w.vault.ProjectPath, evt.Path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			slog.Error("read file", "path", evt.Path, "err", err)
			return
		}
		hash, err := w.vault.Store.Write(content, "")
		if err != nil {
			slog.Error("store content", "path", evt.Path, "err", err)
			return
		}
		op.ObjectRef = hash
		op.Meta.Size = int64(len(content))
	}

	if _, err := w.vault.OpWriter.Write(op); err != nil {
		slog.Error("write operation", "path", evt.Path, "err", err)
		return
	}

	// Check auto-checkpoint
	if w.autoCP != nil {
		if cp := w.autoCP.MaybeCheckpoint(stream.ID, w.vault.Config.Author.Name); cp != nil {
			slog.Info("auto-checkpoint created", "title", cp.Title, "seq", cp.Seq)
		}
	}
}

func (w *Watcher) addDirRecursive(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(w.vault.ProjectPath, path)
		if w.filter.ShouldIgnore(filepath.ToSlash(relPath)) {
			return filepath.SkipDir
		}
		return w.fsWatcher.Add(path)
	})
}

func toCoreOpType(op OpKind) core.OpType {
	switch op {
	case OpCreate:
		return core.OpCreate
	case OpModify:
		return core.OpModify
	case OpDelete:
		return core.OpDelete
	case OpRename:
		return core.OpRename
	default:
		return core.OpModify
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./internal/watch/... -v -count=1 -timeout 30s`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/flakerim/LoomProject/loom
git add internal/watch/watcher.go internal/watch/watcher_test.go go.mod go.sum
git commit -m "feat(watch): add file watcher with fsnotify, space routing, and auto-checkpoint"
```

---

### Task 5: CLI — `loom watch` Command

**Files:**
- Create: `internal/cli/watch.go`
- Modify: `internal/cli/root.go` (register command)

- [ ] **Step 1: Implement the watch CLI command**

```go
// internal/cli/watch.go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/watch"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	var debounceMs int
	var noAutoCheckpoint bool

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch for file changes and auto-version",
		Long:  "Start a file watcher that automatically records operations for every file change.",
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			stream, err := vault.ActiveStream()
			if err != nil {
				return err
			}

			config := watch.WatcherConfig{
				DebounceMs:     debounceMs,
				AutoCheckpoint: !noAutoCheckpoint,
			}

			w, err := watch.NewWatcher(vault, config)
			if err != nil {
				return fmt.Errorf("create watcher: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Watching %s (stream: %s)\n", vault.Config.Project.Name, stream.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Spaces: ")
			for id := range vault.Config.Spaces {
				fmt.Fprintf(cmd.OutOrStdout(), "%s ", id)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl+C to stop.")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle interrupt signal
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Fprintln(cmd.OutOrStdout(), "\nStopping watcher...")
				cancel()
			}()

			return w.Start(ctx)
		},
	}

	cmd.Flags().IntVar(&debounceMs, "debounce", 0, "Debounce window in milliseconds (default: from config)")
	cmd.Flags().BoolVar(&noAutoCheckpoint, "no-auto-checkpoint", false, "Disable auto-checkpointing")

	return cmd
}
```

- [ ] **Step 2: Register the command in root.go**

Add `newWatchCmd()` to the `rootCmd.AddCommand(...)` list in `internal/cli/root.go`:

```go
rootCmd.AddCommand(
    newInitCmd(),
    newStatusCmd(),
    newCheckpointCmd(),
    newLogCmd(),
    newWeaveCmd(),
    newStreamCmd(),
    newHubCmd(),
    newSendCmd(),
    newReceiveCmd(),
    newWatchCmd(),
)
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/flakerim/LoomProject/loom && go build ./cmd/loom/`
Expected: Successful build, no errors

- [ ] **Step 4: Commit**

```bash
cd /Users/flakerim/LoomProject/loom
git add internal/cli/watch.go internal/cli/root.go
git commit -m "feat(cli): add 'loom watch' command for continuous auto-versioning"
```

---

### Task 6: Integration Test — End-to-End Watch Flow

**Files:**
- Create: `test/integration/watch_test.go`

- [ ] **Step 1: Write the integration test**

```go
// test/integration/watch_test.go
package integration

import (
	"context"
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

	// Create a project
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)

	vault, err := core.InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	headBefore, _ := vault.OpReader.Head()

	// Start watcher
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := watch.NewWatcher(vault, watch.WatcherConfig{
		DebounceMs:     50,
		AutoCheckpoint: false, // manual control
	})
	require.NoError(t, err)

	go w.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Create a file
	os.WriteFile(filepath.Join(dir, "handler.go"), []byte("package main\n\nfunc handler() {}\n"), 0644)
	time.Sleep(200 * time.Millisecond)

	// Modify the file
	os.WriteFile(filepath.Join(dir, "handler.go"), []byte("package main\n\nfunc handler() { return }\n"), 0644)
	time.Sleep(200 * time.Millisecond)

	// Delete a file
	os.Remove(filepath.Join(dir, "handler.go"))
	time.Sleep(200 * time.Millisecond)

	cancel()

	headAfter, _ := vault.OpReader.Head()
	assert.Greater(t, headAfter, headBefore, "operations should have been recorded")

	// Verify we have create, modify, delete ops
	ops, err := vault.OpReader.ReadRange(headBefore, headAfter)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(ops), 2, "should have at least create+delete ops")
}

func TestWatch_EndToEnd_AutoCheckpoint(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)

	vault, err := core.InitVault(dir)
	require.NoError(t, err)
	defer vault.Close()

	stream, _ := vault.ActiveStream()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := watch.NewWatcher(vault, watch.WatcherConfig{
		DebounceMs:      50,
		AutoCheckpoint:  true,
		IntervalOps:     3, // Low threshold for testing
		IntervalSeconds: 9999,
	})
	require.NoError(t, err)

	go w.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Create enough files to trigger auto-checkpoint
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, fmt.Sprintf("file_%d.go", i))
		os.WriteFile(name, []byte("package main\n"), 0644)
		time.Sleep(150 * time.Millisecond) // Space out to allow debounce
	}
	time.Sleep(500 * time.Millisecond) // Let auto-checkpoint evaluate

	cancel()

	// Check for auto-checkpoint
	cps, err := vault.Checkpoints.List(stream.ID, 10)
	require.NoError(t, err)

	autoCount := 0
	for _, cp := range cps {
		if cp.Source == core.SourceAuto {
			autoCount++
		}
	}
	assert.Greater(t, autoCount, 0, "auto-checkpoint should have been created")
}
```

- [ ] **Step 2: Run the integration test**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./test/integration/... -v -count=1 -run Watch -timeout 30s`
Expected: All PASS

- [ ] **Step 3: Run the full test suite**

Run: `cd /Users/flakerim/LoomProject/loom && go test ./... -count=1 -timeout 60s`
Expected: All packages PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/flakerim/LoomProject/loom
git add test/integration/watch_test.go
git commit -m "test: add end-to-end integration tests for file watching"
```

---

## Verification Checklist

After all tasks are complete:

- [ ] `go test ./... -count=1` — all pass
- [ ] `go build ./cmd/loom/` — compiles cleanly
- [ ] `go vet ./...` — no issues
- [ ] Manual test: `cd /tmp/test-project && loom init && loom watch` → create/edit/delete files → see ops in `loom log`
