package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

func TestPricesSanityChecker_PublishesSnapshotAndFlagsOutlier(t *testing.T) {
	b := bus.New(64)
	sub := b.Subscribe(16, "sanity.prices")
	defer b.Unsubscribe(sub)

	sc := NewPricesSanityChecker(b, 50*time.Millisecond, 0.5, []string{"BTCUSDT"})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { _ = sc.Run(ctx); close(done) }()

	// 10ms for subscription to register.
	time.Sleep(10 * time.Millisecond)

	now := time.Now()
	// Three consensus venues + one outlier (Bybit at +5%).
	publish := func(ex string, px float64) {
		b.Publish(bus.Message{
			Topic:   "ohlc.binance.BTCUSDT",
			Payload: feeds.OHLC{Instrument: "BTCUSDT", Exchange: ex, Close: px, Timestamp: now},
		})
	}
	publish("binance", 60000)
	publish("okx", 60010)
	publish("coinbase", 59990)
	publish("bybit", 63000) // +5% — should flag outlier

	// Wait for a tick to fire.
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	// Drain snapshots; there should be at least one for BTCUSDT.
	var got *SanitySnapshot
	for {
		select {
		case msg, ok := <-sub.C:
			if !ok {
				goto done
			}
			if snap, isSnap := msg.Payload.(SanitySnapshot); isSnap && snap.Instrument == "BTCUSDT" {
				got = &snap
			}
		default:
			goto done
		}
	}
done:
	if got == nil {
		t.Fatal("no SanitySnapshot published for BTCUSDT")
	}
	if got.VenueCount != 4 {
		t.Errorf("venue count: got %d want 4", got.VenueCount)
	}
	if got.OutlierCount != 1 {
		t.Errorf("outlier count: got %d want 1 (bybit only)", got.OutlierCount)
	}
	if got.Median < 59990 || got.Median > 60010 {
		t.Errorf("median: got %.2f, expected near 60000", got.Median)
	}
	var bybit *SanityVenue
	for i := range got.Venues {
		if got.Venues[i].Exchange == "bybit" {
			bybit = &got.Venues[i]
		}
	}
	if bybit == nil {
		t.Fatal("bybit not in snapshot")
	}
	if !bybit.Outlier {
		t.Errorf("bybit should be flagged outlier at +5%%")
	}
}

func TestPricesSanityChecker_NoPairsIsNoOp(t *testing.T) {
	b := bus.New(8)
	sc := NewPricesSanityChecker(b, 10*time.Millisecond, 0.5, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := sc.Run(ctx); err != nil {
		t.Errorf("Run returned error: %v", err)
	}
}

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{[]float64{1, 2, 3}, 2},
		{[]float64{1, 2, 3, 4}, 2.5},
		{[]float64{5}, 5},
		{nil, 0},
		{[]float64{60000, 60010, 59990, 63000}, 60005},
	}
	for _, c := range cases {
		if got := median(c.in); got != c.want {
			t.Errorf("median(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
