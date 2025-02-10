package google

import (
	"context"
	"log/slog"

	"github.com/bornholm/ghostwriter/pkg/search"
	searchEngine "github.com/bornholm/ghostwriter/pkg/search"
	"github.com/pkg/errors"
	"google.golang.org/api/customsearch/v1"
	"google.golang.org/api/option"
)

// Client implements the search.Engine interface using Google Custom Search API.
type Client struct {
	apiKey string
	cx     string
}

// Search implements the search.Engine interface.
func (c *Client) Search(ctx context.Context, query string) ([]search.Result, error) {
	service, err := customsearch.NewService(ctx, option.WithAPIKey(c.apiKey))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	slog.DebugContext(ctx, "executing search", slog.String("query", query))

	// Create a search call with the query and search engine ID
	search := service.Cse.List()
	search.Q(query)
	search.Cx(c.cx)
	search.Num(10) // Retrieve 10 results by default

	// Execute the search
	searchResult, err := search.Do()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Convert Google search results to our search.Result format
	var results []searchEngine.Result
	for _, item := range searchResult.Items {
		results = append(results, searchEngine.Result{
			Title:       item.Title,
			URL:         item.Link,
			Description: item.Snippet,
		})
	}

	return results, nil
}

// NewClient creates a new Google Custom Search API client.
func NewClient(apiKey, cx string) *Client {
	return &Client{
		apiKey: apiKey,
		cx:     cx,
	}
}

// Ensure Client implements search.Engine
var _ search.Client = &Client{}
