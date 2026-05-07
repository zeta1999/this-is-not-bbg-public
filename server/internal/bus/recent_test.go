package bus

import (
	"sort"
	"testing"
)

// RecentPerTopic is the engine behind the TUI/SSE news-backfill at
// subscribe time. These tests pin the contract so a future change
// can't silently regress the "0 items on connect" bug from
// 2026-04-28.

func TestRecentPerTopic_ReturnsLastNPerTopic(t *testing.T) {
	b := New(10)
	for i := 1; i <= 5; i++ {
		b.Publish(Message{Topic: "news", Payload: i})
	}
	for i := 1; i <= 3; i++ {
		b.Publish(Message{Topic: "alert", Payload: i})
	}

	got := b.RecentPerTopic(100, "news", "alert")
	// 5 news + 3 alerts = 8 messages.
	if len(got) != 8 {
		t.Fatalf("len=%d want 8: %v", len(got), got)
	}
}

func TestRecentPerTopic_RespectsPerTopicCap(t *testing.T) {
	b := New(10)
	for i := 1; i <= 7; i++ {
		b.Publish(Message{Topic: "news", Payload: i})
	}
	got := b.RecentPerTopic(3, "news")
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	// Must be the most recent 3 (5,6,7), not the oldest.
	values := make([]int, len(got))
	for i, m := range got {
		values[i] = m.Payload.(int)
	}
	sort.Ints(values)
	want := []int{5, 6, 7}
	for i, v := range values {
		if v != want[i] {
			t.Errorf("recent %d = %d, want %d", i, v, want[i])
		}
	}
}

func TestRecentPerTopic_ZeroPerTopicTreatedAsOne(t *testing.T) {
	b := New(10)
	for i := 1; i <= 4; i++ {
		b.Publish(Message{Topic: "news", Payload: i})
	}
	got := b.RecentPerTopic(0, "news")
	if len(got) != 1 {
		t.Fatalf("perTopic=0 len=%d want 1", len(got))
	}
	if got[0].Payload != 4 {
		t.Errorf("got %v want 4", got[0].Payload)
	}
}

func TestRecentPerTopic_PatternMatch(t *testing.T) {
	b := New(10)
	b.Publish(Message{Topic: "ohlc.binance.BTCUSD", Payload: "btc"})
	b.Publish(Message{Topic: "ohlc.kraken.ETHUSD", Payload: "eth"})
	b.Publish(Message{Topic: "news", Payload: "n"})

	// Glob patterns route to per-topic ring lookup.
	got := b.RecentPerTopic(100, "ohlc.binance.*")
	if len(got) != 1 || got[0].Payload != "btc" {
		t.Fatalf("ohlc.binance.* = %v, want [btc]", got)
	}
	got = b.RecentPerTopic(100, "ohlc.*.*")
	if len(got) != 2 {
		t.Fatalf("ohlc.*.* len=%d want 2", len(got))
	}
}

func TestRecentPerTopic_EmptyForUnknownTopic(t *testing.T) {
	b := New(10)
	got := b.RecentPerTopic(100, "news")
	if len(got) != 0 {
		t.Fatalf("len=%d want 0 on empty bus", len(got))
	}
}

func TestRecentPerTopic_RingDepthCapsHistory(t *testing.T) {
	// Ring depth 3, push 10 — RecentPerTopic with perTopic=100
	// should still return at most 3 (the ring's stored count).
	b := New(3)
	for i := 0; i < 10; i++ {
		b.Publish(Message{Topic: "news", Payload: i})
	}
	got := b.RecentPerTopic(100, "news")
	if len(got) != 3 {
		t.Fatalf("ring depth=3 returned %d items, want 3", len(got))
	}
}
