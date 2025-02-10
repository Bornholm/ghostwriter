package tool

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bornholm/genai/llm"
	"github.com/bornholm/ghostwriter/pkg/search"
	"github.com/pkg/errors"
)

func NewWebSearchTool(client search.Client) llm.Tool {
	return llm.NewFuncTool(
		"web_search",
		"execute a research on the web about a topic",
		llm.NewJSONSchema().
			RequiredProperty("topic", "the topic to research", "string"),
		func(ctx context.Context, params map[string]any) (string, error) {
			topic, err := llm.ToolParam[string](params, "topic")
			if err != nil {
				return "", errors.WithStack(err)
			}

			slog.DebugContext(ctx, "executing a web search", slog.String("topic", topic))

			results, err := client.Search(ctx, topic)
			if err != nil {
				return "", errors.WithStack(err)
			}

			var sb strings.Builder

			sb.WriteString("# Search results\n\n")

			for i, r := range results {
				sb.WriteString(fmt.Sprintf("## %d. %s\n\n", i+1, r.Title))
				sb.WriteString(fmt.Sprintf("**URL**: %s\n", r.URL))
				sb.WriteString(fmt.Sprintf("**Description**:\n%s\n\n", r.Description))
			}

			return sb.String(), nil
		},
	)
}
