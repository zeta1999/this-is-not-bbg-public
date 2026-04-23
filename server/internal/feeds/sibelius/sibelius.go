// Package sibelius reads Sibelius-format JSON payloads
// (../Sibelius/TestData/inputs/aria_*.json) and maps them to
// dim-keyed SeriesPoints.
//
// Sibelius uses picojson-style free-form JSON: top-level shape is
//
//	{"name": "AriaCalibrate/1", "arguments": [{ ... }]}
//
// Known shapes we extract from arguments[0]:
//
//   - "instruments": [{strike, T, price, ...}]   → one point per strike/T
//   - "book":        [{type, strike, maturity, notional, ...}] → one point per deal
//   - "trades":      [{script, notional, ...}]   → one point per script
//
// Anything else is wrapped in a single catch-all point with the job
// payload carried as raw bytes, so nothing is ever silently dropped.
//
// See DATA-PLAN.md §5.
package sibelius

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/notbbg/notbbg/server/internal/dimpath"
)

// Point is a dim-keyed observation. Mirrors the notbbg.v1.SeriesPoint
// proto shape; until buf regenerates we carry it as Go.
type Point struct {
	Dims  map[string]string
	T     time.Time
	Value Value
}

// Value holds a typed observation. Exactly one field is non-zero.
type Value struct {
	Scalar float64
	Raw    json.RawMessage
}

// envelope mirrors the outer shape of a Sibelius input file.
type envelope struct {
	Name      string            `json:"name"`
	Arguments []json.RawMessage `json:"arguments"`
}

// ParseFile reads path and returns all points it can derive. Unknown
// shapes surface as a single "job" point so callers can still see
// them.
func ParseFile(path string) ([]Point, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBytes(data)
}

// ParseBytes is the stdin-friendly form of ParseFile.
func ParseBytes(data []byte) ([]Point, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("sibelius envelope: %w", err)
	}
	if len(env.Arguments) == 0 {
		return []Point{jobPoint(env.Name, data)}, nil
	}

	// Inspect arguments[0] for the known subfields.
	var args map[string]json.RawMessage
	if err := json.Unmarshal(env.Arguments[0], &args); err != nil {
		return nil, fmt.Errorf("sibelius arguments[0]: %w", err)
	}

	now := time.Now().UTC()
	var out []Point

	if raw, ok := args["instruments"]; ok {
		pts, err := instrumentsToPoints(env.Name, raw, args, now)
		if err != nil {
			return nil, err
		}
		out = append(out, pts...)
	}

	if raw, ok := args["book"]; ok {
		pts, err := bookToPoints(env.Name, raw, now)
		if err != nil {
			return nil, err
		}
		out = append(out, pts...)
	}

	if raw, ok := args["trades"]; ok {
		pts, err := tradesToPoints(env.Name, raw, now)
		if err != nil {
			return nil, err
		}
		out = append(out, pts...)
	}

	// Nothing matched — surface the whole payload as a single point.
	if len(out) == 0 {
		out = append(out, jobPoint(env.Name, data))
	}
	return out, nil
}

// instrumentsToPoints maps an aria_calibrate_* instruments list into
// one point per (strike, T). Reference spot/rate/model on args get
// carried as shared dims so downstream queries can pin a scenario.
func instrumentsToPoints(jobName string, raw json.RawMessage, args map[string]json.RawMessage, t time.Time) ([]Point, error) {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("instruments: %w", err)
	}
	shared := sharedDims(jobName, args)
	shared["type"] = "option"

	var out []Point
	for _, r := range rows {
		dims := copyDims(shared)
		if s, ok := floatField(r, "strike"); ok {
			dims["strike"] = strconv.FormatFloat(s, 'f', -1, 64)
		}
		if t, ok := floatField(r, "T"); ok {
			dims["tenor"] = strconv.FormatFloat(t, 'f', -1, 64)
		}
		if isCall, ok := boolField(r, "is_call"); ok {
			if isCall {
				dims["side"] = "call"
			} else {
				dims["side"] = "put"
			}
		}
		dims = dimpath.MergeDefaults(dims, dimpath.Defaults)

		price, _ := floatField(r, "price")
		out = append(out, Point{Dims: dims, T: t, Value: Value{Scalar: price}})
	}
	return out, nil
}

