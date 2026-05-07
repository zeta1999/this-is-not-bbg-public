package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// HyperliquidAdapter connects to Hyperliquid L1 perpetuals via WebSocket.
// Hyperliquid is a custom L1 using HyperBFT consensus, fully on-chain order book.
type HyperliquidAdapter struct {
	bus          *bus.Bus
	pollInterval time.Duration
	symbols      []string

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

func NewHyperliquidAdapter(b *bus.Bus, pollInterval time.Duration) *HyperliquidAdapter {
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	return &HyperliquidAdapter{
		bus:          b,
		pollInterval: pollInterval,
		symbols:      []string{"BTC", "ETH", "SOL", "ARB", "DOGE", "AVAX", "LINK"},
		state:        "disconnected",
	}
}

func (a *HyperliquidAdapter) Name() string { return "hyperliquid" }

func (a *HyperliquidAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "hyperliquid",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *HyperliquidAdapter) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		err := a.connectAndStream(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			a.mu.Lock()
			a.errorCount++
			a.state = "reconnecting"
			a.mu.Unlock()
			slog.Warn("hyperliquid connection lost, reconnecting", "error", err)
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *HyperliquidAdapter) connectAndStream(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, "wss://api.hyperliquid.xyz/ws", nil)
	if err != nil {
		return fmt.Errorf("dial hyperliquid: %w", err)
	}
	defer conn.Close()

	// Subscribe to trades, L2 book, and per-asset context for each
	// symbol. `activeAssetCtx` delivers mark / oracle / funding /
	// open interest, which feeds the PerpetualSnapshot payload.
	for _, sym := range a.symbols {
		sub, _ := json.Marshal(map[string]any{
			"method": "subscribe",
			"subscription": map[string]any{"type": "trades", "coin": sym},
		})
		_ = conn.WriteMessage(websocket.TextMessage, sub)

		sub3, _ := json.Marshal(map[string]any{
			"method":       "subscribe",
			"subscription": map[string]any{"type": "activeAssetCtx", "coin": sym},
		})
		_ = conn.WriteMessage(websocket.TextMessage, sub3)

		sub4, _ := json.Marshal(map[string]any{
			"method":       "subscribe",
			"subscription": map[string]any{"type": "liquidations", "coin": sym},
		})
		_ = conn.WriteMessage(websocket.TextMessage, sub4)

		sub2, _ := json.Marshal(map[string]any{
			"method": "subscribe",
			"subscription": map[string]any{"type": "l2Book", "coin": sym},
		})
		_ = conn.WriteMessage(websocket.TextMessage, sub2)
	}

	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()
	slog.Info("hyperliquid connected", "symbols", len(a.symbols))

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

func (a *HyperliquidAdapter) processMessage(raw []byte) {
	var msg struct {
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return
	}

	switch msg.Channel {
	case "trades":
		a.handleTrades(msg.Data)
	case "l2Book":
		a.handleBook(msg.Data)
	case "activeAssetCtx":
		a.handleAssetCtx(msg.Data)
	case "liquidations":
		a.handleLiquidations(msg.Data)
	}
}

// handleLiquidations maps Hyperliquid liquidation frames to the
// canonical LiquidationEvent stream. HL ships an array per frame;
// one bus message per row so downstream subscribers can filter by
// topic without re-splitting.
func (a *HyperliquidAdapter) handleLiquidations(data json.RawMessage) {
	var liqs []struct {
		Coin string      `json:"coin"`
		Px   json.Number `json:"px"`
		Sz   json.Number `json:"sz"`
		Side string      `json:"side"` // "B" or "A" — taker side
		Time int64       `json:"time"`
	}
	if json.Unmarshal(data, &liqs) != nil {
		return
	}
	for _, l := range liqs {
		price, _ := l.Px.Float64()
		qty, _ := l.Sz.Float64()
		symbol := l.Coin + "USD"
		side := "buy"
		if strings.EqualFold(l.Side, "A") || strings.EqualFold(l.Side, "sell") {
			side = "sell"
		}
		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("liquidation.hyperliquid.%s", symbol),
			Payload: feeds.LiquidationEvent{
				Instrument: symbol,
				Exchange:   "hyperliquid",
				Timestamp:  time.UnixMilli(l.Time),
				Side:       side,
				Price:      price,
				Quantity:   qty,
				Notional:   price * qty,
			},
		})
	}
}

