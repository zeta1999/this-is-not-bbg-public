package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// DefiProtocolAdapter is a generic adapter that fetches DeFi token prices
// via DeFi Llama's free API. Used for GMX, dYdX, Drift, and other protocols.
type DefiProtocolAdapter struct {
	bus          *bus.Bus
	name         string
	pollInterval time.Duration
	tokens       []defiToken

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

type defiToken struct {
	Chain   string
	Address string
	Symbol  string
}

func newDefiProtocolAdapter(b *bus.Bus, name string, pollInterval time.Duration, tokens []defiToken) *DefiProtocolAdapter {
	if pollInterval == 0 {
		pollInterval = 30 * time.Second
	}
	return &DefiProtocolAdapter{
		bus:          b,
		name:         name,
		pollInterval: pollInterval,
		tokens:       tokens,
		state:        "disconnected",
	}
}

func (a *DefiProtocolAdapter) Name() string { return a.name }

func (a *DefiProtocolAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          a.name,
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *DefiProtocolAdapter) Start(ctx context.Context) error {
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

func (a *DefiProtocolAdapter) fetch(ctx context.Context) {
	var keys []string
	for _, t := range a.tokens {
		keys = append(keys, fmt.Sprintf("%s:%s", t.Chain, t.Address))
	}

	url := fmt.Sprintf("https://coins.llama.fi/prices/current/%s", strings.Join(keys, ","))
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
		slog.Warn(a.name+" fetch error", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.mu.Lock()
		a.errorCount++
		a.state = "error"
		a.mu.Unlock()
		return
	}

	var result llamaPriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
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
			Topic: fmt.Sprintf("ohlc.%s.%sUSD", a.name, symbol),
			Payload: feeds.OHLC{
				Instrument: symbol + "USD", Exchange: a.name, Timeframe: "spot",
				Timestamp: now,
				Open: coin.Price, High: coin.Price, Low: coin.Price, Close: coin.Price,
			},
		})
		published++
	}

	a.mu.Lock()
	a.lastUpdate = now
	a.state = "connected"
	a.bytesRecv += uint64(resp.ContentLength)
	a.mu.Unlock()

	slog.Info(a.name+" fetched", "tokens", published)
}

// NewGMXAdapter tracks GMX protocol tokens (Arbitrum + Avalanche perps DEX).
func NewGMXAdapter(b *bus.Bus, pollInterval time.Duration) *DefiProtocolAdapter {
	return newDefiProtocolAdapter(b, "gmx", pollInterval, []defiToken{
		{Chain: "arbitrum", Address: "0xfc5A1A6EB076a2C7aD06eD22C90d7E710E35ad0a", Symbol: "GMX"},
		{Chain: "arbitrum", Address: "0x82aF49447D8a07e3bd95BD0d56f35241523fBab1", Symbol: "WETH"},
		{Chain: "arbitrum", Address: "0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f", Symbol: "WBTC"},
		{Chain: "arbitrum", Address: "0x912CE59144191C1204E64559FE8253a0e49E6548", Symbol: "ARB"},
	})
}

// NewDYDXAdapter tracks dYdX v4 token (Cosmos-based perpetuals).
func NewDYDXAdapter(b *bus.Bus, pollInterval time.Duration) *DefiProtocolAdapter {
	return newDefiProtocolAdapter(b, "dydx", pollInterval, []defiToken{
		{Chain: "ethereum", Address: "0x92D6C1e31e14520e676a687F0a93788B716BEff5", Symbol: "DYDX"},
	})
}

// NewDriftAdapter tracks Drift Protocol tokens (Solana perpetuals).
func NewDriftAdapter(b *bus.Bus, pollInterval time.Duration) *DefiProtocolAdapter {
	return newDefiProtocolAdapter(b, "drift", pollInterval, []defiToken{
		{Chain: "solana", Address: "DriFtupJYLTosbwoN8koMbEYSx54aFAVLddWsbksjwg7", Symbol: "DRIFT"},
	})
}

// NewSerumAdapter tracks Serum/OpenBook tokens (Solana orderbook DEX).
func NewSerumAdapter(b *bus.Bus, pollInterval time.Duration) *DefiProtocolAdapter {
	return newDefiProtocolAdapter(b, "serum", pollInterval, []defiToken{
		{Chain: "solana", Address: "SRMuApVNdxXokk5GT7XD5cUUgXMBCoAz2LHeuAoKWRt", Symbol: "SRM"},
		{Chain: "solana", Address: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", Symbol: "USDC"},
		{Chain: "solana", Address: "So11111111111111111111111111111111111111112", Symbol: "SOL"},
	})
}
