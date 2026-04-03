package whitepaper

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/ghostwriter/pkg/article"
	"github.com/bornholm/ghostwriter/pkg/scraper/surf"
	"github.com/bornholm/ghostwriter/pkg/search/duckduckgo"
	"github.com/pkg/errors"
)


// OrchestratorOptions configures the white paper pipeline.
type OrchestratorOptions struct {
	TargetWordCount   int
	ResearchDepth     article.ResearchDepth
	StyleGuidelines   string
	AdditionalContext string
	OutputDir         string
	RenderHTML        string // output path for HTML, empty = skip
	RenderPDF         string // output path for PDF, empty = skip
	ChromiumPath      string
	NoSandbox         bool
	KnowledgeBase     article.KnowledgeBase
	Tools             []llm.Tool
	MaxReviewRounds   int
}

// OrchestratorOptionFunc configures OrchestratorOptions.
type OrchestratorOptionFunc func(*OrchestratorOptions)

func NewOrchestratorOptions(fns ...OrchestratorOptionFunc) *OrchestratorOptions {
	opts := &OrchestratorOptions{
		TargetWordCount: 10000,
		ResearchDepth:   article.ResearchDeep,
		Tools:           make([]llm.Tool, 0),
		MaxReviewRounds: 2,
	}
	for _, fn := range fns {
		fn(opts)
	}
	return opts
}

func WithTargetWordCount(n int) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.TargetWordCount = n }
}

func WithResearchDepth(d article.ResearchDepth) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.ResearchDepth = d }
}

func WithStyleGuidelines(s string) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.StyleGuidelines = s }
}

func WithAdditionalContext(s string) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.AdditionalContext = s }
}

func WithOutputDir(dir string) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.OutputDir = dir }
}

func WithRenderHTML(path string) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.RenderHTML = path }
}

func WithRenderPDF(path string) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.RenderPDF = path }
}

func WithChromiumPath(path string) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.ChromiumPath = path }
}

func WithNoSandbox(v bool) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.NoSandbox = v }
}

func WithKnowledgeBase(kb article.KnowledgeBase) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.KnowledgeBase = kb }
}

func WithTools(tools []llm.Tool) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) { o.Tools = tools }
}

// WithMaxReviewRounds sets the maximum number of write→review rounds per chapter (minimum 1).
func WithMaxReviewRounds(n int) OrchestratorOptionFunc {
	return func(o *OrchestratorOptions) {
		if n < 1 {
			n = 1
		}
		o.MaxReviewRounds = n
	}
}

// Orchestrator coordinates the white paper writing pipeline.
type Orchestrator struct {
	researcher     *article.ResearchAgent
	planner        *PlannerHandler
	chapterWriter  *ChapterWriterHandler
	chapterEditor  *ChapterEditorHandler
	coherenceEditor *CoherenceEditorHandler
}

// WriteWhitePaper orchestrates the full pipeline.
func (o *Orchestrator) WriteWhitePaper(ctx context.Context, subject string, emit agent.EmitFunc, optFuncs ...OrchestratorOptionFunc) (WhitePaper, error) {
	opts := NewOrchestratorOptions(optFuncs...)

	kb := opts.KnowledgeBase
	if kb == nil {
		var err error
		kb, err = article.NewKnowledgeBase()
		if err != nil {
			return WhitePaper{}, errors.WithStack(err)
		}
	}

	// Build base context
	ctx = withCtxSubject(ctx, subject)
	ctx = withCtxTargetWordCount(ctx, opts.TargetWordCount)
	ctx = withCtxResearchDepth(ctx, opts.ResearchDepth)
	if opts.StyleGuidelines != "" {
		ctx = withCtxStyleGuidelines(ctx, opts.StyleGuidelines)
	}
	if opts.AdditionalContext != "" {
		ctx = withCtxAdditionalContext(ctx, opts.AdditionalContext)
	}
	ctx = withCtxKnowledgeBase(ctx, kb)

	searcher := NewKnowledgeBaseAdapter(kb)
	ctx = withCtxSearcher(ctx, searcher)

	// Step 1: Research
	if err := o.conductResearch(ctx, subject, opts.ResearchDepth, kb, emit); err != nil {
		return WhitePaper{}, errors.Wrap(err, "research phase failed")
	}

	// Step 2: Plan
	plan, err := o.generatePlan(ctx, subject, opts.TargetWordCount, emit)
	if err != nil {
		return WhitePaper{}, errors.Wrap(err, "planning phase failed")
	}
	ctx = withCtxPlan(ctx, plan)

	// Step 3: Write + edit each chapter
	chapters, err := o.writeAndEditChapters(ctx, plan, opts.MaxReviewRounds, emit)
	if err != nil {
		return WhitePaper{}, errors.Wrap(err, "writing phase failed")
	}

	// Step 4: Coherence pass
	coherence, err := o.coherencePass(ctx, plan, chapters, emit)
	if err != nil {
		return WhitePaper{}, errors.Wrap(err, "coherence phase failed")
	}

	// Step 5: Collect sources
	sources := extractSources(ctx)
	slices.SortFunc(sources, func(a, b article.Source) int {
		if a.Relevance > b.Relevance {
			return -1
		}
		if a.Relevance < b.Relevance {
			return 1
		}
		return 0
	})
	// Merge sources into bibliography if coherence didn't produce any
	if len(coherence.Bibliography) == 0 {
		for _, s := range sources {
			if s.URL != "" {
				coherence.Bibliography = append(coherence.Bibliography, BibEntry{
					URL:        s.URL,
					Title:      s.Title,
					SourceType: s.SourceType,
				})
			}
		}
	}

	// Step 6: Assemble files
	assembleOpts := AssembleOptions{OutputDir: opts.OutputDir}
	if assembleOpts.OutputDir == "" {
		assembleOpts.OutputDir = "."
	}

	whitePaper, err := Assemble(plan, chapters, coherence, assembleOpts)
	if err != nil {
		return WhitePaper{}, errors.Wrap(err, "assembly phase failed")
	}
	whitePaper.Metadata.Sources = sources

	// Step 7: Optional rendering
	if opts.RenderHTML != "" {
		if err := RenderWhitePaper(ctx, whitePaper.Entrypoint, RenderOptions{
			Format:       RenderFormatHTML,
			OutputPath:   opts.RenderHTML,
			ChromiumPath: opts.ChromiumPath,
			NoSandbox:    opts.NoSandbox,
		}); err != nil {
			return whitePaper, errors.Wrap(err, "HTML rendering failed")
		}
	}

	if opts.RenderPDF != "" {
		if err := RenderWhitePaper(ctx, whitePaper.Entrypoint, RenderOptions{
			Format:       RenderFormatPDF,
			OutputPath:   opts.RenderPDF,
			ChromiumPath: opts.ChromiumPath,
			NoSandbox:    opts.NoSandbox,
		}); err != nil {
			return whitePaper, errors.Wrap(err, "PDF rendering failed")
		}
	}

	return whitePaper, nil
}

