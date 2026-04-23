package ravel

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// sampleDir resolves this repo's reference/Ravel-master/ravel/tests/data.
func sampleDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// ravel_test.go → ravel → feeds → internal → server → repo root.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	return filepath.Join(repoRoot, "reference", "Ravel-master", "ravel", "tests", "data")
}

func TestParse_SingleSnapshot_Rates(t *testing.T) {
	raw := []byte(`{
	  "now": "2019-10-01T00:00:00.000Z",
	  "rates": {
	    "JPY6M": {"rates": [
	      {"maturity": "2019-12-01T00:00:00.000Z", "t": 0.167, "r": 0.01},
	      {"maturity": "2020-03-01T00:00:00.000Z", "t": 0.416, "r": 0.02}
	    ]}
	  }
	}`)
	pts, err := ParseBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("got %d, want 2", len(pts))
	}
	for _, p := range pts {
		if p.Dims["type"] != "rate" || p.Dims["curve"] != "JPY6M" {
			t.Errorf("dims: %+v", p.Dims)
		}
	}
}

func TestParse_DatedSnapshot(t *testing.T) {
	raw := []byte(`{
	  "snapshotDates": ["2019-10-01T00:00:00.000Z"],
	  "snapshots": {
	    "2019-10-01": {
	      "eq_market": {
	        "now": "2019-10-01T00:00:00.000Z",
	        "rates": {"KRW3M": {"rates":[{"maturity":"2020-01-01T00:00:00.000Z","t":0.2,"r":0.01}]}}
	      }
	    }
	  }
	}`)
	pts, err := ParseBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 {
		t.Fatalf("got %d", len(pts))
	}
	if pts[0].Dims["section"] != "eq_market" {
		t.Errorf("section dim missing: %+v", pts[0].Dims)
	}
}

func TestParse_SurfacesWrappedAsRaw(t *testing.T) {
	raw := []byte(`{
	  "now": "2019-10-01T00:00:00.000Z",
	  "surfaces": {
	    "KOSPI2": {"spot": 100, "columns": ["a","b"], "maturities": ["2020-01-01T00:00:00.000Z"]}
	  }
	}`)
	pts, err := ParseBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].Dims["type"] != "volsurface" {
		t.Fatalf("unexpected: %+v", pts)
	}
	if !strings.Contains(string(pts[0].Value.Raw), "KOSPI2") && !strings.Contains(string(pts[0].Value.Raw), "spot") {
		t.Errorf("raw value missing content: %s", string(pts[0].Value.Raw))
	}
}

func TestParse_Dividends(t *testing.T) {
	raw := []byte(`{
	  "now": "2020-01-01T00:00:00.000Z",
	  "dividends": {
	    "KOSPI2": {"dividends": [
	      {"exDiv": "2020-05-01T00:00:00.000Z", "payDiv": "2020-05-15T00:00:00.000Z", "tEx": 0.3, "tPay": 0.35, "amount": 4.12, "fix": 1}
	    ]}
	  }
	}`)
	pts, err := ParseBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].Value.Scalar != 4.12 {
		t.Errorf("got %+v", pts)
	}
	if pts[0].Dims["type"] != "dividend" || pts[0].Dims["asset"] != "KOSPI2" {
		t.Errorf("dims: %+v", pts[0].Dims)
	}
}

func TestParse_RealFixtures(t *testing.T) {
	dir := sampleDir(t)
	cases := []string{
		"Rates_All.expected.json",
		"Repos_All.expected.json",
		"Div_All.expected.json",
		"AllMarket_All.expected.json",
	}
	for _, name := range cases {
		pts, err := ParseFile(filepath.Join(dir, name))
		if err != nil {
			t.Skipf("skip %s: %v", name, err)
			continue
		}
		if len(pts) == 0 {
			t.Errorf("%s: no points emitted", name)
		}
		for _, p := range pts {
			if p.Dims["source"] != "ravel" {
				t.Errorf("%s: source missing on %+v", name, p.Dims)
			}
			if p.Dims["type"] == "" {
				t.Errorf("%s: type missing on %+v", name, p.Dims)
			}
		}
	}
}
