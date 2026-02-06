package article

import (
	"context"
	"embed"
	"fmt"
	"net/url"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/PuerkitoBio/goquery"
	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/bornholm/ghostwriter/pkg/scraper"
	"github.com/bornholm/ghostwriter/pkg/search"
	"github.com/invopop/jsonschema"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var researcherPrompts embed.FS

// SearchQuery represents a structured search query
type SearchQuery struct {
	Query     string   `json:"query" jsonschema:"required,description=The search query string"`
	Keywords  []string `json:"keywords" jsonschema:"required,description=Key terms related to this query"`
	Priority  int      `json:"priority" jsonschema:"required,description=Priority level 1-5, 5 being highest"`
	Rationale string   `json:"rationale" jsonschema:"required,description=Why this query is important for the research"`
}

// SearchQueriesResponse represents the LLM response for query generation
type SearchQueriesResponse struct {
	Queries []SearchQuery `json:"queries" jsonschema:"required,description=List of search queries to execute"`
	Focus   string        `json:"focus" jsonschema:"required,description=Main research focus for this batch"`
}

// ResearchState tracks the research progress
type ResearchState struct {
	ProcessedURLs    map[string]bool
	TotalArticles    int
	TargetArticles   int
	CurrentIteration int
	MaxIterations    int
	ContentSummaries []string
}

// ResearchAgent conducts comprehensive research and builds knowledge base
type ResearchAgent struct {
	client       llm.ChatCompletionClient
	searchClient search.Client
	scraper      scraper.Scraper
}

// ResearchRequestEvent represents a request to conduct research
type ResearchRequestEvent struct {
	ctx           context.Context
	id            agent.EventID
	Subject       string         `json:"subject"`
	Depth         ResearchDepth  `json:"depth"`
	KnowledgeBase *KnowledgeBase `json:"knowledgeBase"`
}

// Context implements agent.Event
func (e *ResearchRequestEvent) Context() context.Context {
	return e.ctx
}

// ID implements agent.Event
func (e *ResearchRequestEvent) ID() agent.EventID {
	return e.id
}

// WithContext implements agent.Event
func (e *ResearchRequestEvent) WithContext(ctx context.Context) agent.Event {
	return &ResearchRequestEvent{
		ctx:           ctx,
		id:            e.id,
		Subject:       e.Subject,
		Depth:         e.Depth,
		KnowledgeBase: e.KnowledgeBase,
	}
}

// NewResearchRequestEvent creates a new research request event
func NewResearchRequestEvent(ctx context.Context, subject string, depth ResearchDepth, kb *KnowledgeBase) *ResearchRequestEvent {
	return &ResearchRequestEvent{
		ctx:           ctx,
		id:            agent.NewEventID(),
		Subject:       subject,
		Depth:         depth,
		KnowledgeBase: kb,
	}
}

// ResearchCompleteEvent represents completed research with knowledge base
type ResearchCompleteEvent struct {
	ctx     context.Context
	id      agent.EventID
	Subject string                 `json:"subject"`
	KB      *KnowledgeBase         `json:"-"` // Don't serialize the knowledge base
	Stats   map[string]interface{} `json:"stats"`
}

// Context implements agent.Event
func (e *ResearchCompleteEvent) Context() context.Context {
	return e.ctx
}

// ID implements agent.Event
func (e *ResearchCompleteEvent) ID() agent.EventID {
	return e.id
}

// WithContext implements agent.Event
func (e *ResearchCompleteEvent) WithContext(ctx context.Context) agent.Event {
	return &ResearchCompleteEvent{
		ctx:     ctx,
		id:      e.id,
		Subject: e.Subject,
		KB:      e.KB,
		Stats:   e.Stats,
	}
}

// NewResearchCompleteEvent creates a new research complete event
func NewResearchCompleteEvent(ctx context.Context, subject string, kb *KnowledgeBase) *ResearchCompleteEvent {
	return &ResearchCompleteEvent{
		ctx:     ctx,
		id:      agent.NewEventID(),
		Subject: subject,
		KB:      kb,
		Stats:   kb.GetStats(),
	}
}

// Handle implements agent.Handler for research requests
func (h *ResearchAgent) Handle(input agent.Event, outputs chan agent.Event) error {
	researchRequest, ok := input.(*ResearchRequestEvent)
	if !ok {
		return errors.Wrapf(agent.ErrNotSupported, "event type '%T' not supported by researcher", input)
	}

	ctx := input.Context()
	subject := researchRequest.Subject
	depth := researchRequest.Depth
	kb := researchRequest.KnowledgeBase

	if kb == nil {
		return errors.New("missing knowledge base")
	}

	// Conduct research and populate knowledge base
	if err := h.conductResearch(ctx, subject, depth, kb); err != nil {
		return errors.WithStack(err)
	}

	// Create and send research complete event
	completeEvent := NewResearchCompleteEvent(ctx, subject, kb)
	outputs <- completeEvent

	return nil
}

// conductResearch performs structured research using the new approach
func (h *ResearchAgent) conductResearch(ctx context.Context, subject string, depth ResearchDepth, kb *KnowledgeBase) error {
	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)

	tracker.EmitSubProgress(PhaseResearching, "Initializing structured research process",
		GetPhaseBaseProgress(PhaseResearching), 0.05, ResearchingWeight, map[string]interface{}{
			"step":    "initialization",
			"subject": subject,
			"depth":   depth,
		})

	// Initialize research state
	state := &ResearchState{
		ProcessedURLs:    make(map[string]bool),
		TotalArticles:    0,
		TargetArticles:   h.getTargetArticles(depth),
		CurrentIteration: 0,
		MaxIterations:    h.getMaxIterations(depth),
		ContentSummaries: make([]string, 0),
	}

	tracker.EmitSubProgress(PhaseResearching, fmt.Sprintf("Target: %d articles, Max iterations: %d", state.TargetArticles, state.MaxIterations),
		GetPhaseBaseProgress(PhaseResearching), 0.1, ResearchingWeight, map[string]interface{}{
			"step":            "target_set",
			"target_articles": state.TargetArticles,
			"max_iterations":  state.MaxIterations,
		})

	// Main research loop
	for state.CurrentIteration < state.MaxIterations && state.TotalArticles < state.TargetArticles {
		state.CurrentIteration++

		iterationProgress := 0.1 + (0.8 * float64(state.CurrentIteration-1) / float64(state.MaxIterations))

		tracker.EmitSubProgress(PhaseResearching, fmt.Sprintf("Starting iteration %d/%d", state.CurrentIteration, state.MaxIterations),
			GetPhaseBaseProgress(PhaseResearching), iterationProgress, ResearchingWeight, map[string]interface{}{
				"step":      "iteration_start",
				"iteration": state.CurrentIteration,
				"articles":  state.TotalArticles,
			})

		// Generate search queries for this iteration
		queries, err := h.generateSearchQueries(ctx, subject, state.ContentSummaries, state.CurrentIteration)
		if err != nil {
			return errors.WithStack(err)
		}

		// Execute search and scrape for each query
		articles, err := h.executeSearchAndScrape(ctx, queries, state)
		if err != nil {
			return errors.WithStack(err)
		}

		// Add articles to knowledge base with deduplication
		if err := h.addToKnowledgeBaseWithDeduplication(ctx, articles, kb, state); err != nil {
			return errors.WithStack(err)
		}

		// Update content summaries for next iteration
		if len(articles) > 0 {
			summary := h.extractContentSummary(articles)
			state.ContentSummaries = append(state.ContentSummaries, summary)
		}

		tracker.EmitSubProgress(PhaseResearching, fmt.Sprintf("Iteration %d complete: %d/%d articles collected", state.CurrentIteration, state.TotalArticles, state.TargetArticles),
			GetPhaseBaseProgress(PhaseResearching), iterationProgress+0.1, ResearchingWeight, map[string]interface{}{
				"step":      "iteration_complete",
				"iteration": state.CurrentIteration,
				"articles":  state.TotalArticles,
				"target":    state.TargetArticles,
			})
	}

	stats := kb.GetStats()
	tracker.EmitSubProgress(PhaseResearching, fmt.Sprintf("Research completed: %d documents indexed", stats["total_documents"]),
		GetPhaseBaseProgress(PhaseResearching), 1.0, ResearchingWeight, map[string]interface{}{
			"step":  "research_complete",
			"stats": stats,
		})

	return nil
}

