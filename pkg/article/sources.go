package article

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/prompt"
	"github.com/pkg/errors"
)

//go:embed prompts/*.gotmpl
var sourcePrompts embed.FS

// ContentSeparationResult represents the JSON response for content separation
type ContentSeparationResult struct {
	Content string   `json:"content"`
	Sources []string `json:"sources"`
}

// SourceExtractionResult represents the JSON response for source extraction
type SourceExtractionResult struct {
	Sources []string `json:"sources"`
}

// SourceConsolidationResult represents the JSON response for source consolidation
type SourceConsolidationResult struct {
	ConsolidatedSources []string `json:"consolidated_sources"`
}

// SourceExtractor provides LLM-based source extraction from text content
type SourceExtractor struct {
	client llm.ChatCompletionClient
}

// NewSourceExtractor creates a new source extractor instance
func NewSourceExtractor(client llm.ChatCompletionClient) *SourceExtractor {
	return &SourceExtractor{
		client: client,
	}
}

// ExtractContentAndSources separates main content from sources using LLM
func (se *SourceExtractor) ExtractContentAndSources(ctx context.Context, text string) (content string, sources []string, err error) {
	// Load the content separation prompt
	systemPrompt, err := prompt.FromFS[any](&sourcePrompts, "prompts/content_separator_system.gotmpl", nil)
	if err != nil {
		return "", nil, errors.WithStack(err)
	}

	userPrompt := se.createContentSeparationPrompt(text)

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	}

	// Use low temperature for consistent parsing
	response, err := se.client.ChatCompletion(ctx,
		llm.WithMessages(messages...),
		llm.WithTemperature(0.1),
	)
	if err != nil {
		return "", nil, errors.WithStack(err)
	}

	// Parse the JSON response using llm.ParseJSON
	results, err := llm.ParseJSON[ContentSeparationResult](response.Message())
	if err != nil {
		return "", nil, errors.Wrapf(err, "failed to parse content separation JSON response")
	}

	if len(results) == 0 {
		return "", nil, errors.New("no content separation results found in response")
	}

	// Use the first result
	result := results[0]
	return result.Content, result.Sources, nil
}

// ExtractSources extracts sources from text using LLM
func (se *SourceExtractor) ExtractSources(ctx context.Context, text string) ([]string, error) {
	// Load the source extraction prompt
	systemPrompt, err := prompt.FromFS[any](&sourcePrompts, "prompts/source_extractor_system.gotmpl", nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	userPrompt := se.createSourceExtractionPrompt(text)

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	}

	// Use low temperature for consistent parsing
	response, err := se.client.ChatCompletion(ctx,
		llm.WithMessages(messages...),
		llm.WithTemperature(0.1),
	)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Parse the JSON response using llm.ParseJSON
	results, err := llm.ParseJSON[SourceExtractionResult](response.Message())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse source extraction JSON response")
	}

	if len(results) == 0 {
		return []string{}, nil // Return empty slice if no results
	}

	// Use the first result
	result := results[0]
	return result.Sources, nil
}

// ConsolidateSources merges and deduplicates sources from multiple sections using LLM
func (se *SourceExtractor) ConsolidateSources(ctx context.Context, sectionSources [][]string) ([]string, error) {
	if len(sectionSources) == 0 {
		return []string{}, nil
	}

	// Flatten all sources
	var allSources []string
	for _, sources := range sectionSources {
		allSources = append(allSources, sources...)
	}

	if len(allSources) == 0 {
		return []string{}, nil
	}

	// Load the source consolidation prompt
	systemPrompt, err := prompt.FromFS[any](&sourcePrompts, "prompts/source_consolidator_system.gotmpl", nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	userPrompt := se.createSourceConsolidationPrompt(allSources)

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	}

	// Use low temperature for consistent parsing
	response, err := se.client.ChatCompletion(ctx,
		llm.WithMessages(messages...),
		llm.WithTemperature(0.1),
	)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Parse the JSON response using llm.ParseJSON
	results, err := llm.ParseJSON[SourceConsolidationResult](response.Message())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse source consolidation JSON response")
	}

	if len(results) == 0 {
		return []string{}, nil // Return empty slice if no results
	}

	// Use the first result
	result := results[0]
	return result.ConsolidatedSources, nil
}

// FormatSourcesSection creates a properly formatted sources section with URLs
func (se *SourceExtractor) FormatSourcesSection(sources []string) string {
	if len(sources) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n\n## Sources\n\n")

	for i, source := range sources {
		source = strings.TrimSpace(source)

		// Ensure each source is properly formatted with numbering for better readability
		builder.WriteString(fmt.Sprintf("%d. %s", i+1, source))

		if i < len(sources)-1 {
			builder.WriteString("\n")
		}
	}

	return builder.String()
}

// createContentSeparationPrompt creates the prompt for separating content from sources
func (se *SourceExtractor) createContentSeparationPrompt(text string) string {
	var prompt strings.Builder

	prompt.WriteString("Please separate the main article content from the sources section in the following text:\n\n")
	prompt.WriteString("```\n")
	prompt.WriteString(text)
	prompt.WriteString("\n```\n\n")
	prompt.WriteString("Extract the main content and sources separately and return them in the specified JSON format.")

	return prompt.String()
}

// createSourceExtractionPrompt creates the prompt for extracting sources
func (se *SourceExtractor) createSourceExtractionPrompt(text string) string {
	var prompt strings.Builder

	prompt.WriteString("Please extract all sources and references from the following text:\n\n")
	prompt.WriteString("```\n")
	prompt.WriteString(text)
	prompt.WriteString("\n```\n\n")
	prompt.WriteString("Extract and format the sources and return them in the specified JSON format.")

	return prompt.String()
}

// createSourceConsolidationPrompt creates the prompt for consolidating sources
func (se *SourceExtractor) createSourceConsolidationPrompt(sources []string) string {
	var prompt strings.Builder

	prompt.WriteString("Please consolidate, deduplicate, and format the following sources:\n\n")
	for i, source := range sources {
		prompt.WriteString("- ")
		prompt.WriteString(strings.TrimSpace(source))
		if i < len(sources)-1 {
			prompt.WriteString("\n")
		}
	}
	prompt.WriteString("\n\nConsolidate these sources and return them in the specified JSON format.")

	return prompt.String()
}
