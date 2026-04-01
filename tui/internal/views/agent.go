package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderAgent renders the agent panel — a scrollable terminal-like output.
// scrollOff = 0 means auto-scroll to bottom; >0 means lines from bottom.
func RenderAgent(lines []string, width, height int, scrollOff int) string {
	header := amberStyle.Render("  AGENT")
	countStr := dimStyle.Render(fmt.Sprintf("  %d lines", len(lines)))

	if len(lines) == 0 {
		return header + countStr + "\n\n  " + dimStyle.Render("No agent running. Use /agent <skill> to start one.")
	}

	available := height - 3
	if available < 1 {
		available = 1
	}

	// Scroll: offset from bottom.
	end := len(lines) - scrollOff
	if end < 0 {
		end = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	start := end - available
	if start < 0 {
		start = 0
	}
	visible := lines[start:end]

	if scrollOff > 0 {
		countStr = dimStyle.Render(fmt.Sprintf("  %d lines  ↑%d", len(lines), scrollOff))
	}

	outputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
	artifactStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8C00")).Bold(true)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444"))
	suggestionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))

	var rendered []string
	for _, line := range visible {
		if lipgloss.Width(line) > width-4 {
			line = line[:width-6] + "..."
		}

		switch {
		case strings.HasPrefix(line, "NOTBBG:"):
			rendered = append(rendered, "  "+artifactStyle.Render(line))
		case strings.Contains(line, "ERROR") || strings.Contains(line, "error"):
			rendered = append(rendered, "  "+errorStyle.Render(line))
		case strings.Contains(line, "suggestion") || strings.Contains(line, "SUGGESTION"):
			rendered = append(rendered, "  "+suggestionStyle.Render(line))
		default:
			rendered = append(rendered, "  "+outputStyle.Render(line))
		}
	}

	return header + countStr + "\n\n" + strings.Join(rendered, "\n")
}
