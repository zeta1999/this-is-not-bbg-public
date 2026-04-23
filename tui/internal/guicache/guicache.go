// Package guicache provides bounded, per-panel client caches. Each
// type here enforces a cap so the TUI never grows its memory when
// the user scrolls instruments, timeframes, or tapes.
//
// Semantics are mirrored by the desktop app (Tauri/React) — see
// DATA-PLAN.md §3 and feedback_desktop_app.
package guicache

import (
	"sort"
	"sync"
	"time"
)

// OHLCRow is the shape stored by OHLCCache. Keeping a minimal struct
// here (rather than importing the server's proto/pb types) so the
// package has no heavy dependencies.
type OHLCRow struct {
	Timestamp            time.Time
	Open, High, Low, Close float64
	Volume               float64
}

// OHLCCache holds time-ordered OHLC rows keyed by (instrument,
// timeframe). Each key is capped at Limit rows; oldest rows are
// evicted when the cap is exceeded.
type OHLCCache struct {
	Limit int

	mu   sync.RWMutex
	data map[string][]OHLCRow
}

// NewOHLC builds an OHLCCache with the given per-key cap.
func NewOHLC(limit int) *OHLCCache {
	if limit <= 0 {
		limit = 1
	}
	return &OHLCCache{Limit: limit, data: make(map[string][]OHLCRow)}
}

// Add inserts row under (instrument, timeframe) keeping rows ordered
// by Timestamp ascending. If a row with the same Timestamp exists it
// is updated in place (common when feeds republish the current
// candle).
func (c *OHLCCache) Add(instrument, timeframe string, row OHLCRow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := instrument + "|" + timeframe
	rows := c.data[key]

	// Binary search for insertion point.
	i := sort.Search(len(rows), func(i int) bool {
		return !rows[i].Timestamp.Before(row.Timestamp)
	})
	if i < len(rows) && rows[i].Timestamp.Equal(row.Timestamp) {
		rows[i] = row
	} else {
		rows = append(rows, OHLCRow{})
		copy(rows[i+1:], rows[i:])
		rows[i] = row
	}

	// Evict oldest if over cap.
	if len(rows) > c.Limit {
		rows = rows[len(rows)-c.Limit:]
	}
	c.data[key] = rows
}

// Rows returns a copy of the rows for (instrument, timeframe). Safe
// to call concurrently with Add.
func (c *OHLCCache) Rows(instrument, timeframe string) []OHLCRow {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rows := c.data[instrument+"|"+timeframe]
	out := make([]OHLCRow, len(rows))
	copy(out, rows)
	return out
}

// Drop removes all rows for (instrument, timeframe).
func (c *OHLCCache) Drop(instrument, timeframe string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, instrument+"|"+timeframe)
}

// --- Trades (per-instrument ring buffer) ---

// Trade is what TradesRing stores.
type Trade struct {
	Timestamp time.Time
	Price     float64
	Quantity  float64
	Side      string // "buy" | "sell"
	ID        string
}

// TradesRing is a per-instrument bounded trade tape. Oldest trades
// are discarded when the ring is full.
type TradesRing struct {
	Limit int

	mu   sync.RWMutex
	data map[string]*ring[Trade]
}

// NewTrades builds a TradesRing with the given per-instrument cap.
func NewTrades(limit int) *TradesRing {
	if limit <= 0 {
		limit = 1
	}
	return &TradesRing{Limit: limit, data: make(map[string]*ring[Trade])}
}

// Add appends a trade for the instrument. O(1).
func (t *TradesRing) Add(instrument string, tr Trade) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.data[instrument]
	if !ok {
		r = newRing[Trade](t.Limit)
		t.data[instrument] = r
	}
	r.push(tr)
}

// Recent returns up to n trades (newest first). Safe under concurrency.
func (t *TradesRing) Recent(instrument string, n int) []Trade {
	t.mu.RLock()
	defer t.mu.RUnlock()
	r, ok := t.data[instrument]
	if !ok {
		return nil
	}
	return r.recent(n)
}

// --- LOB (bounded depth) ---

// LOBLevel is one price level.
type LOBLevel struct {
	Price    float64
	Quantity float64
}

// LOB holds the top-N bids and asks for a single instrument. Bids
// sorted descending by price, asks ascending.
type LOB struct {
	DepthLimit int

	mu   sync.RWMutex
	data map[string]*lobState
}

type lobState struct {
	bids []LOBLevel
	asks []LOBLevel
}

// NewLOB builds a LOB with a per-side depth cap.
func NewLOB(depth int) *LOB {
	if depth <= 0 {
		depth = 1
	}
	return &LOB{DepthLimit: depth, data: make(map[string]*lobState)}
}

// Set replaces the book for instrument, trimming to DepthLimit
// levels per side.
func (l *LOB) Set(instrument string, bids, asks []LOBLevel) {
	// Copy + sort so callers can hand us unsorted slices.
	b := append([]LOBLevel(nil), bids...)
	a := append([]LOBLevel(nil), asks...)
	sort.Slice(b, func(i, j int) bool { return b[i].Price > b[j].Price })
	sort.Slice(a, func(i, j int) bool { return a[i].Price < a[j].Price })
	if len(b) > l.DepthLimit {
		b = b[:l.DepthLimit]
	}
	if len(a) > l.DepthLimit {
		a = a[:l.DepthLimit]
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.data[instrument] = &lobState{bids: b, asks: a}
}

// Snapshot returns copies of the current bids/asks for an instrument.
func (l *LOB) Snapshot(instrument string) (bids, asks []LOBLevel) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s, ok := l.data[instrument]
	if !ok {
		return nil, nil
	}
	bids = append(bids, s.bids...)
	asks = append(asks, s.asks...)
	return bids, asks
}

// --- generic ring buffer ---

type ring[T any] struct {
	buf  []T
	pos  int
	full bool
}

func newRing[T any](n int) *ring[T] {
	return &ring[T]{buf: make([]T, n)}
}

func (r *ring[T]) push(v T) {
	r.buf[r.pos] = v
	r.pos = (r.pos + 1) % len(r.buf)
	if r.pos == 0 {
		r.full = true
	}
}

// recent returns up to n elements, newest first.
func (r *ring[T]) recent(n int) []T {
	size := len(r.buf)
	if !r.full {
		size = r.pos
	}
	if n <= 0 || n > size {
		n = size
	}
	out := make([]T, 0, n)
	// Walk backwards from the most-recent slot.
	idx := r.pos - 1
	if idx < 0 {
		idx = len(r.buf) - 1
	}
	for i := 0; i < n; i++ {
		out = append(out, r.buf[idx])
		idx--
		if idx < 0 {
			idx = len(r.buf) - 1
		}
	}
	return out
}
