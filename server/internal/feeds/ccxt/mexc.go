package ccxt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// MEXCAdapter polls MEXC's v3 REST API for market data. MEXC moved
// its public spot WebSocket to protobuf in 2025, which would pull
// in a non-trivial codegen dependency for a cross-venue-sanity use
// case that only needs ~30s cadence. REST polling is simpler, works
// today, and hits the same bus topics so sanity + consistency treat
// MEXC as an equal peer alongside the WS-based venues.
//
// Cadence: OHLC 1m + depth-20 pulled every `PollInterval` (default
// 5s) per symbol. Each symbol poll is a separate HTTP GET, so at the
// default interval a ten-symbol config makes 20 GETs every 5s — well
// under MEXC's 20000 req/5s IP limit.
type MEXCAdapter struct {
	bus          *bus.Bus
	symbols      []string
	feedTypes    []string
	restBase     string
	pollInterval time.Duration
	client       *http.Client

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

func NewMEXCAdapter(b *bus.Bus, symbols, feedTypes []string, restBase string, pollInterval time.Duration) *MEXCAdapter {
	if restBase == "" {
		restBase = "https://api.mexc.com"
	}
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	return &MEXCAdapter{
		bus:          b,
		symbols:      symbols,
		feedTypes:    feedTypes,
		restBase:     restBase,
		pollInterval: pollInterval,
		client:       &http.Client{Timeout: 10 * time.Second},
		state:        "disconnected",
	}
}

func (a *MEXCAdapter) Name() string { return "mexc" }

func (a *MEXCAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "mexc",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *MEXCAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	a.state = "connected" // REST polling; no persistent handshake to maintain
	a.mu.Unlock()
	slog.Info("mexc polling", "symbols", len(a.symbols), "interval", a.pollInterval)

	// Kick one round immediately so the MON panel doesn't sit on
	// "connected" for up to pollInterval with no data.
	a.pollAll(ctx)

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.pollAll(ctx)
		}
	}
}

func (a *MEXCAdapter) pollAll(ctx context.Context) {
	for _, sym := range a.symbols {
		for _, ft := range a.feedTypes {
			if ctx.Err() != nil {
				return
			}
			switch ft {
			case "ohlc":
				a.pollKline(ctx, sym)
			case "orderbook":
				a.pollDepth(ctx, sym)
			case "trades":
				a.pollTrades(ctx, sym)
			}
		}
	}
}

// pollKline fetches the most recent 1m candle for sym. MEXC's klines
// endpoint returns an array of [openTs, open, high, low, close, vol,
// closeTs, quoteVol] per bar.
func (a *MEXCAdapter) pollKline(ctx context.Context, sym string) {
	url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=1m&limit=1", a.restBase, sym)
	body, err := a.get(ctx, url)
	if err != nil {
		return
	}
	var rows [][]any
	if err := json.Unmarshal(body, &rows); err != nil || len(rows) == 0 || len(rows[0]) < 6 {
		return
	}
	r := rows[0]
	ts, _ := toInt64(r[0])
	o, _ := toFloat(r[1])
	h, _ := toFloat(r[2])
	l, _ := toFloat(r[3])
	c, _ := toFloat(r[4])
	vol, _ := toFloat(r[5])

	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("ohlc.mexc.%s", sym),
		Payload: feeds.OHLC{
			Instrument: sym, Exchange: "mexc", Timeframe: "1m",
			Timestamp: time.UnixMilli(ts),
			Open:      o, High: h, Low: l, Close: c, Volume: vol,
		},
	})
}

func (a *MEXCAdapter) pollDepth(ctx context.Context, sym string) {
	url := fmt.Sprintf("%s/api/v3/depth?symbol=%s&limit=20", a.restBase, sym)
	body, err := a.get(ctx, url)
	if err != nil {
		return
	}
	var d struct {
		Bids [][]string `json:"bids"`
		Asks [][]string `json:"asks"`
	}
	if json.Unmarshal(body, &d) != nil {
		return
	}
	bids := make([]feeds.LOBLevel, 0, len(d.Bids))
	for _, row := range d.Bids {
		if len(row) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(row[0], 64)
		q, _ := strconv.ParseFloat(row[1], 64)
		bids = append(bids, feeds.LOBLevel{Price: p, Quantity: q})
	}
	asks := make([]feeds.LOBLevel, 0, len(d.Asks))
	for _, row := range d.Asks {
		if len(row) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(row[0], 64)
		q, _ := strconv.ParseFloat(row[1], 64)
		asks = append(asks, feeds.LOBLevel{Price: p, Quantity: q})
	}
	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("lob.mexc.%s", sym),
		Payload: feeds.LOBSnapshot{
			Instrument: sym, Exchange: "mexc",
			Timestamp: time.Now(), Bids: bids, Asks: asks,
		},
	})
}

func (a *MEXCAdapter) pollTrades(ctx context.Context, sym string) {
	url := fmt.Sprintf("%s/api/v3/trades?symbol=%s&limit=50", a.restBase, sym)
	body, err := a.get(ctx, url)
	if err != nil {
		return
	}
	var rows []struct {
		ID           int64       `json:"id"`
		Price        json.Number `json:"price"`
		Qty          json.Number `json:"qty"`
		Time         int64       `json:"time"`
		IsBuyerMaker bool        `json:"isBuyerMaker"`
	}
	if json.Unmarshal(body, &rows) != nil {
		return
	}
	for _, r := range rows {
		px, _ := r.Price.Float64()
		qty, _ := r.Qty.Float64()
		side := "buy"
		if r.IsBuyerMaker {
			side = "sell"
		}
		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("trade.mexc.%s", sym),
			Payload: feeds.Trade{
				Instrument: sym, Exchange: "mexc",
				Timestamp: time.UnixMilli(r.Time),
				Price:     px, Quantity: qty, Side: side,
				TradeID: strconv.FormatInt(r.ID, 10),
			},
		})
	}
}

func (a *MEXCAdapter) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		return nil, fmt.Errorf("mexc %s: http %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.lastUpdate = time.Now()
	a.bytesRecv += uint64(len(body))
	a.mu.Unlock()
	return body, nil
}

// toFloat / toInt64 coerce JSON values that may arrive as either
// strings (common for MEXC price/qty) or numbers (timestamps).
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case string:
		i, err := strconv.ParseInt(x, 10, 64)
		return i, err == nil
	}
	return 0, false
}
