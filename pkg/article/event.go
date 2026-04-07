package article

import (
	"context"
	"time"
)

// Source represents a research source document.
type Source struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Keywords   []string `json:"keywords"`
	SourceType string   `json:"source_type"` // "web", "article", "academic", "news"
	Relevance  float64  `json:"relevance"`
}

// ProgressPhase represents the current phase of article generation
type ProgressPhase string

const (
	PhaseInitializing ProgressPhase = "initializing"
	PhaseResearching  ProgressPhase = "researching"
	PhasePlanning     ProgressPhase = "planning"
	PhaseWriting      ProgressPhase = "writing"
	PhaseEditing     ProgressPhase = "editing"
	PhaseAttributing ProgressPhase = "attributing"
	PhaseCompleted    ProgressPhase = "completed"
)

// ProgressEvent carries progress information during article generation
type ProgressEvent interface {
	Phase() ProgressPhase
	Step() string
	Progress() float64 // 0.0 to 1.0
	ElapsedTime() time.Duration
	EstimatedTimeRemaining() time.Duration
	Details() map[string]interface{}
}

type BaseProgressEvent struct {
	ctx                    context.Context
	phase                  ProgressPhase
	step                   string
	progress               float64
	elapsedTime            time.Duration
	estimatedTimeRemaining time.Duration
	details                map[string]interface{}
}

// Phase implements ProgressEvent.
func (e *BaseProgressEvent) Phase() ProgressPhase {
	return e.phase
}

// Step implements ProgressEvent.
func (e *BaseProgressEvent) Step() string {
	return e.step
}

// Progress implements ProgressEvent.
func (e *BaseProgressEvent) Progress() float64 {
	return e.progress
}

// ElapsedTime implements ProgressEvent.
func (e *BaseProgressEvent) ElapsedTime() time.Duration {
	return e.elapsedTime
}

// EstimatedTimeRemaining implements ProgressEvent.
func (e *BaseProgressEvent) EstimatedTimeRemaining() time.Duration {
	return e.estimatedTimeRemaining
}

// Details implements ProgressEvent.
func (e *BaseProgressEvent) Details() map[string]interface{} {
	return e.details
}

var _ ProgressEvent = &BaseProgressEvent{}

// NewProgressEvent creates a new progress event
func NewProgressEvent(ctx context.Context, phase ProgressPhase, step string, progress float64, elapsedTime time.Duration, estimatedTimeRemaining time.Duration, details map[string]interface{}) *BaseProgressEvent {
	if details == nil {
		details = make(map[string]interface{})
	}

	return &BaseProgressEvent{
		ctx:                    ctx,
		phase:                  phase,
		step:                   step,
		progress:               progress,
		elapsedTime:            elapsedTime,
		estimatedTimeRemaining: estimatedTimeRemaining,
		details:                details,
	}
}
