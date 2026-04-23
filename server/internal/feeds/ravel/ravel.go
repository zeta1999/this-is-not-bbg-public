// Package ravel reads Ravel-format JSON snapshots
// (reference/Ravel-master/ravel/tests/data/*.expected.json) and
// emits dim-keyed Points.
//
// Two shapes are recognised:
//
//  1. Single-market snapshot (Rates_All.expected.json etc.):
//     { "now": <iso>, "rates": {...}, "repo": {...},
//       "dividends": {...}, "surfaces": {...}, ... }
//
//  2. Dated multi-market snapshot (AllMarket_All.expected.json):
//     { "snapshotDates": [...],
//       "snapshots": { "<date>": { "eq_market": <single>,
//                                  "fx_market": <single>, ... } } }
//
// Rates / repo / dividends / forwards expand to one Point per curve
// point. Vol surfaces are wrapped as a single Point per surface with
// the raw surface JSON attached — they're a natural fit once the
// dim model supports arrays.
//
// See DATA-PLAN.md §5.
package ravel

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/notbbg/notbbg/server/internal/dimpath"
)

// Point mirrors the sibelius.Point type. Kept package-local so
// package boundaries are explicit — callers that want a unified
// view translate at the edges.
type Point struct {
	Dims  map[string]string
	T     time.Time
	Value Value
}

type Value struct {
	Scalar float64
	Raw    json.RawMessage
}

// ParseFile reads path and returns all decoded points.
func ParseFile(path string) ([]Point, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBytes(data)
}

