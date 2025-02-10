package article

import (
	"context"
	"time"
)

// Progress tracking context keys
type progressContextKey string

const (
	progressStartTimeKey    progressContextKey = "progress_start_time"
	progressCallbackKey     progressContextKey = "progress_callback"
	progressCurrentPhaseKey progressContextKey = "progress_current_phase"
)

// ProgressCallback is a function that receives progress events
type ProgressCallback func(event ProgressEvent)

// ProgressTracker helps track and emit progress events
type ProgressTracker struct {
	startTime time.Time
	callback  ProgressCallback
	ctx       context.Context
}

// WithProgressTracking adds progress tracking to a context
func WithProgressTracking(ctx context.Context, callback ProgressCallback) context.Context {
	startTime := time.Now()
	ctx = context.WithValue(ctx, progressStartTimeKey, startTime)
	ctx = context.WithValue(ctx, progressCallbackKey, callback)
	return ctx
}

// WithProgressPhase sets the current progress phase in context
func WithProgressPhase(ctx context.Context, phase ProgressPhase) context.Context {
	return context.WithValue(ctx, progressCurrentPhaseKey, phase)
}

// NewProgressTracker creates a new progress tracker from context
func NewProgressTracker(ctx context.Context) *ProgressTracker {
	startTime, _ := ctx.Value(progressStartTimeKey).(time.Time)
	callback, _ := ctx.Value(progressCallbackKey).(ProgressCallback)

	if startTime.IsZero() {
		startTime = time.Now()
	}

	return &ProgressTracker{
		startTime: startTime,
		callback:  callback,
		ctx:       ctx,
	}
}

// EmitProgress emits a progress event
func (pt *ProgressTracker) EmitProgress(phase ProgressPhase, step string, progress float64, details map[string]interface{}) {
	if pt.callback == nil {
		return
	}

	elapsed := time.Since(pt.startTime)

	// Estimate remaining time based on current progress
	var estimatedRemaining time.Duration
	if progress > 0 && progress < 1.0 {
		totalEstimated := time.Duration(float64(elapsed) / progress)
		estimatedRemaining = totalEstimated - elapsed
	}

	event := NewProgressEvent(pt.ctx, phase, step, progress, elapsed, estimatedRemaining, details)
	pt.callback(event)
}

// EmitPhaseStart emits a progress event for the start of a phase
func (pt *ProgressTracker) EmitPhaseStart(phase ProgressPhase, step string, baseProgress float64) {
	pt.EmitProgress(phase, step, baseProgress, map[string]interface{}{
		"phase_start": true,
	})
}

// EmitPhaseComplete emits a progress event for the completion of a phase
func (pt *ProgressTracker) EmitPhaseComplete(phase ProgressPhase, step string, baseProgress float64) {
	pt.EmitProgress(phase, step, baseProgress, map[string]interface{}{
		"phase_complete": true,
	})
}

// EmitSubProgress emits a progress event for sub-tasks within a phase
func (pt *ProgressTracker) EmitSubProgress(phase ProgressPhase, step string, baseProgress, subProgress, phaseWeight float64, details map[string]interface{}) {
	// Calculate overall progress: baseProgress + (subProgress * phaseWeight)
	overallProgress := baseProgress + (subProgress * phaseWeight)

	if details == nil {
		details = make(map[string]interface{})
	}
	details["sub_progress"] = subProgress
	details["phase_weight"] = phaseWeight

	pt.EmitProgress(phase, step, overallProgress, details)
}

// Progress phase weights for calculating overall progress
const (
	PlanningWeight = 0.20 // 20% of total progress
	WritingWeight  = 0.60 // 60% of total progress
	EditingWeight  = 0.20 // 20% of total progress
)

// GetPhaseBaseProgress returns the base progress for a phase
func GetPhaseBaseProgress(phase ProgressPhase) float64 {
	switch phase {
	case PhaseInitializing:
		return 0.0
	case PhasePlanning:
		return 0.0
	case PhaseWriting:
		return PlanningWeight
	case PhaseEditing:
		return PlanningWeight + WritingWeight
	case PhaseCompleted:
		return 1.0
	default:
		return 0.0
	}
}

// ProgressEventChannel creates a channel for progress events and returns both the channel and callback
func ProgressEventChannel() (<-chan ProgressEvent, ProgressCallback) {
	ch := make(chan ProgressEvent, 10) // Buffered channel to prevent blocking

	callback := func(event ProgressEvent) {
		select {
		case ch <- event:
		default:
			// Channel is full, skip this event to prevent blocking
		}
	}

	return ch, callback
}
