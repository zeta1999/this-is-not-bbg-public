package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/transport"
)

// clientRelay splits bus messages into realtime and bulk lanes,
// applying credit-based backpressure to bulk (OHLC) data.
type clientRelay struct {
	conn         *transport.FramedConn
	controlCh    chan *transport.WireMsg // highest priority: overflow notices, health pings
	realtimeCh   chan bus.Message
	bulkCh       chan bus.Message
	credits      atomic.Int64
	creditSignal chan struct{}

	// Stats for logging.
	realtimeSent atomic.Int64
	bulkSent     atomic.Int64
	realtimeDrop atomic.Int64
	bulkDrop     atomic.Int64
}

const (
	realtimeBufSize = 1024
	bulkBufSize     = 8192

	// initialCredits is the bulk-lane grant handed to a freshly-
	// connected client. Sized to absorb a typical cold-start burst
	// (all-instruments OHLC "latest" replay) without stalling at
	// credit=0 before the client's first MsgCredit reply lands.
	// The client refills CreditRefill credits per creditInterval
	// received messages (see tui/cmd/notbbg/main.go — currently
	// 256 / 256), so steady-state is a 1:1 sliding window.
	initialCredits = 1024

	writeDeadline      = 5 * time.Second
	relayStatsInterval = 30 * time.Second
)

func newClientRelay(conn *transport.FramedConn) *clientRelay {
	r := &clientRelay{
		conn:         conn,
		controlCh:    make(chan *transport.WireMsg, 16),
		realtimeCh:   make(chan bus.Message, realtimeBufSize),
		bulkCh:       make(chan bus.Message, bulkBufSize),
		creditSignal: make(chan struct{}, 1),
	}
	r.credits.Store(initialCredits)
	return r
}

// addCredits adds bulk credits and signals the sender.
func (r *clientRelay) addCredits(n int) {
	r.credits.Add(int64(n))
	select {
	case r.creditSignal <- struct{}{}:
	default:
	}
}

// enqueueControl pushes a control-plane wire message (e.g. MsgOverflow)
// to the front-of-queue lane. Never blocks; if the 16-slot buffer is
// already full we're backed up enough that one more notice can wait.
func (r *clientRelay) enqueueControl(msg *transport.WireMsg) {
	select {
	case r.controlCh <- msg:
	default:
	}
}

// isBulk returns true for topics that should be credit-gated.
func isBulk(topic string) bool {
	return strings.HasPrefix(topic, "ohlc.")
}

// isDropped returns true for topics too high-volume for client relay.
// Raw trades go to cache + datalake only; clients get trade.agg.* and trade.snap.* instead.
func isDropped(topic string) bool {
	return strings.HasPrefix(topic, "trade.") &&
		!strings.HasPrefix(topic, "trade.agg.") &&
		!strings.HasPrefix(topic, "trade.snap.")
}

// splitter reads from the bus subscriber and routes to realtime or bulk channels.
func (r *clientRelay) splitter(ctx context.Context, sub *bus.Subscriber) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.C:
			if !ok {
				return
			}
			if isDropped(msg.Topic) {
				continue // raw trades go to cache/datalake only
			}
			if isBulk(msg.Topic) {
				select {
				case r.bulkCh <- msg:
				default:
					r.bulkDrop.Add(1)
				}
			} else {
				select {
				case r.realtimeCh <- msg:
				default:
					r.realtimeDrop.Add(1)
				}
			}
		}
	}
}

// sender sends messages to the client with priority: control > realtime > bulk (credit-gated).
func (r *clientRelay) sender(ctx context.Context) error {
	statsTicker := time.NewTicker(relayStatsInterval)
	defer statsTicker.Stop()
	defer r.logShutdown()

	for {
		// Priority 0: drain control plane (overflow notices, etc.).
		select {
		case wm := <-r.controlCh:
			if err := r.sendWire(wm); err != nil {
				return err
			}
			continue
		default:
		}

		// Priority 1: always drain realtime.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case wm := <-r.controlCh:
			if err := r.sendWire(wm); err != nil {
				return err
			}
			continue
		case msg := <-r.realtimeCh:
			if err := r.sendMsg(msg); err != nil {
				return err
			}
			r.realtimeSent.Add(1)
			continue
		case <-statsTicker.C:
			r.logStats()
			continue
		default:
			// No realtime pending — fall through to try bulk.
		}

		// Priority 2: send bulk if we have credits, or wait for realtime/credits.
		if r.credits.Load() > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case wm := <-r.controlCh:
				if err := r.sendWire(wm); err != nil {
					return err
				}
			case msg := <-r.realtimeCh:
				if err := r.sendMsg(msg); err != nil {
					return err
				}
				r.realtimeSent.Add(1)
			case msg := <-r.bulkCh:
				if err := r.sendMsg(msg); err != nil {
					return err
				}
				r.credits.Add(-1)
				r.bulkSent.Add(1)
			case <-statsTicker.C:
				r.logStats()
			}
		} else {
			// No credits — only drain realtime + control; wait for credit signal.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case wm := <-r.controlCh:
				if err := r.sendWire(wm); err != nil {
					return err
				}
			case msg := <-r.realtimeCh:
				if err := r.sendMsg(msg); err != nil {
					return err
				}
				r.realtimeSent.Add(1)
			case <-r.creditSignal:
				// Credits arrived, loop back to try bulk.
			case <-statsTicker.C:
				r.logStats()
			}
		}
	}
}

// sendWire serializes a control-plane wire message and ships it.
func (r *clientRelay) sendWire(wm *transport.WireMsg) error {
	data, err := wm.Encode()
	if err != nil {
		return nil
	}
	_ = r.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
	err = r.conn.WriteFrame(data)
	_ = r.conn.SetWriteDeadline(time.Time{})
	return err
}

func (r *clientRelay) sendMsg(msg bus.Message) error {
	payload, err := json.Marshal(msg.Payload)
	if err != nil {
		return nil // skip bad payloads, don't disconnect
	}
	wireMsg := &transport.WireMsg{
		Type:    transport.MsgUpdate,
		Topic:   msg.Topic,
		Payload: payload,
	}
	data, _ := wireMsg.Encode()

	_ = r.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
	err = r.conn.WriteFrame(data)
	_ = r.conn.SetWriteDeadline(time.Time{}) // clear deadline
	return err
}

// logShutdown emits a final stats line when the relay is tearing
// down so an operator can see cumulative drops even if the usual
// 30 s tick never fired (short-lived sessions). Mirrors the cache
// writer's on-shutdown log added in 0c9594b.
func (r *clientRelay) logShutdown() {
	slog.Info("client relay stopped",
		"realtime_sent", r.realtimeSent.Load(),
		"bulk_sent", r.bulkSent.Load(),
		"realtime_drop", r.realtimeDrop.Load(),
		"bulk_drop", r.bulkDrop.Load(),
		"credits_final", r.credits.Load(),
	)
}

func (r *clientRelay) logStats() {
	rt := r.realtimeSent.Load()
	bk := r.bulkSent.Load()
	rd := r.realtimeDrop.Load()
	bd := r.bulkDrop.Load()
	cr := r.credits.Load()
	if rt+bk+rd+bd > 0 {
		slog.Info("client relay stats",
			"realtime_sent", rt,
			"bulk_sent", bk,
			"realtime_drop", rd,
			"bulk_drop", bd,
			"credits", cr,
		)
	}
}
