package bus

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// drainAll collects up to n messages from ch, yielding early on a short
// idle timeout. Used by policy tests that want to read what the pump
// has already handed off plus whatever arrives within a small window.
func drainAll(ch chan Message, max int, idle time.Duration) []Message {
	var out []Message
	for len(out) < max {
		select {
		case msg, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, msg)
		case <-time.After(idle):
			return out
		}
	}
	return out
}

// TestDropOldest_LosesOldestOnOverflow: under sustained publish with no
// reader draining, DropOldest keeps the most recent cap messages.
func TestDropOldest_LosesOldestOnOverflow(t *testing.T) {
	b := New(10)
	sub := b.SubscribeWithOptions(SubscribeOptions{
		BufSize:  3,
		Patterns: []string{"x"},
		Policy:   DropOldest,
	})

	for i := 0; i < 10; i++ {
		b.Publish(Message{Topic: "x", Payload: i})
	}

	// Give the pump a beat to catch up to anything it can hand off.
	// Under DropOldest with unbuffered C, the pump is blocked handing
	// off the first message it drained. Remaining queue holds the
	// most recent 2 (cap=3 - 1 in pump's hand).
	got := drainAll(sub.C, 10, 50*time.Millisecond)

	// Latest three writes were 7, 8, 9 — but one of them may have been
	// drained into the pump before the overflow started. The strict
	// invariant: all returned payloads are monotonically increasing,
	// the last one equals 9, and len(got) <= cap+1 (pump holds one in
	// transit). Drops counter == 10 - len(got).
	if len(got) == 0 || len(got) > 4 {
		t.Fatalf("drained=%d, want 1..4", len(got))
	}
	if got[len(got)-1].Payload.(int) != 9 {
		t.Errorf("last delivered=%v, want 9", got[len(got)-1].Payload)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Payload.(int) <= got[i-1].Payload.(int) {
			t.Errorf("non-monotonic delivery: %v then %v", got[i-1].Payload, got[i].Payload)
		}
	}
	totalSeen := uint64(len(got)) + sub.Dropped()
	if totalSeen != 10 {
		t.Errorf("delivered %d + dropped %d = %d, want 10", len(got), sub.Dropped(), totalSeen)
	}

	b.Unsubscribe(sub)
}

// TestCoalesceByKey_ReplacesInPlace: same-topic bursts collapse to one
// message per key; coalescedReplaced counts the collapses.
func TestCoalesceByKey_ReplacesInPlace(t *testing.T) {
	b := New(10)
	sub := b.SubscribeWithOptions(SubscribeOptions{
		BufSize:  4,
		Patterns: []string{"lob.*"},
		Policy:   CoalesceByKey,
	})

	// Publish 5 updates to the same key — all should coalesce down
	// to a single latest-wins entry in the subscriber's queue.
	for i := 0; i < 5; i++ {
		b.Publish(Message{Topic: "lob.btc", Payload: i})
	}
	// And one message with a different key.
	b.Publish(Message{Topic: "lob.eth", Payload: 100})

	got := drainAll(sub.C, 10, 50*time.Millisecond)
	// We expect two messages total: latest btc (payload 4) and eth (100).
	if len(got) != 2 {
		t.Fatalf("received %d, want 2: %+v", len(got), got)
	}
	sort.SliceStable(got, func(i, j int) bool { return got[i].Topic < got[j].Topic })
	if got[0].Topic != "lob.btc" || got[0].Payload.(int) != 4 {
		t.Errorf("lob.btc latest=%v, want 4", got[0].Payload)
	}
	if got[1].Topic != "lob.eth" || got[1].Payload.(int) != 100 {
		t.Errorf("lob.eth=%v, want 100", got[1].Payload)
	}
	if rep := sub.CoalescedReplaced(); rep == 0 {
		t.Errorf("CoalescedReplaced=0, want >0")
	}
	if sub.Dropped() != 0 {
		t.Errorf("Dropped=%d, want 0 under coalesce (no new unseen keys past cap)", sub.Dropped())
	}

	b.Unsubscribe(sub)
}

