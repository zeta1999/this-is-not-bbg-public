package app

import (
	"encoding/json"
	"testing"

	"github.com/notbbg/notbbg/tui/internal/client"
	"github.com/notbbg/notbbg/tui/internal/views"
)

func TestParseOHLCChunk_FiltersByTimeframe(t *testing.T) {
	mk := func(tf string, close float64) client.DataRangeRecord {
		p, _ := json.Marshal(map[string]any{
			"Timeframe": tf, "Open": 1.0, "High": 2.0, "Low": 0.5, "Close": close, "Volume": 10.0,
		})
		return client.DataRangeRecord{Topic: "ohlc.binance.BTCUSDT", Payload: p}
	}
	ch := client.DataRangeChunk{Records: []client.DataRangeRecord{mk("1m", 100), mk("1h", 200), mk("1m", 101)}}

	got := parseOHLCChunk(ch, "1m")
	if len(got) != 2 {
		t.Fatalf("want 2 1m candles, got %d", len(got))
	}
	if got[1].Close != 101 {
		t.Fatalf("want close=101, got %v", got[1].Close)
	}
}

func TestApplyHistoryEvent_MergesAndCaps(t *testing.T) {
	m := &Model{
		ohlcData: map[string]*instrumentData{
			"BTCUSDT/binance": {
				Instrument: "BTCUSDT", Exchange: "binance", ActiveTF: "1m",
				Timeframes: map[string]*timeframeData{
					"1m": {Candles: []views.Candle{{Close: 50}, {Close: 51}}, Loading: true},
				},
			},
		},
	}
	m.guiCache.OHLCRowsPerInstrument = 3

	m.applyHistoryEvent(historyEvent{
		key:     "BTCUSDT/binance",
		tf:      "1m",
		candles: []views.Candle{{Close: 48}, {Close: 49}},
	})

	got := m.ohlcData["BTCUSDT/binance"].Timeframes["1m"].Candles
	if len(got) != 3 {
		t.Fatalf("cap mismatch: len=%d", len(got))
	}
	if got[0].Close != 49 || got[2].Close != 51 {
		t.Fatalf("merge order wrong: %+v", got)
	}

	// EOF flips loading off.
	m.applyHistoryEvent(historyEvent{key: "BTCUSDT/binance", tf: "1m", eof: true})
	if m.ohlcData["BTCUSDT/binance"].Timeframes["1m"].Loading {
		t.Fatalf("loading should be false after eof")
	}
}
