package views

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestSanitySnapshot_DecodesServerJSON locks the JSON-tag mapping
// against the shape the desktop store consumes, so the TUI parses
// the same `sanity.prices` payload without drift.
func TestSanitySnapshot_DecodesServerJSON(t *testing.T) {
	raw := []byte(`{
		"instrument": "BTCUSDT",
		"median": 70000.5,
		"threshold_pct": 0.5,
		"venue_count": 3,
		"outlier_count": 1,
		"timestamp": "2026-04-28T12:00:00Z",
		"venues": [
			{"exchange": "binance",  "mid": 70010.0, "delta_pct":  0.014, "age_seconds":  2.5, "outlier": false},
			{"exchange": "coinbase", "mid": 70080.0, "delta_pct":  0.114, "age_seconds":  3.0, "outlier": false},
			{"exchange": "kraken",   "mid": 71500.0, "delta_pct":  2.143, "age_seconds":  4.0, "outlier": true}
		]
	}`)
	var s SanitySnapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Instrument != "BTCUSDT" {
		t.Errorf("instrument = %q", s.Instrument)
	}
	if s.Median != 70000.5 {
		t.Errorf("median = %v", s.Median)
	}
	if s.OutlierCount != 1 {
		t.Errorf("outlier_count = %d", s.OutlierCount)
	}
	if len(s.Venues) != 3 {
		t.Fatalf("venues len = %d, want 3", len(s.Venues))
	}
	if !s.Venues[2].Outlier {
		t.Error("kraken should be marked outlier")
	}
	if s.Venues[2].DeltaPct < 2.0 {
		t.Errorf("kraken delta_pct = %v, want > 2", s.Venues[2].DeltaPct)
	}
}

// TestRenderSanity_EmptyShowsWaiting confirms the empty-state hint
// renders, mirroring the desktop and phone surfaces.
func TestRenderSanity_EmptyShowsWaiting(t *testing.T) {
	out := RenderSanity(map[string]*SanitySnapshot{}, 100, 30)
	if !strings.Contains(out, "Waiting for sanity.prices") {
		t.Errorf("empty state should hint at sanity.prices subscription, got: %q", out)
	}
}

// TestRenderSanity_RendersInstrumentAndOutlier checks that an
// outlier flag survives into the rendered string. The exact ANSI
// styling is irrelevant — we only assert the substring is present.
func TestRenderSanity_RendersInstrumentAndOutlier(t *testing.T) {
	snaps := map[string]*SanitySnapshot{
		"BTCUSDT": {
			Instrument:   "BTCUSDT",
			Median:       70000,
			ThresholdPct: 0.5,
			VenueCount:   2,
			OutlierCount: 1,
			Timestamp:    "2026-04-28T12:00:00Z",
			LastUpdate:   time.Now(),
			Venues: []SanityVenue{
				{Exchange: "binance", Mid: 69990, DeltaPct: -0.01, AgeSeconds: 2, Outlier: false},
				{Exchange: "kraken", Mid: 72000, DeltaPct: 2.86, AgeSeconds: 4, Outlier: true},
			},
		},
	}
	out := RenderSanity(snaps, 120, 40)
	if !strings.Contains(out, "BTCUSDT") {
		t.Errorf("instrument missing from render")
	}
	if !strings.Contains(out, "OUTLIER") {
		t.Errorf("outlier badge missing")
	}
	if !strings.Contains(out, "FLAG") {
		t.Errorf("per-venue FLAG missing")
	}
}
