package whitepaper

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	researcher      *article.ResearchAgent
	planner         *PlannerHandler
	chapterWriter   *ChapterWriterHandler
	chapterEditor   *ChapterEditorHandler
	coherenceEditor *CoherenceEditorHandler
	citationLinker  *CitationLinkerHandler
	diagramInserter *DiagramInserterHandler
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

	// Step 4b: Enrichment pass (citation linking + Mermaid diagrams)
	chapters, err = o.enrichChapters(ctx, chapters, emit)
	if err != nil {
		return WhitePaper{}, errors.Wrap(err, "enrichment phase failed")
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

	emitInfo := func(msg string) {
		_ = emit(agent.NewEvent(agent.EventTypeTextDelta, &agent.TextDeltaData{Delta: msg}))
	}

	emitInfo(fmt.Sprintf("  %d chapitres à analyser :\n", len(chapters)))
	for _, ch := range chapters {
		emitInfo(fmt.Sprintf("  — Chapitre %d : %s\n", ch.Number, ch.Title))
	}
	emitInfo("  Génération de l'abstract, du résumé exécutif et de la bibliographie…\n")

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

func (o *Orchestrator) enrichChapters(ctx context.Context, chapters []ChapterContent, emit agent.EmitFunc) ([]ChapterContent, error) {
	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{Name: "Enrichissement"}))

	enriched := make([]ChapterContent, len(chapters))
	copy(enriched, chapters)

	for i, ch := range enriched {
		select {
		case <-ctx.Done():
			return nil, errors.WithStack(ctx.Err())
		default:
		}

		_ = emit(agent.NewEvent(EventTypeChapterStart, &ChapterStartData{
			Number: ch.Number,
			Total:  len(enriched),
			Title:  ch.Title,
			Target: ch.WordCount,
		}))

		// Pass 1: citation linking
		chapter := &Chapter{
			ID:     ch.ChapterID,
			Number: FlexInt(ch.Number),
			Title:  ch.Title,
		}
		citCtx := withCtxChapter(ctx, chapter)
		citCtx = withCtxAllChapters(citCtx, enriched)

		var linkedContent string
		citEmit := func(evt agent.Event) error {
			switch evt.Type() {
			case agent.EventTypeTextDelta:
				return nil
			case agent.EventTypeComplete:
				linkedContent = strings.TrimSpace(evt.Data().(*agent.CompleteData).Message)
				return nil
			}
			return emit(evt)
		}

		if err := agent.NewRunner(o.citationLinker).Run(citCtx, agent.NewInput(ch.Content), citEmit); err != nil {
			return nil, errors.Wrapf(err, "citation linking failed for chapter %q", ch.Title)
		}
		if linkedContent != "" {
			enriched[i].Content = linkedContent
			enriched[i].WordCount = countWords(linkedContent)
		}

		// Pass 2: diagram insertion (ctxAllChapters contains citation-enriched content up to i)
		diagCtx := withCtxChapter(ctx, chapter)
		diagCtx = withCtxAllChapters(diagCtx, enriched)

		var diagContent string
		diagEmit := func(evt agent.Event) error {
			switch evt.Type() {
			case agent.EventTypeTextDelta:
				return nil
			case agent.EventTypeComplete:
				diagContent = strings.TrimSpace(evt.Data().(*agent.CompleteData).Message)
				return nil
			}
			return emit(evt)
		}

		if err := agent.NewRunner(o.diagramInserter).Run(diagCtx, agent.NewInput(enriched[i].Content), diagEmit); err != nil {
			return nil, errors.Wrapf(err, "diagram insertion failed for chapter %q", ch.Title)
		}
		if diagContent != "" {
			enriched[i].Content = diagContent
			enriched[i].WordCount = countWords(diagContent)
		}

		_ = emit(agent.NewEvent(EventTypeChapterDone, &ChapterDoneData{
			Number:    enriched[i].Number,
			Total:     len(enriched),
			Title:     enriched[i].Title,
			WordCount: enriched[i].WordCount,
		}))
	}

	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{
		Name: "Enrichissement",
		Done: true,
		Info: fmt.Sprintf("%d chapitres enrichis", len(enriched)),
	}))

	return enriched, nil
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
		citationLinker:  NewCitationLinkerHandler(client),
		diagramInserter: NewDiagramInserterHandler(client),
	}
}

