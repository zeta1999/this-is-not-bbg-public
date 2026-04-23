package tsbase_files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

func TestAdapter_PublishesJSONL(t *testing.T) {
	dir := t.TempDir()
	line := []byte(`{"instrument":"BTCUSDT","exchange":"binance","price":123.45}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, "trades.jsonl"), line, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	b := bus.New(64)
	sub := b.Subscribe(16, "tsbase.*")
	defer b.Unsubscribe(sub)

	a := NewAdapter(b, Config{Path: dir, Interval: 50 * time.Millisecond})
	if a.Name() != "tsbase_files" {
		t.Fatalf("bad name %s", a.Name())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() { _ = a.Start(ctx) }()

	select {
	case <-sub.C:
		// ok
	case <-ctx.Done():
		t.Fatalf("no message published within timeout (status=%+v)", a.Status())
	}
}
