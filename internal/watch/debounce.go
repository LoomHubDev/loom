package watch

import (
	"sync"
	"time"
)

// OpKind identifies the type of file-system operation.
type OpKind string

const (
	OpCreate OpKind = "create"
	OpModify OpKind = "modify"
	OpDelete OpKind = "delete"
	OpRename OpKind = "rename"
)

// FileEvent represents a debounced file-system event.
type FileEvent struct {
	Path    string
	Op      OpKind
	OldPath string // populated for rename events
}

// pending holds state for a single path that is inside the debounce window.
type pending struct {
	evt   FileEvent
	timer *time.Timer
}

// Debouncer coalesces rapid file events per-path into a single event after a
// quiet period (window). The coalescing rules are:
//
//   create + delete  → cancelled (nothing emitted)
//   create + modify  → create
//   modify + delete  → delete
//   delete + create  → create  (re-created file)
//   anything else    → latest event wins
type Debouncer struct {
	window time.Duration

	mu      sync.Mutex
	pending map[string]*pending

	in   chan FileEvent
	out  chan FileEvent
	done chan struct{}
	once sync.Once
}

// NewDebouncer creates a Debouncer with the given quiet window.
func NewDebouncer(window time.Duration) *Debouncer {
	return &Debouncer{
		window:  window,
		pending: make(map[string]*pending),
		in:      make(chan FileEvent, 64),
		out:     make(chan FileEvent, 64),
		done:    make(chan struct{}),
	}
}

// Start begins the processing goroutine and returns the output channel.
func (d *Debouncer) Start() <-chan FileEvent {
	go d.run()
	return d.out
}

// Send submits an event for debouncing. It is safe to call from multiple
// goroutines. Calls after Stop are silently dropped.
func (d *Debouncer) Send(evt FileEvent) {
	select {
	case <-d.done:
	case d.in <- evt:
	}
}

// Stop performs a graceful shutdown. It waits for the processing goroutine to
// exit and closes the output channel.
func (d *Debouncer) Stop() {
	d.once.Do(func() {
		close(d.done)
	})
}

// run is the processing goroutine. It reads from in and handles timer-driven
// flushes.
func (d *Debouncer) run() {
	defer close(d.out)

	for {
		select {
		case <-d.done:
			// Drain any remaining pending events immediately.
			d.mu.Lock()
			for path, p := range d.pending {
				p.timer.Stop()
				delete(d.pending, path)
				d.out <- p.evt
			}
			d.mu.Unlock()
			return

		case evt, ok := <-d.in:
			if !ok {
				return
			}
			d.schedule(evt)
		}
	}
}

// schedule inserts or merges evt into the pending map, resetting (or creating)
// the per-path timer.
func (d *Debouncer) schedule(evt FileEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	p, exists := d.pending[evt.Path]
	if !exists {
		// First event for this path — arm the timer.
		p = &pending{}
		p.evt = evt
		p.timer = time.AfterFunc(d.window, func() {
			d.flush(evt.Path)
		})
		d.pending[evt.Path] = p
		return
	}

	// Merge incoming event with the existing accumulated event.
	p.evt = coalesce(p.evt, evt)

	// Reset the quiet-period timer.
	p.timer.Reset(d.window)
}

// flush emits the accumulated event for path (if any) and removes it from the
// pending map. It is called by the per-path timer.
func (d *Debouncer) flush(path string) {
	d.mu.Lock()
	p, ok := d.pending[path]
	if !ok {
		d.mu.Unlock()
		return
	}
	evt := p.evt
	cancelled := isCancelled(evt)
	delete(d.pending, path)
	d.mu.Unlock()

	if !cancelled {
		select {
		case d.out <- evt:
		case <-d.done:
		}
	}
}

// coalesce applies the op-merging rules to produce the effective event after
// both prev and next have occurred within the debounce window.
func coalesce(prev, next FileEvent) FileEvent {
	switch {
	case prev.Op == OpCreate && next.Op == OpModify:
		// A file was created and then modified — still a create.
		return FileEvent{Path: prev.Path, Op: OpCreate, OldPath: prev.OldPath}

	case prev.Op == OpCreate && next.Op == OpDelete:
		// Created then deleted within window — mark as cancelled.
		return FileEvent{Path: prev.Path, Op: "cancelled"}

	case prev.Op == OpModify && next.Op == OpDelete:
		return FileEvent{Path: prev.Path, Op: OpDelete}

	case prev.Op == OpDelete && next.Op == OpCreate:
		// Deleted then re-created — treat as a plain create.
		return FileEvent{Path: prev.Path, Op: OpCreate}

	default:
		// Latest event wins.
		return next
	}
}

// isCancelled reports whether an event has been marked as cancelled by the
// coalescing logic (create+delete).
func isCancelled(evt FileEvent) bool {
	return evt.Op == "cancelled"
}