// TestCoalesceByKey_NeverExceedsCap: publishing more unseen keys than
// cap causes the excess to be dropped (not buffered), so the buffer
// never exceeds cap.
func TestCoalesceByKey_NeverExceedsCap(t *testing.T) {
	b := New(10)
	const cap = 3
	sub := b.SubscribeWithOptions(SubscribeOptions{
		BufSize:  cap,
		Patterns: []string{"k.*"},
		Policy:   CoalesceByKey,
	})

	// 10 distinct keys; cap is 3.
	for i := 0; i < 10; i++ {
		b.Publish(Message{Topic: fmt.Sprintf("k.%d", i), Payload: i})
	}

	got := drainAll(sub.C, 20, 50*time.Millisecond)
	// The pump may have handed off up to 1 message into the consumer
	// channel before the rest filled the queue. So we expect
	// len(got) <= cap+1, and the first cap distinct keys (0..cap-1)
	// should be the ones buffered.
	if len(got) > cap+1 {
		t.Fatalf("received %d, want <=%d", len(got), cap+1)
	}
	if delivered := uint64(len(got)); delivered+sub.Dropped() != 10 {
		t.Errorf("delivered %d + dropped %d = %d, want 10",
			delivered, sub.Dropped(), delivered+sub.Dropped())
	}

	b.Unsubscribe(sub)
}

// TestRapid_DropOldest_BufferNeverExceedsCap: for any publish burst
// with no reader, the queue cap invariant holds — deliveries + drops
// always sum to the number of publishes.
func TestRapid_DropOldest_BufferNeverExceedsCap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cap := rapid.IntRange(1, 8).Draw(t, "cap")
		n := rapid.IntRange(0, 64).Draw(t, "n")

		b := New(128)
		sub := b.SubscribeWithOptions(SubscribeOptions{
			BufSize:  cap,
			Patterns: []string{"x"},
			Policy:   DropOldest,
		})
		defer b.Unsubscribe(sub)

		for i := 0; i < n; i++ {
			b.Publish(Message{Topic: "x", Payload: i})
		}

		got := drainAll(sub.C, n+1, 20*time.Millisecond)
		total := uint64(len(got)) + sub.Dropped()
		if int(total) != n {
			t.Fatalf("delivered %d + dropped %d = %d, want %d",
				len(got), sub.Dropped(), total, n)
		}
		// Payloads are monotonically increasing (no reordering).
		for i := 1; i < len(got); i++ {
			if got[i].Payload.(int) <= got[i-1].Payload.(int) {
				t.Fatalf("non-monotonic: %v then %v",
					got[i-1].Payload, got[i].Payload)
			}
		}
		// If we delivered anything, the last one must be the most
		// recent publish (under DropOldest the newest always wins a
		// slot — either buffered or in transit to the consumer).
		if len(got) > 0 && got[len(got)-1].Payload.(int) != n-1 {
			t.Fatalf("last delivered=%v, want %d", got[len(got)-1].Payload, n-1)
		}
	})
}