// generateSearchQueries generates structured search queries using LLM
func (h *ResearchAgent) generateSearchQueries(ctx context.Context, subject string, existingContent []string, iteration int) ([]SearchQuery, error) {
	// Load the query generation system prompt
	systemPrompt, err := prompt.FromFS[any](&researcherPrompts, "prompts/query_generator_system.gotmpl", nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	additionalContext := ContextAdditionalContext(ctx, "")

	// Create query generation prompt
	userPrompt := h.createQueryGenerationPrompt(subject, existingContent, iteration, additionalContext)

	// Create JSON schema for SearchQueriesResponse
	queriesSchema := h.createSearchQueriesSchema()

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	}

	// Make LLM call with structured output
	response, err := h.client.ChatCompletion(ctx,
		llm.WithMessages(messages...),
		llm.WithTemperature(0.3),
		llm.WithResponseSchema(queriesSchema),
	)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Parse the response
	message := llm.NewMessage(llm.RoleAssistant, response.Message().Content())
	queriesResponses, err := llm.ParseJSON[SearchQueriesResponse](message)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if len(queriesResponses) == 0 {
		return nil, errors.New("no queries generated")
	}

	return queriesResponses[0].Queries, nil
}

// executeSearchAndScrape executes searches and scrapes top 5 articles for each query
func (h *ResearchAgent) executeSearchAndScrape(ctx context.Context, queries []SearchQuery, state *ResearchState) ([]ResearchDocument, error) {
	var allArticles []ResearchDocument

	for _, query := range queries {
		// Search for results
		results, err := h.searchClient.Search(ctx, query.Query)
		if err != nil {
			// Log error but continue with other queries
			continue
		}

		// Take top 5 results (or fewer if less available)
		maxResults := 5
		if len(results) < maxResults {
			maxResults = len(results)
		}

		for j := 0; j < maxResults; j++ {
			result := results[j]

			// Check if URL already processed (deduplication)
			normalizedURL := h.normalizeURL(result.URL)
			if state.ProcessedURLs[normalizedURL] {
				continue
			}

			// Scrape the article
			article, err := h.scrapeArticle(ctx, result)
			if err != nil {
				// Log error but continue with other articles
				continue
			}

			// Mark URL as processed
			state.ProcessedURLs[normalizedURL] = true
			allArticles = append(allArticles, article)
		}
	}

	return allArticles, nil
}

