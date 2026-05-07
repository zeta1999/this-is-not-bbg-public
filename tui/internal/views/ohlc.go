// Package views provides TUI panel renderers for market data.
package views

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	amberStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8C00"))
	greenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	redStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
)

// Candle represents a single OHLC candle for rendering. Timestamp
// is the bar's start time — required so the chart can anchor its
// right edge at "now" instead of "the array tail" (the array tail
// drifts whenever a backfill chunk arrives after realtime ticks).
type Candle struct {
	Timestamp              time.Time
	Open, High, Low, Close float64
	Volume                 float64
}

// OHLCSidebarEntry represents one instrument in the OHLC sidebar.
type OHLCSidebarEntry struct {
	Label      string
	Exchange   string
	LastPrice  float64
	LastUpdate time.Time
	Active     bool
}

const sidebarWidth = 28

// RenderOHLC renders an ASCII candlestick chart (kept for compatibility).
func RenderOHLC(candles []Candle, width, height int, instrument, exchange, timeframe string) string {
	return RenderOHLCWithSidebar(candles, width, height, instrument, exchange, timeframe, nil, nil, "")
}

// RenderOHLCWithSidebar renders the OHLC chart with an instrument
// sidebar on the right. loadingHint, when non-empty, is shown in
// amber next to the timeframe list so the user sees progressive
// DataRange fetches without the panel blocking.
func RenderOHLCWithSidebar(candles []Candle, width, height int, instrument, exchange, timeframe string, sidebar []OHLCSidebarEntry, availableTFs []string, loadingHint string) string {
	// Reserve space for sidebar if we have entries.
	chartWidth := width
	if len(sidebar) > 0 {
		chartWidth = width - sidebarWidth - 1
		if chartWidth < 40 {
			chartWidth = 40
		}
	}

	chart := renderOHLCChart(candles, chartWidth, height, instrument, exchange, timeframe, availableTFs, loadingHint)

	if len(sidebar) == 0 {
		return chart
	}

	sidebarStr := renderSidebar(sidebar, sidebarWidth, height)

	// Join chart and sidebar side by side.
	chartLines := strings.Split(chart, "\n")
	sidebarLines := strings.Split(sidebarStr, "\n")

	// Pad to same height.
	for len(chartLines) < height {
		chartLines = append(chartLines, "")
	}
	for len(sidebarLines) < height {
		sidebarLines = append(sidebarLines, "")
	}

	sep := dimStyle.Render("│")
	var lines []string
	for i := 0; i < height; i++ {
		cl := chartLines[i]
		clVisible := lipgloss.Width(cl)
		if clVisible < chartWidth {
			cl += strings.Repeat(" ", chartWidth-clVisible)
		}
		sl := ""
		if i < len(sidebarLines) {
			sl = sidebarLines[i]
		}
		lines = append(lines, cl+sep+sl)
	}

	return strings.Join(lines, "\n")
}

