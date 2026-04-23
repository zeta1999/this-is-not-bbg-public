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

// BitgetAdapter connects to Bitget V2 WebSocket for market data.
type BitgetAdapter struct {
	bus       *bus.Bus
	symbols   []string
	feedTypes []string
	wsURL     string

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

func NewBitgetAdapter(b *bus.Bus, symbols, feedTypes []string, wsURL string) *BitgetAdapter {
	if wsURL == "" {
		wsURL = "wss://ws.bitget.com/v2/ws/public"
	}
	return &BitgetAdapter{
		bus:       b,
		symbols:   symbols,
		feedTypes: feedTypes,
		wsURL:     wsURL,
		state:     "disconnected",
	}
}

func (a *BitgetAdapter) Name() string { return "bitget" }

func (a *BitgetAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "bitget",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *BitgetAdapter) Start(ctx context.Context) error {
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
			slog.Warn("bitget connection lost, reconnecting", "error", err)
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *BitgetAdapter) connectAndStream(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, a.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial bitget: %w", err)
	}
	defer conn.Close()

	// Subscribe to channels.
	var args []map[string]string
	for _, sym := range a.symbols {
		for _, ft := range a.feedTypes {
			switch ft {
			case "ohlc":
				args = append(args, map[string]string{
					"instType": "SPOT", "channel": "candle1m", "instId": sym,
				})
			case "trades":
				args = append(args, map[string]string{
					"instType": "SPOT", "channel": "trade", "instId": sym,
				})
			case "orderbook":
				args = append(args, map[string]string{
					"instType": "SPOT", "channel": "books15", "instId": sym,
				})
			}
		}
	}

	subMsg, _ := json.Marshal(map[string]any{"op": "subscribe", "args": args})
	if err := conn.WriteMessage(websocket.TextMessage, subMsg); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()
	slog.Info("bitget connected", "channels", len(args))

	// Ping every 30s.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = conn.WriteMessage(websocket.TextMessage, []byte("ping"))
			}
		}
	}()

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

		// Bitget sends "pong" as text.
		if string(msg) == "pong" {
			continue
		}

		a.mu.Lock()
		a.lastUpdate = time.Now()
		a.bytesRecv += uint64(len(msg))
		a.mu.Unlock()

		a.processMessage(msg)
	}
}

type bitgetMsg struct {
	Action string `json:"action"` // "snapshot" or "update"
	Arg    struct {
		Channel  string `json:"channel"`
		InstType string `json:"instType"`
		InstID   string `json:"instId"`
	} `json:"arg"`
	Data []json.RawMessage `json:"data"`
}

func (a *BitgetAdapter) processMessage(raw []byte) {
	var msg bitgetMsg
	if json.Unmarshal(raw, &msg) != nil || len(msg.Data) == 0 {
		return
	}

	symbol := strings.ReplaceAll(msg.Arg.InstID, "-", "")
	channel := msg.Arg.Channel

	switch {
	case strings.HasPrefix(channel, "candle"):
		for _, d := range msg.Data {
			a.handleCandle(symbol, d)
		}
	case channel == "trade":
		for _, d := range msg.Data {
			a.handleTrade(symbol, d)
		}
	case strings.HasPrefix(channel, "books"):
		for _, d := range msg.Data {
			a.handleBook(symbol, d)
		}
	}
}

func (a *BitgetAdapter) handleCandle(symbol string, data json.RawMessage) {
	var candle []json.Number // [ts, o, h, l, c, baseVol, quoteVol]
	if json.Unmarshal(data, &candle) != nil || len(candle) < 6 {
		return
	}
	ts, _ := candle[0].Int64()
	open, _ := candle[1].Float64()
	high, _ := candle[2].Float64()
	low, _ := candle[3].Float64()
	close_, _ := candle[4].Float64()
	vol, _ := candle[5].Float64()

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("ohlc.bitget.%s", symbol),
		Payload: feeds.OHLC{
			Instrument: symbol, Exchange: "bitget", Timeframe: "1m",
			Timestamp: time.UnixMilli(ts),
			Open: open, High: high, Low: low, Close: close_, Volume: vol,
		},
	})
}

func (a *BitgetAdapter) handleTrade(symbol string, data json.RawMessage) {
	var t struct {
		Price   json.Number `json:"price"`
		Size    json.Number `json:"size"`
		Side    string      `json:"side"`
		Ts      json.Number `json:"ts"`
		TradeID string      `json:"tradeId"`
	}
	if json.Unmarshal(data, &t) != nil {
		return
	}
	price, _ := t.Price.Float64()
	qty, _ := t.Size.Float64()
	ts, _ := t.Ts.Int64()

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("trade.bitget.%s", symbol),
		Payload: feeds.Trade{
			Instrument: symbol, Exchange: "bitget",
			Timestamp: time.UnixMilli(ts),
			Price: price, Quantity: qty, Side: strings.ToLower(t.Side),
			TradeID: t.TradeID,
		},
	})
}

func (a *BitgetAdapter) handleBook(symbol string, data json.RawMessage) {
	var book struct {
		Bids [][]string `json:"bids"` // [price, qty]
		Asks [][]string `json:"asks"`
	}
	if json.Unmarshal(data, &book) != nil {
		return
	}

	bids := make([]feeds.LOBLevel, 0, len(book.Bids))
	for _, b := range book.Bids {
		if len(b) < 2 { continue }
		p, _ := strconv.ParseFloat(b[0], 64)
		q, _ := strconv.ParseFloat(b[1], 64)
		bids = append(bids, feeds.LOBLevel{Price: p, Quantity: q})
	}
	asks := make([]feeds.LOBLevel, 0, len(book.Asks))
	for _, a_ := range book.Asks {
		if len(a_) < 2 { continue }
		p, _ := strconv.ParseFloat(a_[0], 64)
		q, _ := strconv.ParseFloat(a_[1], 64)
		asks = append(asks, feeds.LOBLevel{Price: p, Quantity: q})
	}

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("lob.bitget.%s", symbol),
		Payload: feeds.LOBSnapshot{
			Instrument: symbol, Exchange: "bitget",
			Timestamp: time.Now(), Bids: bids, Asks: asks,
		},
	})
}
