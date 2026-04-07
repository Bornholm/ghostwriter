package article

import (
	"context"
	"time"
)

type progressContextKey string

const (
	progressStartTimeKey progressContextKey = "progress_start_time"
	progressCallbackKey  progressContextKey = "progress_callback"
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
func (pt *ProgressTracker) EmitProgress(phase ProgressPhase, step string, progress float64, details map[string]any) {
	if pt.callback == nil {
		return
	}

	elapsed := time.Since(pt.startTime)

	var estimatedRemaining time.Duration
	if progress > 0 && progress < 1.0 {
		totalEstimated := time.Duration(float64(elapsed) / progress)
		estimatedRemaining = totalEstimated - elapsed
	}

	event := NewProgressEvent(pt.ctx, phase, step, progress, elapsed, estimatedRemaining, details)
	pt.callback(event)
}
