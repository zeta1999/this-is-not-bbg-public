package pluginsdk

import "fmt"

// ---------------------------------------------------------------------------
// Cell grid types — mirrors proto/notbbg/v1/plugin.proto as JSON-friendly Go.
// Plugins use these types to build screens; the SDK serializes them to JSON
// over stdin/stdout.
// ---------------------------------------------------------------------------

// CellStyle controls text appearance.
type CellStyle struct {
	Fg        string `json:"fg,omitempty"`        // "green", "red", "#FF4444"
	Bg        string `json:"bg,omitempty"`        // background color
	Bold      bool   `json:"bold,omitempty"`      //
	Italic    bool   `json:"italic,omitempty"`    //
	Underline bool   `json:"underline,omitempty"` //
}

// CellAddress identifies a cell position.
type CellAddress struct {
	Row uint32 `json:"row"`
	Col uint32 `json:"col"`
}

// EnumOption is a value in an enum dropdown.
type EnumOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Cell is the fundamental unit of the screen grid.
type Cell struct {
	Address CellAddress `json:"address"`
	Style   *CellStyle  `json:"style,omitempty"`
	Type    string      `json:"type"` // "text", "input_decimal", "input_integer", "input_string", "input_enum", "input_selection", "input_script", "number", "formula", "component", "chart", "table", "image"

	// Text content (type=text)
	Text string `json:"text,omitempty"`

	// Input fields (type=input_*)
	Label       string       `json:"label,omitempty"`
	Value       any          `json:"value,omitempty"` // current value: float64, int64, string
	Min         *float64     `json:"min,omitempty"`
	Max         *float64     `json:"max,omitempty"`
	Step        *int64       `json:"step,omitempty"`
	Precision   uint32       `json:"precision,omitempty"`
	Placeholder string       `json:"placeholder,omitempty"`
	Options     []EnumOption `json:"options,omitempty"` // for enum input
	SearchHint  string       `json:"search_hint,omitempty"`

	// Number output (type=number)
	Unit  string `json:"unit,omitempty"`  // "%", "ms", "$"
	Delta string `json:"delta,omitempty"` // "up", "down", ""

	// Formula (type=formula)
	Expression string `json:"expression,omitempty"`

	// Component (type=component)
	ComponentID string `json:"component_id,omitempty"`

	// Layout
	VisibleWhen string `json:"visible_when,omitempty"` // "R0C3=Barrier"
	ColSpan     uint32 `json:"col_span,omitempty"`
	RowSpan     uint32 `json:"row_span,omitempty"`

	// Chart (type=chart). One or more series rendered inline. TUI
	// renders as an ASCII sparkline per series; desktop as Lightweight
	// Charts lines; phone as an SVG polyline. Keep series counts
	// small (≤4) so the cell stays readable without scrolling.
	Series []ChartSeries `json:"series,omitempty"`

	// Table (type=table). Columnar layout inside a single cell.
	// Renderers clip or scroll as needed; the SDK does not enforce a
	// max row count, but plugins should keep it well under the
	// panel height.
	Columns []TableColumn `json:"columns,omitempty"`
	Rows    [][]string    `json:"rows,omitempty"`

	// Image (type=image). Src is an http(s):// URL OR a NOTBBG:/path
	// reference that a client resolves locally; Alt is shown when the
	// image fails to load or the client cannot render images inline
	// (e.g. the TUI, which prints `[IMG alt]` + offers an open
	// shortcut). Width/Height in pixels are hints; renderers may
	// clamp to their own layout.
	Src    string `json:"src,omitempty"`
	Alt    string `json:"alt,omitempty"`
	Width  uint32 `json:"width,omitempty"`
	Height uint32 `json:"height,omitempty"`
}

// ChartSeries is one line/bar series in a ChartCell.
type ChartSeries struct {
	Name   string    `json:"name,omitempty"`
	Values []float64 `json:"values"`
	Kind   string    `json:"kind,omitempty"` // "line" (default), "bar", "area"
	Color  string    `json:"color,omitempty"` // named color (green/red/amber/cyan) or hex
}

// TableColumn describes a column in a TableCell.
type TableColumn struct {
	Header string `json:"header"`
	Width  uint32 `json:"width,omitempty"`  // preferred width in characters; 0 = auto
	Align  string `json:"align,omitempty"`  // "left" (default), "right", "center"
}

