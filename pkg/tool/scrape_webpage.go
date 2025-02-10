package tool

import (
	"context"
	"log/slog"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/PuerkitoBio/goquery"
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/ghostwriter/pkg/scraper"
	"github.com/pkg/errors"
)

func NewScrapeWebpageTool(scraper scraper.Scraper) llm.Tool {
	return llm.NewFuncTool(
		"scrape_webpage",
		"scrape the given webpage url and returns its content as markdown",
		llm.NewJSONSchema().
			RequiredProperty("url", "the url of the webpage to scrape", "string"),
		func(ctx context.Context, params map[string]any) (string, error) {
			url, err := llm.ToolParam[string](params, "url")
			if err != nil {
				return "", errors.WithStack(err)
			}

			slog.DebugContext(ctx, "scraping page", slog.String("url", url))

			res, err := scraper.Get(ctx, url)
			if err != nil {
				return "", errors.WithStack(err)
			}

			defer res.Close()

			doc, err := goquery.NewDocumentFromReader(res)
			if err != nil {
				return "", errors.WithStack(err)
			}

			conv := converter.NewConverter(
				converter.WithPlugins(
					base.NewBasePlugin(),
					commonmark.NewCommonmarkPlugin(),
					table.NewTablePlugin(),
				),
			)

			html, err := doc.Find("body").Html()
			if err != nil {
				return "", errors.WithStack(err)
			}

			markdown, err := conv.ConvertString(html)
			if err != nil {
				return "", errors.WithStack(err)
			}

			return strings.TrimSpace(markdown), nil
		},
	)
}