func renderOHLCChart(candles []Candle, width, height int, instrument, exchange, timeframe string, availableTFs []string, loadingHint string) string {
	// Timeframe indicator.
	tfStr := ""
	if len(availableTFs) > 0 {
		sort.Strings(availableTFs)
		var parts []string
		for _, tf := range availableTFs {
			if tf == timeframe {
				parts = append(parts, amberStyle.Bold(true).Render("["+tf+"]"))
			} else {
				parts = append(parts, dimStyle.Render(tf))
			}
		}
		tfStr = "  " + strings.Join(parts, " ")
	}
	if loadingHint != "" {
		tfStr += "  " + amberStyle.Render("⟳ "+loadingHint)
	}

	if len(candles) == 0 {
		header := amberStyle.Render(fmt.Sprintf("  %s  %s  %s  Waiting for data...", instrument, exchange, timeframe))
		if tfStr != "" {
			header += "\n" + tfStr
		}
		return header
	}

	last := candles[len(candles)-1]
	change := 0.0
	if len(candles) > 1 {
		prev := candles[len(candles)-2].Close
		if prev != 0 {
			change = (last.Close - prev) / prev * 100
		}
	}

	priceStyle := greenStyle
	changeStr := fmt.Sprintf("+%.2f%%", change)
	if change < 0 {
		priceStyle = redStyle
		changeStr = fmt.Sprintf("%.2f%%", change)
	}

	// Smart price formatting — currency-aware so non-USD instruments
	// (^N225/JPY, ^FTSE/GBP, ^GDAXI/EUR) don't get a misleading "$".
	priceStr := formatPriceCcy(last.Close, instrument, exchange)

	// Stale-data hint: if the rightmost known bar is more than one
	// timeframe behind "now", surface the gap explicitly so the
	// trader doesn't read a stale candle as "now". The chart's
	// right edge is anchored to now via alignToNow(); this just
	// puts the lag in words.
	staleStr := ""
	if step := tfDuration(timeframe); step > 0 && !last.Timestamp.IsZero() {
		lag := time.Since(last.Timestamp)
		if lag > 2*step {
			staleStr = "  " + redStyle.Render(fmt.Sprintf("⚠ last bar %s ago", compactDur(lag)))
		}
	}

	header := amberStyle.Render(fmt.Sprintf("  %s  %s  %s", instrument, exchange, timeframe))
	price := priceStyle.Bold(true).Render(fmt.Sprintf("  %s  %s", priceStr, changeStr))
	candleCount := dimStyle.Render(fmt.Sprintf("  %d candles", len(candles)))

	headerLine := header + tfStr
	priceLine := price + candleCount + staleStr

	chartHeight := height - 4
	if chartHeight < 3 {
		return headerLine + "\n" + priceLine
	}

	candleWidth := 3
	maxCandles := (width - 10) / candleWidth
	if maxCandles < 1 {
		maxCandles = 1
	}

	// Anchor the right edge to NOW and walk maxCandles slots back
	// in time, dropping each known candle into the slot whose start
	// time matches its Timestamp. Missing slots stay empty so the
	// trader sees the gap-since-now visually rather than an
	// equally-spaced backfill mirage. The "now" anchor is the
	// CURRENT bar's start time (floor(now / TF)) — the close of
	// that bar hasn't happened yet but the open/running high/low
	// from realtime ticks belong there.
	visible := alignToNow(candles, timeframe, maxCandles)

	minPrice, maxPrice := math.Inf(1), math.Inf(-1)
	for _, c := range visible {
		if c == nil {
			continue
		}
		if c.Low < minPrice {
			minPrice = c.Low
		}
		if c.High > maxPrice {
			maxPrice = c.High
		}
	}
	if math.IsInf(minPrice, 1) {
		// No real candles in the visible window — fall back to the
		// last known bar's price so the axis isn't NaN-blank while
		// backfill is loading.
		if len(candles) > 0 {
			minPrice = candles[len(candles)-1].Low
			maxPrice = candles[len(candles)-1].High
		} else {
			minPrice, maxPrice = 0, 1
		}
	}

	priceRange := maxPrice - minPrice
	if priceRange == 0 {
		priceRange = 1
	}

	chart := renderAlignedCandleChart(visible, chartHeight, minPrice, priceRange)

	var lines []string
	for row := 0; row < chartHeight; row++ {
		priceAtRow := maxPrice - (float64(row)/float64(chartHeight-1))*priceRange
		label := dimStyle.Render(fmt.Sprintf("%10s", formatPriceCcy(priceAtRow, instrument, exchange)))
		lines = append(lines, label+" "+chart[row])
	}

	return headerLine + "\n" + priceLine + "\n\n" + strings.Join(lines, "\n")
}

func formatPrice(p float64) string {
	return formatPriceCcy(p, "", "")
}

// PriceSym returns the right currency prefix for an instrument we
// know enough about to be honest. USD-quoted crypto pairs and
// known US indices keep "$"; non-USD indices get the right symbol;
// otherwise we drop the prefix rather than lie. Exported so the
// LOB / sanity / trades renderers can opt in incrementally.
func PriceSym(instrument, exchange string) string {
	if instrument == "" {
		return ""
	}
	upper := strings.ToUpper(instrument)
	if strings.HasSuffix(upper, "USDT") || strings.HasSuffix(upper, "USDC") ||
		strings.HasSuffix(upper, "USD") || strings.HasSuffix(upper, "-USD") ||
		strings.HasSuffix(upper, "/USD") {
		return "$"
	}
	if strings.EqualFold(exchange, "yahoo") {
		switch upper {
		case "^DJI", "^GSPC", "^IXIC", "^RUT", "^VIX", "^TNX":
			return "$"
		case "^FTSE":
			return "£"
		case "^GDAXI", "^FCHI", "^STOXX50E":
			return "€"
		case "^N225", "^TOPX":
			return "¥"
		case "^KS11":
			return "₩"
		}
	}
	return ""
}

