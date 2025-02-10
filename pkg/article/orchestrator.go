package article

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/ghostwriter/pkg/tool"
	"github.com/pkg/errors"
)

// Orchestrator coordinates the multi-agent article writing process
type Orchestrator struct {
	plannerAgent *agent.Agent
	writerAgents []*agent.Agent
	editorAgent  *agent.Agent

	// Configuration
	maxConcurrentWriters int
	timeout              time.Duration
}

// OrchestratorOptions configures the orchestrator
type OrchestratorOptions struct {
	MaxConcurrentWriters int
	Timeout              time.Duration
	TargetWordCount      int
	ResearchDepth        ResearchDepth
}

// OrchestratorOptionFunc is a function that configures orchestrator options
type OrchestratorOptionFunc func(*OrchestratorOptions)

// WithMaxConcurrentWriters sets the maximum number of concurrent writers
func WithMaxConcurrentWriters(max int) OrchestratorOptionFunc {
	return func(opts *OrchestratorOptions) {
		opts.MaxConcurrentWriters = max
	}
}

// WithTimeout sets the overall timeout for article generation
func WithTimeout(timeout time.Duration) OrchestratorOptionFunc {
	return func(opts *OrchestratorOptions) {
		opts.Timeout = timeout
	}
}

// WithTargetWordCount sets the target word count for the article
func WithTargetWordCount(wordCount int) OrchestratorOptionFunc {
	return func(opts *OrchestratorOptions) {
		opts.TargetWordCount = wordCount
	}
}

// WithResearchDepth sets the research depth for writers
func WithResearchDepth(depth ResearchDepth) OrchestratorOptionFunc {
	return func(opts *OrchestratorOptions) {
		opts.ResearchDepth = depth
	}
}

// WriteArticle orchestrates the complete article writing process
func (o *Orchestrator) WriteArticle(ctx context.Context, subject string, optFuncs ...OrchestratorOptionFunc) (ArticleDocument, error) {
	// Apply options
	opts := &OrchestratorOptions{
		MaxConcurrentWriters: 3,
		Timeout:              30 * time.Minute,
		TargetWordCount:      1500,
		ResearchDepth:        ResearchDeep,
	}
	for _, fn := range optFuncs {
		fn(opts)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)
	tracker.EmitProgress(PhaseInitializing, "Starting article generation", 0.0, map[string]interface{}{
		"subject":           subject,
		"target_word_count": opts.TargetWordCount,
		"max_writers":       opts.MaxConcurrentWriters,
	})

	// Step 1: Generate document plan
	tracker.EmitPhaseStart(PhasePlanning, "Starting document planning and research", GetPhaseBaseProgress(PhasePlanning))
	plan, err := o.generatePlan(ctx, subject, opts.TargetWordCount)
	if err != nil {
		return ArticleDocument{}, errors.WithStack(err)
	}
	tracker.EmitPhaseComplete(PhasePlanning, "Document plan completed", GetPhaseBaseProgress(PhaseWriting))

	// Step 2: Write sections concurrently
	tracker.EmitPhaseStart(PhaseWriting, "Starting section writing", GetPhaseBaseProgress(PhaseWriting))
	sections, err := o.writeSections(ctx, plan, subject, opts)
	if err != nil {
		return ArticleDocument{}, errors.WithStack(err)
	}
	tracker.EmitPhaseComplete(PhaseWriting, "All sections completed", GetPhaseBaseProgress(PhaseEditing))

	// Step 3: Edit and finalize article
	tracker.EmitPhaseStart(PhaseEditing, "Starting article editing and finalization", GetPhaseBaseProgress(PhaseEditing))
	article, err := o.editArticle(ctx, plan, sections, subject)
	if err != nil {
		return ArticleDocument{}, errors.WithStack(err)
	}
	tracker.EmitProgress(PhaseCompleted, "Article generation completed", 1.0, map[string]interface{}{
		"final_word_count": article.WordCount,
		"sections_count":   len(article.Sections),
		"sources_count":    len(article.Sources),
	})

	return article, nil
}

