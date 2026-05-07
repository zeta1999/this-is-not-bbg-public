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

	// Overflow policy. DropNewest uses the C channel's buffer directly.
	// Other policies use the queue/keyIndex fields below, fed by a pump
	// goroutine that forwards messages to C.
	policy            DispositionPolicy
	cap               int
	coalescedReplaced atomic.Uint64

	qmu         sync.Mutex
	queue       []Message
	keyIndex    map[string]int
	coalesceKey func(Message) string
	wake        chan struct{}
	stopCh      chan struct{}
	pumpDone    chan struct{}
}

// Dropped returns the number of messages the bus tried to deliver to
// this subscriber but had to drop because the channel was full.
func (s *Subscriber) Dropped() uint64 { return s.dropped.Load() }

// CoalescedReplaced returns the number of buffered messages that were
// replaced in place due to the CoalesceByKey policy. Zero for other
// policies.
func (s *Subscriber) CoalescedReplaced() uint64 { return s.coalescedReplaced.Load() }

// Policy returns the subscriber's configured disposition policy.
func (s *Subscriber) Policy() DispositionPolicy { return s.policy }

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
// bufSize controls the channel buffer; messages are dropped if the subscriber
// can't keep up (DropNewest policy).
func (b *Bus) Subscribe(bufSize int, patterns ...string) *Subscriber {
	return b.SubscribeWithOptions(SubscribeOptions{
		BufSize:  bufSize,
		Patterns: patterns,
		Policy:   DropNewest,
	})
}

// SubscribeWithOptions creates a subscriber with explicit disposition
// policy. See SubscribeOptions for field semantics.
func (b *Bus) SubscribeWithOptions(opts SubscribeOptions) *Subscriber {
	bufSize := opts.BufSize
	if bufSize <= 0 {
		bufSize = 256
	}

	sub := &Subscriber{
		patterns: opts.Patterns,
		policy:   opts.Policy,
		cap:      bufSize,
	}

	switch opts.Policy {
	case DropOldest, CoalesceByKey:
		sub.C = make(chan Message) // unbuffered; queue owns capacity
		sub.queue = make([]Message, 0, bufSize)
		sub.wake = make(chan struct{}, 1)
		sub.stopCh = make(chan struct{})
		sub.pumpDone = make(chan struct{})
		if opts.Policy == CoalesceByKey {
			sub.keyIndex = make(map[string]int, bufSize)
			sub.coalesceKey = opts.CoalesceKey
			if sub.coalesceKey == nil {
				sub.coalesceKey = topicKey
			}
		}
	default:
		// DropNewest or any not-yet-implemented policy falls through
		// to historical Go-channel-buffered behavior.
		sub.policy = DropNewest
		sub.C = make(chan Message, bufSize)
	}

	b.mu.Lock()
	sub.id = b.nextID
	b.nextID++
	b.subscribers[sub.id] = sub

	// Snapshot-replay from ring buffers. For DropNewest we preserve the
	// historical silent-drop-on-full behavior (no counter bump) to keep
	// Phase 1 "behavior unchanged" for unswitched subscribers.
	for topic, ring := range b.ringBuffers {
		if matchesAny(topic, opts.Patterns) {
			for _, msg := range ring.snapshot() {
				if sub.policy == DropNewest {
					select {
					case sub.C <- msg:
					default:
					}
				} else {
					b.deliverLocked(sub, msg)
				}
			}
		}
	}
	b.mu.Unlock()

	if sub.pumpDone != nil {
		go sub.pump()
	}
	return sub
}

// Unsubscribe removes a subscriber from the bus and closes its channel.
func (b *Bus) Unsubscribe(sub *Subscriber) {
	b.mu.Lock()
	if _, ok := b.subscribers[sub.id]; !ok {
		b.mu.Unlock()
		return
	}
	delete(b.subscribers, sub.id)
	b.mu.Unlock()

	// For queue-backed subs, stop the pump before closing C.
	if sub.stopCh != nil {
		close(sub.stopCh)
		<-sub.pumpDone
	}
	close(sub.C)
}

