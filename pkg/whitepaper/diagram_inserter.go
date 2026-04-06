package whitepaper

import (
	"context"
	"embed"
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
var diagramInserterPrompts embed.FS

const diagramInserterLoopMaxIterations = 3

// DiagramInserterHandler identifies sections of a chapter that would benefit
// from a Mermaid diagram and inserts them inline.
type DiagramInserterHandler struct {
	client llm.ChatCompletionClient
}

func (h *DiagramInserterHandler) Handle(ctx context.Context, input agent.Input, emit agent.EmitFunc) error {
	chapter, _ := ctxChapter(ctx)
	content := ChapterContent{
		Content: input.Message,
	}
	if chapter != nil {
		content.ChapterID = chapter.ID
		content.Number = int(chapter.Number)
		content.Title = chapter.Title
	}
	content.WordCount = countWords(content.Content)

	systemPrompt, err := prompt.FromFS[any](&diagramInserterPrompts, "prompts/diagram_inserter_system.gotmpl", nil)
	if err != nil {
		return errors.WithStack(err)
	}

	userPrompt := h.buildPrompt(content, ctxStyleGuidelines(ctx))

	// Build combined document from all chapters so query_document can scan for existing diagrams
	var tools []llm.Tool
	if allChapters, ok := ctxAllChapters(ctx); ok && len(allChapters) > 0 {
		var combined strings.Builder
		for _, ch := range allChapters {
			combined.WriteString(ch.Content)
			combined.WriteString("\n\n")
		}
		tools = append(tools, tool.NewQueryDocumentTool(combined.String()))
	}

	loopHandler, err := loop.NewHandler(
		loop.WithClient(h.client),
		loop.WithSystemPrompt(systemPrompt),
		loop.WithTools(tools...),
		loop.WithMaxIterations(diagramInserterLoopMaxIterations),
	)
	if err != nil {
		return errors.WithStack(err)
	}

	var enrichedContent string
	innerEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			enrichedContent = strings.TrimSpace(evt.Data().(*agent.CompleteData).Message)
		}
		return emit(evt)
	}

	if err := agent.NewRunner(loopHandler).Run(ctx, agent.NewInput(userPrompt), innerEmit); err != nil {
		return errors.WithStack(err)
	}

	return emit(agent.NewEvent(agent.EventTypeComplete, &agent.CompleteData{Message: enrichedContent}))
}

func (h *DiagramInserterHandler) buildPrompt(content ChapterContent, styleGuidelines string) string {
	var b strings.Builder

	b.WriteString("Identify sections that would genuinely benefit from a Mermaid diagram or a Markdown table, and insert the most appropriate element. Be very selective — only insert where the visual clearly adds comprehension value beyond what the prose already provides.\n\n")

	if content.Title != "" {
		fmt.Fprintf(&b, "**Chapter Title:** %s\n", content.Title)
	}
	if content.Number > 0 {
		fmt.Fprintf(&b, "**Chapter Number:** %d\n", content.Number)
	}
	b.WriteString("\n")

	b.WriteString("Use `query_document` with `{\"selectors\": [\"code[lang=\\\"mermaid\\\"]\", \"table\"]}` to check for existing diagrams and tables in one call before inserting anything.\n\n")

	if styleGuidelines != "" {
		b.WriteString("**Style Guidelines:**\n```\n" + styleGuidelines + "\n```\n\n")
		b.WriteString("**CRITICAL**: Write the ENTIRE chapter in the language specified above. Never mix languages.\n\n")
	}

	b.WriteString("**Chapter Content:**\n\n")
	b.WriteString(content.Content)
	b.WriteString("\n\nProvide ONLY the enhanced chapter content (without the # chapter title heading). No meta-commentary, no horizontal rules (`---`).")

	return b.String()
}

func NewDiagramInserterHandler(client llm.ChatCompletionClient) *DiagramInserterHandler {
	return &DiagramInserterHandler{client: client}
}

var _ agent.Handler = &DiagramInserterHandler{}
