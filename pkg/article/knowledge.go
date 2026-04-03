package article

import (
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/pkg/errors"
)

// ResearchDocument represents a single piece of research data.
type ResearchDocument struct {
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Keywords   []string `json:"keywords"`
	SourceType string   `json:"source_type"` // "web", "article", "academic", "news"
	Relevance  float64  `json:"relevance"`
}

// KnowledgeBase is the interface for storing and searching research documents.
type KnowledgeBase interface {
	AddDocument(doc ResearchDocument) error
	HasDocument(url string) bool
	Search(query string, limit int) ([]ResearchDocument, error)
	GetAllDocuments() []ResearchDocument
	GetStats() map[string]interface{}
	Close() error
}

// BleveKnowledgeBase manages the centralized research data using Bleve.
type BleveKnowledgeBase struct {
	index     bleve.Index
	documents map[string]ResearchDocument
	mutex     sync.RWMutex
}

// NewKnowledgeBase creates a new in-memory Bleve-backed knowledge base.
func NewKnowledgeBase() (KnowledgeBase, error) {
	// Create index mapping
	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultAnalyzer = AnalyzerDynamicLang

	// Configure document mapping
	docMapping := bleve.NewDocumentMapping()
	docMapping.DefaultAnalyzer = AnalyzerDynamicLang

	// Title field - searchable and stored
	titleFieldMapping := bleve.NewTextFieldMapping()
	titleFieldMapping.Store = true
	titleFieldMapping.Index = true
	titleFieldMapping.Analyzer = AnalyzerDynamicLang
	docMapping.AddFieldMappingsAt("title", titleFieldMapping)

	// Content field - searchable
	contentFieldMapping := bleve.NewTextFieldMapping()
	contentFieldMapping.Store = false
	contentFieldMapping.Index = true
	contentFieldMapping.Analyzer = AnalyzerDynamicLang
	docMapping.AddFieldMappingsAt("content", contentFieldMapping)

	urlFieldMapping := bleve.NewTextFieldMapping()
	urlFieldMapping.Store = false
	urlFieldMapping.Index = true
	urlFieldMapping.Analyzer = AnalyzerDynamicLang
	docMapping.AddFieldMappingsAt("url", urlFieldMapping)

	// Keywords field - searchable
	keywordsFieldMapping := bleve.NewTextFieldMapping()
	keywordsFieldMapping.Store = true
	keywordsFieldMapping.Index = true
	keywordsFieldMapping.Analyzer = AnalyzerDynamicLang
	docMapping.AddFieldMappingsAt("keywords", keywordsFieldMapping)

	// Source type field - stored and searchable
	sourceTypeFieldMapping := bleve.NewKeywordFieldMapping()
	sourceTypeFieldMapping.Store = true
	sourceTypeFieldMapping.Index = true
	sourceTypeFieldMapping.Analyzer = AnalyzerDynamicLang
	docMapping.AddFieldMappingsAt("source_type", sourceTypeFieldMapping)

	indexMapping.AddDocumentMapping("research_doc", docMapping)

	// Create in-memory index
	index, err := bleve.NewMemOnly(indexMapping)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &BleveKnowledgeBase{
		index:     index,
		documents: make(map[string]ResearchDocument),
	}, nil
}

// HasDocument reports whether a document with the given URL is already indexed.
func (kb *BleveKnowledgeBase) HasDocument(url string) bool {
	kb.mutex.RLock()
	defer kb.mutex.RUnlock()
	_, exists := kb.documents[url]
	return exists
}

// AddDocument adds a research document to the knowledge base.
func (kb *BleveKnowledgeBase) AddDocument(doc ResearchDocument) error {
	kb.mutex.Lock()
	defer kb.mutex.Unlock()

	kb.documents[doc.URL] = doc

	err := kb.index.Index(doc.URL, doc)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

// Search performs full-text search across all documents.
func (kb *BleveKnowledgeBase) Search(query string, limit int) ([]ResearchDocument, error) {
	kb.mutex.RLock()
	defer kb.mutex.RUnlock()

	searchRequest := bleve.NewSearchRequest(bleve.NewQueryStringQuery(query))
	searchRequest.Size = limit
	searchRequest.Highlight = bleve.NewHighlight()

	searchResults, err := kb.index.Search(searchRequest)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var results []ResearchDocument
	for _, hit := range searchResults.Hits {
		if doc, exists := kb.documents[hit.ID]; exists {
			doc.Relevance = hit.Score
			results = append(results, doc)
		}
	}

	return results, nil
}

// GetAllDocuments returns all documents in the knowledge base.
func (kb *BleveKnowledgeBase) GetAllDocuments() []ResearchDocument {
	kb.mutex.RLock()
	defer kb.mutex.RUnlock()

	var docs []ResearchDocument
	for _, doc := range kb.documents {
		docs = append(docs, doc)
	}

	return docs
}

// GetStats returns statistics about the knowledge base.
func (kb *BleveKnowledgeBase) GetStats() map[string]interface{} {
	kb.mutex.RLock()
	defer kb.mutex.RUnlock()

	stats := make(map[string]interface{})
	stats["total_documents"] = len(kb.documents)

	sourceTypeCounts := make(map[string]int)
	for _, doc := range kb.documents {
		sourceTypeCounts[doc.SourceType]++
	}
	stats["source_type_counts"] = sourceTypeCounts

	return stats
}

// Close closes the knowledge base and releases resources.
func (kb *BleveKnowledgeBase) Close() error {
	kb.mutex.Lock()
	defer kb.mutex.Unlock()

	if kb.index != nil {
		return kb.index.Close()
	}

	return nil
}
