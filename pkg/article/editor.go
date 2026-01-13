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
	tools  []llm.Tool
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

// editArticle processes and enhances the complete article using section-by-section editing
func (h *EditorHandler) editArticle(ctx context.Context, request *EditRequestEvent) (Document, error) {
	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)

	// Emit progress for editing initialization
	tracker.EmitSubProgress(PhaseEditing, "Initializing section-by-section editing with knowledge base access",
		GetPhaseBaseProgress(PhaseEditing), 0.1, EditingWeight, map[string]interface{}{
			"step":           "initialization",
			"sections_count": len(request.Sections),
			"title":          request.Title,
		})

	// Get target word count from context
	targetWordCount := ContextTargetWordCount(ctx, request.Plan.TotalWords)

	// Calculate current word count from sections
	currentWordCount := 0
	for _, section := range request.Sections {
		currentWordCount += h.countWords(section.Content)
	}

	tracker.EmitSubProgress(PhaseEditing, fmt.Sprintf("Processing %d sections individually to preserve context window", len(request.Sections)),
		GetPhaseBaseProgress(PhaseEditing), 0.2, EditingWeight, map[string]interface{}{
			"step":               "section_processing_start",
			"current_word_count": currentWordCount,
			"target_word_count":  targetWordCount,
		})

	// Process each section individually
	editedSections := make([]SectionContent, len(request.Sections))
	totalEditedWords := 0

	for i, section := range request.Sections {
		sectionProgress := 0.2 + (0.6 * float64(i) / float64(len(request.Sections)))

		tracker.EmitSubProgress(PhaseEditing, fmt.Sprintf("Editing section %d/%d: %s", i+1, len(request.Sections), section.Title),
			GetPhaseBaseProgress(PhaseEditing), sectionProgress, EditingWeight, map[string]interface{}{
				"step":          "section_editing",
				"section_index": i,
				"section_title": section.Title,
			})

		// Find the corresponding planned section for target word count
		var plannedSection *DocumentSection
		for _, ps := range request.Plan.Sections {
			if ps.Title == section.Title {
				plannedSection = ps
				break
			}
		}

		// Get previous section for transition context
		var previousSection *SectionContent
		if i > 0 {
			previousSection = &editedSections[i-1]
		}

		editedSection, err := h.editSectionIndependently(ctx, section, plannedSection, request.Subject, targetWordCount, currentWordCount, previousSection)
		if err != nil {
			return Document{}, errors.WithStack(err)
		}

		editedSections[i] = editedSection
		totalEditedWords += editedSection.WordCount
	}

	// Emit progress for content assembly
	tracker.EmitSubProgress(PhaseEditing, "Assembling edited sections into final article",
		GetPhaseBaseProgress(PhaseEditing), 0.8, EditingWeight, map[string]interface{}{
			"step":               "content_assembly",
			"total_edited_words": totalEditedWords,
		})

	// Combine sections into final article
	article, err := h.assembleFinalArticle(ctx, request, editedSections)
	if err != nil {
		return Document{}, errors.WithStack(err)
	}

	// Emit progress for editing completion
	tracker.EmitSubProgress(PhaseEditing, fmt.Sprintf("Section-by-section editing completed: %d words", article.WordCount),
		GetPhaseBaseProgress(PhaseEditing), 1.0, EditingWeight, map[string]interface{}{
			"step":        "editing_complete",
			"final_title": article.Title,
			"word_count":  article.WordCount,
		})

	return article, nil
}

// editSectionIndependently edits a single section with focused context
func (h *EditorHandler) editSectionIndependently(ctx context.Context, section SectionContent, plannedSection *DocumentSection, subject string, totalTargetWords int, totalCurrentWords int, previousSection *SectionContent) (SectionContent, error) {
	// Load the editor system prompt
	systemPrompt, err := prompt.FromFS[any](&editorPrompts, "prompts/editor_system.gotmpl", nil)
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	// Calculate section target word count
	sectionTargetWords := section.WordCount // Default to current
	if plannedSection != nil {
		sectionTargetWords = plannedSection.WordCount
	}

	// Create section-specific editing prompt
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	additionalContext := ContextAdditionalContext(ctx, "")
	userPrompt := h.createSectionEditingPrompt(section, plannedSection, subject, sectionTargetWords, totalTargetWords, totalCurrentWords, styleGuidelines, additionalContext, previousSection)

	// Set up task context for section editing
	taskCtx := task.WithContextMinIterations(ctx, 1)
	taskCtx = task.WithContextMaxIterations(taskCtx, 3) // Fewer iterations for individual sections
	taskCtx = task.WithContextMaxToolIterations(taskCtx, 4)
	taskCtx = agent.WithContextMessages(taskCtx, []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	})

	// Create task handler
	taskHandler := task.NewHandler(h.client)
	editorAgent := agent.New(taskHandler)

	// Start the agent
	if _, _, err := editorAgent.Start(taskCtx); err != nil {
		return SectionContent{}, errors.WithStack(err)
	}
	defer editorAgent.Stop()

	// Execute section editing task
	result, err := task.Do(taskCtx, editorAgent, userPrompt)
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	// Parse the edited section content
	editedContent := strings.TrimSpace(result.Result())
	wordCount := h.countWords(editedContent)

	return SectionContent{
		Title:     section.Title,
		Content:   editedContent,
		WordCount: wordCount,
	}, nil
}

