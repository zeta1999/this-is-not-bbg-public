package datalake

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	pb "github.com/notbbg/notbbg/server/pkg/protocol/notbbg/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestWriter_ProtoPayloadRoundTrips asserts that an OHLC proto message
// published on the bus lands in the datalake as canonical protojson
// and unmarshals cleanly into the same proto type.
func TestWriter_ProtoPayloadRoundTrips(t *testing.T) {
	root := t.TempDir()
	b := bus.New(8)
	w := New(b, Config{
		Path:    root,
		Enabled: true,
		Topics:  []string{"ohlc.*.*"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(done)
	}()

	// Let Run's subscribe complete before publishing.
	time.Sleep(50 * time.Millisecond)

	orig := &pb.OHLC{
		Instrument: "BTCUSDT",
		Exchange:   "binance",
		Timeframe:  "1m",
		Timestamp:  timestamppb.New(time.Unix(1700000000, 0).UTC()),
		Open:       1.1, High: 2.2, Low: 0.5, Close: 1.7, Volume: 42.5,
	}
	b.Publish(bus.Message{Topic: "ohlc.binance.BTCUSDT", Payload: orig})

	// Give the writer a moment to flush.
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	// Find the written file.
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, "data.jsonl") {
			found = path
		}
		return nil
	})
	if err != nil || found == "" {
		t.Fatalf("did not find data.jsonl under %s (err=%v)", root, err)
	}

	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 record, got %d: %q", len(lines), string(data))
	}

	// Parse envelope.
	var env struct {
		Topic   string          `json:"_topic"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env.Topic != "ohlc.binance.BTCUSDT" {
		t.Errorf("topic: got %q", env.Topic)
	}

	// The payload must round-trip through protojson.
	got := &pb.OHLC{}
	if err := protojson.Unmarshal(env.Payload, got); err != nil {
		t.Fatalf("protojson.Unmarshal: %v (payload=%s)", err, env.Payload)
	}
	if got.Instrument != orig.Instrument || got.Close != orig.Close {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", got, orig)
	}
}

// TestWriter_NonProtoPayloadStillWrites asserts that non-proto
// payloads (plain maps / strings) don't break the writer.
func TestWriter_NonProtoPayloadStillWrites(t *testing.T) {
	root := t.TempDir()
	b := bus.New(8)
	w := New(b, Config{Path: root, Topics: []string{"news"}})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)

	b.Publish(bus.Message{Topic: "news", Payload: map[string]string{"title": "hi"}})
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	var found string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, "data.jsonl") {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Fatal("no data.jsonl written")
	}
	data, _ := os.ReadFile(found)
	if !strings.Contains(string(data), `"title":"hi"`) {
		t.Errorf("payload missing expected content: %s", string(data))
	}
}
