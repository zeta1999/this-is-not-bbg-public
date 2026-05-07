package views

import "testing"

func TestMatchesTokenQuery_SingleToken(t *testing.T) {
	if !MatchesTokenQuery("btc", "BTCUSDT", "binance") {
		t.Fatal("btc should match BTCUSDT")
	}
	if MatchesTokenQuery("xrp", "BTCUSDT", "binance") {
		t.Fatal("xrp should not match BTCUSDT")
	}
}

func TestMatchesTokenQuery_MultiToken(t *testing.T) {
	cases := []struct {
		q    string
		want bool
	}{
		{"BTC bin", true},          // both tokens hit
		{"BTCUSDT bin", true},      // exact instrument + prefix exchange
		{"BTCUSDT binance", true},  // exact both
		{"btc bina", true},         // case-insensitive
		{"BTC kraken", false},      // exchange token absent
		{"sol bin", false},         // instrument token absent
		{"", false},                // empty rejected
		{"   ", false},             // whitespace-only rejected
	}
	for _, tc := range cases {
		got := MatchesTokenQuery(tc.q, "BTCUSDT", "binance", "BTCUSDT/binance")
		if got != tc.want {
			t.Errorf("MatchesTokenQuery(%q) = %v, want %v", tc.q, got, tc.want)
		}
	}
}

func TestMatchesTokenQuery_AllFieldsEmpty(t *testing.T) {
	if MatchesTokenQuery("btc", "", "", "") {
		t.Fatal("query with no haystacks should not match")
	}
}

func TestMatchesTokenQuery_PartialFieldsOK(t *testing.T) {
	// One field empty — shouldn't break matching against the others.
	if !MatchesTokenQuery("eth coin", "ETH-USD", "", "coinbase") {
		t.Fatal("token spread across non-empty fields should match")
	}
}
