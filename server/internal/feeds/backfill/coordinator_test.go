package backfill

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// fakeBackfiller is a canned feeds.Backfiller for tests.
type fakeBackfiller struct {
	calls  atomic.Int32
	delay  time.Duration
	result []feeds.OHLC
	err    error
}

func (f *fakeBackfiller) BackfillHistorical(ctx context.Context, req feeds.BackfillRequest) ([]feeds.OHLC, error) {
	f.calls.Add(1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.result, f.err
}

func TestRequest_DispatchesRecordsAndProgress(t *testing.T) {
	b := bus.New(64)
	sub := b.Subscribe(32, "ohlc.*.*", "backfill.progress")
	defer b.Unsubscribe(sub)

	fake := &fakeBackfiller{result: []feeds.OHLC{
		{Instrument: "BTCUSDT", Exchange: "binance", Timeframe: "1m"},
		{Instrument: "BTCUSDT", Exchange: "binance", Timeframe: "1m"},
	}}
	c := New(b, map[string]feeds.Backfiller{"binance": fake},
		FeedLimits{RequestsPerSec: 100, Burst: 10})

	got, err := c.Request(context.Background(), "binance", feeds.BackfillRequest{
		Instrument: "BTCUSDT", Exchange: "binance", Timeframe: "1m",
		From: time.Now().Add(-time.Hour), To: time.Now(),
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 records, got %d", len(got))
	}

	// Collect bus messages for a brief moment.
	deadline := time.After(100 * time.Millisecond)
	var progress []Progress
	var ohlcs int
	for {
		select {
		case m := <-sub.C:
			switch m.Topic {
			case "backfill.progress":
				progress = append(progress, m.Payload.(Progress))
			default:
				if m.Topic[:5] == "ohlc." {
					ohlcs++
				}
			}
		case <-deadline:
			goto done
		}
	}
done:
	if ohlcs != 2 {
		t.Errorf("expected 2 ohlc publishes, got %d", ohlcs)
	}
	if len(progress) < 2 {
		t.Errorf("expected >=2 progress events (started + complete), got %d", len(progress))
	}
	stages := map[string]bool{}
	for _, p := range progress {
		stages[p.Stage] = true
	}
	if !stages["started"] || !stages["complete"] {
		t.Errorf("missing stages, got %v", stages)
	}
}

func TestRequest_DedupsInFlight(t *testing.T) {
	b := bus.New(64)
	fake := &fakeBackfiller{
		delay:  50 * time.Millisecond,
		result: []feeds.OHLC{{Instrument: "x"}},
	}
	c := New(b, map[string]feeds.Backfiller{"binance": fake},
		FeedLimits{RequestsPerSec: 100, Burst: 10})

	req := feeds.BackfillRequest{
		Instrument: "BTCUSDT", Exchange: "binance", Timeframe: "1m",
		From: time.Unix(1000, 0), To: time.Unix(2000, 0),
	}

	var wg sync.WaitGroup
	var errs [5]error
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.Request(context.Background(), "binance", req)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: %v", i, e)
		}
	}
	if got := fake.calls.Load(); got != 1 {
		t.Errorf("expected 1 underlying call, got %d", got)
	}
}

func TestRequest_UnknownFeed(t *testing.T) {
	b := bus.New(8)
	c := New(b, nil, FeedLimits{})
	_, err := c.Request(context.Background(), "nope", feeds.BackfillRequest{})
	if err == nil {
		t.Fatal("expected error for unknown feed")
	}
}

func TestRequest_PropagatesAdapterError(t *testing.T) {
	b := bus.New(8)
	want := errors.New("boom")
	fake := &fakeBackfiller{err: want}
	c := New(b, map[string]feeds.Backfiller{"binance": fake},
		FeedLimits{RequestsPerSec: 100, Burst: 10})

	_, err := c.Request(context.Background(), "binance", feeds.BackfillRequest{})
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestLimiter_BurstThenRateLimit(t *testing.T) {
	l := newLimiter(10, 2) // 10 rps, burst 2
	ctx := context.Background()

	start := time.Now()
	if err := l.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	// Third call must wait for refill (~100ms at 10 rps).
	if err := l.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Errorf("third call returned in %v; expected throttling", elapsed)
	}
}

func TestLimiter_ContextCancel(t *testing.T) {
	l := newLimiter(0.1, 1) // 1 token / 10s
	ctx, cancel := context.WithCancel(context.Background())

	// Burn the one burst token.
	if err := l.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	// Second call should block; cancel and verify we return quickly.
	done := make(chan error, 1)
	go func() { done <- l.Wait(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Wait did not return after cancel")
	}
}
