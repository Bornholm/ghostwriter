package article

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/agent/loop"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/bornholm/ghostwriter/pkg/tool"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var editorPrompts embed.FS

// EditorHandler reviews individual sections and either approves them (with polished
// content) or returns revision feedback for the writer.
type EditorHandler struct {
	client llm.ChatCompletionClient
	tools  []llm.Tool
}

// reviewSection runs the reviewer agent on a single section.
// It returns a SectionReview: Approved=true with polished content, or
// Approved=false with actionable feedback for the writer.
func (h *EditorHandler) reviewSection(
	ctx context.Context,
	section SectionContent,
	plannedSection *DocumentSection,
	subject string,
	draft string,
	emit agent.EmitFunc,
) (SectionReview, error) {
	systemPrompt, err := prompt.FromFS[any](&editorPrompts, "prompts/editor_review.gotmpl", nil)
	if err != nil {
		return SectionReview{}, errors.WithStack(err)
	}

	styleGuidelines := ContextStyleGuidelines(ctx, "")
	additionalContext := ContextAdditionalContext(ctx, "")

	// Give the reviewer access to the knowledge base and the document draft
	kb, hasKB := ContextKnowledgeBase(ctx)
	tools := append(h.tools, tool.NewQueryDocumentTool(draft))
	if hasKB {
		tools = append(tools, NewSearchKnowledgeBaseTool(kb))
	}

	userPrompt := h.createReviewPrompt(section, plannedSection, subject, styleGuidelines, additionalContext)

	loopHandler, err := loop.NewHandler(
		loop.WithClient(h.client),
		loop.WithSystemPrompt(systemPrompt),
		loop.WithTools(tools...),
		loop.WithMaxIterations(5),
	)
	if err != nil {
		return SectionReview{}, errors.WithStack(err)
	}

	var reviewJSON string
	innerEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			reviewJSON = strings.TrimSpace(evt.Data().(*agent.CompleteData).Message)
		}
		return emit(evt)
	}

	if err := agent.NewRunner(loopHandler).Run(ctx, agent.NewInput(userPrompt), innerEmit); err != nil {
		return SectionReview{}, errors.WithStack(err)
	}

	// Strip potential markdown code fences the LLM might wrap around the JSON
	reviewJSON = strings.TrimPrefix(reviewJSON, "```json")
	reviewJSON = strings.TrimPrefix(reviewJSON, "```")
	reviewJSON = strings.TrimSuffix(reviewJSON, "```")
	reviewJSON = strings.TrimSpace(reviewJSON)

	var review SectionReview
	if err := json.Unmarshal([]byte(reviewJSON), &review); err != nil {
		return SectionReview{}, errors.Wrapf(err, "could not parse reviewer JSON output: %s", reviewJSON)
	}

	return review, nil
}

// createReviewPrompt builds the user-facing prompt for the review agent.
func (h *EditorHandler) createReviewPrompt(
	section SectionContent,
	plannedSection *DocumentSection,
	subject string,
	styleGuidelines string,
	additionalContext string,
) string {
	var p strings.Builder

	p.WriteString("Review the following section of the article.\n\n")
	p.WriteString("**Article subject:** ")
	p.WriteString(subject)
	p.WriteString("\n\n")

	p.WriteString("**Section information:**\n")
	p.WriteString(fmt.Sprintf("- **Title:** %s\n", section.Title))
	p.WriteString(fmt.Sprintf("- **Word count:** %d words\n", section.WordCount))

	if plannedSection != nil {
		p.WriteString(fmt.Sprintf("- **Target word count:** %d words\n", plannedSection.WordCount))
		if len(plannedSection.KeyPoints) > 0 {
			p.WriteString("- **Key points to cover:**\n")
			for _, kp := range plannedSection.KeyPoints {
				p.WriteString(fmt.Sprintf("  - %s\n", kp))
			}
		}
		if plannedSection.Description != "" {
			p.WriteString(fmt.Sprintf("- **Section description:** %s\n", plannedSection.Description))
		}
	}

	if styleGuidelines != "" {
		p.WriteString("\n**Style guidelines:**\n```\n")
		p.WriteString(styleGuidelines)
		p.WriteString("\n```\n")
		p.WriteString("\n**CRITICAL**: Write the approved `content` strictly in the language specified in the style guidelines. Never mix languages.\n")
	}

	if additionalContext != "" {
		p.WriteString("\n**Additional context:**\n```\n")
		p.WriteString(additionalContext)
		p.WriteString("\n```\n")
	}

	p.WriteString("\n**Section content to review:**\n\n")
	p.WriteString(section.Content)
	p.WriteString("\n\n---\n\n")
	p.WriteString("Use `query_document` to check for redundancies in the already-validated sections before deciding.\n")
	p.WriteString("Return ONLY a valid JSON object — no text before or after.")

	return p.String()
}

// assembleFinalArticle assembles the validated sections into a Document.
func assembleFinalArticle(title string, plan DocumentPlan, sections []SectionContent) Document {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("# %s\n\n", title))

	totalWords := 0
	for _, section := range sections {
		content.WriteString(fmt.Sprintf("## %s\n\n", section.Title))
		content.WriteString(section.Content)
		content.WriteString("\n\n")
		totalWords += section.WordCount
	}

	return Document{
		DocumentMetadata: DocumentMetadata{
			Title:     title,
			WordCount: totalWords,
			Keywords:  plan.Keywords,
		},
		Content:  content.String(),
		Sections: sections,
	}
}

// NewEditorHandler creates a new editor/reviewer handler.
func NewEditorHandler(client llm.ChatCompletionClient, tools ...llm.Tool) *EditorHandler {
	return &EditorHandler{
		client: client,
		tools:  tools,
	}
}
