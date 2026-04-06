package article

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/agent/loop"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var writerPrompts embed.FS

// WriterHandler handles section writing requests
type WriterHandler struct {
	client llm.ChatCompletionClient
	tools  []llm.Tool
}

// Handle implements agent.Handler for writing requests
func (h *WriterHandler) Handle(ctx context.Context, input agent.Input, emit agent.EmitFunc) error {
	section, ok := ContextDocumentSection(ctx)
	if !ok {
		return errors.New("document section not found in context")
	}

	subject := ContextSubject(ctx, "")
	previousSection := ContextPreviousSectionContent(ctx)

	// Write the section content using knowledge base
	content, err := h.writeSection(ctx, section, subject, previousSection, emit)
	if err != nil {
		return errors.WithStack(err)
	}

	// JSON-encode and emit result
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return errors.WithStack(err)
	}

	return emit(agent.NewEvent(agent.EventTypeComplete, &agent.CompleteData{Message: string(contentJSON)}))
}

// writeSection creates content using knowledge base queries
func (h *WriterHandler) writeSection(ctx context.Context, section DocumentSection, subject string, previousSection *SectionContent, emit agent.EmitFunc) (SectionContent, error) {
	tracker := NewProgressTracker(ctx)

	// Get knowledge base from context
	kb, hasKB := ContextKnowledgeBase(ctx)
	if !hasKB {
		return SectionContent{}, errors.New("knowledge base not available in context")
	}

	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("Querying knowledge base for: %s", section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "knowledge_query",
		})

	// Load the writer system prompt
	systemPrompt, err := prompt.FromFS[any](&writerPrompts, "prompts/writer_system.gotmpl", nil, prompt.WithFuncs(template.FuncMap{
		"now": func() string { return time.Now().Format("2006-01-02") },
	}))
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	// Create knowledge-based writing tools
	tools := append(h.tools, NewSearchKnowledgeBaseTool(kb))

	// Create section writing prompt
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	additionalContext := ContextAdditionalContext(ctx, "")
	userPrompt := h.createKnowledgeBasedSectionPrompt(section, subject, styleGuidelines, additionalContext, previousSection)

	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("Writing content using knowledge base: %s", section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "content_writing",
		})

	// Create loop handler for agentic writing
	loopHandler, err := loop.NewHandler(
		loop.WithClient(h.client),
		loop.WithSystemPrompt(systemPrompt),
		loop.WithTools(tools...),
		loop.WithMaxIterations(3),
	)
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	// Capture the written content from EventTypeComplete
	var writtenContent string
	innerEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			writtenContent = evt.Data().(*agent.CompleteData).Message
		}
		return emit(evt)
	}

	if err := agent.NewRunner(loopHandler).Run(ctx, agent.NewInput(userPrompt), innerEmit); err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("Processing content: %s", section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"section_id":    section.ID,
			"section_title": section.Title,
			"step":          "content_processing",
		})

	wordCount := h.countWords(writtenContent)
	sectionContent := SectionContent{
		Title:     section.Title,
		Content:   writtenContent,
		WordCount: wordCount,
	}

	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("Completed section: %s (%d words)", section.Title, wordCount),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"section_id":        section.ID,
			"section_title":     section.Title,
			"step":              "section_complete",
			"word_count":        wordCount,
			"target_word_count": section.WordCount,
		})

	return sectionContent, nil
}