// generatePlan uses the planner agent to create a document plan
func (o *Orchestrator) generatePlan(ctx context.Context, subject string, targetWordCount int) (DocumentPlan, error) {
	// Create planning context
	planCtx := WithContextAgentRole(ctx, RolePlanner)
	planCtx = WithContextSubject(planCtx, subject)
	planCtx = WithContextTargetWordCount(planCtx, targetWordCount)

	// Send planning request
	planRequest := agent.NewMessageEvent(planCtx, subject)
	if err := o.plannerAgent.In(planRequest); err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	// Wait for plan result
	select {
	case evt := <-o.plannerAgent.Output():
		if planEvent, ok := evt.(DocumentPlanEvent); ok {
			if planEvent.Origin().ID() == planRequest.ID() {
				return planEvent.Plan(), nil
			}
		}
		return DocumentPlan{}, errors.New("unexpected event type from planner")

	case err := <-o.plannerAgent.Err():
		return DocumentPlan{}, errors.WithStack(err)

	case <-ctx.Done():
		return DocumentPlan{}, errors.WithStack(ctx.Err())
	}
}

// writeSections coordinates multiple writer agents to write sections
func (o *Orchestrator) writeSections(ctx context.Context, plan DocumentPlan, subject string, opts *OrchestratorOptions) ([]SectionContent, error) {
	sections := make([]SectionContent, len(plan.Sections))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var writeErr error
	var completedSections int

	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)
	totalSections := len(plan.Sections)

	// Create a semaphore to limit concurrent writers
	semaphore := make(chan struct{}, opts.MaxConcurrentWriters)

	for i, section := range plan.Sections {
		wg.Add(1)
		go func(index int, sec DocumentSection) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check if context is cancelled
			select {
			case <-ctx.Done():
				mu.Lock()
				if writeErr == nil {
					writeErr = ctx.Err()
				}
				mu.Unlock()
				return
			default:
			}

			// Emit progress for section start
			tracker.EmitSubProgress(PhaseWriting, fmt.Sprintf("Writing section: %s", sec.Title),
				GetPhaseBaseProgress(PhaseWriting), 0.0, WritingWeight, map[string]interface{}{
					"section_id":    sec.ID,
					"section_title": sec.Title,
					"word_count":    sec.WordCount,
				})

			// Write the section
			content, err := o.writeSection(ctx, sec, subject, opts.ResearchDepth, index)
			if err != nil {
				mu.Lock()
				if writeErr == nil {
					writeErr = err
				}
				mu.Unlock()
				return
			}

			mu.Lock()
			sections[index] = content
			completedSections++
			currentProgress := float64(completedSections) / float64(totalSections)
			mu.Unlock()

			// Emit progress for section completion
			tracker.EmitSubProgress(PhaseWriting, fmt.Sprintf("Completed section: %s (%d words)", content.Title, content.WordCount),
				GetPhaseBaseProgress(PhaseWriting), currentProgress, WritingWeight, map[string]interface{}{
					"section_id":         content.SectionID,
					"section_title":      content.Title,
					"actual_word_count":  content.WordCount,
					"completed_sections": completedSections,
					"total_sections":     totalSections,
					"written_by":         content.WrittenBy,
				})
		}(i, section)
	}

	wg.Wait()

	if writeErr != nil {
		return nil, errors.WithStack(writeErr)
	}

	return sections, nil
}

// writeSection assigns a section to a writer agent
func (o *Orchestrator) writeSection(ctx context.Context, section DocumentSection, subject string, depth ResearchDepth, writerIndex int) (SectionContent, error) {
	// Select a writer agent (round-robin)
	writerAgent := o.writerAgents[writerIndex%len(o.writerAgents)]

	// Create writing context
	writeCtx := WithContextAgentRole(ctx, RoleWriter)
	writeCtx = WithContextSubject(writeCtx, subject)
	writeCtx = WithContextResearchDepth(writeCtx, depth)
	writeCtx = WithContextWriterID(writeCtx, fmt.Sprintf("writer_%d", writerIndex))

	// Create section assignment
	assignment := NewSectionAssignmentEvent(writeCtx, section, subject, nil)

	// Send to writer
	if err := writerAgent.In(assignment); err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	// Wait for result
	select {
	case evt := <-writerAgent.Output():
		if contentEvent, ok := evt.(SectionContentEvent); ok {
			return contentEvent.Content(), nil
		}
		return SectionContent{}, errors.New("unexpected event type from writer")

	case err := <-writerAgent.Err():
		return SectionContent{}, errors.WithStack(err)

	case <-ctx.Done():
		return SectionContent{}, errors.WithStack(ctx.Err())
	}
}

