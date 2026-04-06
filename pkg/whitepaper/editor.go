package whitepaper

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

const editorLoopMaxIterations = 4

// ChapterEditorHandler edits a single chapter of the white paper.
type ChapterEditorHandler struct {
	client llm.ChatCompletionClient
}

func (h *ChapterEditorHandler) Handle(ctx context.Context, input agent.Input, emit agent.EmitFunc) error {
	chapter, ok := ctxChapter(ctx)
	if !ok {
		return errors.New("chapter not found in context")
	}

	ks, _ := ctxSearcher(ctx)

	// Get written content from input
	content := ChapterContent{}
	if err := json.Unmarshal([]byte(input.Message), &content); err != nil {
		// Fallback: treat input as raw Markdown
		content = ChapterContent{
			ChapterID: chapter.ID,
			Number:    int(chapter.Number),
			Title:     chapter.Title,
			Content:   input.Message,
			WordCount: countWords(input.Message),
		}
	}

	plan, _ := ctxPlan(ctx)
	subject := ctxSubject(ctx, "")

	edited, err := h.editChapter(ctx, content, chapter, plan, subject, ks, emit)
	if err != nil {
		return errors.WithStack(err)
	}

	editedJSON, err := json.Marshal(edited)
	if err != nil {
		return errors.WithStack(err)
	}

	return emit(agent.NewEvent(agent.EventTypeComplete, &agent.CompleteData{Message: string(editedJSON)}))
}

func (h *ChapterEditorHandler) editChapter(ctx context.Context, content ChapterContent, chapter *Chapter, plan WhitePaperPlan, subject string, ks KnowledgeSearcher, emit agent.EmitFunc) (ChapterContent, error) {
	systemPrompt, err := prompt.FromFS[any](&editorPrompts, "prompts/editor_system.gotmpl", nil)
	if err != nil {
		return ChapterContent{}, errors.WithStack(err)
	}

	userPrompt := h.buildPrompt(content, chapter, plan, subject, ctxStyleGuidelines(ctx), ctxAdditionalContext(ctx), ctxAnnotations(ctx))

	var tools []llm.Tool
	if ks != nil {
		tools = append(tools, NewSearchKnowledgeBaseTool(ks))
	}

	// If previous chapters are available, expose them via query_document so the
	// editor can detect cross-chapter redundancies.
	if previousChapters, ok := ctxAllChapters(ctx); ok && len(previousChapters) > 0 {
		var combined strings.Builder
		for _, prev := range previousChapters {
			combined.WriteString(prev.Content)
			combined.WriteString("\n\n")
		}
		tools = append(tools, tool.NewQueryDocumentTool(combined.String()))
	}

	loopHandler, err := loop.NewHandler(
		loop.WithClient(h.client),
		loop.WithSystemPrompt(systemPrompt),
		loop.WithTools(tools...),
		loop.WithMaxIterations(editorLoopMaxIterations),
	)
	if err != nil {
		return ChapterContent{}, errors.WithStack(err)
	}

	var editedContent string
	innerEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			editedContent = strings.TrimSpace(evt.Data().(*agent.CompleteData).Message)
		}
		return emit(evt)
	}

	if err := agent.NewRunner(loopHandler).Run(ctx, agent.NewInput(userPrompt), innerEmit); err != nil {
		return ChapterContent{}, errors.WithStack(err)
	}

	return ChapterContent{
		ChapterID: content.ChapterID,
		Number:    content.Number,
		Title:     content.Title,
		Content:   editedContent,
		WordCount: countWords(editedContent),
	}, nil
}

func (h *ChapterEditorHandler) buildPrompt(content ChapterContent, chapter *Chapter, plan WhitePaperPlan, subject, styleGuidelines, additionalContext string, annotations []string) string {
	var b strings.Builder

	b.WriteString("Edit and enhance this chapter of a professional white paper.\n\n")
	b.WriteString("**White Paper Subject:** " + subject + "\n\n")
	b.WriteString("**White Paper Title:** " + plan.Title + "\n\n")

	if plan.CentralArgument != "" {
		b.WriteString("**Central Argument of the White Paper:** " + plan.CentralArgument + "\n\n")
	}

	if len(plan.Objectives) > 0 {
		b.WriteString("**White Paper Objectives (every chapter must support these):**\n")
		for _, obj := range plan.Objectives {
			b.WriteString("- " + obj + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("**Chapter Information:**\n")
	fmt.Fprintf(&b, "- **Title:** %s\n", chapter.Title)
	fmt.Fprintf(&b, "- **Current word count:** %d words\n", content.WordCount)
	fmt.Fprintf(&b, "- **Target word count:** %d words\n", chapter.WordCount)

	if len(chapter.KeyPoints) > 0 {
		b.WriteString("- **Key points to cover:**\n")
		for _, kp := range chapter.KeyPoints {
			b.WriteString("  - " + kp + "\n")
		}
	}

	if len(annotations) > 0 {
		b.WriteString("\n**Reviewer Annotations to Address (PRIORITY):**\n")
		for i, ann := range annotations {
			fmt.Fprintf(&b, "%d. %s\n", i+1, ann)
		}
		b.WriteString("\n**Action needed:** Address ALL reviewer annotations above while maintaining chapter quality.\n")
	} else if content.WordCount < chapter.WordCount {
		gap := chapter.WordCount - content.WordCount
		fmt.Fprintf(&b, "\n**Action needed:** Expand by approximately %d words with quality research-backed content.\n", gap)
	} else {
		b.WriteString("\n**Action needed:** Maintain length; improve quality, transitions, and evidence.\n")
	}

	if styleGuidelines != "" {
		b.WriteString("\n**Style Guidelines:**\n```\n" + styleGuidelines + "\n```\n")
		b.WriteString("\n**CRITICAL**: Write the ENTIRE chapter in the language specified above. Never mix languages.\n")
	}

	if additionalContext != "" {
		b.WriteString("\n**Additional Context:**\n```\n" + additionalContext + "\n```\n")
	}

	b.WriteString("\n**Chapter Content to Edit:**\n\n")
	b.WriteString(content.Content)
	b.WriteString("\n\nProvide ONLY the enhanced chapter content (without the # chapter title heading). No meta-commentary, no horizontal rules (`---`), no closing summary.")
	return b.String()
}

func NewChapterEditorHandler(client llm.ChatCompletionClient) *ChapterEditorHandler {
	return &ChapterEditorHandler{client: client}
}

var _ agent.Handler = &ChapterEditorHandler{}
