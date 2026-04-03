package article

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/ghostwriter/pkg/scraper/surf"
	"github.com/bornholm/ghostwriter/pkg/search/duckduckgo"
	"github.com/bornholm/ghostwriter/pkg/tool"
	"github.com/pkg/errors"
)

// Orchestrator coordinates the multi-agent article writing process
type Orchestrator struct {
	researchHandler *ResearchAgent
	plannerHandler  *PlannerHandler
	writerHandler   *WriterHandler
	editorHandler   *EditorHandler
}

// OrchestratorOptions configures the orchestrator
type OrchestratorOptions struct {
	TargetWordCount   int
	ResearchDepth     ResearchDepth
	StyleGuidelines   string
	AdditionalContext string
	Tools             []llm.Tool
	KnowledgeBase     KnowledgeBase
	MaxReviewRounds   int
}

func NewOrchestratorOptions(optFuncs ...OrchestratorOptionFunc) *OrchestratorOptions {
	opts := &OrchestratorOptions{
		TargetWordCount: 1500,
		ResearchDepth:   ResearchDeep,
		Tools:           make([]llm.Tool, 0),
		MaxReviewRounds: 2,
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
func WithKnowledgeBase(kb KnowledgeBase) OrchestratorOptionFunc {
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

// WithMaxReviewRounds sets the maximum number of write→review rounds per section (minimum 1)
func WithMaxReviewRounds(n int) OrchestratorOptionFunc {
	return func(opts *OrchestratorOptions) {
		if n < 1 {
			n = 1
		}
		opts.MaxReviewRounds = n
	}
}

// WriteArticle orchestrates the complete article writing process
func (o *Orchestrator) WriteArticle(ctx context.Context, subject string, emit agent.EmitFunc, optFuncs ...OrchestratorOptionFunc) (Document, error) {
	opts := NewOrchestratorOptions(optFuncs...)

	if opts.StyleGuidelines != "" {
		ctx = WithContextStyleGuidelines(ctx, opts.StyleGuidelines)
	}

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

	// Step 1: Research
	tracker.EmitPhaseStart(PhaseResearching, "Starting comprehensive research", GetPhaseBaseProgress(PhaseResearching))
	if err := o.conductResearch(ctx, subject, opts.ResearchDepth, knowledgeBase, opts, emit); err != nil {
		return Document{}, errors.WithStack(err)
	}
	ctx = WithContextKnowledgeBase(ctx, knowledgeBase)
	ctx = WithContextResearchComplete(ctx, true)
	tracker.EmitPhaseComplete(PhaseResearching, "Research completed", GetPhaseBaseProgress(PhasePlanning))

	// Step 2: Plan
	tracker.EmitPhaseStart(PhasePlanning, "Starting document planning", GetPhaseBaseProgress(PhasePlanning))
	plan, err := o.generatePlan(ctx, subject, opts.TargetWordCount, opts, emit)
	if err != nil {
		return Document{}, errors.WithStack(err)
	}
	tracker.EmitPhaseComplete(PhasePlanning, "Document plan completed", GetPhaseBaseProgress(PhaseWriting))

	// Step 3: Write + review sections (ping-pong)
	tracker.EmitPhaseStart(PhaseWriting, "Starting section writing and review", GetPhaseBaseProgress(PhaseWriting))
	sections, err := o.writeSectionsWithReview(ctx, plan, subject, opts, emit)
	if err != nil {
		return Document{}, errors.WithStack(err)
	}
	tracker.EmitPhaseComplete(PhaseWriting, "All sections completed", GetPhaseBaseProgress(PhaseAttributing))

	article := assembleFinalArticle(plan.Title, plan, sections)

	tracker.EmitProgress(PhaseCompleted, "Article generation completed", 1.0, map[string]interface{}{
		"final_word_count": article.WordCount,
		"sections_count":   len(article.Sections),
	})

	// Step 5: Collect research sources
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
func (o *Orchestrator) conductResearch(ctx context.Context, subject string, depth ResearchDepth, kb KnowledgeBase, opts *OrchestratorOptions, emit agent.EmitFunc) error {
	researchCtx := WithContextAgentRole(ctx, RoleResearcher)
	researchCtx = WithContextSubject(researchCtx, subject)
	researchCtx = WithContextResearchDepth(researchCtx, depth)
	researchCtx = WithContextKnowledgeBase(researchCtx, kb)

	if opts.AdditionalContext != "" {
		researchCtx = WithContextAdditionalContext(researchCtx, opts.AdditionalContext)
	}

	return agent.NewRunner(o.researchHandler).Run(researchCtx, agent.NewInput(subject), emit)
}

// generatePlan uses the planner agent to create a document plan
func (o *Orchestrator) generatePlan(ctx context.Context, subject string, targetWordCount int, opts *OrchestratorOptions, emit agent.EmitFunc) (DocumentPlan, error) {
	planCtx := WithContextAgentRole(ctx, RolePlanner)
	planCtx = WithContextSubject(planCtx, subject)
	planCtx = WithContextTargetWordCount(planCtx, targetWordCount)

	if opts.StyleGuidelines != "" {
		planCtx = WithContextStyleGuidelines(planCtx, opts.StyleGuidelines)
	}
	if opts.AdditionalContext != "" {
		planCtx = WithContextAdditionalContext(planCtx, opts.AdditionalContext)
	}

	var planJSON string
	proxyEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			planJSON = evt.Data().(*agent.CompleteData).Message
		}
		return emit(evt)
	}

	if err := agent.NewRunner(o.plannerHandler).Run(planCtx, agent.NewInput(subject), proxyEmit); err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	var plan DocumentPlan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	return plan, nil
}

// writeSectionsWithReview coordinates writing and reviewing sections in a ping-pong loop.
// For each section: the writer writes, the reviewer validates or sends feedback,
// the writer revises — up to opts.MaxReviewRounds rounds.
func (o *Orchestrator) writeSectionsWithReview(ctx context.Context, plan DocumentPlan, subject string, opts *OrchestratorOptions, emit agent.EmitFunc) ([]SectionContent, error) {
	sections := make([]SectionContent, len(plan.Sections))
	tracker := NewProgressTracker(ctx)
	totalSections := len(plan.Sections)

	// draft holds the markdown of already-approved sections (for redundancy checking)
	var draft strings.Builder
	var previousSectionContent *SectionContent

	for index, section := range plan.Sections {
		select {
		case <-ctx.Done():
			return nil, errors.WithStack(ctx.Err())
		default:
		}

		sectionProgress := float64(index) / float64(totalSections)
		tracker.EmitSubProgress(PhaseWriting, fmt.Sprintf("Writing section %d/%d: %s", index+1, totalSections, section.Title),
			GetPhaseBaseProgress(PhaseWriting), sectionProgress, WritingWeight, map[string]interface{}{
				"section_id":    section.ID,
				"section_title": section.Title,
			})

		// Find the matching planned section for key points / word count
		var plannedSection *DocumentSection
		for _, ps := range plan.Sections {
			if ps.Title == section.Title {
				plannedSection = ps
				break
			}
		}

		var approved SectionContent
		var feedback string

		for round := 0; round < opts.MaxReviewRounds; round++ {
			var content SectionContent
			var err error

			if round == 0 {
				content, err = o.writeSection(ctx, *section, subject, opts.ResearchDepth, index, previousSectionContent, emit)
			} else {
				content, err = o.writerHandler.reviseSection(ctx, *section, subject, previousSectionContent, approved.Content, feedback, round, emit)
			}
			if err != nil {
				return nil, errors.WithStack(err)
			}
			approved = content

			// Review the section
			tracker.EmitSubProgress(PhaseReviewing, fmt.Sprintf("Reviewing section (round %d/%d): %s", round+1, opts.MaxReviewRounds, section.Title),
				GetPhaseBaseProgress(PhaseWriting), sectionProgress, WritingWeight, map[string]interface{}{
					"section_id":    section.ID,
					"section_title": section.Title,
					"round":         round + 1,
				})

			reviewCtx := WithContextAgentRole(ctx, RoleEditor)
			reviewCtx = WithContextDocumentDraft(reviewCtx, draft.String())
			review, err := o.editorHandler.reviewSection(reviewCtx, content, plannedSection, subject, draft.String(), emit)
			if err != nil {
				return nil, errors.WithStack(err)
			}

			if review.Approved {
				approved = SectionContent{
					Title:     section.Title,
					Content:   review.Content,
					WordCount: o.writerHandler.countWords(review.Content),
				}
				break
			}

			feedback = review.Feedback
		}

		// Update the draft with the approved section
		draft.WriteString(fmt.Sprintf("## %s\n\n", approved.Title))
		draft.WriteString(approved.Content)
		draft.WriteString("\n\n")

		sections[index] = approved
		previousSectionContent = &sections[index]

		tracker.EmitSubProgress(PhaseWriting, fmt.Sprintf("Section validated: %s (%d words)", approved.Title, approved.WordCount),
			GetPhaseBaseProgress(PhaseWriting), float64(index+1)/float64(totalSections), WritingWeight, map[string]interface{}{
				"section_title":     approved.Title,
				"actual_word_count": approved.WordCount,
			})
	}

	return sections, nil
}

// writeSection assigns a section to a writer agent
func (o *Orchestrator) writeSection(ctx context.Context, section DocumentSection, subject string, depth ResearchDepth, _ int, previousSectionContent *SectionContent, emit agent.EmitFunc) (SectionContent, error) {
	writeCtx := WithContextAgentRole(ctx, RoleWriter)
	writeCtx = WithContextSubject(writeCtx, subject)
	writeCtx = WithContextResearchDepth(writeCtx, depth)
	writeCtx = WithContextDocumentSection(writeCtx, section)
	writeCtx = WithContextPreviousSectionContent(writeCtx, previousSectionContent)

	styleGuidelines := ContextStyleGuidelines(ctx, "")
	if styleGuidelines != "" {
		writeCtx = WithContextStyleGuidelines(writeCtx, styleGuidelines)
	}

	additionalContext := ContextAdditionalContext(ctx, "")
	if additionalContext != "" {
		writeCtx = WithContextAdditionalContext(writeCtx, additionalContext)
	}

	var contentJSON string
	proxyEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			contentJSON = evt.Data().(*agent.CompleteData).Message
		}
		return emit(evt)
	}

	if err := agent.NewRunner(o.writerHandler).Run(writeCtx, agent.NewInput(section.Title), proxyEmit); err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	var content SectionContent
	if err := json.Unmarshal([]byte(contentJSON), &content); err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	return content, nil
}

// NewOrchestrator creates a new article writing orchestrator
func NewOrchestrator(client llm.Client, tools ...llm.Tool) *Orchestrator {
	scraper := surf.NewScraper()
	scraperTool := tool.NewScrapeWebpageTool(scraper)

	researchHandler := NewResearchAgent(client, duckduckgo.NewClient(scraper), scraper)

	plannerTools := append(tools, scraperTool)
	plannerHandler := NewPlannerHandler(client, plannerTools...)

	writerTools := append(tools, scraperTool)
	writerHandler := NewWriterHandler(client, writerTools...)

	editorHandler := NewEditorHandler(client)

	return &Orchestrator{
		researchHandler: researchHandler,
		plannerHandler:  plannerHandler,
		writerHandler:   writerHandler,
		editorHandler:   editorHandler,
	}
}

// WriteArticle is a convenience function to create an orchestrator and write an article
func WriteArticle(ctx context.Context, client llm.Client, subject string, emit agent.EmitFunc, optFuncs ...OrchestratorOptionFunc) (Document, error) {
	opts := NewOrchestratorOptions(optFuncs...)
	orchestrator := NewOrchestrator(client, opts.Tools...)
	return orchestrator.WriteArticle(ctx, subject, emit, optFuncs...)
}