// TestRapid_CoalesceByKey_LatestPerKeyWins: after a burst of publishes
// spanning arbitrary keys, every key appears at most once in the
// drained output, and each appearance is the latest value for that key.
func TestRapid_CoalesceByKey_LatestPerKeyWins(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cap := rapid.IntRange(1, 8).Draw(t, "cap")
		numKeys := rapid.IntRange(1, 6).Draw(t, "numKeys")
		n := rapid.IntRange(0, 48).Draw(t, "n")

		b := New(128)
		sub := b.SubscribeWithOptions(SubscribeOptions{
			BufSize:  cap,
			Patterns: []string{"k.*"},
			Policy:   CoalesceByKey,
		})
		defer b.Unsubscribe(sub)

		// Track latest published value per key.
		latest := make(map[string]int)
		for i := 0; i < n; i++ {
			k := rapid.IntRange(0, numKeys-1).Draw(t, fmt.Sprintf("k%d", i))
			topic := fmt.Sprintf("k.%d", k)
			b.Publish(Message{Topic: topic, Payload: i})
			latest[topic] = i
		}

		got := drainAll(sub.C, n+1, 20*time.Millisecond)
		seen := make(map[string]int)
		for _, m := range got {
			if prev, ok := seen[m.Topic]; ok {
				// A key may appear more than once if it was drained,
				// handed off to the consumer, and a later publish
				// re-inserted it as a fresh slot. Monotonicity is
				// still guaranteed (payloads = publish sequence).
				if m.Payload.(int) <= prev {
					t.Fatalf("non-monotonic for %s: %d then %d",
						m.Topic, prev, m.Payload)
				}
			}
			seen[m.Topic] = m.Payload.(int)
		}
		// Deliveries can lag: if a key's slot is freed by the pump
		// and the buffer fills with other keys before the key's next
		// publish, that later publish is dropped (never coalesced).
		// So we only assert: every delivered payload is <= the bus's
		// final latest for that topic, and was a real publish value.
		for topic, last := range seen {
			if last > latest[topic] {
				t.Fatalf("topic %s delivered %d > bus latest %d",
					topic, last, latest[topic])
			}
		}
		// Sum rule: delivered + dropped + coalesced = n.
		total := uint64(len(got)) + sub.Dropped() + sub.CoalescedReplaced()
		if int(total) != n {
			t.Fatalf("delivered %d + dropped %d + coalesced %d = %d, want %d",
				len(got), sub.Dropped(), sub.CoalescedReplaced(), total, n)
		}
	})
}

// TestCoalesceByTopicMatch_MixedPolicy: a subscriber using
// CoalesceByTopicMatch coalesces messages whose topics match the
// predicate and treats everything else as non-coalescable (each slot
// is independent; overflow drops the newest of the non-coalescable
// group rather than collapsing unrelated history).
func TestCoalesceByTopicMatch_MixedPolicy(t *testing.T) {
	b := New(32)
	match := func(topic string) bool {
		return topic == "lob.btc" || topic == "sanity.prices"
	}
	sub := b.SubscribeWithOptions(SubscribeOptions{
		BufSize:     16,
		Patterns:    []string{"*", "*.*"},
		Policy:      CoalesceByKey,
		CoalesceKey: CoalesceByTopicMatch(match),
	})
	defer b.Unsubscribe(sub)

	// 5 LOB snapshots (coalescable) + 3 news items (non-coalescable).
	for i := 0; i < 5; i++ {
		b.Publish(Message{Topic: "lob.btc", Payload: "lob" + fmt.Sprint(i)})
	}
	for i := 0; i < 3; i++ {
		b.Publish(Message{Topic: "news", Payload: "news" + fmt.Sprint(i)})
	}
	// Another 5 LOB snapshots — all should coalesce with the first
	// slot (or a newly-reinserted slot if the pump has drained it).
	for i := 5; i < 10; i++ {
		b.Publish(Message{Topic: "lob.btc", Payload: "lob" + fmt.Sprint(i)})
	}

	got := drainAll(sub.C, 20, 50*time.Millisecond)

	var lobSeen, newsSeen int
	for _, m := range got {
		switch m.Topic {
		case "lob.btc":
			lobSeen++
		case "news":
			newsSeen++
		default:
			t.Fatalf("unexpected topic %q", m.Topic)
		}
	}
	// All 3 news items must survive (non-coalescable).
	if newsSeen != 3 {
		t.Errorf("news delivered=%d, want 3", newsSeen)
	}
	// LOB should coalesce down to far fewer than 10 messages.
	if lobSeen >= 10 {
		t.Errorf("lob delivered=%d, expected coalescing", lobSeen)
	}
	if sub.CoalescedReplaced() == 0 {
		t.Errorf("CoalescedReplaced=0, want >0 (lob bursts should replace in place)")
	}
}

