package app

import (
	"reflect"
	"testing"
)

func TestCompleteSelection_NoMatches(t *testing.T) {
	universe := []string{"BTCUSDT", "ETHUSDT"}
	completed, matches := completeSelection("XYZ", universe)
	if completed != "" || matches != nil {
		t.Fatalf("completed=%q matches=%v, want empty", completed, matches)
	}
}

func TestCompleteSelection_Unique(t *testing.T) {
	universe := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	completed, matches := completeSelection("eth", universe)
	if completed != "ETHUSDT" {
		t.Fatalf("completed=%q, want ETHUSDT", completed)
	}
	if !reflect.DeepEqual(matches, []string{"ETHUSDT"}) {
		t.Fatalf("matches=%v", matches)
	}
}

func TestCompleteSelection_ExtendsToCommonPrefix(t *testing.T) {
	universe := []string{"BTCUSDT", "BTCUSDC", "BTCBUSD"}
	completed, matches := completeSelection("bt", universe)
	if completed != "BTC" {
		t.Fatalf("completed=%q, want BTC", completed)
	}
	if len(matches) != 3 {
		t.Fatalf("matches=%v", matches)
	}
}

func TestCompleteSelection_NoExtensionWhenAlreadyAtLCP(t *testing.T) {
	universe := []string{"BTCUSDT", "BTCUSDC"}
	completed, matches := completeSelection("BTCUS", universe)
	// LCP = "BTCUSD", partial = "BTCUS" → extends to BTCUSD.
	if completed != "BTCUSD" {
		t.Fatalf("completed=%q, want BTCUSD", completed)
	}
	if len(matches) != 2 {
		t.Fatalf("matches=%v", matches)
	}

	// Now at the LCP itself — extension returns "" but matches stay.
	completed2, matches2 := completeSelection("BTCUSD", universe)
	if completed2 != "" {
		t.Fatalf("at-LCP completed=%q, want empty", completed2)
	}
	if len(matches2) != 2 {
		t.Fatalf("at-LCP matches=%v", matches2)
	}
}