// formatPriceCcy is formatPrice with a currency-aware prefix. Pass
// empty instrument when the caller doesn't know — the result drops
// the prefix instead of inventing a "$".
func formatPriceCcy(p float64, instrument, exchange string) string {
	sym := PriceSym(instrument, exchange)
	switch {
	case p >= 10000:
		return fmt.Sprintf("%s%.0f", sym, p)
	case p >= 100:
		return fmt.Sprintf("%s%.1f", sym, p)
	case p >= 1:
		return fmt.Sprintf("%s%.2f", sym, p)
	case p >= 0.01:
		return fmt.Sprintf("%s%.4f", sym, p)
	default:
		return fmt.Sprintf("%s%.6f", sym, p)
	}
}

func renderSidebar(entries []OHLCSidebarEntry, width, height int) string {
	title := amberStyle.Bold(true).Render(" INSTRUMENTS")
	divider := dimStyle.Render(strings.Repeat("─", width))

	var lines []string
	lines = append(lines, title)
	lines = append(lines, divider)

	now := time.Now()

	// Each entry takes 2 lines. Cap to fit in height.
	maxEntries := (height - 2) / 2
	if maxEntries < 1 {
		maxEntries = 1
	}

	// Find active index for scroll window.
	activeIdx := 0
	for i, e := range entries {
		if e.Active {
			activeIdx = i
			break
		}
	}

	// Scroll window: keep active entry visible.
	scrollStart := 0
	if activeIdx >= maxEntries {
		scrollStart = activeIdx - maxEntries/2
	}
	if scrollStart+maxEntries > len(entries) {
		scrollStart = len(entries) - maxEntries
	}
	if scrollStart < 0 {
		scrollStart = 0
	}
	scrollEnd := scrollStart + maxEntries
	if scrollEnd > len(entries) {
		scrollEnd = len(entries)
	}
	visible := entries[scrollStart:scrollEnd]

	for _, e := range visible {
		label := e.Label
		if e.Exchange != "" {
			label = fmt.Sprintf("%-8s %s", e.Label, dimStyle.Render(e.Exchange))
		}

		age := "—"
		if !e.LastUpdate.IsZero() {
			d := now.Sub(e.LastUpdate)
			switch {
			case d < time.Minute:
				age = fmt.Sprintf("%ds", int(d.Seconds()))
			case d < time.Hour:
				age = fmt.Sprintf("%dm", int(d.Minutes()))
			default:
				age = fmt.Sprintf("%dh", int(d.Hours()))
			}
		}

		priceStr := ""
		if e.LastPrice > 0 {
			priceStr = formatPriceCcy(e.LastPrice, e.Label, e.Exchange)
		}

		if e.Active {
			marker := amberStyle.Render("▸ ")
			nameStr := amberStyle.Bold(true).Render(label)
			info := fmt.Sprintf("%s %s", priceStr, dimStyle.Render(age))
			lines = append(lines, marker+nameStr)
			lines = append(lines, "  "+info)
		} else {
			nameStr := dimStyle.Render("  " + label)
			info := dimStyle.Render(fmt.Sprintf("  %s %s", priceStr, age))
			lines = append(lines, nameStr)
			lines = append(lines, info)
		}
	}

	if len(entries) > maxEntries {
		above := scrollStart
		below := len(entries) - scrollEnd
		var scrollHint string
		if above > 0 && below > 0 {
			scrollHint = fmt.Sprintf("  ↑%d ↓%d", above, below)
		} else if above > 0 {
			scrollHint = fmt.Sprintf("  ↑%d more", above)
		} else if below > 0 {
			scrollHint = fmt.Sprintf("  ↓%d more", below)
		}
		if scrollHint != "" {
			lines = append(lines, dimStyle.Render(scrollHint))
		}
	}

	for len(lines) < height {
		lines = append(lines, "")
	}

	return strings.Join(lines[:height], "\n")
}

