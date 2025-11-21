package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bornholm/genai/llm/circuitbreaker"
	"github.com/bornholm/genai/llm/provider"
	"github.com/bornholm/genai/llm/ratelimit"
	"github.com/bornholm/genai/llm/retry"
	"github.com/bornholm/ghostwriter/pkg/article"
	"github.com/bornholm/ghostwriter/pkg/tool"
	"github.com/gosimple/slug"
	"github.com/pkg/errors"

	_ "github.com/bornholm/genai/llm/provider/all"
	"github.com/bornholm/genai/llm/provider/openrouter"

	"github.com/bornholm/genai/llm/provider/env"
)

var (
	subject                   string
	output                    string
	styleGuidelinesFilename   string
	additionalContextFilename string
	workspace                 string
)

func init() {
	flag.StringVar(&subject, "subject", "", "the article subject")
	flag.StringVar(&output, "output", "", "filename of the resulting article, default to slug of title")
	flag.StringVar(&styleGuidelinesFilename, "style", "", "filename containing the writing style guidelines")
	flag.StringVar(&additionalContextFilename, "context", "", "filename containing additional context information for the agents")
	flag.StringVar(&workspace, "workspace", "", "workspace directory that the agent can access")
}

func main() {
	flag.Parse()

	subject = strings.TrimSpace(subject)

	if subject == "" {
		fmt.Printf("Please specify a research subject.\nFlags:\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	// Create a LLM chat completion client
	baseClient, err := provider.Create(ctx, env.With("GHOSTWRITER_", ".env"))
	if err != nil {
		log.Fatalf("%+v", errors.WithStack(err))
	}

	// Wrap with retry logic (3 retries with 1 second base delay)
	retryClient := retry.Wrap(baseClient, time.Second, 3)

	// Wrap with rate limiting (max 30 requests per minute)
	rateLimitedClient := ratelimit.Wrap(retryClient, time.Minute/30, 1)

	// Wrap with circuit breaker (max 5 failures, 5 second reset timeout)
	resilientClient := circuitbreaker.NewClient(rateLimitedClient, 5, 5*time.Second)

	// Generate article
	log.Println("=== GHOSTWRITER ===")
	log.Printf("Generating article on: %s", subject)

	// Create progress tracking with simple logging
	progressCh, progressCallback := article.ProgressEventChannel()
	ctx = article.WithProgressTracking(ctx, progressCallback)

	orchestratorOptions := []article.OrchestratorOptionFunc{
		article.WithTargetWordCount(2000),
		article.WithMaxConcurrentWriters(2),
		article.WithResearchDepth(article.ResearchDeep),
		article.WithTimeout(time.Hour),
	}

	if styleGuidelinesFilename != "" {
		data, err := os.ReadFile(styleGuidelinesFilename)
		if err != nil {
			log.Fatalf("Failed to read style guidelines file: %+v", errors.WithStack(err))
		}

		orchestratorOptions = append(orchestratorOptions, article.WithStyleGuidelines(string(data)))
	}

	var additionalContext strings.Builder

	if additionalContextFilename != "" {
		data, err := os.ReadFile(additionalContextFilename)
		if err != nil {
			log.Fatalf("Failed to read additional context file: %+v", errors.WithStack(err))
		}

		additionalContext.WriteString(string(data))
	}

	if workspace != "" {
		root, err := os.OpenRoot(workspace)
		if err != nil {
			log.Fatalf("Failed to read style guidelines file: %+v", errors.WithStack(err))
		}

		tools := tool.GetDefaultResearchTools()
		tools = append(tools, tool.NewFSTools(root.FS())...)

		tree, err := tool.GenerateDirectoryTree(root.FS(), ".", ".git")
		if err != nil {
			log.Fatalf("Failed to generate workspace directory tree: %+v", errors.WithStack(err))
		}

		additionalContext.WriteString("\n\n**Workspace:**\n\n")
		additionalContext.WriteString("This is the directory tree of the files you have access to:\n\n")
		additionalContext.WriteString(tree)

		orchestratorOptions = append(orchestratorOptions, article.WithTools(tools))
	}

	orchestratorOptions = append(orchestratorOptions, article.WithAdditionalContext(additionalContext.String()))

	// Start simple progress logging goroutine
	done := make(chan bool)
	go logProgress(progressCh, done)

	// Enable middle-out transforms when using openrouter provider
	ctx = openrouter.WithTransforms(ctx, []string{"middle-out"})

	// Generate the article using the multi-agent system
	generatedArticle, err := article.WriteArticle(ctx, resilientClient, subject,
		orchestratorOptions...,
	)

	// Signal progress logging to stop
	done <- true

	if err != nil {
		log.Fatalf("Failed to generate article: %+v", errors.WithStack(err))
	}

	// Log the results
	log.Println("=== ARTICLE GENERATION COMPLETE ===")
	log.Printf("Title: %s", generatedArticle.Title)
	log.Printf("Word Count: %d words", generatedArticle.WordCount)
	log.Printf("Sections: %d", len(generatedArticle.Sections))
	log.Printf("Sources: %d", len(generatedArticle.Sources))
	log.Printf("Generated: %s", generatedArticle.CreatedAt.Format("2006-01-02 15:04:05"))
	log.Printf("Completed: %s", generatedArticle.CompletedAt.Format("2006-01-02 15:04:05"))
	log.Printf("Duration: %s", generatedArticle.CompletedAt.Sub(generatedArticle.CreatedAt).Round(time.Second))

	// Log article summary
	log.Println("=== ARTICLE SUMMARY ===")
	log.Println(generatedArticle.Summary)

	// Log section breakdown
	log.Println("=== SECTION BREAKDOWN ===")
	for i, section := range generatedArticle.Sections {
		log.Printf("%d. %s (%d words) - Written by: %s",
			i+1, section.Title, section.WordCount, section.WrittenBy)
	}

	// Log sources
	if len(generatedArticle.Sources) > 0 {
		log.Println("=== SOURCES ===")
		for i, source := range generatedArticle.Sources {
			log.Printf("%d. %s", i+1, source)
		}
	}

	// Log keywords
	if len(generatedArticle.Keywords) > 0 {
		log.Println("=== KEYWORDS ===")
		log.Printf("Keywords: %s", joinStrings(generatedArticle.Keywords, ", "))
	}

	// Log the full article content
	log.Println("=== FULL ARTICLE ===")
	log.Println(generatedArticle.Content)

	if output == "" {
		output = slug.Make(generatedArticle.Title) + ".md"
	}

	if err := os.WriteFile(output, []byte(generatedArticle.Content), 0644); err != nil {
		log.Fatalf("Failed to write article: %+v", errors.WithStack(err))
	}

	log.Printf("Article written to: %s", output)
}

// logProgress shows simple progress updates using log package
func logProgress(progressCh <-chan article.ProgressEvent, done <-chan bool) {
	log.Println("=== PROGRESS TRACKING ===")

	for {
		select {
		case event := <-progressCh:
			// Simple progress logging
			progressPercent := int(event.Progress() * 100)
			elapsed := event.ElapsedTime().Round(time.Second)

			// Log progress update
			log.Printf("Progress: %d%% | %s | Elapsed: %s", progressPercent, event.Step(), elapsed)

			// Log phase transitions
			if details := event.Details(); details != nil {
				if phaseStart, ok := details["phase_start"].(bool); ok && phaseStart {
					log.Printf("Starting: %s", event.Step())
				} else if phaseComplete, ok := details["phase_complete"].(bool); ok && phaseComplete {
					log.Printf("Completed: %s", event.Step())
				} else if event.Progress() >= 1.0 {
					log.Printf("Finished: %s", event.Step())
				}
			}

		case <-done:
			log.Println("=== PROGRESS COMPLETE ===")
			return
		}
	}
}

// Helper function to join strings
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}

	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
