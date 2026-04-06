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
	"github.com/bornholm/ghostwriter/pkg/article"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var citationLinkerPrompts embed.FS

const citationLinkerLoopMaxIterations = 4

// CitationLinkerHandler enriches a chapter by adding inline citation links
// to factual claims, pointing to source documents from the knowledge base.
type CitationLinkerHandler struct {
	client llm.ChatCompletionClient
}

func (h *CitationLinkerHandler) Handle(ctx context.Context, input agent.Input, emit agent.EmitFunc) error {
	ks, ok := ctxSearcher(ctx)
	sources := extractSources(ctx)

	// If no knowledge base is available, return the chapter unchanged
	if !ok || len(sources) == 0 {
		return emit(agent.NewEvent(agent.EventTypeComplete, &agent.CompleteData{Message: input.Message}))
	}

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

	systemPrompt, err := prompt.FromFS[any](&citationLinkerPrompts, "prompts/citation_linker_system.gotmpl", nil)
	if err != nil {
		return errors.WithStack(err)
	}

	userPrompt := h.buildPrompt(content, sources, ctxStyleGuidelines(ctx))

	loopHandler, err := loop.NewHandler(
		loop.WithClient(h.client),
		loop.WithSystemPrompt(systemPrompt),
		loop.WithTools(NewSearchKnowledgeBaseTool(ks)),
		loop.WithMaxIterations(citationLinkerLoopMaxIterations),
	)
	if err != nil {
		return errors.WithStack(err)
	}

	var linkedContent string
	innerEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			linkedContent = strings.TrimSpace(evt.Data().(*agent.CompleteData).Message)
		}
		return emit(evt)
	}

	if err := agent.NewRunner(loopHandler).Run(ctx, agent.NewInput(userPrompt), innerEmit); err != nil {
		return errors.WithStack(err)
	}

	return emit(agent.NewEvent(agent.EventTypeComplete, &agent.CompleteData{Message: linkedContent}))
}

func (h *CitationLinkerHandler) buildPrompt(content ChapterContent, sources []article.Source, styleGuidelines string) string {
	var b strings.Builder

	b.WriteString("Add inline citation links to the following white paper chapter.\n\n")

	if content.Title != "" {
		fmt.Fprintf(&b, "**Chapter Title:** %s\n\n", content.Title)
	}

	b.WriteString(h.formatSources(sources))
	b.WriteString("\n")

	b.WriteString("Use `search_knowledge_base` to find the source URL for any claim not covered by the list above.\n\n")

	if styleGuidelines != "" {
		b.WriteString("**Style Guidelines:**\n```\n" + styleGuidelines + "\n```\n\n")
		b.WriteString("**CRITICAL**: Write the ENTIRE chapter in the language specified above. Never mix languages.\n\n")
	}

	b.WriteString("**Chapter Content:**\n\n")
	b.WriteString(content.Content)
	b.WriteString("\n\nProvide ONLY the enhanced chapter content (without the # chapter title heading). No meta-commentary, no horizontal rules (`---`).")

	return b.String()
}

func (h *CitationLinkerHandler) formatSources(sources []article.Source) string {
	var b strings.Builder
	b.WriteString("**Available Sources:**\n")

	seen := make(map[string]bool)
	count := 0

	for _, s := range sources {
		if s.URL == "" || seen[s.URL] || count >= 50 {
			continue
		}
		seen[s.URL] = true

		title := s.Title
		if title == "" {
			title = s.URL
		}

		keywords := strings.Join(s.Keywords, ", ")
		if keywords != "" {
			fmt.Fprintf(&b, "- [%s](%s) — %s — keywords: %s\n", title, s.URL, s.SourceType, keywords)
		} else {
			fmt.Fprintf(&b, "- [%s](%s) — %s\n", title, s.URL, s.SourceType)
		}

		count++
	}

	return b.String()
}

func NewCitationLinkerHandler(client llm.ChatCompletionClient) *CitationLinkerHandler {
	return &CitationLinkerHandler{client: client}
}

var _ agent.Handler = &CitationLinkerHandler{}