// compactDur formats a Duration as a short human string ("3s",
// "12m", "2h"). Used in the chart header to show how far behind
// "now" the most recent bar is, so the trader sees the lag.
func compactDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

// tfDuration converts a timeframe string ("1m", "5m", "1h", "1d",
// "1w") to its duration. Unknown / "spot" returns 0 — caller falls
// back to even-spacing.
func tfDuration(tf string) time.Duration {
	switch tf {
	case "1m":
		return 1 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return 1 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "1d":
		return 24 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	}
	return 0
}

// alignToNow lays out maxCandles slots from oldest (index 0) to
// newest (index maxCandles-1, anchored to the current bar's start
// time). Each slot holds either a pointer to the candle whose
// Timestamp falls inside that slot or nil for "no data yet". When
// the timeframe isn't recognised or no candle has a timestamp,
// degrades to the legacy "right-N candles by array position"
// layout so the chart still renders.
//
// Formal invariants (verified by ohlc_test.go):
//
//	I1 (right=now):   slots[maxCandles-1] (when non-nil) has
//	                  Timestamp.Truncate(step) == now.Truncate(step).
//	I2 (slot honesty): for every k in [0..maxCandles): if
//	                   slots[k] != nil then
//	                   slots[k].Timestamp.Truncate(step) ==
//	                   leftmost.Add(k * step).
//	I3 (no phantom):  every input candle either occupies exactly
//	                  one slot or is out-of-window (dropped). No
//	                  duplication, no synthetic candles.
//	I4 (gap honesty): nil at slots[k] means "no candle exists for
//	                  bar k", NEVER "we have data but hid it".
func alignToNow(candles []Candle, timeframe string, maxCandles int) []*Candle {
	step := tfDuration(timeframe)
	out := make([]*Candle, maxCandles)

	// Degraded mode: no real timestamps anywhere — fall back to
	// array-tail. Trader sees something instead of an empty chart;
	// honesty is preserved because the indicator above will tell
	// them backfill is loading.
	hasTime := false
	for _, c := range candles {
		if !c.Timestamp.IsZero() {
			hasTime = true
			break
		}
	}
	if !hasTime || step == 0 {
		start := 0
		if len(candles) > maxCandles {
			start = len(candles) - maxCandles
		}
		for i := start; i < len(candles); i++ {
			c := candles[i]
			out[maxCandles-(len(candles)-i)] = &c
		}
		return out
	}

	// Right-edge slot is the bar that contains "now" — open bar
	// for the current TF. Each slot to the left is one TF earlier.
	now := time.Now().UTC()
	rightStart := now.Truncate(step)
	// Map slot index (0..maxCandles-1, 0 = oldest, last = now) to
	// a target start time.
	slotStart := func(slot int) time.Time {
		offsetFromRight := (maxCandles - 1 - slot)
		return rightStart.Add(-time.Duration(offsetFromRight) * step)
	}

	// Walk candles oldest→newest and place timestamped ones into
	// their time slots. Candles with zero Timestamp are DROPPED —
	// a candle without a timestamp is useless to a trader (you
	// can't reason about a price level without knowing when it
	// happened), so we refuse to render it. The upstream feed/
	// gateway must populate Timestamp; if it doesn't, the gap is
	// honest and the operator knows to fix the source.
	leftmost := slotStart(0)
	rightmost := rightStart
	dropped := 0
	zeroTs := 0
	beforeLeft := 0
	afterRight := 0
	placed := 0
	for i := range candles {
		ts := candles[i].Timestamp
		if ts.IsZero() {
			zeroTs++
			dropped++
			continue
		}
		bar := ts.Truncate(step)
		if bar.Before(leftmost) {
			beforeLeft++
			dropped++
			continue
		}
		if bar.After(rightmost) {
			afterRight++
			dropped++
			continue
		}
		slotsFromRight := int(rightmost.Sub(bar) / step)
		slot := maxCandles - 1 - slotsFromRight
		if slot >= 0 && slot < maxCandles {
			c := candles[i]
			out[slot] = &c
			placed++
		}
	}
	if alignDebug != nil {
		alignDebug("ALIGN tf=%s in=%d slots=%d placed=%d drop=%d (zero=%d before=%d after=%d) win=[%s..%s]",
			timeframe, len(candles), maxCandles, placed, dropped,
			zeroTs, beforeLeft, afterRight,
			leftmost.Format("01-02 15:04:05"),
			rightmost.Format("01-02 15:04:05"))
	}

	return out
}

