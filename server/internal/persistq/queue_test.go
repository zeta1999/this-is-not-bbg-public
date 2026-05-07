package persistq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func newQ(t *testing.T, cap, seg int64) *Queue {
	t.Helper()
	dir, err := os.MkdirTemp("", "persistq-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	q, err := Open(Options{Dir: dir, CapBytes: cap, SegmentBytes: seg})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

func TestEnqueueDequeue_RoundTrip(t *testing.T) {
	q := newQ(t, 1<<20, 64*1024)
	for i := 0; i < 50; i++ {
		if err := q.Enqueue([]byte(fmt.Sprintf("payload-%d", i))); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i := 0; i < 50; i++ {
		got, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("dequeue %d: %v", i, err)
		}
		want := []byte(fmt.Sprintf("payload-%d", i))
		if !bytes.Equal(got, want) {
			t.Fatalf("msg %d = %q, want %q", i, got, want)
		}
	}
}

func TestRotation_CrossesSegment(t *testing.T) {
	q := newQ(t, 1<<20, 1024) // tiny 1 KiB segments
	// Each record ~70 bytes with header+payload, so ~14 per segment.
	// Write 100 to guarantee multiple rotations.
	const n = 100
	for i := 0; i < n; i++ {
		pl := []byte(fmt.Sprintf("line-%060d", i))
		if err := q.Enqueue(pl); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if st := q.Stats(); st.Segments < 2 {
		t.Fatalf("expected multiple segments, got %d", st.Segments)
	}
	ctx := context.Background()
	for i := 0; i < n; i++ {
		got, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("dequeue %d: %v", i, err)
		}
		if want := []byte(fmt.Sprintf("line-%060d", i)); !bytes.Equal(got, want) {
			t.Fatalf("msg %d mismatch", i)
		}
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir, err := os.MkdirTemp("", "persistq-reopen-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	q, err := Open(Options{Dir: dir, CapBytes: 1 << 20, SegmentBytes: 4 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := q.Enqueue([]byte(fmt.Sprintf("msg-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	// Drain the first 5 — the cursor should persist.
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		got, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if want := []byte(fmt.Sprintf("msg-%d", i)); !bytes.Equal(got, want) {
			t.Fatalf("first-pass msg %d = %q, want %q", i, got, want)
		}
	}
	_ = q.Close()

	// Reopen and read the remaining 15.
	q2, err := Open(Options{Dir: dir, CapBytes: 1 << 20, SegmentBytes: 4 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer q2.Close()
	for i := 5; i < 20; i++ {
		got, err := q2.Dequeue(ctx)
		if err != nil {
			t.Fatalf("post-reopen dequeue %d: %v", i, err)
		}
		if want := []byte(fmt.Sprintf("msg-%d", i)); !bytes.Equal(got, want) {
			t.Fatalf("post-reopen msg %d = %q, want %q", i, got, want)
		}
	}
}

func TestFull_DropWhenNothingDrained(t *testing.T) {
	// Cap is 1 segment's worth; with nothing drained, a second
	// segment's worth triggers ErrFull.
	q := newQ(t, 2048, 512)
	var full bool
	for i := 0; i < 2000; i++ {
		err := q.Enqueue([]byte("xxxxxxxxxxxxxxxxxxxxxxxx"))
		if err == nil {
			continue
		}
		if errors.Is(err, ErrFull) {
			full = true
			break
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if !full {
		t.Fatal("never hit ErrFull")
	}
	if d := q.Stats().Dropped; d == 0 {
		t.Fatal("expected dropped > 0")
	}
}

func TestDropOldestDrainedSegment(t *testing.T) {
	// Small queue: 2 segments total capacity.
	q := newQ(t, 2*256, 256)

	// Fill segment 0 + start of segment 1.
	for i := 0; i < 20; i++ {
		_ = q.Enqueue([]byte("aaaaaaaaaaaa")) // 16-byte records
	}
	// Drain half — reader should move past segment 0.
	ctx := context.Background()
	for i := 0; i < 16; i++ {
		if _, err := q.Dequeue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Now writing more should force drop of segment 0 (drained).
	for i := 0; i < 10; i++ {
		if err := q.Enqueue([]byte("bbbbbbbbbbbb")); err != nil {
			t.Fatalf("expected enqueue to succeed (drained seg reclaimed), got %v", err)
		}
	}
	// Read the remaining should still yield bs after the as.
	seenA, seenB := 0, 0
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		p, err := q.Dequeue(ctx2)
		cancel()
		if err != nil {
			break
		}
		switch string(p) {
		case "aaaaaaaaaaaa":
			seenA++
		case "bbbbbbbbbbbb":
			seenB++
		default:
			t.Fatalf("unexpected payload %q", p)
		}
	}
	if seenB == 0 {
		t.Fatalf("no 'b' records delivered (a=%d, b=%d)", seenA, seenB)
	}
}

func TestCtxCancel_Dequeue(t *testing.T) {
	q := newQ(t, 1<<20, 1024)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := q.Dequeue(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want DeadlineExceeded", err)
	}
}

func TestConcurrentProducerConsumer(t *testing.T) {
	q := newQ(t, 4<<20, 16*1024)
	const n = 500
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if err := q.Enqueue([]byte(fmt.Sprintf("%d", i))); err != nil {
				t.Errorf("enqueue %d: %v", i, err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for i := 0; i < n; i++ {
			got, err := q.Dequeue(ctx)
			if err != nil {
				t.Errorf("dequeue %d: %v", i, err)
				return
			}
			if want := []byte(fmt.Sprintf("%d", i)); !bytes.Equal(got, want) {
				t.Errorf("msg %d = %q, want %q", i, got, want)
				return
			}
		}
	}()

	wg.Wait()
}

func TestClose_UnblocksDequeue(t *testing.T) {
	q := newQ(t, 1<<20, 1024)
	done := make(chan struct{})
	var gotErr error
	go func() {
		_, gotErr = q.Dequeue(context.Background())
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	_ = q.Close()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Dequeue did not return after Close")
	}
	if !errors.Is(gotErr, ErrClosed) {
		t.Fatalf("err=%v, want ErrClosed", gotErr)
	}
}
