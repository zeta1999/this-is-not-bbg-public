// Package bus implements a topic-based pub/sub message bus with glob-pattern
// subscriptions and ring buffers for late-joining clients.
package bus

import (
	"path"
	"sync"
	"sync/atomic"
)

// Message is a topic-tagged payload sent through the bus.
type Message struct {
	Topic   string
	Payload any
}

// Subscriber receives messages matching its subscription patterns.
type Subscriber struct {
	C        chan Message
	patterns []string
	id       uint64
	dropped  atomic.Uint64
}

// Dropped returns the number of messages the bus tried to deliver to
// this subscriber but had to drop because the channel was full.
func (s *Subscriber) Dropped() uint64 { return s.dropped.Load() }

// Bus is a topic-based pub/sub message bus.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[uint64]*Subscriber
	nextID      uint64
	ringBuffers map[string]*ringBuffer
	ringDepth   int
	dropped     atomic.Uint64 // total drops across all subscribers
}

// Stats is a snapshot of bus counters.
type Stats struct {
	Subscribers int
	Topics      int
	Dropped     uint64
}

// Stats returns a snapshot of current counters.
func (b *Bus) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return Stats{
		Subscribers: len(b.subscribers),
		Topics:      len(b.ringBuffers),
		Dropped:     b.dropped.Load(),
	}
}

type ringBuffer struct {
	buf  []Message
	pos  int
	full bool
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{buf: make([]Message, size)}
}

func (r *ringBuffer) push(msg Message) {
	r.buf[r.pos] = msg
	r.pos = (r.pos + 1) % len(r.buf)
	if r.pos == 0 {
		r.full = true
	}
}

// latest returns only the most recent message in the ring buffer.
func (r *ringBuffer) latest() (Message, bool) {
	if !r.full && r.pos == 0 {
		return Message{}, false
	}
	idx := r.pos - 1
	if idx < 0 {
		idx = len(r.buf) - 1
	}
	return r.buf[idx], true
}

func (r *ringBuffer) snapshot() []Message {
	if !r.full {
		return append([]Message{}, r.buf[:r.pos]...)
	}
	out := make([]Message, len(r.buf))
	copy(out, r.buf[r.pos:])
	copy(out[len(r.buf)-r.pos:], r.buf[:r.pos])
	return out
}

// New creates a new Bus with the given ring buffer depth per topic.
func New(ringDepth int) *Bus {
	if ringDepth <= 0 {
		ringDepth = 100
	}
	return &Bus{
		subscribers: make(map[uint64]*Subscriber),
		ringBuffers: make(map[string]*ringBuffer),
		ringDepth:   ringDepth,
	}
}

// Subscribe creates a new subscriber listening on the given topic patterns.
// Patterns use glob syntax (e.g., "ohlc.binance.*").
// The returned Subscriber's channel C will receive matching messages.
// bufSize controls the channel buffer; messages are dropped if the subscriber can't keep up.
func (b *Bus) Subscribe(bufSize int, patterns ...string) *Subscriber {
	if bufSize <= 0 {
		bufSize = 256
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	sub := &Subscriber{
		C:        make(chan Message, bufSize),
		patterns: patterns,
		id:       b.nextID,
	}
	b.nextID++
	b.subscribers[sub.id] = sub

	// Send buffered messages that match any pattern.
	for topic, ring := range b.ringBuffers {
		if matchesAny(topic, patterns) {
			for _, msg := range ring.snapshot() {
				select {
				case sub.C <- msg:
				default:
				}
			}
		}
	}

	return sub
}

// Unsubscribe removes a subscriber from the bus and closes its channel.
func (b *Bus) Unsubscribe(sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[sub.id]; ok {
		delete(b.subscribers, sub.id)
		close(sub.C)
	}
}

// Publish sends a message to all subscribers whose patterns match the topic.
// Non-blocking: if a subscriber's channel is full, the message is dropped for that subscriber.
func (b *Bus) Publish(msg Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Store in ring buffer.
	ring, ok := b.ringBuffers[msg.Topic]
	if !ok {
		// Promote to write lock to create ring buffer.
		b.mu.RUnlock()
		b.mu.Lock()
		ring, ok = b.ringBuffers[msg.Topic]
		if !ok {
			ring = newRingBuffer(b.ringDepth)
			b.ringBuffers[msg.Topic] = ring
		}
		b.mu.Unlock()
		b.mu.RLock()
	}
	ring.push(msg)

	// Fan out to subscribers.
	for _, sub := range b.subscribers {
		if matchesAny(msg.Topic, sub.patterns) {
			select {
			case sub.C <- msg:
			default:
				sub.dropped.Add(1)
				b.dropped.Add(1)
			}
		}
	}
}

// LatestPerTopic returns the most recent message from each ring buffer
// whose topic matches any of the given patterns.
func (b *Bus) LatestPerTopic(patterns ...string) []Message {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var out []Message
	for topic, ring := range b.ringBuffers {
		if matchesAny(topic, patterns) {
			if msg, ok := ring.latest(); ok {
				out = append(out, msg)
			}
		}
	}
	return out
}

// matchesAny checks if topic matches any of the given glob patterns.
func matchesAny(topic string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := path.Match(p, topic); matched {
			return false || matched
		}
	}
	return false
}
