// Package dimpath builds and parses canonical paths for the
// dim-ordered datalake layout. A path segment has the shape
//
//	dims=type=ohlc;exchange=binance;instrument=BTCUSDT;tenor=spot
//
// Dims are sorted by key so the path is canonical — any two callers
// that hand us the same map produce the same path. Unknown dims pass
// through; empty-string defaults are dropped so the path stays tight.
//
// See DATA-PLAN.md §5.
package dimpath

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Build returns the canonical path segment for the given dims.
// Empty-valued dims are omitted so zero-value maps produce "dims=".
func Build(dims map[string]string) string {
	if len(dims) == 0 {
		return "dims="
	}
	keys := make([]string, 0, len(dims))
	for k, v := range dims {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("dims=")
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(dims[k]))
	}
	return b.String()
}

// Parse decodes a path segment produced by Build. Returns an error
// if the segment is malformed. Missing "dims=" prefix is rejected so
// callers don't accidentally parse a date segment.
func Parse(segment string) (map[string]string, error) {
	if !strings.HasPrefix(segment, "dims=") {
		return nil, fmt.Errorf("segment %q missing dims= prefix", segment)
	}
	body := strings.TrimPrefix(segment, "dims=")
	out := make(map[string]string)
	if body == "" {
		return out, nil
	}
	for _, kv := range strings.Split(body, ";") {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			return nil, fmt.Errorf("malformed pair %q", kv)
		}
		k, err := url.QueryUnescape(kv[:eq])
		if err != nil {
			return nil, fmt.Errorf("decode key %q: %w", kv[:eq], err)
		}
		v, err := url.QueryUnescape(kv[eq+1:])
		if err != nil {
			return nil, fmt.Errorf("decode value for %q: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

// Defaults is the built-in fallback map for well-known dim keys.
// When an adapter produces a record without one of these keys, the
// writer substitutes the default so historical reads stay
// consistent. Callers can override by loading their own map and
// passing it to MergeDefaults.
var Defaults = map[string]string{
	"type":       "unknown",
	"tenor":      "spot",
	"asset":      "",
	"pair":       "",
	"source":     "",
	"exchange":   "",
	"instrument": "",
	"strike":     "",
	"side":       "",
}

// MergeDefaults fills in any missing keys from defaults. Existing
// values win; empty strings in dims are treated as absent so the
// default applies.
func MergeDefaults(dims, defaults map[string]string) map[string]string {
	out := make(map[string]string, len(dims)+len(defaults))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range dims {
		if v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// FromLegacyTopic maps a legacy bus topic (type.exchange.instrument,
// type.source, or type) into the dim set that would have been
// produced had the record been written under the new layout from
// the start. Used by datamigrate. Unknown shapes yield only the
// `type` dim.
func FromLegacyTopic(topic string) map[string]string {
	parts := strings.SplitN(topic, ".", 3)
	out := map[string]string{"type": parts[0]}
	switch len(parts) {
	case 2:
		// "news.reuters" style — second slot is a source.
		out["source"] = parts[1]
	case 3:
		out["exchange"] = parts[1]
		out["instrument"] = parts[2]
	}
	return out
}
