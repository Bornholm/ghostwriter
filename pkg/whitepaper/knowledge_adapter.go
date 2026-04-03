package whitepaper

import (
	"context"
	"fmt"
	"strings"

	"github.com/bornholm/genai/llm"
	"github.com/bornholm/ghostwriter/pkg/article"
	"github.com/pkg/errors"
)

// SearchResult is a knowledge base search result, agnostic of backend.
type SearchResult struct {
	Title      string
	URL        string
	Content    string
	SourceType string
	Relevance  float64
}

// KnowledgeSearcher abstracts search across backends (Bleve or corpus).
type KnowledgeSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// KnowledgeBaseAdapter wraps an article.KnowledgeBase as a KnowledgeSearcher.
type KnowledgeBaseAdapter struct {
	kb article.KnowledgeBase
}

func NewKnowledgeBaseAdapter(kb article.KnowledgeBase) *KnowledgeBaseAdapter {
	return &KnowledgeBaseAdapter{kb: kb}
}

func (a *KnowledgeBaseAdapter) Search(_ context.Context, query string, limit int) ([]SearchResult, error) {
	docs, err := a.kb.Search(query, limit)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	results := make([]SearchResult, len(docs))
	for i, d := range docs {
		results[i] = SearchResult{
			Title:      d.Title,
			URL:        d.URL,
			Content:    d.Content,
			SourceType: d.SourceType,
			Relevance:  d.Relevance,
		}
	}
	return results, nil
}

// NewSearchKnowledgeBaseTool builds an LLM tool backed by a KnowledgeSearcher.
// It accepts a list of queries so the LLM can issue several searches in one call.
func NewSearchKnowledgeBaseTool(ks KnowledgeSearcher) llm.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"queries": map[string]any{
				"type":        "array",
				"description": "One or more search queries to run against the knowledge base",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
			},
		},
		"required":             []string{"queries"},
		"additionalProperties": false,
	}
	return llm.NewFuncTool(
		"search_knowledge_base",
		"Search the research knowledge base for information relevant to the white paper. Pass multiple queries to retrieve diverse results in one call.",
		schema,
		func(ctx context.Context, params map[string]any) (llm.ToolResult, error) {
			raw, ok := params["queries"]
			if !ok {
				return nil, errors.New("missing 'queries' parameter")
			}
			items, ok := raw.([]any)
			if !ok {
				return nil, errors.New("'queries' must be an array of strings")
			}

			seen := make(map[string]bool)
			var allResults []SearchResult
			for _, item := range items {
				query, ok := item.(string)
				if !ok || query == "" {
					continue
				}
				results, err := ks.Search(ctx, query, 10)
				if err != nil {
					return nil, errors.WithStack(err)
				}
				for _, r := range results {
					key := r.URL
					if key == "" {
						key = r.Title
					}
					if !seen[key] {
						seen[key] = true
						allResults = append(allResults, r)
					}
				}
			}

			if len(allResults) == 0 {
				return llm.NewToolResult("No results found for the provided queries."), nil
			}

			var sb strings.Builder
			sb.WriteString("# Research Results\n\n")
			for i, r := range allResults {
				sb.WriteString(fmt.Sprintf("## %d. %s\n\n", i+1, r.Title))
				if r.URL != "" {
					sb.WriteString(fmt.Sprintf("**Source:** %s\n", r.URL))
				}
				sb.WriteString(fmt.Sprintf("**Type:** %s\n", r.SourceType))
				sb.WriteString(fmt.Sprintf("**Relevance:** %.2f\n\n", r.Relevance))
				if r.Content != "" {
					sb.WriteString("**Content:**\n")
					sb.WriteString(r.Content)
					sb.WriteString("\n\n")
				}
				sb.WriteString("---\n\n")
			}
			return llm.NewToolResult(sb.String()), nil
		},
	)
}
