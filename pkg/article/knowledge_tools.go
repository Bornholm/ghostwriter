package article

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bornholm/genai/llm"
	"github.com/pkg/errors"
)

// NewAddToKnowledgeBaseTool creates a tool for adding research documents to knowledge base
func NewAddToKnowledgeBaseTool(kb *KnowledgeBase) llm.Tool {
	return llm.NewFuncTool(
		"add_to_knowledge_base",
		"Add a research document to the knowledge base for later use by other agents",
		llm.NewJSONSchema().
			RequiredProperty("title", "title of the research document or source", "string").
			RequiredProperty("content", "main content or key excerpts from the source", "string").
			RequiredProperty("source_type", "type of source (web, academic, news, government, industry)", "string").
			RequiredProperty("url", "source URL if available, empty string if not", "string").
			RequiredProperty("keywords", "comma-separated list of relevant keywords", "string").
			RequiredProperty("relevance", "relevance score from 0.0 to 1.0", "number"),
		func(ctx context.Context, params map[string]any) (string, error) {
			title, err := llm.ToolParam[string](params, "title")
			if err != nil {
				return "", errors.WithStack(err)
			}

			content, err := llm.ToolParam[string](params, "content")
			if err != nil {
				return "", errors.WithStack(err)
			}

			sourceType, err := llm.ToolParam[string](params, "source_type")
			if err != nil {
				return "", errors.WithStack(err)
			}

			url, err := llm.ToolParam[string](params, "url")
			if err != nil {
				return "", errors.WithStack(err)
			}

			keywordsStr, err := llm.ToolParam[string](params, "keywords")
			if err != nil {
				return "", errors.WithStack(err)
			}

			relevance, err := llm.ToolParam[float64](params, "relevance")
			if err != nil {
				return "", errors.WithStack(err)
			}

			// Parse keywords from comma-separated string
			var keywords []string
			if keywordsStr != "" {
				keywordList := strings.Split(keywordsStr, ",")
				for _, keyword := range keywordList {
					trimmed := strings.TrimSpace(keyword)
					if trimmed != "" {
						keywords = append(keywords, trimmed)
					}
				}
			}

			slog.DebugContext(ctx, "adding document to knowledge base", slog.String("url", url), slog.String("title", title))

			// Create research document
			doc := ResearchDocument{
				ID:         generateDocumentID(title),
				URL:        url,
				Title:      title,
				Content:    content,
				Keywords:   keywords,
				SourceType: sourceType,
				Relevance:  relevance,
			}

			// Add to knowledge base
			err = kb.AddDocument(doc)
			if err != nil {
				return "", errors.WithStack(err)
			}

			return fmt.Sprintf("Successfully added research document '%s' to knowledge base (relevance: %.2f, type: %s)", title, relevance, sourceType), nil
		},
	)
}

// NewSearchKnowledgeBaseTool creates a tool for full-text search
func NewSearchKnowledgeBaseTool(kb *KnowledgeBase) llm.Tool {
	return llm.NewFuncTool(
		"search_knowledge_base",
		"Perform full-text search across all research documents",
		llm.NewJSONSchema().
			RequiredProperty("query", "search terms or keywords", "string"),
		func(ctx context.Context, params map[string]any) (string, error) {
			query, err := llm.ToolParam[string](params, "query")
			if err != nil {
				return "", errors.WithStack(err)
			}

			var results []ResearchDocument

			slog.DebugContext(ctx, "searching knowledge base", slog.String("query", query))

			// General search
			results, err = kb.Search(query, 15)
			if err != nil {
				return "", errors.WithStack(err)
			}

			return formatSearchResults(results), nil
		},
	)
}

// Helper functions for formatting results

func formatSearchResults(results []ResearchDocument) string {
	if len(results) == 0 {
		return "No research data found for the specified query."
	}

	var sb strings.Builder
	sb.WriteString("# Research Results\n\n")

	for i, doc := range results {
		sb.WriteString(fmt.Sprintf("## %d. %s\n\n", i+1, doc.Title))
		if doc.URL != "" {
			sb.WriteString(fmt.Sprintf("**Source:** %s\n", doc.URL))
		}
		sb.WriteString(fmt.Sprintf("**Type:** %s\n", doc.SourceType))
		sb.WriteString(fmt.Sprintf("**Relevance:** %.2f\n\n", doc.Relevance))

		if doc.Content != "" {
			sb.WriteString("**Content:**\n")
			sb.WriteString(doc.Content)
			sb.WriteString("\n\n")
		}

		if len(doc.Keywords) > 0 {
			sb.WriteString("**Keywords:** ")
			sb.WriteString(strings.Join(doc.Keywords, ", "))
			sb.WriteString("\n\n")
		}

		sb.WriteString("---\n\n")
	}

	return sb.String()
}

// Helper function to generate document ID
func generateDocumentID(title string) string {
	// Create a simple ID from title and timestamp
	cleanTitle := strings.ToLower(strings.ReplaceAll(title, " ", "_"))
	// Remove special characters
	cleanTitle = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return -1
	}, cleanTitle)

	// Add timestamp to ensure uniqueness
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%s_%d", cleanTitle, timestamp)
}
