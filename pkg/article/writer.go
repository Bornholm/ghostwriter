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
var writerPrompts embed.FS

// WriterHandler handles section writing requests
type WriterHandler struct {
	client llm.ChatCompletionClient
}

// Handle implements agent.Handler for writing requests
func (h *WriterHandler) Handle(input agent.Event, outputs chan agent.Event) error {
	assignmentEvent, ok := input.(SectionAssignmentEvent)
	if !ok {
		return errors.Wrapf(agent.ErrNotSupported, "event type '%T' not supported by writer", input)
	}

	ctx := input.Context()
	section := assignmentEvent.Section()
	subject := assignmentEvent.Subject()

	// Get context values
	writerID := ContextWriterID(ctx, "writer")
	researchDepth := ContextResearchDepth(ctx, ResearchDeep)
	agentRole := ContextAgentRole(ctx, RoleWriter)

	if agentRole != RoleWriter {
		return errors.New("writer handler can only process writer role events")
	}

	// Create writing context
	writingCtx := WithContextSubject(ctx, subject)
	writingCtx = WithContextWriterID(writingCtx, writerID)
	writingCtx = WithContextResearchDepth(writingCtx, researchDepth)

	// Pass through style guidelines if available
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	if styleGuidelines != "" {
		writingCtx = WithContextStyleGuidelines(writingCtx, styleGuidelines)
	}

	// Pass through additional context if available
	additionalContext := ContextAdditionalContext(ctx, "")
	if additionalContext != "" {
		writingCtx = WithContextAdditionalContext(writingCtx, additionalContext)
	}

	// Write the section content using knowledge base
	content, err := h.writeSection(writingCtx, section, subject)
	if err != nil {
		return errors.WithStack(err)
	}

	// Create and send the section content event
	contentEvent := NewSectionContentEvent(ctx, content, assignmentEvent)
	outputs <- contentEvent

	return nil
}

// writeSectionFromKnowledgeBase creates content using knowledge base queries
func (h *WriterHandler) writeSection(ctx context.Context, section DocumentSection, subject string) (SectionContent, error) {
	tracker := NewProgressTracker(ctx)
	writerID := ContextWriterID(ctx, "writer")

	// Get knowledge base from context
	kb, hasKB := ContextKnowledgeBase(ctx)
	if !hasKB {
		return SectionContent{}, errors.New("knowledge base not available in context")
	}

	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("[%s] Querying knowledge base for: %s", writerID, section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"writer_id":     writerID,
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "knowledge_query",
		})

	// Load the writer system prompt (updated version)
	systemPrompt, err := prompt.FromFS[any](&writerPrompts, "prompts/writer_system.gotmpl", nil)
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	// Create knowledge-based writing tools
	knowledgeTools := []llm.Tool{
		NewSearchKnowledgeBaseTool(kb),
	}

	// Create section writing prompt
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	additionalContext := ContextAdditionalContext(ctx, "")
	userPrompt := h.createKnowledgeBasedSectionPrompt(section, subject, styleGuidelines, additionalContext)

	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("[%s] Writing content using knowledge base: %s", writerID, section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"writer_id":     writerID,
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "content_writing",
		})

	// Set up task context for knowledge-based writing
	taskCtx := task.WithContextMinIterations(ctx, 1) // Fewer iterations since research is done
	taskCtx = task.WithContextMaxIterations(taskCtx, 3)
	taskCtx = task.WithContextMaxToolIterations(taskCtx, 6)
	taskCtx = agent.WithContextMessages(taskCtx, []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	})
	taskCtx = agent.WithContextTools(taskCtx, knowledgeTools)

	// Create task handler
	taskHandler := task.NewHandler(h.client, task.WithDefaultTools(knowledgeTools...))
	writerAgent := agent.New(taskHandler)

	// Start the agent
	if _, _, err := writerAgent.Start(taskCtx); err != nil {
		return SectionContent{}, errors.WithStack(err)
	}
	defer writerAgent.Stop()

	// Execute writing task
	result, err := task.Do(taskCtx, writerAgent, userPrompt)
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("[%s] Processing content and extracting sources: %s", writerID, section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"writer_id":     writerID,
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "content_processing",
		})

	content := result.Result()

	// Create section content
	wordCount := h.countWords(content)
	sectionContent := SectionContent{
		SectionID: section.ID,
		Title:     section.Title,
		Content:   content,
		WordCount: wordCount,
	}

	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("[%s] Completed section: %s (%d words)", writerID, section.Title, wordCount),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"writer_id":         writerID,
			"section_id":        section.ID,
			"section_title":     section.Title,
			"step":              "section_complete",
			"word_count":        wordCount,
			"target_word_count": section.WordCount,
		})

	return sectionContent, nil
}

// createKnowledgeBasedSectionPrompt creates prompt for knowledge-based writing
func (h *WriterHandler) createKnowledgeBasedSectionPrompt(section DocumentSection, subject string, styleGuidelines string, additionalContext string) string {
	var prompt strings.Builder

	prompt.WriteString("Write a comprehensive section using the research knowledge base.\n\n")
	prompt.WriteString("**Article Subject:** ")
	prompt.WriteString(subject)
	prompt.WriteString("\n\n")

	prompt.WriteString("**Section Assignment:**\n")
	prompt.WriteString("- **Title:** ")
	prompt.WriteString(section.Title)
	prompt.WriteString("\n")
	prompt.WriteString("- **Description:** ")
	prompt.WriteString(section.Description)
	prompt.WriteString("\n")
	prompt.WriteString("- **Target Word Count:** ")
	prompt.WriteString(fmt.Sprintf("%d", section.WordCount))
	prompt.WriteString(" words\n")

	if len(section.KeyPoints) > 0 {
		prompt.WriteString("- **Key Points to Cover:**\n")
		for _, point := range section.KeyPoints {
			prompt.WriteString("  - ")
			prompt.WriteString(point)
			prompt.WriteString("\n")
		}
	}

	prompt.WriteString("\n**Available Tools:**\n")
	prompt.WriteString("- `search_knowledge_base`: Full-text search across research\n\n")

	prompt.WriteString("**Instructions:**\n")
	prompt.WriteString("1. Use the knowledge base tools to gather relevant information for this section\n")
	prompt.WriteString("2. Query for facts, examples, and supporting data related to the key points\n")
	prompt.WriteString("3. Write engaging, well-structured content based on the research data\n")
	prompt.WriteString("4. Aim for the target word count\n")
	prompt.WriteString("5. Use a professional yet accessible tone\n")

	if styleGuidelines != "" {
		prompt.WriteString("7. Follow the provided style guidelines carefully\n\n")
		prompt.WriteString("**Style Guidelines:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(styleGuidelines)
		prompt.WriteString("\n```\n\n")
	}

	if additionalContext != "" {
		prompt.WriteString("**Additional Context:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(additionalContext)
		prompt.WriteString("\n```\n\n")
	}

	prompt.WriteString("Start by querying the knowledge base for relevant information, then write your section content.")

	return prompt.String()
}

// countWords provides a simple word count
func (h *WriterHandler) countWords(text string) int {
	if text == "" {
		return 0
	}
	words := strings.Fields(text)
	return len(words)
}

// NewWriterHandler creates a new writer handler (updated)
func NewWriterHandler(client llm.ChatCompletionClient) *WriterHandler {
	return &WriterHandler{
		client: client,
	}
}

var _ agent.Handler = &WriterHandler{}
