package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/notbbg/notbbg/tui/internal/client"
	"github.com/notbbg/notbbg/tui/internal/views"
)

// historyChunkSchedule is the explicit "fetch the recent past
// first, then progressively deeper" sequence the trader asked for.
// Each entry is a window from NOW backwards, so chunk N's window
// fully contains chunk N-1's. upsertCandle dedupes overlap, so the
// chart accretes coverage outward from "now" without holes.
//
// 10m → 30m → 1h → 4h → 12h → 1d → 3d → 7d → 30d → 90d → 1y → 5y.
//
// Tuned so the FIRST fetch (10m) lands in well under a second, and
// subsequent fetches double-or-quadruple the horizon. The trader
// sees the most-recent past immediately; deeper history fills in
// behind it without ever blocking the right edge.
var historyChunkSchedule = []time.Duration{
	10 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
	4 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
	3 * 24 * time.Hour,
	7 * 24 * time.Hour,
	30 * 24 * time.Hour,
	90 * 24 * time.Hour,
	365 * 24 * time.Hour,
	5 * 365 * 24 * time.Hour,
}

// autoFetchTotalChunks returns the index in historyChunkSchedule
// up to which we automatically backfill for a given TF. Higher
// timeframes don't benefit from very-recent windows (1h on a 10m
// fetch returns 0 candles) and don't benefit from very-deep ones
// either (a 5y window for 1m TF is 2.6 million bars). This caps
// the schedule to a sensible per-TF horizon.
func autoFetchTotalChunks(tf string) int {
	// Index into historyChunkSchedule. Anything past this is
	// out-of-scope for the auto-load (operator can press H for
	// the legacy 24h manual load).
	switch tf {
	case "1m":
		return 5 // up to 12h
	case "5m":
		return 6 // up to 24h
	case "15m":
		return 7 // up to 3d
	case "30m":
		return 8 // up to 7d
	case "1h":
		return 9 // up to 30d
	case "4h":
		return 10 // up to 90d
	case "1d":
		return 11 // up to 1y
	case "1w":
		return 12 // up to 5y
	}
	return 6 // 24h default
}

// AutoLoadHistoryIfNeeded fires a background backfill when the
// active OHLC instrument has too few candles to populate the
// visible chart. Idempotent — tfd.Loading + tfd.AutoFetchedAt gate
// re-triggers, so a slow REST adapter doesn't spam requests.
//
// Called from the pollData loop so it cost-amortises into the
// existing 16ms tick — no extra goroutines, no race against
// keypresses. The actual fetch still runs in its own goroutine
// via startHistoryLoad.
func (m *Model) AutoLoadHistoryIfNeeded() {
	if m.activePanelName() != PanelOHLC {
		return
	}
	if m.historyCli == nil {
		return
	}
	if m.ohlcActiveIdx >= len(m.ohlcKeys) {
		return
	}
	key := m.ohlcKeys[m.ohlcActiveIdx]
	d, ok := m.ohlcData[key]
	if !ok || d.ActiveTF == "" {
		return
	}
	tfd, ok := d.Timeframes[d.ActiveTF]
	if !ok {
		// No timeframeData yet — wait for the first realtime tick
		// to materialise it, then auto-fetch on the next pollData.
		return
	}
	if tfd.Loading {
		return
	}
	// "Too few" = fewer candles than we'd want visible. The chart
	// width can grow at runtime, but 80 is a reasonable threshold —
	// matches a typical 200-col terminal's slot count.
	const minCandles = 80
	if len(tfd.Candles) >= minCandles {
		return
	}
	// Throttle: don't re-fire within 60s of the last attempt. A
	// failing fetch shouldn't hammer the REST gateway.
	if !tfd.AutoFetchedAt.IsZero() && time.Since(tfd.AutoFetchedAt) < 60*time.Second {
		return
	}
	tfd.AutoFetchedAt = time.Now()
	// Total chunks come from the per-TF schedule. startHistoryLoad
	// fires chunk 1 (10m, the most-recent past), and each EOF
	// triggers the next chunk in sequence — 30m, 1h, 4h, …
	m.startHistoryLoad(0)
	m.statusMsg = "Auto-loading " + d.ActiveTF + " history (recent past first)…"
}

