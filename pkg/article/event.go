package article

import (
	"context"
	"time"
)

// DocumentSection represents a section in the article plan
type DocumentSection struct {
	ID          string   `json:"-"`
	Title       string   `json:"title" jsonschema:"required,description=Title of the section"`
	Description string   `json:"description" jsonschema:"required,description=Description or guidance for writing this section"`
	KeyPoints   []string `json:"key_points" jsonschema:"required,description=Key points to cover in this section"`
	WordCount   int      `json:"word_count" jsonschema:"required,description=Target word count for this section"`
}

// DocumentPlan represents the complete article structure
type DocumentPlan struct {
	Title      string             `json:"title" jsonschema:"required,description=The main title of the article"`
	Sections   []*DocumentSection `json:"sections" jsonschema:"required,description=Array of document sections"`
	TotalWords int                `json:"total_words" jsonschema:"required,description=Target total number of words for the article"`
	Keywords   []string           `json:"keywords" jsonschema:"required,description=Principal keywords illustrating the article"`
}

// SectionContent represents completed content for a section
type SectionContent struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	WordCount int    `json:"word_count"`
}

// Document represents the final complete article
type Document struct {
	DocumentMetadata
	Summary  string           `json:"summary"`
	Content  string           `json:"content"`
	Sections []SectionContent `json:"sections"`
}

type DocumentMetadata struct {
	Title     string   `json:"title"`
	WordCount int      `json:"word_count"`
	Keywords  []string `json:"keywords"`
	Sources   []Source `json:"sources"`
}

type Source struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Keywords   []string `json:"keywords"`
	SourceType string   `json:"source_type"` // "web", "article", "academic", "news"
	Relevance  float64  `json:"relevance"`
}

// SectionReview is the result returned by the reviewer agent for a section
type SectionReview struct {
	Approved bool   `json:"approved"`
	Content  string `json:"content"`  // set when Approved=true, contains polished content
	Feedback string `json:"feedback"` // set when Approved=false, contains revision instructions
}

// ProgressPhase represents the current phase of article generation
type ProgressPhase string

const (
	PhaseInitializing ProgressPhase = "initializing"
	PhaseResearching  ProgressPhase = "researching"
	PhasePlanning     ProgressPhase = "planning"
	PhaseWriting      ProgressPhase = "writing"
	PhaseReviewing    ProgressPhase = "reviewing"
	PhaseEditing      ProgressPhase = "editing" // kept for progress.go compat, will be removed with old editor
	PhaseAttributing  ProgressPhase = "attributing"
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
