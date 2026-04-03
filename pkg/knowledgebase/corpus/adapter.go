package corpus

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bornholm/corpus/pkg/corpus"
	"github.com/bornholm/corpus/pkg/model"
	"github.com/bornholm/corpus/pkg/port"
	"github.com/bornholm/ghostwriter/pkg/article"
	"github.com/pkg/errors"
)

const (
	indexingPollInterval = 200 * time.Millisecond
	indexingTimeout      = 2 * time.Minute
	searchTimeout        = 5 * time.Minute
)

// Adapter implements article.KnowledgeBase using a Corpus instance.
// Documents are indexed into Corpus for semantic search; a local cache
// provides GetAllDocuments and enables doc reconstruction after Search.
type Adapter struct {
	c            *corpus.Corpus
	collectionID model.CollectionID
	docs         map[string]article.ResearchDocument
	mu           sync.RWMutex
}

// New returns an Adapter backed by the given Corpus and collection.
func New(c *corpus.Corpus, collectionID model.CollectionID) *Adapter {
	return &Adapter{
		c:            c,
		collectionID: collectionID,
		docs:         make(map[string]article.ResearchDocument),
	}
}

// HasDocument reports whether a document with the given URL is already cached.
func (a *Adapter) HasDocument(rawURL string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	_, exists := a.docs[u.String()]
	return exists
}

// AddDocument indexes the document in Corpus and caches it locally.
// Blocks until indexing completes or times out.
func (a *Adapter) AddDocument(doc article.ResearchDocument) error {
	sourceURL := docSourceURL(doc)

	content := formatDocAsMarkdown(doc)

	ctx, cancel := context.WithTimeout(context.Background(), indexingTimeout)
	defer cancel()

	taskID, err := a.c.IndexFile(ctx, a.collectionID, doc.Title+".md",
		strings.NewReader(content),
		corpus.WithIndexFileSource(sourceURL),
	)
	if err != nil {
		return errors.WithStack(err)
	}

	if err := waitForTask(ctx, a.c, taskID); err != nil {
		return errors.WithStack(err)
	}

	a.mu.Lock()
	a.docs[sourceURL.String()] = doc
	a.mu.Unlock()

	return nil
}

// Search queries Corpus and reconstructs ResearchDocuments from the local cache.
func (a *Adapter) Search(query string, limit int) ([]article.ResearchDocument, error) {
	ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	defer cancel()

	results, err := a.c.Search(ctx, query,
		corpus.WithSearchCollections(a.collectionID),
		corpus.WithSearchMaxResults(limit),
	)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	total := len(results)
	seen := make(map[string]bool, total)
	docs := make([]article.ResearchDocument, 0, total)

	for i, r := range results {
		if r.Source == nil {
			continue
		}
		key := r.Source.String()
		if seen[key] {
			continue
		}
		seen[key] = true

		doc, ok := a.docs[key]
		if !ok {
			continue
		}

		// Assign a position-based relevance score (highest for first result).
		doc.Relevance = float64(total-i) / float64(total)
		docs = append(docs, doc)
	}

	return docs, nil
}

// GetAllDocuments returns all cached documents.
func (a *Adapter) GetAllDocuments() []article.ResearchDocument {
	a.mu.RLock()
	defer a.mu.RUnlock()

	docs := make([]article.ResearchDocument, 0, len(a.docs))
	for _, doc := range a.docs {
		docs = append(docs, doc)
	}
	return docs
}

// GetStats returns basic statistics from the local cache.
func (a *Adapter) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	sourceTypeCounts := make(map[string]int)
	for _, doc := range a.docs {
		sourceTypeCounts[doc.SourceType]++
	}

	return map[string]interface{}{
		"total_documents":   len(a.docs),
		"source_type_counts": sourceTypeCounts,
	}
}

// Close is a no-op; Corpus manages its own resources.
func (a *Adapter) Close() error {
	return nil
}

// docSourceURL returns a URL for the document, generating a synthetic one when
// the document has no URL (e.g. documents added from plain text).
func docSourceURL(doc article.ResearchDocument) *url.URL {
	if doc.URL != "" {
		u, err := url.Parse(doc.URL)
		if err == nil {
			return u
		}
	}

	return &url.URL{
		Scheme: "doc",
		Host:   "ghostwriter",
		Path:   "/" + url.PathEscape(doc.Title),
	}
}

// formatDocAsMarkdown converts a ResearchDocument to indexed Markdown.
func formatDocAsMarkdown(doc article.ResearchDocument) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", doc.Title)
	if len(doc.Keywords) > 0 {
		fmt.Fprintf(&b, "Keywords: %s\n\n", strings.Join(doc.Keywords, ", "))
	}
	b.WriteString(doc.Content)
	return b.String()
}

// waitForTask polls GetTaskState until the task succeeds, fails, or ctx expires.
func waitForTask(ctx context.Context, c *corpus.Corpus, taskID model.TaskID) error {
	for {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "timed out waiting for corpus indexing task")
		default:
		}

		state, err := c.GetTaskState(ctx, taskID)
		if err != nil {
			return errors.WithStack(err)
		}

		switch state.Status {
		case port.TaskStatusSucceeded:
			return nil
		case port.TaskStatusFailed:
			return errors.New("corpus indexing task failed")
		}

		time.Sleep(indexingPollInterval)
	}
}