// bookToPoints maps aria_backtest book entries into one point per
// deal. Uses `type` from the row as the dim type directly so a
// heterogeneous book spans multiple dim types.
func bookToPoints(jobName string, raw json.RawMessage, t time.Time) ([]Point, error) {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("book: %w", err)
	}
	var out []Point
	for _, r := range rows {
		dims := map[string]string{
			"source": "sibelius",
			"name":   jobName,
		}
		if ty, ok := stringField(r, "type"); ok {
			dims["type"] = ty
		}
		if s, ok := floatField(r, "strike"); ok {
			dims["strike"] = strconv.FormatFloat(s, 'f', -1, 64)
		}
		if m, ok := floatField(r, "maturity"); ok {
			dims["tenor"] = strconv.FormatFloat(m, 'f', -1, 64)
		}
		if isCall, ok := boolField(r, "is_call"); ok {
			if isCall {
				dims["side"] = "call"
			} else {
				dims["side"] = "put"
			}
		}
		dims = dimpath.MergeDefaults(dims, dimpath.Defaults)
		notional, _ := floatField(r, "notional")
		out = append(out, Point{Dims: dims, T: t, Value: Value{Scalar: notional}})
	}
	return out, nil
}

// tradesToPoints maps aria_book_price trades into one point per
// script. The script body is not itself a number, so we emit the
// notional as the scalar and attach the script body as raw bytes.
func tradesToPoints(jobName string, raw json.RawMessage, t time.Time) ([]Point, error) {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("trades: %w", err)
	}
	var out []Point
	for _, r := range rows {
		dims := map[string]string{
			"type":   "contract",
			"source": "sibelius",
			"name":   jobName,
		}
		dims = dimpath.MergeDefaults(dims, dimpath.Defaults)
		notional, _ := floatField(r, "notional")
		var script json.RawMessage
		if s, ok := r["script"]; ok {
			script = s
		}
		out = append(out, Point{Dims: dims, T: t, Value: Value{Scalar: notional, Raw: script}})
	}
	return out, nil
}

// jobPoint wraps an entire payload as one catch-all point — nothing
// drops silently.
func jobPoint(jobName string, data []byte) Point {
	dims := map[string]string{
		"type":   "sibelius_job",
		"source": "sibelius",
		"name":   jobName,
	}
	return Point{
		Dims:  dimpath.MergeDefaults(dims, dimpath.Defaults),
		T:     time.Now().UTC(),
		Value: Value{Raw: append(json.RawMessage(nil), data...)},
	}
}

// sharedDims extracts scenario-level dims (model, spot, rate, ...)
// from arguments[0] so instrument-level points can reference them.
func sharedDims(jobName string, args map[string]json.RawMessage) map[string]string {
	dims := map[string]string{
		"source": "sibelius",
		"name":   jobName,
	}
	if s, ok := stringField(args, "model"); ok {
		dims["model"] = s
	}
	if s, ok := floatField(args, "spot"); ok {
		dims["spot"] = strconv.FormatFloat(s, 'f', -1, 64)
	}
	if s, ok := floatField(args, "rate"); ok {
		dims["rate"] = strconv.FormatFloat(s, 'f', -1, 64)
	}
	return dims
}

func copyDims(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func floatField(m map[string]json.RawMessage, key string) (float64, bool) {
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, false
	}
	return f, true
}

func stringField(m map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func boolField(m map[string]json.RawMessage, key string) (bool, bool) {
	raw, ok := m[key]
	if !ok {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, false
	}
	return b, true
}