// editArticle uses the editor agent to finalize the article
func (o *Orchestrator) editArticle(ctx context.Context, plan DocumentPlan, sections []SectionContent, subject string) (ArticleDocument, error) {
	// Create editing context
	editCtx := WithContextAgentRole(ctx, RoleEditor)

	// Create edit request
	editRequest := NewEditRequestEvent(editCtx, plan.Title, subject, sections, plan)

	// Alternative: Use the editor agent (commented out for now)

	if err := o.editorAgent.In(editRequest); err != nil {
		return ArticleDocument{}, errors.WithStack(err)
	}

	// Wait for result
	select {
	case evt := <-o.editorAgent.Output():
		if articleEvent, ok := evt.(FinalArticleEvent); ok {
			return articleEvent.Article(), nil
		}

		return ArticleDocument{}, errors.New("unexpected event type from editor")

	case err := <-o.editorAgent.Err():
		return ArticleDocument{}, errors.WithStack(err)

	case <-ctx.Done():
		return ArticleDocument{}, errors.WithStack(ctx.Err())
	}
}

// Start starts all the agents
func (o *Orchestrator) Start(ctx context.Context) error {
	// Start planner agent
	if _, _, err := o.plannerAgent.Start(ctx); err != nil {
		return errors.WithStack(err)
	}

	// Start writer agents
	for i, writerAgent := range o.writerAgents {
		if _, _, err := writerAgent.Start(ctx); err != nil {
			return errors.Wrapf(err, "failed to start writer agent %d", i)
		}
	}

	// Start editor agent
	if _, _, err := o.editorAgent.Start(ctx); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

// Stop stops all the agents
func (o *Orchestrator) Stop() error {
	var errs []error

	// Stop planner
	if err := o.plannerAgent.Stop(); err != nil {
		errs = append(errs, errors.Wrap(err, "failed to stop planner agent"))
	}

	// Stop writers
	for i, writerAgent := range o.writerAgents {
		if err := writerAgent.Stop(); err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to stop writer agent %d", i))
		}
	}

	// Stop editor
	if err := o.editorAgent.Stop(); err != nil {
		errs = append(errs, errors.Wrap(err, "failed to stop editor agent"))
	}

	if len(errs) > 0 {
		return errors.Errorf("errors stopping agents: %v", errs)
	}

	return nil
}

// NewOrchestrator creates a new article writing orchestrator
func NewOrchestrator(client llm.ChatCompletionClient, tools []llm.Tool) *Orchestrator {
	// Create specialized agents with research tools
	plannerHandler := NewPlannerHandler(client, tools...)
	plannerAgent := agent.New(plannerHandler)

	// Create multiple writer agents for concurrent processing
	writerAgents := make([]*agent.Agent, 3)
	for i := 0; i < 3; i++ {
		writerHandler := NewWriterHandler(client, tools...)
		writerAgents[i] = agent.New(writerHandler)
	}

	// Create editor agent
	editorHandler := NewEditorHandler(client)
	editorAgent := agent.New(editorHandler)

	return &Orchestrator{
		plannerAgent:         plannerAgent,
		writerAgents:         writerAgents,
		editorAgent:          editorAgent,
		maxConcurrentWriters: 3,
		timeout:              30 * time.Minute,
	}
}

// WriteArticle is a convenience function to create an orchestrator and write an article
func WriteArticle(ctx context.Context, client llm.ChatCompletionClient, subject string, optFuncs ...OrchestratorOptionFunc) (ArticleDocument, error) {
	// Create orchestrator with default research tools
	orchestrator := NewOrchestrator(client, tool.GetDefaultResearchTools())

	// Start the orchestrator
	if err := orchestrator.Start(ctx); err != nil {
		return ArticleDocument{}, errors.WithStack(err)
	}
	defer orchestrator.Stop()

	// Write the article
	return orchestrator.WriteArticle(ctx, subject, optFuncs...)
}
