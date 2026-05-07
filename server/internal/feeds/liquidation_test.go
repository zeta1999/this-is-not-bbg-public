package feeds

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLiquidationEvent_JSONRoundTrip(t *testing.T) {
	ts := time.Unix(1700000000, 0).UTC()
	orig := LiquidationEvent{
		Instrument: "BTCUSD",
		Exchange:   "hyperliquid",
		Timestamp:  ts,
		Side:       "sell",
		Price:      65100,
		Quantity:   1.5,
		Notional:   97650,
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back LiquidationEvent
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back != orig {
		t.Fatalf("round-trip mismatch:\n  orig=%+v\n  back=%+v", orig, back)
	}
}

func TestLiquidationEvent_OmitsZero(t *testing.T) {
	e := LiquidationEvent{
		Instrument: "BTCUSD",
		Exchange:   "bybit",
		Timestamp:  time.Unix(1, 0).UTC(),
		Side:       "buy",
		Price:      100,
	}
	data, _ := json.Marshal(e)
	for _, zero := range []string{"Quantity", "Notional"} {
		if strings.Contains(string(data), zero) {
			t.Errorf("%q not omitted when zero: %s", zero, data)
		}
	}
}