// startHistoryLoad kicks off a *progressive* background DataRange
// fetch for the active OHLC instrument + timeframe. The schedule
// (historyChunkSchedule) is cumulative-from-now:
//
//   chunk 0 → [now - 10m, now]   ← fastest, lands first
//   chunk 1 → [now - 30m, now]
//   chunk 2 → [now - 1h , now]
//   chunk 3 → [now - 4h , now]
//   ... up to autoFetchTotalChunks(tf) entries.
//
// Each chunk fully contains the previous one. upsertCandle dedupes
// overlap on apply. The trader sees the most-recent past first;
// deeper history accretes leftward as later chunks land.
//
// totalChunksOverride > 0 forces a different chunk count (used by
// the explicit H-load shortcut to fetch a deeper horizon than
// the per-TF auto default).
func (m *Model) startHistoryLoad(totalChunksOverride int) {
	if m.historyCli == nil {
		return
	}
	if m.ohlcActiveIdx >= len(m.ohlcKeys) {
		return
	}
	key := m.ohlcKeys[m.ohlcActiveIdx]
	d, ok := m.ohlcData[key]
	if !ok || d.ActiveTF == "" {
		return
	}
	tfd, ok := d.Timeframes[d.ActiveTF]
	if !ok {
		tfd = &timeframeData{}
		d.Timeframes[d.ActiveTF] = tfd
	}
	if tfd.Loading {
		return // already in flight
	}

	tf := d.ActiveTF
	chunks := autoFetchTotalChunks(tf)
	if totalChunksOverride > 0 && totalChunksOverride <= len(historyChunkSchedule) {
		chunks = totalChunksOverride
	}
	if chunks < 1 {
		chunks = 1
	}

	tfd.Loading = true
	tfd.LoadSeq = 0
	tfd.ProgressIdx = 0
	tfd.ProgressTotal = chunks

	// Fire chunk 0 = [now - schedule[0], now]. The 10m window is
	// fast enough to land in well under a second on any reasonable
	// REST gateway — the trader sees the most-recent past
	// immediately.
	to := time.Now().UTC()
	from := to.Add(-historyChunkSchedule[0])
	topic := "ohlc." + d.Exchange + "." + d.Instrument
	go fetchHistory(m.historyCli, m.histCh, key, tf, topic, from, to)
}

// startNextHistoryChunk fires chunk N+1 of a progressive backfill,
// covering [now - schedule[N+1], now]. Cumulative from-now so each
// chunk fully contains the previous one. Called from
// applyHistoryEvent on EOF when more horizon remains.
func (m *Model) startNextHistoryChunk(key, tf string) {
	if m.historyCli == nil {
		return
	}
	d, ok := m.ohlcData[key]
	if !ok {
		return
	}
	tfd, ok := d.Timeframes[tf]
	if !ok {
		return
	}
	idx := tfd.ProgressIdx
	if idx >= len(historyChunkSchedule) || idx >= tfd.ProgressTotal {
		// Schedule exhausted — nothing more to fetch.
		tfd.Loading = false
		return
	}
	to := time.Now().UTC()
	from := to.Add(-historyChunkSchedule[idx])
	tfd.Loading = true
	topic := "ohlc." + d.Exchange + "." + d.Instrument
	go fetchHistory(m.historyCli, m.histCh, key, tf, topic, from, to)
}

// fetchHistory runs in its own goroutine. It does one Fetch call,
// translates each NDJSON chunk into a historyEvent, and pushes
// onto out. On success emits a final eof=true event; on error
// emits one event with err set + eof=true.
func fetchHistory(c *client.DataRangeClient, out chan<- historyEvent, key, tf, topic string, from, to time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := c.Fetch(ctx, topic, from, to, key+"/"+tf, 5000, func(ch client.DataRangeChunk) error {
		if ch.EOF {
			out <- historyEvent{key: key, tf: tf, eof: true}
			return nil
		}
		candles := parseOHLCChunk(ch, tf, topic)
		if len(candles) > 0 {
			out <- historyEvent{key: key, tf: tf, candles: candles}
		}
		return nil
	})
	if err != nil {
		slog.Debug("tui history fetch error", "topic", topic, "tf", tf, "err", err)
		out <- historyEvent{key: key, tf: tf, err: err, eof: true}
	}
}

