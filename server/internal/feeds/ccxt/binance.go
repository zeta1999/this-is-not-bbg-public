// Package ccxt provides exchange adapters using CCXT-compatible REST/WS APIs.
package ccxt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// BinanceAdapter connects to Binance spot market data.
type BinanceAdapter struct {
	bus        *bus.Bus
	symbols    []string
	feedTypes  []string
	timeframes []string
	wsURL      string
	restBase   string
	rateLimit  int

	mu          sync.RWMutex
	state       string
	lastUpdate  time.Time
	latencyMs   float64
	errorCount  uint64
	bytesRecv   uint64
}

// NewBinanceAdapter creates a Binance exchange adapter. Multiple
// timeframes subscribe to the corresponding native kline streams
// from Binance — feeds ingest at native rate per the
// backend-resample policy (no client-side or capture-time
// resampling). Empty timeframes defaults to ["1m"] to preserve the
// pre-multi-TF behaviour for older callers.
func NewBinanceAdapter(b *bus.Bus, symbols, feedTypes, timeframes []string, wsURL, restBase string, rateLimit int) *BinanceAdapter {
	if wsURL == "" {
		wsURL = "wss://stream.binance.com:9443/ws"
	}
	if restBase == "" {
		restBase = "https://api.binance.com"
	}
	if len(timeframes) == 0 {
		timeframes = []string{"1m"}
	}
	return &BinanceAdapter{
		bus:        b,
		symbols:    symbols,
		feedTypes:  feedTypes,
		timeframes: timeframes,
		wsURL:      wsURL,
		restBase:   restBase,
		rateLimit:  rateLimit,
		state:      "disconnected",
	}
}

func (a *BinanceAdapter) Name() string { return "binance" }

func (a *BinanceAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "binance",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		LatencyMs:     a.latencyMs,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *BinanceAdapter) setState(s string) {
	a.mu.Lock()
	a.state = s
	a.mu.Unlock()
}

func (a *BinanceAdapter) recordUpdate(bytes int, latency time.Duration) {
	a.mu.Lock()
	a.lastUpdate = time.Now()
	a.latencyMs = float64(latency.Microseconds()) / 1000.0
	a.bytesRecv += uint64(bytes)
	a.mu.Unlock()
}

func (a *BinanceAdapter) recordError() {
	a.mu.Lock()
	a.errorCount++
	a.mu.Unlock()
}

// Start connects to Binance WebSocket streams and begins publishing data.
func (a *BinanceAdapter) Start(ctx context.Context) error {
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
			a.recordError()
			slog.Warn("binance connection lost, reconnecting", "error", err)
			a.setState("reconnecting")
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *BinanceAdapter) connectAndStream(ctx context.Context) error {
	// Build combined stream URL.
	var streams []string
	for _, sym := range a.symbols {
		s := strings.ToLower(sym)
		for _, ft := range a.feedTypes {
			switch ft {
			case "trades":
				streams = append(streams, s+"@trade")
			case "ohlc":
				for _, tf := range a.timeframes {
					streams = append(streams, s+"@kline_"+tf)
				}
			case "orderbook":
				streams = append(streams, s+"@depth20@100ms")
			}
		}
	}

	url := strings.TrimSuffix(a.wsURL, "/ws") + "/stream?streams=" + strings.Join(streams, "/")
	slog.Info("connecting to binance", "url", url)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial binance: %w", err)
	}
	defer conn.Close()

	a.setState("connected")
	slog.Info("binance connected", "streams", len(streams))

	// Per-type counters for periodic logging.
	var klineCount, tradeCount, depthCount, unknownCount uint64
	lastLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			slog.Warn("binance read error", "error", err, "klines", klineCount, "trades", tradeCount, "depth", depthCount)
			return fmt.Errorf("read: %w", err)
		}

		start := time.Now()
		msgType := a.processMessageCounted(msg, &klineCount, &tradeCount, &depthCount, &unknownCount)
		a.recordUpdate(len(msg), time.Since(start))
		_ = msgType

		// Log stats every 30 seconds.
		if time.Since(lastLog) > 30*time.Second {
			slog.Info("binance stream stats",
				"klines", klineCount,
				"trades", tradeCount,
				"depth", depthCount,
				"unknown", unknownCount,
			)
			lastLog = time.Now()
		}
	}
}

// binanceStreamMsg is the wrapper for combined stream messages.
type binanceStreamMsg struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

