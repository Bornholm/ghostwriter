package article

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/agent/task"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/bornholm/ghostwriter/pkg/tool"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var writerPrompts embed.FS

// WriterHandler handles section writing requests
type WriterHandler struct {
	client          llm.ChatCompletionClient
	tools           []llm.Tool
	sourceExtractor *SourceExtractor
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
	writingCtx = agent.WithContextTools(writingCtx, h.tools)

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

	// Write the section content
	content, err := h.writeSection(writingCtx, section, subject)
	if err != nil {
		return errors.WithStack(err)
	}

	// Create and send the section content event
	contentEvent := NewSectionContentEvent(ctx, content, assignmentEvent)
	outputs <- contentEvent

	return nil
}

// writeSection creates content for a specific section
func (h *WriterHandler) writeSection(ctx context.Context, section DocumentSection, subject string) (SectionContent, error) {
	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)
	writerID := ContextWriterID(ctx, "writer")

	// Note: Individual section progress is handled at the orchestrator level
	// This method focuses on the internal steps of writing a single section

	// Emit progress for initialization
	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("[%s] Initializing section: %s", writerID, section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"writer_id":     writerID,
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "initialization",
		})

	// Load the writer system prompt
	systemPrompt, err := prompt.FromFS[any](&writerPrompts, "prompts/writer_system.gotmpl", nil)
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	// Create the section assignment prompt
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	additionalContext := ContextAdditionalContext(ctx, "")
	userPrompt := h.createSectionPrompt(section, subject, styleGuidelines, additionalContext)

	// Emit progress for research start
	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("[%s] Starting research for: %s", writerID, section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"writer_id":     writerID,
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "research_start",
		})

	// Set up the task context for iterative writing with research
	taskCtx := task.WithContextMinIterations(ctx, 2)
	taskCtx = task.WithContextMaxIterations(taskCtx, 5)
	taskCtx = agent.WithContextMessages(taskCtx, []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	})

	// Create a task handler for the writing process
	taskHandler := task.NewHandler(h.client, task.WithDefaultTools(h.tools...))

	// Create a temporary agent for this writing task
	writerAgent := agent.New(taskHandler)

	// Start the agent
	if _, _, err := writerAgent.Start(taskCtx); err != nil {
		return SectionContent{}, errors.WithStack(err)
	}
	defer writerAgent.Stop()

	// Emit progress for writing execution
	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("[%s] Writing content for: %s", writerID, section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"writer_id":     writerID,
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "writing_execution",
		})

	// Execute the writing task
	result, err := task.Do(taskCtx, writerAgent, userPrompt)
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	// Emit progress for content processing
	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("[%s] Processing and extracting sources for: %s", writerID, section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"writer_id":     writerID,
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "content_processing",
		})

	// Parse the result to extract content and sources using the shared utility
	content, sources, err := h.sourceExtractor.ExtractContentAndSources(ctx, result.Result())
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	// Create the section content
	wordCount := h.countWords(content)
	sectionContent := SectionContent{
		SectionID:   section.ID,
		Title:       section.Title,
		Content:     content,
		Sources:     sources,
		WordCount:   wordCount,
		WrittenBy:   writerID,
		CompletedAt: time.Now(),
	}

	// Emit progress for section completion
	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("[%s] Completed section: %s (%d words, %d sources)", writerID, section.Title, wordCount, len(sources)),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"writer_id":         writerID,
			"section_id":        section.ID,
			"section_title":     section.Title,
			"step":              "section_complete",
			"word_count":        wordCount,
			"sources_count":     len(sources),
			"target_word_count": section.WordCount,
		})

	return sectionContent, nil
}

// createSectionPrompt creates the user prompt for section writing
func (h *WriterHandler) createSectionPrompt(section DocumentSection, subject string, styleGuidelines string, additionalContext string) string {
	var prompt strings.Builder

	prompt.WriteString("Write a comprehensive section for an article about: ")
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

	prompt.WriteString("\n**Instructions:**\n")
	prompt.WriteString("1. Research the topic thoroughly using your available tools\n")
	prompt.WriteString("2. Write engaging, well-structured content that covers all key points\n")
	prompt.WriteString("3. Include proper citations and sources\n")
	prompt.WriteString("4. Aim for the target word count\n")
	prompt.WriteString("5. Use a professional yet accessible tone\n")

	if styleGuidelines != "" {
		prompt.WriteString("6. Follow the provided style guidelines carefully\n\n")
		prompt.WriteString("**Style Guidelines:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(styleGuidelines)
		prompt.WriteString("\n```\n\n")
		prompt.WriteString("Ensure your writing adheres to these style preferences throughout the section.\n\n")
	} else {
		prompt.WriteString("\n")
	}

	if additionalContext != "" {
		prompt.WriteString("**Additional Context:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(additionalContext)
		prompt.WriteString("\n```\n\n")
		prompt.WriteString("Please consider this additional context when writing and incorporate relevant information as appropriate.\n\n")
	}

	prompt.WriteString("Please start by researching the topic, then provide your final section content.")

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

// NewWriterHandler creates a new writer handler
func NewWriterHandler(client llm.ChatCompletionClient, tools ...llm.Tool) *WriterHandler {
	if len(tools) == 0 {
		tools = tool.GetDefaultResearchTools()
	}

	return &WriterHandler{
		client:          client,
		tools:           tools,
		sourceExtractor: NewSourceExtractor(client),
	}
}

var _ agent.Handler = &WriterHandler{}
