package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ScriptEditor is a simple multi-line text editor for Aria strategy scripts.
type ScriptEditor struct {
	Lines    []string // text lines
	CursorR  int      // cursor row (line index)
	CursorC  int      // cursor column
	ScrollOff int     // first visible line
	Title    string
	Language string   // "aria-strategy", "aria-payoff", etc.

	// File picker overlay.
	FilePicker    bool     // true when file picker is open
	FileList      []string // available files
	FilePickerIdx int      // selected index in file list
	ScriptsDir    string   // path to scripts directory
}

// OpenFilePicker populates the file list and shows the picker overlay.
func (e *ScriptEditor) OpenFilePicker(files []string, scriptsDir string) {
	e.FilePicker = true
	e.FileList = files
	e.FilePickerIdx = 0
	e.ScriptsDir = scriptsDir
}

// CloseFilePicker hides the picker.
func (e *ScriptEditor) CloseFilePicker() {
	e.FilePicker = false
}

// SelectedFile returns the currently highlighted file name, or empty.
func (e *ScriptEditor) SelectedFile() string {
	if e.FilePickerIdx >= 0 && e.FilePickerIdx < len(e.FileList) {
		return e.FileList[e.FilePickerIdx]
	}
	return ""
}

// NewScriptEditor creates an editor pre-loaded with text.
func NewScriptEditor(title, language, text string) *ScriptEditor {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	return &ScriptEditor{
		Lines:    lines,
		Title:    title,
		Language: language,
	}
}

// Text returns the full editor content as a single string.
func (e *ScriptEditor) Text() string {
	return strings.Join(e.Lines, "\n")
}

// InsertChar inserts a character at the cursor position.
func (e *ScriptEditor) InsertChar(ch rune) {
	line := e.Lines[e.CursorR]
	if e.CursorC > len(line) {
		e.CursorC = len(line)
	}
	e.Lines[e.CursorR] = line[:e.CursorC] + string(ch) + line[e.CursorC:]
	e.CursorC++
}

// InsertTab inserts 2 spaces.
func (e *ScriptEditor) InsertTab() {
	e.InsertChar(' ')
	e.InsertChar(' ')
}

// Backspace deletes the character before the cursor.
func (e *ScriptEditor) Backspace() {
	if e.CursorC > 0 {
		line := e.Lines[e.CursorR]
		e.Lines[e.CursorR] = line[:e.CursorC-1] + line[e.CursorC:]
		e.CursorC--
	} else if e.CursorR > 0 {
		// Merge with previous line.
		prev := e.Lines[e.CursorR-1]
		e.CursorC = len(prev)
		e.Lines[e.CursorR-1] = prev + e.Lines[e.CursorR]
		e.Lines = append(e.Lines[:e.CursorR], e.Lines[e.CursorR+1:]...)
		e.CursorR--
	}
}

// Delete deletes the character at the cursor.
func (e *ScriptEditor) Delete() {
	line := e.Lines[e.CursorR]
	if e.CursorC < len(line) {
		e.Lines[e.CursorR] = line[:e.CursorC] + line[e.CursorC+1:]
	} else if e.CursorR < len(e.Lines)-1 {
		// Merge with next line.
		e.Lines[e.CursorR] = line + e.Lines[e.CursorR+1]
		e.Lines = append(e.Lines[:e.CursorR+1], e.Lines[e.CursorR+2:]...)
	}
}

// Enter splits the line at the cursor.
func (e *ScriptEditor) Enter() {
	line := e.Lines[e.CursorR]
	if e.CursorC > len(line) {
		e.CursorC = len(line)
	}
	before := line[:e.CursorC]
	after := line[e.CursorC:]

	// Auto-indent: copy leading whitespace from current line.
	indent := ""
	for _, ch := range before {
		if ch == ' ' || ch == '\t' {
			indent += string(ch)
		} else {
			break
		}
	}

	e.Lines[e.CursorR] = before
	newLines := make([]string, 0, len(e.Lines)+1)
	newLines = append(newLines, e.Lines[:e.CursorR+1]...)
	newLines = append(newLines, indent+after)
	newLines = append(newLines, e.Lines[e.CursorR+1:]...)
	e.Lines = newLines
	e.CursorR++
	e.CursorC = len(indent)
}