type binanceTradeMsg struct {
	Symbol   string `json:"s"`
	Price    string `json:"p"`
	Quantity string `json:"q"`
	Time     int64  `json:"T"`
	IsBuyer  bool   `json:"m"` // true if buyer is market maker (i.e., sell aggressor)
	TradeID  int64  `json:"t"`
}

type binanceKlineMsg struct {
	Symbol string       `json:"s"`
	Kline  binanceKline `json:"k"`
}

type binanceKline struct {
	StartTime int64       `json:"t"`
	Interval  string      `json:"i"`
	Open      json.Number `json:"o"`
	High      json.Number `json:"h"`
	Low       json.Number `json:"l"`
	Close     json.Number `json:"c"`
	Volume    json.Number `json:"v"`
	IsClosed  bool        `json:"x"`
}

type binanceDepthMsg struct {
	Symbol string     `json:"s,omitempty"`
	Bids   [][]string `json:"bids"`
	Asks   [][]string `json:"asks"`
}

func (a *BinanceAdapter) processMessageCounted(raw []byte, klineCount, tradeCount, depthCount, unknownCount *uint64) string {
	// Try combined stream format first.
	var wrapper binanceStreamMsg
	if err := json.Unmarshal(raw, &wrapper); err != nil || wrapper.Stream == "" {
		slog.Debug("binance non-stream message", "raw", string(raw[:min(len(raw), 200)]))
		*unknownCount++
		return "unknown"
	}
	return a.parseAndPublish(wrapper.Stream, wrapper.Data, klineCount, tradeCount, depthCount, unknownCount)
}

func (a *BinanceAdapter) parseAndPublish(stream string, data json.RawMessage, klineCount, tradeCount, depthCount, unknownCount *uint64) string {
	switch {
	case strings.Contains(stream, "@trade"):
		a.handleTrade(data)
		*tradeCount++
		return "trade"
	case strings.Contains(stream, "@kline"):
		a.handleKline(data)
		*klineCount++
		return "kline"
	case strings.Contains(stream, "@depth"):
		a.handleDepth(stream, data)
		*depthCount++
		return "depth"
	default:
		slog.Debug("binance unknown stream type", "stream", stream)
		*unknownCount++
		return "unknown"
	}
}

func (a *BinanceAdapter) handleTrade(data json.RawMessage) {
	var msg binanceTradeMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		slog.Debug("binance trade parse error", "error", err)
		return
	}

	price, _ := strconv.ParseFloat(msg.Price, 64)
	qty, _ := strconv.ParseFloat(msg.Quantity, 64)

	side := "buy"
	if msg.IsBuyer {
		side = "sell"
	}

	trade := feeds.Trade{
		Instrument:      msg.Symbol,
		Exchange:        "binance",
		Timestamp:       time.UnixMilli(msg.Time),
		Price:           price,
		Quantity:        qty,
		Side:            side,
		TradeID:         strconv.FormatInt(msg.TradeID, 10),
		PriceDecimal:    msg.Price,
		QuantityDecimal: msg.Quantity,
	}

	a.bus.Publish(bus.Message{
		Topic:   fmt.Sprintf("trade.binance.%s", msg.Symbol),
		Payload: trade,
	})
}

func (a *BinanceAdapter) handleKline(data json.RawMessage) {
	var msg binanceKlineMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		slog.Warn("binance kline parse error", "error", err, "data", string(data[:min(len(data), 200)]))
		return
	}

	if msg.Symbol == "" {
		slog.Warn("binance kline missing symbol", "data", string(data[:min(len(data), 200)]))
		return
	}

	open, _ := msg.Kline.Open.Float64()
	high, _ := msg.Kline.High.Float64()
	low, _ := msg.Kline.Low.Float64()
	close_, _ := msg.Kline.Close.Float64()
	vol, _ := msg.Kline.Volume.Float64()

	// Anchor the bar's Timestamp at the bar OPEN by truncating to
	// the timeframe interval. The WAL was previously storing
	// "14:27:59.999+09:00" — that's the kline CLOSE time, which
	// happens because binance's WS payload has both `t` (open) and
	// `T` (close) and somewhere a stamp landed on the close field.
	// Truncate makes the source-of-truth unambiguous: every update
	// for the 14:27 1m bar produces the SAME 14:27:00 timestamp, so
	// upsertCandle's equal-Timestamp dedup actually works and
	// DataRange [now-10m, now] returns ~10 distinct minute bars
	// instead of thousands of close-stamped duplicates.
	ts := time.UnixMilli(msg.Kline.StartTime)
	if interval := tfInterval(msg.Kline.Interval); interval > 0 {
		ts = ts.Truncate(interval)
	}
	ohlc := feeds.OHLC{
		Instrument:    msg.Symbol,
		Exchange:      "binance",
		Timeframe:     msg.Kline.Interval,
		Timestamp:     ts,
		Open:          open,
		High:          high,
		Low:           low,
		Close:         close_,
		Volume:        vol,
		OpenDecimal:   msg.Kline.Open.String(),
		HighDecimal:   msg.Kline.High.String(),
		LowDecimal:    msg.Kline.Low.String(),
		CloseDecimal:  msg.Kline.Close.String(),
		VolumeDecimal: msg.Kline.Volume.String(),
	}

	a.bus.Publish(bus.Message{
		Topic:   fmt.Sprintf("ohlc.binance.%s", msg.Symbol),
		Payload: ohlc,
	})
}

