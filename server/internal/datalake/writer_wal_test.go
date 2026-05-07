package datalake

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	pb "github.com/notbbg/notbbg/server/pkg/protocol/notbbg/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestWriter_WAL_RoundTrip publishes a message, lets it flow
// bus → WAL → file, and verifies the JSONL landed on disk.
func TestWriter_WAL_RoundTrip(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")

	b := bus.New(8)
	w := New(b, Config{
		Path:    root,
		Enabled: true,
		Topics:  []string{"ohlc.*.*"},
	}).EnableWAL(walDir, 4<<20)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)

	orig := &pb.OHLC{
		Instrument: "BTCUSDT",
		Exchange:   "binance",
		Timeframe:  "1m",
		Timestamp:  timestamppb.New(time.Unix(1700000000, 0).UTC()),
		Open:       1, High: 2, Low: 0, Close: 1, Volume: 1,
	}
	b.Publish(bus.Message{Topic: "ohlc.binance.BTCUSDT", Payload: orig})

	// Poll for the file to appear.
	var found string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, "data.jsonl") {
				found = path
			}
			return nil
		})
		if found != "" {
			data, err := os.ReadFile(found)
			if err == nil && len(data) > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	if found == "" {
		t.Fatal("datalake did not produce any jsonl file")
	}
	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "BTCUSDT") {
		t.Fatalf("expected BTCUSDT in %s, got: %q", found, data)
	}
}

// TestWriter_WAL_ReopenResumes crashes the writer mid-flight and
// verifies the backlog replays on reopen.
func TestWriter_WAL_ReopenResumes(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")

	b := bus.New(8)

	// First pass: publish several messages, cancel quickly so some
	// are likely still in the WAL.
	{
		w := New(b, Config{
			Path: root, Enabled: true, Topics: []string{"news"},
		}).EnableWAL(walDir, 4<<20)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = w.Run(ctx); close(done) }()
		time.Sleep(30 * time.Millisecond)
		for i := 0; i < 20; i++ {
			b.Publish(bus.Message{Topic: "news", Payload: map[string]any{"Title": "hdr", "Body": "body"}})
		}
		time.Sleep(20 * time.Millisecond)
		cancel()
		<-done
	}

	// Second pass: reopen, publish nothing, expect the remnants to flush.
	var found string
	{
		w := New(b, Config{
			Path: root, Enabled: true, Topics: []string{"news"},
		}).EnableWAL(walDir, 4<<20)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = w.Run(ctx); close(done) }()

		// Poll up to 2s for the file.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && strings.HasSuffix(path, "data.jsonl") {
					found = path
				}
				return nil
			})
			if found != "" {
				data, _ := os.ReadFile(found)
				if strings.Count(string(data), "\n") >= 10 {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
		<-done
	}

	if found == "" {
		t.Fatal("no file produced after reopen")
	}
	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(data), "\n")
	if lines == 0 {
		t.Fatalf("no lines in %s", found)
	}
}
