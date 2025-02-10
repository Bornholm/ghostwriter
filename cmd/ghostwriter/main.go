package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/provider"
	"github.com/bornholm/genai/llm/tool/index"
	"github.com/hashicorp/go-envparse"
	"github.com/pkg/errors"

	_ "github.com/bornholm/genai/llm/provider/mistral"
	"github.com/bornholm/genai/llm/provider/openai"
	_ "github.com/bornholm/genai/llm/provider/openai"
)

//go:embed prompts/*.gotmpl
var promptFS embed.FS

var (
	envFile   string = ".env"
	outputDir string = ""
)

func init() {
	flag.StringVar(&envFile, "env-file", envFile, "environment file")
	flag.StringVar(&outputDir, "output-dir", outputDir, "output directory")
}

func main() {
	flag.Parse()

	filename := flag.Arg(0)

	outputPrefix := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))

	project, err := parseProjectFile(filename)
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	searchOptions, err := projectCorpusToResourceOptions(project.Corpus)
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	log.Println("Indexing content...")
	search, err := index.Search(searchOptions...)
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	tools := []llm.Tool{
		search,
	}

	baseClient, err := createClient(ctx, envFile)
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	toolClient, err := createClient(ctx, envFile, "TOOL_")
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	writingContext, err := createSectionContext(ctx, toolClient, project.Topic, &DocumentSection{
		Title:       project.Topic,
		Description: project.Topic,
	}, tools)
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	systemPrompt, err := llm.PromptTemplateFS(promptFS, "prompts/layout_system.gotmpl", struct {
		Context  string
		Language string
	}{
		Context:  writingContext,
		Language: project.Language,
	})
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, fmt.Sprintf("Give me a document layout to write about:\n\n%s", project.Topic)),
	}

	response, err := baseClient.ChatCompletion(
		ctx,
		llm.WithJSONResponse(llm.NewResponseSchema(
			"DocumentLayout",
			"A document layout",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{
						"type":        "string",
						"description": "The document title",
					},
					"sections": map[string]any{
						"type":        "array",
						"description": "The document sections",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"title": map[string]any{
									"type":        "string",
									"description": "The section title",
								},
								"description": map[string]any{
									"type":        "string",
									"description": "Description of what the section will cover",
								},
							},
							"required":             []string{"title", "description"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"title", "sections"},
				"additionalProperties": false,
			},
		)),
		llm.WithMessages(messages...),
	)
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	documentLayoutResults, err := llm.ParseJSON[DocumentLayout](response.Message())
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	if len(documentLayoutResults) == 0 {
		log.Printf("[FATAL] %+v", errors.New("invalid response from llm"))
		os.Exit(1)
	}

	documentLayout := documentLayoutResults[0]

	log.Println("Document layout:")
	log.Println("#", documentLayout.Title)
	for _, s := range documentLayout.Sections {
		log.Printf("##	%s\n\t%s", s.Title, s.Description)
	}

	var sb strings.Builder

	sb.WriteString("# ")

	sb.WriteString(documentLayout.Title)
	sb.WriteString("\n\n")

	var previousSectionContent string

	for _, section := range documentLayout.Sections {
		log.Printf("Processing section '%s'", section.Title)

		sb.WriteString("## ")
		sb.WriteString(section.Title)
		sb.WriteString("\n\n")

		log.Println("Generating context...")

		writingContext, err := createSectionContext(ctx, toolClient, project.Topic, section, tools)
		if err != nil {
			log.Printf("[FATAL] %+v", errors.WithStack(err))
			os.Exit(1)
		}

		log.Println("Writing section...")

		sectionContent, err := writeSection(ctx, baseClient, project.Language, writingContext, previousSectionContent, section)
		if err != nil {
			log.Printf("[FATAL] %+v", errors.WithStack(err))
			os.Exit(1)
		}

		previousSectionContent = sectionContent

		log.Printf("Generated content:\n\n%s", sectionContent)

		sb.WriteString(sectionContent)
		sb.WriteString("\n\n")
	}

	log.Println("Editing document...")

	document, err := editDocument(ctx, baseClient, project.Language, sb.String())
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}

	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0740); err != nil {
			log.Printf("[FATAL] %+v", errors.WithStack(err))
			os.Exit(1)
		}
	}

	outputFilename := fmt.Sprintf("%s_%s_%d.md", outputPrefix, baseClient.Model(), time.Now().Unix())
	outputPath := filepath.Join(outputDir, outputFilename)

	log.Printf("Writing document to '%s'...", outputPath)

	err = os.WriteFile(outputPath, []byte(document), 0640)
	if err != nil {
		log.Printf("[FATAL] %+v", errors.WithStack(err))
		os.Exit(1)
	}
}