func (a *BinanceAdapter) handleDepth(stream string, data json.RawMessage) {
	var msg binanceDepthMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		slog.Debug("binance depth parse error", "error", err)
		return
	}

	// Extract symbol from stream name: "btcusdt@depth20@100ms" -> "BTCUSDT"
	symbol := msg.Symbol
	if symbol == "" {
		parts := strings.Split(stream, "@")
		if len(parts) > 0 {
			symbol = strings.ToUpper(parts[0])
		}
	}

	bids := make([]feeds.LOBLevel, 0, len(msg.Bids))
	for _, b := range msg.Bids {
		if len(b) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(b[0], 64)
		qty, _ := strconv.ParseFloat(b[1], 64)
		bids = append(bids, feeds.LOBLevel{Price: price, Quantity: qty, PriceDecimal: b[0], QuantityDecimal: b[1]})
	}

	asks := make([]feeds.LOBLevel, 0, len(msg.Asks))
	for _, a_ := range msg.Asks {
		if len(a_) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(a_[0], 64)
		qty, _ := strconv.ParseFloat(a_[1], 64)
		asks = append(asks, feeds.LOBLevel{Price: price, Quantity: qty, PriceDecimal: a_[0], QuantityDecimal: a_[1]})
	}

	snap := feeds.LOBSnapshot{
		Instrument: symbol,
		Exchange:   "binance",
		Timestamp:  time.Now(),
		Bids:       bids,
		Asks:       asks,
	}

	a.bus.Publish(bus.Message{
		Topic:   fmt.Sprintf("lob.binance.%s", symbol),
		Payload: snap,
	})
}

// BackfillHistorical implements feeds.Backfiller. Fetches historical
// klines across the requested time window, paging through Binance's
// 1000-rows-per-call limit. Called by the backfill coordinator.
func (a *BinanceAdapter) BackfillHistorical(ctx context.Context, req feeds.BackfillRequest) ([]feeds.OHLC, error) {
	if req.Instrument == "" || req.Timeframe == "" {
		return nil, fmt.Errorf("instrument and timeframe required")
	}
	if req.From.IsZero() || req.To.IsZero() {
		return nil, fmt.Errorf("from and to required")
	}
	if req.To.Before(req.From) {
		return nil, fmt.Errorf("to before from")
	}

	const pageSize = 1000
	cap := req.Limit
	if cap <= 0 {
		cap = 100_000
	}

	// Page NEWEST → oldest. Each call uses `endTime=<cursor>` and
	// gets up to pageSize bars whose StartTime ≤ cursor. We then
	// advance cursor to the FIRST page bar's StartTime - 1 ms and
	// loop. Recent-first means the most useful bars (last hour, last
	// day) hit downstream consumers (cache, datalake, DataRange-as-
	// it-streams) within the first REST call instead of after the
	// whole 365-day grind. Caller still sees a single time-sorted
	// slice on return — we sort at the end.
	var out []feeds.OHLC
	cursor := req.To
	for cursor.After(req.From) && len(out) < cap {
		if err := ctx.Err(); err != nil {
			return out, err
		}

		url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&startTime=%d&endTime=%d&limit=%d",
			a.restBase, req.Instrument, req.Timeframe,
			req.From.UnixMilli(), cursor.UnixMilli(), pageSize)

		page, _, err := a.fetchKlineWindow(ctx, url, req.Instrument, req.Timeframe)
		if err != nil {
			return out, err
		}
		if len(page) == 0 {
			break
		}
		out = append(out, page...)
		// Advance backwards: the FIRST bar in the page (oldest in this
		// window) — minus 1 ms — becomes the new endTime, so the next
		// call picks up bars older than that without overlap.
		firstTS := page[0].Timestamp.UnixMilli()
		cursor = time.UnixMilli(firstTS - 1)
		if len(page) < pageSize {
			break
		}
	}
	if len(out) > cap {
		out = out[:cap]
	}
	// Sort ASC by Timestamp so callers (cache key derivation,
	// datalake partitioning, alignToNow, upsertCandle's fast path)
	// see a normal oldest-first slice regardless of fetch direction.
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out, nil
}