// alignDebug, when non-nil, is called by alignToNow with one
// summary line per render. The TUI app wires this to the same
// /tmp/notbbg-ohlc.log file the realtime + history paths use, so
// the operator can correlate "in=10000 placed=2" against the
// chunk/realtime traces above. Lives in views (not app) because
// alignToNow is in this package; setter exposed below for the app
// package to call without a circular import.
var alignDebug func(format string, args ...any)

// SetAlignDebug installs (or clears, with nil) the trace hook used
// by alignToNow. Idempotent — safe to call from main / tests.
func SetAlignDebug(fn func(format string, args ...any)) {
	alignDebug = fn
}

// renderAlignedCandleChart renders candles from the slot-aligned
// `[]*Candle` produced by alignToNow. Empty slots render as blank
// columns so missing-data gaps are visible.
//
// Shape invariants (the trader-sees-now of vertical layout):
//
//	S1 (preservation):    rendering uses Open/High/Low/Close
//	                      verbatim — no smoothing, no synthesis.
//	S2 (visibility):      every non-nil slot renders >= 1 body row,
//	                      even a doji (O==C) or a body whose
//	                      magnitude is below one-row resolution.
//	                      Without this, sub-row bodies disappear
//	                      and the trader sees a wick-only marker
//	                      where a real bar exists.
//	S3 (proportionality): body row count ≈ |O-C|/priceRange *
//	                      chartHeight ± 1; wick rows ≈ (H-L)/
//	                      priceRange * chartHeight ± 1. Holds by
//	                      construction of the row→price mapping.
//	S4 (color):           bullish (C >= O) → green, bearish → red,
//	                      both body and wick.
func renderAlignedCandleChart(slots []*Candle, height int, minPrice, priceRange float64) []string {
	rows := make([]string, height)
	for i := range rows {
		rows[i] = ""
	}
	if height < 2 {
		return rows
	}
	// Half-row in price units. A body whose top/bottom both fall
	// inside the same row would otherwise be invisible due to the
	// `priceAtRow <= bodyTop && priceAtRow >= bodyBot` collapsing
	// to a single price-point match. Expanding the body bounds by
	// half a row in each direction guarantees S2 (visibility):
	// every body covers at least one row, with no over-extension
	// past the actual O-C range as seen by the human eye (a bar
	// occupying exactly one row IS exactly one row tall).
	rowSpan := priceRange / float64(height-1)
	halfRow := rowSpan / 2
	for _, c := range slots {
		if c == nil {
			for row := 0; row < height; row++ {
				rows[row] += "  " // blank slot
			}
			continue
		}
		bodyTop := math.Max(c.Open, c.Close)
		bodyBot := math.Min(c.Open, c.Close)
		// Expand to guarantee at least one row of body coverage.
		bodyTopRender := bodyTop + halfRow
		bodyBotRender := bodyBot - halfRow
		bullish := c.Close >= c.Open
		style := redStyle
		if bullish {
			style = greenStyle
		}
		for row := 0; row < height; row++ {
			priceAtRow := (minPrice + priceRange) - (float64(row)/float64(height-1))*priceRange
			inWick := priceAtRow <= c.High+halfRow && priceAtRow >= c.Low-halfRow
			inBody := priceAtRow <= bodyTopRender && priceAtRow >= bodyBotRender
			if inBody {
				if bullish {
					rows[row] += style.Render("█")
				} else {
					rows[row] += style.Render("▓")
				}
			} else if inWick {
				rows[row] += style.Render("│")
			} else {
				rows[row] += " "
			}
			rows[row] += " "
		}
	}
	return rows
}
