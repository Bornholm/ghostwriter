package article

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var editorPrompts embed.FS

// EditorHandler handles article editing and finalization
type EditorHandler struct {
	client          llm.ChatCompletionClient
	sourceExtractor *SourceExtractor
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

	// Edit and finalize the article
	article, err := h.editArticle(ctx, editRequest)
	if err != nil {
		return errors.WithStack(err)
	}

	// Create and send the final article event
	articleEvent := NewFinalArticleEvent(ctx, article, editRequest)
	outputs <- articleEvent

	return nil
}

// editArticle processes and enhances the complete article
func (h *EditorHandler) editArticle(ctx context.Context, request *EditRequestEvent) (ArticleDocument, error) {
	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)

	// Emit progress for editing initialization
	tracker.EmitSubProgress(PhaseEditing, "Initializing article editing process",
		GetPhaseBaseProgress(PhaseEditing), 0.1, EditingWeight, map[string]interface{}{
			"step":           "initialization",
			"sections_count": len(request.Sections),
			"title":          request.Title,
		})

	// Load the editor system prompt
	systemPrompt, err := prompt.FromFS[any](&editorPrompts, "prompts/editor_system.gotmpl", nil)
	if err != nil {
		return ArticleDocument{}, errors.WithStack(err)
	}

	// Emit progress for prompt creation
	tracker.EmitSubProgress(PhaseEditing, "Preparing editing instructions and content review",
		GetPhaseBaseProgress(PhaseEditing), 0.2, EditingWeight, map[string]interface{}{
			"step": "prompt_creation",
		})

	// Create the editing prompt with all sections
	userPrompt := h.createEditingPrompt(request)

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	}

	// Get client from context or use default
	client := agent.ContextClient(ctx, h.client)
	temperature := agent.ContextTemperature(ctx, 0.2) // Lower temperature for editing

	// Emit progress for editing execution
	tracker.EmitSubProgress(PhaseEditing, "Reviewing content flow and enhancing article structure",
		GetPhaseBaseProgress(PhaseEditing), 0.4, EditingWeight, map[string]interface{}{
			"step": "editing_execution",
		})

	// Make the LLM call
	response, err := client.ChatCompletion(ctx,
		llm.WithMessages(messages...),
		llm.WithTemperature(temperature),
	)
	if err != nil {
		return ArticleDocument{}, errors.WithStack(err)
	}

	// Emit progress for content processing
	tracker.EmitSubProgress(PhaseEditing, "Processing edited content and consolidating sources",
		GetPhaseBaseProgress(PhaseEditing), 0.7, EditingWeight, map[string]interface{}{
			"step": "content_processing",
		})

	// Parse the edited article using the shared utility
	article, err := h.parseEditedArticle(ctx, response.Message().Content(), request)
	if err != nil {
		return ArticleDocument{}, errors.WithStack(err)
	}

	// Emit progress for editing completion
	tracker.EmitSubProgress(PhaseEditing, fmt.Sprintf("Article editing completed: %d words, %d sources", article.WordCount, len(article.Sources)),
		GetPhaseBaseProgress(PhaseEditing), 1.0, EditingWeight, map[string]interface{}{
			"step":          "editing_complete",
			"final_title":   article.Title,
			"word_count":    article.WordCount,
			"sources_count": len(article.Sources),
		})

	return article, nil
}

// createEditingPrompt creates the user prompt for editing
func (h *EditorHandler) createEditingPrompt(request *EditRequestEvent) string {
	var prompt strings.Builder

	prompt.WriteString("Please edit and finalize the following article:\n\n")
	prompt.WriteString("**Article Subject:** ")
	prompt.WriteString(request.Subject)
	prompt.WriteString("\n")
	prompt.WriteString("**Planned Title:** ")
	prompt.WriteString(request.Title)
	prompt.WriteString("\n\n")

	prompt.WriteString("**Original Plan Summary:**\n")
	prompt.WriteString(request.Plan.Summary)
	prompt.WriteString("\n\n")

	prompt.WriteString("**Section Content to Edit:**\n\n")

	// Add each section
	for i, section := range request.Sections {
		prompt.WriteString(fmt.Sprintf("### Section %d: %s\n\n", i+1, section.Title))
		prompt.WriteString(section.Content)
		prompt.WriteString("\n\n")

		if len(section.Sources) > 0 {
			prompt.WriteString("**Section Sources:**\n")
			for _, source := range section.Sources {
				prompt.WriteString("- ")
				prompt.WriteString(source)
				prompt.WriteString("\n")
			}
			prompt.WriteString("\n")
		}
	}

	prompt.WriteString("**Editing Instructions:**\n")
	prompt.WriteString("1. Review the entire article for consistency and flow\n")
	prompt.WriteString("2. Create smooth transitions between sections\n")
	prompt.WriteString("3. Enhance the introduction and conclusion\n")
	prompt.WriteString("4. Ensure consistent tone and style throughout\n")
	prompt.WriteString("5. Consolidate and properly format all sources\n")
	prompt.WriteString("6. Optimize for readability and engagement\n")
	prompt.WriteString("7. Maintain all factual content and research\n\n")

	prompt.WriteString("Please provide the complete, edited article in the specified format.")

	return prompt.String()
}

// parseEditedArticle extracts the final article from the editor response
func (h *EditorHandler) parseEditedArticle(ctx context.Context, response string, request *EditRequestEvent) (ArticleDocument, error) {
	// First, try to extract content and sources using the shared utility
	content, sources, err := h.sourceExtractor.ExtractContentAndSources(ctx, response)
	if err != nil {
		return ArticleDocument{}, errors.WithStack(err)
	}

	// If no sources were found in the LLM response, consolidate from individual sections
	if len(sources) == 0 {
		var sectionSources [][]string
		for _, section := range request.Sections {
			if len(section.Sources) > 0 {
				sectionSources = append(sectionSources, section.Sources)
			}
		}

		if len(sectionSources) > 0 {
			consolidatedSources, err := h.sourceExtractor.ConsolidateSources(ctx, sectionSources)
			if err != nil {
				return ArticleDocument{}, errors.WithStack(err)
			}
			sources = consolidatedSources
		}
	}

	// Extract title from the content
	title := h.extractTitle(content, request.Title)

	// Add sources section to content if sources exist
	if len(sources) > 0 {
		sourcesSection := h.sourceExtractor.FormatSourcesSection(sources)
		content = content + sourcesSection
	}

	// Calculate word count
	wordCount := h.countWords(content)

	// Create the final article document
	article := ArticleDocument{
		Title:       title,
		Summary:     request.Plan.Summary,
		Content:     content,
		Sections:    request.Sections, // Keep original sections for reference
		Sources:     sources,
		WordCount:   wordCount,
		Keywords:    request.Plan.Keywords,
		CreatedAt:   request.Plan.CreatedAt,
		CompletedAt: time.Now(),
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
		client:          client,
		sourceExtractor: NewSourceExtractor(client),
	}
}

var _ agent.Handler = &EditorHandler{}