// parseOHLCChunk picks the candles that match the requested
// timeframe out of a DataRangeChunk. OHLC bus messages carry the
// timeframe in the payload (topic is `ohlc.<exchange>.<instrument>`
// — not partitioned by timeframe).
// parseOHLCChunk picks the candles that match the requested
// timeframe out of a DataRangeChunk. The bar's Timestamp lives on
// the OUTER DataRangeRecord (`_timestamp`), not inside the
// payload — that's how the WAL stores it. Falling back to the
// payload's own Timestamp covers the realtime SSE path which
// embeds it there. A candle without ANY timestamp is dropped (a
// price level without a time is meaningless for the trader).
func parseOHLCChunk(ch client.DataRangeChunk, wantTF string, topic string) []views.Candle {
	var out []views.Candle
	for _, r := range ch.Records {
		var p struct {
			Timeframe string  `json:"Timeframe"`
			Timestamp string  `json:"Timestamp"`
			Open      float64 `json:"Open"`
			High      float64 `json:"High"`
			Low       float64 `json:"Low"`
			Close     float64 `json:"Close"`
			Volume    float64 `json:"Volume"`
		}
		if err := json.Unmarshal(r.Payload, &p); err != nil {
			continue
		}
		if wantTF != "" && p.Timeframe != wantTF {
			continue
		}
		// Prefer the payload's Timestamp — that's the bar's
		// actual open time, set by the feed adapter from the
		// exchange's kline.StartTime. The outer _timestamp is the
		// WAL ingest time (= "now" during a backfill replay), which
		// is useless as a canonical event time. Fall back to outer
		// only when the payload has nothing.
		var ts time.Time
		for _, raw := range []string{p.Timestamp, r.Timestamp} {
			if raw == "" {
				continue
			}
			if t, err := time.Parse(time.RFC3339Nano, raw); err == nil && !t.IsZero() {
				ts = t
				break
			}
			if t, err := time.Parse(time.RFC3339, raw); err == nil && !t.IsZero() {
				ts = t
				break
			}
		}
		if ts.IsZero() {
			// No usable timestamp → useless candle, skip.
			continue
		}
		out = append(out, views.Candle{
			Timestamp: ts,
			Open:      p.Open,
			High:      p.High,
			Low:       p.Low,
			Close:     p.Close,
			Volume:    p.Volume,
		})
	}
	if ohlcDebugMatchTopic(topic) {
		if len(out) > 0 {
			ohlcDebug("CHUNK %s tf=%s n=%d span=[%s..%s]",
				topic, wantTF, len(out),
				out[0].Timestamp.UTC().Format("01-02 15:04:05"),
				out[len(out)-1].Timestamp.UTC().Format("01-02 15:04:05"))
		} else {
			ohlcDebug("CHUNK %s tf=%s n=0 (filtered or empty, raw=%d)",
				topic, wantTF, len(ch.Records))
		}
	}
	return out
}

// applyHistoryEvent merges one history chunk into state. Runs on
// the main bubbletea goroutine — no locking needed.
func (m *Model) applyHistoryEvent(ev historyEvent) {
	d, ok := m.ohlcData[ev.key]
	if !ok {
		return
	}
	tfd, ok := d.Timeframes[ev.tf]
	if !ok {
		tfd = &timeframeData{}
		d.Timeframes[ev.tf] = tfd
	}
	if len(ev.candles) > 0 {
		tfd.LoadSeq++
		// Use the same upsertCandle primitive the realtime path
		// uses so the array stays sorted by Timestamp ASC and
		// duplicates collapse — backfill chunks may overlap with
		// realtime ticks (the bar at the boundary) and naive
		// prepend used to produce out-of-order arrays that the
		// time-anchored renderer couldn't trust.
		for _, c := range ev.candles {
			tfd.Candles = upsertCandle(tfd.Candles, c)
		}
		cap := m.guiCache.OHLCRowsPerInstrument
		if cap <= 0 {
			cap = 2000
		}
		if len(tfd.Candles) > cap {
			tfd.Candles = tfd.Candles[len(tfd.Candles)-cap:]
		}
		tfd.LastUpdate = time.Now()
		if ohlcDebugMatchTopic(ev.key) {
			first := tfd.Candles[0].Timestamp.UTC().Format("01-02 15:04:05")
			last := tfd.Candles[len(tfd.Candles)-1].Timestamp.UTC().Format("01-02 15:04:05")
			ohlcDebug("APPLY %s tf=%s +%d total=%d span=[%s..%s] cap=%d",
				ev.key, ev.tf, len(ev.candles), len(tfd.Candles), first, last, cap)
		}
	}
	if ev.eof {
		tfd.Loading = false
		// Progressive backfill: when this chunk's EOF arrives, fire
		// the next-older chunk if we haven't reached the total
		// horizon yet. Right-to-left fill — the trader sees recent
		// bars first, history accretes leftward.
		if tfd.ProgressIdx < tfd.ProgressTotal && ev.err == nil {
			tfd.ProgressIdx++
			m.startNextHistoryChunk(ev.key, ev.tf)
		}
	}
	if ev.err != nil {
		m.statusMsg = "history: " + ev.err.Error()
	}
}