// WriteWhitePaper is a convenience function.
func WriteWhitePaper(ctx context.Context, client llm.Client, subject string, emit agent.EmitFunc, optFuncs ...OrchestratorOptionFunc) (WhitePaper, error) {
	o := NewOrchestrator(client)
	return o.WriteWhitePaper(ctx, subject, emit, optFuncs...)
}

// FixOptions configures the fix pipeline.
type FixOptions struct {
	InputDir          string
	StyleGuidelines   string
	AdditionalContext string
	KnowledgeBase     article.KnowledgeBase
	ForceEnrichment   bool
}

// FixOptionFunc configures FixOptions.
type FixOptionFunc func(*FixOptions)

// FixResult is the outcome of a fix run.
type FixResult struct {
	FixedFiles   []string
	SkippedFiles []string
}

func WithFixInputDir(dir string) FixOptionFunc {
	return func(o *FixOptions) { o.InputDir = dir }
}

func WithFixStyleGuidelines(s string) FixOptionFunc {
	return func(o *FixOptions) { o.StyleGuidelines = s }
}

func WithFixAdditionalContext(s string) FixOptionFunc {
	return func(o *FixOptions) { o.AdditionalContext = s }
}

func WithFixKnowledgeBase(kb article.KnowledgeBase) FixOptionFunc {
	return func(o *FixOptions) { o.KnowledgeBase = kb }
}

func WithFixForceEnrichment(v bool) FixOptionFunc {
	return func(o *FixOptions) { o.ForceEnrichment = v }
}

