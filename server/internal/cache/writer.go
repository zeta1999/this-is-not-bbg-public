// Package cache provides a bus subscriber that persists messages to BBolt.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
	"github.com/notbbg/notbbg/server/internal/persistq"
)

// WALStats is the payload published periodically on `wal.cache.stats`
// so the MON panel can surface WAL depth and drop counts without
// scraping logs. Datalake uses the same shape under `wal.datalake.stats`.
type WALStats struct {
	Name        string `json:"name"`         // "cache" | "datalake"
	Enqueued    uint64 `json:"enqueued"`     // records enqueued since last tick
	BusDropped  uint64 `json:"bus_dropped"`  // cumulative bus-level drops
	WALDropped  uint64 `json:"wal_dropped"`  // cumulative WAL-level drops (cap exceeded)
	BytesOnDisk int64  `json:"bytes_on_disk"`
	Segments    int    `json:"segments"`
	Enabled     bool   `json:"enabled"` // false in direct mode
}

// Writer subscribes to the bus and persists messages to the cache store.
//
// Two run modes:
//
//   - Direct: bus sub → store.Put inline. Historical default; simple
//     but blocks the bus subscriber when BBolt is slow, which can
//     propagate back to the bus as drops.
//   - WAL-backed: bus sub → persistq (disk-spill) → drainer →
//     store.Put. Enable via EnableWAL. Absorbs BBolt stalls up to
//     the WAL byte budget without dropping on the bus side.
type Writer struct {
	store *Store
	bus   *bus.Bus

	walDir   string
	walBytes int64
}

// NewWriter creates a cache writer in direct mode. Call EnableWAL to
// opt into disk-spill WAL.
func NewWriter(store *Store, b *bus.Bus) *Writer {
	return &Writer{store: store, bus: b}
}

// EnableWAL configures the writer to route traffic through a
// disk-spill WAL rooted at dir with the given byte budget. Must be
// called before Run.
func (w *Writer) EnableWAL(dir string, capBytes int64) *Writer {
	w.walDir = dir
	w.walBytes = capBytes
	return w
}

// record is the WAL envelope: everything store.Put needs. Keeping
// the schema flat means the drainer can write bytes-to-bytes without
// reflecting on payload types, which is cheaper and avoids tying the
// WAL format to the in-process type registry.
type record struct {
	Bucket string          `json:"b"`
	Key    string          `json:"k"`
	Data   json.RawMessage `json:"d"`
}

// Run subscribes to all data topics and writes incoming messages to BBolt.
// Blocks until ctx is cancelled.
func (w *Writer) Run(ctx context.Context) error {
	// `ohlc-historical.*.*` carries backfill replays. They share the
	// same payload shape (and therefore the same persisted bucket /
	// key derived from Exchange+Instrument+Timeframe+Timestamp) so
	// the cache index covers both prefixes. Live SSE consumers stay
	// on `ohlc.*.*` and don't see backfills.
	topics := []string{"ohlc.*.*", "ohlc-historical.*.*", "trade.*.*", "lob.*.*", "news", "alert"}
	sub := w.bus.Subscribe(1024, topics...)
	defer w.bus.Unsubscribe(sub)

	if w.walDir == "" {
		return w.runDirect(ctx, sub)
	}
	return w.runWAL(ctx, sub)
}

// runDirect is the pre-Phase-5 behavior: persist inline.
func (w *Writer) runDirect(ctx context.Context, sub *bus.Subscriber) error {
	var written uint64
	logTicker := time.NewTicker(30 * time.Second)
	defer logTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("cache writer stopped",
				"written", written,
				"bus_dropped", sub.Dropped())
			return nil

		case <-logTicker.C:
			dropped := sub.Dropped()
			if written > 0 || dropped > 0 {
				slog.Info("cache writer stats",
					"written", written,
					"bus_dropped", dropped)
				written = 0
			}

		case msg, ok := <-sub.C:
			if !ok {
				return nil
			}
			rec, ok := w.buildRecord(msg)
			if !ok {
				continue
			}
			if err := w.store.Put(rec.Bucket, rec.Key, rec.Data); err != nil {
				slog.Debug("cache write error", "topic", msg.Topic, "error", err)
				continue
			}
			written++
		}
	}
}

