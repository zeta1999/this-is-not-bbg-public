package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// FeedStatusEntry represents a feed's health for display.
type FeedStatusEntry struct {
	Name       string
	State      string
	LastUpdate time.Time
	LatencyMs  float64
	ErrorCount uint64
}

// RenderMonitor renders the feed health monitoring panel.
func RenderMonitor(feeds []FeedStatusEntry, width, height int) string {
	header := amberStyle.Render("  FEED MONITOR")

	if len(feeds) == 0 {
		return header + "\n\n  " + dimStyle.Render("No feeds connected")
	}

	// Column headers.
	colHeader := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render(
		fmt.Sprintf("  %-20s  %-12s  %10s  %8s  %s", "SOURCE", "STATUS", "LATENCY", "ERRORS", "LAST UPDATE"),
	)

	var rows []string
	for _, f := range feeds {
		dot := statusDot(f.State)
		stateStr := lipgloss.NewStyle().Foreground(stateColor(f.State)).Render(
			fmt.Sprintf("%-12s", f.State),
		)

		latency := dimStyle.Render(fmt.Sprintf("%8.1fms", f.LatencyMs))

		errStr := dimStyle.Render(fmt.Sprintf("%8d", f.ErrorCount))
		if f.ErrorCount > 0 {
			errStr = redStyle.Render(fmt.Sprintf("%8d", f.ErrorCount))
		}

		ago := time.Since(f.LastUpdate)
		agoStr := dimStyle.Render(formatAgo(ago))

		rows = append(rows, fmt.Sprintf("  %s %-20s  %s  %s  %s  %s",
			dot, f.Name, stateStr, latency, errStr, agoStr))
	}

	return header + "\n" + colHeader + "\n" + strings.Join(rows, "\n")
}

func statusDot(state string) string {
	switch state {
	case "connected":
		return greenStyle.Render("●")
	case "reconnecting":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Render("●")
	case "stale":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Render("●")
	case "error":
		return redStyle.Render("●")
	default:
		return dimStyle.Render("○")
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case "connected":
		return lipgloss.Color("#00FF00")
	case "reconnecting", "stale":
		return lipgloss.Color("#FFFF00")
	case "error":
		return lipgloss.Color("#FF4444")
	default:
		return lipgloss.Color("#666666")
	}
}

func formatAgo(d time.Duration) string {
	if d < time.Second {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}