// ParseBytes auto-detects the two supported shapes.
func ParseBytes(data []byte) ([]Point, error) {
	// Probe for "snapshots" → dated shape.
	var probe struct {
		Snapshots map[string]json.RawMessage `json:"snapshots"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && len(probe.Snapshots) > 0 {
		return parseDated(probe.Snapshots)
	}
	return parseSingleSnapshot(data, time.Time{})
}

func parseDated(snaps map[string]json.RawMessage) ([]Point, error) {
	var out []Point
	for dateStr, raw := range snaps {
		t, _ := time.Parse("2006-01-02", dateStr)
		if t.IsZero() {
			// Fallback: parse full iso.
			t, _ = time.Parse(time.RFC3339, dateStr)
		}
		// Each snapshot may carry multiple market sections.
		var markets map[string]json.RawMessage
		if err := json.Unmarshal(raw, &markets); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", dateStr, err)
		}
		for section, body := range markets {
			pts, err := parseMarketSection(section, body, t)
			if err != nil {
				return nil, fmt.Errorf("snapshot %s/%s: %w", dateStr, section, err)
			}
			out = append(out, pts...)
		}
	}
	return out, nil
}

// parseSingleSnapshot handles the unnested shape. If t is zero the
// top-level `now` field is used.
func parseSingleSnapshot(data []byte, t time.Time) ([]Point, error) {
	return parseMarketSection("", data, t)
}

// parseMarketSection decodes one market block (rates/repo/dividends/
// forwards/surfaces). section names the parent ("eq_market" etc.) —
// used as a dim so cross-section queries work.
//
// Snapshots may contain sibling fields that are not market-shaped
// (e.g. top-level arrays like "correlationKeys"). Those don't unmarshal
// into the struct below; we skip them silently rather than fail the
// whole snapshot.
func parseMarketSection(section string, raw json.RawMessage, t time.Time) ([]Point, error) {
	// Quick shape check: we only handle objects here.
	trimmed := bytesFirstNonSpace(raw)
	if trimmed != '{' {
		return nil, nil
	}

	var s struct {
		Now        string                     `json:"now"`
		Rates      map[string]ratesCurve      `json:"rates"`
		Repo       map[string]ratesCurve      `json:"repo"`
		Dividends  map[string]dividendsCurve  `json:"dividends"`
		Forwards   map[string]json.RawMessage `json:"forwards"`
		Surfaces   map[string]json.RawMessage `json:"surfaces"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	// Prefer snapshot-level `now` over the parent date.
	if s.Now != "" {
		if parsed, err := time.Parse(time.RFC3339, s.Now); err == nil {
			t = parsed
		}
	}

	var out []Point
	for curve, c := range s.Rates {
		for _, p := range c.Rates {
			dims := map[string]string{
				"type":   "rate",
				"source": "ravel",
				"curve":  curve,
			}
			if section != "" {
				dims["section"] = section
			}
			dims["tenor"] = strconv.FormatFloat(p.T, 'f', -1, 64)
			dims = dimpath.MergeDefaults(dims, dimpath.Defaults)

			mat, _ := time.Parse(time.RFC3339, p.Maturity)
			if mat.IsZero() {
				mat = t
			}
			out = append(out, Point{Dims: dims, T: mat, Value: Value{Scalar: p.R}})
		}
	}
	for curve, c := range s.Repo {
		for _, p := range c.Rates {
			dims := map[string]string{
				"type":   "repo",
				"source": "ravel",
				"curve":  curve,
			}
			if section != "" {
				dims["section"] = section
			}
			dims["tenor"] = strconv.FormatFloat(p.T, 'f', -1, 64)
			dims = dimpath.MergeDefaults(dims, dimpath.Defaults)

			mat, _ := time.Parse(time.RFC3339, p.Maturity)
			if mat.IsZero() {
				mat = t
			}
			out = append(out, Point{Dims: dims, T: mat, Value: Value{Scalar: p.R}})
		}
	}
	for asset, d := range s.Dividends {
		for _, div := range d.Dividends {
			dims := map[string]string{
				"type":   "dividend",
				"source": "ravel",
				"asset":  asset,
			}
			if section != "" {
				dims["section"] = section
			}
			dims = dimpath.MergeDefaults(dims, dimpath.Defaults)

			ex, _ := time.Parse(time.RFC3339, div.ExDiv)
			if ex.IsZero() {
				ex = t
			}
			out = append(out, Point{Dims: dims, T: ex, Value: Value{Scalar: div.Amount}})
		}
	}
	for asset, raw := range s.Surfaces {
		dims := map[string]string{
			"type":   "volsurface",
			"source": "ravel",
			"asset":  asset,
		}
		if section != "" {
			dims["section"] = section
		}
		dims = dimpath.MergeDefaults(dims, dimpath.Defaults)
		out = append(out, Point{
			Dims: dims, T: t,
			Value: Value{Raw: append(json.RawMessage(nil), raw...)},
		})
	}
	for asset, raw := range s.Forwards {
		dims := map[string]string{
			"type":   "forward",
			"source": "ravel",
			"asset":  asset,
		}
		if section != "" {
			dims["section"] = section
		}
		dims = dimpath.MergeDefaults(dims, dimpath.Defaults)
		out = append(out, Point{
			Dims: dims, T: t,
			Value: Value{Raw: append(json.RawMessage(nil), raw...)},
		})
	}

	return out, nil
}

type ratesCurve struct {
	Rates []ratePoint `json:"rates"`
}

type ratePoint struct {
	Maturity string  `json:"maturity"`
	T        float64 `json:"t"`
	R        float64 `json:"r"`
}

// bytesFirstNonSpace returns the first non-whitespace byte of b, or
// 0 if b is empty / all whitespace.
func bytesFirstNonSpace(b []byte) byte {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return c
	}
	return 0
}

type dividendsCurve struct {
	Dividends []dividendPoint `json:"dividends"`
}

type dividendPoint struct {
	ExDiv  string  `json:"exDiv"`
	PayDiv string  `json:"payDiv"`
	TEx    float64 `json:"tEx"`
	TPay   float64 `json:"tPay"`
	Amount float64 `json:"amount"`
	Fix    float64 `json:"fix"`
}
