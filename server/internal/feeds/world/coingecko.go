// Package world provides adapters for non-exchange data sources (world monitor feeds).
package world

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// CoinGeckoAdapter fetches cryptocurrency market data from the CoinGecko API.
type CoinGeckoAdapter struct {
	bus          *bus.Bus
	pollInterval time.Duration
	mu           sync.RWMutex
	state        string
	lastUpdate   time.Time
	errorCount   uint64
	bytesRecv    uint64
}

func NewCoinGeckoAdapter(b *bus.Bus, pollInterval time.Duration) *CoinGeckoAdapter {
	if pollInterval == 0 {
		pollInterval = 30 * time.Second
	}
	return &CoinGeckoAdapter{
		bus:          b,
		pollInterval: pollInterval,
		state:        "disconnected",
	}
}

func (a *CoinGeckoAdapter) Name() string { return "coingecko" }

func (a *CoinGeckoAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "coingecko",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *CoinGeckoAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	// Fetch immediately, then on tick.
	a.fetch(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.fetch(ctx)
		}
	}
}

type coinGeckoMarket struct {
	ID            string  `json:"id"`
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	CurrentPrice  float64 `json:"current_price"`
	High24h       float64 `json:"high_24h"`
	Low24h        float64 `json:"low_24h"`
	PriceChange24 float64 `json:"price_change_percentage_24h"`
	TotalVolume   float64 `json:"total_volume"`
	MarketCap     float64 `json:"market_cap"`
}

func (a *CoinGeckoAdapter) fetch(ctx context.Context) {
	url := "https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&order=market_cap_desc&per_page=50&page=1&sparkline=false"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.mu.Lock()
		a.errorCount++
		a.state = "error"
		a.mu.Unlock()
		slog.Debug("coingecko fetch error", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		slog.Debug("coingecko bad status", "status", resp.StatusCode)
		return
	}

	var markets []coinGeckoMarket
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		return
	}

	now := time.Now()
	for _, m := range markets {
		ohlc := feeds.OHLC{
			Instrument: fmt.Sprintf("%sUSD", m.Symbol),
			Exchange:   "coingecko",
			Timeframe:  "spot",
			Timestamp:  now,
			Open:       m.CurrentPrice, // CoinGecko doesn't give real OHLC, use current as approximation
			High:       m.High24h,
			Low:        m.Low24h,
			Close:      m.CurrentPrice,
			Volume:     m.TotalVolume,
		}
		a.bus.Publish(bus.Message{
			Topic:   fmt.Sprintf("ohlc.coingecko.%sUSD", m.Symbol),
			Payload: ohlc,
		})
	}

	a.mu.Lock()
	a.lastUpdate = now
	a.state = "connected"
	a.bytesRecv += uint64(resp.ContentLength)
	a.mu.Unlock()

	slog.Debug("coingecko fetched", "markets", len(markets))
}