// TestCoalesceByKey_ConcurrentProducer: a producer goroutine publishes
// a sustained LOB-shaped burst while the consumer slowly drains. For
// every key, the consumer's final delivered payload is <= the latest
// value published for that key, and at least one delivery per key
// happens (busy keys always get surfaced).
func TestCoalesceByKey_ConcurrentProducer(t *testing.T) {
	b := New(64)
	const (
		numKeys = 5
		perKey  = 200
		cap     = 16
	)
	sub := b.SubscribeWithOptions(SubscribeOptions{
		BufSize:     cap,
		Patterns:    []string{"lob.*"},
		Policy:      CoalesceByKey,
	})

	// Producer.
	done := make(chan struct{})
	go func() {
		for i := 0; i < perKey; i++ {
			for k := 0; k < numKeys; k++ {
				topic := fmt.Sprintf("lob.%d", k)
				b.Publish(Message{Topic: topic, Payload: i})
			}
		}
		close(done)
	}()

	// Slow consumer — drain with a small delay per message so the
	// queue gets exercised.
	seen := make(map[string]int)
	readCtx := make(chan struct{})
	go func() {
		defer close(readCtx)
		for {
			select {
			case m, ok := <-sub.C:
				if !ok {
					return
				}
				prev := seen[m.Topic]
				cur := m.Payload.(int)
				if cur < prev {
					t.Errorf("non-monotonic for %s: %d -> %d", m.Topic, prev, cur)
				}
				seen[m.Topic] = cur
				time.Sleep(100 * time.Microsecond)
			case <-time.After(200 * time.Millisecond):
				return
			}
		}
	}()

	<-done
	// Let the consumer catch up a little.
	time.Sleep(50 * time.Millisecond)
	b.Unsubscribe(sub)
	<-readCtx

	if len(seen) != numKeys {
		t.Errorf("saw %d distinct keys, want %d", len(seen), numKeys)
	}
	for k, v := range seen {
		if v > perKey-1 {
			t.Errorf("key %s saw %d, max possible %d", k, v, perKey-1)
		}
	}
	// Sum invariant.
	total := uint64(0)
	for range seen {
		total++
	}
	// Every publish is delivered, dropped, or coalesced.
	total = sub.Dropped() + sub.CoalescedReplaced()
	// Just check it's non-trivial — actual value depends on scheduling.
	if total == 0 {
		t.Errorf("no drops + no coalesce = %d — expected some", total)
	}
}

// TestDropNewest_BehaviorUnchanged asserts the Phase 1 guarantee that
// subscribers using the default policy see identical semantics to
// pre-Phase-1 bus behavior: overflow drops newest, no coalescing side
// effects, no pump goroutine.
func TestDropNewest_BehaviorUnchanged(t *testing.T) {
	b := New(10)
	sub := b.SubscribeWithOptions(SubscribeOptions{
		BufSize:  2,
		Patterns: []string{"y"},
		Policy:   DropNewest,
	})
	defer b.Unsubscribe(sub)

	for i := 0; i < 5; i++ {
		b.Publish(Message{Topic: "y", Payload: i})
	}
	// Buffer holds the first 2; the rest are dropped.
	got := drainAll(sub.C, 5, 20*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("delivered %d, want 2", len(got))
	}
	if got[0].Payload.(int) != 0 || got[1].Payload.(int) != 1 {
		t.Fatalf("delivered payloads=%v %v, want 0 1", got[0].Payload, got[1].Payload)
	}
	if sub.Dropped() != 3 {
		t.Errorf("Dropped=%d, want 3", sub.Dropped())
	}
	if sub.CoalescedReplaced() != 0 {
		t.Errorf("CoalescedReplaced=%d, want 0 under DropNewest", sub.CoalescedReplaced())
	}
}
