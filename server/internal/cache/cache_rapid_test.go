package cache

import (
	"bytes"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestRapid_GetAfterPutWithinTTL is the Go mirror of the TLA+ cache
// safety invariant: for any sequence of well-formed puts, a read
// issued within TTL must return the exact bytes written.
func TestRapid_GetAfterPutWithinTTL(t *testing.T) {
	root := t.TempDir()
	var seq atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		bucket := rapid.SampledFrom(bucketNames).Draw(rt, "bucket")
		// BBolt rejects empty keys; generate non-empty strings.
		key := rapid.StringMatching(`[a-zA-Z0-9/._-]{1,32}`).Draw(rt, "key")
		value := rapid.SliceOfN(rapid.Byte(), 0, 1024).Draw(rt, "value")

		path := filepath.Join(root, "cache-"+strconv.FormatInt(seq.Add(1), 10)+".db")
		s, err := Open(path, time.Hour)
		if err != nil {
			rt.Fatalf("open: %v", err)
		}
		defer s.Close()

		if err := s.Put(bucket, key, value); err != nil {
			rt.Fatalf("put: %v", err)
		}
		got, err := s.Get(bucket, key)
		if err != nil {
			rt.Fatalf("get: %v", err)
		}
		if !bytes.Equal(got, value) {
			rt.Fatalf("round-trip mismatch: got=%x want=%x", got, value)
		}
	})
}

// TestRapid_LastPutWinsForSameKey: repeated Put on the same key
// overwrites. Mirrors the TLA+ assumption that each Message is the
// canonical record for its key.
func TestRapid_LastPutWinsForSameKey(t *testing.T) {
	root := t.TempDir()
	var seq atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		path := filepath.Join(root, "cache-"+strconv.FormatInt(seq.Add(1), 10)+".db")
		s, err := Open(path, time.Hour)
		if err != nil {
			rt.Fatal(err)
		}
		defer s.Close()

		bucket := "ohlc"
		key := "k"
		values := rapid.SliceOfN(rapid.SliceOfN(rapid.Byte(), 0, 32), 1, 10).Draw(rt, "values")
		for _, v := range values {
			if err := s.Put(bucket, key, v); err != nil {
				rt.Fatal(err)
			}
		}
		got, err := s.Get(bucket, key)
		if err != nil {
			rt.Fatal(err)
		}
		want := values[len(values)-1]
		if !bytes.Equal(got, want) {
			rt.Fatalf("got=%x want=%x (last of %d puts)", got, want, len(values))
		}
	})
}
