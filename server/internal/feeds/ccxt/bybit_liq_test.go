package ccxt

import (
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// TestBybit_HandleLiquidations_SideMapping feeds a canned
// allLiquidation payload (both short-form and long-form side
// strings) and asserts the taker-side mapping + notional math.
func TestBybit_HandleLiquidations_SideMapping(t *testing.T) {
	b := bus.New(8)
	sub := b.Subscribe(8, "liquidation.bybit.*")
	defer b.Unsubscribe(sub)

	a := NewBybitAdapter(b, []string{"BTCUSDT"}, []string{"liquidations"}, "wss://example")

	data := []byte(`[
		{"T":1700000000000,"S":"Buy","v":"0.5","p":"65000"},
		{"T":1700000001000,"S":"Sell","v":"0.25","p":"64900"},
		{"T":1700000002000,"S":"S","v":"0.1","p":"64800"}
	]`)
	a.handleLiquidations("BTCUSDT", data)

	want := []struct {
		side string
		px   float64
		qty  float64
	}{
		{"buy", 65000, 0.5},
		{"sell", 64900, 0.25},
		{"sell", 64800, 0.1},
	}

	for i, w := range want {
		select {
		case m := <-sub.C:
			if m.Topic != "liquidation.bybit.BTCUSDT" {
				t.Fatalf("msg %d topic=%q", i, m.Topic)
			}
			p := m.Payload.(feeds.LiquidationEvent)
			if p.Side != w.side || p.Price != w.px || p.Quantity != w.qty {
				t.Errorf("msg %d got side=%s price=%v qty=%v, want %s %v %v",
					i, p.Side, p.Price, p.Quantity, w.side, w.px, w.qty)
			}
			if p.Notional != w.px*w.qty {
				t.Errorf("msg %d notional=%v, want %v", i, p.Notional, w.px*w.qty)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("msg %d never arrived", i)
		}
	}
}
