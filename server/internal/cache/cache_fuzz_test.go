package cache

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

// FuzzStorePutGet asserts the fundamental invariant: a value written
// to the cache with TTL T can be read back, unchanged, within T.
// Arbitrary keys and values are allowed (BBolt accepts any bytes).
func FuzzStorePutGet(f *testing.F) {
	f.Add("ohlc", "binance/BTCUSDT/1m/1700000000", []byte("hello"))
	f.Add("trades", "k", []byte(""))
	f.Add("news", "k\x00with\x00nulls", []byte{0xff, 0, 0xff})
	f.Add("alerts", "unicode-🦀", []byte("payload-🦀"))

	f.Fuzz(func(t *testing.T, bucket, key string, value []byte) {
		// Skip inputs we know the store rejects on contract grounds;
		// they're not drift bugs.
		if key == "" {
			t.Skip() // BBolt requires non-empty keys
		}
		known := false
		for _, name := range bucketNames {
			if bucket == name {
				known = true
				break
			}
		}
		if !known {
			t.Skip()
		}

		path := filepath.Join(t.TempDir(), "cache.db")
		s, err := Open(path, time.Hour)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer s.Close()

		if err := s.Put(bucket, key, value); err != nil {
			t.Fatalf("put: %v", err)
		}
		got, err := s.Get(bucket, key)
		if err != nil {
			t.Fatalf("get: %v", err)
		}

		// Get returns a fresh copy; value may be len==0 for empty input.
		if !bytes.Equal(got, value) {
			t.Fatalf("round-trip mismatch: got %x want %x", got, value)
		}
	})
}
