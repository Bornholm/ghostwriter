package duckduckgo

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/bornholm/ghostwriter/pkg/scraper"
	"github.com/bornholm/ghostwriter/pkg/search"
	searchEngine "github.com/bornholm/ghostwriter/pkg/search"
	"github.com/pkg/errors"
)

type Client struct {
	scraper scraper.Scraper
}

func (c *Client) Search(ctx context.Context, search string) ([]search.Result, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   "duckduckgo.com",
		Path:   "/html/",
	}

	query := url.Query()
	query.Set("q", search)
	url.RawQuery = query.Encode()

	slog.DebugContext(ctx, "scraping duckduckgo results", slog.String("url", url.String()))

	var results []searchEngine.Result

	body, err := c.scraper.Get(ctx, url.String())
	if err != nil {
		return nil, errors.WithStack(errors.WithStack(err))
	}

	defer body.Close()

	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	captcha := doc.Find("#challenge-form")
	if captcha.Length() > 0 {
		return nil, errors.WithStack(ErrCaptcha)
	}

	resultElements := doc.Find(".result")
	if resultElements.Length() == 0 {
		return nil, errors.Errorf("unexpected result:\n%s", doc.Text())
	}

	resultElements.Each(func(i int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find(".result__title").Text())
		if title == "" {
			return
		}

		rawDDGLink := s.Find(".result__a").AttrOr("href", "")
		if rawDDGLink == "" {
			return
		}

		ddgLink, err := url.Parse(rawDDGLink)
		if err != nil {
			return
		}

		snippet := strings.TrimSpace(s.Find(".result__snippet").Text())
		if snippet == "" {
			return
		}

		results = append(results, searchEngine.Result{
			Title:       title,
			Description: snippet,
			URL:         ddgLink.Query().Get("uddg"),
		})
	})

	return results, nil
}

func NewClient(scraper scraper.Scraper) *Client {
	return &Client{
		scraper: scraper,
	}
}

var _ search.Client = &Client{}
