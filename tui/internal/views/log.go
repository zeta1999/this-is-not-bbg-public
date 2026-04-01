package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderLog renders the server log panel, showing the most recent lines that fit.
func RenderLog(lines []string, width, height int) string {
	header := amberStyle.Render("  SERVER LOG")
	countStr := dimStyle.Render(fmt.Sprintf("  %d lines", len(lines)))

	if len(lines) == 0 {
		return header + countStr + "\n\n  " + dimStyle.Render("No server logs yet")
	}

	// Show as many recent lines as fit.
	available := height - 3
	if available < 1 {
		available = 1
	}
	start := len(lines) - available
	if start < 0 {
		start = 0
	}
	visible := lines[start:]

	logStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00CC00"))

	var rendered []string
	for _, line := range visible {
		// Truncate to width.
		if lipgloss.Width(line) > width-2 {
			line = line[:width-4] + "..."
		}

		switch {
		case strings.Contains(line, "level=ERROR"):
			rendered = append(rendered, "  "+errStyle.Render(line))
		case strings.Contains(line, "level=WARN"):
			rendered = append(rendered, "  "+warnStyle.Render(line))
		case strings.Contains(line, "level=INFO"):
			rendered = append(rendered, "  "+infoStyle.Render(line))
		default:
			rendered = append(rendered, "  "+logStyle.Render(line))
		}
	}

	return header + countStr + "\n\n" + strings.Join(rendered, "\n")
}