// FixWhitePaper applies > EDITOR: annotations found in the whitepaper output
// directory, then runs a coherence pass to update index.md and bibliography.
func (o *Orchestrator) FixWhitePaper(ctx context.Context, emit agent.EmitFunc, optFuncs ...FixOptionFunc) (FixResult, error) {
	opts := &FixOptions{}
	for _, fn := range optFuncs {
		fn(opts)
	}

	// Load the plan saved during generation
	planData, err := os.ReadFile(filepath.Join(opts.InputDir, "plan.json"))
	if err != nil {
		return FixResult{}, errors.Wrap(err, "could not read plan.json — was the whitepaper generated with a recent version of ghostwriter?")
	}
	var plan WhitePaperPlan
	if err := json.Unmarshal(planData, &plan); err != nil {
		return FixResult{}, errors.Wrap(err, "could not parse plan.json")
	}

	// Build context
	ctx = withCtxPlan(ctx, plan)
	if opts.StyleGuidelines != "" {
		ctx = withCtxStyleGuidelines(ctx, opts.StyleGuidelines)
	}
	if opts.AdditionalContext != "" {
		ctx = withCtxAdditionalContext(ctx, opts.AdditionalContext)
	}

	kb := opts.KnowledgeBase
	if kb != nil {
		ctx = withCtxKnowledgeBase(ctx, kb)
		searcher := NewKnowledgeBaseAdapter(kb)
		ctx = withCtxSearcher(ctx, searcher)
	}

	// Load all existing chapters (clean content, annotations stripped)
	allChapters, err := loadChaptersFromDir(opts.InputDir)
	if err != nil {
		return FixResult{}, errors.Wrap(err, "could not load existing chapters")
	}

	// Parse all chapter files to find annotated ones
	matches, err := filepath.Glob(filepath.Join(opts.InputDir, "chapter-*.md"))
	if err != nil {
		return FixResult{}, errors.Wrap(err, "could not glob chapter files")
	}

	var result FixResult

	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{Name: "Corrections"}))

	for _, filePath := range matches {
		af, err := ParseAnnotatedFile(filePath)
		if err != nil {
			return FixResult{}, errors.Wrapf(err, "could not parse %s", filePath)
		}

		if len(af.Annotations) == 0 {
			result.SkippedFiles = append(result.SkippedFiles, filePath)
			continue
		}

		_ = emit(agent.NewEvent(EventTypeChapterStart, &ChapterStartData{
			Number: af.Number,
			Total:  len(matches),
			Title:  af.Title,
			Target: countWords(af.CleanContent),
		}))

		// Build minimal Chapter struct from the file metadata
		chapter := &Chapter{
			ID:     strings.TrimSuffix(filepath.Base(filePath), ".md"),
			Number: FlexInt(af.Number),
			Title:  af.Title,
		}

		// Build ChapterContent with clean content (annotations removed)
		content := ChapterContent{
			ChapterID: chapter.ID,
			Number:    af.Number,
			Title:     af.Title,
			Content:   af.CleanContent,
			WordCount: countWords(af.CleanContent),
		}

		// Set up context: annotations + all chapters (enables query_document tool)
		fixCtx := withCtxChapter(ctx, chapter)
		fixCtx = withCtxAnnotations(fixCtx, af.Annotations)
		fixCtx = withCtxAllChapters(fixCtx, allChapters)

		// Serialize content for the editor input
		contentJSON, err := json.Marshal(content)
		if err != nil {
			return FixResult{}, errors.Wrapf(err, "could not serialize chapter %q", af.Title)
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

		if err := agent.NewRunner(o.chapterEditor).Run(fixCtx, agent.NewInput(string(contentJSON)), editEmit); err != nil {
			return FixResult{}, errors.Wrapf(err, "could not fix chapter %q", af.Title)
		}

		var edited ChapterContent
		if err := json.Unmarshal([]byte(editedJSON), &edited); err != nil {
			return FixResult{}, errors.Wrapf(err, "could not parse edited chapter %q", af.Title)
		}

		// Write fixed chapter file immediately
		fileContent := fmt.Sprintf("# %s\n\n%s\n", edited.Title, edited.Content)
		if err := os.WriteFile(filePath, []byte(fileContent), 0644); err != nil {
			return FixResult{}, errors.Wrapf(err, "could not write fixed chapter %q", filePath)
		}

		// Update the in-memory chapter list for the coherence pass
		for i, ch := range allChapters {
			if ch.Number == af.Number {
				allChapters[i] = edited
				break
			}
		}

		result.FixedFiles = append(result.FixedFiles, filePath)

		_ = emit(agent.NewEvent(EventTypeChapterDone, &ChapterDoneData{
			Number:    edited.Number,
			Total:     len(matches),
			Title:     edited.Title,
			WordCount: edited.WordCount,
		}))
	}

	if len(result.FixedFiles) == 0 && !opts.ForceEnrichment {
		_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{
			Name: "Corrections",
			Done: true,
			Info: "aucune annotation trouvée",
		}))
		return result, nil
	}

	_ = emit(agent.NewEvent(EventTypePhase, &PhaseData{
		Name: "Corrections",
		Done: true,
		Info: fmt.Sprintf("%d fichier(s) corrigé(s)", len(result.FixedFiles)),
	}))

	// Coherence pass with updated chapters
	coherence, err := o.coherencePass(ctx, plan, allChapters, emit)
	if err != nil {
		return result, errors.Wrap(err, "coherence phase failed")
	}

	// Enrichment pass (citation linking + Mermaid diagrams)
	allChapters, err = o.enrichChapters(ctx, allChapters, emit)
	if err != nil {
		return result, errors.Wrap(err, "enrichment phase failed")
	}

	// Collect sources from KB if available
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

	// Re-assemble to update index.md, bibliography.md, appendices
	if _, err := Assemble(plan, allChapters, coherence, AssembleOptions{OutputDir: opts.InputDir}); err != nil {
		return result, errors.Wrap(err, "assembly phase failed")
	}

	return result, nil
}

// FixWhitePaperInDir is a convenience function.
func FixWhitePaperInDir(ctx context.Context, client llm.Client, emit agent.EmitFunc, optFuncs ...FixOptionFunc) (FixResult, error) {
	o := NewOrchestrator(client)
	return o.FixWhitePaper(ctx, emit, optFuncs...)
}
