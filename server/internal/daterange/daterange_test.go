package daterange

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFixture(t *testing.T, root string, topic string, records int) {
	t.Helper()
	parts := splitTopic(topic)
	dir := filepath.Join(root,
		"type="+parts[0],
		"exchange="+parts[1],
		"instrument="+parts[2],
		"year=2026", "month=04", "day=22")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "data.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := 0; i < records; i++ {
		fmt.Fprintf(f, `{"_topic":%q,"_timestamp":"2026-04-22T00:00:%02dZ","payload":{"n":%d}}`+"\n",
			topic, i%60, i)
	}
}

func splitTopic(t string) []string {
	// Tiny helper for tests only; production code uses parseTopic.
	out := []string{"", "", ""}
	i := 0
	cur := ""
	for _, r := range t {
		if r == '.' && i < 2 {
			out[i] = cur
			cur = ""
			i++
			continue
		}
		cur += string(r)
	}
	out[i] = cur
	return out
}

func TestServe_ChunksUntilEOF(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "ohlc.binance.BTCUSDT", 1200)

	h := New(root)
	day := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	req := Request{
		Topic: "ohlc.binance.BTCUSDT",
		From:  day, To: day,
		CorrelationID: "c1",
	}

	var chunks []Chunk
	err := h.Serve(context.Background(), req, func(c Chunk) error {
		chunks = append(chunks, c)
		return nil
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}

	// 1200 records / 500 per chunk = 3 data chunks (500, 500, 200) + 1 EOF chunk.
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks, want 4: %+v", len(chunks), chunks)
	}
	last := chunks[len(chunks)-1]
	if !last.EOF {
		t.Error("last chunk should have EOF=true")
	}
	if last.CorrelationID != "c1" {
		t.Errorf("correlation_id lost: %q", last.CorrelationID)
	}
	// Total records across non-EOF chunks.
	total := 0
	for _, c := range chunks {
		total += len(c.Records)
	}
	if total > 1200 {
		t.Errorf("returned more records than written: %d", total)
	}
}

func TestServe_EmptyRangeStillEOF(t *testing.T) {
	root := t.TempDir()
	h := New(root)
	day := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)

	var got []Chunk
	err := h.Serve(context.Background(), Request{
		Topic: "ohlc.nope.NOPE", From: day, To: day, CorrelationID: "x",
	}, func(c Chunk) error { got = append(got, c); return nil })
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if len(got) != 1 || !got[0].EOF {
		t.Errorf("empty window should deliver exactly one EOF chunk, got %+v", got)
	}
}

func TestServe_CancellationStopsMidStream(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "ohlc.binance.BTCUSDT", 2000)

	ctx, cancel := context.WithCancel(context.Background())
	h := New(root)
	day := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)

	var chunks []Chunk
	err := h.Serve(ctx, Request{
		Topic: "ohlc.binance.BTCUSDT", From: day, To: day,
	}, func(c Chunk) error {
		chunks = append(chunks, c)
		cancel() // cancel after first chunk
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if len(chunks) == 0 {
		t.Error("expected at least one chunk before cancel")
	}
}

func TestServe_ValidationErrors(t *testing.T) {
	h := New(t.TempDir())
	noop := func(Chunk) error { return nil }

	cases := map[string]Request{
		"empty topic":        {From: time.Now(), To: time.Now()},
		"zero from":          {Topic: "ohlc.x.y", To: time.Now()},
		"to before from":     {Topic: "ohlc.x.y", From: time.Now(), To: time.Now().Add(-time.Hour)},
	}
	for name, req := range cases {
		if err := h.Serve(context.Background(), req, noop); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestParseTopic(t *testing.T) {
	dt, ex, inst, _ := parseTopic("ohlc.binance.BTCUSDT")
	if dt != "ohlc" || ex != "binance" || inst != "BTCUSDT" {
		t.Errorf("3-part: got (%q,%q,%q)", dt, ex, inst)
	}
	dt, ex, inst, _ = parseTopic("news")
	if dt != "news" || ex != "" || inst != "" {
		t.Errorf("1-part: got (%q,%q,%q)", dt, ex, inst)
	}
}
