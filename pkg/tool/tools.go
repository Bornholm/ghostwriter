package tool

import (
	"github.com/bornholm/genai/llm"
	"github.com/bornholm/ghostwriter/pkg/scraper/surf"
	"github.com/bornholm/ghostwriter/pkg/search/duckduckgo"
)

// GetDefaultResearchTools returns the standard set of research tools
func GetDefaultResearchTools() []llm.Tool {
	scraper := surf.NewScraper()

	return []llm.Tool{
		NewWebSearchTool(duckduckgo.NewClient(scraper)),
		NewScrapeWebpageTool(scraper),
	}
}
