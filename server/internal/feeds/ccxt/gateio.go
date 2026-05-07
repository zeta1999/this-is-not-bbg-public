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

// GateioAdapter connects to Gate.io v4 spot WebSocket for market
// data. Gate.io uses underscore-separated symbols on the wire
// (BTC_USDT); we normalize to the compact form (BTCUSDT) on topics
// so the sanity checker treats them as the same pair across venues.
type GateioAdapter struct {
	bus       *bus.Bus
	symbols   []string // configured in compact form, e.g. "BTCUSDT"
	feedTypes []string
	wsURL     string

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

func NewGateioAdapter(b *bus.Bus, symbols, feedTypes []string, wsURL string) *GateioAdapter {
	if wsURL == "" {
		wsURL = "wss://api.gateio.ws/ws/v4/"
	}
	return &GateioAdapter{
		bus:       b,
		symbols:   symbols,
		feedTypes: feedTypes,
		wsURL:     wsURL,
		state:     "disconnected",
	}
}

func (a *GateioAdapter) Name() string { return "gateio" }

func (a *GateioAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "gateio",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *GateioAdapter) Start(ctx context.Context) error {
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
			slog.Warn("gateio connection lost, reconnecting", "error", err)
			time.Sleep(5 * time.Second)
		}
	}
}

// gateSymbol converts "BTCUSDT" → "BTC_USDT". Best effort — we walk
// the known-quote list (USDT, USDC, USD, BTC, ETH, DAI, BNB) and
// split on the first that matches.
func gateSymbol(compact string) string {
	for _, quote := range []string{"USDT", "USDC", "USD", "BTC", "ETH", "DAI", "BNB"} {
		if strings.HasSuffix(compact, quote) && len(compact) > len(quote) {
			return compact[:len(compact)-len(quote)] + "_" + quote
		}
	}
	return compact // fall through; subscription will likely fail but won't crash
}

func (a *GateioAdapter) connectAndStream(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, a.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial gateio: %w", err)
	}
	defer conn.Close()

	type subReq struct {
		Time    int64    `json:"time"`
		Channel string   `json:"channel"`
		Event   string   `json:"event"`
		Payload []string `json:"payload"`
	}

	now := time.Now().Unix()
	subCount := 0
	for _, compact := range a.symbols {
		wireSym := gateSymbol(compact)
		for _, ft := range a.feedTypes {
			var req subReq
			switch ft {
			case "ohlc":
				req = subReq{Time: now, Channel: "spot.candlesticks", Event: "subscribe", Payload: []string{"1m", wireSym}}
			case "trades":
				req = subReq{Time: now, Channel: "spot.trades", Event: "subscribe", Payload: []string{wireSym}}
			case "orderbook":
				// spot.order_book: channel, interval(1000ms), limit(20)
				req = subReq{Time: now, Channel: "spot.order_book", Event: "subscribe", Payload: []string{wireSym, "20", "1000ms"}}
			default:
				continue
			}
			body, _ := json.Marshal(req)
			if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}
			subCount++
		}
	}

	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()
	slog.Info("gateio connected", "subscriptions", subCount)

	// Gate.io's server expects a ping every 10s; we send app-level
	// ping frames. Use a quick channel-scoped goroutine tied to ctx.
	pingCtx, pingCancel := context.WithCancel(ctx)
	defer pingCancel()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				_ = conn.WriteMessage(websocket.PingMessage, nil)
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

		a.mu.Lock()
		a.lastUpdate = time.Now()
		a.bytesRecv += uint64(len(msg))
		a.mu.Unlock()

		a.processMessage(msg)
	}
}

type gateEnvelope struct {
	Time    int64           `json:"time"`
	Channel string          `json:"channel"`
	Event   string          `json:"event"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error,omitempty"`
}

func (a *GateioAdapter) processMessage(raw []byte) {
	var env gateEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return
	}
	// `subscribe` acknowledgement events carry no market payload.
	if env.Event == "subscribe" || env.Event == "unsubscribe" {
		return
	}
	if len(env.Error) > 0 && string(env.Error) != "null" {
		slog.Debug("gateio error", "channel", env.Channel, "error", string(env.Error))
		return
	}

	switch env.Channel {
	case "spot.candlesticks":
		a.handleCandle(env.Result)
	case "spot.trades":
		a.handleTrade(env.Result)
	case "spot.order_book":
		a.handleBook(env.Result)
	}
}

