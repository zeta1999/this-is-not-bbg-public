package ccxt

import (
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// TestBybit_HandleTrade_PopulatesDecimal asserts the U6 string-decimal
// fidelity path: the venue-native price/qty strings flow through to
// Trade.PriceDecimal / QuantityDecimal, parallel to the parsed
// float64s. Backtester / audit consumers prefer the strings to avoid
// double-precision drift on crypto tick prices.
func TestBybit_HandleTrade_PopulatesDecimal(t *testing.T) {
	b := bus.New(4)
	sub := b.Subscribe(4, "trade.bybit.*")
	defer b.Unsubscribe(sub)

	a := NewBybitAdapter(b, []string{"BTCUSDT"}, []string{"trade"}, "wss://example")
	a.handleTrade("BTCUSDT", []byte(`[
		{"T":1700000000000,"S":"Buy","v":"0.123","p":"65432.10","i":"abc"}
	]`))

	select {
	case m := <-sub.C:
		tr := m.Payload.(feeds.Trade)
		if tr.PriceDecimal != "65432.10" {
			t.Errorf("PriceDecimal: want %q, got %q", "65432.10", tr.PriceDecimal)
		}
		if tr.QuantityDecimal != "0.123" {
			t.Errorf("QuantityDecimal: want %q, got %q", "0.123", tr.QuantityDecimal)
		}
		if tr.Price == 0 || tr.Quantity == 0 {
			t.Errorf("float fields lost: %+v", tr)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("trade never arrived")
	}
}

// TestBybit_HandleBook_PopulatesDecimal asserts the LOB level decimals
// flow through too — same fidelity argument applies to bids/asks.
func TestBybit_HandleBook_PopulatesDecimal(t *testing.T) {
	b := bus.New(4)
	sub := b.Subscribe(4, "lob.bybit.*")
	defer b.Unsubscribe(sub)

	a := NewBybitAdapter(b, []string{"BTCUSDT"}, []string{"orderbook"}, "wss://example")
	a.handleBook("BTCUSDT", []byte(`{"b":[["65000.5","1.25","3"]],"a":[["65010.0","0.75"]]}`))

	select {
	case m := <-sub.C:
		snap := m.Payload.(feeds.LOBSnapshot)
		if len(snap.Bids) != 1 || len(snap.Asks) != 1 {
			t.Fatalf("expected 1 bid + 1 ask, got %+v", snap)
		}
		if snap.Bids[0].PriceDecimal != "65000.5" || snap.Bids[0].QuantityDecimal != "1.25" {
			t.Errorf("bid decimals drift: %+v", snap.Bids[0])
		}
		if snap.Bids[0].OrderCount != 3 {
			t.Errorf("bid OrderCount: want 3, got %d", snap.Bids[0].OrderCount)
		}
		if snap.Asks[0].PriceDecimal != "65010.0" || snap.Asks[0].QuantityDecimal != "0.75" {
			t.Errorf("ask decimals drift: %+v", snap.Asks[0])
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("snapshot never arrived")
	}
}