// fetchKlineWindow GETs a single Binance klines page and decodes it.
// Returns the decoded rows plus the last start-timestamp so the
// caller can page forward.
func (a *BinanceAdapter) fetchKlineWindow(ctx context.Context, url, symbol, timeframe string) ([]feeds.OHLC, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch klines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("binance REST %d", resp.StatusCode)
	}
	var raw [][]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, 0, fmt.Errorf("decode klines: %w", err)
	}

	var (
		candles []feeds.OHLC
		lastTS  int64
	)
	for _, k := range raw {
		if len(k) < 6 {
			continue
		}
		var ts int64
		_ = json.Unmarshal(k[0], &ts)
		var openS, highS, lowS, closeS, volS string
		_ = json.Unmarshal(k[1], &openS)
		_ = json.Unmarshal(k[2], &highS)
		_ = json.Unmarshal(k[3], &lowS)
		_ = json.Unmarshal(k[4], &closeS)
		_ = json.Unmarshal(k[5], &volS)

		open, _ := strconv.ParseFloat(openS, 64)
		high, _ := strconv.ParseFloat(highS, 64)
		low, _ := strconv.ParseFloat(lowS, 64)
		close_, _ := strconv.ParseFloat(closeS, 64)
		vol, _ := strconv.ParseFloat(volS, 64)

		barTs := time.UnixMilli(ts)
		if interval := tfInterval(timeframe); interval > 0 {
			barTs = barTs.Truncate(interval)
		}
		candles = append(candles, feeds.OHLC{
			Instrument: symbol,
			Exchange:   "binance",
			Timeframe:  timeframe,
			Timestamp:  barTs,
			Open:       open, High: high, Low: low, Close: close_, Volume: vol,
		})
		if ts > lastTS {
			lastTS = ts
		}
	}
	return candles, lastTS, nil
}

// FetchOHLCHistory fetches historical klines via REST API.
func (a *BinanceAdapter) FetchOHLCHistory(ctx context.Context, symbol, interval string, limit int) ([]feeds.OHLC, error) {
	url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d",
		a.restBase, symbol, interval, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch klines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance REST %d", resp.StatusCode)
	}

	var raw [][]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode klines: %w", err)
	}

	var candles []feeds.OHLC
	for _, k := range raw {
		if len(k) < 6 {
			continue
		}
		var ts int64
		_ = json.Unmarshal(k[0], &ts)
		var openS, highS, lowS, closeS, volS string
		_ = json.Unmarshal(k[1], &openS)
		_ = json.Unmarshal(k[2], &highS)
		_ = json.Unmarshal(k[3], &lowS)
		_ = json.Unmarshal(k[4], &closeS)
		_ = json.Unmarshal(k[5], &volS)

		open, _ := strconv.ParseFloat(openS, 64)
		high, _ := strconv.ParseFloat(highS, 64)
		low, _ := strconv.ParseFloat(lowS, 64)
		close_, _ := strconv.ParseFloat(closeS, 64)
		vol, _ := strconv.ParseFloat(volS, 64)

		barTs := time.UnixMilli(ts)
		if step := tfInterval(interval); step > 0 {
			barTs = barTs.Truncate(step)
		}
		candles = append(candles, feeds.OHLC{
			Instrument: symbol,
			Exchange:   "binance",
			Timeframe:  interval,
			Timestamp:  barTs,
			Open:       open,
			High:       high,
			Low:        low,
			Close:      close_,
			Volume:     vol,
		})
	}

	return candles, nil
}

