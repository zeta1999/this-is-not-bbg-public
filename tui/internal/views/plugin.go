package views

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Legacy styled-line types (backward compat with v0 plugins).
// ---------------------------------------------------------------------------

// PluginStyledLine is a single styled line from a plugin screen.
type PluginStyledLine struct {
	Text  string `json:"text"`
	Style string `json:"style"`
}

// ---------------------------------------------------------------------------
// Cell grid types — mirrors pluginsdk/cells.go for the TUI renderer.
// ---------------------------------------------------------------------------

// PluginCellStyle controls text appearance.
type PluginCellStyle struct {
	Fg        string `json:"fg,omitempty"`
	Bg        string `json:"bg,omitempty"`
	Bold      bool   `json:"bold,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Underline bool   `json:"underline,omitempty"`
}

// PluginCell is a single cell in the grid.
type PluginCell struct {
	Address struct {
		Row uint32 `json:"row"`
		Col uint32 `json:"col"`
	} `json:"address"`
	Style *PluginCellStyle `json:"style,omitempty"`
	Type  string           `json:"type"`

	// Content fields (used depending on Type).
	Text        string  `json:"text,omitempty"`
	Label       string  `json:"label,omitempty"`
	Value       any     `json:"value,omitempty"`
	Precision   uint32  `json:"precision,omitempty"`
	Unit        string  `json:"unit,omitempty"`
	Delta       string  `json:"delta,omitempty"`
	Expression  string  `json:"expression,omitempty"`
	Options     []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"options,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	SearchHint  string `json:"search_hint,omitempty"`
	ComponentID string `json:"component_id,omitempty"`
	VisibleWhen string `json:"visible_when,omitempty"`
	ColSpan     uint32 `json:"col_span,omitempty"`
	RowSpan     uint32 `json:"row_span,omitempty"`
}

// PluginScreenData holds the state for a plugin screen tab.
type PluginScreenData struct {
	ID    string
	Label string
	Topic string

	// Legacy mode.
	Lines []PluginStyledLine

	// Cell grid mode.
	Cells    []PluginCell
	CellGrid bool // true if using cell grid

	// Cursor state for cell editing.
	CursorRow int  // -1 = no cursor
	CursorCol int
	Editing   bool   // true when actively editing a cell
	EditValue string // current edit buffer
	EnumIdx   int    // for enum cycling
}

// CellAt returns the cell at (row, col), or nil if not found.
func (s *PluginScreenData) CellAt(row, col int) *PluginCell {
	for i := range s.Cells {
		if int(s.Cells[i].Address.Row) == row && int(s.Cells[i].Address.Col) == col {
			return &s.Cells[i]
		}
	}
	return nil
}

// IsInputCell returns true if the cell type is an interactive input.
func IsInputCell(c *PluginCell) bool {
	if c == nil {
		return false
	}
	switch c.Type {
	case "input_decimal", "input_integer", "input_string", "input_enum", "input_selection", "input_script":
		return true
	}
	return false
}

// InputCellsSorted returns all input cells sorted by row then col.
func (s *PluginScreenData) InputCellsSorted() []PluginCell {
	var inputs []PluginCell
	values := buildCellValueMap(s.Cells)
	for _, c := range s.Cells {
		if !IsInputCell(&c) {
			continue
		}
		if c.VisibleWhen != "" && !evaluateVisibleWhen(c.VisibleWhen, values) {
			continue
		}
		inputs = append(inputs, c)
	}
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Address.Row != inputs[j].Address.Row {
			return inputs[i].Address.Row < inputs[j].Address.Row
		}
		return inputs[i].Address.Col < inputs[j].Address.Col
	})
	return inputs
}

// NextInputCell finds the next input cell from (row, col) in the given direction.
// dir: 1 = forward (down/right), -1 = backward (up/left). Wraps around.
func (s *PluginScreenData) NextInputCell(dir int) (int, int, bool) {
	inputs := s.InputCellsSorted()
	if len(inputs) == 0 {
		return -1, -1, false
	}

	// Find current index.
	curIdx := -1
	for i, c := range inputs {
		if int(c.Address.Row) == s.CursorRow && int(c.Address.Col) == s.CursorCol {
			curIdx = i
			break
		}
	}

	// Move.
	nextIdx := curIdx + dir
	if nextIdx < 0 {
		nextIdx = len(inputs) - 1
	} else if nextIdx >= len(inputs) {
		nextIdx = 0
	}

	c := inputs[nextIdx]
	return int(c.Address.Row), int(c.Address.Col), true
}

// SelectFirstInput sets the cursor to the first input cell.
func (s *PluginScreenData) SelectFirstInput() {
	inputs := s.InputCellsSorted()
	if len(inputs) > 0 {
		s.CursorRow = int(inputs[0].Address.Row)
		s.CursorCol = int(inputs[0].Address.Col)
	}
}

var (
	pluginGreenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	pluginRedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444"))
	pluginWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))
	pluginNormStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	pluginCyanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00CCCC"))
	cellCursorStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#333333")).Foreground(lipgloss.Color("#FF8C00"))
	cellEditingStyle  = lipgloss.NewStyle().Background(lipgloss.Color("#1a1a4a")).Foreground(lipgloss.Color("#FFFFFF"))
)

// RenderPluginScreen renders a plugin screen (auto-detects legacy vs cell grid).
func RenderPluginScreen(screen PluginScreenData, width, height int) string {
	if screen.CellGrid && len(screen.Cells) > 0 {
		return renderCellGrid(screen, width, height)
	}
	return renderLegacyLines(screen, width, height)
}

// ---------------------------------------------------------------------------
// Legacy renderer (StyledLine).
// ---------------------------------------------------------------------------

func renderLegacyLines(screen PluginScreenData, width, height int) string {
	header := amberStyle.Render("  " + screen.Label)

	if len(screen.Lines) == 0 {
		return header + "\n\n  " + dimStyle.Render(fmt.Sprintf("Plugin screen %q — waiting for data...", screen.ID))
	}

	available := height - 3
	if available < 1 {
		available = 1
	}
	start := 0
	if len(screen.Lines) > available {
		start = len(screen.Lines) - available
	}
	visible := screen.Lines[start:]

	var rendered []string
	for _, line := range visible {
		text := line.Text
		if lipgloss.Width(text) > width-2 {
			text = text[:width-4] + "..."
		}

		var styled string
		switch line.Style {
		case "header":
			styled = amberStyle.Bold(true).Render(text)
		case "green":
			styled = pluginGreenStyle.Render(text)
		case "red":
			styled = pluginRedStyle.Render(text)
		case "warn":
			styled = pluginWarnStyle.Render(text)
		case "dim":
			styled = dimStyle.Render(text)
		default:
			styled = pluginNormStyle.Render(text)
		}
		rendered = append(rendered, styled)
	}

	return header + "\n\n" + strings.Join(rendered, "\n")
}

// ---------------------------------------------------------------------------
// Cell grid renderer.
// ---------------------------------------------------------------------------

const cellWidth = 28 // characters per cell column

func renderCellGrid(screen PluginScreenData, width, height int) string {
	header := amberStyle.Render("  " + screen.Label)

	if len(screen.Cells) == 0 {
		return header + "\n\n  " + dimStyle.Render("No cells")
	}

	// Build visibility map from enum/input values for visible_when evaluation.
	cellValues := buildCellValueMap(screen.Cells)

	// Collect visible cells, organized by row.
	type rowEntry struct {
		col  uint32
		cell PluginCell
	}
	rows := make(map[uint32][]rowEntry)
	var maxRow uint32

	for _, c := range screen.Cells {
		if c.VisibleWhen != "" && !evaluateVisibleWhen(c.VisibleWhen, cellValues) {
			continue
		}
		rows[c.Address.Row] = append(rows[c.Address.Row], rowEntry{col: c.Address.Col, cell: c})
		if c.Address.Row > maxRow {
			maxRow = c.Address.Row
		}
	}

	// Sort rows by row number, cells within row by col.
	rowNums := make([]uint32, 0, len(rows))
	for r := range rows {
		rowNums = append(rowNums, r)
	}
	sort.Slice(rowNums, func(i, j int) bool { return rowNums[i] < rowNums[j] })

	// Determine how many columns fit in the terminal width.
	maxCols := uint32((width - 2) / cellWidth)
	if maxCols < 2 {
		maxCols = 2
	}

	var rendered []string
	available := height - 3
	for _, rowNum := range rowNums {
		if len(rendered) >= available {
			break
		}
		entries := rows[rowNum]
		sort.Slice(entries, func(i, j int) bool { return entries[i].col < entries[j].col })

		// Render each cell in the row into fixed-width columns.
		cols := make([]string, maxCols)
		for i := range cols {
			cols[i] = strings.Repeat(" ", cellWidth)
		}

		for _, entry := range entries {
			if entry.col >= maxCols {
				continue
			}
			isCursor := screen.CursorRow == int(entry.cell.Address.Row) && screen.CursorCol == int(entry.cell.Address.Col)
			var text string
			if isCursor && screen.Editing {
				text = cellEditingStyle.Render("[" + screen.EditValue + "│]")
			} else {
				text = renderCell(entry.cell)
				if isCursor {
					text = cellCursorStyle.Render("▸") + text
				}
			}
			span := entry.cell.ColSpan
			if span == 0 {
				span = 1
			}
			availWidth := int(span) * cellWidth
			if lipgloss.Width(text) > availWidth {
				text = text[:availWidth-1] + "…"
			} else {
				text = text + strings.Repeat(" ", availWidth-lipgloss.Width(text))
			}
			cols[entry.col] = text
			// Clear spanned columns.
			for s := uint32(1); s < span && entry.col+s < maxCols; s++ {
				cols[entry.col+s] = ""
			}
		}

		// Join non-empty columns.
		var line strings.Builder
		for _, c := range cols {
			if c != "" {
				line.WriteString(c)
			}
		}
		rendered = append(rendered, "  "+line.String())
	}

	return header + "\n\n" + strings.Join(rendered, "\n")
}

// renderCell returns a styled string for a single cell.
func renderCell(c PluginCell) string {
	style := cellStyle(c.Style)

	switch c.Type {
	case "text":
		return style.Render(c.Text)

	case "input_decimal":
		val := cellFloat(c.Value)
		prec := c.Precision
		if prec == 0 {
			prec = 2
		}
		label := c.Label
		if label != "" {
			label += " "
		}
		return dimStyle.Render(label) + style.Render(fmt.Sprintf("[%.*f]", prec, val))

	case "input_integer":
		val := cellFloat(c.Value)
		label := c.Label
		if label != "" {
			label += " "
		}
		return dimStyle.Render(label) + style.Render(fmt.Sprintf("[%d]", int64(val)))

	case "input_string":
		val, ok := c.Value.(string)
		if !ok {
			val = ""
		}
		if val == "" {
			val = c.Placeholder
		}
		label := c.Label
		if label != "" {
			label += " "
		}
		return dimStyle.Render(label) + style.Render("["+val+"]")

	case "input_enum":
		val := fmt.Sprintf("%v", c.Value)
		label := c.Label
		if label != "" {
			label += " "
		}
		return dimStyle.Render(label) + pluginCyanStyle.Render("["+val+"▼]")

	case "input_selection":
		val := fmt.Sprintf("%v", c.Value)
		label := c.Label
		if label != "" {
			label += " "
		}
		return dimStyle.Render(label) + pluginCyanStyle.Render("["+val+"]")

	case "number":
		val := cellFloat(c.Value)
		prec := c.Precision
		if prec == 0 {
			prec = 2
		}
		label := c.Label
		if label != "" {
			label += " "
		}
		numStr := fmt.Sprintf("%.*f", prec, val)
		if c.Unit != "" {
			numStr += c.Unit
		}
		// Delta indicator.
		switch c.Delta {
		case "up":
			numStr = "▲" + numStr
		case "down":
			numStr = "▼" + numStr
		}
		// Color by value sign or explicit style.
		numStyle := style
		if c.Style == nil {
			if val > 0 {
				numStyle = pluginGreenStyle
			} else if val < 0 {
				numStyle = pluginRedStyle
			}
		}
		return dimStyle.Render(label) + numStyle.Render(numStr)

	case "formula":
		val := cellFloat(c.Value)
		prec := c.Precision
		if prec == 0 {
			prec = 4
		}
		label := c.Label
		if label != "" {
			label += " "
		}
		return dimStyle.Render(label) + pluginGreenStyle.Render(fmt.Sprintf("%.*f", prec, val))

	case "input_script":
		// Show truncated preview of script content.
		val, _ := c.Value.(string)
		if val == "" {
			val = "(empty script)"
		}
		preview := strings.ReplaceAll(val, "\n", " ")
		if len(preview) > 40 {
			preview = preview[:37] + "..."
		}
		label := c.Label
		if label != "" {
			label += " "
		}
		return dimStyle.Render(label) + pluginCyanStyle.Render("["+preview+" ✎]")

	case "component":
		if c.ComponentID == "progress" {
			progress := cellFloat(c.Value)
			if progress < 0 { progress = 0 }
			if progress > 1 { progress = 1 }
			barWidth := 20
			filled := int(progress * float64(barWidth))
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			pct := fmt.Sprintf("%.0f%%", progress*100)
			label := c.Label
			if label != "" { label += " " }
			status := c.Text
			if status != "" { status = " " + status }
			return dimStyle.Render(label) + pluginCyanStyle.Render("["+bar+"] ") + pluginNormStyle.Render(pct+status)
		}
		return dimStyle.Render("[" + c.ComponentID + "]")

	default:
		return c.Text
	}
}

// cellStyle converts a PluginCellStyle to a lipgloss style.
func cellStyle(s *PluginCellStyle) lipgloss.Style {
	base := lipgloss.NewStyle()
	if s == nil {
		return pluginNormStyle
	}
	if s.Fg != "" {
		base = base.Foreground(pluginColor(s.Fg))
	}
	if s.Bg != "" {
		base = base.Background(pluginColor(s.Bg))
	}
	if s.Bold {
		base = base.Bold(true)
	}
	if s.Italic {
		base = base.Italic(true)
	}
	if s.Underline {
		base = base.Underline(true)
	}
	return base
}

// pluginColor maps named colors and hex to lipgloss.Color.
func pluginColor(c string) lipgloss.Color {
	switch c {
	case "green":
		return lipgloss.Color("#00FF00")
	case "red":
		return lipgloss.Color("#FF4444")
	case "cyan":
		return lipgloss.Color("#00CCCC")
	case "yellow", "warn":
		return lipgloss.Color("#FFAA00")
	case "dim", "gray":
		return lipgloss.Color("#888888")
	case "white":
		return lipgloss.Color("#FFFFFF")
	default:
		return lipgloss.Color(c) // hex passthrough
	}
}

// cellFloat extracts a float64 from an any value (JSON numbers decode as float64).
func cellFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		if math.IsNaN(val) {
			return 0
		}
		return val
	case int64:
		return float64(val)
	case int:
		return float64(val)
	default:
		return 0
	}
}

// buildCellValueMap extracts current values from input/enum cells for visibility evaluation.
func buildCellValueMap(cells []PluginCell) map[string]string {
	m := make(map[string]string)
	for _, c := range cells {
		key := fmt.Sprintf("R%dC%d", c.Address.Row, c.Address.Col)
		switch c.Type {
		case "input_enum", "input_string", "input_selection":
			m[key] = fmt.Sprintf("%v", c.Value)
		case "input_decimal", "input_integer":
			m[key] = fmt.Sprintf("%v", c.Value)
		}
	}
	return m
}

// evaluateVisibleWhen checks a simple "R0C3=Barrier" condition.
func evaluateVisibleWhen(cond string, values map[string]string) bool {
	parts := strings.SplitN(cond, "=", 2)
	if len(parts) != 2 {
		return true // malformed → show
	}
	cellRef := strings.TrimSpace(parts[0])
	expected := strings.TrimSpace(parts[1])
	actual, ok := values[cellRef]
	if !ok {
		return false // referenced cell not found → hide
	}
	return actual == expected
}
