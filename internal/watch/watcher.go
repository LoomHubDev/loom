package watch

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/constructspace/loom/internal/core"
	"github.com/fsnotify/fsnotify"
)

// WatcherConfig holds configuration for the file watcher.
type WatcherConfig struct {
	DebounceMs      int
	AutoCheckpoint  bool
	IntervalOps     int
	IntervalSeconds int
}

// Watcher orchestrates fsnotify events through filter, debounce, and op writing.
type Watcher struct {
	vault     *core.Vault
	config    WatcherConfig
	filter    *Filter
	debouncer *Debouncer
	autoCP    *AutoCheckpointer
	fsWatcher *fsnotify.Watcher
	spaceMap  map[string]string // spaceID -> path
}

// NewWatcher creates a Watcher wired to the given vault.
func NewWatcher(vault *core.Vault, config WatcherConfig) (*Watcher, error) {
	if config.DebounceMs <= 0 {
		config.DebounceMs = 500
	}

	fsW, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Build spaceMap from vault config.
	spaceMap := make(map[string]string, len(vault.Config.Spaces))
	for id, sc := range vault.Config.Spaces {
		spaceMap[id] = sc.Path
	}

	filter := NewFilter(vault.Config.Watch.Ignore, vault.ProjectPath)
	debouncer := NewDebouncer(time.Duration(config.DebounceMs) * time.Millisecond)

	w := &Watcher{
		vault:     vault,
		config:    config,
		filter:    filter,
		debouncer: debouncer,
		fsWatcher: fsW,
		spaceMap:  spaceMap,
	}

	if config.AutoCheckpoint {
		intervalOps := config.IntervalOps
		if intervalOps <= 0 {
			intervalOps = vault.Config.CPoint.IntervalOps
		}
		intervalSecs := config.IntervalSeconds
		if intervalSecs <= 0 {
			intervalSecs = vault.Config.CPoint.IntervalSeconds
		}
		w.autoCP = NewAutoCheckpointer(vault.Checkpoints, AutoCheckpointConfig{
			IntervalOps:     intervalOps,
			IntervalSeconds: intervalSecs,
		})
	}

	return w, nil
}

// Start begins watching the project directory. It blocks until ctx is cancelled.
func (w *Watcher) Start(ctx context.Context) error {
	defer w.fsWatcher.Close()

	w.addDirRecursive(w.vault.ProjectPath)

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
			log.Printf("watch error: %v", err)

		case evt, ok := <-debounced:
			if !ok {
				return nil
			}
			w.processEvent(evt)
		}
	}
}

// handleFSEvent converts an fsnotify event into a debounced FileEvent.
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
		// If it's a new directory, add it to the watcher and return.
		if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
			w.fsWatcher.Add(event.Name)
			return
		}
		op = OpCreate

	case event.Has(fsnotify.Write):
		op = OpModify

	case event.Has(fsnotify.Remove):
		op = OpDelete

	case event.Has(fsnotify.Rename):
		op = OpDelete

	default:
		return
	}

	// Skip directories for non-create events.
	if op != OpCreate {
		if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
			return
		}
		// For Remove/Rename the file is gone, statErr != nil is expected — that's fine.
	}

	w.debouncer.Send(FileEvent{Path: relPath, Op: op})
}

// processEvent writes a core.Operation for the given debounced event.
func (w *Watcher) processEvent(evt FileEvent) {
	stream, err := w.vault.ActiveStream()
	if err != nil {
		return
	}

	spaceID := w.filter.RouteToSpace(evt.Path, w.spaceMap)
	if spaceID == "" {
		return
	}

	// Build entity path: strip the space prefix when it's not root.
	entityPath := evt.Path
	spacePath := w.spaceMap[spaceID]
	if spacePath != "." && spacePath != "" {
		sp := filepath.ToSlash(spacePath)
		entityPath = strings.TrimPrefix(evt.Path, sp+"/")
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

	// For create/modify: read content and store the object.
	if evt.Op == OpCreate || evt.Op == OpModify {
		fullPath := filepath.Join(w.vault.ProjectPath, evt.Path)
		content, readErr := os.ReadFile(fullPath)
		if readErr != nil {
			return
		}
		hash, storeErr := w.vault.Store.Write(content, "")
		if storeErr != nil {
			return
		}
		op.ObjectRef = hash
		op.Meta.Size = int64(len(content))
	}

	if _, writeErr := w.vault.OpWriter.Write(op); writeErr != nil {
		log.Printf("watch: write op failed: %v", writeErr)
		return
	}

	// Auto-checkpoint check.
	if w.autoCP != nil {
		if cp := w.autoCP.MaybeCheckpoint(stream.ID, w.vault.Config.Author.Name); cp != nil {
			log.Printf("watch: auto-checkpoint created: %s", cp.ID)
		}
	}
}

// addDirRecursive walks root and adds every non-ignored directory to fsWatcher.
func (w *Watcher) addDirRecursive(root string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		relPath, relErr := filepath.Rel(w.vault.ProjectPath, path)
		if relErr != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		if relPath != "." && w.filter.ShouldIgnore(relPath) {
			return filepath.SkipDir
		}
		w.fsWatcher.Add(path)
		return nil
	})
}

// toCoreOpType maps a watch.OpKind to core.OpType.
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
