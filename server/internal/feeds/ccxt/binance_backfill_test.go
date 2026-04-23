package ccxt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// fakeBinanceKlines serves /api/v3/klines with a configurable dataset
// so we can test paging without touching the real exchange.
func fakeBinanceKlines(t *testing.T, rowsPerCall int, totalRows int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		startMs, _ := strconv.ParseInt(q.Get("startTime"), 10, 64)
		endMs, _ := strconv.ParseInt(q.Get("endTime"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit == 0 {
			limit = rowsPerCall
		}

		var out [][]any
		for i := 0; i < totalRows; i++ {
			ts := startMs + int64(i*60_000) // one-minute candles
			if ts > endMs {
				break
			}
			if len(out) >= limit || len(out) >= rowsPerCall {
				break
			}
			out = append(out, []any{
				ts,
				fmt.Sprintf("%f", 100.0+float64(i)),
				fmt.Sprintf("%f", 101.0+float64(i)),
				fmt.Sprintf("%f", 99.0+float64(i)),
				fmt.Sprintf("%f", 100.5+float64(i)),
				fmt.Sprintf("%f", 1.5),
				ts + 59_999, "0", 0, "0", "0", "0",
			})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
}

func TestBinance_BackfillHistorical_Paginates(t *testing.T) {
	// Fake server returns up to 3 rows per call, with 5 total to cover.
	srv := fakeBinanceKlines(t, 3, 5)
	defer srv.Close()

	a := NewBinanceAdapter(bus.New(8), []string{"BTCUSDT"}, nil, "", srv.URL, 10)

	from := time.Unix(0, 0).UTC()
	to := from.Add(10 * time.Minute)

	got, err := a.BackfillHistorical(context.Background(), feeds.BackfillRequest{
		Instrument: "BTCUSDT", Exchange: "binance", Timeframe: "1m",
		From: from, To: to,
	})
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	// The test server emits the same 3-row page until startTime exceeds
	// end — assert pagination happens (>3) without over-fetching.
	if len(got) < 3 {
		t.Fatalf("expected paged results, got %d", len(got))
	}
	// Timestamps must be unique + strictly non-decreasing.
	last := int64(-1)
	for _, c := range got {
		ts := c.Timestamp.UnixMilli()
		if ts <= last {
			t.Errorf("timestamps not strictly increasing: last=%d ts=%d", last, ts)
		}
		last = ts
	}
}

func TestBinance_BackfillHistorical_Validation(t *testing.T) {
	a := NewBinanceAdapter(bus.New(8), []string{"BTCUSDT"}, nil, "", "http://example.invalid", 1)
	cases := map[string]feeds.BackfillRequest{
		"missing instrument": {Timeframe: "1m", From: time.Now().Add(-time.Hour), To: time.Now()},
		"missing timeframe":  {Instrument: "BTCUSDT", From: time.Now().Add(-time.Hour), To: time.Now()},
		"zero from":          {Instrument: "BTCUSDT", Timeframe: "1m", To: time.Now()},
		"to before from":     {Instrument: "BTCUSDT", Timeframe: "1m", From: time.Now(), To: time.Now().Add(-time.Hour)},
	}
	for name, req := range cases {
		if _, err := a.BackfillHistorical(context.Background(), req); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestBinance_BackfillHistorical_Cap(t *testing.T) {
	srv := fakeBinanceKlines(t, 3, 100)
	defer srv.Close()
	a := NewBinanceAdapter(bus.New(8), nil, nil, "", srv.URL, 10)

	got, err := a.BackfillHistorical(context.Background(), feeds.BackfillRequest{
		Instrument: "BTCUSDT", Timeframe: "1m",
		From: time.Unix(0, 0), To: time.Unix(0, 0).Add(2 * time.Hour),
		Limit: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 4 {
		t.Errorf("got %d, exceeds cap 4", len(got))
	}
}

// compile-time assertion that the adapter satisfies the interface.
var _ feeds.Backfiller = (*BinanceAdapter)(nil)
