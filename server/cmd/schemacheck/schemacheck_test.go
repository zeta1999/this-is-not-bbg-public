package main

import (
	"fmt"
	"strings"
	"testing"

	pb "github.com/notbbg/notbbg/server/pkg/protocol/notbbg/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestScan_HappyPath(t *testing.T) {
	ohlcJSON, err := protojson.Marshal(&pb.OHLC{
		Instrument: "BTCUSDT", Exchange: "binance", Timeframe: "1m",
		Timestamp: timestamppb.Now(),
		Open:      1, High: 2, Low: 0.5, Close: 1.5, Volume: 10,
	})
	if err != nil {
		t.Fatalf("marshal ohlc: %v", err)
	}
	tradeJSON, err := protojson.Marshal(&pb.Trade{
		Instrument: "BTCUSDT", Exchange: "binance",
		Timestamp: timestamppb.Now(),
		Price:     1, Quantity: 0.1, Side: pb.Side_SIDE_BUY, TradeId: "t1",
	})
	if err != nil {
		t.Fatalf("marshal trade: %v", err)
	}

	input := strings.Join([]string{
		fmt.Sprintf(`{"_topic":"ohlc.binance.BTCUSDT","_timestamp":"t","payload":%s}`, ohlcJSON),
		fmt.Sprintf(`{"_topic":"trade.binance.BTCUSDT","_timestamp":"t","payload":%s}`, tradeJSON),
	}, "\n")

	rep := NewReport()
	if err := Scan(rep, strings.NewReader(input), "test", 0); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rep.Records != 2 {
		t.Errorf("records: got %d want 2", rep.Records)
	}
	if b := rep.Buckets["ohlc"]; b == nil || b.OK != 1 || b.Mismatch != 0 {
		t.Errorf("ohlc bucket: %+v", b)
	}
	if b := rep.Buckets["trade"]; b == nil || b.OK != 1 || b.Mismatch != 0 {
		t.Errorf("trade bucket: %+v", b)
	}
	if rep.AnyMismatch() {
		t.Errorf("unexpected mismatch: %+v", rep.Buckets)
	}
}

func TestScan_BadEnvelope(t *testing.T) {
	input := "this is not json\n"
	rep := NewReport()
	if err := Scan(rep, strings.NewReader(input), "test", 0); err != nil {
		t.Fatalf("scan: %v", err)
	}
	b := rep.Buckets["__envelope__"]
	if b == nil || b.Mismatch != 1 {
		t.Errorf("expected 1 envelope mismatch, got %+v", b)
	}
	if !rep.AnyMismatch() {
		t.Errorf("AnyMismatch should be true")
	}
}

func TestScan_SchemaDrift(t *testing.T) {
	// Valid JSON, valid envelope, but payload is not a valid OHLC.
	// protojson rejects unknown fields by default.
	input := `{"_topic":"ohlc.binance.BTCUSDT","_timestamp":"t","payload":{"totally_unknown_field":42}}`
	rep := NewReport()
	if err := Scan(rep, strings.NewReader(input), "test", 0); err != nil {
		t.Fatalf("scan: %v", err)
	}
	b := rep.Buckets["ohlc"]
	if b == nil || b.Mismatch != 1 || b.OK != 0 {
		t.Errorf("expected ohlc bucket with 1 mismatch, got %+v", b)
	}
	if len(b.Examples) != 1 {
		t.Errorf("expected 1 example, got %d", len(b.Examples))
	}
}

func TestScan_UnknownTopic(t *testing.T) {
	// Topic has no mapped proto — counted as unknown, not mismatch.
	input := `{"_topic":"custom.thing","_timestamp":"t","payload":{}}`
	rep := NewReport()
	if err := Scan(rep, strings.NewReader(input), "test", 0); err != nil {
		t.Fatalf("scan: %v", err)
	}
	b := rep.Buckets["custom"]
	if b == nil || b.Unknown != 1 {
		t.Errorf("expected custom bucket with 1 unknown, got %+v", b)
	}
	if rep.AnyMismatch() {
		t.Errorf("unknown should not count as mismatch")
	}
}

func TestScan_Sample(t *testing.T) {
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, `{"_topic":"custom","_timestamp":"t","payload":{}}`)
	}
	rep := NewReport()
	if err := Scan(rep, strings.NewReader(strings.Join(lines, "\n")), "test", 2); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rep.Records != 2 {
		t.Errorf("sample=2: got %d records", rep.Records)
	}
}

func TestTopicPrefix(t *testing.T) {
	cases := map[string]string{
		"ohlc.binance.BTCUSDT": "ohlc",
		"news":                 "news",
		"trade.kraken.ETHUSD":  "trade",
		"":                     "",
	}
	for in, want := range cases {
		if got := topicPrefix(in); got != want {
			t.Errorf("topicPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