func (o *Orchestrator) conductResearch(ctx context.Context, subject string, depth article.ResearchDepth, kb article.KnowledgeBase, emit agent.EmitFunc) error {
	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{Name: "Recherche"}))

	researchCtx := article.WithContextAgentRole(ctx, article.RoleResearcher)
	researchCtx = article.WithContextSubject(researchCtx, subject)
	researchCtx = article.WithContextResearchDepth(researchCtx, depth)
	researchCtx = article.WithContextKnowledgeBase(researchCtx, kb)
	researchCtx = article.WithContextAdditionalContext(researchCtx, ctxAdditionalContext(ctx))

	// Wire the ProgressTracker callback to forward research steps as TextDelta events.
	researchCtx = article.WithProgressTracking(researchCtx, func(evt article.ProgressEvent) {
		step := evt.Step()
		if step == "" {
			return
		}
		_ = emit(agent.NewEvent(agent.EventTypeTextDelta, &agent.TextDeltaData{
			Delta: "  " + step + "\n",
		}))
	})

	// Suppress the final "research complete" CompleteData — we emit our own summary.
	researchEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			return nil
		}
		return emit(evt)
	}

	if err := agent.NewRunner(o.researcher).Run(researchCtx, agent.NewInput(subject), researchEmit); err != nil {
		return errors.WithStack(err)
	}

	stats := kb.GetStats()
	total, _ := stats["total_documents"].(int)
	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{
		Name: "Recherche",
		Done: true,
		Info: fmt.Sprintf("%d documents indexés", total),
	}))

	return nil
}

func (o *Orchestrator) generatePlan(ctx context.Context, subject string, targetWordCount int, emit agent.EmitFunc) (WhitePaperPlan, error) {
	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{Name: "Planification"}))

	planCtx := withCtxTargetWordCount(ctx, targetWordCount)

	var planJSON string
	proxyEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			planJSON = evt.Data().(*agent.CompleteData).Message
			return nil // suppress JSON payload from reaching the UI
		}
		return emit(evt)
	}

	if err := agent.NewRunner(o.planner).Run(planCtx, agent.NewInput(subject), proxyEmit); err != nil {
		return WhitePaperPlan{}, errors.WithStack(err)
	}

	var plan WhitePaperPlan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return WhitePaperPlan{}, errors.Wrap(err, "could not parse white paper plan")
	}

	chapters := plan.allChapters()
	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{
		Name: "Planification",
		Done: true,
		Info: fmt.Sprintf("%q — %d chapitres, %d mots", plan.Title, len(chapters), plan.TotalWords),
	}))

	return plan, nil
}

