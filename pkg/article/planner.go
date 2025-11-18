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
var plannerPrompts embed.FS

// PlannerHandler handles document planning requests
type PlannerHandler struct {
	client llm.ChatCompletionClient
	tools  []llm.Tool
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

	// Generate the document plan with research
	plan, err := h.generatePlanWithResearch(planningCtx, subject, targetWordCount)
	if err != nil {
		return errors.WithStack(err)
	}

	// Create and send the document plan event
	planEvent := NewDocumentPlanEvent(ctx, plan, subject, messageEvent)
	outputs <- planEvent

	return nil
}

// generatePlanWithResearch creates a document plan using research tools and LLM
func (h *PlannerHandler) generatePlanWithResearch(ctx context.Context, subject string, targetWordCount int) (DocumentPlan, error) {
	// Initialize progress tracking
	tracker := NewProgressTracker(ctx)

	// Emit progress for research start
	tracker.EmitSubProgress(PhasePlanning, "Loading planning templates and preparing research",
		GetPhaseBaseProgress(PhasePlanning), 0.1, PlanningWeight, map[string]interface{}{
			"step": "initialization",
		})

	// Load the planner system prompt
	systemPrompt, err := prompt.FromFS[any](&plannerPrompts, "prompts/planner_system.gotmpl", nil)
	if err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	// Create the research and planning prompt
	userPrompt := h.createResearchPlanningPrompt(subject, targetWordCount)

	// Create JSON schema for DocumentPlan
	planSchema := h.createDocumentPlanSchema()

	// Emit progress for research phase
	tracker.EmitSubProgress(PhasePlanning, "Starting research and information gathering",
		GetPhaseBaseProgress(PhasePlanning), 0.2, PlanningWeight, map[string]interface{}{
			"step": "research_start",
		})

	// Set up the task context for iterative planning with research
	taskCtx := task.WithContextMinIterations(ctx, 2)
	taskCtx = task.WithContextMaxIterations(taskCtx, 4)
	taskCtx = task.WithContextSchema(taskCtx, planSchema)
	taskCtx = agent.WithContextMessages(taskCtx, []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	})
	taskCtx = agent.WithContextTools(taskCtx, h.tools)

	// Create a task handler for the planning process
	taskHandler := task.NewHandler(h.client, task.WithDefaultTools(h.tools...))

	// Create a temporary agent for this planning task
	plannerAgent := agent.New(taskHandler)

	// Start the agent
	if _, _, err := plannerAgent.Start(taskCtx); err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}
	defer plannerAgent.Stop()

	// Emit progress for planning execution
	tracker.EmitSubProgress(PhasePlanning, "Conducting research and generating document structure",
		GetPhaseBaseProgress(PhasePlanning), 0.5, PlanningWeight, map[string]interface{}{
			"step": "planning_execution",
		})

	// Execute the planning task
	result, err := task.Do(taskCtx, plannerAgent, userPrompt)
	if err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	// Emit progress for plan parsing
	tracker.EmitSubProgress(PhasePlanning, "Processing research results and finalizing plan",
		GetPhaseBaseProgress(PhasePlanning), 0.8, PlanningWeight, map[string]interface{}{
			"step": "plan_parsing",
		})

	// Parse the JSON response from the research-informed planning
	plan, err := h.parsePlanResponse(result.Result())
	if err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	// Set creation timestamp
	plan.CreatedAt = time.Now()

	// Emit progress for plan completion
	tracker.EmitSubProgress(PhasePlanning, fmt.Sprintf("Plan completed: %d sections, %d total words", len(plan.Sections), plan.TotalWords),
		GetPhaseBaseProgress(PhasePlanning), 1.0, PlanningWeight, map[string]interface{}{
			"step":           "plan_complete",
			"sections_count": len(plan.Sections),
			"total_words":    plan.TotalWords,
			"title":          plan.Title,
		})

	return plan, nil
}

