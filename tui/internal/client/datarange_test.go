package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDataRangeClient_StreamsChunks(t *testing.T) {
	// Fake gateway that emits two chunks + an EOF line. Topic/token
	// are ignored here — the client should just parse NDJSON.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		enc := json.NewEncoder(w)
		_ = enc.Encode(DataRangeChunk{CorrelationID: "x", Seq: 0, Records: []DataRangeRecord{
			{Topic: "ohlc.binance.BTCUSDT", Timestamp: "2026-04-23T00:00:00Z"},
		}})
		_ = enc.Encode(DataRangeChunk{CorrelationID: "x", Seq: 1, Records: []DataRangeRecord{
			{Topic: "ohlc.binance.BTCUSDT", Timestamp: "2026-04-23T00:01:00Z"},
		}})
		_ = enc.Encode(DataRangeChunk{CorrelationID: "x", Seq: 2, EOF: true})
	}))
	defer srv.Close()

	c := &DataRangeClient{baseURL: srv.URL, http: srv.Client()}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var got []DataRangeChunk
	err := c.Fetch(ctx, "ohlc.binance.BTCUSDT",
		time.Now().Add(-time.Hour), time.Now(), "x", 0,
		func(ch DataRangeChunk) error {
			got = append(got, ch)
			return nil
		})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(got))
	}
	if !got[2].EOF {
		t.Fatalf("last chunk not EOF: %+v", got[2])
	}
	if got[0].Records[0].Topic != "ohlc.binance.BTCUSDT" {
		t.Fatalf("wrong topic: %s", got[0].Records[0].Topic)
	}
}

func TestDataRangeClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad from"}`, 400)
	}))
	defer srv.Close()

	c := &DataRangeClient{baseURL: srv.URL, http: srv.Client()}
	err := c.Fetch(context.Background(), "x",
		time.Now(), time.Now().Add(time.Hour), "", 0,
		func(DataRangeChunk) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected 400, got %v", err)
	}
}
