package datalake

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// FuzzReader feeds arbitrary bytes through the Query path — corrupt
// JSONL, mid-file truncation, garbage envelopes, empty files, giant
// lines. Invariant: Query must never panic, and must return a
// non-nil (possibly empty) slice with no error for malformed input
// (malformed lines are silently skipped — this is the contract).
func FuzzReader(f *testing.F) {
	// Seed with shapes we know about.
	f.Add([]byte(`{"_topic":"ohlc.binance.BTCUSDT","_timestamp":"2026-04-22T00:00:00Z","payload":{}}` + "\n"))
	f.Add([]byte(""))
	f.Add([]byte("\n\n\n"))
	f.Add([]byte("not json\n{partial"))
	// Oversized single line — just under the 1 MiB scanner buffer.
	big := make([]byte, 900_000)
	for i := range big {
		big[i] = 'x'
	}
	f.Add(append([]byte(`{"_topic":"ohlc.x.y","payload":"`), append(big, []byte(`"}` + "\n")...)...))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Build a minimal Hive layout that Query() will actually visit.
		root := t.TempDir()
		day := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
		dir := filepath.Join(root,
			"type=ohlc", "exchange=fuzz", "instrument=FUZZ",
			"year=2026", "month=04", "day=22")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "data.jsonl")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}

		r := NewReader(root)
		// Invariants: Query must not panic, must not return an error on
		// malformed records (it's documented to skip them), and must
		// respect the limit.
		const limit = 1000
		recs, err := r.Query("ohlc", "fuzz", "FUZZ", day, day, limit)
		if err != nil {
			t.Fatalf("unexpected Query error on fuzz input: %v", err)
		}
		if len(recs) > limit {
			t.Fatalf("Query returned %d records, exceeds limit %d", len(recs), limit)
		}
	})
}