// BackfillHistory fetches and caches historical OHLC data for all
// symbols across `days` days. Uses BackfillHistorical's paginated
// path so a 365-day daily window or 90-day hourly window aren't
// silently truncated at Binance's 1000-row-per-call ceiling — the
// cap before this commit hid useful 1d/1w history that BTCUSDT
// definitely has on Binance's end.
func (a *BinanceAdapter) BackfillHistory(ctx context.Context, days int, timeframes []string) error {
	var errCount atomic.Int64
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -days)

	// Recent-first pass: fetch the last 2 hours for every (symbol,
	// tf) BEFORE grinding through the deep 365-day window. This is
	// the chunk the trader actually sees on the chart's visible
	// window — populating it inside the first second of boot beats
	// staring at "3 candles" while the slow REST loop catches up.
	// Each call is a single REST page (≤120 rows for 1m × 2h).
	// Errors here are non-fatal; the deep pass below will retry.
	a.backfillRecentPass(ctx, now.Add(-2*time.Hour), now, timeframes)

	for _, symbol := range a.symbols {
		for _, tf := range timeframes {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			candles, err := a.BackfillHistorical(ctx, feeds.BackfillRequest{
				Instrument: symbol,
				Timeframe:  tf,
				From:       from,
				To:         now,
				Limit:      daysToLimit(days, tf),
			})
			if err != nil {
				slog.Warn("backfill error", "symbol", symbol, "tf", tf, "error", err)
				errCount.Add(1)
				continue
			}

			// Historical bars go on a dedicated topic prefix so live
			// consumers (sanity, alerts, consistency, TUI/desktop SSE)
			// don't see year-old klines as fresh ticks. The cache +
			// datalake writers subscribe to both prefixes; the
			// datalake normalizes the topic on the way to disk so
			// DataRange queries against `ohlc.*.*` still find these.
			for _, c := range candles {
				a.bus.Publish(bus.Message{
					Topic:   fmt.Sprintf("ohlc-historical.binance.%s", symbol),
					Payload: c,
				})
			}

			slog.Info("backfilled", "symbol", symbol, "tf", tf, "candles", len(candles))

			// Rate limit.
			time.Sleep(time.Second / time.Duration(max(a.rateLimit, 1)))
		}
	}

	if errCount.Load() > 0 {
		slog.Warn("backfill completed with errors", "error_count", errCount.Load())
	}
	return nil
}

func daysToLimit(days int, timeframe string) int {
	minutesPerDay := 24 * 60
	switch timeframe {
	case "1m":
		return days * minutesPerDay
	case "5m":
		return days * minutesPerDay / 5
	case "15m":
		return days * minutesPerDay / 15
	case "30m":
		return days * minutesPerDay / 30
	case "1h":
		return days * 24
	case "4h":
		return days * 6
	case "1d":
		return days
	case "1w":
		// 7 days per candle, so days/7 — round up so a 7-day request
		// still returns at least one candle.
		w := days / 7
		if w < 1 {
			w = 1
		}
		return w
	default:
		return days * minutesPerDay
	}
}

// tfInterval converts a binance kline interval string ("1m", "5m",
// "1h", "1d", "1w") to a time.Duration suitable for Truncate. The
// adapter uses this to align bar timestamps to the interval open
// boundary regardless of which field on the kline payload the
// timestamp was sourced from. Unknown / unsupported intervals
// return 0 so the caller leaves the timestamp untruncated.
func tfInterval(s string) time.Duration {
	switch s {
	case "1m":
		return 1 * time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return 1 * time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "8h":
		return 8 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "1d":
		return 24 * time.Hour
	case "3d":
		return 3 * 24 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	}
	return 0
}

// backfillRecentPass fetches the [from, to] window for every
// (symbol, tf) pair and publishes the results on the historical
// topic, IN PARALLEL across symbols (rate-limit gated by binance's
// per-second budget — the deep pass below uses sleep, but the
// recent pass is small enough to fire concurrently). Designed to
// finish within seconds even for a dozen symbols × 7 timeframes
// so the chart populates the visible window immediately instead
// of waiting for the 365-day grind.
//
// Errors per (symbol, tf) are logged and swallowed — the deep
// pass that follows handles retries naturally.
func (a *BinanceAdapter) backfillRecentPass(ctx context.Context, from, to time.Time, timeframes []string) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // cap concurrency so we don't blow the REST budget

	for _, symbol := range a.symbols {
		for _, tf := range timeframes {
			select {
			case <-ctx.Done():
				return
			default:
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(symbol, tf string) {
				defer wg.Done()
				defer func() { <-sem }()
				candles, err := a.BackfillHistorical(ctx, feeds.BackfillRequest{
					Instrument: symbol,
					Timeframe:  tf,
					From:       from,
					To:         to,
				})
				if err != nil || len(candles) == 0 {
					return
				}
				for _, c := range candles {
					a.bus.Publish(bus.Message{
						Topic:   fmt.Sprintf("ohlc-historical.binance.%s", symbol),
						Payload: c,
					})
				}
				slog.Info("backfill recent-pass", "symbol", symbol, "tf", tf, "candles", len(candles))
			}(symbol, tf)
		}
	}
	wg.Wait()
}
