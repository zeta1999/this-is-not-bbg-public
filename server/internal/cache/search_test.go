package cache

import "testing"

func TestBM25Search(t *testing.T) {
	idx := NewSearchIndex()

	idx.Index("1", "Bitcoin ETF approved by SEC", "The SEC has approved the first spot Bitcoin ETF.", "reuters", []string{"BTC"})
	idx.Index("2", "Ethereum upgrade scheduled", "The Ethereum network will undergo a major upgrade.", "coindesk", []string{"ETH"})
	idx.Index("3", "Bitcoin hits new all-time high", "Bitcoin reached $100,000 for the first time.", "bloomberg", []string{"BTC"})
	idx.Index("4", "Federal Reserve holds rates steady", "The Fed maintained interest rates unchanged.", "reuters", nil)

	// Search for "bitcoin".
	results := idx.Search("bitcoin", 10)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results for 'bitcoin', got %d", len(results))
	}
	// Both Bitcoin articles should rank higher than others.
	for _, r := range results {
		if r.Score <= 0 {
			t.Errorf("expected positive score, got %f for %s", r.Score, r.ID)
		}
	}

	// Search for "ETF".
	results = idx.Search("ETF", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'ETF', got %d", len(results))
	}
	if results[0].ID != "1" {
		t.Errorf("expected doc 1, got %s", results[0].ID)
	}

	// Search for "ethereum".
	results = idx.Search("ethereum", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'ethereum', got %d", len(results))
	}

	// Empty query.
	results = idx.Search("", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}
