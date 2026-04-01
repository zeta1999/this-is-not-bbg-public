// Package rss provides an RSS feed aggregator for financial news.
package rss

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// NewsItem represents a parsed news item from RSS.
type NewsItem struct {
	ID        string
	Title     string
	Body      string
	Source    string
	URL       string
	Published time.Time
	Tickers  []string
	Keywords []string
}

// Adapter aggregates multiple RSS feeds.
type Adapter struct {
	bus          *bus.Bus
	feeds        []string
	pollInterval time.Duration

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
	seen       map[string]bool // dedup by URL hash
	parser     *gofeed.Parser
}

// DefaultFeeds is a curated list of financial news RSS feeds.
var DefaultFeeds = []string{
	"https://feeds.reuters.com/reuters/businessNews",
	"https://feeds.reuters.com/reuters/technologyNews",
	"https://feeds.bbci.co.uk/news/business/rss.xml",
	"https://rss.nytimes.com/services/xml/rss/nyt/Business.xml",
	"https://www.coindesk.com/arc/outboundfeeds/rss/",
	"https://cointelegraph.com/rss",
	"https://decrypt.co/feed",
	"https://thedefiant.io/feed",
	"https://www.theblockcrypto.com/rss.xml",
	"https://blockworks.co/feed",
	"https://www.ft.com/rss/home/us",
	"https://feeds.bloomberg.com/markets/news.rss",
	"https://www.wsj.com/xml/rss/3_7085.xml",
	"https://rss.cnn.com/rss/money_latest.rss",
	"https://feeds.marketwatch.com/marketwatch/topstories/",
	"https://finance.yahoo.com/news/rssindex",
	"https://seekingalpha.com/market_currents.xml",
	"https://www.zerohedge.com/fullrss2.xml",
	"https://feeds.feedburner.com/zerohedge/feed",
	"https://www.economist.com/finance-and-economics/rss.xml",
}

// NewAdapter creates an RSS feed aggregator.
func NewAdapter(b *bus.Bus, feedURLs []string, pollInterval time.Duration) *Adapter {
	if len(feedURLs) == 0 {
		feedURLs = DefaultFeeds
	}
	if pollInterval == 0 {
		pollInterval = 5 * time.Minute
	}
	return &Adapter{
		bus:          b,
		feeds:        feedURLs,
		pollInterval: pollInterval,
		state:        "disconnected",
		seen:         make(map[string]bool),
		parser:       gofeed.NewParser(),
	}
}

func (a *Adapter) Name() string { return "rss" }

func (a *Adapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "rss",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	a.fetchAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.fetchAll(ctx)
		}
	}
}

func (a *Adapter) fetchAll(ctx context.Context) {
	for _, feedURL := range a.feeds {
		select {
		case <-ctx.Done():
			return
		default:
		}

		a.fetchFeed(ctx, feedURL)
	}

	a.mu.Lock()
	a.lastUpdate = time.Now()
	a.state = "connected"
	a.mu.Unlock()
}

func (a *Adapter) fetchFeed(ctx context.Context, feedURL string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	feed, err := a.parser.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		slog.Debug("rss fetch error", "url", feedURL, "error", err)
		return
	}

	source := feed.Title
	if source == "" {
		source = feedURL
	}

	for _, item := range feed.Items {
		if item == nil {
			continue
		}

		// Dedup by URL.
		urlHash := hashURL(item.Link)
		a.mu.Lock()
		if a.seen[urlHash] {
			a.mu.Unlock()
			continue
		}
		a.seen[urlHash] = true
		a.mu.Unlock()

		published := time.Now()
		if item.PublishedParsed != nil {
			published = *item.PublishedParsed
		} else if item.UpdatedParsed != nil {
			published = *item.UpdatedParsed
		}

		tickers := extractTickers(item.Title + " " + item.Description)

		news := NewsItem{
			ID:        urlHash,
			Title:     item.Title,
			Body:      item.Description,
			Source:    source,
			URL:       item.Link,
			Published: published,
			Tickers:  tickers,
		}

		a.bus.Publish(bus.Message{
			Topic:   "news",
			Payload: news,
		})
	}
}

func hashURL(url string) string {
	h := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", h[:8])
}

// extractTickers looks for common ticker patterns in text.
var knownTickers = []string{
	"BTC", "ETH", "SOL", "XRP", "ADA", "DOT", "AVAX", "MATIC", "LINK",
	"DOGE", "SHIB", "UNI", "AAVE", "CRV", "LDO", "ARB", "OP",
	"AAPL", "GOOGL", "MSFT", "AMZN", "TSLA", "NVDA", "META",
	"SPX", "DJI", "NASDAQ", "VIX",
}

func extractTickers(text string) []string {
	upper := strings.ToUpper(text)
	var found []string
	for _, t := range knownTickers {
		if strings.Contains(upper, t) {
			found = append(found, t)
		}
	}
	return found
}
