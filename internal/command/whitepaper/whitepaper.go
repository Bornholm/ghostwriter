package whitepaper

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/genai/llm/provider"
	providerenv "github.com/bornholm/genai/llm/provider/env"
	"github.com/bornholm/ghostwriter/internal/command/llmclient"
	"github.com/bornholm/ghostwriter/pkg/article"
	corpusadapter "github.com/bornholm/ghostwriter/pkg/knowledgebase/corpus"
	wppkg "github.com/bornholm/ghostwriter/pkg/whitepaper"
	"github.com/bornholm/corpus/pkg/corpus"
	"github.com/gosimple/slug"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

func Whitepaper() *cli.Command {
	return &cli.Command{
		Name:  "whitepaper",
		Usage: "Write a complete white paper about the given subject",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "subject",
				Required: true,
				Aliases:  []string{"s"},
				EnvVars:  []string{"GHOSTWRITER_SUBJECT"},
			},
			&cli.IntFlag{
				Name:    "target-words",
				Value:   10000,
				Aliases: []string{"t"},
				EnvVars: []string{"GHOSTWRITER_TARGET_WORDS"},
			},
			&cli.StringFlag{
				Name:    "output-dir",
				Value:   "",
				Aliases: []string{"o"},
				EnvVars: []string{"GHOSTWRITER_OUTPUT_DIR"},
			},
			&cli.StringFlag{
				Name:    "output-pdf",
				Value:   "",
				EnvVars: []string{"GHOSTWRITER_OUTPUT_PDF"},
			},
			&cli.StringFlag{
				Name:    "output-html",
				Value:   "",
				EnvVars: []string{"GHOSTWRITER_OUTPUT_HTML"},
			},
			&cli.StringFlag{
				Name:    "style-guide",
				Value:   "",
				Aliases: []string{"g"},
				EnvVars: []string{"GHOSTWRITER_STYLE_GUIDE"},
			},
			&cli.StringFlag{
				Name:    "research-depth",
				Value:   string(article.ResearchDeep),
				Aliases: []string{"d"},
				EnvVars: []string{"GHOSTWRITER_RESEARCH_DEPTH"},
			},
			&cli.StringSliceFlag{
				Name:      "files",
				Aliases:   []string{"f"},
				EnvVars:   []string{"GHOSTWRITER_FILES"},
				TakesFile: true,
			},
			&cli.StringFlag{
				Name:    "additional-context",
				Value:   "",
				Aliases: []string{"c"},
				EnvVars: []string{"GHOSTWRITER_ADDITIONAL_CONTEXT"},
			},
			&cli.StringFlag{
				Name:    "chromium-path",
				Value:   "",
				EnvVars: []string{"GHOSTWRITER_CHROMIUM_PATH"},
			},
			&cli.BoolFlag{
				Name:    "no-sandbox",
				Value:   false,
				EnvVars: []string{"GHOSTWRITER_NO_SANDBOX"},
			},
			&cli.BoolFlag{
				Name:    "parts",
				Value:   false,
				Usage:   "Enable grouping of chapters into parts",
				EnvVars: []string{"GHOSTWRITER_PARTS"},
			},
			&cli.IntFlag{
				Name:    "max-review-rounds",
				Value:   2,
				Usage:   "Maximum number of write→review rounds per chapter (minimum 1)",
				EnvVars: []string{"GHOSTWRITER_MAX_REVIEW_ROUNDS"},
			},
			&cli.StringFlag{
				Name:    "corpus-storage-path",
				Value:   ".corpus",
				Usage:   "Path to the Corpus data directory (activates the Corpus knowledge base backend)",
				EnvVars: []string{"GHOSTWRITER_CORPUS_STORAGE_PATH"},
			},
		},
		Action: func(cliCtx *cli.Context) error {
			subject := strings.TrimSpace(cliCtx.String("subject"))
			targetWords := cliCtx.Int("target-words")
			outputDir := cliCtx.String("output-dir")
			outputPDF := cliCtx.String("output-pdf")
			outputHTML := cliCtx.String("output-html")
			styleGuide := cliCtx.String("style-guide")
			researchDepth := cliCtx.String("research-depth")
			files := cliCtx.StringSlice("files")
			additionalContext := cliCtx.String("additional-context")
			chromiumPath := cliCtx.String("chromium-path")
			noSandbox := cliCtx.Bool("no-sandbox")
			corpusStoragePath := cliCtx.String("corpus-storage-path")
			maxReviewRounds := cliCtx.Int("max-review-rounds")

			if outputDir == "" {
				outputDir = slug.Make(subject)
			}

			ctx, cancel := context.WithTimeout(cliCtx.Context, 2*time.Hour)
			defer cancel()

			resilientClient, err := llmclient.NewClient(ctx)
			if err != nil {
				return errors.Wrap(err, "failed to create llm client")
			}

			orchestratorOptions := []wppkg.OrchestratorOptionFunc{
				wppkg.WithTargetWordCount(targetWords),
				wppkg.WithResearchDepth(article.ResearchDepth(researchDepth)),
				wppkg.WithOutputDir(outputDir),
				wppkg.WithRenderHTML(outputHTML),
				wppkg.WithRenderPDF(outputPDF),
				wppkg.WithChromiumPath(chromiumPath),
				wppkg.WithNoSandbox(noSandbox),
				wppkg.WithMaxReviewRounds(maxReviewRounds),
			}

			if styleGuide != "" {
				data, err := os.ReadFile(styleGuide)
				if err != nil {
					return errors.Wrap(err, "failed to read style guide")
				}
				orchestratorOptions = append(orchestratorOptions, wppkg.WithStyleGuidelines(string(data)))
			}

			if additionalContext != "" {
				data, err := os.ReadFile(additionalContext)
				if err != nil {
					return errors.Wrap(err, "failed to read additional context file")
				}
				orchestratorOptions = append(orchestratorOptions, wppkg.WithAdditionalContext(string(data)))
			}

			// Build knowledge base: Corpus backend (default) or Bleve fallback.
			kb, kbClose, err := buildKnowledgeBase(ctx, corpusStoragePath)
			if err != nil {
				return errors.Wrap(err, "could not create knowledge base")
			}
			defer func() { _ = kbClose() }()

			orchestratorOptions = append(orchestratorOptions, wppkg.WithKnowledgeBase(kb))

			if len(files) > 0 {
				if err := bootstrapKnowledgeBase(kb, files); err != nil {
					return errors.Wrap(err, "could not bootstrap knowledge base")
				}
			}

			fmt.Printf("\n%s\n%s\n\n",
				titleStyle.Render(fmt.Sprintf("Livre blanc : %q", subject)),
				subtleStyle.Render(strings.Repeat("─", 60)),
			)

			emit := func(evt agent.Event) error {
				output := RenderEvent(evt)
				if output != "" {
					fmt.Print(output)
				}
				return nil
			}

			result, err := wppkg.WriteWhitePaper(ctx, resilientClient, subject, emit, orchestratorOptions...)
			if err != nil {
				return errors.Wrap(err, "failed to generate white paper")
			}

			fmt.Printf("\n%s\n", successStyle.Render(fmt.Sprintf("✓ Livre blanc généré dans %s/", outputDir)))
			fmt.Printf("  %s %s\n", subtleStyle.Render("Index :"), result.Entrypoint)
			if outputHTML != "" {
				fmt.Printf("  %s %s\n", subtleStyle.Render("HTML  :"), outputHTML)
			}
			if outputPDF != "" {
				fmt.Printf("  %s %s\n", subtleStyle.Render("PDF   :"), outputPDF)
			}

			return nil
		},
	}
}

