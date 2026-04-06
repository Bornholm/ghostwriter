package whitepaper

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/ghostwriter/internal/command/llmclient"
	"github.com/bornholm/ghostwriter/internal/command/shared"
	"github.com/bornholm/ghostwriter/pkg/article"
	wppkg "github.com/bornholm/ghostwriter/pkg/whitepaper"
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
				Name:    "subject",
				Aliases: []string{"s"},
				EnvVars: []string{"GHOSTWRITER_SUBJECT"},
			},
			&cli.StringFlag{
				Name:      "subject-file",
				Aliases:   []string{"sf"},
				Usage:     "Path to a file whose content will be used as the subject",
				TakesFile: true,
				EnvVars:   []string{"GHOSTWRITER_SUBJECT_FILE"},
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
			if subjectFile := cliCtx.String("subject-file"); subjectFile != "" {
				data, err := os.ReadFile(subjectFile)
				if err != nil {
					return errors.Wrap(err, "failed to read subject file")
				}
				subject = strings.TrimSpace(string(data))
			}
			if subject == "" {
				return errors.New("subject is required: use --subject or --subject-file")
			}
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
			kb, kbClose, err := shared.BuildKnowledgeBase(ctx, corpusStoragePath)
			if err != nil {
				return errors.Wrap(err, "could not create knowledge base")
			}
			defer func() { _ = kbClose() }()

			orchestratorOptions = append(orchestratorOptions, wppkg.WithKnowledgeBase(kb))

			if len(files) > 0 {
				if err := shared.BootstrapKnowledgeBase(kb, files); err != nil {
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