// handleAssetCtx maps Hyperliquid's activeAssetCtx payload to a
// feeds.PerpetualSnapshot on `perp.hyperliquid.<symbol>`. The
// `ctx` object carries mark price, oracle (index) price, funding,
// and open interest; we populate only what the venue exposes.
func (a *HyperliquidAdapter) handleAssetCtx(data json.RawMessage) {
	var msg struct {
		Coin string `json:"coin"`
		Ctx  struct {
			MarkPx       json.Number `json:"markPx"`
			OraclePx     json.Number `json:"oraclePx"`
			Funding      json.Number `json:"funding"`
			OpenInterest json.Number `json:"openInterest"`
		} `json:"ctx"`
	}
	if json.Unmarshal(data, &msg) != nil || msg.Coin == "" {
		return
	}
	mark, _ := msg.Ctx.MarkPx.Float64()
	oracle, _ := msg.Ctx.OraclePx.Float64()
	fr, _ := msg.Ctx.Funding.Float64()
	oi, _ := msg.Ctx.OpenInterest.Float64()
	symbol := msg.Coin + "USD"
	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("perp.hyperliquid.%s", symbol),
		Payload: feeds.PerpetualSnapshot{
			Instrument:       symbol,
			Exchange:         "hyperliquid",
			Timestamp:        time.Now(),
			MarkPrice:        mark,
			IndexPrice:       oracle,
			FundingRate:      fr,
			OpenInterestBase: oi,
		},
	})
}

func (a *HyperliquidAdapter) handleTrades(data json.RawMessage) {
	var trades []struct {
		Coin  string      `json:"coin"`
		Px    json.Number `json:"px"`
		Sz    json.Number `json:"sz"`
		Side  string      `json:"side"`
		Time  int64       `json:"time"`
		Hash  string      `json:"hash"`
	}
	if json.Unmarshal(data, &trades) != nil {
		return
	}

	for _, t := range trades {
		price, _ := t.Px.Float64()
		qty, _ := t.Sz.Float64()
		symbol := t.Coin + "USD"

		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("trade.hyperliquid.%s", symbol),
			Payload: feeds.Trade{
				Instrument: symbol, Exchange: "hyperliquid",
				Timestamp: time.UnixMilli(t.Time),
				Price: price, Quantity: qty, Side: strings.ToLower(t.Side),
				TradeID: t.Hash,
			},
		})

		// Also publish as OHLC (spot price point).
		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("ohlc.hyperliquid.%s", symbol),
			Payload: feeds.OHLC{
				Instrument: symbol, Exchange: "hyperliquid", Timeframe: "spot",
				Timestamp: time.UnixMilli(t.Time),
				Open: price, High: price, Low: price, Close: price, Volume: qty,
			},
		})
	}
}

func (a *HyperliquidAdapter) handleBook(data json.RawMessage) {
	var book struct {
		Coin   string `json:"coin"`
		Levels [2][]struct {
			Px json.Number `json:"px"`
			Sz json.Number `json:"sz"`
			N  int         `json:"n"`
		} `json:"levels"`
	}
	if json.Unmarshal(data, &book) != nil {
		return
	}

	symbol := book.Coin + "USD"

	bids := make([]feeds.LOBLevel, 0, len(book.Levels[0]))
	for _, l := range book.Levels[0] {
		p, _ := l.Px.Float64()
		q, _ := l.Sz.Float64()
		bids = append(bids, feeds.LOBLevel{Price: p, Quantity: q})
	}

	asks := make([]feeds.LOBLevel, 0, len(book.Levels[1]))
	for _, l := range book.Levels[1] {
		p, _ := l.Px.Float64()
		q, _ := l.Sz.Float64()
		asks = append(asks, feeds.LOBLevel{Price: p, Quantity: q})
	}

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("lob.hyperliquid.%s", symbol),
		Payload: feeds.LOBSnapshot{
			Instrument: symbol, Exchange: "hyperliquid",
			Timestamp: time.Now(), Bids: bids, Asks: asks,
		},
	})
}
