// Package cache provides a bus subscriber that indexes news into BM25 search.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/notbbg/notbbg/server/internal/bus"
)

// Indexer subscribes to news topics and indexes items into the search index.
type Indexer struct {
	index *SearchIndex
	bus   *bus.Bus
}

// NewIndexer creates a news indexer.
func NewIndexer(index *SearchIndex, b *bus.Bus) *Indexer {
	return &Indexer{index: index, bus: b}
}

// Run subscribes to news and indexes incoming items. Blocks until ctx is cancelled.
func (ix *Indexer) Run(ctx context.Context) error {
	sub := ix.bus.Subscribe(256, "news")
	defer ix.bus.Unsubscribe(sub)

	var count int
	for {
		select {
		case <-ctx.Done():
			slog.Info("news indexer stopped", "indexed", count)
			return nil
		case msg, ok := <-sub.C:
			if !ok {
				return nil
			}
			ix.indexMsg(msg)
			count++
		}
	}
}

func (ix *Indexer) indexMsg(msg bus.Message) {
	// JSON roundtrip to normalize any struct type to map[string]any.
	data, err := json.Marshal(msg.Payload)
	if err != nil {
		return
	}
	var v map[string]any
	if json.Unmarshal(data, &v) != nil {
		return
	}

	id := fmt.Sprint(v["ID"])
	title := fmt.Sprint(v["Title"])
	body := fmt.Sprint(v["Body"])
	source := fmt.Sprint(v["Source"])
	var tickers []string
	if raw, ok := v["Tickers"].([]any); ok {
		for _, t := range raw {
			tickers = append(tickers, fmt.Sprint(t))
		}
	}
	if title != "" && title != "<nil>" {
		ix.index.Index(id, title, body, source, tickers)
	}
}
