package article

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/invopop/jsonschema"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var plannerPrompts embed.FS

// PlannerHandler handles document planning requests
type PlannerHandler struct {
	client llm.ChatCompletionClient
	// Removed: tools []llm.Tool - No longer needs research tools
}

// Handle implements agent.Handler for planning requests
func (h *PlannerHandler) Handle(input agent.Event, outputs chan agent.Event) error {
	messageEvent, ok := input.(agent.MessageEvent)
	if !ok {
		return errors.Wrapf(agent.ErrNotSupported, "event type '%T' not supported by planner", input)
	}

	ctx := input.Context()
	subject := messageEvent.Message()

	// Get context values
	targetWordCount := ContextTargetWordCount(ctx, 1500)
	agentRole := ContextAgentRole(ctx, RolePlanner)

	if agentRole != RolePlanner {
		return errors.New("planner handler can only process planner role events")
	}

	// Create planning context
	planningCtx := WithContextSubject(ctx, subject)
	planningCtx = WithContextTargetWordCount(planningCtx, targetWordCount)

	// Pass through style guidelines if available
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	if styleGuidelines != "" {
		planningCtx = WithContextStyleGuidelines(planningCtx, styleGuidelines)
	}

	// Pass through additional context if available
	additionalContext := ContextAdditionalContext(ctx, "")
	if additionalContext != "" {
		planningCtx = WithContextAdditionalContext(planningCtx, additionalContext)
	}

	// Generate the document plan using knowledge base
	plan, err := h.generatePlan(planningCtx, subject, targetWordCount)
	if err != nil {
		return errors.WithStack(err)
	}

	// Create and send the document plan event
	planEvent := NewDocumentPlanEvent(ctx, plan, subject, messageEvent)
	outputs <- planEvent

	return nil
}

// generatePlan creates a document plan using existing research
func (h *PlannerHandler) generatePlan(ctx context.Context, subject string, targetWordCount int) (DocumentPlan, error) {
	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)

	// Get knowledge base from context
	kb, hasKB := ContextKnowledgeBase(ctx)
	if !hasKB {
		return DocumentPlan{}, errors.New("knowledge base not available in context - research phase must complete first")
	}

	tracker.EmitSubProgress(PhasePlanning, "Analyzing research data for planning",
		GetPhaseBaseProgress(PhasePlanning), 0.1, PlanningWeight, map[string]interface{}{
			"step": "knowledge_analysis",
		})

	// Load the planner system prompt (updated version)
	systemPrompt, err := prompt.FromFS[any](&plannerPrompts, "prompts/planner_system.gotmpl", nil)
	if err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	// Create planning prompt using knowledge base data
	styleGuidelines := ContextStyleGuidelines(ctx, "")
	additionalContext := ContextAdditionalContext(ctx, "")
	userPrompt := h.createPlanningPrompt(subject, targetWordCount, kb, styleGuidelines, additionalContext)

	tracker.EmitSubProgress(PhasePlanning, "Creating document structure from research findings",
		GetPhaseBaseProgress(PhasePlanning), 0.5, PlanningWeight, map[string]interface{}{
			"step": "structure_creation",
		})

	// Create JSON schema for DocumentPlan
	planSchema := h.createDocumentPlanSchema()

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	}

	// Get client from context
	client := agent.ContextClient(ctx, h.client)
	temperature := agent.ContextTemperature(ctx, 0.3)

	// Make LLM call with structured output
	response, err := client.ChatCompletion(ctx,
		llm.WithMessages(messages...),
		llm.WithTemperature(temperature),
		llm.WithResponseSchema(planSchema),
	)
	if err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	tracker.EmitSubProgress(PhasePlanning, "Finalizing document plan",
		GetPhaseBaseProgress(PhasePlanning), 0.8, PlanningWeight, map[string]interface{}{
			"step": "plan_finalization",
		})

	// Parse the plan response
	plan, err := h.parsePlanResponse(response.Message().Content())
	if err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	tracker.EmitSubProgress(PhasePlanning, fmt.Sprintf("Plan completed: %d sections, %d total words", len(plan.Sections), plan.TotalWords),
		GetPhaseBaseProgress(PhasePlanning), 1.0, PlanningWeight, map[string]interface{}{
			"step":           "plan_complete",
			"sections_count": len(plan.Sections),
			"total_words":    plan.TotalWords,
			"title":          plan.Title,
		})

	return plan, nil
}

