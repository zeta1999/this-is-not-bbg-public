package views

import (
	"testing"
	"time"
)

// TestAlignToNow_RightEdgeIsNow encodes the trader-sees-now
// invariant: the rightmost slot of the rendered window must
// correspond to the current bar (floor(now / TF)). Without this
// the chart silently "replayed the past" — the user's complaint
// from the 2026-04-28 ski.txt OHLC screenshot.
func TestAlignToNow_RightEdgeIsNow(t *testing.T) {
	step := time.Minute
	now := time.Now().UTC()
	rightStart := now.Truncate(step)

	// 5 contiguous 1m bars ending at "now".
	candles := []Candle{
		{Timestamp: rightStart.Add(-4 * step), Close: 100},
		{Timestamp: rightStart.Add(-3 * step), Close: 101},
		{Timestamp: rightStart.Add(-2 * step), Close: 102},
		{Timestamp: rightStart.Add(-1 * step), Close: 103},
		{Timestamp: rightStart, Close: 104},
	}

	slots := alignToNow(candles, "1m", 8)
	if len(slots) != 8 {
		t.Fatalf("want 8 slots, got %d", len(slots))
	}
	// Right edge must be the "now" bar.
	if slots[7] == nil || slots[7].Close != 104 {
		t.Errorf("right edge should be the now-bar (Close=104); got %+v", slots[7])
	}
	// Left of the data should be empty (no extrapolation).
	if slots[0] != nil || slots[1] != nil || slots[2] != nil {
		t.Errorf("expected empty slots before the oldest known bar; got %+v %+v %+v",
			slots[0], slots[1], slots[2])
	}
	// Continuous block: slots[3..7] are filled.
	for i := 3; i <= 7; i++ {
		if slots[i] == nil {
			t.Errorf("slot %d should be filled", i)
		}
	}
}

// TestAlignToNow_GapBetweenBackfillAndNow ensures that when
// realtime hasn't caught up after a gap, the chart leaves the gap
// visible at "now" instead of squashing the old bars into the
// rightmost slots.
func TestAlignToNow_GapBetweenBackfillAndNow(t *testing.T) {
	step := time.Minute
	now := time.Now().UTC()
	rightStart := now.Truncate(step)

	// Two old bars, then a 5-minute gap, then nothing live.
	candles := []Candle{
		{Timestamp: rightStart.Add(-7 * step), Close: 50},
		{Timestamp: rightStart.Add(-6 * step), Close: 51},
	}
	slots := alignToNow(candles, "1m", 10)
	if len(slots) != 10 {
		t.Fatalf("want 10 slots, got %d", len(slots))
	}
	// The two old bars should sit at slot 3 and 4 (10 - 1 - 6 = 3,
	// 10 - 1 - 7 = 2 ... let's compute directly: slot index for
	// rightStart-N*step is maxCandles - 1 - N).
	if slots[10-1-7] == nil || slots[10-1-7].Close != 50 {
		t.Errorf("old bar #1 misplaced; slots[%d] = %+v", 10-1-7, slots[10-1-7])
	}
	if slots[10-1-6] == nil || slots[10-1-6].Close != 51 {
		t.Errorf("old bar #2 misplaced; slots[%d] = %+v", 10-1-6, slots[10-1-6])
	}
	// The right edge must be empty — no realtime data has arrived.
	if slots[9] != nil {
		t.Errorf("right edge should be empty (no realtime data); got %+v", slots[9])
	}
}

// TestAlignToNow_DegradedNoTimestamp falls back to legacy
// array-tail rendering when no candle has a timestamp. This
// preserves the old behaviour for backfill chunks that haven't
// been re-fetched with timestamps yet — the trader sees something
// instead of an empty chart.
func TestAlignToNow_DegradedNoTimestamp(t *testing.T) {
	candles := []Candle{
		{Close: 1}, {Close: 2}, {Close: 3}, {Close: 4}, {Close: 5},
	}
	slots := alignToNow(candles, "1m", 5)
	for i, s := range slots {
		if s == nil || s.Close != float64(i+1) {
			t.Errorf("slot %d: want Close=%d, got %+v", i, i+1, s)
		}
	}
}

// TestRenderAlignedCandleChart_DojiVisible enforces shape S2:
// every non-nil slot must render at least one body row, even
// when Open == Close (a doji) or the body magnitude is below
// one row of vertical resolution. Without this the trader sees
// a wick-only marker where a real bar exists.
func TestRenderAlignedCandleChart_DojiVisible(t *testing.T) {
	doji := &Candle{Open: 100, High: 105, Low: 95, Close: 100}
	slots := []*Candle{doji}
	rows := renderAlignedCandleChart(slots, 30, 95, 10)
	bodyRows := 0
	for _, r := range rows {
		// Strip lipgloss escape sequences with a coarse heuristic:
		// the body chars are █ or ▓ (always rendered with a
		// foreground color). Containment check is enough.
		if containsAny(r, "█▓") {
			bodyRows++
		}
	}
	if bodyRows < 1 {
		t.Errorf("doji must render >= 1 body row; got %d", bodyRows)
	}
}

// TestRenderAlignedCandleChart_BullishProportion sanity-checks
// shape S3: a bullish candle with body filling roughly half the
// price range produces roughly half the chart height in body
// rows. Tolerant enough to absorb the half-row visibility
// expansion from S2 (±2 rows).
func TestRenderAlignedCandleChart_BullishProportion(t *testing.T) {
	// chart spans 100 → 200 (range 100), bar Open=120 Close=170,
	// body fills 50 of 100 = half. With chartHeight=20 expect ~10
	// body rows, tolerance ±2.
	bullish := &Candle{Open: 120, High: 175, Low: 115, Close: 170}
	slots := []*Candle{bullish}
	rows := renderAlignedCandleChart(slots, 20, 100, 100)
	bodyRows := 0
	for _, r := range rows {
		if containsAny(r, "█") { // bullish body char
			bodyRows++
		}
	}
	if bodyRows < 8 || bodyRows > 12 {
		t.Errorf("bullish body should fill ~10 of 20 rows; got %d", bodyRows)
	}
}

func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, sc := range s {
			if sc == c {
				return true
			}
		}
	}
	return false
}
