package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/notbbg/notbbg/tui/internal/client"
	"github.com/notbbg/notbbg/tui/internal/views"
)

func TestParseOHLCChunk_FiltersByTimeframe(t *testing.T) {
	t0 := time.Date(2026, 4, 28, 23, 0, 0, 0, time.UTC)
	mk := func(tf string, close float64, when time.Time) client.DataRangeRecord {
		p, _ := json.Marshal(map[string]any{
			"Timeframe": tf, "Timestamp": when.Format(time.RFC3339Nano),
			"Open": 1.0, "High": 2.0, "Low": 0.5, "Close": close, "Volume": 10.0,
		})
		return client.DataRangeRecord{
			Topic:     "ohlc.binance.BTCUSDT",
			Timestamp: when.Format(time.RFC3339Nano),
			Payload:   p,
		}
	}
	ch := client.DataRangeChunk{Records: []client.DataRangeRecord{
		mk("1m", 100, t0),
		mk("1h", 200, t0.Add(time.Hour)),
		mk("1m", 101, t0.Add(time.Minute)),
	}}

	got := parseOHLCChunk(ch, "1m", "ohlc.binance.BTCUSDT")
	if len(got) != 2 {
		t.Fatalf("want 2 1m candles, got %d", len(got))
	}
	if got[1].Close != 101 {
		t.Fatalf("want close=101, got %v", got[1].Close)
	}
}

// TestParseOHLCChunk_RequiresTimestamp guarantees the user-asked
// invariant: a candle without a usable timestamp is useless and
// must be dropped, not rendered at an approximate slot.
func TestParseOHLCChunk_RequiresTimestamp(t *testing.T) {
	p, _ := json.Marshal(map[string]any{
		"Timeframe": "1m", "Open": 1.0, "High": 2.0, "Low": 0.5, "Close": 100.0,
	})
	ch := client.DataRangeChunk{Records: []client.DataRangeRecord{
		{Topic: "ohlc.binance.BTCUSDT", Payload: p}, // no outer or inner ts
	}}
	got := parseOHLCChunk(ch, "1m", "ohlc.binance.BTCUSDT")
	if len(got) != 0 {
		t.Fatalf("candle without timestamp must be dropped; got %d", len(got))
	}
}

func TestApplyHistoryEvent_MergesAndCaps(t *testing.T) {
	// Use timestamped candles — that's what production wires after
	// the time-anchored chart change. upsertCandle sorts by
	// Timestamp ASC and dedupes equal timestamps.
	t0 := time.Date(2026, 4, 28, 23, 0, 0, 0, time.UTC)
	m := &Model{
		ohlcData: map[string]*instrumentData{
			"BTCUSDT/binance": {
				Instrument: "BTCUSDT", Exchange: "binance", ActiveTF: "1m",
				Timeframes: map[string]*timeframeData{
					"1m": {Candles: []views.Candle{
						{Timestamp: t0.Add(2 * time.Minute), Close: 50},
						{Timestamp: t0.Add(3 * time.Minute), Close: 51},
					}, Loading: true},
				},
			},
		},
	}
	m.guiCache.OHLCRowsPerInstrument = 3

	m.applyHistoryEvent(historyEvent{
		key: "BTCUSDT/binance",
		tf:  "1m",
		candles: []views.Candle{
			{Timestamp: t0, Close: 48},
			{Timestamp: t0.Add(time.Minute), Close: 49},
		},
	})

	got := m.ohlcData["BTCUSDT/binance"].Timeframes["1m"].Candles
	if len(got) != 3 {
		t.Fatalf("cap mismatch: len=%d", len(got))
	}
	// Sorted by Timestamp ASC, capped at 3 = drop oldest →
	// {49 @t+1m, 50 @t+2m, 51 @t+3m}.
	if got[0].Close != 49 || got[1].Close != 50 || got[2].Close != 51 {
		t.Fatalf("merge order wrong: %+v", got)
	}

	// EOF flips loading off (when no progressive backfill remains).
	m.applyHistoryEvent(historyEvent{key: "BTCUSDT/binance", tf: "1m", eof: true})
	if m.ohlcData["BTCUSDT/binance"].Timeframes["1m"].Loading {
		t.Fatalf("loading should be false after eof")
	}
}
