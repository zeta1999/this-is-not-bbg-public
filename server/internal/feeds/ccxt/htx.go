package ccxt

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// HTXAdapter connects to HTX (formerly Huobi) spot WebSocket. HTX
// ships everything gzip-compressed and runs a {"ping":N} / {"pong":N}
// keepalive at the application layer, so this adapter carries more
// plumbing than the other CCXT-style feeds.
//
// Symbol wire format is lowercase (`btcusdt`); we translate to and
// from the compact uppercase form (`BTCUSDT`) used on the rest of
// the bus so sanity + consistency checkers see the same instrument
// identifier across venues.
type HTXAdapter struct {
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

func NewHTXAdapter(b *bus.Bus, symbols, feedTypes []string, wsURL string) *HTXAdapter {
	if wsURL == "" {
		wsURL = "wss://api.huobi.pro/ws"
	}
	return &HTXAdapter{
		bus:       b,
		symbols:   symbols,
		feedTypes: feedTypes,
		wsURL:     wsURL,
		state:     "disconnected",
	}
}

func (a *HTXAdapter) Name() string { return "htx" }

func (a *HTXAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "htx",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *HTXAdapter) Start(ctx context.Context) error {
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
			slog.Warn("htx connection lost, reconnecting", "error", err)
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *HTXAdapter) connectAndStream(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, a.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial htx: %w", err)
	}
	defer conn.Close()

	subCount := 0
	for _, compact := range a.symbols {
		wire := strings.ToLower(compact)
		for _, ft := range a.feedTypes {
			var topic string
			switch ft {
			case "ohlc":
				topic = fmt.Sprintf("market.%s.kline.1min", wire)
			case "trades":
				topic = fmt.Sprintf("market.%s.trade.detail", wire)
			case "orderbook":
				// step0 = full precision (all quoted levels, typically ~150)
				topic = fmt.Sprintf("market.%s.depth.step0", wire)
			default:
				continue
			}
			req := map[string]any{"sub": topic, "id": fmt.Sprintf("id-%d", subCount)}
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
	slog.Info("htx connected", "subscriptions", subCount)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		// HTX compresses every payload with gzip.
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			// Not gzipped — ignore. Normal WS control frames land here
			// from some proxies.
			continue
		}
		msg, err := io.ReadAll(gr)
		_ = gr.Close()
		if err != nil {
			continue
		}

		a.mu.Lock()
		a.lastUpdate = time.Now()
		a.bytesRecv += uint64(len(msg))
		a.mu.Unlock()

		// Keepalive: {"ping": N} → {"pong": N}. Reply immediately;
		// the server expects a ~5s round-trip or it disconnects.
		if pingVal, ok := parsePing(msg); ok {
			pong, _ := json.Marshal(map[string]int64{"pong": pingVal})
			if err := conn.WriteMessage(websocket.TextMessage, pong); err != nil {
				return fmt.Errorf("pong write: %w", err)
			}
			continue
		}

		a.processMessage(msg)
	}
}

func parsePing(raw []byte) (int64, bool) {
	var p struct {
		Ping int64 `json:"ping"`
	}
	if json.Unmarshal(raw, &p) == nil && p.Ping != 0 {
		return p.Ping, true
	}
	return 0, false
}

type htxEnvelope struct {
	Ch     string          `json:"ch"`     // e.g. "market.btcusdt.kline.1min"
	TS     int64           `json:"ts"`     // ms
	Tick   json.RawMessage `json:"tick"`   // push payload
	Status string          `json:"status"` // "ok" on sub acks
}

func (a *HTXAdapter) processMessage(raw []byte) {
	var env htxEnvelope
	if json.Unmarshal(raw, &env) != nil || env.Ch == "" {
		return
	}
	// Subscription acks have no `tick`; skip them.
	if len(env.Tick) == 0 {
		return
	}

	// Channel: "market.<symbol>.<type>[.<subtype>]"
	parts := strings.Split(env.Ch, ".")
	if len(parts) < 3 {
		return
	}
	compact := strings.ToUpper(parts[1])
	kind := parts[2]

	switch kind {
	case "kline":
		a.handleKline(compact, env.Tick)
	case "trade":
		// Subtype "detail" etc.
		a.handleTrade(compact, env.Tick)
	case "depth":
		a.handleDepth(compact, env.Tick, env.TS)
	}
}

func (a *HTXAdapter) handleKline(compact string, tick json.RawMessage) {
	var k struct {
		ID     int64       `json:"id"`     // open time in seconds
		Open   json.Number `json:"open"`
		High   json.Number `json:"high"`
		Low    json.Number `json:"low"`
		Close  json.Number `json:"close"`
		Amount json.Number `json:"amount"` // base-asset volume
	}
	if json.Unmarshal(tick, &k) != nil {
		return
	}
	o, _ := k.Open.Float64()
	h, _ := k.High.Float64()
	l, _ := k.Low.Float64()
	c, _ := k.Close.Float64()
	vol, _ := k.Amount.Float64()

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("ohlc.htx.%s", compact),
		Payload: feeds.OHLC{
			Instrument: compact, Exchange: "htx", Timeframe: "1m",
			Timestamp: time.Unix(k.ID, 0),
			Open:      o, High: h, Low: l, Close: c, Volume: vol,
		},
	})
}

func (a *HTXAdapter) handleTrade(compact string, tick json.RawMessage) {
	var t struct {
		Data []struct {
			Ts        int64       `json:"ts"`
			TradeID   int64       `json:"tradeId"`
			Amount    json.Number `json:"amount"`
			Price     json.Number `json:"price"`
			Direction string      `json:"direction"`
		} `json:"data"`
	}
	if json.Unmarshal(tick, &t) != nil {
		return
	}
	for _, row := range t.Data {
		px, _ := row.Price.Float64()
		qty, _ := row.Amount.Float64()
		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("trade.htx.%s", compact),
			Payload: feeds.Trade{
				Instrument: compact, Exchange: "htx",
				Timestamp: time.UnixMilli(row.Ts),
				Price:     px, Quantity: qty, Side: strings.ToLower(row.Direction),
				TradeID: fmt.Sprintf("%d", row.TradeID),
			},
		})
	}
}

func (a *HTXAdapter) handleDepth(compact string, tick json.RawMessage, envTS int64) {
	var d struct {
		Bids [][]float64 `json:"bids"`
		Asks [][]float64 `json:"asks"`
	}
	if json.Unmarshal(tick, &d) != nil {
		return
	}
	bids := make([]feeds.LOBLevel, 0, len(d.Bids))
	for _, row := range d.Bids {
		if len(row) < 2 {
			continue
		}
		bids = append(bids, feeds.LOBLevel{Price: row[0], Quantity: row[1]})
	}
	asks := make([]feeds.LOBLevel, 0, len(d.Asks))
	for _, row := range d.Asks {
		if len(row) < 2 {
			continue
		}
		asks = append(asks, feeds.LOBLevel{Price: row[0], Quantity: row[1]})
	}
	ts := time.Now()
	if envTS > 0 {
		ts = time.UnixMilli(envTS)
	}
	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("lob.htx.%s", compact),
		Payload: feeds.LOBSnapshot{
			Instrument: compact, Exchange: "htx",
			Timestamp: ts, Bids: bids, Asks: asks,
		},
	})
}