// CellGridUpdate is the payload for a cell grid screen update.
type CellGridUpdate struct {
	ScreenID    string `json:"screen_id"`
	Cells       []Cell `json:"cells"`
	FullReplace bool   `json:"full_replace,omitempty"`
	Version     string `json:"version"` // "cellgrid/v1" — distinguishes from legacy StyledLine
}

// InputEvent is received from the GUI when a user interacts with an
// input cell OR when a control event (e.g. job cancel) is routed to
// the plugin. Kind discriminates; empty string defaults to "edit"
// for backward compatibility with pre-2026-04-25 plugins.
//
//   Kind == ""       — cell edit; Address + Value populated.
//   Kind == "edit"   — cell edit (explicit); same fields.
//   Kind == "cancel" — operator requested cancellation of JobID.
//                      Address / Value are empty.
type InputEvent struct {
	Kind     string      `json:"kind,omitempty"`
	ScreenID string      `json:"screen_id"`
	Address  CellAddress `json:"address,omitempty"`
	Value    any         `json:"value,omitempty"`
	JobID    string      `json:"job_id,omitempty"`
}

// IsCancel reports whether the event asks the plugin to cancel a
// running job (rather than apply a cell edit). Convenience around
// the Kind string so plugin authors don't hard-code the constant.
func (e InputEvent) IsCancel() bool { return e.Kind == "cancel" }

// ---------------------------------------------------------------------------
// Builder helpers — fluent API for constructing cells.
// ---------------------------------------------------------------------------

// TextCell creates a styled text cell (label, header, section divider).
func TextCell(row, col uint32, text string, style *CellStyle) Cell {
	return Cell{
		Address: CellAddress{Row: row, Col: col},
		Type:    "text",
		Text:    text,
		Style:   style,
	}
}

// HeaderCell creates a bold header text cell.
func HeaderCell(row, col uint32, text string) Cell {
	return Cell{
		Address: CellAddress{Row: row, Col: col},
		Type:    "text",
		Text:    text,
		Style:   &CellStyle{Bold: true, Fg: "cyan"},
	}
}

// SectionCell creates a section divider spanning multiple columns.
func SectionCell(row, col uint32, text string, span uint32) Cell {
	return Cell{
		Address: CellAddress{Row: row, Col: col},
		Type:    "text",
		Text:    text,
		Style:   &CellStyle{Bold: true, Fg: "cyan"},
		ColSpan: span,
	}
}

// DecimalInputCell creates a decimal number input.
func DecimalInputCell(row, col uint32, label string, value float64, precision uint32) Cell {
	return Cell{
		Address:   CellAddress{Row: row, Col: col},
		Type:      "input_decimal",
		Label:     label,
		Value:     value,
		Precision: precision,
	}
}

// IntegerInputCell creates an integer input.
func IntegerInputCell(row, col uint32, label string, value int64) Cell {
	return Cell{
		Address: CellAddress{Row: row, Col: col},
		Type:    "input_integer",
		Label:   label,
		Value:   value,
	}
}

// EnumInputCell creates a dropdown selector.
func EnumInputCell(row, col uint32, label string, value string, options []EnumOption) Cell {
	return Cell{
		Address: CellAddress{Row: row, Col: col},
		Type:    "input_enum",
		Label:   label,
		Value:   value,
		Options: options,
	}
}

// SelectionInputCell creates a ticker/instrument search picker.
func SelectionInputCell(row, col uint32, label string, value string, hint string) Cell {
	return Cell{
		Address:    CellAddress{Row: row, Col: col},
		Type:       "input_selection",
		Label:      label,
		Value:      value,
		SearchHint: hint,
	}
}

// StringInputCell creates a free-text input.
func StringInputCell(row, col uint32, label string, value string, placeholder string) Cell {
	return Cell{
		Address:     CellAddress{Row: row, Col: col},
		Type:        "input_string",
		Label:       label,
		Value:       value,
		Placeholder: placeholder,
	}
}

// NumberCell creates a formatted numeric output cell.
func NumberCell(row, col uint32, label string, value float64, precision uint32, unit string) Cell {
	return Cell{
		Address:   CellAddress{Row: row, Col: col},
		Type:      "number",
		Label:     label,
		Value:     value,
		Precision: precision,
		Unit:      unit,
	}
}

// NumberCellWithDelta creates a numeric output with direction indicator.
func NumberCellWithDelta(row, col uint32, label string, value float64, precision uint32, unit string, delta string) Cell {
	return Cell{
		Address:   CellAddress{Row: row, Col: col},
		Type:      "number",
		Label:     label,
		Value:     value,
		Precision: precision,
		Unit:      unit,
		Delta:     delta,
	}
}

