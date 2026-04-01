package ccxt

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

// KrakenAdapter connects to the Kraken WebSocket v2 API.
type KrakenAdapter struct {
	bus       *bus.Bus
	symbols   []string
	feedTypes []string
	wsURL     string
	restBase  string
	rateLimit int

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

func NewKrakenAdapter(b *bus.Bus, symbols, feedTypes []string, wsURL, restBase string, rateLimit int) *KrakenAdapter {
	if wsURL == "" {
		wsURL = "wss://ws.kraken.com/v2"
	}
	if restBase == "" {
		restBase = "https://api.kraken.com"
	}
	return &KrakenAdapter{
		bus:       b,
		symbols:   symbols,
		feedTypes: feedTypes,
		wsURL:     wsURL,
		restBase:  restBase,
		rateLimit: rateLimit,
		state:     "disconnected",
	}
}

func (a *KrakenAdapter) Name() string { return "kraken" }

func (a *KrakenAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "kraken",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *KrakenAdapter) Start(ctx context.Context) error {
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
			slog.Warn("kraken connection lost, reconnecting", "error", err)
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *KrakenAdapter) connectAndStream(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, a.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial kraken: %w", err)
	}
	defer conn.Close()

	// Subscribe to channels (Kraken v2 format).
	for _, ft := range a.feedTypes {
		var channel string
		switch ft {
		case "trades":
			channel = "trade"
		case "ohlc":
			channel = "ohlc"
		case "orderbook":
			channel = "book"
		default:
			continue
		}

		sub := map[string]any{
			"method": "subscribe",
			"params": map[string]any{
				"channel": channel,
				"symbol":  a.symbols,
			},
		}
		if channel == "ohlc" {
			sub["params"].(map[string]any)["interval"] = 1 // 1 minute
		}
		if channel == "book" {
			sub["params"].(map[string]any)["depth"] = 25
		}

		if err := conn.WriteJSON(sub); err != nil {
			return fmt.Errorf("subscribe %s: %w", channel, err)
		}
	}

	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()
	slog.Info("kraken connected", "symbols", a.symbols)

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

		a.processMessage(msg)

		a.mu.Lock()
		a.lastUpdate = time.Now()
		a.bytesRecv += uint64(len(msg))
		a.mu.Unlock()
	}
}

type krakenV2Msg struct {
	Channel string          `json:"channel"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
}

type krakenTrade struct {
	Symbol  string  `json:"symbol"`
	Price   float64 `json:"price"`
	Qty     float64 `json:"qty"`
	Side    string  `json:"side"`
	OrdType string  `json:"ord_type"`
	Time    string  `json:"timestamp"`
	TradeID int64   `json:"trade_id"`
}

type krakenOHLC struct {
	Symbol    string  `json:"symbol"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	Timestamp string  `json:"timestamp"`
	Interval  int     `json:"interval"`
}

type krakenBookEntry struct {
	Price float64 `json:"price"`
	Qty   float64 `json:"qty"`
}

type krakenBook struct {
	Symbol string            `json:"symbol"`
	Bids   []krakenBookEntry `json:"bids"`
	Asks   []krakenBookEntry `json:"asks"`
}

func (a *KrakenAdapter) processMessage(raw []byte) {
	var msg krakenV2Msg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	// Skip heartbeats and subscription confirmations.
	if msg.Channel == "heartbeat" || msg.Type == "subscribe" {
		return
	}

	switch msg.Channel {
	case "trade":
		var trades []krakenTrade
		json.Unmarshal(msg.Data, &trades)
		for _, t := range trades {
			sym := normalizeKrakenSymbol(t.Symbol)
			ts, _ := time.Parse(time.RFC3339Nano, t.Time)

			a.bus.Publish(bus.Message{
				Topic: fmt.Sprintf("trade.kraken.%s", sym),
				Payload: feeds.Trade{
					Instrument: t.Symbol,
					Exchange:   "kraken",
					Timestamp:  ts,
					Price:      t.Price,
					Quantity:   t.Qty,
					Side:       strings.ToLower(t.Side),
					TradeID:    strconv.FormatInt(t.TradeID, 10),
				},
			})
		}

	case "ohlc":
		var candles []krakenOHLC
		json.Unmarshal(msg.Data, &candles)
		for _, c := range candles {
			sym := normalizeKrakenSymbol(c.Symbol)
			ts, _ := time.Parse(time.RFC3339Nano, c.Timestamp)

			a.bus.Publish(bus.Message{
				Topic: fmt.Sprintf("ohlc.kraken.%s", sym),
				Payload: feeds.OHLC{
					Instrument: c.Symbol,
					Exchange:   "kraken",
					Timeframe:  fmt.Sprintf("%dm", c.Interval),
					Timestamp:  ts,
					Open:       c.Open,
					High:       c.High,
					Low:        c.Low,
					Close:      c.Close,
					Volume:     c.Volume,
				},
			})
		}

	case "book":
		var books []krakenBook
		json.Unmarshal(msg.Data, &books)
		for _, b := range books {
			sym := normalizeKrakenSymbol(b.Symbol)

			bids := make([]feeds.LOBLevel, len(b.Bids))
			for i, bid := range b.Bids {
				bids[i] = feeds.LOBLevel{Price: bid.Price, Quantity: bid.Qty}
			}
			asks := make([]feeds.LOBLevel, len(b.Asks))
			for i, ask := range b.Asks {
				asks[i] = feeds.LOBLevel{Price: ask.Price, Quantity: ask.Qty}
			}

			a.bus.Publish(bus.Message{
				Topic: fmt.Sprintf("lob.kraken.%s", sym),
				Payload: feeds.LOBSnapshot{
					Instrument: b.Symbol,
					Exchange:   "kraken",
					Timestamp:  time.Now(),
					Bids:       bids,
					Asks:       asks,
				},
			})
		}
	}
}

// normalizeKrakenSymbol converts "XBT/USD" to "XBTUSD" for topic names.
func normalizeKrakenSymbol(sym string) string {
	return strings.ReplaceAll(sym, "/", "")
}
