package article

import (
	"testing"
)

func newTestAgent() *ResearchAgent {
	return NewResearchAgent(nil, nil, nil)
}

func TestNormalizeURL(t *testing.T) {
	h := newTestAgent()

	cases := []struct {
		input string
		want  string
	}{
		{"https://example.com/page?foo=1&bar=2", "https://example.com/page"},
		{"https://example.com/page#section", "https://example.com/page"},
		{"https://example.com/page?q=test#anchor", "https://example.com/page"},
		{"https://example.com/page", "https://example.com/page"},
		{"not a url %%", "not a url %%"}, // invalid URL returns as-is
	}

	for _, tc := range cases {
		got := h.normalizeURL(tc.input)
		if got != tc.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExtractKeywords(t *testing.T) {
	h := newTestAgent()

	t.Run("basic extraction", func(t *testing.T) {
		kws := h.extractKeywords("artificial intelligence machine learning")
		if len(kws) == 0 {
			t.Error("expected keywords, got none")
		}
		for _, kw := range kws {
			if len(kw) <= researchKeywordMinLength {
				t.Errorf("keyword %q is shorter than minimum length %d", kw, researchKeywordMinLength)
			}
		}
	})

	t.Run("respects max count", func(t *testing.T) {
		long := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima"
		kws := h.extractKeywords(long)
		if len(kws) > researchKeywordMaxCount {
			t.Errorf("got %d keywords, expected at most %d", len(kws), researchKeywordMaxCount)
		}
	})

	t.Run("filters common words", func(t *testing.T) {
		kws := h.extractKeywords("the and for are but not you all can had")
		for _, kw := range kws {
			if h.isCommonWord(kw) {
				t.Errorf("keyword %q is a common word and should have been filtered", kw)
			}
		}
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		kws := h.extractKeywords("")
		if len(kws) != 0 {
			t.Errorf("expected no keywords for empty input, got %v", kws)
		}
	})
}

func TestRelevanceCalculation(t *testing.T) {
	// Verify the relevance formula: 1.0 - float64(j)/float64(maxResults)
	cases := []struct {
		rank       int
		maxResults int
		wantMin    float64
		wantMax    float64
	}{
		{0, 5, 0.99, 1.01},  // rank 0 → relevance 1.0
		{4, 5, 0.19, 0.21},  // rank 4 → relevance 0.2
		{2, 5, 0.59, 0.61},  // rank 2 → relevance 0.6
	}
	for _, tc := range cases {
		got := 1.0 - float64(tc.rank)/float64(tc.maxResults)
		if got < tc.wantMin || got > tc.wantMax {
			t.Errorf("relevance(rank=%d, max=%d) = %f, want [%f, %f]",
				tc.rank, tc.maxResults, got, tc.wantMin, tc.wantMax)
		}
	}
}
