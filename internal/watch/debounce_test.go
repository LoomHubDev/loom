package watch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWindow = 50 * time.Millisecond

// collect drains up to maxEvents from ch within timeout, then returns what it got.
func collect(t *testing.T, ch <-chan FileEvent, maxEvents int, timeout time.Duration) []FileEvent {
	t.Helper()
	var events []FileEvent
	deadline := time.After(timeout)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, evt)
			if len(events) >= maxEvents {
				return events
			}
		case <-deadline:
			return events
		}
	}
}

// TestDebouncer_CoalescesRapidEvents verifies that 5 rapid modify events for
// the same path result in exactly one emitted event.
func TestDebouncer_CoalescesRapidEvents(t *testing.T) {
	d := NewDebouncer(testWindow)
	out := d.Start()
	defer d.Stop()

	for i := 0; i < 5; i++ {
		d.Send(FileEvent{Path: "foo.go", Op: OpModify})
	}

	events := collect(t, out, 2, testWindow*3)
	require.Len(t, events, 1, "expected exactly 1 coalesced event")
	assert.Equal(t, "foo.go", events[0].Path)
	assert.Equal(t, OpModify, events[0].Op)
}

// TestDebouncer_DifferentFilesEmitSeparately verifies that events for different
// paths produce separate output events.
func TestDebouncer_DifferentFilesEmitSeparately(t *testing.T) {
	d := NewDebouncer(testWindow)
	out := d.Start()
	defer d.Stop()

	d.Send(FileEvent{Path: "a.go", Op: OpModify})
	d.Send(FileEvent{Path: "b.go", Op: OpModify})

	events := collect(t, out, 3, testWindow*4)
	require.Len(t, events, 2, "expected one event per file")

	paths := map[string]bool{}
	for _, e := range events {
		paths[e.Path] = true
	}
	assert.True(t, paths["a.go"], "a.go event missing")
	assert.True(t, paths["b.go"], "b.go event missing")
}

// TestDebouncer_CreateThenModifyBecomesCreate verifies the create+modify
// coalescing rule.
func TestDebouncer_CreateThenModifyBecomesCreate(t *testing.T) {
	d := NewDebouncer(testWindow)
	out := d.Start()
	defer d.Stop()

	d.Send(FileEvent{Path: "new.go", Op: OpCreate})
	d.Send(FileEvent{Path: "new.go", Op: OpModify})

	events := collect(t, out, 2, testWindow*3)
	require.Len(t, events, 1)
	assert.Equal(t, OpCreate, events[0].Op)
}

// TestDebouncer_CreateThenDeleteCancels verifies that a create followed by a
// delete within the debounce window emits no event.
func TestDebouncer_CreateThenDeleteCancels(t *testing.T) {
	d := NewDebouncer(testWindow)
	out := d.Start()
	defer d.Stop()

	d.Send(FileEvent{Path: "tmp.go", Op: OpCreate})
	d.Send(FileEvent{Path: "tmp.go", Op: OpDelete})

	events := collect(t, out, 1, testWindow*3)
	assert.Empty(t, events, "create+delete should cancel — no event expected")
}

// TestDebouncer_DeleteEmits verifies that a lone delete event is emitted.
func TestDebouncer_DeleteEmits(t *testing.T) {
	d := NewDebouncer(testWindow)
	out := d.Start()
	defer d.Stop()

	d.Send(FileEvent{Path: "gone.go", Op: OpDelete})

	events := collect(t, out, 2, testWindow*3)
	require.Len(t, events, 1)
	assert.Equal(t, OpDelete, events[0].Op)
	assert.Equal(t, "gone.go", events[0].Path)
}

// TestDebouncer_StopDrainsCleanly verifies that Stop does not panic or hang
// even when there are pending events in flight.
func TestDebouncer_StopDrainsCleanly(t *testing.T) {
	d := NewDebouncer(testWindow)
	_ = d.Start()

	// Send several events without waiting for the window to expire.
	for i := 0; i < 10; i++ {
		d.Send(FileEvent{Path: "x.go", Op: OpModify})
	}

	done := make(chan struct{})
	go func() {
		d.Stop()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() hung")
	}
}
