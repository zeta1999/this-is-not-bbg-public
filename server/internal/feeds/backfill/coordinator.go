// Package backfill coordinates on-demand historical data requests
// against adapters that implement feeds.Backfiller. It handles:
//
//   - de-duplication of overlapping in-flight requests
//   - per-feed rate limiting (token bucket)
//   - progress events published on the bus so clients see "loading
//     N/M" indicators without polling
//   - result dispatch to the bus (cache + datalake capture for free)
//
// See DATA-PLAN.md §4.
package backfill

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// limiter is a tiny token-bucket rate limiter. One token drips in
// every `interval`; up to `burst` tokens can accumulate. Wait blocks
// until a token is available or ctx is cancelled.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	burst    int
	tokens   int
	last     time.Time
}

func newLimiter(rps float64, burst int) *limiter {
	if rps <= 0 {
		rps = 1
	}
	if burst <= 0 {
		burst = 1
	}
	return &limiter{
		interval: time.Duration(float64(time.Second) / rps),
		burst:    burst,
		tokens:   burst, // start full so the first call is free
		last:     time.Now(),
	}
}

func (l *limiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// Refill.
	elapsed := now.Sub(l.last)
	gained := int(elapsed / l.interval)
	if gained > 0 {
		l.tokens += gained
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.last = l.last.Add(time.Duration(gained) * l.interval)
	}
	if l.tokens > 0 {
		l.tokens--
		return 0
	}
	// No tokens — caller must wait for the next refill.
	return l.interval - now.Sub(l.last)
}

func (l *limiter) Wait(ctx context.Context) error {
	for {
		wait := l.reserve()
		if wait == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// Coordinator serialises backfill requests and publishes results.
type Coordinator struct {
	bus      *bus.Bus
	mu       sync.Mutex
	inflight map[string]*job
	feeds    map[string]feeds.Backfiller
	limiters map[string]*limiter
	defaults FeedLimits
}

// FeedLimits configures the per-feed token bucket used to protect
// upstream exchanges from burst traffic.
type FeedLimits struct {
	RequestsPerSec float64
	Burst          int
}

// New builds a Coordinator. feedsByName maps adapter names to their
// Backfiller capability. limits applies to feeds without an explicit
// override.
func New(b *bus.Bus, feedsByName map[string]feeds.Backfiller, defaults FeedLimits) *Coordinator {
	if defaults.RequestsPerSec <= 0 {
		defaults.RequestsPerSec = 5 // conservative default
	}
	if defaults.Burst <= 0 {
		defaults.Burst = 5
	}
	c := &Coordinator{
		bus:      b,
		inflight: make(map[string]*job),
		feeds:    feedsByName,
		limiters: make(map[string]*limiter),
		defaults: defaults,
	}
	for name := range feedsByName {
		c.limiters[name] = newLimiter(defaults.RequestsPerSec, defaults.Burst)
	}
	return c
}

// SetFeedLimit overrides the rate limit for a specific feed.
func (c *Coordinator) SetFeedLimit(feed string, rps float64, burst int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limiters[feed] = newLimiter(rps, burst)
}

type job struct {
	req     feeds.BackfillRequest
	feed    string
	done    chan struct{}
	records []feeds.OHLC
	err     error
}

// Request serves a backfill. If an equivalent request is already
// in-flight the caller waits on its result (dedup). Publishes a
// backfill-progress event to the bus when the job starts and a
// backfill-complete event (plus one ohlc.* per record) when it
// finishes. Synchronous: returns when the job is done.
func (c *Coordinator) Request(ctx context.Context, feed string, req feeds.BackfillRequest) ([]feeds.OHLC, error) {
	key := jobKey(feed, req)

	c.mu.Lock()
	if existing, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-existing.done:
			return existing.records, existing.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	adapter, ok := c.feeds[feed]
	if !ok {
		c.mu.Unlock()
		return nil, fmt.Errorf("feed %q does not support backfill", feed)
	}
	lim, hasLim := c.limiters[feed]
	if !hasLim {
		lim = newLimiter(c.defaults.RequestsPerSec, c.defaults.Burst)
		c.limiters[feed] = lim
	}
	j := &job{req: req, feed: feed, done: make(chan struct{})}
	c.inflight[key] = j
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.inflight, key)
		c.mu.Unlock()
		close(j.done)
	}()

	c.publishProgress(feed, req, "started", 0)

	if err := lim.Wait(ctx); err != nil {
		j.err = fmt.Errorf("rate-limit wait: %w", err)
		c.publishProgress(feed, req, "failed", 0)
		return nil, j.err
	}

	records, err := adapter.BackfillHistorical(ctx, req)
	if err != nil {
		j.err = err
		c.publishProgress(feed, req, "failed", 0)
		return nil, err
	}
	j.records = records

	// Dispatch records onto the bus so cache + datalake capture for
	// free. Clients also see them via any live subscription.
	for _, o := range records {
		c.bus.Publish(bus.Message{
			Topic:   fmt.Sprintf("ohlc.%s.%s", o.Exchange, o.Instrument),
			Payload: o,
		})
	}
	c.publishProgress(feed, req, "complete", len(records))

	slog.Info("backfill complete",
		"feed", feed, "instrument", req.Instrument,
		"timeframe", req.Timeframe, "records", len(records))

	return records, nil
}

// publishProgress emits a "backfill.progress" event so UI panels can
// show a live loading indicator.
func (c *Coordinator) publishProgress(feed string, req feeds.BackfillRequest, stage string, count int) {
	c.bus.Publish(bus.Message{
		Topic: "backfill.progress",
		Payload: Progress{
			Feed:       feed,
			Instrument: req.Instrument,
			Timeframe:  req.Timeframe,
			From:       req.From,
			To:         req.To,
			Stage:      stage,
			Records:    count,
		},
	})
}

// Progress is the payload published on "backfill.progress".
type Progress struct {
	Feed       string    `json:"feed"`
	Instrument string    `json:"instrument"`
	Timeframe  string    `json:"timeframe"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	Stage      string    `json:"stage"` // "started" | "complete" | "failed"
	Records    int       `json:"records"`
}

func jobKey(feed string, req feeds.BackfillRequest) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d-%d",
		feed, req.Exchange, req.Instrument, req.Timeframe,
		req.From.UnixMilli(), req.To.UnixMilli())
}
