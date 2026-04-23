package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// TradeAggData holds aggregated trade statistics for display.
type TradeAggData struct {
	Instrument string
	Exchange   string
	Timestamp  time.Time
	Count      int64
	Volume     float64
	BuyVolume  float64
	SellVolume float64
	VWAP       float64
	Open       float64
	High       float64
	Low        float64
	Close      float64
	Turnover   float64
	P25        float64
	P50        float64
	P75        float64
}

// TradeSnapEntry is a single trade in the snapshot.
type TradeSnapEntry struct {
	Price     float64
	Quantity  float64
	Side      string
	Timestamp time.Time
}

// TradeViewData holds all trade display state for one instrument.
type TradeViewData struct {
	Agg    *TradeAggData
	Trades []TradeSnapEntry
}

// RenderTrades renders the trade aggregate + recent trades view.
func RenderTrades(data map[string]*TradeViewData, keys []string, activeIdx int, width, height int) string {
	header := amberStyle.Render("  TRADES")

	if len(keys) == 0 || len(data) == 0 {
		return header + "\n\n  " + dimStyle.Render("Waiting for trade data...")
	}

	// Sidebar with instruments (showing exchange).
	var sidebar []string
	for i, key := range keys {
		label := key // "exchange/instrument"
		if d, ok := data[key]; ok && d.Agg != nil {
			label = fmt.Sprintf("%s/%s", d.Agg.Instrument, d.Agg.Exchange)
		}
		if i == activeIdx {
			sidebar = append(sidebar, amberStyle.Bold(true).Render(" ▸ "+label))
		} else {
			sidebar = append(sidebar, dimStyle.Render("   "+label))
		}
	}

	if activeIdx >= len(keys) {
		activeIdx = 0
	}
	active := data[keys[activeIdx]]
	if active == nil {
		return header + "\n\n  " + dimStyle.Render("No data for selected instrument")
	}

	var lines []string

	// Aggregate stats.
	if agg := active.Agg; agg != nil {
		lines = append(lines, amberStyle.Render(fmt.Sprintf("  %s/%s", agg.Instrument, agg.Exchange)))
		lines = append(lines, "")

		// VWAP + Price range.
		lines = append(lines, fmt.Sprintf("  %s  %s",
			dimStyle.Render("VWAP"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#00CCCC")).Render(fmt.Sprintf("%.2f", agg.VWAP)),
		))
		lines = append(lines, fmt.Sprintf("  %s  %s  %s  %s  %s  %s",
			dimStyle.Render("O"), fmtPrice(agg.Open),
			dimStyle.Render("H"), greenStyle.Render(fmt.Sprintf("%.2f", agg.High)),
			dimStyle.Render("L"), redStyle.Render(fmt.Sprintf("%.2f", agg.Low)),
		))

		// Volume.
		buyPct := 0.0
		if agg.Volume > 0 {
			buyPct = agg.BuyVolume / agg.Volume * 100
		}
		lines = append(lines, fmt.Sprintf("  %s %s  %s %s (%.0f%%)  %s %s (%.0f%%)",
			dimStyle.Render("Vol"), fmtVol(agg.Volume),
			dimStyle.Render("Buy"), greenStyle.Render(fmtVol(agg.BuyVolume)), buyPct,
			dimStyle.Render("Sell"), redStyle.Render(fmtVol(agg.SellVolume)), 100-buyPct,
		))

		// Quantiles.
		lines = append(lines, fmt.Sprintf("  %s  P25 %s  P50 %s  P75 %s",
			dimStyle.Render("Quantiles"),
			fmtPrice(agg.P25), fmtPrice(agg.P50), fmtPrice(agg.P75),
		))

		// Trade count + turnover.
		lines = append(lines, fmt.Sprintf("  %s %d  %s $%s",
			dimStyle.Render("Trades/s"), agg.Count,
			dimStyle.Render("Turnover"), fmtLargeNum(agg.Turnover),
		))
		lines = append(lines, "")
	}

	// Recent trades table.
	lines = append(lines, amberStyle.Render("  RECENT TRADES"))
	lines = append(lines, fmt.Sprintf("  %s  %s  %s  %s",
		dimStyle.Render(pad("Side", 6)),
		dimStyle.Render(pad("Price", 14)),
		dimStyle.Render(pad("Qty", 12)),
		dimStyle.Render("Time"),
	))

	for i := len(active.Trades) - 1; i >= 0; i-- {
		t := active.Trades[i]
		sideStyle := greenStyle
		sideStr := "BUY "
		if t.Side == "sell" {
			sideStyle = redStyle
			sideStr = "SELL"
		}
		ts := t.Timestamp.Format("15:04:05")
		lines = append(lines, fmt.Sprintf("  %s  %s  %s  %s",
			sideStyle.Render(pad(sideStr, 6)),
			fmtPrice(t.Price),
			pad(fmtVol(t.Quantity), 12),
			dimStyle.Render(ts),
		))
	}

	// Compose: sidebar on left, separator, content on right.
	sidebarStr := strings.Join(sidebar, "\n")
	contentStr := strings.Join(lines, "\n")

	available := height - 3
	sideW := 22
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("#333333")).Render("│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│\n│")
	if width > 100 {
		return header + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(sideW).MaxHeight(available).Render(sidebarStr),
			lipgloss.NewStyle().Width(1).MaxHeight(available).Render(sep),
			lipgloss.NewStyle().Width(width-sideW-6).MaxHeight(available).PaddingLeft(2).Render(contentStr),
		)
	}
	return header + "\n\n" + contentStr
}

func fmtPrice(v float64) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Render(fmt.Sprintf("%.2f", v))
}

func fmtVol(v float64) string {
	if v >= 1 {
		return fmt.Sprintf("%.4f", v)
	}
	return fmt.Sprintf("%.6f", v)
}

func fmtLargeNum(v float64) string {
	if v >= 1e9 {
		return fmt.Sprintf("%.2fB", v/1e9)
	}
	if v >= 1e6 {
		return fmt.Sprintf("%.2fM", v/1e6)
	}
	if v >= 1e3 {
		return fmt.Sprintf("%.1fK", v/1e3)
	}
	return fmt.Sprintf("%.2f", v)
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// greenStyle and redStyle are declared in ohlc.go
