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

// Candle represents a single OHLC candle for rendering.
type Candle struct {
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

	// Smart price formatting.
	priceStr := formatPrice(last.Close)

	header := amberStyle.Render(fmt.Sprintf("  %s  %s  %s", instrument, exchange, timeframe))
	price := priceStyle.Bold(true).Render(fmt.Sprintf("  %s  %s", priceStr, changeStr))
	candleCount := dimStyle.Render(fmt.Sprintf("  %d candles", len(candles)))

	headerLine := header + tfStr
	priceLine := price + candleCount

	chartHeight := height - 4
	if chartHeight < 3 {
		return headerLine + "\n" + priceLine
	}

	candleWidth := 3
	maxCandles := (width - 10) / candleWidth
	if maxCandles < 1 {
		maxCandles = 1
	}
	visible := candles
	if len(visible) > maxCandles {
		visible = visible[len(visible)-maxCandles:]
	}

	minPrice, maxPrice := math.Inf(1), math.Inf(-1)
	for _, c := range visible {
		if c.Low < minPrice {
			minPrice = c.Low
		}
		if c.High > maxPrice {
			maxPrice = c.High
		}
	}

	priceRange := maxPrice - minPrice
	if priceRange == 0 {
		priceRange = 1
	}

	chart := renderCandleChart(visible, chartHeight, minPrice, priceRange)

	var lines []string
	for row := 0; row < chartHeight; row++ {
		priceAtRow := maxPrice - (float64(row)/float64(chartHeight-1))*priceRange
		label := dimStyle.Render(fmt.Sprintf("%10s", formatPrice(priceAtRow)))
		lines = append(lines, label+" "+chart[row])
	}

	return headerLine + "\n" + priceLine + "\n\n" + strings.Join(lines, "\n")
}

func formatPrice(p float64) string {
	switch {
	case p >= 10000:
		return fmt.Sprintf("$%.0f", p)
	case p >= 100:
		return fmt.Sprintf("$%.1f", p)
	case p >= 1:
		return fmt.Sprintf("$%.2f", p)
	case p >= 0.01:
		return fmt.Sprintf("$%.4f", p)
	default:
		return fmt.Sprintf("$%.6f", p)
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
			priceStr = formatPrice(e.LastPrice)
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

func renderCandleChart(candles []Candle, height int, minPrice, priceRange float64) []string {
	rows := make([]string, height)
	for i := range rows {
		rows[i] = ""
	}

	for _, c := range candles {
		bodyTop := math.Max(c.Open, c.Close)
		bodyBot := math.Min(c.Open, c.Close)

		bullish := c.Close >= c.Open
		style := redStyle
		if bullish {
			style = greenStyle
		}

		for row := 0; row < height; row++ {
			priceAtRow := (minPrice + priceRange) - (float64(row)/float64(height-1))*priceRange

			inWick := priceAtRow <= c.High && priceAtRow >= c.Low
			inBody := priceAtRow <= bodyTop && priceAtRow >= bodyBot

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
			rows[row] += " " // spacing between candles
		}
	}

	return rows
}
