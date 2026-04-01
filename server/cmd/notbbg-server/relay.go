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
	realtimeBufSize   = 1024
	bulkBufSize       = 8192
	initialCredits    = 512
	writeDeadline     = 5 * time.Second
	relayStatsInterval = 30 * time.Second
)

func newClientRelay(conn *transport.FramedConn) *clientRelay {
	r := &clientRelay{
		conn:         conn,
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

// isBulk returns true for topics that should be credit-gated.
func isBulk(topic string) bool {
	return strings.HasPrefix(topic, "ohlc.")
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

// sender sends messages to the client with priority: realtime first, bulk only with credits.
func (r *clientRelay) sender(ctx context.Context) error {
	statsTicker := time.NewTicker(relayStatsInterval)
	defer statsTicker.Stop()

	for {
		// Priority 1: always drain realtime.
		select {
		case <-ctx.Done():
			return ctx.Err()
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
			// No credits — only drain realtime and wait for credit signal.
			select {
			case <-ctx.Done():
				return ctx.Err()
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

	r.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
	err = r.conn.WriteFrame(data)
	r.conn.SetWriteDeadline(time.Time{}) // clear deadline
	return err
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
