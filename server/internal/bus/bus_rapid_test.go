package bus

import (
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestRapid_NoDropWhenBufferHasRoom is the Go mirror of the TLA+
// invariant RealtimeProgress: if a subscriber's channel buffer has
// capacity when Publish fires, the message must be delivered — it
// never drops.
func TestRapid_NoDropWhenBufferHasRoom(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bufSize := rapid.IntRange(1, 64).Draw(t, "bufSize")
		numMessages := rapid.IntRange(0, bufSize).Draw(t, "numMessages")

		b := New(128)
		sub := b.Subscribe(bufSize, "*")
		defer b.Unsubscribe(sub)

		for i := 0; i < numMessages; i++ {
			b.Publish(Message{Topic: "x", Payload: i})
		}

		// Every message should sit in the channel because numMessages <= bufSize.
		if got := sub.Dropped(); got != 0 {
			t.Fatalf("subscriber dropped %d with bufSize=%d numMessages=%d",
				got, bufSize, numMessages)
		}
		// And the bus-wide counter must agree.
		if s := b.Stats().Dropped; s != 0 {
			t.Fatalf("bus dropped=%d; want 0", s)
		}
	})
}

// TestRapid_DropCounterMatchesOverflow: beyond bufSize, every extra
// Publish increments both counters by exactly 1.
func TestRapid_DropCounterMatchesOverflow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bufSize := rapid.IntRange(1, 16).Draw(t, "bufSize")
		overflow := rapid.IntRange(1, 32).Draw(t, "overflow")

		b := New(128)
		sub := b.Subscribe(bufSize, "*")
		defer b.Unsubscribe(sub)

		total := bufSize + overflow
		for i := 0; i < total; i++ {
			b.Publish(Message{Topic: "x", Payload: i})
		}

		if got := sub.Dropped(); int(got) != overflow {
			t.Fatalf("sub.Dropped=%d, want %d (bufSize=%d total=%d)",
				got, overflow, bufSize, total)
		}
		if got := b.Stats().Dropped; int(got) != overflow {
			t.Fatalf("bus.Stats.Dropped=%d, want %d", got, overflow)
		}
	})
}

// TestRapid_RingBufferReplayCap: ring buffer retains at most N
// messages for any topic (TLA+ BulkBounded analogue). Subscribers
// joining after publishes see at most N of them.
func TestRapid_RingBufferReplayCap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ringDepth := rapid.IntRange(1, 16).Draw(t, "ringDepth")
		numPublished := rapid.IntRange(0, 64).Draw(t, "numPublished")

		b := New(ringDepth)
		for i := 0; i < numPublished; i++ {
			b.Publish(Message{Topic: "x", Payload: i})
		}

		// Subscriber joins now — receives replay from ring buffer.
		sub := b.Subscribe(ringDepth+8, "*")
		defer b.Unsubscribe(sub)

		// Drain what's buffered at the moment of subscription.
		deadline := time.After(10 * time.Millisecond)
		var got int
	loop:
		for {
			select {
			case <-sub.C:
				got++
			case <-deadline:
				break loop
			}
		}

		// Ring holds at most ringDepth messages.
		expected := numPublished
		if expected > ringDepth {
			expected = ringDepth
		}
		if got != expected {
			t.Fatalf("replay=%d, want %d (ringDepth=%d numPublished=%d)",
				got, expected, ringDepth, numPublished)
		}
	})
}

// A small smoke helper so fuzz output is readable if rapid shrinks
// a failing case.
var _ = fmt.Sprint
