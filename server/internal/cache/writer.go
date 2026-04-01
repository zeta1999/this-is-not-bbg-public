// Package cache provides a bus subscriber that persists messages to BBolt.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// Writer subscribes to the bus and persists messages to the cache store.
type Writer struct {
	store *Store
	bus   *bus.Bus
}

// NewWriter creates a cache writer.
func NewWriter(store *Store, b *bus.Bus) *Writer {
	return &Writer{store: store, bus: b}
}

// Run subscribes to all data topics and writes incoming messages to BBolt.
// Blocks until ctx is cancelled.
func (w *Writer) Run(ctx context.Context) error {
	sub := w.bus.Subscribe(1024, "ohlc.*.*", "trade.*.*", "lob.*.*", "news", "alert")
	defer w.bus.Unsubscribe(sub)

	var written uint64
	logTicker := time.NewTicker(30 * time.Second)
	defer logTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-logTicker.C:
			if written > 0 {
				slog.Info("cache writer stats", "written", written)
				written = 0
			}

		case msg, ok := <-sub.C:
			if !ok {
				return nil
			}
			if err := w.persist(msg); err != nil {
				slog.Debug("cache write error", "topic", msg.Topic, "error", err)
				continue
			}
			written++
		}
	}
}

func (w *Writer) persist(msg bus.Message) error {
	switch v := msg.Payload.(type) {
	case feeds.OHLC:
		key := fmt.Sprintf("%s/%s/%s/%d", v.Exchange, v.Instrument, v.Timeframe, v.Timestamp.UnixMilli())
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return w.store.Put("ohlc", key, data)

	case feeds.Trade:
		key := fmt.Sprintf("%s/%s/%s/%d", v.Exchange, v.Instrument, v.TradeID, v.Timestamp.UnixMilli())
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return w.store.Put("trades", key, data)

	case feeds.LOBSnapshot:
		key := fmt.Sprintf("%s/%s/%d", v.Exchange, v.Instrument, v.Timestamp.UnixMilli())
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return w.store.Put("lob_snapshots", key, data)
	}

	return nil
}
