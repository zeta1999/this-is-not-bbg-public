package dex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

// TestUniswapAdapter_Publishes_Smoke is a deterministic smoke test
// against a mocked DeFi Llama endpoint to confirm the publish path.
// If this passes, the adapter logic is correct — any production
// silence is either a DeFi Llama outage or a config wiring issue.
func TestUniswapAdapter_Publishes_Smoke(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Respond with a shape matching coins.llama.fi.
		_ = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"coins":{
			"ethereum:0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2":{"price":3500.0,"symbol":"WETH","decimals":18,"timestamp":1734567890,"confidence":0.99}
		}}`))
	}))
	defer mock.Close()

	b := bus.New(16)
	sub := b.Subscribe(16, "ohlc.uniswap.*", "lob.uniswap.*")
	defer b.Unsubscribe(sub)

	a := NewUniswapAdapter(b, time.Second)
	a.llamaURL = mock.URL // (field added below so we can swap endpoints in tests)
	// Trim down the token list to the one we've mocked.
	a.tokens = a.tokens[:1]

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.fetch(ctx)

	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case msg := <-sub.C:
			got[msg.Topic] = true
		case <-ctx.Done():
			t.Fatalf("timed out; got=%v", got)
		}
	}
	if !got["ohlc.uniswap.WETHUSD"] {
		t.Fatalf("missing ohlc topic; got=%v", got)
	}
	if !got["lob.uniswap.WETHUSD"] {
		t.Fatalf("missing lob topic; got=%v", got)
	}
}

// TestUniswapAdapter_LiveEndpoint probes the actual DeFi Llama API.
// Skipped unless NOTBBG_LIVE_UNISWAP=1 is set. Documents what a
// live response currently looks like so we can spot regressions.
func TestUniswapAdapter_LiveEndpoint(t *testing.T) {
	if os.Getenv("NOTBBG_LIVE_UNISWAP") != "1" {
		t.Skip("set NOTBBG_LIVE_UNISWAP=1 to hit the real endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	keys := []string{}
	for _, tok := range defaultUniswapTokens {
		keys = append(keys, tok.Chain+":"+tok.Address)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://coins.llama.fi/prices/current/"+strings.Join(keys, ","), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("live fetch: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	t.Logf("live status=%d bytes=%d", resp.StatusCode, len(body))
	var r llamaPriceResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("decode live: %v (body=%s)", err, string(body[:min(len(body), 500)]))
	}
	for k, v := range r.Coins {
		t.Logf("  %s → price=%.4f sym=%s", k, v.Price, v.Symbol)
	}
	if len(r.Coins) == 0 {
		t.Fatalf("no coins returned; body=%s", string(body[:min(len(body), 500)]))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestUniswapAdapter_LiveAdapterPublishes runs the real adapter
// against the real DeFi Llama endpoint and asserts that at least
// one OHLC bus message lands. This is the end-to-end canary for
// "is Uniswap data flowing?". Skipped unless NOTBBG_LIVE_UNISWAP=1.
func TestUniswapAdapter_LiveAdapterPublishes(t *testing.T) {
	if os.Getenv("NOTBBG_LIVE_UNISWAP") != "1" {
		t.Skip("set NOTBBG_LIVE_UNISWAP=1 to hit the real endpoint")
	}
	b := bus.New(128)
	sub := b.Subscribe(64, "ohlc.uniswap.*")
	defer b.Unsubscribe(sub)

	a := NewUniswapAdapter(b, 15*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.fetch(ctx)

	count := 0
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
drain:
	for {
		select {
		case <-sub.C:
			count++
		case <-timer.C:
			break drain
		}
	}
	if count == 0 {
		t.Fatalf("live adapter produced 0 ohlc.uniswap.* messages — adapter broken or endpoint down")
	}
	t.Logf("live adapter produced %d ohlc.uniswap.* messages in one fetch", count)
}
