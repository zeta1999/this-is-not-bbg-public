package dex

import (
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// TestHyperliquid_HandleLiquidations_Streams asserts each array
// entry becomes its own LiquidationEvent on the canonical topic.
func TestHyperliquid_HandleLiquidations_Streams(t *testing.T) {
	b := bus.New(8)
	sub := b.Subscribe(8, "liquidation.hyperliquid.*")
	defer b.Unsubscribe(sub)

	a := NewHyperliquidAdapter(b, time.Minute)

	data := []byte(`[
		{"coin":"BTC","px":"65000","sz":"0.5","side":"A","time":1700000000000},
		{"coin":"BTC","px":"64900","sz":"0.2","side":"B","time":1700000001000}
	]`)
	a.handleLiquidations(data)

	var got []feeds.LiquidationEvent
	deadline := time.After(200 * time.Millisecond)
	for len(got) < 2 {
		select {
		case m := <-sub.C:
			if m.Topic != "liquidation.hyperliquid.BTCUSD" {
				t.Fatalf("topic=%q", m.Topic)
			}
			p, ok := m.Payload.(feeds.LiquidationEvent)
			if !ok {
				t.Fatalf("payload type %T", m.Payload)
			}
			got = append(got, p)
		case <-deadline:
			t.Fatalf("got %d, want 2", len(got))
		}
	}
	if got[0].Side != "sell" || got[1].Side != "buy" {
		t.Errorf("sides=%v %v", got[0].Side, got[1].Side)
	}
	if got[0].Price != 65000 || got[0].Quantity != 0.5 {
		t.Errorf("first: price=%v qty=%v", got[0].Price, got[0].Quantity)
	}
	if got[0].Notional != 65000*0.5 {
		t.Errorf("first notional=%v, want %v", got[0].Notional, 65000*0.5)
	}
}

// TestHyperliquid_HandleAssetCtx_PopulatesSnapshot drives
// handleAssetCtx directly with a canned activeAssetCtx payload and
// asserts that a PerpetualSnapshot is published on the right topic
// with the mark / oracle / funding / OI fields filled.
func TestHyperliquid_HandleAssetCtx_PopulatesSnapshot(t *testing.T) {
	b := bus.New(4)
	sub := b.Subscribe(4, "perp.hyperliquid.*")
	defer b.Unsubscribe(sub)

	a := NewHyperliquidAdapter(b, time.Minute)

	// Canned payload — numeric strings match HL's JSON shape.
	data := []byte(`{
		"coin": "BTC",
		"ctx": {
			"markPx": "65100.5",
			"oraclePx": "65090.25",
			"funding": "0.00005",
			"openInterest": "12345"
		}
	}`)
	a.handleAssetCtx(data)

	select {
	case m := <-sub.C:
		if m.Topic != "perp.hyperliquid.BTCUSD" {
			t.Fatalf("topic=%q", m.Topic)
		}
		p, ok := m.Payload.(feeds.PerpetualSnapshot)
		if !ok {
			t.Fatalf("payload type %T", m.Payload)
		}
		if p.MarkPrice != 65100.5 || p.IndexPrice != 65090.25 {
			t.Errorf("mark=%v index=%v", p.MarkPrice, p.IndexPrice)
		}
		if p.FundingRate != 0.00005 || p.OpenInterestBase != 12345 {
			t.Errorf("fr=%v oi=%v", p.FundingRate, p.OpenInterestBase)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no message published")
	}
}