// createKnowledgeBasedSectionPrompt creates prompt for knowledge-based writing
func (h *WriterHandler) createKnowledgeBasedSectionPrompt(section DocumentSection, subject string, styleGuidelines string, additionalContext string, previousSection *SectionContent) string {
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
	prompt.WriteString("- `search_knowledge_base`: Full-text search across knowledge base\n")
	prompt.WriteString("- `scrape_webpage`: Fetch a web resource content by its URL\n\n")

	prompt.WriteString("**Instructions:**\n")
	prompt.WriteString("1. Use the knowledge base tools to gather relevant information for this section\n")
	prompt.WriteString("2. Query for facts, examples, and supporting data related to the key points\n")
	prompt.WriteString("3. Write engaging, well-structured content based on the research data\n")
	prompt.WriteString("4. Aim for the target word count\n")
	prompt.WriteString("5. Use a professional yet accessible tone\n")
	prompt.WriteString("6. Do NOT add a conclusion or closing paragraph at the end of the section\n")

	if styleGuidelines != "" {
		prompt.WriteString("7. Follow the provided style guidelines carefully\n\n")
		prompt.WriteString("**Style Guidelines:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(styleGuidelines)
		prompt.WriteString("\n```\n\n")
		prompt.WriteString("**CRITICAL**: Respect strictly the language specified in the style guidelines above. Write the ENTIRE section content in that language only. Never mix languages.\n\n")
	}

	if additionalContext != "" {
		prompt.WriteString("**Additional Context:**\n")
		prompt.WriteString("```\n")
		prompt.WriteString(additionalContext)
		prompt.WriteString("\n```\n\n")
	}

	if previousSection != nil {
		prompt.WriteString("**Previous section:**\n")
		prompt.WriteString("- **Title:** ")
		prompt.WriteString(previousSection.Title)
		prompt.WriteString("\n")
		prompt.WriteString("- **Content:**\n")
		prompt.WriteString(previousSection.Content)

		prompt.WriteString("\n---\n\n")
	}

	prompt.WriteString("Start by querying the knowledge base for relevant information, then write your section content.")

	return prompt.String()
}

// reviseSection rewrites a section incorporating the reviewer's feedback.
func (h *WriterHandler) reviseSection(ctx context.Context, section DocumentSection, subject string, previousSection *SectionContent, previousContent string, feedback string, round int, emit agent.EmitFunc) (SectionContent, error) {
	tracker := NewProgressTracker(ctx)

	kb, hasKB := ContextKnowledgeBase(ctx)
	if !hasKB {
		return SectionContent{}, errors.New("knowledge base not available in context")
	}

	tracker.EmitProgress(PhaseWriting, fmt.Sprintf("Revising section (round %d): %s", round, section.Title),
		GetPhaseBaseProgress(PhaseWriting), map[string]interface{}{
			"section_id":    section.ID,
			"section_title": section.Title,
			"round":         round,
		})

	systemPrompt, err := prompt.FromFS[any](&writerPrompts, "prompts/writer_system.gotmpl", nil, prompt.WithFuncs(template.FuncMap{
		"now": func() string { return time.Now().Format("2006-01-02") },
	}))
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	tools := append(h.tools, NewSearchKnowledgeBaseTool(kb))

	styleGuidelines := ContextStyleGuidelines(ctx, "")
	additionalContext := ContextAdditionalContext(ctx, "")
	userPrompt := h.createRevisionPrompt(section, subject, styleGuidelines, additionalContext, previousSection, previousContent, feedback, round)

	loopHandler, err := loop.NewHandler(
		loop.WithClient(h.client),
		loop.WithSystemPrompt(systemPrompt),
		loop.WithTools(tools...),
		loop.WithMaxIterations(3),
	)
	if err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	var writtenContent string
	innerEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			writtenContent = evt.Data().(*agent.CompleteData).Message
		}
		return emit(evt)
	}

	if err := agent.NewRunner(loopHandler).Run(ctx, agent.NewInput(userPrompt), innerEmit); err != nil {
		return SectionContent{}, errors.WithStack(err)
	}

	wordCount := h.countWords(writtenContent)
	return SectionContent{
		Title:     section.Title,
		Content:   writtenContent,
		WordCount: wordCount,
	}, nil
}

// createRevisionPrompt builds the prompt for a revision round.
func (h *WriterHandler) createRevisionPrompt(section DocumentSection, subject string, styleGuidelines string, additionalContext string, previousSection *SectionContent, previousContent string, feedback string, round int) string {
	var p strings.Builder

	p.WriteString(fmt.Sprintf("Revise this section based on the reviewer's feedback (revision round %d).\n\n", round))
	p.WriteString("**Article subject:** ")
	p.WriteString(subject)
	p.WriteString("\n\n")

	p.WriteString("**Section:**\n")
	p.WriteString(fmt.Sprintf("- **Title:** %s\n", section.Title))
	p.WriteString(fmt.Sprintf("- **Target word count:** %d words\n", section.WordCount))

	if len(section.KeyPoints) > 0 {
		p.WriteString("- **Key points to cover:**\n")
		for _, kp := range section.KeyPoints {
			p.WriteString(fmt.Sprintf("  - %s\n", kp))
		}
	}

	p.WriteString(fmt.Sprintf("\n**Reviewer feedback (round %d):**\n", round))
	p.WriteString(feedback)
	p.WriteString("\n\n")

	p.WriteString("**Previous version of the section (to revise):**\n\n")
	p.WriteString(previousContent)
	p.WriteString("\n\n")

	p.WriteString("**Instructions:**\n")
	p.WriteString("1. Address ALL points in the reviewer's feedback\n")
	p.WriteString("2. Preserve content that the reviewer did not ask to change\n")
	p.WriteString("3. Use the knowledge base to find supporting data if the reviewer requested more evidence\n")
	p.WriteString("4. Do NOT add a conclusion paragraph at the end of the section\n")

	if styleGuidelines != "" {
		p.WriteString("5. Follow the provided style guidelines carefully\n\n")
		p.WriteString("**Style guidelines:**\n```\n")
		p.WriteString(styleGuidelines)
		p.WriteString("\n```\n\n")
		p.WriteString("**CRITICAL**: Write the ENTIRE section content in the language specified in the style guidelines. Never mix languages.\n\n")
	}

	if additionalContext != "" {
		p.WriteString("**Additional context:**\n```\n")
		p.WriteString(additionalContext)
		p.WriteString("\n```\n\n")
	}

	if previousSection != nil {
		p.WriteString("**Previous section (for continuity):**\n")
		p.WriteString(fmt.Sprintf("- **Title:** %s\n", previousSection.Title))
		prevWords := strings.Fields(previousSection.Content)
		startIdx := len(prevWords) - 200
		if startIdx < 0 {
			startIdx = 0
		}
		p.WriteString(strings.Join(prevWords[startIdx:], " "))
		p.WriteString("\n\n")
	}

	p.WriteString("Output only the revised section content (no section title header).")

	return p.String()
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
	return &WriterHandler{
		client: client,
		tools:  tools,
	}
}

var _ agent.Handler = &WriterHandler{}
