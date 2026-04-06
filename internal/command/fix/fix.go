package fix

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bornholm/genai/agent"
	"github.com/bornholm/ghostwriter/internal/command/llmclient"
	"github.com/bornholm/ghostwriter/internal/command/shared"
	whitepaperui "github.com/bornholm/ghostwriter/internal/command/whitepaper"
	wppkg "github.com/bornholm/ghostwriter/pkg/whitepaper"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

func Fix() *cli.Command {
	return &cli.Command{
		Name:  "fix",
		Usage: "Apply '> EDITOR: ...' annotations from a whitepaper output directory",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "dir",
				Required: true,
				Aliases:  []string{"d"},
				Usage:    "Path to the whitepaper output directory",
				EnvVars:  []string{"GHOSTWRITER_FIX_DIR"},
			},
			&cli.StringFlag{
				Name:    "style-guide",
				Value:   "",
				Aliases: []string{"g"},
				EnvVars: []string{"GHOSTWRITER_STYLE_GUIDE"},
			},
			&cli.StringFlag{
				Name:    "additional-context",
				Value:   "",
				Aliases: []string{"c"},
				EnvVars: []string{"GHOSTWRITER_ADDITIONAL_CONTEXT"},
			},
			&cli.StringFlag{
				Name:    "corpus-storage-path",
				Value:   "",
				Usage:   "Path to Corpus data dir (defaults to <dir>/.corpus if it exists)",
				EnvVars: []string{"GHOSTWRITER_CORPUS_STORAGE_PATH"},
			},
			&cli.BoolFlag{
				Name:    "enrich",
				Value:   false,
				Usage:   "Force the enrichment pass (citation links + Mermaid diagrams) even if no > EDITOR: annotations are found",
				EnvVars: []string{"GHOSTWRITER_FIX_ENRICH"},
			},
		},
		Action: func(cliCtx *cli.Context) error {
			dir := strings.TrimSpace(cliCtx.String("dir"))
			styleGuide := cliCtx.String("style-guide")
			additionalContext := cliCtx.String("additional-context")
			corpusStoragePath := cliCtx.String("corpus-storage-path")
			forceEnrich := cliCtx.Bool("enrich")

			// Auto-discover corpus path
			if corpusStoragePath == "" {
				candidate := filepath.Join(dir, ".corpus")
				if _, err := os.Stat(candidate); err == nil {
					corpusStoragePath = candidate
				}
			}

			ctx, cancel := context.WithTimeout(cliCtx.Context, 2*time.Hour)
			defer cancel()

			resilientClient, err := llmclient.NewClient(ctx)
			if err != nil {
				return errors.Wrap(err, "failed to create llm client")
			}

			fixOptions := []wppkg.FixOptionFunc{
				wppkg.WithFixInputDir(dir),
				wppkg.WithFixForceEnrichment(forceEnrich),
			}

			if styleGuide != "" {
				data, err := os.ReadFile(styleGuide)
				if err != nil {
					return errors.Wrap(err, "failed to read style guide")
				}
				fixOptions = append(fixOptions, wppkg.WithFixStyleGuidelines(string(data)))
			}

			if additionalContext != "" {
				data, err := os.ReadFile(additionalContext)
				if err != nil {
					return errors.Wrap(err, "failed to read additional context file")
				}
				fixOptions = append(fixOptions, wppkg.WithFixAdditionalContext(string(data)))
			}

			if corpusStoragePath != "" {
				kb, kbClose, err := shared.BuildKnowledgeBase(ctx, corpusStoragePath)
				if err != nil {
					return errors.Wrap(err, "could not open knowledge base")
				}
				defer func() { _ = kbClose() }()
				fixOptions = append(fixOptions, wppkg.WithFixKnowledgeBase(kb))
			}

			fmt.Printf("\n%s\n\n", fmt.Sprintf("Corrections : %q", dir))

			emit := func(evt agent.Event) error {
				output := whitepaperui.RenderEvent(evt)
				if output != "" {
					fmt.Print(output)
				}
				return nil
			}

			result, err := wppkg.FixWhitePaperInDir(ctx, resilientClient, emit, fixOptions...)
			if err != nil {
				return errors.Wrap(err, "failed to fix white paper")
			}

			fmt.Printf("\n✓ %d fichier(s) corrigé(s)", len(result.FixedFiles))
			if len(result.SkippedFiles) > 0 {
				fmt.Printf(", %d ignoré(s)", len(result.SkippedFiles))
			}
			fmt.Println()

			return nil
		},
	}
}

func Root() *cli.Command {
	return Fix()
}
