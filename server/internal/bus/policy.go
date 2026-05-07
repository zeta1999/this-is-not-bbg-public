package bus

import (
	"strconv"
	"sync/atomic"
)

// DispositionPolicy controls how a subscriber's buffer handles overflow.
//
// Phase 1 (this file + bus.go changes) implements DropNewest, DropOldest,
// and CoalesceByKey. The remaining policies are reserved identifiers
// for later phases (see docs/BACKPRESSURE-PROPOSAL.md §6.1 / §7).
// Requesting a not-yet-implemented policy falls back to DropNewest.
type DispositionPolicy int

const (
	// DropNewest discards the incoming message when the subscriber's
	// buffer is full. Matches historical bus behavior; default for
	// plain Subscribe() callers.
	DropNewest DispositionPolicy = iota

	// DropOldest evicts the oldest buffered message to make room for
	// the newest. Use for streams where freshness matters more than
	// history.
	DropOldest

	// CoalesceByKey replaces any already-buffered message sharing the
	// same key with the new one. When the buffer is full and the
	// incoming message has an unseen key, the new message is dropped
	// (the buffer never grows beyond its cap). Default key is topic.
	CoalesceByKey

	// BoundedBlock — reserved for Phase 5+; not implemented yet.
	BoundedBlock

	// CreditGated — reserved for Phase 3/4; not implemented yet.
	CreditGated

	// DiskSpillThenDrop — reserved for Phase 5; not implemented yet.
	DiskSpillThenDrop
)

// SubscribeOptions configures a subscription created via
// Bus.SubscribeWithOptions. Zero values are safe: BufSize=0 picks
// the default (256), Policy=DropNewest preserves historical behavior.
type SubscribeOptions struct {
	// BufSize is the subscriber's buffer capacity in messages. For
	// DropNewest this sizes the Go channel directly; for the queue-
	// backed policies (DropOldest, CoalesceByKey) it sizes the
	// internal queue.
	BufSize int

	// Patterns are glob topic patterns forwarded to the bus matcher.
	Patterns []string

	// Policy selects the overflow disposition for this subscriber.
	Policy DispositionPolicy

	// CoalesceKey extracts a grouping key from a message when Policy
	// is CoalesceByKey. If nil, the message's topic is used.
	CoalesceKey func(Message) string
}

// CoalesceByTopicMatch returns a CoalesceKey function that uses the
// topic as the grouping key when match(topic) returns true, and a
// unique per-message sentinel otherwise. Under CoalesceByKey this
// gives the caller a mixed-policy subscription: matched topics
// coalesce (latest-wins per topic), unmatched topics never coalesce
// (each message occupies its own slot, dropped only on buffer full).
//
// Safe to reuse the returned function across many subscribers.
func CoalesceByTopicMatch(match func(topic string) bool) func(Message) string {
	var counter atomic.Uint64
	return func(m Message) string {
		if match(m.Topic) {
			return m.Topic
		}
		// NUL prefix guarantees no collision with any real topic.
		return "\x00uniq\x00" + strconv.FormatUint(counter.Add(1), 10)
	}
}