type DocumentSection struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type DocumentLayout struct {
	Title    string             `json:"title"`
	Sections []*DocumentSection `json:"sections"`
}

func createSectionContext(ctx context.Context, client llm.Client, query string, section *DocumentSection, tools []llm.Tool) (string, error) {
	systemPrompt, err := llm.PromptTemplateFS[any](promptFS, "prompts/section_context_system.gotmpl", nil)
	if err != nil {
		return "", errors.WithStack(err)
	}

	userPrompt, err := llm.PromptTemplateFS[any](promptFS, "prompts/section_context_user.gotmpl", struct {
		Topic   string
		Context string
	}{
		Topic:   query,
		Context: section.Description,
	})
	if err != nil {
		return "", errors.WithStack(err)
	}

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	}

	maxIterations := 2
	totalIterations := 0
	done := false
	for {
		totalIterations++

		response, err := client.ChatCompletion(
			ctx,
			llm.WithTools(tools...),
			llm.WithMessages(messages...),
		)
		if err != nil {
			return "", errors.WithStack(err)
		}

		if done {
			return response.Message().Content(), nil
		}

		if totalIterations >= maxIterations {
			messages = append(messages, llm.NewMessage(llm.RoleAssistant, "I have found all the informations required."))
			messages = append(messages, llm.NewMessage(llm.RoleUser, "Write a synthesis of the informations you've found. Do not add any additional information than your synthesis."))
			done = true
			continue
		}

		toolCalls := response.ToolCalls()

		if len(toolCalls) > 0 {
			for _, tc := range toolCalls {
				result, err := llm.ExecuteToolCall(ctx, tc, tools...)
				if err != nil {
					return "", errors.WithStack(err)
				}

				messages = append(messages, tc)
				messages = append(messages, result)
			}

			continue
		}

		return response.Message().Content(), nil
	}
}

func writeSection(ctx context.Context, client llm.Client, language string, writingContext string, previousContent string, section *DocumentSection) (string, error) {
	systemPrompt, err := llm.PromptTemplateFS[any](promptFS, "prompts/section_system.gotmpl", struct {
		Context  string
		Language string
	}{
		Context:  writingContext,
		Language: language,
	})
	if err != nil {
		return "", errors.WithStack(err)
	}

	userPrompt, err := llm.PromptTemplateFS[any](promptFS, "prompts/section_user.gotmpl", struct {
		Topic           string
		PreviousContent string
	}{
		Topic:           section.Description,
		PreviousContent: previousContent,
	})
	if err != nil {
		return "", errors.WithStack(err)
	}

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, userPrompt),
	}

	response, err := client.ChatCompletion(
		ctx,
		llm.WithMessages(messages...),
	)
	if err != nil {
		return "", errors.WithStack(err)
	}

	return response.Message().Content(), nil
}

func editDocument(ctx context.Context, client llm.Client, language string, document string) (string, error) {
	systemPrompt, err := llm.PromptTemplateFS[any](promptFS, "prompts/editor_system.gotmpl", struct {
		Language string
	}{
		Language: language,
	})
	if err != nil {
		return "", errors.WithStack(err)
	}

	messages := []llm.Message{
		llm.NewMessage(llm.RoleSystem, systemPrompt),
		llm.NewMessage(llm.RoleUser, document),
	}

	response, err := client.ChatCompletion(
		ctx,
		llm.WithMessages(messages...),
	)
	if err != nil {
		return "", errors.WithStack(err)
	}

	return response.Message().Content(), nil
}

func createClient(ctx context.Context, environmentFile string, prefixes ...string) (llm.Client, error) {
	prefixes = append([]string{""}, prefixes...)

	dotEnvFile, err := os.Open(environmentFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.WithStack(err)
	}

	if dotEnvFile != nil {
		defer dotEnvFile.Close()

		dotEnv, err := envparse.Parse(dotEnvFile)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		for _, p := range prefixes {
			ctx = provider.FromMap(ctx, p, dotEnv)
		}
	}

	for _, p := range prefixes {
		ctx = provider.FromEnvironment(ctx, p)
	}

	client, err := provider.Create(ctx, openai.Name)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return client, nil
}
