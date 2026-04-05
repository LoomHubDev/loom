package watch

import (
	"fmt"
	"time"

	"github.com/constructspace/loom/internal/core"
)

// AutoCheckpointConfig holds the thresholds for automatic checkpoint creation.
type AutoCheckpointConfig struct {
	// IntervalOps is the number of pending operations that trigger a checkpoint.
	// A value of 0 disables the op-count threshold.
	IntervalOps int
	// IntervalSeconds is the minimum seconds between automatic checkpoints.
	// A value of 0 means: trigger whenever there are ops and the time threshold is checked.
	// A negative value disables the time threshold.
	IntervalSeconds int
}

// AutoCheckpointer watches op counts and elapsed time to trigger checkpoints
// automatically.
type AutoCheckpointer struct {
	engine         *core.CheckpointEngine
	config         AutoCheckpointConfig
	LastCheckpoint time.Time // exported for testing
}

// NewAutoCheckpointer creates a new AutoCheckpointer with the given engine and
// config. LastCheckpoint is initialised to the current time so the first
// interval is measured from construction.
func NewAutoCheckpointer(engine *core.CheckpointEngine, config AutoCheckpointConfig) *AutoCheckpointer {
	return &AutoCheckpointer{
		engine:         engine,
		config:         config,
		LastCheckpoint: time.Now(),
	}
}

// MaybeCheckpoint inspects pending operation counts and elapsed time for
// streamID and creates an auto-checkpoint if any threshold is exceeded.
// It returns the new Checkpoint, or nil if no checkpoint was created.
func (ac *AutoCheckpointer) MaybeCheckpoint(streamID, author string) *core.Checkpoint {
	counts, err := ac.engine.CountSinceCheckpoint(streamID)
	if err != nil {
		return nil
	}

	// Sum all pending operations across spaces.
	var pendingOps int
	for _, sc := range counts {
		pendingOps += sc.Created + sc.Modified + sc.Deleted
	}

	// Nothing to checkpoint.
	if pendingOps == 0 {
		return nil
	}

	triggered := false
	reason := ""

	// Op-count threshold.
	if ac.config.IntervalOps > 0 && pendingOps >= ac.config.IntervalOps {
		triggered = true
		reason = fmt.Sprintf("%d ops", pendingOps)
	}

	// Time threshold (only when IntervalSeconds >= 0).
	if !triggered && ac.config.IntervalSeconds >= 0 {
		threshold := time.Duration(ac.config.IntervalSeconds) * time.Second
		if time.Since(ac.LastCheckpoint) >= threshold {
			triggered = true
			reason = fmt.Sprintf("%ds interval", ac.config.IntervalSeconds)
		}
	}

	if !triggered {
		return nil
	}

	cp, err := ac.engine.Create(core.CheckpointInput{
		StreamID: streamID,
		Title:    fmt.Sprintf("Auto-checkpoint (%s)", reason),
		Author:   author,
		Source:   core.SourceAuto,
	})
	if err != nil {
		return nil
	}

	ac.LastCheckpoint = time.Now()
	return cp
}
