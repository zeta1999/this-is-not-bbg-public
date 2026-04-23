package backfill

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
	"pgregory.net/rapid"
)

// TestRapid_DedupCollapsesConcurrent asserts the TLA+ analogue that
// N concurrent requests for the same (feed, exchange, instrument,
// timeframe, from, to) key trigger the underlying adapter exactly
// once — provided all N goroutines are in flight before the first
// adapter call completes. A start-barrier + a 50ms adapter delay
// make this true under the Go scheduler; without either, the last
// goroutines can arrive after the first job's cleanup and start a
// fresh call (which is correct coordinator behaviour, just not
// what the dedup property is asserting).
func TestRapid_DedupCollapsesConcurrent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 16).Draw(t, "concurrent")
		fake := &countBackfiller{
			delay:  50 * time.Millisecond,
			result: []feeds.OHLC{{Instrument: "X"}},
		}
		c := New(bus.New(64), map[string]feeds.Backfiller{"f": fake},
			FeedLimits{RequestsPerSec: 1000, Burst: 100})

		req := feeds.BackfillRequest{
			Instrument: "BTCUSDT", Exchange: "f", Timeframe: "1m",
			From: time.Unix(1000, 0), To: time.Unix(2000, 0),
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, _ = c.Request(context.Background(), "f", req)
			}()
		}
		close(start) // release all goroutines simultaneously
		wg.Wait()

		if got := fake.calls.Load(); got != 1 {
			t.Fatalf("underlying calls=%d, want 1 (concurrent=%d)", got, n)
		}
	})
}

// TestRapid_DistinctKeysAreIndependent: different request keys must
// not collapse onto each other. Exactly one call per distinct key.
func TestRapid_DistinctKeysAreIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		distinctInstruments := rapid.SliceOfNDistinct(
			rapid.StringMatching(`[A-Z]{3}USDT`),
			2, 6,
			func(s string) string { return s },
		).Draw(t, "instruments")

		fake := &countBackfiller{
			delay:  0,
			result: []feeds.OHLC{{}},
		}
		c := New(bus.New(64), map[string]feeds.Backfiller{"f": fake},
			FeedLimits{RequestsPerSec: 1000, Burst: 100})

		for _, inst := range distinctInstruments {
			req := feeds.BackfillRequest{
				Instrument: inst, Exchange: "f", Timeframe: "1m",
				From: time.Unix(1000, 0), To: time.Unix(2000, 0),
			}
			_, _ = c.Request(context.Background(), "f", req)
		}
		if got := fake.calls.Load(); int(got) != len(distinctInstruments) {
			t.Fatalf("calls=%d, want %d", got, len(distinctInstruments))
		}
	})
}

// countBackfiller is a test-only adapter that counts calls.
type countBackfiller struct {
	calls  atomic.Int32
	delay  time.Duration
	result []feeds.OHLC
}

func (c *countBackfiller) BackfillHistorical(ctx context.Context, req feeds.BackfillRequest) ([]feeds.OHLC, error) {
	c.calls.Add(1)
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.result, nil
}
