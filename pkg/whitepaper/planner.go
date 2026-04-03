package whitepaper

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/bornholm/ghostwriter/pkg/article"
	"github.com/invopop/jsonschema"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var plannerPrompts embed.FS

// PlannerHandler generates a WhitePaperPlan from the knowledge base.
type PlannerHandler struct {
	client llm.ChatCompletionClient
}

func (h *PlannerHandler) Handle(ctx context.Context, input agent.Input, emit agent.EmitFunc) error {
	subject := input.Message
	if subject == "" {
		subject = ctxSubject(ctx, "")
	}

	targetWordCount := ctxTargetWordCount(ctx, 10000)

	plan, err := h.generatePlan(ctx, subject, targetWordCount)
	if err != nil {
		return errors.WithStack(err)
	}

	planJSON, err := json.Marshal(plan)
	if err != nil {
		return errors.WithStack(err)
	}

	return emit(agent.NewEvent(agent.EventTypeComplete, &agent.CompleteData{Message: string(planJSON)}))
}

const plannerMaxRetries = 3

func (h *PlannerHandler) generatePlan(ctx context.Context, subject string, targetWordCount int) (WhitePaperPlan, error) {
	kb, hasKB := ctxKnowledgeBase(ctx)
	if !hasKB {
		return WhitePaperPlan{}, errors.New("knowledge base not available in context")
	}

	systemPrompt, err := prompt.FromFS[any](&plannerPrompts, "prompts/planner_system.gotmpl", nil)
	if err != nil {
		return WhitePaperPlan{}, errors.WithStack(err)
	}

	userPrompt := h.buildPlanningPrompt(subject, targetWordCount, kb,
		ctxStyleGuidelines(ctx), ctxAdditionalContext(ctx))

	schema := h.buildSchema()
	client := agent.ContextClient(ctx, h.client)
	temperature := agent.ContextTemperature(ctx, 0.3)

	var lastErr error
	for attempt := range plannerMaxRetries {
		messages := []llm.Message{
			llm.NewMessage(llm.RoleSystem, systemPrompt),
			llm.NewMessage(llm.RoleUser, userPrompt),
		}

		if attempt > 0 && lastErr != nil {
			// Add a correction instruction so the model understands what went wrong.
			messages = append(messages, llm.NewMessage(llm.RoleAssistant, ""),
				llm.NewMessage(llm.RoleUser, fmt.Sprintf(
					"Your previous plan was invalid: %s\n\nPlease generate a new plan. "+
						"IMPORTANT: the `chapters` array (or `parts[*].chapters`) MUST contain at least 3 entries. "+
						"Do NOT leave chapters or parts empty.",
					lastErr.Error(),
				)),
			)
		}

		response, err := client.ChatCompletion(ctx,
			llm.WithMessages(messages...),
			llm.WithTemperature(temperature),
			llm.WithResponseSchema(schema),
		)
		if err != nil {
			return WhitePaperPlan{}, errors.WithStack(err)
		}

		raw := response.Message().Content()
		plan, err := h.parsePlan(raw, targetWordCount)
		if err != nil {
			slog.WarnContext(ctx, "[PLANNER] plan validation failed, will retry",
				slog.Int("attempt", attempt+1),
				slog.Int("max", plannerMaxRetries),
				slog.Any("error", err),
				slog.String("raw", raw),
			)
			lastErr = err
			continue
		}

		return plan, nil
	}

	return WhitePaperPlan{}, errors.Wrap(lastErr, fmt.Sprintf("plan generation failed after %d attempts", plannerMaxRetries))
}

func (h *PlannerHandler) buildPlanningPrompt(subject string, targetWordCount int, kb article.KnowledgeBase, styleGuidelines, additionalContext string) string {
	var b strings.Builder

	b.WriteString("Create a comprehensive white paper plan based on the research data provided below.\n\n")
	b.WriteString("**Subject:** " + subject + "\n\n")

	researchDocs := kb.GetAllDocuments()
	stats := kb.GetStats()
	fmt.Fprintf(&b, "**Available Research:** %v documents\n\n", stats["total_documents"])

	b.WriteString("**Research Sources:**\n")
	for _, doc := range researchDocs {
		fmt.Fprintf(&b, "- **%s** (%s)\n", doc.Title, doc.SourceType)
	}
	b.WriteString("\n")

	allKeywords := make(map[string]int)
	for _, doc := range researchDocs {
		for _, kw := range doc.Keywords {
			allKeywords[kw]++
		}
	}
	b.WriteString("**Key Topics Identified:**\n")
	for kw, count := range allKeywords {
		fmt.Fprintf(&b, "- %s (%d sources)\n", kw, count)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "**Requirements:**\n- Target word count: %d words\n- Format: professional white paper\n\n", targetWordCount)

	if styleGuidelines != "" {
		b.WriteString("**Style Guidelines:**\n```\n" + styleGuidelines + "\n```\n\n")
	}

	if additionalContext != "" {
		b.WriteString("**Additional Context:**\n```\n" + additionalContext + "\n```\n\n")
	}

	b.WriteString("Provide the white paper plan in the specified JSON format.")
	return b.String()
}

func (h *PlannerHandler) parsePlan(raw string, targetWordCount int) (WhitePaperPlan, error) {
	msg := llm.NewMessage(llm.RoleAssistant, raw)
	plans, err := llm.ParseJSON[WhitePaperPlan](msg)
	if err != nil {
		return WhitePaperPlan{}, errors.WithStack(err)
	}

	plan := plans[0]

	// Ensure chapter IDs and numbers
	chapters := plan.allChapters()
	for i, ch := range chapters {
		if ch.Number == 0 {
			ch.Number = FlexInt(i + 1)
		}
		if ch.ID == "" {
			ch.ID = generateChapterID(ch.Title)
		}
		if ch.WordCount <= 0 {
			ch.WordCount = targetWordCount / max(1, len(chapters))
		}
	}

	if plan.TotalWords == 0 {
		for _, ch := range chapters {
			plan.TotalWords += ch.WordCount
		}
	}

	if err := validatePlan(&plan); err != nil {
		return WhitePaperPlan{}, errors.Wrap(err, "invalid plan")
	}

	return plan, nil
}

func validatePlan(plan *WhitePaperPlan) error {
	chapters := plan.allChapters()
	if len(chapters) == 0 {
		return errors.New("plan has no chapters")
	}
	seenIDs := make(map[string]bool)
	for _, ch := range chapters {
		if ch.WordCount <= 0 {
			return errors.Errorf("chapter %q has invalid word count %d", ch.Title, ch.WordCount)
		}
		if ch.ID == "" {
			return errors.Errorf("chapter %q has empty ID", ch.Title)
		}
		if seenIDs[ch.ID] {
			return errors.Errorf("duplicate chapter ID %q", ch.ID)
		}
		seenIDs[ch.ID] = true
	}
	return nil
}

func (h *PlannerHandler) buildSchema() llm.ResponseSchema {
	r := jsonschema.Reflector{AllowAdditionalProperties: false, DoNotReference: true}
	schema := r.Reflect(&WhitePaperPlan{})
	return llm.NewResponseSchema(
		"white_paper_plan",
		"Structured plan for a professional white paper with chapters, word counts, and guidance",
		schema,
	)
}

func generateChapterID(title string) string {
	clean := strings.ToLower(strings.ReplaceAll(title, " ", "_"))
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return -1
	}, clean)
}

func NewPlannerHandler(client llm.ChatCompletionClient) *PlannerHandler {
	return &PlannerHandler{client: client}
}

var _ agent.Handler = &PlannerHandler{}
