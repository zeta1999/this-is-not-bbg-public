package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/auth"
	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/datalake"
	"github.com/notbbg/notbbg/server/internal/daterange"
)

func TestHandleDataRange_StreamsNDJSON(t *testing.T) {
	root := t.TempDir()

	// Seed a tiny datalake partition with a single record.
	day := time.Now().UTC().Format("2006-01-02")
	dir := filepath.Join(root, "ohlc", "binance", "BTCUSDT", day)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rec := map[string]any{
		"_topic":     "ohlc.binance.BTCUSDT",
		"_timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"payload":    map[string]any{"Close": 64500.0},
	}
	b, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(dir, "data.jsonl"), append(b, '\n'), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Minimal gateway — auth + datarange wired, nothing else.
	authMgr := auth.NewManager()
	sessionID, _ := auth.GenerateTokenID()
	authMgr.SeedSession(sessionID, "test", auth.RightRead|auth.RightSubscribe)

	gw := NewHTTPGateway(bus.New(16), "127.0.0.1:0", authMgr)
	gw.SetDatalakeReader(datalake.NewReader(root))
	gw.SetDateRangeHandler(daterange.New(root))

	srv := httptest.NewServer(http.HandlerFunc(gw.requireAuth(gw.handleDataRange)))
	defer srv.Close()

	q := "?token=" + sessionID +
		"&topic=ohlc.binance.BTCUSDT" +
		"&from=" + time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339) +
		"&to=" + time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+q, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(body))
	}

	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) == 0 {
		t.Fatalf("expected at least one NDJSON line")
	}
	// The last line must be the EOF chunk.
	last := lines[len(lines)-1]
	if !strings.Contains(last, `"EOF":true`) {
		t.Fatalf("last line missing EOF marker: %q", last)
	}
}