// addToKnowledgeBaseWithDeduplication adds articles to KB while preventing duplicates
func (h *ResearchAgent) addToKnowledgeBaseWithDeduplication(ctx context.Context, articles []ResearchDocument, kb *KnowledgeBase, state *ResearchState) error {
	for _, article := range articles {
		// Add to knowledge base
		if err := kb.AddDocument(article); err != nil {
			return errors.WithStack(err)
		}
		state.TotalArticles++
	}
	return nil
}

// extractContentSummary extracts key themes from scraped content for next iteration
func (h *ResearchAgent) extractContentSummary(articles []ResearchDocument) string {
	var themes []string
	keywordCounts := make(map[string]int)

	// Collect all keywords and count frequency
	for _, article := range articles {
		for _, keyword := range article.Keywords {
			keywordCounts[keyword]++
		}
	}

	// Get most frequent keywords as themes
	for keyword, count := range keywordCounts {
		if count >= 2 { // Keyword appears in multiple articles
			themes = append(themes, keyword)
		}
	}

	if len(themes) == 0 {
		return "General coverage of the topic"
	}

	return fmt.Sprintf("Covered themes: %s", strings.Join(themes, ", "))
}

// Helper methods
func (h *ResearchAgent) normalizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	// Remove query parameters and fragments for deduplication
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (h *ResearchAgent) scrapeArticle(ctx context.Context, result search.Result) (ResearchDocument, error) {
	// Scrape the webpage content
	res, err := h.scraper.Get(ctx, result.URL)
	if err != nil {
		return ResearchDocument{}, errors.WithStack(err)
	}
	defer res.Close()

	// Parse HTML document
	doc, err := goquery.NewDocumentFromReader(res)
	if err != nil {
		return ResearchDocument{}, errors.WithStack(err)
	}

	// Extract HTML body content
	html, err := doc.Find("body").Html()
	if err != nil {
		return ResearchDocument{}, errors.WithStack(err)
	}

	// Convert HTML to markdown
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
		),
	)

	markdown, err := conv.ConvertString(html)
	if err != nil {
		return ResearchDocument{}, errors.WithStack(err)
	}

	contentStr := strings.TrimSpace(markdown)

	// Limit content size to prevent memory issues
	if len(contentStr) > 10000 {
		contentStr = contentStr[:10000] + "..."
	}

	// Extract keywords from title and description
	keywords := h.extractKeywords(result.Title + " " + result.Description)

	return ResearchDocument{
		URL:        result.URL,
		Title:      result.Title,
		Content:    contentStr,
		Keywords:   keywords,
		SourceType: h.detectSourceType(result.URL),
		Relevance:  0.8, // Default relevance, could be calculated based on content quality
	}, nil
}

