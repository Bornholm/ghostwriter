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
	"github.com/bornholm/ghostwriter/pkg/scraper/surf"
	"github.com/bornholm/ghostwriter/pkg/tool"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var writerPrompts embed.FS

const writerLoopMaxIterations = 6

// ChapterWriterHandler writes a single chapter of the white paper.
type ChapterWriterHandler struct {
	client llm.ChatCompletionClient
	tools  []llm.Tool
}

func (h *ChapterWriterHandler) Handle(ctx context.Context, input agent.Input, emit agent.EmitFunc) error {
	chapter, ok := ctxChapter(ctx)
	if !ok {
		return errors.New("chapter not found in context")
	}

	subject := ctxSubject(ctx, input.Message)
	previous := ctxPreviousChapter(ctx)

	ks, ok := ctxSearcher(ctx)
	if !ok {
		return errors.New("knowledge searcher not found in context")
	}

	content, err := h.writeChapter(ctx, chapter, subject, ks, previous, emit)
	if err != nil {
		return errors.WithStack(err)
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return errors.WithStack(err)
	}

	return emit(agent.NewEvent(agent.EventTypeComplete, &agent.CompleteData{Message: string(contentJSON)}))
}

func (h *ChapterWriterHandler) writeChapter(ctx context.Context, chapter *Chapter, subject string, ks KnowledgeSearcher, previous *ChapterContent, emit agent.EmitFunc) (ChapterContent, error) {
	systemPrompt, err := prompt.FromFS[any](&writerPrompts, "prompts/writer_system.gotmpl", nil)
	if err != nil {
		return ChapterContent{}, errors.WithStack(err)
	}

	tools := append(h.tools, NewSearchKnowledgeBaseTool(ks))

	plan, _ := ctxPlan(ctx)
	userPrompt := h.buildPrompt(chapter, plan, subject, ctxStyleGuidelines(ctx), ctxAdditionalContext(ctx), previous)

	loopHandler, err := loop.NewHandler(
		loop.WithClient(h.client),
		loop.WithSystemPrompt(systemPrompt),
		loop.WithTools(tools...),
		loop.WithMaxIterations(writerLoopMaxIterations),
	)
	if err != nil {
		return ChapterContent{}, errors.WithStack(err)
	}

	var writtenContent string
	innerEmit := func(evt agent.Event) error {
		if evt.Type() == agent.EventTypeComplete {
			writtenContent = strings.TrimSpace(evt.Data().(*agent.CompleteData).Message)
		}
		return emit(evt)
	}

	if err := agent.NewRunner(loopHandler).Run(ctx, agent.NewInput(userPrompt), innerEmit); err != nil {
		return ChapterContent{}, errors.WithStack(err)
	}

	return ChapterContent{
		ChapterID: chapter.ID,
		Number:    int(chapter.Number),
		Title:     chapter.Title,
		Content:   writtenContent,
		WordCount: countWords(writtenContent),
	}, nil
}

func (h *ChapterWriterHandler) buildPrompt(ch *Chapter, plan WhitePaperPlan, subject, styleGuidelines, additionalContext string, previous *ChapterContent) string {
	var b strings.Builder

	b.WriteString("Write chapter " + fmt.Sprintf("%d", ch.Number) + " of a professional white paper.\n\n")
	b.WriteString("**White Paper Subject:** " + subject + "\n\n")

	if plan.CentralArgument != "" {
		b.WriteString("**Central Argument of the White Paper:** " + plan.CentralArgument + "\n\n")
	}

	if len(plan.Objectives) > 0 {
		b.WriteString("**White Paper Objectives (what every chapter must support):**\n")
		for _, obj := range plan.Objectives {
			b.WriteString("- " + obj + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("**Chapter Assignment:**\n")
	fmt.Fprintf(&b, "- **Title:** %s\n", ch.Title)
	fmt.Fprintf(&b, "- **Description:** %s\n", ch.Description)
	fmt.Fprintf(&b, "- **Target Word Count:** %d words\n", ch.WordCount)

	if len(ch.KeyPoints) > 0 {
		b.WriteString("- **Key Points to Cover:**\n")
		for _, kp := range ch.KeyPoints {
			b.WriteString("  - " + kp + "\n")
		}
	}

	if len(ch.Sections) > 0 {
		b.WriteString("- **Subsections:**\n")
		for _, s := range ch.Sections {
			fmt.Fprintf(&b, "  - **%s**: %s\n", s.Title, s.Description)
		}
	}

	b.WriteString("\n")

	if styleGuidelines != "" {
		b.WriteString("**Style Guidelines:**\n```\n" + styleGuidelines + "\n```\n\n")
		b.WriteString("**CRITICAL**: Write the ENTIRE chapter in the language specified above. Never mix languages.\n\n")
	}

	if additionalContext != "" {
		b.WriteString("**Additional Context:**\n```\n" + additionalContext + "\n```\n\n")
	}

	if previous != nil {
		b.WriteString("**Previous Chapter (for continuity):**\n")
		fmt.Fprintf(&b, "- Title: %s\n", previous.Title)
		words := strings.Fields(previous.Content)
		start := len(words) - 200
		if start < 0 {
			start = 0
		}
		b.WriteString("- Last 200 words:\n")
		b.WriteString(strings.Join(words[start:], " "))
		b.WriteString("\n\n")
		b.WriteString("Ensure a smooth logical transition from the previous chapter.\n\n")
	}

	b.WriteString("Start by querying the knowledge base for relevant information, then write the chapter.")
	return b.String()
}

func countWords(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Fields(text))
}

func NewChapterWriterHandler(client llm.ChatCompletionClient, extraTools ...llm.Tool) *ChapterWriterHandler {
	scraper := surf.NewScraper()
	defaultTools := []llm.Tool{tool.NewScrapeWebpageTool(scraper)}
	return &ChapterWriterHandler{
		client: client,
		tools:  append(defaultTools, extraTools...),
	}
}

var _ agent.Handler = &ChapterWriterHandler{}
