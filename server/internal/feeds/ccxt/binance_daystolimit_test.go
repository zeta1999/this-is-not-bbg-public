package ccxt

import "testing"

// TestDaysToLimit_KnownTimeframes locks the candle-count math for
// each supported timeframe so a future regression in the dev.yaml
// timeframe list (15m / 30m / 4h / 1w added in 2026-04) can't
// silently fall through to the 1m default and blow past Binance's
// 1000-candle window.
func TestDaysToLimit_KnownTimeframes(t *testing.T) {
	cases := []struct {
		days int
		tf   string
		want int
	}{
		{1, "1m", 1440},
		{1, "5m", 288},
		{1, "15m", 96},
		{1, "30m", 48},
		{1, "1h", 24},
		{1, "4h", 6},
		{1, "1d", 1},
		{7, "1w", 1},
		{14, "1w", 2},
		{1, "1w", 1}, // round-up so a same-week request still returns one candle
	}
	for _, tc := range cases {
		got := daysToLimit(tc.days, tc.tf)
		if got != tc.want {
			t.Errorf("daysToLimit(%d, %q) = %d, want %d", tc.days, tc.tf, got, tc.want)
		}
	}
}

// TestDaysToLimit_UnknownFallsBackToMinute documents the default
// branch: an unrecognised timeframe is treated as 1m so the caller
// can still get a populated buffer rather than zero. Binance's
// fetchKlineWindow then clamps to the API's 1000-candle ceiling.
func TestDaysToLimit_UnknownFallsBackToMinute(t *testing.T) {
	got := daysToLimit(1, "XYZ")
	if got != 1440 {
		t.Errorf("unknown timeframe should fall back to 1m granularity, got %d", got)
	}
}
