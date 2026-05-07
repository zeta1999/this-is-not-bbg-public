package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SettingRow is one displayed entry in the SETTINGS panel. Section
// groups consecutive rows under a header; Key / Value are the
// labelled value. Keep Value stringified by the caller so the view
// can't leak typed state.
type SettingRow struct {
	Section string
	Key     string
	Value   string
}

// RenderSettings renders the SETTINGS panel as a table grouped by
// Section header. Read-only; editing lives in the CLI and config
// file. Footer reminds the user where to edit.
func RenderSettings(rows []SettingRow, width, height int) string {
	header := amberStyle.Render("  SETTINGS")

	if len(rows) == 0 {
		return header + "\n\n  " + dimStyle.Render("No settings to show.")
	}

	// Column widths.
	keyWidth := 0
	for _, r := range rows {
		if n := len(r.Key); n > keyWidth {
			keyWidth = n
		}
	}
	if keyWidth < 18 {
		keyWidth = 18
	}

	currentSection := ""
	var lines []string
	for _, r := range rows {
		if r.Section != currentSection {
			if currentSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, amberStyle.Render("  ["+r.Section+"]"))
			currentSection = r.Section
		}
		key := lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA")).Render(
			fmt.Sprintf("%-*s", keyWidth, r.Key),
		)
		val := r.Value
		// Clip overlong values so the row doesn't wrap in narrow terms.
		maxValWidth := width - keyWidth - 6
		if maxValWidth > 0 && lipgloss.Width(val) > maxValWidth {
			val = val[:maxValWidth-1] + "…"
		}
		lines = append(lines, "    "+key+"  "+val)
	}

	footer := dimStyle.Render(
		"  Read-only view. Edit ~/.config/notbbg/config.yaml or run `notbbg pair-collector …` to change.",
	)
	body := strings.Join(lines, "\n")
	return header + "\n\n" + body + "\n\n" + footer
}
