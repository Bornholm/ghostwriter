package article

import (
	"context"
	"fmt"
	"slices"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/ghostwriter/pkg/scraper/surf"
	"github.com/bornholm/ghostwriter/pkg/search/duckduckgo"
	"github.com/bornholm/ghostwriter/pkg/tool"
	"github.com/pkg/errors"
)

// Orchestrator coordinates the multi-agent article writing process
type Orchestrator struct {
	researchAgent *agent.Agent
	plannerAgent  *agent.Agent
	writerAgent   *agent.Agent
	editorAgent   *agent.Agent
}

// OrchestratorOptions configures the orchestrator
type OrchestratorOptions struct {
	TargetWordCount   int
	ResearchDepth     ResearchDepth
	StyleGuidelines   string
	AdditionalContext string
	Tools             []llm.Tool
	KnowledgeBase     *KnowledgeBase
}

func NewOrchestratorOptions(optFuncs ...OrchestratorOptionFunc) *OrchestratorOptions {
	// Apply options
	opts := &OrchestratorOptions{
		TargetWordCount: 1500,
		ResearchDepth:   ResearchDeep,
		Tools:           make([]llm.Tool, 0),
	}
	for _, fn := range optFuncs {
		fn(opts)
	}
	return opts
}

// OrchestratorOptionFunc is a function that configures orchestrator options
type OrchestratorOptionFunc func(*OrchestratorOptions)

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

// WithStyleGuidelines sets the style guidelines for all agents
func WithStyleGuidelines(styleGuidelines string) OrchestratorOptionFunc {
	return func(opts *OrchestratorOptions) {
		opts.StyleGuidelines = styleGuidelines
	}
}

// WithTools sets the tools for all agents
func WithTools(tools []llm.Tool) OrchestratorOptionFunc {
	return func(opts *OrchestratorOptions) {
		opts.Tools = tools
	}
}

// WithKnowledgeBase sets knowledge base for all agents
func WithKnowledgeBase(kb *KnowledgeBase) OrchestratorOptionFunc {
	return func(opts *OrchestratorOptions) {
		opts.KnowledgeBase = kb
	}
}

// WithAdditionalContext sets additional context information for all agents
func WithAdditionalContext(additionalContext string) OrchestratorOptionFunc {
	return func(opts *OrchestratorOptions) {
		opts.AdditionalContext = additionalContext
	}
}

// WriteArticle orchestrates the complete article writing process
func (o *Orchestrator) WriteArticle(ctx context.Context, subject string, optFuncs ...OrchestratorOptionFunc) (Document, error) {
	opts := NewOrchestratorOptions(optFuncs...)

	// Add style guidelines to context if provided
	if opts.StyleGuidelines != "" {
		ctx = WithContextStyleGuidelines(ctx, opts.StyleGuidelines)
	}

	// Add additional context to context if provided
	if opts.AdditionalContext != "" {
		ctx = WithContextAdditionalContext(ctx, opts.AdditionalContext)
	}

	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)
	tracker.EmitProgress(PhaseInitializing, "Starting article generation", 0.0, map[string]interface{}{
		"subject":           subject,
		"target_word_count": opts.TargetWordCount,
	})

	knowledgeBase := opts.KnowledgeBase
	if knowledgeBase == nil {
		kb, err := NewKnowledgeBase()
		if err != nil {
			return Document{}, errors.WithStack(err)
		}

		knowledgeBase = kb
	}

	// Step 1: Conduct comprehensive research and build knowledge base
	tracker.EmitPhaseStart(PhaseResearching, "Starting comprehensive research", GetPhaseBaseProgress(PhaseResearching))
	if err := o.conductResearch(ctx, subject, opts.ResearchDepth, knowledgeBase); err != nil {
		return Document{}, errors.WithStack(err)
	}
	// Add knowledge base to context for subsequent phases
	ctx = WithContextKnowledgeBase(ctx, knowledgeBase)
	ctx = WithContextResearchComplete(ctx, true)
	tracker.EmitPhaseComplete(PhaseResearching, "Research completed", GetPhaseBaseProgress(PhasePlanning))

	// Step 2: Generate document plan using knowledge base
	tracker.EmitPhaseStart(PhasePlanning, "Starting document planning", GetPhaseBaseProgress(PhasePlanning))
	plan, err := o.generatePlan(ctx, subject, opts.TargetWordCount, opts)
	if err != nil {
		return Document{}, errors.WithStack(err)
	}
	tracker.EmitPhaseComplete(PhasePlanning, "Document plan completed", GetPhaseBaseProgress(PhaseWriting))

	// Step 3: Write sections using knowledge base
	tracker.EmitPhaseStart(PhaseWriting, "Starting section writing", GetPhaseBaseProgress(PhaseWriting))
	sections, err := o.writeSections(ctx, plan, subject, opts)
	if err != nil {
		return Document{}, errors.WithStack(err)
	}
	tracker.EmitPhaseComplete(PhaseWriting, "All sections completed", GetPhaseBaseProgress(PhaseEditing))

	// Step 4: Edit and finalize article
	tracker.EmitPhaseStart(PhaseEditing, "Starting article editing and finalization", GetPhaseBaseProgress(PhaseEditing))
	article, err := o.editArticle(ctx, plan, sections, subject)
	if err != nil {
		return Document{}, errors.WithStack(err)
	}
	tracker.EmitPhaseComplete(PhaseEditing, "Article editing completed", GetPhaseBaseProgress(PhaseAttributing))

	tracker.EmitProgress(PhaseCompleted, "Article generation completed", 1.0, map[string]interface{}{
		"final_word_count": article.WordCount,
		"sections_count":   len(article.Sections),
	})

	// Step 5: Collecte research documents
	documents := knowledgeBase.GetAllDocuments()
	for _, d := range documents {
		article.Sources = append(article.Sources, Source{
			URL:        d.URL,
			Title:      d.Title,
			Keywords:   d.Keywords,
			SourceType: d.SourceType,
			Relevance:  d.Relevance,
		})
	}

	slices.SortFunc(article.Sources, func(a Source, b Source) int {
		if a.Relevance > b.Relevance {
			return -1
		}
		if a.Relevance < b.Relevance {
			return 1
		}
		return 0
	})

	return article, nil
}

