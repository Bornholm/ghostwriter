package article

import (
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/pkg/errors"
)

// ResearchDocument represents a single piece of research data
type ResearchDocument struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Keywords   []string `json:"keywords"`
	SourceType string   `json:"source_type"` // "web", "article", "academic", "news"
	Relevance  float64  `json:"relevance"`
}

// KnowledgeBase manages the centralized research data using Bleve
type KnowledgeBase struct {
	index     bleve.Index
	documents map[string]ResearchDocument
	mutex     sync.RWMutex
	subject   string
}

// NewKnowledgeBase creates a new in-memory knowledge base
func NewKnowledgeBase(subject string) (*KnowledgeBase, error) {
	// Create index mapping
	indexMapping := bleve.NewIndexMapping()

	// Configure document mapping
	docMapping := bleve.NewDocumentMapping()

	// Title field - searchable and stored
	titleFieldMapping := bleve.NewTextFieldMapping()
	titleFieldMapping.Store = true
	titleFieldMapping.Index = true
	docMapping.AddFieldMappingsAt("title", titleFieldMapping)

	// Content field - searchable
	contentFieldMapping := bleve.NewTextFieldMapping()
	contentFieldMapping.Store = false
	contentFieldMapping.Index = true
	docMapping.AddFieldMappingsAt("content", contentFieldMapping)

	// Summary field - searchable and stored
	summaryFieldMapping := bleve.NewTextFieldMapping()
	summaryFieldMapping.Store = true
	summaryFieldMapping.Index = true
	docMapping.AddFieldMappingsAt("summary", summaryFieldMapping)

	// Keywords field - searchable
	keywordsFieldMapping := bleve.NewTextFieldMapping()
	keywordsFieldMapping.Store = true
	keywordsFieldMapping.Index = true
	docMapping.AddFieldMappingsAt("keywords", keywordsFieldMapping)

	// Source type field - stored and searchable
	sourceTypeFieldMapping := bleve.NewKeywordFieldMapping()
	sourceTypeFieldMapping.Store = true
	sourceTypeFieldMapping.Index = true
	docMapping.AddFieldMappingsAt("source_type", sourceTypeFieldMapping)

	indexMapping.AddDocumentMapping("research_doc", docMapping)

	// Create in-memory index
	index, err := bleve.NewMemOnly(indexMapping)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &KnowledgeBase{
		index:     index,
		documents: make(map[string]ResearchDocument),
		subject:   subject,
	}, nil
}

// AddDocument adds a research document to the knowledge base
func (kb *KnowledgeBase) AddDocument(doc ResearchDocument) error {
	kb.mutex.Lock()
	defer kb.mutex.Unlock()

	// Store in map for quick access
	kb.documents[doc.ID] = doc

	// Index in Bleve for search
	err := kb.index.Index(doc.ID, doc)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

// Search performs full-text search across all documents
func (kb *KnowledgeBase) Search(query string, limit int) ([]ResearchDocument, error) {
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

// GetByID retrieves a document by its ID
func (kb *KnowledgeBase) GetByID(id string) (ResearchDocument, error) {
	kb.mutex.RLock()
	defer kb.mutex.RUnlock()

	doc, exists := kb.documents[id]
	if !exists {
		return ResearchDocument{}, errors.Errorf("document with ID %s not found", id)
	}

	return doc, nil
}

// FindByKeywords finds documents containing specific keywords
func (kb *KnowledgeBase) FindByKeywords(keywords []string) ([]ResearchDocument, error) {
	if len(keywords) == 0 {
		return []ResearchDocument{}, nil
	}

	// Build query for keywords - use match query instead of term query
	var queryStrings []string
	for _, keyword := range keywords {
		queryStrings = append(queryStrings, "keywords:"+keyword)
	}

	queryString := "(" + strings.Join(queryStrings, " OR ") + ")"
	searchQuery := bleve.NewQueryStringQuery(queryString)
	searchRequest := bleve.NewSearchRequest(searchQuery)
	searchRequest.Size = 50 // Reasonable limit

	kb.mutex.RLock()
	defer kb.mutex.RUnlock()

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

// FindBySourceType finds documents of a specific source type
func (kb *KnowledgeBase) FindBySourceType(sourceType string) ([]ResearchDocument, error) {
	queryString := "source_type:" + sourceType
	searchQuery := bleve.NewQueryStringQuery(queryString)
	searchRequest := bleve.NewSearchRequest(searchQuery)
	searchRequest.Size = 100

	kb.mutex.RLock()
	defer kb.mutex.RUnlock()

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

// GetAllDocuments returns all documents in the knowledge base
func (kb *KnowledgeBase) GetAllDocuments() []ResearchDocument {
	kb.mutex.RLock()
	defer kb.mutex.RUnlock()

	var docs []ResearchDocument
	for _, doc := range kb.documents {
		docs = append(docs, doc)
	}

	return docs
}

// GetStats returns statistics about the knowledge base
func (kb *KnowledgeBase) GetStats() map[string]interface{} {
	kb.mutex.RLock()
	defer kb.mutex.RUnlock()

	stats := make(map[string]interface{})
	stats["total_documents"] = len(kb.documents)
	stats["subject"] = kb.subject

	// Count by source type
	sourceTypeCounts := make(map[string]int)
	for _, doc := range kb.documents {
		sourceTypeCounts[doc.SourceType]++
	}
	stats["source_type_counts"] = sourceTypeCounts

	return stats
}

// Close closes the knowledge base and releases resources
func (kb *KnowledgeBase) Close() error {
	kb.mutex.Lock()
	defer kb.mutex.Unlock()

	if kb.index != nil {
		return kb.index.Close()
	}

	return nil
}