// detectSourceType attempts to determine the source type from URL
func (h *ResearchAgent) detectSourceType(url string) string {
	url = strings.ToLower(url)

	// Academic sources
	if strings.Contains(url, ".edu") || strings.Contains(url, "scholar.google") ||
		strings.Contains(url, "arxiv.org") || strings.Contains(url, "pubmed") {
		return "academic"
	}

	// Government sources
	if strings.Contains(url, ".gov") || strings.Contains(url, ".mil") {
		return "government"
	}

	// News sources (common news domains)
	newsPatterns := []string{"news", "cnn.com", "bbc.com", "reuters.com", "ap.org",
		"nytimes.com", "wsj.com", "guardian.com", "washingtonpost.com"}
	for _, pattern := range newsPatterns {
		if strings.Contains(url, pattern) {
			return "news"
		}
	}

	// Industry/professional sources
	industryPatterns := []string{"techcrunch.com", "forbes.com", "bloomberg.com",
		"industry", "professional", "trade"}
	for _, pattern := range industryPatterns {
		if strings.Contains(url, pattern) {
			return "industry"
		}
	}

	// Default to web
	return "web"
}

func (h *ResearchAgent) extractKeywords(text string) []string {
	// Simplified keyword extraction - split by spaces and filter
	words := strings.Fields(strings.ToLower(text))
	var keywords []string

	for _, word := range words {
		// Simple filtering - remove short words and common words
		if len(word) > 3 && !h.isCommonWord(word) {
			keywords = append(keywords, word)
		}
	}

	// Limit to 10 keywords
	if len(keywords) > 10 {
		keywords = keywords[:10]
	}

	return keywords
}

func (h *ResearchAgent) isCommonWord(word string) bool {
	commonWords := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "had": true,
		"her": true, "was": true, "one": true, "our": true, "out": true,
		"day": true, "get": true, "has": true, "him": true, "his": true,
		"how": true, "its": true, "may": true, "new": true, "now": true,
		"old": true, "see": true, "two": true, "who": true, "boy": true,
		"did": true, "man": true, "way": true, "she": true, "use": true,
		"many": true, "oil": true, "sit": true, "set": true,
		"said": true, "each": true, "which": true, "their": true,
	}
	return commonWords[word]
}