// createResearchPlanningPrompt creates the user prompt for research-based planning
func (h *PlannerHandler) createResearchPlanningPrompt(subject string, targetWordCount int) string {
	var prompt strings.Builder

	prompt.WriteString("Create a comprehensive, research-informed document plan for an article on the following subject:\n\n")
	prompt.WriteString("**Subject:** ")
	prompt.WriteString(subject)
	prompt.WriteString("\n\n")
	prompt.WriteString("**Requirements:**\n")
	prompt.WriteString("- Target word count: ")
	prompt.WriteString(fmt.Sprintf("%d", targetWordCount))
	prompt.WriteString(" words\n")
	prompt.WriteString("- Create a well-structured, engaging article plan\n")
	prompt.WriteString("- Ensure comprehensive coverage of the topic\n")
	prompt.WriteString("- Design sections that can be researched and written independently\n")
	prompt.WriteString("- Include specific guidance for writers\n\n")
	prompt.WriteString("**Instructions:**\n")
	prompt.WriteString("1. **First, conduct preliminary research** using your available tools to understand:\n")
	prompt.WriteString("   - Current developments and trends related to the subject\n")
	prompt.WriteString("   - Key aspects, challenges, and opportunities\n")
	prompt.WriteString("   - Important subtopics and angles to cover\n")
	prompt.WriteString("   - Relevant examples, case studies, or data points\n\n")
	prompt.WriteString("2. **Then, create a detailed document plan** based on your research findings\n")
	prompt.WriteString("3. **Ensure the plan reflects current, accurate information** from your research\n")
	prompt.WriteString("4. **Include research-informed key points** for each section\n\n")
	prompt.WriteString("Please start by researching the topic thoroughly, then provide your final document plan in the specified JSON format.")

	return prompt.String()
}

// LLMPlanResponse represents the structure that the LLM actually returns
type LLMPlanResponse struct {
	ArticleTitle    string `json:"article_title"`
	TargetWordCount int    `json:"target_word_count"`
	Introduction    struct {
		Title              string   `json:"title"`
		WordCountTarget    int      `json:"word_count_target"`
		KeyPoints          []string `json:"key_points"`
		GuidanceForWriters string   `json:"guidance_for_writers"`
	} `json:"introduction"`
	Sections []struct {
		Title              string   `json:"title"`
		WordCountTarget    int      `json:"word_count_target"`
		KeyPoints          []string `json:"key_points"`
		GuidanceForWriters string   `json:"guidance_for_writers"`
	} `json:"sections"`
	Conclusion struct {
		Title              string   `json:"title"`
		WordCountTarget    int      `json:"word_count_target"`
		KeyPoints          []string `json:"key_points"`
		GuidanceForWriters string   `json:"guidance_for_writers"`
	} `json:"conclusion"`
}

