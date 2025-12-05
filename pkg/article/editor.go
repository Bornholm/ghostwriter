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
var editorPrompts embed.FS

// EditorHandler handles article editing and finalization
type EditorHandler struct {
	client llm.ChatCompletionClient
}

// EditRequest represents a request to edit an article
type EditRequestEvent struct {
	ctx context.Context
	id  agent.EventID

	Title    string           `json:"title"`
	Subject  string           `json:"subject"`
	Sections []SectionContent `json:"sections"`
	Plan     DocumentPlan     `json:"plan"`
}

// Context implements agent.Event.
func (e *EditRequestEvent) Context() context.Context {
	return e.ctx
}

// ID implements agent.Event.
func (e *EditRequestEvent) ID() agent.EventID {
	return e.id
}

// WithContext implements agent.Event.
func (e *EditRequestEvent) WithContext(ctx context.Context) agent.Event {
	return &EditRequestEvent{
		id:       e.id,
		ctx:      ctx,
		Title:    e.Title,
		Subject:  e.Subject,
		Sections: e.Sections,
		Plan:     e.Plan,
	}
}

func NewEditRequestEvent(ctx context.Context, title, subject string, sections []SectionContent, plan DocumentPlan) *EditRequestEvent {
	return &EditRequestEvent{
		ctx:      ctx,
		id:       agent.NewEventID(),
		Title:    title,
		Subject:  subject,
		Sections: sections,
		Plan:     plan,
	}
}

var _ agent.Event = &EditRequestEvent{}

// Handle implements agent.Handler for editing requests
func (h *EditorHandler) Handle(input agent.Event, outputs chan agent.Event) error {
	editRequest, ok := input.(*EditRequestEvent)
	if !ok {
		return errors.Wrapf(agent.ErrNotSupported, "event type '%T' not supported by editor", input)
	}

	ctx := input.Context()

	// Get context values
	agentRole := ContextAgentRole(ctx, RoleEditor)
	if agentRole != RoleEditor {
		return errors.New("editor handler can only process editor role events")
	}

	// Edit and finalize the article using knowledge base
	article, err := h.editArticle(ctx, editRequest)
	if err != nil {
		return errors.WithStack(err)
	}

	// Create and send the final article event
	articleEvent := NewFinalArticleEvent(ctx, article, editRequest)
	outputs <- articleEvent

	return nil
}

// editArticle processes and enhances the complete article using knowledge base
func (h *EditorHandler) editArticle(ctx context.Context, request *EditRequestEvent) (Document, error) {
	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)

	// Get knowledge base from context
	kb, hasKB := ContextKnowledgeBase(ctx)
	if !hasKB {
		return Document{}, errors.New("knowledge base not available in context")
	}

	// Emit progress for editing initialization
	tracker.EmitSubProgress(PhaseEditing, "Initializing article editing with knowledge base access",
		GetPhaseBaseProgress(PhaseEditing), 0.1, EditingWeight, map[string]interface{}{
			"step":           "initialization",
			"sections_count": len(request.Sections),
			"title":          request.Title,
		})

	// Load the editor system prompt (updated version)
	systemPrompt, err := prompt.FromFS[any](&editorPrompts, "prompts/editor_system.gotmpl", nil)
	if err != nil {
		return Document{}, errors.WithStack(err)
	}

	// Create knowledge-based editing tools
	knowledgeTools := []llm.Tool{
		NewSearchKnowledgeBaseTool(kb),
	}

	// Emit progress for prompt creation
	tracker.EmitSubProgress(PhaseEditing, "Preparing editing instructions",
		GetPhaseBaseProgress(PhaseEditing), 0.2, EditingWeight, map[string]interface{}{
			"step": "prompt_creation",
		})

	// Create the knowledge-enhanced editing prompt
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	additionalContext := ContextAdditionalContext(ctx, "")
	userPrompt := h.createEditingPrompt(request, kb, styleGuidelines, additionalContext)

	// Emit progress for editing execution
	tracker.EmitSubProgress(PhaseEditing, "Enhancing content using knowledge base insights",
		GetPhaseBaseProgress(PhaseEditing), 0.4, EditingWeight, map[string]interface{}{
			"step": "editing_execution",
		})

	// Set up task context for knowledge-enhanced editing
	taskCtx := task.WithContextMinIterations(ctx, 1)
	taskCtx = task.WithContextMaxIterations(taskCtx, 3)
	taskCtx = task.WithContextMaxToolIterations(taskCtx, 6)
	taskCtx = agent.WithContextMessages(taskCtx, []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
	})
	taskCtx = agent.WithContextTools(taskCtx, knowledgeTools)

	// Create task handler
	taskHandler := task.NewHandler(h.client, task.WithDefaultTools(knowledgeTools...))
	editorAgent := agent.New(taskHandler)

	// Start the agent
	if _, _, err := editorAgent.Start(taskCtx); err != nil {
		return Document{}, errors.WithStack(err)
	}
	defer editorAgent.Stop()

	// Execute editing task
	result, err := task.Do(taskCtx, editorAgent, userPrompt)
	if err != nil {
		return Document{}, errors.WithStack(err)
	}

	// Emit progress for content processing
	tracker.EmitSubProgress(PhaseEditing, "Processing enhanced content and consolidating sources",
		GetPhaseBaseProgress(PhaseEditing), 0.7, EditingWeight, map[string]interface{}{
			"step": "content_processing",
		})

	// Parse the edited article using the shared utility
	article, err := h.parseEditedArticle(ctx, result.Result(), request)
	if err != nil {
		return Document{}, errors.WithStack(err)
	}

	// Emit progress for editing completion
	tracker.EmitSubProgress(PhaseEditing, fmt.Sprintf("Article editing completed: %d words", article.WordCount),
		GetPhaseBaseProgress(PhaseEditing), 1.0, EditingWeight, map[string]interface{}{
			"step":        "editing_complete",
			"final_title": article.Title,
			"word_count":  article.WordCount,
		})

	return article, nil
}