// runWAL splits into producer (bus → WAL) + drainer (WAL → BBolt).
// The WAL absorbs BBolt stalls up to its byte budget; drops beyond
// that are counted separately from bus-level drops so the operator
// can distinguish bus overflow (subscriber too slow to enqueue) from
// WAL overflow (downstream BBolt durably broken).
func (w *Writer) runWAL(ctx context.Context, sub *bus.Subscriber) error {
	q, err := persistq.Open(persistq.Options{
		Dir:      w.walDir,
		CapBytes: w.walBytes,
	})
	if err != nil {
		return fmt.Errorf("cache writer: open WAL: %w", err)
	}
	defer q.Close()

	drainerDone := make(chan struct{})
	drainerCtx, cancelDrainer := context.WithCancel(context.Background())
	go func() {
		defer close(drainerDone)
		w.drainWAL(drainerCtx, q)
	}()

	var enqueued, walDropped uint64
	logTicker := time.NewTicker(30 * time.Second)
	defer logTicker.Stop()

	producerLoop := func() {
		for {
			select {
			case <-ctx.Done():
				return

			case <-logTicker.C:
				st := q.Stats()
				busDropped := sub.Dropped()
				slog.Info("cache writer stats (WAL)",
					"enqueued", enqueued,
					"bus_dropped", busDropped,
					"wal_dropped", walDropped,
					"wal_bytes", st.BytesOnDisk,
					"wal_segments", st.Segments,
				)
				w.bus.Publish(bus.Message{
					Topic: "wal.cache.stats",
					Payload: WALStats{
						Name:        "cache",
						Enqueued:    enqueued,
						BusDropped:  busDropped,
						WALDropped:  walDropped,
						BytesOnDisk: st.BytesOnDisk,
						Segments:    st.Segments,
						Enabled:     true,
					},
				})
				enqueued = 0

			case msg, ok := <-sub.C:
				if !ok {
					return
				}
				rec, ok := w.buildRecord(msg)
				if !ok {
					continue
				}
				payload, err := json.Marshal(rec)
				if err != nil {
					continue
				}
				if err := q.Enqueue(payload); err != nil {
					if errors.Is(err, persistq.ErrFull) {
						walDropped++
						continue
					}
					slog.Debug("WAL enqueue error", "error", err)
				}
				enqueued++
			}
		}
	}

	producerLoop()

	// Allow the drainer a brief window to finish flushing before we
	// close. If BBolt is truly stuck, we give up after the timeout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	select {
	case <-shutdownCtx.Done():
	case <-drainerDone:
	}
	cancelDrainer()
	<-drainerDone
	slog.Info("cache writer stopped",
		"bus_dropped", sub.Dropped(),
		"wal_dropped", walDropped)
	return nil
}

// drainWAL reads records from the WAL and persists them to BBolt.
// Runs until its own context is cancelled. Errors from store.Put are
// logged but do not halt the loop — the record has already advanced
// past the WAL cursor, so surfacing the error is all we can do.
func (w *Writer) drainWAL(ctx context.Context, q *persistq.Queue) {
	for {
		payload, err := q.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, persistq.ErrClosed) {
				return
			}
			slog.Debug("WAL dequeue error", "error", err)
			continue
		}
		var rec record
		if err := json.Unmarshal(payload, &rec); err != nil {
			slog.Debug("WAL decode error", "error", err)
			continue
		}
		if err := w.store.Put(rec.Bucket, rec.Key, rec.Data); err != nil {
			slog.Debug("cache write error", "bucket", rec.Bucket, "key", rec.Key, "error", err)
		}
	}
}

// buildRecord turns a bus.Message into a persist-ready record. Drops
// unknown payload types silently — mirrors the historical behavior
// where persist returned nil for unhandled cases.
func (w *Writer) buildRecord(msg bus.Message) (record, bool) {
	switch v := msg.Payload.(type) {
	case feeds.OHLC:
		data, err := json.Marshal(v)
		if err != nil {
			return record{}, false
		}
		return record{
			Bucket: "ohlc",
			Key:    fmt.Sprintf("%s/%s/%s/%d", v.Exchange, v.Instrument, v.Timeframe, v.Timestamp.UnixNano()),
			Data:   data,
		}, true

	case feeds.Trade:
		data, err := json.Marshal(v)
		if err != nil {
			return record{}, false
		}
		return record{
			Bucket: "trades",
			Key:    fmt.Sprintf("%s/%s/%s/%d", v.Exchange, v.Instrument, v.TradeID, v.Timestamp.UnixNano()),
			Data:   data,
		}, true

	case feeds.LOBSnapshot:
		data, err := json.Marshal(v)
		if err != nil {
			return record{}, false
		}
		return record{
			Bucket: "lob_snapshots",
			Key:    fmt.Sprintf("%s/%s/%d", v.Exchange, v.Instrument, v.Timestamp.UnixNano()),
			Data:   data,
		}, true
	}
	return record{}, false
}