func (o *Orchestrator) writeAndEditChapters(ctx context.Context, plan WhitePaperPlan, maxReviewRounds int, emit agent.EmitFunc) ([]ChapterContent, error) {
	chapters := plan.allChapters()
	results := make([]ChapterContent, 0, len(chapters))

	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{Name: "Rédaction"}))

	var previousChapter *ChapterContent

	for _, ch := range chapters {
		select {
		case <-ctx.Done():
			return nil, errors.WithStack(ctx.Err())
		default:
		}

		_ = emit(agent.NewEvent(EventTypeChapterStart, &ChapterStartData{
			Number: int(ch.Number),
			Total:  len(chapters),
			Title:  ch.Title,
			Target: ch.WordCount,
		}))

		// Write — suppress LLM streaming text and JSON payload.
		writeCtx := withCtxChapter(ctx, ch)
		writeCtx = withCtxPreviousChapter(writeCtx, previousChapter)

		var writtenJSON string
		writeEmit := func(evt agent.Event) error {
			switch evt.Type() {
			case agent.EventTypeTextDelta:
				return nil // suppress LLM streaming content
			case agent.EventTypeComplete:
				writtenJSON = evt.Data().(*agent.CompleteData).Message
				return nil // capture but suppress JSON payload
			}
			return emit(evt)
		}

		if err := agent.NewRunner(o.chapterWriter).Run(writeCtx, agent.NewInput(ch.Title), writeEmit); err != nil {
			return nil, errors.Wrapf(err, "could not write chapter %q", ch.Title)
		}

		var written ChapterContent
		if err := json.Unmarshal([]byte(writtenJSON), &written); err != nil {
			return nil, errors.Wrapf(err, "could not parse written chapter %q", ch.Title)
		}

		// Edit — repeat maxReviewRounds times; each round refines the previous output.
		currentContent := written
		for round := range maxReviewRounds {
			editCtx := withCtxChapter(ctx, ch)
			editCtx = withCtxPlan(editCtx, plan)
			// Expose already-finished chapters so the editor can detect redundancies.
			editCtx = withCtxAllChapters(editCtx, results)

			currentJSON, err := json.Marshal(currentContent)
			if err != nil {
				return nil, errors.Wrapf(err, "could not serialize chapter %q for editing (round %d)", ch.Title, round+1)
			}

			var editedJSON string
			editEmit := func(evt agent.Event) error {
				switch evt.Type() {
				case agent.EventTypeTextDelta:
					return nil
				case agent.EventTypeComplete:
					editedJSON = evt.Data().(*agent.CompleteData).Message
					return nil
				}
				return emit(evt)
			}

			if err = agent.NewRunner(o.chapterEditor).Run(editCtx, agent.NewInput(string(currentJSON)), editEmit); err != nil {
				return nil, errors.Wrapf(err, "could not edit chapter %q (round %d)", ch.Title, round+1)
			}

			if err = json.Unmarshal([]byte(editedJSON), &currentContent); err != nil {
				return nil, errors.Wrapf(err, "could not parse edited chapter %q (round %d)", ch.Title, round+1)
			}
		}

		results = append(results, currentContent)
		previousChapter = &currentContent

		_ = emit(agent.NewEvent(EventTypeChapterDone, &ChapterDoneData{
			Number:    currentContent.Number,
			Total:     len(chapters),
			Title:     currentContent.Title,
			WordCount: currentContent.WordCount,
		}))
	}

	return results, nil
}

func (o *Orchestrator) coherencePass(ctx context.Context, plan WhitePaperPlan, chapters []ChapterContent, emit agent.EmitFunc) (CoherenceEditResult, error) {
	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{Name: "Cohérence"}))

	cohCtx := withCtxAllChapters(ctx, chapters)
	cohCtx = withCtxPlan(cohCtx, plan)

	var resultJSON string
	proxyEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			resultJSON = evt.Data().(*agent.CompleteData).Message
			return nil // suppress JSON payload
		}
		return emit(evt)
	}

	if err := agent.NewRunner(o.coherenceEditor).Run(cohCtx, agent.NewInput(""), proxyEmit); err != nil {
		return CoherenceEditResult{}, errors.WithStack(err)
	}

	var result CoherenceEditResult
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return CoherenceEditResult{}, errors.Wrap(err, "could not parse coherence edit result")
	}

	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{
		Name: "Cohérence",
		Done: true,
		Info: fmt.Sprintf("%d sources, %d annexes", len(result.Bibliography), len(result.Appendices)),
	}))

	return result, nil
}

// NewOrchestrator creates a new white paper orchestrator.
func NewOrchestrator(client llm.Client) *Orchestrator {
	scraper := surf.NewScraper()
	researchAgent := article.NewResearchAgent(client, duckduckgo.NewClient(scraper), scraper)

	return &Orchestrator{
		researcher:      researchAgent,
		planner:         NewPlannerHandler(client),
		chapterWriter:   NewChapterWriterHandler(client),
		chapterEditor:   NewChapterEditorHandler(client),
		coherenceEditor: NewCoherenceEditorHandler(client),
	}
}

// WriteWhitePaper is a convenience function.
func WriteWhitePaper(ctx context.Context, client llm.Client, subject string, emit agent.EmitFunc, optFuncs ...OrchestratorOptionFunc) (WhitePaper, error) {
	o := NewOrchestrator(client)
	return o.WriteWhitePaper(ctx, subject, emit, optFuncs...)
}
