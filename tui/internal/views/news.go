package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// NewsItem represents a news entry for display.
type NewsItem struct {
	Title     string
	Source    string
	Timestamp time.Time
	Tickers   []string
	Body      string
	URL       string
}

// RenderNews renders the news panel as a list with cursor and optional filter.
func RenderNews(items []NewsItem, filter string, width, height int, selectedIdx int, showDetail bool) string {
	// Apply filter.
	filtered := items
	if filter != "" {
		filtered = filterNews(items, filter)
	}

	// Detail view — show full article for selected item.
	if showDetail && selectedIdx >= 0 && selectedIdx < len(filtered) {
		return renderNewsDetail(filtered[selectedIdx], width, height)
	}

	// List view.
	header := amberStyle.Render("  NEWS")
	if filter != "" {
		header += dimStyle.Render(fmt.Sprintf("  [%s]", filter))
		header += dimStyle.Render(fmt.Sprintf("  %d/%d", len(filtered), len(items)))
	} else {
		header += dimStyle.Render(fmt.Sprintf("  %d items", len(filtered)))
	}

	if len(filtered) == 0 {
		msg := "Waiting for news feed..."
		if filter != "" {
			msg = fmt.Sprintf("No news matching \"%s\"", filter)
		}
		return header + "\n\n  " + dimStyle.Render(msg)
	}

	maxItems := height - 3
	if maxItems < 1 {
		maxItems = 1
	}

	// Scroll window: keep selected item visible.
	scrollStart := 0
	if selectedIdx >= maxItems {
		scrollStart = selectedIdx - maxItems + 1
	}
	scrollEnd := scrollStart + maxItems
	if scrollEnd > len(filtered) {
		scrollEnd = len(filtered)
		scrollStart = scrollEnd - maxItems
		if scrollStart < 0 {
			scrollStart = 0
		}
	}

	var rows []string
	for i := scrollStart; i < scrollEnd; i++ {
		item := filtered[i]
		isSelected := i == selectedIdx

		source := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4488FF")).
			Bold(true).
			Render(fmt.Sprintf("%-14s", truncate(item.Source, 14)))

		ago := formatAgo(time.Since(item.Timestamp))
		timeStr := dimStyle.Render(fmt.Sprintf("%-6s", ago))

		title := item.Title
		for _, ticker := range item.Tickers {
			title = strings.ReplaceAll(title, ticker,
				amberStyle.Bold(true).Render(ticker))
		}

		maxTitle := width - 30
		if maxTitle > 0 && lipgloss.Width(title) > maxTitle {
			title = title[:maxTitle-3] + "..."
		}

		line := fmt.Sprintf("  %s %s %s", source, timeStr, title)

		if isSelected {
			cursor := amberStyle.Render("▸")
			line = cursor + line[1:]
		}

		rows = append(rows, line)
	}

	return header + "\n\n" + strings.Join(rows, "\n")
}

func renderNewsDetail(item NewsItem, width, height int) string {
	// Header.
	back := dimStyle.Render("  ESC: back to list")
	title := amberStyle.Bold(true).Render("  " + item.Title)
	source := lipgloss.NewStyle().Foreground(lipgloss.Color("#4488FF")).Bold(true).Render(item.Source)
	ago := dimStyle.Render(formatAgo(time.Since(item.Timestamp)))
	meta := fmt.Sprintf("  %s  %s", source, ago)

	var tickerLine string
	if len(item.Tickers) > 0 {
		var parts []string
		for _, t := range item.Tickers {
			parts = append(parts, amberStyle.Bold(true).Render(t))
		}
		tickerLine = "  " + strings.Join(parts, "  ")
	}

	urlLine := ""
	if item.URL != "" {
		urlLine = "  " + dimStyle.Render(item.URL)
	}

	divider := dimStyle.Render("  " + strings.Repeat("─", width-4))

	// Body text — word wrap.
	bodyLines := wrapText(item.Body, width-4)
	var body []string
	for _, l := range bodyLines {
		body = append(body, "  "+l)
	}

	var lines []string
	lines = append(lines, back)
	lines = append(lines, title)
	lines = append(lines, meta)
	if tickerLine != "" {
		lines = append(lines, tickerLine)
	}
	if urlLine != "" {
		lines = append(lines, urlLine)
	}
	lines = append(lines, divider)
	lines = append(lines, body...)

	// Truncate to height.
	if len(lines) > height {
		lines = lines[:height]
	}

	return strings.Join(lines, "\n")
}

// FilterNews returns items matching the filter string (case-insensitive).
func FilterNews(items []NewsItem, filter string) []NewsItem {
	return filterNews(items, filter)
}

func filterNews(items []NewsItem, filter string) []NewsItem {
	f := strings.ToUpper(filter)
	var out []NewsItem
	for _, item := range items {
		upper := strings.ToUpper(item.Title + " " + item.Source + " " + item.Body + " " + strings.Join(item.Tickers, " "))
		if strings.Contains(upper, f) {
			out = append(out, item)
		}
	}
	return out
}

func wrapText(text string, maxWidth int) []string {
	if maxWidth < 10 {
		maxWidth = 10
	}
	// Strip HTML tags (RSS bodies often have HTML).
	text = stripHTML(text)
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{dimStyle.Render("(no article body available)")}
	}

	words := strings.Fields(text)
	var lines []string
	current := ""
	for _, w := range words {
		if current == "" {
			current = w
		} else if len(current)+1+len(w) <= maxWidth {
			current += " " + w
		} else {
			lines = append(lines, current)
			current = w
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func stripHTML(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			out.WriteRune(' ')
		case !inTag:
			out.WriteRune(r)
		}
	}
	// Collapse whitespace.
	result := strings.Join(strings.Fields(out.String()), " ")
	return result
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
