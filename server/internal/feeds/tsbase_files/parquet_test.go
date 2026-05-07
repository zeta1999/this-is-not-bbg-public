package tsbase_files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	parquet "github.com/parquet-go/parquet-go"

	"github.com/notbbg/notbbg/server/internal/bus"
)

// parquetRow is a tiny fixed schema the test writes out and then
// asks the tailer to decode.
type parquetRow struct {
	Symbol string  `parquet:"symbol"`
	Price  float64 `parquet:"price"`
	Qty    int64   `parquet:"qty"`
}

func writeTestParquet(t *testing.T, path string, rows []parquetRow) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := parquet.NewGenericWriter[parquetRow](f)
	if _, err := w.Write(rows); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConsumeParquet_RoundTrip(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "trades.parquet")
	writeTestParquet(t, path, []parquetRow{
		{Symbol: "BTCUSDT", Price: 50000.5, Qty: 3},
		{Symbol: "ETHUSDT", Price: 3300.25, Qty: 10},
		{Symbol: "SOLUSDT", Price: 150.0, Qty: 42},
	})

	b := bus.New(16)
	sub := b.Subscribe(16, "tsbase.trades")
	defer b.Unsubscribe(sub)

	tl := New(b, Config{Path: root, Interval: time.Hour})
	if err := tl.scanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := drain(sub.C)
	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3 (rows=%d)", len(got), tl.Rows())
	}
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	for i, m := range got {
		if m.Topic != "tsbase.trades" {
			t.Errorf("msg %d topic=%q, want tsbase.trades", i, m.Topic)
		}
		payload, ok := m.Payload.(map[string]any)
		if !ok {
			t.Fatalf("msg %d payload not map, got %T", i, m.Payload)
		}
		if payload["symbol"] != symbols[i] {
			t.Errorf("msg %d symbol=%v, want %v", i, payload["symbol"], symbols[i])
		}
	}
}

func TestConsumeParquet_IdempotentOnUnchanged(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.parquet")
	writeTestParquet(t, path, []parquetRow{{Symbol: "X", Price: 1, Qty: 1}})

	b := bus.New(8)
	tl := New(b, Config{Path: root, Interval: time.Hour})
	_ = tl.scanOnce(context.Background())
	if tl.Rows() != 1 {
		t.Fatalf("first scan Rows=%d, want 1", tl.Rows())
	}
	_ = tl.scanOnce(context.Background())
	if tl.Rows() != 1 {
		t.Fatalf("second scan Rows=%d, want 1 (no re-publish when unchanged)", tl.Rows())
	}
}