// conductResearch uses the research agent to build knowledge base
func (o *Orchestrator) conductResearch(ctx context.Context, subject string, depth ResearchDepth, kb *KnowledgeBase) error {
	// Create research context
	researchCtx := WithContextAgentRole(ctx, RoleResearcher)
	researchCtx = WithContextSubject(researchCtx, subject)
	researchCtx = WithContextResearchDepth(researchCtx, depth)

	// Send research request
	researchRequest := NewResearchRequestEvent(researchCtx, subject, depth, kb)
	if err := o.researchAgent.In(researchRequest); err != nil {
		return errors.WithStack(err)
	}

	// Wait for research result
	select {
	case evt := <-o.researchAgent.Output():
		if _, ok := evt.(*ResearchCompleteEvent); ok {
			// Research events don't need to match request ID since they're generated internally
			return nil
		}
		return errors.New("unexpected event type from researcher")

	case err := <-o.researchAgent.Err():
		return errors.WithStack(err)

	case <-ctx.Done():
		return errors.WithStack(ctx.Err())
	}
}

// generatePlan uses the planner agent to create a document plan
func (o *Orchestrator) generatePlan(ctx context.Context, subject string, targetWordCount int, opts *OrchestratorOptions) (DocumentPlan, error) {
	// Create planning context
	planCtx := WithContextAgentRole(ctx, RolePlanner)
	planCtx = WithContextSubject(planCtx, subject)
	planCtx = WithContextTargetWordCount(planCtx, targetWordCount)

	// Add style guidelines if provided
	if opts.StyleGuidelines != "" {
		planCtx = WithContextStyleGuidelines(planCtx, opts.StyleGuidelines)
	}

	// Add additional context if provided
	if opts.AdditionalContext != "" {
		planCtx = WithContextAdditionalContext(planCtx, opts.AdditionalContext)
	}

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
	var completedSections int

	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)
	totalSections := len(plan.Sections)

	var previousSectionContent *SectionContent

	for index, section := range plan.Sections {
		select {
		case <-ctx.Done():
			return nil, errors.WithStack(ctx.Err())
		default:
		}

		// Emit progress for section start
		tracker.EmitSubProgress(PhaseWriting, fmt.Sprintf("Writing section: %s", section.Title),
			GetPhaseBaseProgress(PhaseWriting), 0.0, WritingWeight, map[string]interface{}{
				"section_id":    section.ID,
				"section_title": section.Title,
				"word_count":    section.WordCount,
			})

		// Write the section
		content, err := o.writeSection(ctx, *section, subject, opts.ResearchDepth, index, previousSectionContent)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		previousSectionContent = &content

		sections[index] = content
		completedSections++
		currentProgress := float64(completedSections) / float64(totalSections)

		// Emit progress for section completion
		tracker.EmitSubProgress(PhaseWriting, fmt.Sprintf("Completed section: %s (%d words)", content.Title, content.WordCount),
			GetPhaseBaseProgress(PhaseWriting), currentProgress, WritingWeight, map[string]interface{}{
				"section_title":      content.Title,
				"actual_word_count":  content.WordCount,
				"completed_sections": completedSections,
				"total_sections":     totalSections,
			})
	}

	return sections, nil
}

