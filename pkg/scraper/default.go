package scraper

import (
	"context"
	"io"
	"net/http"
)

var defaultScraper Scraper = NewHTTPScraper(http.DefaultClient)

func SetDefault(scraper Scraper) {
	defaultScraper = scraper
}

func DefaultScraper() Scraper {
	return defaultScraper
}

func Check(ctx context.Context, url string) (bool, error) {
	return defaultScraper.Check(ctx, url)
}

func Get(ctx context.Context, url string) (io.ReadCloser, error) {
	return defaultScraper.Get(ctx, url)
}