// Publish sends a message to all subscribers whose patterns match the topic.
// Non-blocking: if a subscriber's buffer is full, the message is handled
// according to that subscriber's DispositionPolicy.
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

	for _, sub := range b.subscribers {
		if matchesAny(msg.Topic, sub.patterns) {
			b.deliverLocked(sub, msg)
		}
	}
}

// deliverLocked routes a message to a single subscriber according to its
// disposition policy. Caller must hold b.mu (read or write).
func (b *Bus) deliverLocked(sub *Subscriber, msg Message) {
	switch sub.policy {
	case DropNewest:
		select {
		case sub.C <- msg:
		default:
			sub.dropped.Add(1)
			b.dropped.Add(1)
		}

	case DropOldest:
		sub.qmu.Lock()
		if len(sub.queue) >= sub.cap {
			sub.queue = sub.queue[1:]
			sub.dropped.Add(1)
			b.dropped.Add(1)
		}
		sub.queue = append(sub.queue, msg)
		sub.qmu.Unlock()
		signalWake(sub)

	case CoalesceByKey:
		sub.qmu.Lock()
		key := sub.coalesceKey(msg)
		if idx, ok := sub.keyIndex[key]; ok {
			sub.queue[idx] = msg
			sub.coalescedReplaced.Add(1)
			sub.qmu.Unlock()
			signalWake(sub)
			return
		}
		if len(sub.queue) >= sub.cap {
			// Buffer full, unseen key — drop incoming (never grow past cap).
			sub.dropped.Add(1)
			b.dropped.Add(1)
			sub.qmu.Unlock()
			return
		}
		sub.queue = append(sub.queue, msg)
		sub.keyIndex[key] = len(sub.queue) - 1
		sub.qmu.Unlock()
		signalWake(sub)
	}
}

func signalWake(sub *Subscriber) {
	select {
	case sub.wake <- struct{}{}:
	default:
	}
}

// pump drains the internal queue and forwards to sub.C. Runs as one
// goroutine per queue-backed subscriber.
func (s *Subscriber) pump() {
	defer close(s.pumpDone)
	for {
		select {
		case <-s.stopCh:
			return
		case <-s.wake:
		}
		for {
			s.qmu.Lock()
			if len(s.queue) == 0 {
				s.qmu.Unlock()
				break
			}
			msg := s.queue[0]
			s.queue = s.queue[1:]
			if s.keyIndex != nil {
				key := s.coalesceKey(msg)
				if idx, ok := s.keyIndex[key]; ok && idx == 0 {
					delete(s.keyIndex, key)
				}
				for k, idx := range s.keyIndex {
					s.keyIndex[k] = idx - 1
				}
			}
			s.qmu.Unlock()
			select {
			case s.C <- msg:
			case <-s.stopCh:
				return
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

// RecentPerTopic returns up to `perTopic` most-recent messages from
// each ring buffer whose topic matches any of the given patterns.
// Use perTopic=1 for snapshot-like topics (lob/ohlc tail wins),
// perTopic>1 for log-like topics (news/alert where each message is
// a unique event and clients want a backfill of recent items at
// subscribe time).
func (b *Bus) RecentPerTopic(perTopic int, patterns ...string) []Message {
	if perTopic <= 0 {
		perTopic = 1
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	var out []Message
	for topic, ring := range b.ringBuffers {
		if !matchesAny(topic, patterns) {
			continue
		}
		snap := ring.snapshot()
		if len(snap) > perTopic {
			snap = snap[len(snap)-perTopic:]
		}
		out = append(out, snap...)
	}
	return out
}

func topicKey(m Message) string { return m.Topic }

// matchesAny checks if topic matches any of the given glob patterns.
func matchesAny(topic string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := path.Match(p, topic); matched {
			return false || matched
		}
	}
	return false
}
