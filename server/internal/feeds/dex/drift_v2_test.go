package dex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

func TestDriftV2_DecodeArrayShape(t *testing.T) {
	body := []byte(`[
		{"baseSymbol":"SOL","oraclePrice":"150.5","lastPrice":"150.6","openInterest":"123","last24hAvgFunding":"0.0001"},
		{"baseCurrency":"BTC","oraclePrice":"65000","lastPrice":"65050","openInterest":"10","openInterestUsd":"650000"}
	]`)
	out := decodeDriftContracts(body)
	if len(out) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(out), out)
	}
	if out[0].Instrument != "SOL-PERP" || out[0].MarkPrice != 150.6 || out[0].IndexPrice != 150.5 {
		t.Errorf("SOL: %+v", out[0])
	}
	if out[1].Instrument != "BTC-PERP" || out[1].OpenInterestQuote != 650000 {
		t.Errorf("BTC: %+v", out[1])
	}
}

func TestDriftV2_DecodeWrappedShape(t *testing.T) {
	body := []byte(`{"contracts":[{"baseSymbol":"ETH","oraclePrice":"3300","openInterest":"50"}]}`)
	out := decodeDriftContracts(body)
	if len(out) != 1 || out[0].Instrument != "ETH-PERP" {
		t.Fatalf("got %+v", out)
	}
}

func TestDriftV2_DropsAllZero(t *testing.T) {
	body := []byte(`[{"baseSymbol":"X","oraclePrice":"","lastPrice":"","openInterest":""}]`)
	if out := decodeDriftContracts(body); len(out) != 0 {
		t.Fatalf("want empty, got %+v", out)
	}
}

func TestDriftV2_PollPublishes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"baseSymbol":"BTC","oraclePrice":"65000","lastPrice":"65050","openInterest":"10"}]`))
	}))
	defer srv.Close()

	b := bus.New(4)
	sub := b.Subscribe(4, "perp.drift.*")
	defer b.Unsubscribe(sub)

	a := NewDriftV2Adapter(b, time.Second, srv.URL)
	if err := a.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-sub.C:
		if m.Topic != "perp.drift.BTC-PERP" {
			t.Errorf("topic=%q", m.Topic)
		}
		p := m.Payload.(feeds.PerpetualSnapshot)
		if p.MarkPrice != 65050 || p.IndexPrice != 65000 {
			t.Errorf("values: %+v", p)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no publish")
	}
}