// createPlanningPrompt creates planning prompt
func (h *PlannerHandler) createPlanningPrompt(subject string, targetWordCount int, kb *KnowledgeBase, styleGuidelines string, additionalContext string) string {
	var prompt strings.Builder

	prompt.WriteString("Create a comprehensive document plan based on the research data provided below.\n\n")
	prompt.WriteString("**Subject:** ")
	prompt.WriteString(subject)
	prompt.WriteString("\n\n")

	// Get research overview from knowledge base
	researchDocs := kb.GetAllDocuments()
	stats := kb.GetStats()

	prompt.WriteString("**Available Research Data:**\n")
	prompt.WriteString(fmt.Sprintf("- Total research documents: %d\n", stats["total_documents"]))

	if sourceTypeCounts, ok := stats["source_type_counts"].(map[string]int); ok {
		prompt.WriteString("- Source types:\n")
		for sourceType, count := range sourceTypeCounts {
			prompt.WriteString(fmt.Sprintf("  - %s: %d documents\n", sourceType, count))
		}
	}
	prompt.WriteString("\n")

	// Include key research findings
	prompt.WriteString("**Key Research Findings:**\n")
	for i, doc := range researchDocs {
		if i >= 5 { // Limit to top 5 documents to avoid prompt bloat
			break
		}
		prompt.WriteString(fmt.Sprintf("- **%s** (%s)", doc.Title, doc.SourceType))
	}
	prompt.WriteString("\n")

	// Include aggregated keywords
	allKeywords := make(map[string]int)
	for _, doc := range researchDocs {
		for _, keyword := range doc.Keywords {
			allKeywords[keyword]++
		}
	}

	prompt.WriteString("**Key Topics Identified:**\n")
	keywordCount := 0
	for keyword, count := range allKeywords {
		if keywordCount >= 10 {
			break
		}
		prompt.WriteString(fmt.Sprintf("- %s (mentioned %d times)\n", keyword, count))
		keywordCount++
	}
	prompt.WriteString("\n")

	prompt.WriteString("**Requirements:**\n")
	prompt.WriteString("- Target word count: ")
	prompt.WriteString(fmt.Sprintf("%d", targetWordCount))
	prompt.WriteString(" words\n")
	prompt.WriteString("- Create a well-structured, engaging article plan\n")
	prompt.WriteString("- Base sections on the research findings provided\n")
	prompt.WriteString("- Ensure comprehensive coverage using available research\n")
	prompt.WriteString("- Design sections that can be written using the knowledge base\n")

	if styleGuidelines != "" {
		prompt.WriteString("- Follow the provided style guidelines throughout the article\n\n")
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

	prompt.WriteString("**Instructions:**\n")
	prompt.WriteString("1. Analyze the research findings to understand the topic scope\n")
	prompt.WriteString("2. Identify the most important aspects covered in the research\n")
	prompt.WriteString("3. Create a logical structure that utilizes the available research\n")
	prompt.WriteString("4. Ensure each section can be written using the knowledge base data\n")
	prompt.WriteString("5. Include research-informed key points for each section\n")
	prompt.WriteString("6. Use the identified keywords and topics in your planning\n\n")

	prompt.WriteString("Please provide your document plan in the specified JSON format.")

	return prompt.String()
}

// parsePlanResponse extracts the document plan from the LLM response
func (h *PlannerHandler) parsePlanResponse(response string) (DocumentPlan, error) {
	// Create a message from the response string for parsing
	message := llm.NewMessage(llm.RoleAssistant, response)

	// First try to parse as the expected DocumentPlan format
	plans, err := llm.ParseJSON[DocumentPlan](message)
	if err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	plan := plans[0]

	if err := h.validatePlan(plan); err == nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	// Generate section IDs if missing
	for i := range plan.Sections {
		if plan.Sections[i].ID == "" {
			plan.Sections[i].ID = generateSectionID(i, plan.Sections[i].Title)
		}
	}

	return plan, nil
}

// validatePlan ensures the document plan is valid
func (h *PlannerHandler) validatePlan(plan DocumentPlan) error {
	if plan.Title == "" {
		return errors.New("document plan must have a title")
	}

	if len(plan.Sections) == 0 {
		return errors.New("document plan must have at least one section")
	}

	totalWords := 0
	for i, section := range plan.Sections {
		if section.Title == "" {
			return errors.Errorf("section %d must have a title", i)
		}
		if section.WordCount <= 0 {
			return errors.Errorf("section %d must have a positive word count", i)
		}
		totalWords += section.WordCount
	}

	// Update total words if not set or inconsistent
	if plan.TotalWords == 0 || plan.TotalWords != totalWords {
		plan.TotalWords = totalWords
	}

	return nil
}

// generateSectionID creates a unique ID for a section
func generateSectionID(index int, title string) string {
	// Create a simple ID from index and title
	cleanTitle := strings.ToLower(strings.ReplaceAll(title, " ", "_"))
	// Remove special characters
	cleanTitle = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return -1
	}, cleanTitle)

	return cleanTitle
}

// createDocumentPlanSchema creates a JSON schema for the DocumentPlan structure
func (h *PlannerHandler) createDocumentPlanSchema() llm.ResponseSchema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}

	// Generate JSON schema from the TaskPlan struct
	schema := reflector.Reflect(&DocumentPlan{})

	return llm.NewResponseSchema(
		"document_plan",
		"A structured document plan for an article with sections, word counts, and guidance",
		schema,
	)
}

// NewPlannerHandler creates a new planner handler (updated)
func NewPlannerHandler(client llm.ChatCompletionClient) *PlannerHandler {
	return &PlannerHandler{
		client: client,
		// No longer needs tools - uses knowledge base instead
	}
}

var _ agent.Handler = &PlannerHandler{}