func (h *ResearchAgent) createQueryGenerationPrompt(subject string, existingContent []string, iteration int, additionalContext string) string {
	var prompt strings.Builder

	prompt.WriteString("Generate strategic search queries for comprehensive research.\n\n")
	prompt.WriteString("**Subject:** ")
	prompt.WriteString(subject)
	prompt.WriteString("\n")
	prompt.WriteString("**Iteration:** ")
	prompt.WriteString(fmt.Sprintf("%d", iteration))
	prompt.WriteString("\n\n")

	if len(existingContent) > 0 {
		prompt.WriteString("**Previous Research Coverage:**\n")
		for i, content := range existingContent {
			prompt.WriteString(fmt.Sprintf("%d. %s\n", i+1, content))
		}
		prompt.WriteString("\n")
	}

	if iteration == 1 {
		prompt.WriteString("**Instructions for Initial Queries:**\n")
		prompt.WriteString("- Generate 3-5 broad, foundational queries\n")
		prompt.WriteString("- Cover main aspects and key concepts\n")
		prompt.WriteString("- Focus on authoritative sources\n")
		prompt.WriteString("- Include current developments and trends\n")
	} else {
		prompt.WriteString("**Instructions for Follow-up Queries:**\n")
		prompt.WriteString("- Generate 2-4 targeted queries based on content gaps\n")
		prompt.WriteString("- Focus on missing perspectives or details\n")
		prompt.WriteString("- Explore specialized or niche aspects\n")
		prompt.WriteString("- Avoid duplicating previous coverage\n")
	}

	prompt.WriteString("\nProvide queries in the specified JSON format with rationale for each.")

	if additionalContext != "" {
		prompt.WriteString("**Additional Context:**\n\n")
		prompt.WriteString(additionalContext)
		prompt.WriteString("\n\n")
	}

	return prompt.String()
}

func (h *ResearchAgent) createSearchQueriesSchema() llm.ResponseSchema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}

	schema := reflector.Reflect(&SearchQueriesResponse{})

	return llm.NewResponseSchema(
		"search_queries",
		"A structured list of search queries for research with focus and rationale",
		schema,
	)
}

// ResearchResults represents the structured output from research agent
type ResearchResults struct {
	Subject  string                 `json:"subject"`
	Summary  string                 `json:"summary"`
	Articles []CollectedArticle     `json:"articles"`
	Keywords []string               `json:"keywords"`
	Sources  []string               `json:"sources"`
	Metadata map[string]interface{} `json:"metadata"`
}

// CollectedArticle represents a single research article/source
type CollectedArticle struct {
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Content     string   `json:"content"`
	Summary     string   `json:"summary"`
	Keywords    []string `json:"keywords"`
	SourceType  string   `json:"source_type"`
	Relevance   float64  `json:"relevance"`
	PublishedAt string   `json:"published_at"`
}

// Helper methods for research configuration
func (h *ResearchAgent) getTargetArticles(depth ResearchDepth) int {
	switch depth {
	case ResearchBasic:
		return 10
	case ResearchDeep:
		return 20
	case ResearchDeepWeb:
		return 32
	case ResearchAcademic:
		return 40
	default:
		return 10
	}
}

func (h *ResearchAgent) getMaxIterations(depth ResearchDepth) int {
	switch depth {
	case ResearchBasic:
		return 3
	case ResearchDeep:
		return 4
	case ResearchDeepWeb:
		return 6
	case ResearchAcademic:
		return 8
	default:
		return 3
	}
}

// NewResearchAgent creates a new research agent
func NewResearchAgent(client llm.ChatCompletionClient, searchClient search.Client, scraper scraper.Scraper) *ResearchAgent {
	return &ResearchAgent{
		client:       client,
		searchClient: searchClient,
		scraper:      scraper,
	}
}

var _ agent.Handler = &ResearchAgent{}
