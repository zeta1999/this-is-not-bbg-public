package feeds

import (
	"encoding/json"
	"testing"
	"time"
)

// TestPerpetualSnapshot_JSONRoundTrip asserts the canonical shape is
// stable under JSON marshal/unmarshal — the datalake writer relies
// on this (each bus payload lands in a JSONL file as the canonical
// field names).
func TestPerpetualSnapshot_JSONRoundTrip(t *testing.T) {
	ts := time.Unix(1700000000, 0).UTC()
	orig := PerpetualSnapshot{
		Instrument:              "BTCUSDT",
		Exchange:                "bybit",
		Timestamp:               ts,
		MarkPrice:               50000,
		IndexPrice:              50010,
		FundingRate:             0.0001,
		NextFundingRate:         0.00012,
		NextFundingTime:         ts.Add(8 * time.Hour).UnixMilli(),
		OpenInterestBase:        12345,
		LastLiquidationSide:     "buy",
		LastLiquidationPrice:    50100,
		LastLiquidationQuantity: 1.5,
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back PerpetualSnapshot
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back != orig {
		t.Fatalf("round-trip mismatch:\n  orig=%+v\n  back=%+v", orig, back)
	}
}

// TestPerpetualSnapshot_ZeroFieldsOmitted confirms that an adapter
// that only has FundingRate + MarkPrice (e.g. OKX) doesn't pollute
// the JSON with zero-valued fields it doesn't populate.
func TestPerpetualSnapshot_ZeroFieldsOmitted(t *testing.T) {
	p := PerpetualSnapshot{
		Instrument:  "BTC-USDT-SWAP",
		Exchange:    "okx",
		Timestamp:   time.Unix(1700000000, 0).UTC(),
		FundingRate: 0.00005,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, field := range []string{
		"MarkPrice", "IndexPrice", "NextFundingRate",
		"NextFundingTime", "OpenInterestBase", "OpenInterestQuote",
		"LastLiquidationSide", "LastLiquidationPrice", "LastLiquidationQuantity",
	} {
		if contains(s, field) {
			t.Errorf("zero field %q should be omitted, JSON=%s", field, s)
		}
	}
	for _, required := range []string{"Instrument", "Exchange", "Timestamp", "FundingRate"} {
		if !contains(s, required) {
			t.Errorf("required field %q missing from JSON=%s", required, s)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
