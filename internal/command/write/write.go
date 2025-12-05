package write

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/circuitbreaker"
	"github.com/bornholm/genai/llm/hook"
	"github.com/bornholm/genai/llm/provider"
	"github.com/bornholm/genai/llm/provider/env"
	"github.com/bornholm/genai/llm/provider/openrouter"
	"github.com/bornholm/genai/llm/ratelimit"
	"github.com/bornholm/genai/llm/retry"
	"github.com/bornholm/ghostwriter/pkg/article"
	"github.com/bornholm/ghostwriter/pkg/tool"
	"github.com/gosimple/slug"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"go.yaml.in/yaml/v3"
)

func Write() *cli.Command {
	return &cli.Command{
		Name:  "write",
		Usage: "Write an article about the given subject",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "subject",
				Required: true,
				Value:    "",
				Aliases:  []string{"s"},
				EnvVars:  []string{"GHOSTWRITER_SUBJECT"},
			},
			&cli.IntFlag{
				Name:    "target-words",
				Value:   1000,
				Aliases: []string{"t"},
				EnvVars: []string{"GHOSTWRITER_TARGET_WORDS"},
			},
			&cli.StringFlag{
				Name:    "style-guide",
				Value:   "",
				Aliases: []string{"g"},
				EnvVars: []string{"GHOSTWRITER_STYLE_GUIDE"},
			},
			&cli.StringFlag{
				Name:      "workspace",
				Value:     "",
				Aliases:   []string{"w"},
				EnvVars:   []string{"GHOSTWRITER_WORKSPACE"},
				TakesFile: true,
			},
			&cli.StringFlag{
				Name:    "additional-context",
				Value:   "",
				Aliases: []string{"c"},
				EnvVars: []string{"GHOSTWRITER_ADDITIONAL_CONTEXT"},
			},
			&cli.StringFlag{
				Name:      "output",
				Value:     "",
				Aliases:   []string{"o"},
				EnvVars:   []string{"GHOSTWRITER_OUTPUT"},
				TakesFile: true,
			},
		},
		Action: func(cliCtx *cli.Context) error {
			subject := cliCtx.String("subject")
			targetWords := cliCtx.Int("target-words")
			output := cliCtx.String("output")
			styleGuide := cliCtx.String("style-guide")
			workspace := cliCtx.String("workspace")
			additionalContext := cliCtx.String("additional-context")

			subject = strings.TrimSpace(subject)

			ctx, cancel := context.WithTimeout(cliCtx.Context, 2*time.Hour)
			defer cancel()

			// Create a LLM chat completion client
			baseClient, err := provider.Create(ctx, env.With("GHOSTWRITER_", ".env"))
			if err != nil {
				return errors.Wrapf(err, "failed to create llm client")
			}

			// Hook client to force injection of "middle-out" transform for OpenRouter provider
			middleOutClient := hook.Wrap(baseClient, hook.WithBeforeChatCompletionFunc(func(ctx context.Context, funcs []llm.ChatCompletionOptionFunc) (context.Context, []llm.ChatCompletionOptionFunc, error) {
				ctx = openrouter.WithTransforms(ctx, []string{openrouter.TransformMiddleOut})
				return ctx, funcs, nil
			}))

			// Wrap with retry logic (3 retries with 1 second base delay)
			retryClient := retry.Wrap(middleOutClient, time.Second, 3)

			// Wrap with rate limiting (max 30 requests per minute)
			rateLimitedClient := ratelimit.Wrap(retryClient, time.Minute/30, 1)

			// Wrap with circuit breaker (max 5 failures, 5 second reset timeout)
			resilientClient := circuitbreaker.NewClient(rateLimitedClient, 5, 5*time.Second)

			// Generate article
			slog.InfoContext(ctx, "generating article", slog.String("subject", subject))

			// Create progress tracking with simple logging
			progressCh, progressCallback := article.ProgressEventChannel()
			ctx = article.WithProgressTracking(ctx, progressCallback)

			orchestratorOptions := []article.OrchestratorOptionFunc{
				article.WithTargetWordCount(targetWords),
				article.WithMaxConcurrentWriters(2),
				article.WithResearchDepth(article.ResearchBasic),
				article.WithTimeout(time.Hour),
			}

			if styleGuide != "" {
				data, err := os.ReadFile(styleGuide)
				if err != nil {
					return errors.Wrapf(err, "failed to read style guidelines file")
				}

				orchestratorOptions = append(orchestratorOptions, article.WithStyleGuidelines(string(data)))
			}

			var additionalContextBuilder strings.Builder

			if additionalContext != "" {
				data, err := os.ReadFile(additionalContext)
				if err != nil {
					return errors.Wrapf(err, "failed to read additional context file")
				}

				additionalContextBuilder.WriteString(string(data))
			}

			tools := tool.GetDefaultResearchTools()

			if workspace != "" {
				root, err := os.OpenRoot(workspace)
				if err != nil {
					return errors.Wrapf(err, "failed to read style guidelines file")
				}

				tools = append(tools, tool.NewFSTools(root.FS())...)

				tree, err := tool.GenerateDirectoryTree(root.FS(), ".", ".git")
				if err != nil {
					return errors.Wrapf(err, "failed to generate workspace directory tree")
				}

				additionalContextBuilder.WriteString("\n\n**Workspace:**\n\n")
				additionalContextBuilder.WriteString("This is the directory tree of the files you have access to:\n\n")
				additionalContextBuilder.WriteString(tree)
			}

			orchestratorOptions = append(orchestratorOptions, article.WithTools(tools))
			orchestratorOptions = append(orchestratorOptions, article.WithAdditionalContext(additionalContextBuilder.String()))

			// Start simple progress logging goroutine
			done := make(chan bool)
			go logProgress(ctx, progressCh, done)

			// Generate the article using the multi-agent system
			generatedArticle, err := article.WriteArticle(ctx, resilientClient, subject,
				orchestratorOptions...,
			)

			// Signal progress logging to stop
			done <- true

			if err != nil {
				return errors.Wrapf(err, "failed to generate article")
			}

			var buff bytes.Buffer

			if _, err := io.WriteString(&buff, "---\n"); err != nil {
				return errors.WithStack(err)
			}

			encoder := yaml.NewEncoder(&buff)
			metadata := struct {
				article.DocumentMetadata `yaml:",inline"`
				Subject                  string
			}{
				Subject:          subject,
				DocumentMetadata: generatedArticle.DocumentMetadata,
			}
			if err := encoder.Encode(metadata); err != nil {
				return errors.Wrapf(err, "failed write document metadata")
			}

			if _, err := io.WriteString(&buff, "---\n\n"); err != nil {
				return errors.WithStack(err)
			}

			if _, err := io.WriteString(&buff, generatedArticle.Content); err != nil {
				return errors.WithStack(err)
			}

			if _, err := io.WriteString(&buff, "\n\n---\n\n"); err != nil {
				return errors.WithStack(err)
			}

			if _, err := io.WriteString(&buff, "**Sources**\n\n"); err != nil {
				return errors.WithStack(err)
			}

			for _, s := range generatedArticle.Sources {
				if strings.TrimSpace(s.URL) == "" {
					continue
				}

				if _, err := io.WriteString(&buff, fmt.Sprintf("- [%s](%s)\n", s.Title, s.URL)); err != nil {
					return errors.WithStack(err)
				}
			}

			if output == "" {
				output = slug.Make(generatedArticle.Title) + ".md"
			}

			if err := os.WriteFile(output, buff.Bytes(), 0644); err != nil {
				return errors.Wrapf(err, "failed to write article")
			}

			slog.InfoContext(ctx, "article written", slog.String("output", output))

			return nil
		},
	}
}

// logProgress shows simple progress updates using log package
func logProgress(ctx context.Context, progressCh <-chan article.ProgressEvent, done <-chan bool) {
	slog.InfoContext(ctx, "progress tracking")

	for {
		select {
		case event := <-progressCh:
			// Simple progress logging
			progressPercent := int(event.Progress() * 100)
			elapsed := event.ElapsedTime().Round(time.Second)

			// Log progress update
			slog.InfoContext(ctx, "step progress", slog.Int("progress", progressPercent), slog.String("step", event.Step()), slog.Duration("elapsed", elapsed))

			// Log phase transitions
			if details := event.Details(); details != nil {
				if phaseStart, ok := details["phase_start"].(bool); ok && phaseStart {
					slog.InfoContext(ctx, "step starting", slog.String("step", event.Step()))
				} else if phaseComplete, ok := details["phase_complete"].(bool); ok && phaseComplete {
					slog.InfoContext(ctx, "step completed", slog.String("step", event.Step()))
				} else if event.Progress() >= 1.0 {
					slog.InfoContext(ctx, "step finished", slog.String("step", event.Step()))
				}
			}

		case <-done:
			slog.InfoContext(ctx, "progress complete")
			return
		}
	}
}
