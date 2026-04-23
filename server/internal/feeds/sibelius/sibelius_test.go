package sibelius

import (
	"path/filepath"
	"runtime"
	"testing"
)

// sampleDir resolves the sibling ../Sibelius/TestData/inputs directory
// relative to this repo. Skipped when absent so CI isn't forced to
// mount the Sibelius test data.
func sampleDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// sibelius_test.go → sibelius → feeds → internal → server → repo root.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	// Sibelius lives as a sibling to this-is-not-bbg: work/Sibelius.
	return filepath.Join(repoRoot, "..", "Sibelius", "TestData", "inputs")
}

func TestParse_InstrumentsProducesOnePointPerRow(t *testing.T) {
	raw := []byte(`{
	  "name": "AriaCalibrate/1",
	  "arguments": [{
	    "model": "heston",
	    "spot": 100,
	    "rate": 0.05,
	    "instruments": [
	      {"strike": 80, "T": 0.5, "price": 22.5},
	      {"strike": 90, "T": 0.5, "price": 14.8},
	      {"strike": 100, "T": 0.5, "price": 8.5}
	    ]
	  }]
	}`)
	pts, err := ParseBytes(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pts) != 3 {
		t.Fatalf("got %d points, want 3", len(pts))
	}
	for _, p := range pts {
		if p.Dims["type"] != "option" {
			t.Errorf("type: %q", p.Dims["type"])
		}
		if p.Dims["source"] != "sibelius" {
			t.Errorf("source: %q", p.Dims["source"])
		}
		if p.Dims["model"] != "heston" {
			t.Errorf("model: %q", p.Dims["model"])
		}
		if p.Dims["spot"] != "100" {
			t.Errorf("spot: %q", p.Dims["spot"])
		}
	}
	if pts[0].Value.Scalar != 22.5 {
		t.Errorf("first price: %v", pts[0].Value.Scalar)
	}
}

func TestParse_BookProducesOnePointPerDeal(t *testing.T) {
	raw := []byte(`{
	  "name": "AriaBacktest/1",
	  "arguments": [{
	    "book": [
	      {"type": "swap", "strike": 0.04, "maturity": 5, "notional": 1000000, "is_call": true},
	      {"type": "swaption", "strike": 0.04, "maturity": 4, "notional": 2000000, "is_call": false}
	    ],
	    "days": 20
	  }]
	}`)
	pts, err := ParseBytes(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pts) != 2 {
		t.Fatalf("got %d, want 2", len(pts))
	}
	if pts[0].Dims["type"] != "swap" || pts[0].Dims["side"] != "call" {
		t.Errorf("deal 0 dims: %v", pts[0].Dims)
	}
	if pts[1].Dims["type"] != "swaption" || pts[1].Dims["side"] != "put" {
		t.Errorf("deal 1 dims: %v", pts[1].Dims)
	}
	if pts[1].Value.Scalar != 2_000_000 {
		t.Errorf("notional: %v", pts[1].Value.Scalar)
	}
}

func TestParse_TradesCarryScript(t *testing.T) {
	raw := []byte(`{
	  "name": "AriaBookPrice/1",
	  "arguments": [{
	    "trades": [
	      {"script": "contract C1 { }", "notional": 1000}
	    ]
	  }]
	}`)
	pts, err := ParseBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].Dims["type"] != "contract" {
		t.Fatalf("got %+v", pts)
	}
	if pts[0].Value.Scalar != 1000 {
		t.Errorf("notional: %v", pts[0].Value.Scalar)
	}
	if len(pts[0].Value.Raw) == 0 {
		t.Error("script raw missing")
	}
}

func TestParse_UnknownShapeFallsBackToJobPoint(t *testing.T) {
	raw := []byte(`{"name":"AriaWeird/1","arguments":[{"whatever":1}]}`)
	pts, err := ParseBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 {
		t.Fatalf("got %d, want 1", len(pts))
	}
	if pts[0].Dims["type"] != "sibelius_job" {
		t.Errorf("type: %q", pts[0].Dims["type"])
	}
	if len(pts[0].Value.Raw) == 0 {
		t.Error("raw payload missing")
	}
}

func TestParse_RealFixtures(t *testing.T) {
	dir := sampleDir(t)
	cases := []string{
		"aria_calibrate_heston.json",
		"aria_backtest.json",
		"aria_book_price.json",
	}
	for _, name := range cases {
		path := filepath.Join(dir, name)
		pts, err := ParseFile(path)
		if err != nil {
			t.Skipf("skip %s (fixture not available: %v)", name, err)
			continue
		}
		if len(pts) == 0 {
			t.Errorf("%s: no points", name)
		}
		for _, p := range pts {
			if p.Dims["type"] == "" {
				t.Errorf("%s: point missing type: %+v", name, p.Dims)
			}
			if p.Dims["source"] != "sibelius" {
				t.Errorf("%s: source missing", name)
			}
		}
	}
}
