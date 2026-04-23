// Package dex provides adapters for decentralized exchange data.
package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// UniswapAdapter fetches DEX pool data via DeFi Llama (free, no auth).
// The Graph hosted service is deprecated; this uses the coins/prices API.
type UniswapAdapter struct {
	bus          *bus.Bus
	pollInterval time.Duration
	tokens       []dexToken
	// llamaURL overrides the DeFi Llama base URL; empty means prod.
	// Test-only seam so uniswap_debug_test can inject a mock server.
	llamaURL string

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

type dexToken struct {
	Chain   string // "ethereum", "bsc", "arbitrum", etc.
	Address string // contract address
	Symbol  string // display symbol
}

// Default tokens to track via DeFi Llama.
var defaultUniswapTokens = []dexToken{
	{Chain: "ethereum", Address: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", Symbol: "WETH"},
	{Chain: "ethereum", Address: "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599", Symbol: "WBTC"},
	{Chain: "ethereum", Address: "0x1f9840a85d5aF5bf1D1762F925BDADdC4201F984", Symbol: "UNI"},
	{Chain: "ethereum", Address: "0x514910771AF9Ca656af840dff83E8264EcF986CA", Symbol: "LINK"},
	{Chain: "ethereum", Address: "0x6B175474E89094C44Da98b954EedeAC495271d0F", Symbol: "DAI"},
	{Chain: "ethereum", Address: "0x7Fc66500c84A76Ad7e9c93437bFc5Ac33E2DDaE9", Symbol: "AAVE"},
	{Chain: "ethereum", Address: "0xD533a949740bb3306d119CC777fa900bA034cd52", Symbol: "CRV"},
	{Chain: "ethereum", Address: "0x5A98FcBEA516Cf06857215779Fd812CA3beF1B32", Symbol: "LDO"},
}

func NewUniswapAdapter(b *bus.Bus, pollInterval time.Duration) *UniswapAdapter {
	if pollInterval == 0 {
		pollInterval = 15 * time.Second
	}
	return &UniswapAdapter{
		bus:          b,
		pollInterval: pollInterval,
		tokens:       defaultUniswapTokens,
		state:        "disconnected",
	}
}

func (a *UniswapAdapter) Name() string { return "uniswap_v3" }

func (a *UniswapAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "uniswap_v3",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *UniswapAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

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

type llamaPriceResponse struct {
	Coins map[string]struct {
		Price     float64 `json:"price"`
		Symbol    string  `json:"symbol"`
		Decimals  int     `json:"decimals"`
		Timestamp int64   `json:"timestamp"`
		Confidence float64 `json:"confidence"`
	} `json:"coins"`
}

func (a *UniswapAdapter) fetch(ctx context.Context) {
	// Build comma-separated token list: "ethereum:0x...,ethereum:0x..."
	var keys []string
	for _, t := range a.tokens {
		keys = append(keys, fmt.Sprintf("%s:%s", t.Chain, t.Address))
	}

	base := "https://coins.llama.fi"
	if a.llamaURL != "" {
		base = a.llamaURL
	}
	url := fmt.Sprintf("%s/prices/current/%s", base, strings.Join(keys, ","))

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
		slog.Warn("uniswap/defi-llama fetch error", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.mu.Lock()
		a.errorCount++
		a.state = "error"
		a.mu.Unlock()
		slog.Warn("uniswap/defi-llama bad status", "status", resp.StatusCode)
		return
	}

	var result llamaPriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		slog.Warn("uniswap/defi-llama decode error", "error", err)
		return
	}

	now := time.Now()
	published := 0
	for _, t := range a.tokens {
		key := fmt.Sprintf("%s:%s", t.Chain, t.Address)
		coin, ok := result.Coins[key]
		if !ok || coin.Price == 0 {
			continue
		}

		symbol := t.Symbol
		if coin.Symbol != "" {
			symbol = coin.Symbol
		}

		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("ohlc.uniswap.%sUSD", symbol),
			Payload: feeds.OHLC{
				Instrument: symbol + "USD",
				Exchange:   "uniswap_v3",
				Timeframe:  "spot",
				Timestamp:  now,
				Open:       coin.Price,
				High:       coin.Price,
				Low:        coin.Price,
				Close:      coin.Price,
				Volume:     0,
			},
		})

		// Synthetic LOB from price.
		lobBids, lobAsks := syntheticLOB(coin.Price, 1e6)
		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("lob.uniswap.%sUSD", symbol),
			Payload: feeds.LOBSnapshot{
				Instrument: symbol + "USD",
				Exchange:   "uniswap_v3",
				Timestamp:  now,
				Bids:       lobBids,
				Asks:       lobAsks,
			},
		})
		published++
	}

	a.mu.Lock()
	a.lastUpdate = now
	a.state = "connected"
	a.bytesRecv += uint64(resp.ContentLength)
	a.mu.Unlock()

	// Info (not Debug) so the LOG tab shows live confirmation that
	// the Uniswap adapter is producing data.
	slog.Info("uniswap/defi-llama fetched", "tokens", published)
}

// syntheticLOB creates a synthetic order book approximation around the current price.
func syntheticLOB(currentPrice, liquidity float64) (bids []feeds.LOBLevel, asks []feeds.LOBLevel) {
	if currentPrice == 0 || liquidity == 0 {
		return nil, nil
	}

	for i := 1; i <= 10; i++ {
		offset := float64(i) * 0.001 * currentPrice
		depth := liquidity / (math.Pow(10, 18) * float64(i))
		if depth < 0 {
			depth = 0.01
		}

		bids = append(bids, feeds.LOBLevel{
			Price:    currentPrice - offset,
			Quantity: depth,
		})
		asks = append(asks, feeds.LOBLevel{
			Price:    currentPrice + offset,
			Quantity: depth,
		})
	}

	return bids, asks
}
