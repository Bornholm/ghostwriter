package article

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/agent/task"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var researcherPrompts embed.FS

// ResearchAgent conducts comprehensive research and builds knowledge base
type ResearchAgent struct {
	client llm.ChatCompletionClient
	tools  []llm.Tool
	kb     *KnowledgeBase
}

// ResearchRequestEvent represents a request to conduct research
type ResearchRequestEvent struct {
	ctx     context.Context
	id      agent.EventID
	Subject string        `json:"subject"`
	Depth   ResearchDepth `json:"depth"`
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
		ctx:     ctx,
		id:      e.id,
		Subject: e.Subject,
		Depth:   e.Depth,
	}
}

// NewResearchRequestEvent creates a new research request event
func NewResearchRequestEvent(ctx context.Context, subject string, depth ResearchDepth) *ResearchRequestEvent {
	return &ResearchRequestEvent{
		ctx:     ctx,
		id:      agent.NewEventID(),
		Subject: subject,
		Depth:   depth,
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

	// Initialize knowledge base
	kb, err := NewKnowledgeBase(subject)
	if err != nil {
		return errors.WithStack(err)
	}
	h.kb = kb

	// Conduct research and populate knowledge base
	err = h.conductResearch(ctx, subject, depth)
	if err != nil {
		return errors.WithStack(err)
	}

	// Create and send research complete event
	completeEvent := NewResearchCompleteEvent(ctx, subject, kb)
	outputs <- completeEvent

	return nil
}

// conductResearch performs comprehensive research using available tools
func (h *ResearchAgent) conductResearch(ctx context.Context, subject string, depth ResearchDepth) error {
	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)

	tracker.EmitSubProgress(PhaseResearching, "Initializing research process",
		GetPhaseBaseProgress(PhaseResearching), 0.1, ResearchingWeight, map[string]interface{}{
			"step":    "initialization",
			"subject": subject,
			"depth":   depth,
		})

	// Load the researcher system prompt
	systemPrompt, err := prompt.FromFS[any](&researcherPrompts, "prompts/researcher_system.gotmpl", nil)
	if err != nil {
		return errors.WithStack(err)
	}

	// Create research prompt based on depth
	userPrompt := h.createResearchPrompt(subject, depth)

	tracker.EmitSubProgress(PhaseResearching, "Starting comprehensive research",
		GetPhaseBaseProgress(PhaseResearching), 0.2, ResearchingWeight, map[string]interface{}{
			"step": "research_start",
		})

	// Set up task context for iterative research with knowledge base tool
	minIterations := h.getMinIterations(depth)
	maxIterations := h.getMaxIterations(depth)

	kbTool := NewAddToKnowledgeBaseTool(h.kb)

	// Combine research tools with knowledge base tool
	allTools := append(h.tools, kbTool)

	taskCtx := task.WithContextMinIterations(ctx, minIterations)
	taskCtx = task.WithContextMaxIterations(taskCtx, maxIterations)
	taskCtx = task.WithContextMaxToolIterations(taskCtx, maxIterations*2)

	taskCtx = agent.WithContextMessages(taskCtx, []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
	})
	taskCtx = agent.WithContextTools(taskCtx, allTools)

	// Create task handler for research
	taskHandler := task.NewHandler(h.client, task.WithDefaultTools(allTools...))
	researchAgent := agent.New(taskHandler)

	// Start the research agent
	if _, _, err := researchAgent.Start(taskCtx); err != nil {
		return errors.WithStack(err)
	}
	defer researchAgent.Stop()

	tracker.EmitSubProgress(PhaseResearching, "Conducting research and gathering sources",
		GetPhaseBaseProgress(PhaseResearching), 0.5, ResearchingWeight, map[string]interface{}{
			"step": "research_execution",
		})

	// Execute research task
	if _, err := task.Do(taskCtx, researchAgent, userPrompt); err != nil {
		return errors.WithStack(err)
	}

	tracker.EmitSubProgress(PhaseResearching, "Processing research results and building knowledge base",
		GetPhaseBaseProgress(PhaseResearching), 0.8, ResearchingWeight, map[string]interface{}{
			"step": "knowledge_base_building",
		})

	stats := h.kb.GetStats()
	tracker.EmitSubProgress(PhaseResearching, fmt.Sprintf("Research completed: %d documents indexed", stats["total_documents"]),
		GetPhaseBaseProgress(PhaseResearching), 1.0, ResearchingWeight, map[string]interface{}{
			"step":  "research_complete",
			"stats": stats,
		})

	return nil
}

