package duckduckgo

import (
	"context"
	"testing"

	"github.com/bornholm/ghostwriter/pkg/scraper/surf"
	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
)

func TestClient(t *testing.T) {
	scraper := surf.NewScraper()
	client := NewClient(scraper)

	ctx := context.Background()

	results, err := client.Search(ctx, "Cadoles site:linkedin.com")
	if err != nil {
		t.Fatalf("%+v", errors.WithStack(err))
	}

	spew.Dump(results)
}
