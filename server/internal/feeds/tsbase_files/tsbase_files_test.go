package tsbase_files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConsumeCSV_PublishesOnePerRow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "macro.csv"),
		"date,series,value\n2020-01-01,FEDFUNDS,0.5\n2020-02-01,FEDFUNDS,0.6\n")

	b := bus.New(16)
	sub := b.Subscribe(16, "tsbase.*")
	defer b.Unsubscribe(sub)

	tl := New(b, Config{Path: root})
	if err := tl.scanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(50 * time.Millisecond)
	var got int
loop:
	for {
		select {
		case <-sub.C:
			got++
		case <-deadline:
			break loop
		}
	}
	if got != 2 {
		t.Errorf("got %d bus messages, want 2", got)
	}
	if tl.Rows() != 2 {
		t.Errorf("Rows()=%d, want 2", tl.Rows())
	}
}

func TestConsumeJSONL_CursorAdvances(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.jsonl")

	writeFile(t, path, `{"a":1}`+"\n"+`{"a":2}`+"\n")

	b := bus.New(16)
	sub := b.Subscribe(16, "tsbase.*")
	defer b.Unsubscribe(sub)

	tl := New(b, Config{Path: root})
	_ = tl.scanOnce(context.Background())

	// Drain first pass.
	first := drain(sub.C)
	if len(first) != 2 {
		t.Fatalf("first pass: got %d", len(first))
	}

	// Append one more line. Cursor must skip the old two.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`{"a":3}` + "\n")
	_ = f.Close()

	_ = tl.scanOnce(context.Background())
	second := drain(sub.C)
	if len(second) != 1 {
		t.Errorf("second pass: got %d, want 1 (should skip already-consumed rows)", len(second))
	}
}

func TestConsumeJSONL_HandlesRotation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.jsonl")
	writeFile(t, path, `{"a":1}`+"\n"+`{"a":2}`+"\n")

	b := bus.New(16)
	sub := b.Subscribe(16, "tsbase.*")
	defer b.Unsubscribe(sub)

	tl := New(b, Config{Path: root})
	_ = tl.scanOnce(context.Background())
	_ = drain(sub.C)

	// Rotate: truncate + write one row.
	if err := os.WriteFile(path, []byte(`{"a":9}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = tl.scanOnce(context.Background())
	got := drain(sub.C)
	if len(got) != 1 {
		t.Errorf("after rotation: got %d, want 1", len(got))
	}
}

func TestConsumeParquet_SkippedWithWarning(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "trades.parquet"), "ignored")

	b := bus.New(8)
	tl := New(b, Config{Path: root})
	if err := tl.scanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tl.Rows() != 0 {
		t.Errorf("parquet should not publish rows, got %d", tl.Rows())
	}
}

func drain(ch chan bus.Message) []bus.Message {
	deadline := time.After(50 * time.Millisecond)
	var msgs []bus.Message
	for {
		select {
		case m := <-ch:
			msgs = append(msgs, m)
		case <-deadline:
			return msgs
		}
	}
}
