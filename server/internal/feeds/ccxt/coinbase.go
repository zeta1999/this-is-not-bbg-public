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

// CoinbaseAdapter connects to Coinbase Advanced Trade WebSocket API.
type CoinbaseAdapter struct {
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

func NewCoinbaseAdapter(b *bus.Bus, symbols, feedTypes []string, wsURL, restBase string, rateLimit int) *CoinbaseAdapter {
	if wsURL == "" {
		wsURL = "wss://advanced-trade-ws.coinbase.com"
	}
	if restBase == "" {
		restBase = "https://api.coinbase.com"
	}
	return &CoinbaseAdapter{
		bus:       b,
		symbols:   symbols,
		feedTypes: feedTypes,
		wsURL:     wsURL,
		restBase:  restBase,
		rateLimit: rateLimit,
		state:     "disconnected",
	}
}

func (a *CoinbaseAdapter) Name() string { return "coinbase" }

func (a *CoinbaseAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "coinbase",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *CoinbaseAdapter) Start(ctx context.Context) error {
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
			slog.Warn("coinbase connection lost, reconnecting", "error", err)
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *CoinbaseAdapter) connectAndStream(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, a.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial coinbase: %w", err)
	}
	defer conn.Close()

	// Subscribe to channels.
	var channels []map[string]any
	for _, ft := range a.feedTypes {
		switch ft {
		case "trades":
			channels = append(channels, map[string]any{
				"name":        "market_trades",
				"product_ids": a.symbols,
			})
		case "ohlc":
			channels = append(channels, map[string]any{
				"name":        "candles",
				"product_ids": a.symbols,
			})
		case "orderbook":
			channels = append(channels, map[string]any{
				"name":        "level2",
				"product_ids": a.symbols,
			})
		}
	}

	sub := map[string]any{
		"type":     "subscribe",
		"channels": channels,
	}

	if err := conn.WriteJSON(sub); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()
	slog.Info("coinbase connected", "symbols", a.symbols)

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

		start := time.Now()
		a.processMessage(msg)

		a.mu.Lock()
		a.lastUpdate = time.Now()
		a.bytesRecv += uint64(len(msg))
		a.mu.Unlock()
		_ = start
	}
}

type coinbaseMsg struct {
	Channel string          `json:"channel"`
	Events  json.RawMessage `json:"events"`
}

type coinbaseTradeEvent struct {
	Trades []struct {
		ProductID string `json:"product_id"`
		Price     string `json:"price"`
		Size      string `json:"size"`
		Side      string `json:"side"`
		Time      string `json:"time"`
		TradeID   string `json:"trade_id"`
	} `json:"trades"`
}

type coinbaseCandleEvent struct {
	Candles []struct {
		ProductID string `json:"product_id"`
		Start     string `json:"start"`
		Open      string `json:"open"`
		High      string `json:"high"`
		Low       string `json:"low"`
		Close     string `json:"close"`
		Volume    string `json:"volume"`
	} `json:"candles"`
}

func (a *CoinbaseAdapter) processMessage(raw []byte) {
	var msg coinbaseMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	switch msg.Channel {
	case "market_trades":
		var events []coinbaseTradeEvent
		_ = json.Unmarshal(msg.Events, &events)
		for _, ev := range events {
			for _, t := range ev.Trades {
				price, _ := strconv.ParseFloat(t.Price, 64)
				qty, _ := strconv.ParseFloat(t.Size, 64)
				ts, _ := time.Parse(time.RFC3339Nano, t.Time)

				a.bus.Publish(bus.Message{
					Topic: fmt.Sprintf("trade.coinbase.%s", strings.ReplaceAll(t.ProductID, "-", "")),
					Payload: feeds.Trade{
						Instrument: t.ProductID,
						Exchange:   "coinbase",
						Timestamp:  ts,
						Price:      price,
						Quantity:   qty,
						Side:       strings.ToLower(t.Side),
						TradeID:    t.TradeID,
					},
				})
			}
		}

	case "candles":
		var events []coinbaseCandleEvent
		_ = json.Unmarshal(msg.Events, &events)
		for _, ev := range events {
			for _, c := range ev.Candles {
				open, _ := strconv.ParseFloat(c.Open, 64)
				high, _ := strconv.ParseFloat(c.High, 64)
				low, _ := strconv.ParseFloat(c.Low, 64)
				close_, _ := strconv.ParseFloat(c.Close, 64)
				vol, _ := strconv.ParseFloat(c.Volume, 64)
				startTS, _ := strconv.ParseInt(c.Start, 10, 64)

				a.bus.Publish(bus.Message{
					Topic: fmt.Sprintf("ohlc.coinbase.%s", strings.ReplaceAll(c.ProductID, "-", "")),
					Payload: feeds.OHLC{
						Instrument: c.ProductID,
						Exchange:   "coinbase",
						Timeframe:  "1m",
						Timestamp:  time.Unix(startTS, 0),
						Open:       open,
						High:       high,
						Low:        low,
						Close:      close_,
						Volume:     vol,
					},
				})
			}
		}
	}
}
