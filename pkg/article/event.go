package article

import (
	"context"
	"time"

	"github.com/bornholm/genai/agent"
)

// DocumentSection represents a section in the article plan
type DocumentSection struct {
	ID          string   `json:"id" jsonschema:"required,description=Unique identifier for the section"`
	Title       string   `json:"title" jsonschema:"required,description=Title of the section"`
	Description string   `json:"description" jsonschema:"required,description=Description or guidance for writing this section"`
	KeyPoints   []string `json:"key_points" jsonschema:"required,description=Key points to cover in this section"`
	WordCount   int      `json:"word_count" jsonschema:"required,description=Target word count for this section"`
}

// DocumentPlan represents the complete article structure
type DocumentPlan struct {
	Title      string            `json:"title" jsonschema:"required,description=The main title of the article"`
	Sections   []DocumentSection `json:"sections" jsonschema:"required,description=Array of document sections"`
	TotalWords int               `json:"total_words" jsonschema:"required,description=Target total number of words for the article"`
	Keywords   []string          `json:"keywords" jsonschema:"required,description=Principal keywords illustrating the article"`
}

// SectionContent represents completed content for a section
type SectionContent struct {
	SectionID string `json:"section_id"`
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

// DocumentPlanEvent carries the article plan from planner to orchestrator
type DocumentPlanEvent interface {
	agent.MessageEvent
	Plan() DocumentPlan
	Subject() string
	Origin() agent.MessageEvent
}

type BaseDocumentPlanEvent struct {
	id      agent.EventID
	ctx     context.Context
	plan    DocumentPlan
	subject string
	origin  agent.MessageEvent
}

// ID implements DocumentPlanEvent.
func (e *BaseDocumentPlanEvent) ID() agent.EventID {
	return e.id
}

// Context implements DocumentPlanEvent.
func (e *BaseDocumentPlanEvent) Context() context.Context {
	return e.ctx
}

// WithContext implements DocumentPlanEvent.
func (e *BaseDocumentPlanEvent) WithContext(ctx context.Context) agent.Event {
	return &BaseDocumentPlanEvent{
		id:      e.id,
		ctx:     ctx,
		plan:    e.plan,
		subject: e.subject,
		origin:  e.origin,
	}
}

// Message implements DocumentPlanEvent.
func (e *BaseDocumentPlanEvent) Message() string {
	return "Document plan created for: " + e.subject
}

// Plan implements DocumentPlanEvent.
func (e *BaseDocumentPlanEvent) Plan() DocumentPlan {
	return e.plan
}

// Subject implements DocumentPlanEvent.
func (e *BaseDocumentPlanEvent) Subject() string {
	return e.subject
}

// Origin implements DocumentPlanEvent.
func (e *BaseDocumentPlanEvent) Origin() agent.MessageEvent {
	return e.origin
}

var _ DocumentPlanEvent = &BaseDocumentPlanEvent{}

// NewDocumentPlanEvent creates a new document plan event
func NewDocumentPlanEvent(ctx context.Context, plan DocumentPlan, subject string, origin agent.MessageEvent) *BaseDocumentPlanEvent {
	return &BaseDocumentPlanEvent{
		id:      agent.NewEventID(),
		ctx:     ctx,
		plan:    plan,
		subject: subject,
		origin:  origin,
	}
}

// SectionAssignmentEvent assigns a section to a writer agent
type SectionAssignmentEvent interface {
	agent.MessageEvent
	Section() DocumentSection
	Subject() string
	Context() context.Context
	Origin() agent.MessageEvent
}

type BaseSectionAssignmentEvent struct {
	id      agent.EventID
	ctx     context.Context
	section DocumentSection
	subject string
	origin  agent.MessageEvent
}

// ID implements SectionAssignmentEvent.
func (e *BaseSectionAssignmentEvent) ID() agent.EventID {
	return e.id
}

// Context implements SectionAssignmentEvent.
func (e *BaseSectionAssignmentEvent) Context() context.Context {
	return e.ctx
}

// WithContext implements SectionAssignmentEvent.
func (e *BaseSectionAssignmentEvent) WithContext(ctx context.Context) agent.Event {
	return &BaseSectionAssignmentEvent{
		id:      e.id,
		ctx:     ctx,
		section: e.section,
		subject: e.subject,
		origin:  e.origin,
	}
}

// Message implements SectionAssignmentEvent.
func (e *BaseSectionAssignmentEvent) Message() string {
	return "Write section: " + e.section.Title
}

// Section implements SectionAssignmentEvent.
func (e *BaseSectionAssignmentEvent) Section() DocumentSection {
	return e.section
}

// Subject implements SectionAssignmentEvent.
func (e *BaseSectionAssignmentEvent) Subject() string {
	return e.subject
}

// Origin implements SectionAssignmentEvent.
func (e *BaseSectionAssignmentEvent) Origin() agent.MessageEvent {
	return e.origin
}

var _ SectionAssignmentEvent = &BaseSectionAssignmentEvent{}

// NewSectionAssignmentEvent creates a new section assignment event
func NewSectionAssignmentEvent(ctx context.Context, section DocumentSection, subject string, origin agent.MessageEvent) *BaseSectionAssignmentEvent {
	return &BaseSectionAssignmentEvent{
		id:      agent.NewEventID(),
		ctx:     ctx,
		section: section,
		subject: subject,
		origin:  origin,
	}
}

// SectionContentEvent carries completed section content
type SectionContentEvent interface {
	agent.MessageEvent
	Content() SectionContent
	Origin() agent.MessageEvent
}

type BaseSectionContentEvent struct {
	id      agent.EventID
	ctx     context.Context
	content SectionContent
	origin  agent.MessageEvent
}

// ID implements SectionContentEvent.
func (e *BaseSectionContentEvent) ID() agent.EventID {
	return e.id
}

// Context implements SectionContentEvent.
func (e *BaseSectionContentEvent) Context() context.Context {
	return e.ctx
}

// WithContext implements SectionContentEvent.
func (e *BaseSectionContentEvent) WithContext(ctx context.Context) agent.Event {
	return &BaseSectionContentEvent{
		id:      e.id,
		ctx:     ctx,
		content: e.content,
		origin:  e.origin,
	}
}

// Message implements SectionContentEvent.
func (e *BaseSectionContentEvent) Message() string {
	return "Section completed: " + e.content.Title
}

// Content implements SectionContentEvent.
func (e *BaseSectionContentEvent) Content() SectionContent {
	return e.content
}

// Origin implements SectionContentEvent.
func (e *BaseSectionContentEvent) Origin() agent.MessageEvent {
	return e.origin
}

var _ SectionContentEvent = &BaseSectionContentEvent{}

// NewSectionContentEvent creates a new section content event
func NewSectionContentEvent(ctx context.Context, content SectionContent, origin agent.MessageEvent) *BaseSectionContentEvent {
	return &BaseSectionContentEvent{
		id:      agent.NewEventID(),
		ctx:     ctx,
		content: content,
		origin:  origin,
	}
}

// FinalArticleEvent carries the completed, edited article
type FinalArticleEvent interface {
	agent.MessageEvent
	Article() Document
	Origin() agent.Event
}

type BaseFinalArticleEvent struct {
	id      agent.EventID
	ctx     context.Context
	article Document
	origin  agent.Event
}

// ID implements FinalArticleEvent.
func (e *BaseFinalArticleEvent) ID() agent.EventID {
	return e.id
}

// Context implements FinalArticleEvent.
func (e *BaseFinalArticleEvent) Context() context.Context {
	return e.ctx
}

// WithContext implements FinalArticleEvent.
func (e *BaseFinalArticleEvent) WithContext(ctx context.Context) agent.Event {
	return &BaseFinalArticleEvent{
		id:      e.id,
		ctx:     ctx,
		article: e.article,
		origin:  e.origin,
	}
}

// ProgressPhase represents the current phase of article generation
type ProgressPhase string

const (
	PhaseInitializing ProgressPhase = "initializing"
	PhaseResearching  ProgressPhase = "researching" // NEW
	PhasePlanning     ProgressPhase = "planning"
	PhaseWriting      ProgressPhase = "writing"
	PhaseEditing      ProgressPhase = "editing"
	PhaseAttributing  ProgressPhase = "attributing" // NEW
	PhaseCompleted    ProgressPhase = "completed"
)

// ProgressEvent carries progress information during article generation
type ProgressEvent interface {
	agent.Event
	Phase() ProgressPhase
	Step() string
	Progress() float64 // 0.0 to 1.0
	ElapsedTime() time.Duration
	EstimatedTimeRemaining() time.Duration
	Details() map[string]interface{}
}

type BaseProgressEvent struct {
	id                     agent.EventID
	ctx                    context.Context
	phase                  ProgressPhase
	step                   string
	progress               float64
	elapsedTime            time.Duration
	estimatedTimeRemaining time.Duration
	details                map[string]interface{}
}

// ID implements ProgressEvent.
func (e *BaseProgressEvent) ID() agent.EventID {
	return e.id
}

// Context implements ProgressEvent.
func (e *BaseProgressEvent) Context() context.Context {
	return e.ctx
}

// WithContext implements ProgressEvent.
func (e *BaseProgressEvent) WithContext(ctx context.Context) agent.Event {
	return &BaseProgressEvent{
		id:                     e.id,
		ctx:                    ctx,
		phase:                  e.phase,
		step:                   e.step,
		progress:               e.progress,
		elapsedTime:            e.elapsedTime,
		estimatedTimeRemaining: e.estimatedTimeRemaining,
		details:                e.details,
	}
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
		id:                     agent.NewEventID(),
		ctx:                    ctx,
		phase:                  phase,
		step:                   step,
		progress:               progress,
		elapsedTime:            elapsedTime,
		estimatedTimeRemaining: estimatedTimeRemaining,
		details:                details,
	}
}

// Message implements FinalArticleEvent.
func (e *BaseFinalArticleEvent) Message() string {
	return "Article completed: " + e.article.Title
}

// Article implements FinalArticleEvent.
func (e *BaseFinalArticleEvent) Article() Document {
	return e.article
}

// Origin implements FinalArticleEvent.
func (e *BaseFinalArticleEvent) Origin() agent.Event {
	return e.origin
}

var _ FinalArticleEvent = &BaseFinalArticleEvent{}

// NewFinalArticleEvent creates a new final article event
func NewFinalArticleEvent(ctx context.Context, article Document, origin agent.Event) *BaseFinalArticleEvent {
	return &BaseFinalArticleEvent{
		id:      agent.NewEventID(),
		ctx:     ctx,
		article: article,
		origin:  origin,
	}
}
