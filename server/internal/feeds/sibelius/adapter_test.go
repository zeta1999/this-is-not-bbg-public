package sibelius

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

func TestAdapter_PublishesPointsOnFileDrop(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"name":"AriaBookPrice/1","arguments":[{"trades":[{"notional":1000,"script":"SELL"}]}]}`)
	if err := os.WriteFile(filepath.Join(dir, "aria_test.json"), payload, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	b := bus.New(64)
	sub := b.Subscribe(16, "sibelius.*")
	defer b.Unsubscribe(sub)

	a := NewAdapter(b, Config{Path: dir, PollInterval: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() { _ = a.Start(ctx) }()

	select {
	case msg := <-sub.C:
		if msg.Topic == "" {
			t.Fatalf("empty topic")
		}
	case <-ctx.Done():
		t.Fatalf("no point published within timeout (status=%+v)", a.Status())
	}
}
