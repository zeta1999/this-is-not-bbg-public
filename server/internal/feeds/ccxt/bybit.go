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

// BybitAdapter connects to Bybit V5 WebSocket for market data (spot + perps).
type BybitAdapter struct {
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

func NewBybitAdapter(b *bus.Bus, symbols, feedTypes []string, wsURL string) *BybitAdapter {
	if wsURL == "" {
		wsURL = "wss://stream.bybit.com/v5/public/spot"
	}
	return &BybitAdapter{
		bus:       b,
		symbols:   symbols,
		feedTypes: feedTypes,
		wsURL:     wsURL,
		state:     "disconnected",
	}
}

func (a *BybitAdapter) Name() string { return "bybit" }

func (a *BybitAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "bybit",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *BybitAdapter) Start(ctx context.Context) error {
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
			slog.Warn("bybit connection lost, reconnecting", "error", err)
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *BybitAdapter) connectAndStream(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, a.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial bybit: %w", err)
	}
	defer conn.Close()

	// Build subscription args.
	var args []string
	for _, sym := range a.symbols {
		for _, ft := range a.feedTypes {
			switch ft {
			case "ohlc":
				args = append(args, "kline.1."+sym)
			case "trades":
				args = append(args, "publicTrade."+sym)
			case "orderbook":
				args = append(args, "orderbook.25."+sym)
			case "funding":
				args = append(args, "tickers."+sym)
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
	slog.Info("bybit connected", "channels", len(args))

	// Bybit requires pings every 20s.
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"op":"ping"}`))
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

type bybitMsg struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
	Type  string          `json:"type"`
	Ts    int64           `json:"ts"`
}

func (a *BybitAdapter) processMessage(raw []byte) {
	var msg bybitMsg
	if json.Unmarshal(raw, &msg) != nil || msg.Topic == "" {
		return
	}

	switch {
	case strings.HasPrefix(msg.Topic, "kline."):
		// topic: "kline.1.BTCUSDT"
		parts := strings.SplitN(msg.Topic, ".", 3)
		if len(parts) == 3 {
			a.handleKline(parts[2], msg.Data)
		}
	case strings.HasPrefix(msg.Topic, "publicTrade."):
		parts := strings.SplitN(msg.Topic, ".", 2)
		if len(parts) == 2 {
			a.handleTrade(parts[1], msg.Data)
		}
	case strings.HasPrefix(msg.Topic, "orderbook."):
		parts := strings.SplitN(msg.Topic, ".", 3)
		if len(parts) == 3 {
			a.handleBook(parts[2], msg.Data)
		}
	case strings.HasPrefix(msg.Topic, "tickers."):
		parts := strings.SplitN(msg.Topic, ".", 2)
		if len(parts) == 2 {
			a.handleTickers(parts[1], msg.Data)
		}
	}
}

func (a *BybitAdapter) handleKline(symbol string, data json.RawMessage) {
	var klines []struct {
		Start     json.Number `json:"start"`
		Open      json.Number `json:"open"`
		High      json.Number `json:"high"`
		Low       json.Number `json:"low"`
		Close     json.Number `json:"close"`
		Volume    json.Number `json:"volume"`
		Confirm   bool        `json:"confirm"`
	}
	if json.Unmarshal(data, &klines) != nil {
		return
	}
	for _, k := range klines {
		ts, _ := k.Start.Int64()
		open, _ := k.Open.Float64()
		high, _ := k.High.Float64()
		low, _ := k.Low.Float64()
		close_, _ := k.Close.Float64()
		vol, _ := k.Volume.Float64()

		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("ohlc.bybit.%s", symbol),
			Payload: feeds.OHLC{
				Instrument: symbol, Exchange: "bybit", Timeframe: "1m",
				Timestamp: time.UnixMilli(ts),
				Open: open, High: high, Low: low, Close: close_, Volume: vol,
			},
		})
	}
}

func (a *BybitAdapter) handleTrade(symbol string, data json.RawMessage) {
	var trades []struct {
		Price json.Number `json:"p"`
		Size  json.Number `json:"v"`
		Side  string      `json:"S"`
		Time  json.Number `json:"T"`
		ID    string      `json:"i"`
	}
	if json.Unmarshal(data, &trades) != nil {
		return
	}
	for _, t := range trades {
		price, _ := t.Price.Float64()
		qty, _ := t.Size.Float64()
		ts, _ := t.Time.Int64()
		side := strings.ToLower(t.Side)

		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("trade.bybit.%s", symbol),
			Payload: feeds.Trade{
				Instrument: symbol, Exchange: "bybit",
				Timestamp: time.UnixMilli(ts),
				Price: price, Quantity: qty, Side: side,
				TradeID: t.ID,
			},
		})
	}
}

func (a *BybitAdapter) handleBook(symbol string, data json.RawMessage) {
	var book struct {
		Bids [][]string `json:"b"` // [price, qty]
		Asks [][]string `json:"a"`
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
		Topic: fmt.Sprintf("lob.bybit.%s", symbol),
		Payload: feeds.LOBSnapshot{
			Instrument: symbol, Exchange: "bybit",
			Timestamp: time.Now(), Bids: bids, Asks: asks,
		},
	})
}

func (a *BybitAdapter) handleTickers(symbol string, data json.RawMessage) {
	var t struct {
		FundingRate     json.Number `json:"fundingRate"`
		NextFundingTime json.Number `json:"nextFundingTime"`
		OpenInterest    json.Number `json:"openInterest"`
		MarkPrice       json.Number `json:"markPrice"`
		IndexPrice      json.Number `json:"indexPrice"`
	}
	if json.Unmarshal(data, &t) != nil {
		return
	}
	fr, _ := t.FundingRate.Float64()
	oi, _ := t.OpenInterest.Float64()
	mp, _ := t.MarkPrice.Float64()
	ip, _ := t.IndexPrice.Float64()
	nft, _ := t.NextFundingTime.Int64()

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("perp.bybit.%s", symbol),
		Payload: map[string]any{
			"Instrument": symbol, "Exchange": "bybit", "Type": "funding",
			"FundingRate": fr, "OpenInterest": oi,
			"MarkPrice": mp, "IndexPrice": ip,
			"NextFundingTime": time.UnixMilli(nft),
		},
	})
}