// createResearchPrompt creates the research prompt based on subject and depth
func (h *ResearchAgent) createResearchPrompt(subject string, depth ResearchDepth) string {
	var prompt strings.Builder

	prompt.WriteString("Conduct comprehensive research on the following subject:\n\n")
	prompt.WriteString("**Subject:** ")
	prompt.WriteString(subject)
	prompt.WriteString("\n\n")

	prompt.WriteString("**Research Depth:** ")
	prompt.WriteString(string(depth))
	prompt.WriteString("\n\n")

	prompt.WriteString("**Research Objectives:**\n")
	prompt.WriteString("1. Gather current, accurate information about the subject\n")
	prompt.WriteString("2. Find diverse, credible sources (news, academic, industry, government)\n")
	prompt.WriteString("3. Identify key developments, trends, and important aspects\n")
	prompt.WriteString("4. Collect facts, statistics, quotes, and examples\n")
	prompt.WriteString("5. Note different perspectives and viewpoints\n")
	prompt.WriteString("6. Ensure information is recent and relevant\n\n")

	switch depth {
	case ResearchBasic:
		prompt.WriteString("**Research Scope:** Basic overview with 5-10 key sources\n")
	case ResearchDeep:
		prompt.WriteString("**Research Scope:** Comprehensive research with 15-25 diverse sources\n")
	case ResearchDeepWeb:
		prompt.WriteString("**Research Scope:** Extensive web research with 25-40 sources including specialized sites\n")
	case ResearchAcademic:
		prompt.WriteString("**Research Scope:** Academic-level research with 30-50 sources including scholarly content\n")
	}

	prompt.WriteString("\n**Instructions:**\n")
	prompt.WriteString("1. Start with broad web searches to understand the topic scope\n")
	prompt.WriteString("2. Identify the most promising sources and scrape them for detailed content\n")
	prompt.WriteString("3. Follow up with targeted searches for specific aspects\n")
	prompt.WriteString("4. Verify important facts across multiple sources\n")
	prompt.WriteString("5. Organize findings by relevance and credibility\n")
	prompt.WriteString("6. Provide a comprehensive summary of your research findings\n")
	prompt.WriteString("7. Use 'add_to_knowledge_base' tool to add the identified resources to the knowledge base\n\n")

	prompt.WriteString("Please conduct thorough research and provide a detailed summary of your findings.")

	return prompt.String()
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

// Helper methods
func (h *ResearchAgent) getMinIterations(depth ResearchDepth) int {
	switch depth {
	case ResearchBasic:
		return 2
	case ResearchDeep:
		return 3
	case ResearchDeepWeb:
		return 4
	case ResearchAcademic:
		return 5
	default:
		return 3
	}
}

func (h *ResearchAgent) getMaxIterations(depth ResearchDepth) int {
	switch depth {
	case ResearchBasic:
		return 4
	case ResearchDeep:
		return 6
	case ResearchDeepWeb:
		return 8
	case ResearchAcademic:
		return 10
	default:
		return 6
	}
}

// NewResearchAgent creates a new research agent
func NewResearchAgent(client llm.ChatCompletionClient, tools ...llm.Tool) *ResearchAgent {
	return &ResearchAgent{
		client: client,
		tools:  tools,
	}
}

var _ agent.Handler = &ResearchAgent{}