// buildKnowledgeBase creates a Corpus-backed knowledge base stored at storagePath.
// A dedicated LLM client is first attempted via GHOSTWRITER_CORPUS_* env vars
// (allows configuring a separate embeddings model). If those vars are absent,
// it falls back to the main GHOSTWRITER_* provider. Corpus can also run without
// an LLM client (disabling vector search, HyDE and Judge).
func buildKnowledgeBase(ctx context.Context, storagePath string) (article.KnowledgeBase, func() error, error) {
	// Try a dedicated Corpus LLM client first (GHOSTWRITER_CORPUS_EMBEDDINGS_* etc.)
	corpusLLMClient, err := provider.Create(ctx, providerenv.With("GHOSTWRITER_CORPUS_", ".env"))
	if err != nil {
		// Fall back to the main provider (embeddings may not be configured).
		corpusLLMClient, _ = provider.Create(ctx, providerenv.With("GHOSTWRITER_", ".env"))
	}
	if corpusLLMClient != nil {
		corpusLLMClient = llmclient.Wrap(corpusLLMClient)
	}

	if err := os.MkdirAll(storagePath, 0755); err != nil {
		return nil, nil, errors.Wrap(err, "could not create corpus storage directory")
	}

	corpusOpts := []corpus.OptionFunc{
		corpus.WithStoragePath(storagePath),
		corpus.WithDisableHyDE(),
		corpus.WithDisableJudge(),
	}
	if corpusLLMClient != nil {
		corpusOpts = append(corpusOpts, corpus.WithLLMClient(corpusLLMClient))
	}

	c, err := corpus.New(ctx, corpusOpts...)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not initialise corpus")
	}

	collectionID, err := c.CreateCollection(ctx, "ghostwriter")
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not create corpus collection")
	}

	kb := corpusadapter.New(c, collectionID)
	return kb, func() error { return nil }, nil
}

func bootstrapKnowledgeBase(kb article.KnowledgeBase, files []string) error {
	for _, f := range files {
		matches, err := filepath.Glob(f)
		if err != nil {
			return errors.Wrapf(err, "could not match file pattern '%s'", f)
		}
		for _, m := range matches {
			absPath, err := filepath.Abs(m)
			if err != nil {
				return errors.Wrapf(err, "could not retrieve absolute path for file '%s'", m)
			}

			data, err := os.ReadFile(m)
			if err != nil {
				return errors.Wrapf(err, "could not read file '%s'", m)
			}

			u := &url.URL{Scheme: "file", Path: absPath}
			err = kb.AddDocument(article.ResearchDocument{
				URL:        u.String(),
				Title:      filepath.Base(m),
				Content:    string(data),
				Keywords:   []string{},
				SourceType: "file",
				Relevance:  1,
			})
			if err != nil {
				return errors.WithStack(err)
			}
		}
	}
	return nil
}
