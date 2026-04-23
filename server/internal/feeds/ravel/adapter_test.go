package ravel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

func TestAdapter_PublishesJSON(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"now":"2024-01-02T00:00:00Z","rates":{"USD":{"rates":[{"maturity":"2024-04-01T00:00:00Z","t":0.25,"r":0.05}]}}}`)
	if err := os.WriteFile(filepath.Join(dir, "rates.json"), payload, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	b := bus.New(64)
	sub := b.Subscribe(16, "ravel.*")
	defer b.Unsubscribe(sub)

	a := NewAdapter(b, Config{Path: dir, PollInterval: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() { _ = a.Start(ctx) }()

	select {
	case msg := <-sub.C:
		if msg.Topic != "ravel.rate" {
			t.Fatalf("unexpected topic %q", msg.Topic)
		}
	case <-ctx.Done():
		t.Fatalf("no point published within timeout (status=%+v)", a.Status())
	}
}