// Move cursor.
func (e *ScriptEditor) Up()    { if e.CursorR > 0 { e.CursorR--; e.clampCol() } }
func (e *ScriptEditor) Down()  { if e.CursorR < len(e.Lines)-1 { e.CursorR++; e.clampCol() } }
func (e *ScriptEditor) Left()  {
	if e.CursorC > 0 {
		e.CursorC--
	} else if e.CursorR > 0 {
		e.CursorR--
		e.CursorC = len(e.Lines[e.CursorR])
	}
}
func (e *ScriptEditor) Right() {
	if e.CursorC < len(e.Lines[e.CursorR]) {
		e.CursorC++
	} else if e.CursorR < len(e.Lines)-1 {
		e.CursorR++
		e.CursorC = 0
	}
}
func (e *ScriptEditor) Home() { e.CursorC = 0 }
func (e *ScriptEditor) End()  { e.CursorC = len(e.Lines[e.CursorR]) }

func (e *ScriptEditor) clampCol() {
	if e.CursorC > len(e.Lines[e.CursorR]) {
		e.CursorC = len(e.Lines[e.CursorR])
	}
}

// Aria strategy DSL keywords for syntax highlighting.
var ariaStrategyKeywords = map[string]bool{
	"signal": true, "let": true, "var": true, "strategy": true,
	"when": true, "buy": true, "sell": true, "flatten": true,
	"limit_buy": true, "limit_sell": true, "vwap": true,
	"sma": true, "ema": true, "cross_up": true, "cross_down": true,
	"run_max": true, "run_min": true, "stdev": true, "zscore": true,
	"percentile": true, "if": true, "close": true, "open": true,
	"high": true, "low": true, "volume": true, "abs": true,
	"max": true, "min": true, "exp": true, "log": true, "prev": true,
}

// Aria payoff DSL keywords.
var ariaPayoffKeywords = map[string]bool{
	"contract": true, "let": true, "one": true, "scale": true,
	"when": true, "barrier": true, "anytime": true, "give": true,
	"and": true, "or": true, "cond": true, "zero": true,
	"spot": true, "rate": true, "fxrate": true, "discount": true,
	"terminate": true, "until": true, "max": true, "min": true,
	"abs": true, "exp": true, "log": true, "prev": true,
	"running_max": true, "running_min": true, "smooth_step": true,
}

var (
	editorBorderStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#555555"))
	editorTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF8C00"))
	editorLineNumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	editorCursorStyle  = lipgloss.NewStyle().Background(lipgloss.Color("#444444"))
	editorKeywordStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8C00")).Bold(true)
	editorNumberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00CCCC"))
	editorStringStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#88CC88"))
	editorCommentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Italic(true)
	editorNormalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
	editorStatusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
)

// RenderScriptEditor renders the editor as a full-screen overlay.
func RenderScriptEditor(e *ScriptEditor, width, height int) string {
	// Reserve space for title, status bar, border.
	innerW := width - 4
	innerH := height - 4
	if innerW < 20 {
		innerW = 20
	}
	if innerH < 5 {
		innerH = 5
	}

	// Scroll to keep cursor visible.
	visibleLines := innerH - 2 // title + status
	if e.CursorR < e.ScrollOff {
		e.ScrollOff = e.CursorR
	}
	if e.CursorR >= e.ScrollOff+visibleLines {
		e.ScrollOff = e.CursorR - visibleLines + 1
	}

	keywords := ariaStrategyKeywords
	if e.Language == "aria-payoff" {
		keywords = ariaPayoffKeywords
	}

	// Title bar.
	title := editorTitleStyle.Render(fmt.Sprintf(" %s ", e.Title))
	helpHint := editorStatusStyle.Render("  Ctrl+S:save  Ctrl+O:open  Esc:cancel")

	// Render visible lines with line numbers and syntax highlighting.
	var lines []string
	lineNumWidth := len(fmt.Sprintf("%d", len(e.Lines))) + 1
	if lineNumWidth < 3 {
		lineNumWidth = 3
	}

	endLine := e.ScrollOff + visibleLines
	if endLine > len(e.Lines) {
		endLine = len(e.Lines)
	}

	for i := e.ScrollOff; i < endLine; i++ {
		lineNum := editorLineNumStyle.Render(fmt.Sprintf("%*d ", lineNumWidth, i+1))
		line := e.Lines[i]

		if i == e.CursorR {
			// Cursor line: highlight with cursor position.
			highlighted := highlightLine(line, keywords, e.CursorC, innerW-lineNumWidth-1)
			lines = append(lines, lineNum+highlighted)
		} else {
			highlighted := highlightLine(line, keywords, -1, innerW-lineNumWidth-1)
			lines = append(lines, lineNum+highlighted)
		}
	}

	// Pad remaining lines.
	for len(lines) < visibleLines {
		lines = append(lines, editorLineNumStyle.Render(strings.Repeat(" ", lineNumWidth+1))+"~")
	}

	// Status bar.
	status := editorStatusStyle.Render(fmt.Sprintf(" Ln %d, Col %d  |  %d lines  |  %s ",
		e.CursorR+1, e.CursorC+1, len(e.Lines), e.Language))

	// File picker overlay.
	if e.FilePicker {
		var pickerLines []string
		pickerLines = append(pickerLines, editorTitleStyle.Render(" Open Script ")+"  "+editorStatusStyle.Render("j/k:nav  Enter:open  Esc:cancel"))
		pickerLines = append(pickerLines, "")
		if len(e.FileList) == 0 {
			pickerLines = append(pickerLines, editorStatusStyle.Render("  No .aria or .strat files found in "+e.ScriptsDir))
		}
		for i, f := range e.FileList {
			if i == e.FilePickerIdx {
				pickerLines = append(pickerLines, editorKeywordStyle.Render("  ▸ "+f))
			} else {
				pickerLines = append(pickerLines, editorNormalStyle.Render("    "+f))
			}
		}
		return editorBorderStyle.Width(innerW).Render(strings.Join(pickerLines, "\n"))
	}

	content := title + helpHint + "\n" + strings.Join(lines, "\n") + "\n" + status
	return editorBorderStyle.Width(innerW).Render(content)
}

