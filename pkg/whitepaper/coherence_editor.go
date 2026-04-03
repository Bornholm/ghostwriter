package whitepaper

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/bornholm/ghostwriter/pkg/article"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var coherencePrompts embed.FS

// CoherenceEditorHandler performs a whole-document coherence pass, generating
// executive summary, abstract, bibliography, and appendices.
type CoherenceEditorHandler struct {
	client llm.ChatCompletionClient
}

func (h *CoherenceEditorHandler) Handle(ctx context.Context, input agent.Input, emit agent.EmitFunc) error {
	chapters, ok := ctxAllChapters(ctx)
	if !ok {
		return errors.New("all chapters not found in context")
	}

	plan, hasPlan := ctxPlan(ctx)
	if !hasPlan {
		return errors.New("plan not found in context")
	}

	sources := extractSources(ctx)

	result, err := h.coherenceEdit(ctx, plan, chapters, sources, emit)
	if err != nil {
		return errors.WithStack(err)
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return errors.WithStack(err)
	}

	return emit(agent.NewEvent(agent.EventTypeComplete, &agent.CompleteData{Message: string(resultJSON)}))
}

func (h *CoherenceEditorHandler) coherenceEdit(ctx context.Context, plan WhitePaperPlan, chapters []ChapterContent, sources []article.Source, _ agent.EmitFunc) (CoherenceEditResult, error) {
	systemPrompt, err := prompt.FromFS[any](&coherencePrompts, "prompts/coherence_editor_system.gotmpl", nil)
	if err != nil {
		return CoherenceEditResult{}, errors.WithStack(err)
	}

	userPrompt := h.buildPrompt(plan, chapters, sources)

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	}

	client := agent.ContextClient(ctx, h.client)
	temperature := agent.ContextTemperature(ctx, 0.3)

	response, err := client.ChatCompletion(ctx,
		llm.WithMessages(messages...),
		llm.WithTemperature(temperature),
	)
	if err != nil {
		return CoherenceEditResult{}, errors.WithStack(err)
	}

	msg := llm.NewMessage(llm.RoleAssistant, response.Message().Content())
	results, err := llm.ParseJSON[CoherenceEditResult](msg)
	if err != nil {
		return CoherenceEditResult{}, errors.Wrap(err, "could not parse coherence edit result")
	}

	return results[0], nil
}

func (h *CoherenceEditorHandler) buildPrompt(plan WhitePaperPlan, chapters []ChapterContent, sources []article.Source) string {
	var b strings.Builder

	b.WriteString("Perform the whole-document coherence pass for the following white paper.\n\n")
	b.WriteString("**Title:** " + plan.Title + "\n")
	if plan.Subtitle != "" {
		b.WriteString("**Subtitle:** " + plan.Subtitle + "\n")
	}
	b.WriteString("**Target Audience:** " + plan.TargetAudience + "\n\n")
	b.WriteString("**Executive Summary Guidance:**\n- " + strings.Join(plan.ExecutiveSummaryGuidance, "\n- ") + "\n\n")

	b.WriteString("**Chapter Summaries (first 300 words each):**\n\n")
	for _, ch := range chapters {
		fmt.Fprintf(&b, "### Chapter %d: %s\n", ch.Number, ch.Title)
		words := strings.Fields(ch.Content)
		end := min(len(words), 300)
		b.WriteString(strings.Join(words[:end], " "))
		if len(words) > 300 {
			b.WriteString(" ...")
		}
		b.WriteString("\n\n")
	}

	if len(plan.AppendixTitles) > 0 {
		b.WriteString("**Planned Appendices:**\n")
		for _, title := range plan.AppendixTitles {
			b.WriteString("- " + title + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("**Available Sources:**\n")
	seen := make(map[string]bool)
	for _, s := range sources {
		if s.URL != "" && !seen[s.URL] {
			seen[s.URL] = true
			fmt.Fprintf(&b, "- [%s](%s) — %s\n", s.Title, s.URL, s.SourceType)
		}
	}
	b.WriteString("\n")

	b.WriteString("Generate the coherence edit result JSON as specified in the system prompt.")
	return b.String()
}

func extractSources(ctx context.Context) []article.Source {
	kb, ok := ctxKnowledgeBase(ctx)
	if !ok {
		return nil
	}
	docs := kb.GetAllDocuments()
	sources := make([]article.Source, 0, len(docs))
	for _, d := range docs {
		sources = append(sources, article.Source{
			URL:        d.URL,
			Title:      d.Title,
			Keywords:   d.Keywords,
			SourceType: d.SourceType,
			Relevance:  d.Relevance,
		})
	}
	return sources
}

func NewCoherenceEditorHandler(client llm.ChatCompletionClient) *CoherenceEditorHandler {
	return &CoherenceEditorHandler{client: client}
}

var _ agent.Handler = &CoherenceEditorHandler{}
