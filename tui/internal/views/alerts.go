package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// AlertEntry represents an alert for display.
type AlertEntry struct {
	ID         string
	Type       string // "price_above", "price_below", "volume_spike", "keyword", "feed_down"
	Instrument string
	Condition  string
	Status     string // "active", "triggered", "dismissed"
	CreatedAt  time.Time
	TriggeredAt time.Time
}

// RenderAlerts renders the alerts panel.
func RenderAlerts(alerts []AlertEntry, width, height int) string {
	header := amberStyle.Render("  ALERTS")

	if len(alerts) == 0 {
		return header + "\n\n  " + dimStyle.Render("No alerts configured. Use ALERT SET <instrument> <condition> <value>")
	}

	// Column headers.
	colHeader := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render(
		fmt.Sprintf("  %-10s  %-12s  %-10s  %-20s  %-10s  %s", "ID", "TYPE", "STATUS", "CONDITION", "INSTRUMENT", "TIME"),
	)

	maxItems := height - 4
	if maxItems < 1 {
		maxItems = 1
	}
	if maxItems > len(alerts) {
		maxItems = len(alerts)
	}

	var rows []string
	for _, a := range alerts[:maxItems] {
		statusStyle := dimStyle
		switch a.Status {
		case "active":
			statusStyle = greenStyle
		case "triggered":
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Bold(true)
		case "dismissed":
			statusStyle = dimStyle
		}

		ts := a.CreatedAt
		if !a.TriggeredAt.IsZero() {
			ts = a.TriggeredAt
		}

		rows = append(rows, fmt.Sprintf("  %-10s  %-12s  %s  %-20s  %-10s  %s",
			a.ID,
			a.Type,
			statusStyle.Render(fmt.Sprintf("%-10s", a.Status)),
			a.Condition,
			a.Instrument,
			dimStyle.Render(formatAgo(time.Since(ts))),
		))
	}

	// Summary.
	active, triggered := 0, 0
	for _, a := range alerts {
		switch a.Status {
		case "active":
			active++
		case "triggered":
			triggered++
		}
	}
	summary := dimStyle.Render(fmt.Sprintf("  %d active, %d triggered, %d total", active, triggered, len(alerts)))

	return header + "\n" + colHeader + "\n" + strings.Join(rows, "\n") + "\n\n" + summary
}