func (a *GateioAdapter) handleCandle(data json.RawMessage) {
	// Gate sends a single-object update: {t, v, c, h, l, o, n, a, w}
	// where n = "1m_BTC_USDT" identifies interval + wire symbol.
	var c struct {
		T json.Number `json:"t"`
		O json.Number `json:"o"`
		H json.Number `json:"h"`
		L json.Number `json:"l"`
		C json.Number `json:"c"`
		V json.Number `json:"v"` // base-asset volume (quote is "a")
		N string      `json:"n"` // "1m_BTC_USDT"
	}
	if json.Unmarshal(data, &c) != nil || c.N == "" {
		return
	}
	// N = "1m_BTC_USDT" → interval="1m", wireSym="BTC_USDT", compact="BTCUSDT"
	parts := strings.SplitN(c.N, "_", 2)
	if len(parts) != 2 {
		return
	}
	tf := parts[0]
	compact := strings.ReplaceAll(parts[1], "_", "")

	ts, _ := c.T.Int64()
	o, _ := c.O.Float64()
	h, _ := c.H.Float64()
	l, _ := c.L.Float64()
	close_, _ := c.C.Float64()
	vol, _ := c.V.Float64()

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("ohlc.gateio.%s", compact),
		Payload: feeds.OHLC{
			Instrument: compact, Exchange: "gateio", Timeframe: tf,
			Timestamp: time.Unix(ts, 0),
			Open:      o, High: h, Low: l, Close: close_, Volume: vol,
		},
	})
}

func (a *GateioAdapter) handleTrade(data json.RawMessage) {
	var t struct {
		ID           int64       `json:"id"`
		CreateTimeMS json.Number `json:"create_time_ms"` // string-encoded ms
		CurrencyPair string      `json:"currency_pair"`  // "BTC_USDT"
		Side         string      `json:"side"`
		Price        json.Number `json:"price"`
		Amount       json.Number `json:"amount"`
	}
	if json.Unmarshal(data, &t) != nil || t.CurrencyPair == "" {
		return
	}
	compact := strings.ReplaceAll(t.CurrencyPair, "_", "")
	px, _ := t.Price.Float64()
	qty, _ := t.Amount.Float64()
	ms, _ := t.CreateTimeMS.Float64()

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("trade.gateio.%s", compact),
		Payload: feeds.Trade{
			Instrument: compact, Exchange: "gateio",
			Timestamp: time.UnixMilli(int64(ms)),
			Price:     px, Quantity: qty, Side: strings.ToLower(t.Side),
			TradeID: strconv.FormatInt(t.ID, 10),
		},
	})
}

func (a *GateioAdapter) handleBook(data json.RawMessage) {
	var b struct {
		S    string     `json:"s"` // wire symbol, e.g. "BTC_USDT"
		Bids [][]string `json:"bids"`
		Asks [][]string `json:"asks"`
	}
	if json.Unmarshal(data, &b) != nil || b.S == "" {
		return
	}
	compact := strings.ReplaceAll(b.S, "_", "")

	bids := make([]feeds.LOBLevel, 0, len(b.Bids))
	for _, row := range b.Bids {
		if len(row) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(row[0], 64)
		q, _ := strconv.ParseFloat(row[1], 64)
		bids = append(bids, feeds.LOBLevel{Price: p, Quantity: q})
	}
	asks := make([]feeds.LOBLevel, 0, len(b.Asks))
	for _, row := range b.Asks {
		if len(row) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(row[0], 64)
		q, _ := strconv.ParseFloat(row[1], 64)
		asks = append(asks, feeds.LOBLevel{Price: p, Quantity: q})
	}

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("lob.gateio.%s", compact),
		Payload: feeds.LOBSnapshot{
			Instrument: compact, Exchange: "gateio",
			Timestamp: time.Now(), Bids: bids, Asks: asks,
		},
	})
}