// createEditingPrompt creates the knowledge-enhanced editing prompt
func (h *EditorHandler) createEditingPrompt(request *EditRequestEvent, kb *KnowledgeBase, styleGuidelines string, additionalContext string) string {
	var prompt strings.Builder

	prompt.WriteString("Edit and enhance the following article using the research knowledge base for additional insights and improvements.\n\n")
	prompt.WriteString("**Article Subject:** ")
	prompt.WriteString(request.Subject)
	prompt.WriteString("\n")
	prompt.WriteString("**Planned Title:** ")
	prompt.WriteString(request.Title)
	prompt.WriteString("\n\n")

	// Get research overview from knowledge base
	stats := kb.GetStats()
	prompt.WriteString("**Available Research Context:**\n")
	prompt.WriteString(fmt.Sprintf("- Total research documents: %d\n", stats["total_documents"]))
	if sourceTypeCounts, ok := stats["source_type_counts"].(map[string]int); ok {
		prompt.WriteString("- Source types available:\n")
		for sourceType, count := range sourceTypeCounts {
			prompt.WriteString(fmt.Sprintf("  - %s: %d documents\n", sourceType, count))
		}
	}
	prompt.WriteString("\n")

	prompt.WriteString("**Planned sections:**\n")
	for i, s := range request.Plan.Sections {
		if i > 0 {
			prompt.WriteString("\n")
		}

		prompt.WriteString(fmt.Sprintf("%d. %s\n", i+1, s.Title))
		prompt.WriteString(fmt.Sprintf("		- **Description:** %s\n", s.Description))
		prompt.WriteString(fmt.Sprintf("		- **Target words:** %d", s.WordCount))
	}

	prompt.WriteString("**Available Knowledge Base Tools:**\n")
	prompt.WriteString("- `search_knowledge_base`: Search for specific topics or facts\n")

	prompt.WriteString("**Enhanced Editing Instructions:**\n")
	prompt.WriteString("1. Review the entire article for consistency and flow\n")
	prompt.WriteString("2. Use knowledge base tools to find additional supporting information\n")
	prompt.WriteString("3. Enhance content with relevant facts, examples, or insights from research\n")
	prompt.WriteString("4. Create smooth transitions between sections\n")
	prompt.WriteString("5. Strengthen the introduction and conclusion with research insights\n")
	prompt.WriteString("6. Ensure consistent tone and style throughout\n")
	prompt.WriteString("7. Add any missing important information found in the knowledge base\n")
	prompt.WriteString("8. Optimize for readability and engagement\n")
	prompt.WriteString("9. Maintain all factual content and research accuracy\n")

	if styleGuidelines != "" {
		prompt.WriteString("11. Apply and enforce the provided style guidelines throughout the article\n\n")
		prompt.WriteString("**Style Guidelines to Apply:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(styleGuidelines)
		prompt.WriteString("\n```\n\n")
		prompt.WriteString("Ensure the final article consistently follows these style preferences in formatting, tone, structure, and presentation.\n\n")
	} else {
		prompt.WriteString("\n")
	}

	if additionalContext != "" {
		prompt.WriteString("**Additional Context:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(additionalContext)
		prompt.WriteString("\n```\n\n")
		prompt.WriteString("Please consider this additional context when editing and incorporate relevant information as appropriate.\n\n")
	}

	prompt.WriteString("Start by querying the knowledge base for any additional insights that could enhance the article, then provide the complete, edited article in the specified format.")

	prompt.WriteString("\n\n---\n\n")

	prompt.WriteString("**Section Content to Edit:**\n\n")

	// Add each section
	for _, section := range request.Sections {
		prompt.WriteString(fmt.Sprintf("## %s\n\n", section.Title))
		prompt.WriteString(section.Content)
		prompt.WriteString("\n\n")
	}

	return prompt.String()
}

// parseEditedArticle extracts the final article from the editor response
func (h *EditorHandler) parseEditedArticle(ctx context.Context, response string, request *EditRequestEvent) (Document, error) {
	// Extract title from the content
	title := h.extractTitle(response, request.Title)

	// Calculate word count
	wordCount := h.countWords(response)

	// Create the final article document
	article := Document{
		DocumentMetadata: DocumentMetadata{
			Title:     title,
			WordCount: wordCount,
			Keywords:  request.Plan.Keywords,
		},
		Content:  response,
		Sections: request.Sections, // Keep original sections for reference
	}

	return article, nil
}

// extractTitle extracts the article title from the content
func (h *EditorHandler) extractTitle(content, fallbackTitle string) string {
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for markdown title
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}

	return fallbackTitle
}

// countWords provides a simple word count
func (h *EditorHandler) countWords(text string) int {
	if text == "" {
		return 0
	}
	words := strings.Fields(text)
	return len(words)
}

// NewEditorHandler creates a new editor handler
func NewEditorHandler(client llm.ChatCompletionClient) *EditorHandler {
	return &EditorHandler{
		client: client,
	}
}

var _ agent.Handler = &EditorHandler{}
