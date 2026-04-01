package views

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// LOBLevel represents a price level for rendering.
type LOBLevel struct {
	Price    float64
	Quantity float64
}

// LOBData holds the data needed to render the order book.
type LOBData struct {
	Instrument string
	Exchange   string
	Bids       []LOBLevel
	Asks       []LOBLevel
	LastUpdate time.Time
}

// LOBSidebarEntry represents one instrument in the LOB sidebar.
type LOBSidebarEntry struct {
	Label      string
	Exchange   string
	Spread     float64
	LastUpdate time.Time
	Active     bool
}

// RenderLOB renders a two-column order book display (compatibility wrapper).
func RenderLOB(data *LOBData, width, height int, sidebar []LOBSidebarEntry) string {
	if data == nil {
		return amberStyle.Render("  Waiting for LOB data...")
	}

	// Reserve space for sidebar.
	bookWidth := width
	if len(sidebar) > 0 {
		bookWidth = width - sidebarWidth - 1
		if bookWidth < 50 {
			bookWidth = 50
		}
	}

	book := renderLOBBook(data, bookWidth, height)

	if len(sidebar) == 0 {
		return book
	}

	sidebarStr := renderLOBSidebar(sidebar, sidebarWidth, height)

	bookLines := strings.Split(book, "\n")
	sidebarLines := strings.Split(sidebarStr, "\n")
	for len(bookLines) < height {
		bookLines = append(bookLines, "")
	}
	for len(sidebarLines) < height {
		sidebarLines = append(sidebarLines, "")
	}

	sep := dimStyle.Render("│")
	var lines []string
	for i := 0; i < height; i++ {
		cl := bookLines[i]
		clW := lipgloss.Width(cl)
		if clW < bookWidth {
			cl += strings.Repeat(" ", bookWidth-clW)
		}
		sl := ""
		if i < len(sidebarLines) {
			sl = sidebarLines[i]
		}
		lines = append(lines, cl+sep+sl)
	}
	return strings.Join(lines, "\n")
}

func renderLOBBook(data *LOBData, width, height int) string {
	if len(data.Bids) == 0 && len(data.Asks) == 0 {
		return amberStyle.Render(fmt.Sprintf("  %s  %s  Waiting for LOB data...", data.Instrument, data.Exchange))
	}

	spread := 0.0
	if len(data.Bids) > 0 && len(data.Asks) > 0 {
		spread = data.Asks[0].Price - data.Bids[0].Price
	}
	spreadStr := fmt.Sprintf("%.2f", spread)

	header := amberStyle.Render(
		fmt.Sprintf("  %s  %s  Spread: $%s", data.Instrument, data.Exchange, spreadStr),
	)

	// Fixed column layout: "  PRICE     QTY  BAR"
	// Each side gets half the width minus gap.
	colWidth := (width - 4) / 2
	if colWidth < 28 {
		colWidth = 28
	}
	barWidth := colWidth - 24
	if barWidth < 0 {
		barWidth = 0
	}

	bidHdr := fmt.Sprintf("  %-12s %9s", "PRICE", "QTY")
	askHdr := fmt.Sprintf("  %-12s %9s", "PRICE", "QTY")
	colHeaders := greenStyle.Bold(true).Render(padRight(bidHdr, colWidth)) + "  " + redStyle.Bold(true).Render(padRight(askHdr, colWidth))

	maxQty := 0.0
	for _, b := range data.Bids {
		if b.Quantity > maxQty {
			maxQty = b.Quantity
		}
	}
	for _, a := range data.Asks {
		if a.Quantity > maxQty {
			maxQty = a.Quantity
		}
	}
	if maxQty == 0 {
		maxQty = 1
	}

	levels := height - 4
	if levels < 1 {
		levels = 1
	}

	var rows []string
	for i := 0; i < levels; i++ {
		bidStr := strings.Repeat(" ", colWidth)
		askStr := strings.Repeat(" ", colWidth)

		if i < len(data.Bids) {
			b := data.Bids[i]
			bar := renderBar(b.Quantity, maxQty, barWidth)
			bidStr = greenStyle.Render(fmt.Sprintf("  %12.2f %9.4f ", b.Price, b.Quantity)) + greenStyle.Render(bar)
		}

		if i < len(data.Asks) {
			a := data.Asks[i]
			bar := renderBar(a.Quantity, maxQty, barWidth)
			askStr = redStyle.Render(fmt.Sprintf("  %12.2f %9.4f ", a.Price, a.Quantity)) + redStyle.Render(bar)
		}

		// Pad bid side to fixed width so ask column aligns.
		bidVisible := lipgloss.Width(bidStr)
		if bidVisible < colWidth {
			bidStr += strings.Repeat(" ", colWidth-bidVisible)
		}

		rows = append(rows, bidStr+"  "+askStr)
	}

	return header + "\n" + colHeaders + "\n" + strings.Join(rows, "\n")
}

func renderLOBSidebar(entries []LOBSidebarEntry, width, height int) string {
	title := amberStyle.Bold(true).Render(" ORDER BOOKS")
	divider := dimStyle.Render(strings.Repeat("─", width))

	var lines []string
	lines = append(lines, title)
	lines = append(lines, divider)

	now := time.Now()
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

		spreadStr := ""
		if e.Spread > 0 {
			spreadStr = "spread " + formatPrice(e.Spread)
		}

		if e.Active {
			marker := amberStyle.Render("▸ ")
			nameStr := amberStyle.Bold(true).Render(label)
			info := fmt.Sprintf("%s %s", spreadStr, dimStyle.Render(age))
			lines = append(lines, marker+nameStr)
			lines = append(lines, "  "+info)
		} else {
			lines = append(lines, dimStyle.Render("  "+label))
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  %s %s", spreadStr, age)))
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

func renderBar(qty, maxQty float64, maxWidth int) string {
	if maxWidth < 1 {
		maxWidth = 1
	}
	barLen := int(math.Round(qty / maxQty * float64(maxWidth)))
	if barLen > maxWidth {
		barLen = maxWidth
	}
	return strings.Repeat("█", barLen)
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}