// highlightLine applies syntax highlighting to a single line.
// cursorCol >= 0 means draw the cursor at that column.
func highlightLine(line string, keywords map[string]bool, cursorCol int, maxWidth int) string {
	if len(line) > maxWidth {
		line = line[:maxWidth]
	}

	var result strings.Builder
	i := 0
	for i < len(line) {
		ch := line[i]

		// Insert cursor.
		renderChar := func(styled string) {
			if i == cursorCol {
				result.WriteString(editorCursorStyle.Render(styled))
			} else {
				result.WriteString(styled)
			}
		}

		// Comment (// or #).
		if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			rest := line[i:]
			if cursorCol >= i && cursorCol < i+len(rest) {
				// Cursor is in comment.
				before := rest[:cursorCol-i]
				at := string(rest[cursorCol-i])
				after := rest[cursorCol-i+1:]
				result.WriteString(editorCommentStyle.Render(before))
				result.WriteString(editorCursorStyle.Render(at))
				result.WriteString(editorCommentStyle.Render(after))
			} else {
				result.WriteString(editorCommentStyle.Render(rest))
			}
			i = len(line)
			continue
		}
		if ch == '#' {
			rest := line[i:]
			result.WriteString(editorCommentStyle.Render(rest))
			i = len(line)
			continue
		}

		// String literal.
		if ch == '"' {
			end := strings.Index(line[i+1:], "\"")
			if end >= 0 {
				str := line[i : i+end+2]
				result.WriteString(editorStringStyle.Render(str))
				i += end + 2
				continue
			}
		}

		// Number.
		if ch >= '0' && ch <= '9' {
			j := i
			for j < len(line) && ((line[j] >= '0' && line[j] <= '9') || line[j] == '.') {
				j++
			}
			num := line[i:j]
			if cursorCol >= i && cursorCol < j {
				// Cursor in number.
				before := num[:cursorCol-i]
				at := string(num[cursorCol-i])
				after := num[cursorCol-i+1:]
				result.WriteString(editorNumberStyle.Render(before))
				result.WriteString(editorCursorStyle.Render(at))
				result.WriteString(editorNumberStyle.Render(after))
			} else {
				result.WriteString(editorNumberStyle.Render(num))
			}
			i = j
			continue
		}

		// Identifier / keyword.
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' {
			j := i
			for j < len(line) && ((line[j] >= 'a' && line[j] <= 'z') || (line[j] >= 'A' && line[j] <= 'Z') || (line[j] >= '0' && line[j] <= '9') || line[j] == '_') {
				j++
			}
			word := line[i:j]
			style := editorNormalStyle
			if keywords[word] {
				style = editorKeywordStyle
			}
			if cursorCol >= i && cursorCol < j {
				before := word[:cursorCol-i]
				at := string(word[cursorCol-i])
				after := word[cursorCol-i+1:]
				result.WriteString(style.Render(before))
				result.WriteString(editorCursorStyle.Render(at))
				result.WriteString(style.Render(after))
			} else {
				result.WriteString(style.Render(word))
			}
			i = j
			continue
		}

		// Operators and other characters.
		renderChar(editorNormalStyle.Render(string(ch)))
		i++
	}

	// Cursor at end of line.
	if cursorCol >= len(line) {
		result.WriteString(editorCursorStyle.Render(" "))
	}

	return result.String()
}
