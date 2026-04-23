package dimpath

import (
	"reflect"
	"testing"
)

func TestBuild_Canonical(t *testing.T) {
	// Different insertion orders must produce the same path.
	a := Build(map[string]string{"type": "ohlc", "exchange": "binance", "instrument": "BTCUSDT"})
	b := Build(map[string]string{"instrument": "BTCUSDT", "type": "ohlc", "exchange": "binance"})
	if a != b {
		t.Errorf("not canonical: %q vs %q", a, b)
	}
	want := "dims=exchange=binance;instrument=BTCUSDT;type=ohlc"
	if a != want {
		t.Errorf("got %q, want %q", a, want)
	}
}

func TestBuild_DropsEmptyValues(t *testing.T) {
	got := Build(map[string]string{"type": "ohlc", "strike": "", "side": ""})
	want := "dims=type=ohlc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuild_Empty(t *testing.T) {
	if got := Build(nil); got != "dims=" {
		t.Errorf("nil: got %q", got)
	}
	if got := Build(map[string]string{}); got != "dims=" {
		t.Errorf("empty: got %q", got)
	}
}

func TestBuild_EscapesReservedChars(t *testing.T) {
	got := Build(map[string]string{"asset": "BTC;USD", "pair": "a=b"})
	// After percent-encoding ';' → %3B, '=' → %3D.
	if got != "dims=asset=BTC%3BUSD;pair=a%3Db" {
		t.Errorf("got %q", got)
	}
}

func TestParse_RoundTrips(t *testing.T) {
	orig := map[string]string{
		"type":       "ohlc",
		"exchange":   "binance",
		"instrument": "BTCUSDT",
		"tenor":      "spot",
		"asset":      "BTC;USD",
	}
	out, err := Parse(Build(orig))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, orig) {
		t.Errorf("round-trip mismatch\n got: %v\nwant: %v", out, orig)
	}
}

func TestParse_RejectsBadSegments(t *testing.T) {
	cases := []string{
		"type=ohlc",           // missing dims= prefix
		"dims=key-without-eq", // malformed pair
	}
	for _, c := range cases {
		if _, err := Parse(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestMergeDefaults(t *testing.T) {
	dims := map[string]string{"type": "ohlc", "exchange": "binance"}
	defs := map[string]string{"type": "unknown", "tenor": "spot", "strike": ""}
	out := MergeDefaults(dims, defs)
	// user's value wins for "type"
	if out["type"] != "ohlc" {
		t.Errorf("type: %q", out["type"])
	}
	// default fills missing "tenor"
	if out["tenor"] != "spot" {
		t.Errorf("tenor: %q", out["tenor"])
	}
	// exchange from user
	if out["exchange"] != "binance" {
		t.Errorf("exchange: %q", out["exchange"])
	}
	// empty-string default survives (callers may drop on Build)
	if _, ok := out["strike"]; !ok {
		t.Error("default strike missing")
	}
}

func TestFromLegacyTopic(t *testing.T) {
	cases := map[string]map[string]string{
		"ohlc.binance.BTCUSDT": {"type": "ohlc", "exchange": "binance", "instrument": "BTCUSDT"},
		"news.reuters":         {"type": "news", "source": "reuters"},
		"news":                 {"type": "news"},
	}
	for topic, want := range cases {
		got := FromLegacyTopic(topic)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", topic, got, want)
		}
	}
}
