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

// NewsPreview holds the result of fetching + stripping the article
// behind a NewsItem.URL. Status is one of "loading", "ready",
// "error". Detail view renders Body when ready, ErrMsg when error,
// "loading…" otherwise.
type NewsPreview struct {
	Status string
	Body   string
	ErrMsg string
}

// RenderNews renders the news panel as a list with cursor and optional filter.
func RenderNews(items []NewsItem, filter string, width, height int, selectedIdx int, showDetail bool) string {
	return RenderNewsWithPreview(items, filter, width, height, selectedIdx, showDetail, nil, 0)
}

// RenderNewsWithPreview renders the news panel and, when in detail
// mode, embeds the fetched article preview for the selected item.
// previews is keyed by NewsItem.URL; missing/loading entries fall
// back to the RSS-supplied Body. scrollOff scrolls the preview's
// sliding window so long articles are reachable with j/k/PgUp/PgDn.
func RenderNewsWithPreview(items []NewsItem, filter string, width, height int, selectedIdx int, showDetail bool, previews map[string]*NewsPreview, scrollOff int) string {
	// Apply filter.
	filtered := items
	if filter != "" {
		filtered = filterNews(items, filter)
	}

	// Detail view — show full article for selected item.
	if showDetail && selectedIdx >= 0 && selectedIdx < len(filtered) {
		item := filtered[selectedIdx]
		var prev *NewsPreview
		if previews != nil && item.URL != "" {
			prev = previews[item.URL]
		}
		return renderNewsDetail(item, width, height, prev, scrollOff)
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

func renderNewsDetail(item NewsItem, width, height int, prev *NewsPreview, scrollOff int) string {
	// Header.
	back := dimStyle.Render("  ESC: back to list   j/k: scroll preview")
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

	// Pick the body source. Order: fetched preview body when ready,
	// fetch error message when failed, RSS-supplied summary as the
	// fallback. The "loading…" stub is a status hint above the
	// summary so the user sees the RSS body immediately and the
	// fetched article slides in when it lands.
	bodyText := item.Body
	statusLine := ""
	if prev != nil {
		switch prev.Status {
		case "ready":
			if strings.TrimSpace(prev.Body) != "" {
				bodyText = prev.Body
			}
		case "loading":
			statusLine = dimStyle.Render("  ⟳ fetching article…")
		case "error":
			statusLine = dimStyle.Render("  ⟳ fetch failed: " + prev.ErrMsg + " (showing RSS summary)")
		}
	}

	// Word-wrap then apply the sliding window offset.
	bodyLines := wrapText(bodyText, width-4)
	if scrollOff > 0 {
		if scrollOff >= len(bodyLines) {
			scrollOff = len(bodyLines) - 1
			if scrollOff < 0 {
				scrollOff = 0
			}
		}
		bodyLines = bodyLines[scrollOff:]
	}
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
	if statusLine != "" {
		lines = append(lines, statusLine)
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

	// Wrap each paragraph independently so the preview's paragraph
	// breaks survive — the previous flat-fields wrap collapsed
	// every newline and produced one giant block of text.
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		if strings.TrimSpace(para) == "" {
			lines = append(lines, "")
			continue
		}
		words := strings.Fields(para)
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
	}
	return lines
}

// StripHTML drops tags and a handful of common entities from an
// HTML document so the news detail view can render an article body
// fetched from a URL. Not a full HTML parser — Reader-mode quality
// is good enough for a sliding-window preview, and going through
// golang.org/x/net/html would pull a heavyweight dependency we
// don't want in the TUI vendor tree.
func StripHTML(s string) string { return stripHTML(s) }

func stripHTML(s string) string {
	// Strip whole script/style blocks first — leaving their text
	// content in produces noisy garbage rendered as words.
	for _, tag := range []string{"script", "style", "noscript", "head"} {
		s = stripBlock(s, "<"+tag, "</"+tag+">")
	}

	// Convert block-level closers to a newline so paragraph breaks
	// survive into the wrapped output.
	for _, br := range []string{"</p>", "</P>", "</div>", "</DIV>", "<br>", "<br/>", "<br />", "<BR>"} {
		s = strings.ReplaceAll(s, br, "\n")
	}

	var out strings.Builder
	out.Grow(len(s))
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

	// Decode the most common entities. Anything else passes through
	// — the preview is informational, not authoritative.
	text := out.String()
	for _, sub := range [][2]string{
		{"&amp;", "&"}, {"&lt;", "<"}, {"&gt;", ">"},
		{"&quot;", "\""}, {"&#39;", "'"}, {"&apos;", "'"},
		{"&nbsp;", " "}, {"&mdash;", "—"}, {"&ndash;", "–"},
		{"&hellip;", "…"}, {"&rsquo;", "'"}, {"&lsquo;", "'"},
		{"&rdquo;", "\""}, {"&ldquo;", "\""},
	} {
		text = strings.ReplaceAll(text, sub[0], sub[1])
	}

	// Collapse whitespace within each paragraph but preserve the
	// paragraph breaks the </p>/<br> conversion just laid down.
	paragraphs := strings.Split(text, "\n")
	for i, p := range paragraphs {
		paragraphs[i] = strings.Join(strings.Fields(p), " ")
	}
	// Drop empty paragraphs but keep one blank between non-empties.
	var cleaned []string
	prevEmpty := false
	for _, p := range paragraphs {
		if p == "" {
			if !prevEmpty && len(cleaned) > 0 {
				cleaned = append(cleaned, "")
			}
			prevEmpty = true
			continue
		}
		cleaned = append(cleaned, p)
		prevEmpty = false
	}
	return strings.Join(cleaned, "\n")
}

func stripBlock(s, open, close string) string {
	for {
		i := strings.Index(strings.ToLower(s), open)
		if i < 0 {
			return s
		}
		j := strings.Index(strings.ToLower(s[i:]), close)
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+j+len(close):]
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
