package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// DYDXV4Adapter streams dYdX v4 perpetuals from the indexer WS.
//
// Channels wired today:
//   - v4_trades.<ticker>  → feeds.Trade on trade.dydx.<ticker>
//   - v4_markets          → feeds.PerpetualSnapshot on
//                           perp.dydx.<ticker> (mark / index /
//                           funding / open interest).
//
// LOB (v4_orderbook) + liquidations are follow-up work; this
// adapter is safe to enable without them because the canonical
// topics it publishes are already consumed by MON / sanity /
// datalake.
type DYDXV4Adapter struct {
	bus          *bus.Bus
	pollInterval time.Duration
	tickers      []string
	wsURL        string

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

// NewDYDXV4Adapter constructs a dYdX v4 indexer-WS adapter. Leave
// `wsURL` empty for the public indexer. Tickers follow the dYdX
// convention (`BTC-USD`, `ETH-USD`, `SOL-USD`, …).
func NewDYDXV4Adapter(b *bus.Bus, pollInterval time.Duration, tickers []string, wsURL string) *DYDXV4Adapter {
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	if wsURL == "" {
		wsURL = "wss://indexer.dydx.trade/v4/ws"
	}
	if len(tickers) == 0 {
		tickers = []string{"BTC-USD", "ETH-USD", "SOL-USD", "AVAX-USD", "LINK-USD", "DOGE-USD"}
	}
	return &DYDXV4Adapter{
		bus:          b,
		pollInterval: pollInterval,
		tickers:      tickers,
		wsURL:        wsURL,
		state:        "disconnected",
	}
}

func (a *DYDXV4Adapter) Name() string { return "dydx-v4" }

func (a *DYDXV4Adapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "dydx-v4",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *DYDXV4Adapter) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if err := a.connectAndStream(ctx); err != nil {
			a.mu.Lock()
			a.state = "reconnecting"
			a.errorCount++
			a.mu.Unlock()
			slog.Warn("dydx-v4 reconnect", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(a.pollInterval):
			}
		}
	}
}

func (a *DYDXV4Adapter) connectAndStream(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, a.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial dydx-v4: %w", err)
	}
	defer conn.Close()

	// One v4_markets subscription covers every ticker.
	mk, _ := json.Marshal(map[string]any{"type": "subscribe", "channel": "v4_markets"})
	if err := conn.WriteMessage(websocket.TextMessage, mk); err != nil {
		return fmt.Errorf("subscribe markets: %w", err)
	}

	// Per-ticker trade subscriptions.
	for _, t := range a.tickers {
		sub, _ := json.Marshal(map[string]any{
			"type": "subscribe", "channel": "v4_trades", "id": t,
		})
		if err := conn.WriteMessage(websocket.TextMessage, sub); err != nil {
			return fmt.Errorf("subscribe trades %s: %w", t, err)
		}
	}

	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()
	slog.Info("dydx-v4 connected", "tickers", len(a.tickers))

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		a.mu.Lock()
		a.lastUpdate = time.Now()
		a.bytesRecv += uint64(len(msg))
		a.mu.Unlock()
		a.processMessage(msg)
	}
}

type dydxEnvelope struct {
	Type     string          `json:"type"`
	Channel  string          `json:"channel"`
	ID       string          `json:"id,omitempty"`
	Contents json.RawMessage `json:"contents,omitempty"`
}

func (a *DYDXV4Adapter) processMessage(raw []byte) {
	var env dydxEnvelope
	if json.Unmarshal(raw, &env) != nil {
		return
	}
	switch env.Channel {
	case "v4_trades":
		a.handleTrades(env.ID, env.Contents)
	case "v4_markets":
		a.handleMarkets(env.Contents)
	}
}

// handleTrades decodes the v4_trades contents payload and emits
// feeds.Trade per trade. dYdX ships snapshots + updates; the shape
// of `trades` is the same either way.
func (a *DYDXV4Adapter) handleTrades(ticker string, data json.RawMessage) {
	if ticker == "" {
		return
	}
	var payload struct {
		Trades []struct {
			ID        string      `json:"id"`
			Side      string      `json:"side"` // "BUY"|"SELL"
			Size      json.Number `json:"size"`
			Price     json.Number `json:"price"`
			CreatedAt string      `json:"createdAt"`
		} `json:"trades"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return
	}
	for _, t := range payload.Trades {
		price, _ := t.Price.Float64()
		qty, _ := t.Size.Float64()
		ts, _ := time.Parse(time.RFC3339Nano, t.CreatedAt)
		if ts.IsZero() {
			ts = time.Now()
		}
		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("trade.dydx.%s", ticker),
			Payload: feeds.Trade{
				Instrument: ticker,
				Exchange:   "dydx",
				Timestamp:  ts,
				Price:      price,
				Quantity:   qty,
				Side:       strings.ToLower(t.Side),
				TradeID:    t.ID,
			},
		})
	}
}

// handleMarkets decodes v4_markets contents and emits one
// PerpetualSnapshot per (ticker, snapshot). dYdX publishes both
// initial snapshots (keyed by `markets`) and incremental updates
// (keyed by `trading`); this handler accepts either shape.
func (a *DYDXV4Adapter) handleMarkets(data json.RawMessage) {
	// Snapshot form: { "markets": { "BTC-USD": {...} } }
	var snap struct {
		Markets map[string]dydxMarket `json:"markets"`
	}
	if json.Unmarshal(data, &snap) == nil && len(snap.Markets) > 0 {
		for ticker, m := range snap.Markets {
			a.publishMarket(ticker, m)
		}
		return
	}
	// Update form: { "trading": { "BTC-USD": {...} } }.
	var upd struct {
		Trading map[string]dydxMarket `json:"trading"`
	}
	if json.Unmarshal(data, &upd) == nil {
		for ticker, m := range upd.Trading {
			a.publishMarket(ticker, m)
		}
	}
}

type dydxMarket struct {
	OraclePrice      string `json:"oraclePrice,omitempty"`
	NextFundingRate  string `json:"nextFundingRate,omitempty"`
	OpenInterest     string `json:"openInterest,omitempty"`
	PriceChange24h   string `json:"priceChange24H,omitempty"`
	TrailingFunding  string `json:"trailingFundingRate,omitempty"`
}

func (a *DYDXV4Adapter) publishMarket(ticker string, m dydxMarket) {
	if ticker == "" {
		return
	}
	parseF := func(s string) float64 {
		if s == "" {
			return 0
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	p := feeds.PerpetualSnapshot{
		Instrument:       ticker,
		Exchange:         "dydx",
		Timestamp:        time.Now(),
		IndexPrice:       parseF(m.OraclePrice),
		FundingRate:      parseF(m.TrailingFunding),
		NextFundingRate:  parseF(m.NextFundingRate),
		OpenInterestBase: parseF(m.OpenInterest),
	}
	// Skip silent all-zero updates (common on disconnect churn).
	if p.IndexPrice == 0 && p.FundingRate == 0 && p.OpenInterestBase == 0 && p.NextFundingRate == 0 {
		return
	}
	a.bus.Publish(bus.Message{
		Topic:   fmt.Sprintf("perp.dydx.%s", ticker),
		Payload: p,
	})
}