// writeSection assigns a section to a writer agent
func (o *Orchestrator) writeSection(ctx context.Context, section DocumentSection, subject string, depth ResearchDepth, writerIndex int, previousSectionContent *SectionContent) (SectionContent, error) {
	// Select a writer agent (round-robin)
	writerAgent := o.writerAgent

	// Create writing context
	writeCtx := WithContextAgentRole(ctx, RoleWriter)
	writeCtx = WithContextSubject(writeCtx, subject)
	writeCtx = WithContextResearchDepth(writeCtx, depth)

	// Add style guidelines if available from parent context
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	if styleGuidelines != "" {
		writeCtx = WithContextStyleGuidelines(writeCtx, styleGuidelines)
	}

	// Add additional context if available from parent context
	additionalContext := ContextAdditionalContext(ctx, "")
	if additionalContext != "" {
		writeCtx = WithContextAdditionalContext(writeCtx, additionalContext)
	}

	// Create section assignment
	assignment := NewSectionAssignmentEvent(writeCtx, section, subject, nil, previousSectionContent)

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
func (o *Orchestrator) editArticle(ctx context.Context, plan DocumentPlan, sections []SectionContent, subject string) (Document, error) {
	// Create editing context
	editCtx := WithContextAgentRole(ctx, RoleEditor)

	// Add style guidelines if available from parent context
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	if styleGuidelines != "" {
		editCtx = WithContextStyleGuidelines(editCtx, styleGuidelines)
	}

	// Add additional context if available from parent context
	additionalContext := ContextAdditionalContext(ctx, "")
	if additionalContext != "" {
		editCtx = WithContextAdditionalContext(editCtx, additionalContext)
	}

	// Create edit request
	editRequest := NewEditRequestEvent(editCtx, plan.Title, subject, sections, plan)

	if err := o.editorAgent.In(editRequest); err != nil {
		return Document{}, errors.WithStack(err)
	}

	// Wait for result
	select {
	case evt := <-o.editorAgent.Output():
		if articleEvent, ok := evt.(FinalArticleEvent); ok {
			return articleEvent.Article(), nil
		}

		return Document{}, errors.New("unexpected event type from editor")

	case err := <-o.editorAgent.Err():
		return Document{}, errors.WithStack(err)

	case <-ctx.Done():
		return Document{}, errors.WithStack(ctx.Err())
	}
}

// Start starts all the agents
func (o *Orchestrator) Start(ctx context.Context) error {
	// Start research agent
	if _, _, err := o.researchAgent.Start(ctx); err != nil {
		return errors.WithStack(err)
	}

	// Start planner agent
	if _, _, err := o.plannerAgent.Start(ctx); err != nil {
		return errors.WithStack(err)
	}

	// Start writer agent
	if _, _, err := o.writerAgent.Start(ctx); err != nil {
		return errors.WithStack(err)
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

	// Stop research agent
	if err := o.researchAgent.Stop(); err != nil {
		errs = append(errs, errors.Wrap(err, "failed to stop research agent"))
	}

	// Stop planner
	if err := o.plannerAgent.Stop(); err != nil {
		errs = append(errs, errors.Wrap(err, "failed to stop planner agent"))
	}

	// Stop writer
	if err := o.writerAgent.Stop(); err != nil {
		errs = append(errs, errors.Wrap(err, "failed to stop writer agent"))
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
func NewOrchestrator(client llm.ChatCompletionClient, tools ...llm.Tool) *Orchestrator {
	scraper := surf.NewScraper()
	scraperTool := tool.NewScrapeWebpageTool(scraper)

	researchHandler := NewResearchAgent(client, duckduckgo.NewClient(scraper), scraper)
	researchAgent := agent.New(researchHandler)

	plannerTools := append(tools, scraperTool)
	plannerHandler := NewPlannerHandler(client, plannerTools...)
	plannerAgent := agent.New(plannerHandler)

	writerTools := append(tools, scraperTool)
	writerHandler := NewWriterHandler(client, writerTools...)
	writerAgent := agent.New(writerHandler)

	// Create editor agent
	editorHandler := NewEditorHandler(client)
	editorAgent := agent.New(editorHandler)

	return &Orchestrator{
		researchAgent: researchAgent,
		plannerAgent:  plannerAgent,
		writerAgent:   writerAgent,
		editorAgent:   editorAgent,
	}
}

// WriteArticle is a convenience function to create an orchestrator and write an article
func WriteArticle(ctx context.Context, client llm.ChatCompletionClient, subject string, optFuncs ...OrchestratorOptionFunc) (Document, error) {
	opts := NewOrchestratorOptions(optFuncs...)

	// Create orchestrator with default research tools
	orchestrator := NewOrchestrator(client, opts.Tools...)

	// Start the orchestrator
	if err := orchestrator.Start(ctx); err != nil {
		return Document{}, errors.WithStack(err)
	}
	defer orchestrator.Stop()

	// Write the article
	return orchestrator.WriteArticle(ctx, subject, optFuncs...)
}
