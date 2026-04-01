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

// OKXAdapter connects to OKX WebSocket for market data (spot + perps).
type OKXAdapter struct {
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

func NewOKXAdapter(b *bus.Bus, symbols, feedTypes []string, wsURL string) *OKXAdapter {
	if wsURL == "" {
		wsURL = "wss://ws.okx.com:8443/ws/v5/public"
	}
	return &OKXAdapter{
		bus:       b,
		symbols:   symbols,
		feedTypes: feedTypes,
		wsURL:     wsURL,
		state:     "disconnected",
	}
}

func (a *OKXAdapter) Name() string { return "okx" }

func (a *OKXAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "okx",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *OKXAdapter) Start(ctx context.Context) error {
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
			slog.Warn("okx connection lost, reconnecting", "error", err)
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *OKXAdapter) connectAndStream(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, a.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial okx: %w", err)
	}
	defer conn.Close()

	// Subscribe to channels.
	var args []map[string]string
	for _, sym := range a.symbols {
		instID := sym // OKX uses "BTC-USDT" format
		for _, ft := range a.feedTypes {
			switch ft {
			case "ohlc":
				args = append(args, map[string]string{"channel": "candle1m", "instId": instID})
			case "trades":
				args = append(args, map[string]string{"channel": "trades", "instId": instID})
			case "orderbook":
				args = append(args, map[string]string{"channel": "books5", "instId": instID})
			case "funding":
				args = append(args, map[string]string{"channel": "funding-rate", "instId": instID})
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
	slog.Info("okx connected", "channels", len(args))

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

type okxMsg struct {
	Arg  struct {
		Channel string `json:"channel"`
		InstID  string `json:"instId"`
	} `json:"arg"`
	Data []json.RawMessage `json:"data"`
}

func (a *OKXAdapter) processMessage(raw []byte) {
	var msg okxMsg
	if json.Unmarshal(raw, &msg) != nil || len(msg.Data) == 0 {
		return
	}

	channel := msg.Arg.Channel
	instID := msg.Arg.InstID
	// Normalize symbol: "BTC-USDT" → "BTCUSDT"
	symbol := strings.ReplaceAll(instID, "-", "")

	switch {
	case strings.HasPrefix(channel, "candle"):
		for _, d := range msg.Data {
			a.handleCandle(symbol, d)
		}
	case channel == "trades":
		for _, d := range msg.Data {
			a.handleTrade(symbol, d)
		}
	case strings.HasPrefix(channel, "books"):
		for _, d := range msg.Data {
			a.handleBook(symbol, d)
		}
	case channel == "funding-rate":
		for _, d := range msg.Data {
			a.handleFunding(symbol, d)
		}
	}
}

func (a *OKXAdapter) handleCandle(symbol string, data json.RawMessage) {
	var candle []json.Number // [ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm]
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
		Topic: fmt.Sprintf("ohlc.okx.%s", symbol),
		Payload: feeds.OHLC{
			Instrument: symbol, Exchange: "okx", Timeframe: "1m",
			Timestamp: time.UnixMilli(ts),
			Open: open, High: high, Low: low, Close: close_, Volume: vol,
		},
	})
}

func (a *OKXAdapter) handleTrade(symbol string, data json.RawMessage) {
	var t struct {
		Px    json.Number `json:"px"`
		Sz    json.Number `json:"sz"`
		Side  string      `json:"side"`
		Ts    json.Number `json:"ts"`
		TradeID string    `json:"tradeId"`
	}
	if json.Unmarshal(data, &t) != nil {
		return
	}
	price, _ := t.Px.Float64()
	qty, _ := t.Sz.Float64()
	ts, _ := t.Ts.Int64()

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("trade.okx.%s", symbol),
		Payload: feeds.Trade{
			Instrument: symbol, Exchange: "okx",
			Timestamp: time.UnixMilli(ts),
			Price: price, Quantity: qty, Side: t.Side,
			TradeID: t.TradeID,
		},
	})
}

func (a *OKXAdapter) handleBook(symbol string, data json.RawMessage) {
	var book struct {
		Bids [][]json.Number `json:"bids"` // [price, qty, _, numOrders]
		Asks [][]json.Number `json:"asks"`
	}
	if json.Unmarshal(data, &book) != nil {
		return
	}

	bids := make([]feeds.LOBLevel, 0, len(book.Bids))
	for _, b := range book.Bids {
		if len(b) < 2 { continue }
		p, _ := b[0].Float64()
		q, _ := b[1].Float64()
		bids = append(bids, feeds.LOBLevel{Price: p, Quantity: q})
	}
	asks := make([]feeds.LOBLevel, 0, len(book.Asks))
	for _, a_ := range book.Asks {
		if len(a_) < 2 { continue }
		p, _ := a_[0].Float64()
		q, _ := a_[1].Float64()
		asks = append(asks, feeds.LOBLevel{Price: p, Quantity: q})
	}

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("lob.okx.%s", symbol),
		Payload: feeds.LOBSnapshot{
			Instrument: symbol, Exchange: "okx",
			Timestamp: time.Now(), Bids: bids, Asks: asks,
		},
	})
}

func (a *OKXAdapter) handleFunding(symbol string, data json.RawMessage) {
	var f struct {
		FundingRate string `json:"fundingRate"`
		NextFundingRate string `json:"nextFundingRate"`
		FundingTime json.Number `json:"fundingTime"`
	}
	if json.Unmarshal(data, &f) != nil {
		return
	}
	rate, _ := strconv.ParseFloat(f.FundingRate, 64)
	nextRate, _ := strconv.ParseFloat(f.NextFundingRate, 64)
	ts, _ := f.FundingTime.Int64()

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("perp.okx.%s", symbol),
		Payload: map[string]any{
			"Instrument": symbol, "Exchange": "okx", "Type": "funding",
			"FundingRate": rate, "NextFundingRate": nextRate,
			"Timestamp": time.UnixMilli(ts),
		},
	})
}
