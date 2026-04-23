package guicache

import (
	"testing"
	"time"
)

func TestOHLC_CapEvictsOldest(t *testing.T) {
	c := NewOHLC(3)
	base := time.Unix(1700000000, 0)
	for i := 0; i < 5; i++ {
		c.Add("BTCUSDT", "1m", OHLCRow{
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			Close:     float64(i),
		})
	}
	rows := c.Rows("BTCUSDT", "1m")
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if rows[0].Close != 2 || rows[2].Close != 4 {
		t.Errorf("expected close=2..4, got %+v", rows)
	}
}

func TestOHLC_OutOfOrderInsertStaysSorted(t *testing.T) {
	c := NewOHLC(10)
	base := time.Unix(1700000000, 0)
	c.Add("X", "1m", OHLCRow{Timestamp: base.Add(2 * time.Minute), Close: 2})
	c.Add("X", "1m", OHLCRow{Timestamp: base.Add(0 * time.Minute), Close: 0})
	c.Add("X", "1m", OHLCRow{Timestamp: base.Add(1 * time.Minute), Close: 1})
	rows := c.Rows("X", "1m")
	if len(rows) != 3 || rows[0].Close != 0 || rows[1].Close != 1 || rows[2].Close != 2 {
		t.Errorf("rows not sorted: %+v", rows)
	}
}

func TestOHLC_SameTimestampOverwrites(t *testing.T) {
	c := NewOHLC(10)
	ts := time.Unix(1700000000, 0)
	c.Add("X", "1m", OHLCRow{Timestamp: ts, Close: 1})
	c.Add("X", "1m", OHLCRow{Timestamp: ts, Close: 2}) // same candle republished
	rows := c.Rows("X", "1m")
	if len(rows) != 1 || rows[0].Close != 2 {
		t.Errorf("expected single row with Close=2, got %+v", rows)
	}
}

func TestOHLC_KeysAreIsolated(t *testing.T) {
	c := NewOHLC(5)
	ts := time.Unix(1700000000, 0)
	c.Add("BTC", "1m", OHLCRow{Timestamp: ts, Close: 1})
	c.Add("BTC", "1h", OHLCRow{Timestamp: ts, Close: 2})
	c.Add("ETH", "1m", OHLCRow{Timestamp: ts, Close: 3})
	if len(c.Rows("BTC", "1m")) != 1 || c.Rows("BTC", "1m")[0].Close != 1 {
		t.Errorf("BTC/1m: %+v", c.Rows("BTC", "1m"))
	}
	if c.Rows("BTC", "1h")[0].Close != 2 {
		t.Errorf("BTC/1h: %+v", c.Rows("BTC", "1h"))
	}
	if c.Rows("ETH", "1m")[0].Close != 3 {
		t.Errorf("ETH/1m: %+v", c.Rows("ETH", "1m"))
	}
}

func TestOHLC_Drop(t *testing.T) {
	c := NewOHLC(5)
	c.Add("X", "1m", OHLCRow{Timestamp: time.Unix(1, 0), Close: 1})
	c.Drop("X", "1m")
	if rows := c.Rows("X", "1m"); len(rows) != 0 {
		t.Errorf("expected empty after Drop, got %+v", rows)
	}
}

func TestTradesRing_NewestFirst(t *testing.T) {
	r := NewTrades(3)
	for i := 0; i < 5; i++ {
		r.Add("BTC", Trade{ID: string(rune('a' + i))})
	}
	recent := r.Recent("BTC", 10)
	if len(recent) != 3 {
		t.Fatalf("got %d, want 3: %+v", len(recent), recent)
	}
	// Most recent was 'e', then 'd', then 'c'.
	want := []string{"e", "d", "c"}
	for i, w := range want {
		if recent[i].ID != w {
			t.Errorf("slot %d: got %q want %q", i, recent[i].ID, w)
		}
	}
}

func TestTradesRing_SmallerN(t *testing.T) {
	r := NewTrades(10)
	for i := 0; i < 4; i++ {
		r.Add("X", Trade{ID: string(rune('a' + i))})
	}
	if got := r.Recent("X", 2); len(got) != 2 || got[0].ID != "d" || got[1].ID != "c" {
		t.Errorf("Recent(2): %+v", got)
	}
}

func TestTradesRing_UnknownInstrument(t *testing.T) {
	r := NewTrades(10)
	if got := r.Recent("nope", 5); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestLOB_TrimAndSort(t *testing.T) {
	l := NewLOB(3)
	l.Set("X",
		[]LOBLevel{{Price: 100, Quantity: 1}, {Price: 99, Quantity: 1}, {Price: 101, Quantity: 1}, {Price: 98, Quantity: 1}, {Price: 102, Quantity: 1}},
		[]LOBLevel{{Price: 110, Quantity: 1}, {Price: 108, Quantity: 1}, {Price: 109, Quantity: 1}, {Price: 111, Quantity: 1}},
	)
	bids, asks := l.Snapshot("X")
	// Bids descending, trimmed to 3.
	if len(bids) != 3 || bids[0].Price != 102 || bids[2].Price != 100 {
		t.Errorf("bids: %+v", bids)
	}
	// Asks ascending, trimmed to 3.
	if len(asks) != 3 || asks[0].Price != 108 || asks[2].Price != 110 {
		t.Errorf("asks: %+v", asks)
	}
}

func TestGUICacheSettings_WithDefaults(t *testing.T) {
	// zero → defaults
	// (constants live in tui/internal/config; test via the guicache
	// package's NewOHLC default-guard instead)
	if NewOHLC(0).Limit != 1 {
		t.Errorf("NewOHLC(0) should clamp to 1")
	}
	if NewTrades(-5).Limit != 1 {
		t.Errorf("NewTrades(-5) should clamp to 1")
	}
	if NewLOB(0).DepthLimit != 1 {
		t.Errorf("NewLOB(0) should clamp to 1")
	}
}
