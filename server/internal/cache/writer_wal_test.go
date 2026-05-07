package cache

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// TestWriter_WAL_PersistsThroughQueue exercises the Phase 5 disk-spill
// path end-to-end: a bus publish must appear in BBolt after the WAL
// drainer catches up.
func TestWriter_WAL_PersistsThroughQueue(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "cache.db")
	walDir := dbPath + ".wal"

	store, err := Open(dbPath, time.Hour)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	b := bus.New(64)

	w := NewWriter(store, b).EnableWAL(walDir, 4<<20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Publish a trade and verify it lands in BBolt under the expected key.
	ts := time.Unix(1700000000, 0)
	tr := feeds.Trade{
		Exchange:   "testex",
		Instrument: "BTCUSDT",
		TradeID:    "t1",
		Price:      101.25,
		Quantity:   0.5,
		Timestamp:  ts,
	}
	b.Publish(bus.Message{Topic: "trade.testex.BTCUSDT", Payload: tr})

	// Poll for the key to appear (drainer runs async).
	key := "testex/BTCUSDT/t1/" + timeToMilliString(ts)
	deadline := time.Now().Add(2 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		got, err = store.Get("trades", key)
		if err == nil && len(got) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || len(got) == 0 {
		t.Fatalf("trade never landed in cache: err=%v got=%q", err, got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

// TestWriter_WAL_ReopenResumes writes a batch with the drainer held
// off, then reopens the writer and verifies the backlog drains.
func TestWriter_WAL_ReopenResumes(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "cache.db")
	walDir := dbPath + ".wal"

	b := bus.New(64)

	// First run: produce some messages, stop immediately so some are
	// likely still in the WAL.
	{
		store, err := Open(dbPath, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		w := NewWriter(store, b).EnableWAL(walDir, 4<<20)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- w.Run(ctx) }()
		for i := 0; i < 50; i++ {
			ts := time.Unix(int64(1700000000+i), 0)
			b.Publish(bus.Message{Topic: "trade.x.BTC", Payload: feeds.Trade{
				Exchange:   "x",
				Instrument: "BTC",
				TradeID:    intToStr(i),
				Timestamp:  ts,
			}})
		}
		time.Sleep(20 * time.Millisecond)
		cancel()
		<-done
		_ = store.Close()
	}

	// Second run: empty bus, reopen, drainer should flush remnants.
	{
		store, err := Open(dbPath, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		w := NewWriter(store, b).EnableWAL(walDir, 4<<20)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- w.Run(ctx) }()

		// Poll — every trade we published should end up in cache.
		deadline := time.Now().Add(2 * time.Second)
		var seen int
		for time.Now().Before(deadline) {
			seen = 0
			for i := 0; i < 50; i++ {
				ts := time.Unix(int64(1700000000+i), 0)
				key := "x/BTC/" + intToStr(i) + "/" + timeToMilliString(ts)
				v, err := store.Get("trades", key)
				if err == nil && len(v) > 0 {
					seen++
				}
			}
			if seen == 50 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if seen != 50 {
			t.Fatalf("only %d/50 trades recovered from WAL", seen)
		}
		cancel()
		<-done
	}
}

func timeToMilliString(t time.Time) string {
	return intToStr64(t.UnixNano())
}

func intToStr(i int) string { return intToStr64(int64(i)) }

func intToStr64(i int64) string {
	// Using simple base-10 conversion to avoid pulling fmt into the
	// hot path of the test helper — though it doesn't matter here.
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}