// assembleFinalArticle combines edited sections into the final document
func (h *EditorHandler) assembleFinalArticle(ctx context.Context, request *EditRequestEvent, editedSections []SectionContent) (Document, error) {
	var content strings.Builder

	// Add title
	content.WriteString(fmt.Sprintf("# %s\n\n", request.Title))

	// Add each edited section
	totalWords := 0
	for _, section := range editedSections {
		content.WriteString(fmt.Sprintf("## %s\n\n", section.Title))
		content.WriteString(section.Content)
		content.WriteString("\n\n")
		totalWords += section.WordCount
	}

	// Create the final article document
	article := Document{
		DocumentMetadata: DocumentMetadata{
			Title:     request.Title,
			WordCount: totalWords,
			Keywords:  request.Plan.Keywords,
		},
		Content:  content.String(),
		Sections: editedSections,
	}

	return article, nil
}

// createSectionEditingPrompt creates a focused prompt for individual section editing
func (h *EditorHandler) createSectionEditingPrompt(section SectionContent, plannedSection *DocumentSection, subject string, sectionTargetWords int, totalTargetWords int, totalCurrentWords int, styleGuidelines string, additionalContext string, previousSection *SectionContent) string {
	var prompt strings.Builder

	prompt.WriteString("Edit and enhance this individual section of an article.\n\n")
	prompt.WriteString("**Article Subject:** ")
	prompt.WriteString(subject)
	prompt.WriteString("\n\n")

	prompt.WriteString("**Section Information:**\n")
	prompt.WriteString(fmt.Sprintf("- **Section Title:** %s\n", section.Title))
	prompt.WriteString(fmt.Sprintf("- **Current word count:** %d words\n", section.WordCount))
	prompt.WriteString(fmt.Sprintf("- **Target word count:** %d words\n", sectionTargetWords))

	if plannedSection != nil && len(plannedSection.KeyPoints) > 0 {
		prompt.WriteString("- **Key points to cover:**\n")
		for _, point := range plannedSection.KeyPoints {
			prompt.WriteString(fmt.Sprintf("  - %s\n", point))
		}
	}

	// Word count guidance
	if section.WordCount < sectionTargetWords {
		wordGap := sectionTargetWords - section.WordCount
		prompt.WriteString(fmt.Sprintf("- **Action needed:** Expand by ~%d words with quality content\n", wordGap))
	} else if section.WordCount > sectionTargetWords {
		prompt.WriteString("- **Action needed:** Maintain current length, only reduce truly redundant content\n")
	} else {
		prompt.WriteString("- **Action needed:** Maintain current word count while improving quality\n")
	}

	prompt.WriteString("\n**Context Information:**\n")
	prompt.WriteString(fmt.Sprintf("- **Total article target:** %d words\n", totalTargetWords))
	prompt.WriteString(fmt.Sprintf("- **Total article current:** %d words\n", totalCurrentWords))
	prompt.WriteString("\n")

	prompt.WriteString("**Section Editing Instructions:**\n")
	prompt.WriteString("1. Focus only on this section - do not add other sections\n")
	prompt.WriteString("2. Enhance content quality while respecting word count targets\n")
	prompt.WriteString("3. Use knowledge base tools to find supporting information if needed\n")
	prompt.WriteString("4. Maintain consistent tone and style\n")
	prompt.WriteString("5. Ensure factual accuracy and research-backed content\n")

	if section.WordCount < sectionTargetWords {
		prompt.WriteString("6. **IMPORTANT**: Expand with quality additions:\n")
		prompt.WriteString("   - Add concrete examples and evidence\n")
		prompt.WriteString("   - Enhance explanations with more detail\n")
		prompt.WriteString("   - Include relevant insights from research\n")
		prompt.WriteString("   - Only add content that genuinely improves the section\n")
	} else {
		prompt.WriteString("6. **IMPORTANT**: Preserve content length - avoid unnecessary reduction\n")
	}

	if styleGuidelines != "" {
		prompt.WriteString("\n**Style Guidelines:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(styleGuidelines)
		prompt.WriteString("\n```\n")
	}

	if additionalContext != "" {
		prompt.WriteString("\n**Additional Context:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(additionalContext)
		prompt.WriteString("\n```\n")
	}

	if previousSection != nil {
		prompt.WriteString("\n**Previous Section for Context:**\n")
		prompt.WriteString(fmt.Sprintf("**Title:** %s\n", previousSection.Title))
		prompt.WriteString("**Content (last 200 words):**\n")
		// Include last 200 words of previous section for transition context
		prevWords := strings.Fields(previousSection.Content)
		startIdx := len(prevWords) - 200
		if startIdx < 0 {
			startIdx = 0
		}
		prevContext := strings.Join(prevWords[startIdx:], " ")
		prompt.WriteString(prevContext)
		prompt.WriteString("\n\n")
		prompt.WriteString("**Transition Instructions:**\n")
		prompt.WriteString("- Create a smooth transition from the previous section\n")
		prompt.WriteString("- Ensure logical flow and connection between sections\n")
		prompt.WriteString("- Avoid abrupt topic changes\n\n")
	}

	prompt.WriteString("**Section Content to Edit:**\n\n")
	prompt.WriteString(section.Content)
	prompt.WriteString("\n\nProvide only the enhanced section content (without the section title header).")

	return prompt.String()
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
func NewEditorHandler(client llm.ChatCompletionClient, tools ...llm.Tool) *EditorHandler {
	return &EditorHandler{
		client: client,
		tools:  tools,
	}
}

var _ agent.Handler = &EditorHandler{}
