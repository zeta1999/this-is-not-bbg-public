package dex

import (
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

func TestDYDX_HandleTrades_Emits(t *testing.T) {
	b := bus.New(4)
	sub := b.Subscribe(4, "trade.dydx.*")
	defer b.Unsubscribe(sub)

	a := NewDYDXV4Adapter(b, 0, []string{"BTC-USD"}, "wss://example")

	payload := []byte(`{"trades":[
		{"id":"t1","side":"BUY","size":"0.5","price":"65000","createdAt":"2026-04-25T00:00:00Z"},
		{"id":"t2","side":"SELL","size":"0.25","price":"64900","createdAt":"2026-04-25T00:00:01Z"}
	]}`)
	a.handleTrades("BTC-USD", payload)

	var got []feeds.Trade
	deadline := time.After(200 * time.Millisecond)
	for len(got) < 2 {
		select {
		case m := <-sub.C:
			if m.Topic != "trade.dydx.BTC-USD" {
				t.Fatalf("topic=%q", m.Topic)
			}
			got = append(got, m.Payload.(feeds.Trade))
		case <-deadline:
			t.Fatalf("got %d/2", len(got))
		}
	}
	if got[0].Side != "buy" || got[1].Side != "sell" {
		t.Errorf("sides=%v %v", got[0].Side, got[1].Side)
	}
	if got[0].Price != 65000 || got[1].Quantity != 0.25 {
		t.Errorf("values off: %+v / %+v", got[0], got[1])
	}
}

func TestDYDX_HandleMarkets_Snapshot(t *testing.T) {
	b := bus.New(4)
	sub := b.Subscribe(4, "perp.dydx.*")
	defer b.Unsubscribe(sub)

	a := NewDYDXV4Adapter(b, 0, nil, "wss://example")

	data := []byte(`{"markets":{
		"BTC-USD":{"oraclePrice":"65100","nextFundingRate":"0.0001","openInterest":"1234","trailingFundingRate":"0.00005"},
		"ETH-USD":{"oraclePrice":"3300","openInterest":"5678"}
	}}`)
	a.handleMarkets(data)

	seen := map[string]feeds.PerpetualSnapshot{}
	deadline := time.After(200 * time.Millisecond)
	for len(seen) < 2 {
		select {
		case m := <-sub.C:
			p := m.Payload.(feeds.PerpetualSnapshot)
			seen[p.Instrument] = p
		case <-deadline:
			t.Fatalf("got %d/2", len(seen))
		}
	}
	if seen["BTC-USD"].IndexPrice != 65100 || seen["BTC-USD"].OpenInterestBase != 1234 {
		t.Errorf("BTC-USD: %+v", seen["BTC-USD"])
	}
	if seen["ETH-USD"].IndexPrice != 3300 || seen["ETH-USD"].OpenInterestBase != 5678 {
		t.Errorf("ETH-USD: %+v", seen["ETH-USD"])
	}
}

func TestDYDX_HandleMarkets_Update(t *testing.T) {
	b := bus.New(4)
	sub := b.Subscribe(4, "perp.dydx.*")
	defer b.Unsubscribe(sub)

	a := NewDYDXV4Adapter(b, 0, nil, "wss://example")

	data := []byte(`{"trading":{"BTC-USD":{"oraclePrice":"65200"}}}`)
	a.handleMarkets(data)

	select {
	case m := <-sub.C:
		p := m.Payload.(feeds.PerpetualSnapshot)
		if p.IndexPrice != 65200 {
			t.Errorf("IndexPrice=%v", p.IndexPrice)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no message")
	}
}

func TestDYDX_HandleMarkets_DropsAllZero(t *testing.T) {
	b := bus.New(4)
	sub := b.Subscribe(4, "perp.dydx.*")
	defer b.Unsubscribe(sub)

	a := NewDYDXV4Adapter(b, 0, nil, "wss://example")

	// All-empty values → publishMarket should skip.
	data := []byte(`{"markets":{"BTC-USD":{"oraclePrice":"","openInterest":""}}}`)
	a.handleMarkets(data)

	select {
	case m := <-sub.C:
		t.Fatalf("unexpected publish: %+v", m)
	case <-time.After(50 * time.Millisecond):
		// good
	}
}
