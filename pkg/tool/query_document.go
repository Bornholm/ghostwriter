package tool

import (
	"bytes"
	"context"
	"fmt"

	amatl "github.com/Bornholm/amatl/pkg/markdown/selector"
	"github.com/bornholm/genai/llm"
	"github.com/pkg/errors"
	"github.com/yuin/goldmark"
	goldmarkext "github.com/yuin/goldmark/extension"
	goldmarkparser "github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// NewQueryDocumentTool creates an LLM tool that queries a markdown document
// using CSS-like selectors powered by the amatl selector package.
//
// The document content is fixed at tool creation time; pass a fresh tool
// instance whenever the draft changes.
func NewQueryDocumentTool(document string) llm.Tool {
	return llm.NewFuncTool(
		"query_document",
		"Query the already-written sections of the document using one or more CSS-like selectors (e.g. `h2`, `h2:contains(\"foo *\")`, `code[lang=\"go\"]`, `table`). "+
			"Returns the matching content as plain text. Use this to check for redundancies across sections.",
		llm.NewJSONSchema().
			RequiredProperty("selectors", "Array of CSS-like selector strings (e.g. [\"h2\", \"code[lang=\\\"mermaid\\\"]\", \"table\"])", "array"),
		func(_ context.Context, params map[string]any) (llm.ToolResult, error) {
			selectors, err := llm.ToolParam[[]any](params, "selectors")
			if err != nil {
				return nil, errors.WithStack(err)
			}

			source := []byte(document)

			md := goldmark.New(goldmark.WithExtensions(goldmarkext.GFM))
			p := md.Parser()
			p.AddOptions(goldmarkparser.WithAutoHeadingID())
			doc := p.Parse(text.NewReader(source))

			var buf bytes.Buffer
			for _, raw := range selectors {
				selectorStr, ok := raw.(string)
				if !ok {
					continue
				}

				sel, err := amatl.Parse(selectorStr)
				if err != nil {
					return nil, fmt.Errorf("invalid selector %q: %w", selectorStr, err)
				}

				nodes := sel.FindAll(doc, source)
				if len(nodes) == 0 {
					fmt.Fprintf(&buf, "(no matches found for selector: %s)\n", selectorStr)
					continue
				}

				fmt.Fprintf(&buf, "=== selector: %s ===\n", selectorStr)
				for _, n := range nodes {
					lines := n.Lines()
					if lines == nil {
						continue
					}
					for i := 0; i < lines.Len(); i++ {
						seg := lines.At(i)
						buf.Write(source[seg.Start:seg.Stop])
					}
					buf.WriteByte('\n')
				}
			}

			return llm.NewToolResult(buf.String()), nil
		},
	)
}
