package shared

import (
	"context"
	"net/url"
	"os"
	"path/filepath"

	"github.com/bornholm/corpus/pkg/corpus"
	"github.com/bornholm/genai/llm/provider"
	providerenv "github.com/bornholm/genai/llm/provider/env"
	"github.com/bornholm/ghostwriter/internal/command/llmclient"
	"github.com/bornholm/ghostwriter/pkg/article"
	corpusadapter "github.com/bornholm/ghostwriter/pkg/knowledgebase/corpus"
	"github.com/pkg/errors"
)

// BuildKnowledgeBase creates a Corpus-backed knowledge base stored at storagePath.
// A dedicated LLM client is first attempted via GHOSTWRITER_CORPUS_* env vars.
// If those vars are absent, it falls back to the main GHOSTWRITER_* provider.
// Corpus can also run without an LLM client (disabling vector search, HyDE and Judge).
func BuildKnowledgeBase(ctx context.Context, storagePath string) (article.KnowledgeBase, func() error, error) {
	corpusLLMClient, err := provider.Create(ctx, providerenv.With("GHOSTWRITER_CORPUS_", ".env"))
	if err != nil {
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

// BootstrapKnowledgeBase adds files from the given glob patterns to the knowledge base.
func BootstrapKnowledgeBase(kb article.KnowledgeBase, files []string) error {
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
