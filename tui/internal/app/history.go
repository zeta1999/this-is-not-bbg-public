package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/notbbg/notbbg/tui/internal/client"
	"github.com/notbbg/notbbg/tui/internal/views"
)

// startHistoryLoad kicks off a background DataRange fetch for the
// currently active OHLC instrument + timeframe. It's a no-op if
// there is no history client (HTTP gateway disabled) or if a load
// for this key/tf is already in flight.
//
// The fetch runs in its own goroutine; chunks are parsed and queued
// on histCh so the main bubbletea loop merges them on its normal
// pollData tick. This is what makes the panel "non-blocking": key
// presses during the fetch still flow through Update; we never
// block on an HTTP request.
func (m *Model) startHistoryLoad(window time.Duration) {
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
	tfd.Loading = true
	tfd.LoadSeq = 0

	instrument, exchange, tf := d.Instrument, d.Exchange, d.ActiveTF
	if window <= 0 {
		window = 24 * time.Hour
	}
	to := time.Now().UTC()
	from := to.Add(-window)
	topic := "ohlc." + exchange + "." + instrument

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
		candles := parseOHLCChunk(ch, tf)
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
func parseOHLCChunk(ch client.DataRangeChunk, wantTF string) []views.Candle {
	var out []views.Candle
	for _, r := range ch.Records {
		var p struct {
			Timeframe string  `json:"Timeframe"`
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
		out = append(out, views.Candle{
			Open:   p.Open,
			High:   p.High,
			Low:    p.Low,
			Close:  p.Close,
			Volume: p.Volume,
		})
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
		// Historical candles are older than what's currently in the
		// buffer. Prepend by prefix-assembly; cap respects gui cache.
		merged := append(ev.candles, tfd.Candles...)
		cap := m.guiCache.OHLCRowsPerInstrument
		if cap <= 0 {
			cap = 2000
		}
		if len(merged) > cap {
			merged = merged[len(merged)-cap:]
		}
		tfd.Candles = merged
		tfd.LastUpdate = time.Now()
	}
	if ev.eof {
		tfd.Loading = false
	}
	if ev.err != nil {
		m.statusMsg = "history: " + ev.err.Error()
	}
}
