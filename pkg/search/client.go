package search

import "context"

type Client interface {
	Search(ctx context.Context, search string) ([]Result, error)
}

type Result struct {
	Title       string
	URL         string
	Description string
}