// ScriptInputCell creates a multi-line script editor input.
func ScriptInputCell(row, col uint32, label string, value string, colSpan uint32) Cell {
	return Cell{
		Address: CellAddress{Row: row, Col: col},
		Type:    "input_script",
		Label:   label,
		Value:   value,
		ColSpan: colSpan,
	}
}

// VolSurfaceCell creates a 3D vol surface component cell.
// data should be a JSON string with {maturities, moneyness, vols, spot, asset}.
func VolSurfaceCell(row, col uint32, data string, colSpan uint32) Cell {
	return Cell{
		Address:     CellAddress{Row: row, Col: col},
		Type:        "component",
		ComponentID: "vol_surface",
		Value:       data,
		ColSpan:     colSpan,
	}
}

// SwaptionCubeCell creates a 3D swaption cube component cell.
func SwaptionCubeCell(row, col uint32, data string, colSpan uint32) Cell {
	return Cell{
		Address:     CellAddress{Row: row, Col: col},
		Type:        "component",
		ComponentID: "swaption_cube",
		Value:       data,
		ColSpan:     colSpan,
	}
}

// ProgressBarCell creates a progress bar component cell.
func ProgressBarCell(row, col uint32, label string, progress float64, status string) Cell {
	return Cell{
		Address:     CellAddress{Row: row, Col: col},
		Type:        "component",
		ComponentID: "progress",
		Label:       label,
		Value:       progress, // 0.0 to 1.0
		Text:        status,   // "Running...", "45%", etc.
	}
}

// FormulaCell creates an Excel-style formula cell.
func FormulaCell(row, col uint32, label string, expression string, cachedValue float64, precision uint32) Cell {
	return Cell{
		Address:    CellAddress{Row: row, Col: col},
		Type:       "formula",
		Label:      label,
		Expression: expression,
		Value:      cachedValue,
		Precision:  precision,
	}
}

// ---------------------------------------------------------------------------
// Plugin extensions for cell grid screens.
// ---------------------------------------------------------------------------

// UpdateCellGrid publishes a cell grid screen update.
func (p *Plugin) UpdateCellGrid(screenID string, cells []Cell, fullReplace bool) {
	update := CellGridUpdate{
		ScreenID:    screenID,
		Cells:       cells,
		FullReplace: fullReplace,
		Version:     "cellgrid/v1",
	}
	p.Publish(p.screenTopic, update)
}

// CellValue extracts a float64 from an InputEvent.Value (handles JSON number types).
func CellValue(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}

// CellStringValue extracts a string from an InputEvent.Value.
func CellStringValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%g", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// ---------------------------------------------------------------------------
// Chart + Table cell builders (plugin Phase 2.4).
// ---------------------------------------------------------------------------

// ChartCell creates a chart cell with one or more series. Typical
// use is a single sparkline:
//
//	ChartCell(row, col, "PnL", []ChartSeries{{Values: pnlPoints}})
//
// Multiple series share the x-axis; series[i].Values[j] is aligned
// with series[0].Values[j].
func ChartCell(row, col uint32, label string, series []ChartSeries) Cell {
	return Cell{
		Address: CellAddress{Row: row, Col: col},
		Type:    "chart",
		Label:   label,
		Series:  series,
	}
}

// LineSeries is a small convenience constructor for a single-line
// series, the most common shape.
func LineSeries(name string, values []float64) ChartSeries {
	return ChartSeries{Name: name, Values: values, Kind: "line"}
}

// TableCell creates a table cell with a column header row and N data
// rows. Cell rendering is clip-on-overflow by default; renderers
// may choose to scroll or truncate based on column widths.
func TableCell(row, col uint32, label string, columns []TableColumn, rows [][]string) Cell {
	return Cell{
		Address: CellAddress{Row: row, Col: col},
		Type:    "table",
		Label:   label,
		Columns: columns,
		Rows:    rows,
	}
}

// ImageCell creates an image cell. Src can be an http(s):// URL or a
// `NOTBBG:/abs/path` reference the client resolves to a local file.
// Alt is shown in non-graphical clients (TUI) and when loading
// fails. Width/Height (pixels) are hints — renderers may clamp.
func ImageCell(row, col uint32, src, alt string, width, height uint32) Cell {
	return Cell{
		Address: CellAddress{Row: row, Col: col},
		Type:    "image",
		Src:    src,
		Alt:    alt,
		Width:  width,
		Height: height,
	}
}