// parsePlanResponse extracts the document plan from the LLM response
func (h *PlannerHandler) parsePlanResponse(response string) (DocumentPlan, error) {
	// Create a message from the response string for parsing
	message := llm.NewMessage(llm.RoleAssistant, response)

	// First try to parse as the expected DocumentPlan format
	plans, err := llm.ParseJSON[DocumentPlan](message)
	if err == nil && len(plans) > 0 {
		plan := plans[0]
		if err := h.validatePlan(plan); err == nil {
			// Generate section IDs if missing
			for i := range plan.Sections {
				if plan.Sections[i].ID == "" {
					plan.Sections[i].ID = generateSectionID(i, plan.Sections[i].Title)
				}
			}
			return plan, nil
		}
	}

	// If that fails, try to parse as the LLM's actual format and convert
	llmPlans, err := llm.ParseJSON[LLMPlanResponse](message)
	if err != nil {
		return DocumentPlan{}, errors.WithStack(err)
	}

	if len(llmPlans) == 0 {
		return DocumentPlan{}, errors.Errorf("could not parse plan in response: \n%s", response)
	}

	llmPlan := llmPlans[0]

	// Convert LLM format to DocumentPlan format
	plan := DocumentPlan{
		Title:      llmPlan.ArticleTitle,
		Summary:    strings.Join(llmPlan.Introduction.KeyPoints, " "),
		TotalWords: llmPlan.TargetWordCount,
		Keywords:   []string{}, // Will be populated later if needed
	}

	// Convert introduction to first section
	if llmPlan.Introduction.Title != "" {
		introSection := DocumentSection{
			ID:          generateSectionID(0, llmPlan.Introduction.Title),
			Title:       llmPlan.Introduction.Title,
			Description: llmPlan.Introduction.GuidanceForWriters,
			KeyPoints:   llmPlan.Introduction.KeyPoints,
			WordCount:   llmPlan.Introduction.WordCountTarget,
			Priority:    1,
		}
		plan.Sections = append(plan.Sections, introSection)
	}

	// Convert main sections
	for i, section := range llmPlan.Sections {
		docSection := DocumentSection{
			ID:          generateSectionID(i+1, section.Title),
			Title:       section.Title,
			Description: section.GuidanceForWriters,
			KeyPoints:   section.KeyPoints,
			WordCount:   section.WordCountTarget,
			Priority:    i + 2,
		}
		plan.Sections = append(plan.Sections, docSection)
	}

	// Convert conclusion to last section
	if llmPlan.Conclusion.Title != "" {
		conclusionSection := DocumentSection{
			ID:          generateSectionID(len(plan.Sections), llmPlan.Conclusion.Title),
			Title:       llmPlan.Conclusion.Title,
			Description: llmPlan.Conclusion.GuidanceForWriters,
			KeyPoints:   llmPlan.Conclusion.KeyPoints,
			WordCount:   llmPlan.Conclusion.WordCountTarget,
			Priority:    len(plan.Sections) + 1,
		}
		plan.Sections = append(plan.Sections, conclusionSection)
	}

	// Validate the converted plan
	if err := h.validatePlan(plan); err != nil {
		return DocumentPlan{}, errors.WithStack(err)
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
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "The main title of the article",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "A brief summary of the article content",
			},
			"sections": map[string]any{
				"type":        "array",
				"description": "Array of document sections",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "Unique identifier for the section",
						},
						"title": map[string]any{
							"type":        "string",
							"description": "Title of the section",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "Description or guidance for writing this section",
						},
						"key_points": map[string]any{
							"type":        "array",
							"description": "Key points to cover in this section",
							"items": map[string]any{
								"type": "string",
							},
						},
						"word_count": map[string]any{
							"type":        "integer",
							"description": "Target word count for this section",
							"minimum":     1,
						},
						"priority": map[string]any{
							"type":        "integer",
							"description": "Priority order for writing this section",
							"minimum":     1,
						},
					},
					"required":             []string{"id", "title", "description", "key_points", "word_count", "priority"},
					"additionalProperties": false,
				},
			},
			"total_words": map[string]any{
				"type":        "integer",
				"description": "Total target word count for the entire article",
				"minimum":     1,
			},
			"keywords": map[string]any{
				"type":        "array",
				"description": "Keywords relevant to the article",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required":             []string{"title", "summary", "sections", "total_words", "keywords"},
		"additionalProperties": false,
	}

	return llm.NewResponseSchema(
		"document_plan",
		"A structured document plan for an article with sections, word counts, and guidance",
		schema,
	)
}

// NewPlannerHandler creates a new planner handler
func NewPlannerHandler(client llm.ChatCompletionClient, tools ...llm.Tool) *PlannerHandler {
	if len(tools) == 0 {
		tools = tool.GetDefaultResearchTools()
	}

	return &PlannerHandler{
		client: client,
		tools:  tools,
	}
}

var _ agent.Handler = &PlannerHandler{}
