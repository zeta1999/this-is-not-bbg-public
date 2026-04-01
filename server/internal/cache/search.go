// Package cache provides BM25 full-text search over cached news items.
package cache

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
)

// SearchIndex is a BM25 inverted index for news items.
type SearchIndex struct {
	mu   sync.RWMutex
	docs map[string]*searchDoc // doc ID -> doc
	idf  map[string]float64   // term -> IDF
	avgDL float64
}

type searchDoc struct {
	ID       string
	Title    string
	Body     string
	Tickers  []string
	Source   string
	TermFreq map[string]int
	DocLen   int
}

// SearchResult is a scored search result.
type SearchResult struct {
	ID    string
	Score float64
	Title string
}

// NewSearchIndex creates an empty search index.
func NewSearchIndex() *SearchIndex {
	return &SearchIndex{
		docs: make(map[string]*searchDoc),
		idf:  make(map[string]float64),
	}
}

// Index adds a document to the search index.
func (si *SearchIndex) Index(id, title, body, source string, tickers []string) {
	si.mu.Lock()
	defer si.mu.Unlock()

	// Tokenize with field weights.
	terms := make(map[string]int)
	for _, t := range tokenize(title) {
		terms[t] += 2 // title weight
	}
	for _, t := range tokenize(body) {
		terms[t]++
	}
	for _, ticker := range tickers {
		for _, t := range tokenize(ticker) {
			terms[t] += 3 // ticker weight
		}
	}

	docLen := 0
	for _, count := range terms {
		docLen += count
	}

	si.docs[id] = &searchDoc{
		ID:       id,
		Title:    title,
		Body:     body,
		Tickers:  tickers,
		Source:   source,
		TermFreq: terms,
		DocLen:   docLen,
	}

	si.recomputeStats()
}

func (si *SearchIndex) recomputeStats() {
	n := float64(len(si.docs))
	if n == 0 {
		return
	}

	// Document frequency per term.
	df := make(map[string]int)
	totalLen := 0
	for _, doc := range si.docs {
		totalLen += doc.DocLen
		seen := make(map[string]bool)
		for term := range doc.TermFreq {
			if !seen[term] {
				df[term]++
				seen[term] = true
			}
		}
	}

	si.avgDL = float64(totalLen) / n
	si.idf = make(map[string]float64)
	for term, freq := range df {
		si.idf[term] = math.Log(1 + (n-float64(freq)+0.5)/(float64(freq)+0.5))
	}
}

// Search performs a BM25 query and returns scored results.
func (si *SearchIndex) Search(query string, limit int) []SearchResult {
	si.mu.RLock()
	defer si.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	const k1 = 1.2
	const b = 0.75

	type scored struct {
		id    string
		title string
		score float64
	}

	var results []scored
	for _, doc := range si.docs {
		score := 0.0
		for _, qt := range queryTerms {
			tf := float64(doc.TermFreq[qt])
			idf := si.idf[qt]
			dl := float64(doc.DocLen)
			num := tf * (k1 + 1)
			den := tf + k1*(1-b+b*dl/si.avgDL)
			score += idf * num / den
		}
		if score > 0 {
			results = append(results, scored{id: doc.ID, title: doc.Title, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	out := make([]SearchResult, len(results))
	for i, r := range results {
		out[i] = SearchResult{ID: r.id, Score: r.score, Title: r.title}
	}
	return out
}

// SearchJSON performs a BM25 search and returns JSON bytes for HTTP response.
func (si *SearchIndex) SearchJSON(query string, limit int) []byte {
	results := si.Search(query, limit)
	data, _ := json.Marshal(results)
	return data
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	// Remove very short terms.
	var out []string
	for _, w := range words {
		if len(w) >= 2 {
			out = append(out, w)
		}
	}
	return out
}
