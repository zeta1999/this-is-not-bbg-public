package ravel

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/notbbg/notbbg/server/internal/dimpath"
	"github.com/xuri/excelize/v2"
)

// ParseXLSX reads a Ravel workbook and emits one Point per data row
// of each sheet. Row 1 is treated as a header; each non-header cell
// becomes a dim when non-numeric and a value when numeric.
//
// Heuristics (conservative, can be tightened per-sheet later):
//   - First numeric column wins as the scalar value.
//   - All other columns become dims keyed by their header.
//   - Sheet name becomes dims["type"].
//   - source=ravel + sheet=<name> always included.
//
// Non-number values with a header like "date"/"time" are parsed as
// RFC3339 / yyyy-mm-dd and used as the point timestamp when present.
//
// See DATA-PLAN.md §5.
func ParseXLSX(path string) ([]Point, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	var out []Point
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return nil, fmt.Errorf("sheet %s: %w", sheet, err)
		}
		if len(rows) < 2 {
			continue
		}
		headers := rows[0]
		for ri, row := range rows[1:] {
			if len(row) == 0 {
				continue
			}
			pt := parseRow(sheet, headers, row, ri+2)
			if pt == nil {
				continue
			}
			out = append(out, *pt)
		}
	}
	return out, nil
}

// parseRow turns one XLSX row into a Point, returning nil for
// entirely empty rows. rowIdx is the 1-based row number for
// diagnostics.
func parseRow(sheet string, headers, row []string, rowIdx int) *Point {
	dims := map[string]string{
		"source": "ravel",
		"sheet":  sheet,
		"type":   canonicalType(sheet),
		"row":    strconv.Itoa(rowIdx),
	}
	var (
		val      float64
		gotVal   bool
		rowTime  time.Time
		rawParts []string
	)
	for ci, cell := range row {
		if cell == "" {
			continue
		}
		header := ""
		if ci < len(headers) {
			header = strings.TrimSpace(headers[ci])
		}
		if header == "" {
			header = "col" + strconv.Itoa(ci)
		}

		// Try numeric.
		if f, err := strconv.ParseFloat(cell, 64); err == nil {
			if !gotVal {
				val = f
				gotVal = true
				continue
			}
			// Subsequent numbers become scalar-valued dims so no data
			// is lost.
			dims[header] = cell
			continue
		}

		// Try time.
		if isTimeHeader(header) {
			if t, ok := tryParseTime(cell); ok {
				rowTime = t
				dims[header] = cell
				continue
			}
		}

		// Fall back: stash as a dim.
		dims[header] = cell
		rawParts = append(rawParts, header+"="+cell)
	}

	if !gotVal && rowTime.IsZero() && len(dims) <= 4 {
		// "source,sheet,type,row" only → empty row, skip.
		return nil
	}

	dims = dimpath.MergeDefaults(dims, dimpath.Defaults)

	p := Point{
		Dims: dims,
		T:    rowTime,
	}
	if gotVal {
		p.Value = Value{Scalar: val}
	} else {
		p.Value = Value{Raw: json.RawMessage(`"` + strings.Join(rawParts, ";") + `"`)}
	}
	return &p
}

// canonicalType maps a Ravel sheet name to a plausible type dim.
// Falls back to the raw sheet name lowercased.
func canonicalType(sheet string) string {
	s := strings.ToLower(sheet)
	switch {
	case strings.Contains(s, "rate"):
		return "rate"
	case strings.Contains(s, "repo"):
		return "repo"
	case strings.Contains(s, "div"):
		return "dividend"
	case strings.Contains(s, "vol"):
		return "volparam"
	case strings.Contains(s, "market"):
		return "market"
	case strings.Contains(s, "listed"):
		return "listed"
	}
	return s
}

func isTimeHeader(h string) bool {
	s := strings.ToLower(h)
	return s == "date" || s == "time" || s == "maturity" || s == "exdiv" || s == "paydiv" || strings.Contains(s, "date")
}

func tryParseTime(s string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
		"01/02/2006",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
