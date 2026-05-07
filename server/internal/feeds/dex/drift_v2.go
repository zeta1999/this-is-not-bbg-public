package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// DriftV2Adapter polls the Drift Protocol v2 REST indexer and emits
// feeds.PerpetualSnapshot per market on `perp.drift.<symbol>`.
//
// Drift on Solana doesn't have a stable public WebSocket we can use
// without running a client SDK; the REST indexer is the cheapest
// path to canonical perp fields. Poll cadence defaults to 10 s,
// which matches Drift's own oracle update frequency.
//
// The indexer response is parsed defensively — Drift has reshuffled
// field names across releases and we don't want a breaking change
// to take the feed offline. Missing fields stay zero and the
// omit-empty JSON rule keeps the bus payload clean.
type DriftV2Adapter struct {
	bus          *bus.Bus
	pollInterval time.Duration
	endpoint     string
	http         *http.Client

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

// NewDriftV2Adapter builds the adapter. Pass an empty `endpoint` for
// the public indexer. PollInterval is clamped to a 1 s floor so a
// misconfiguration can't hammer the indexer.
func NewDriftV2Adapter(b *bus.Bus, pollInterval time.Duration, endpoint string) *DriftV2Adapter {
	if pollInterval < time.Second {
		pollInterval = 10 * time.Second
	}
	if endpoint == "" {
		endpoint = "https://data.api.drift.trade/contracts"
	}
	return &DriftV2Adapter{
		bus:          b,
		pollInterval: pollInterval,
		endpoint:     endpoint,
		http:         &http.Client{Timeout: 10 * time.Second},
		state:        "idle",
	}
}

func (a *DriftV2Adapter) Name() string { return "drift-v2" }

func (a *DriftV2Adapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "drift-v2",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *DriftV2Adapter) Start(ctx context.Context) error {
	t := time.NewTicker(a.pollInterval)
	defer t.Stop()
	// Kick once at start so we don't wait a full interval before the
	// first publish.
	_ = a.poll(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := a.poll(ctx); err != nil {
				slog.Debug("drift-v2 poll", "error", err)
			}
		}
	}
}

func (a *DriftV2Adapter) poll(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		a.mu.Lock()
		a.state = "error"
		a.errorCount++
		a.mu.Unlock()
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.bytesRecv += uint64(len(body))
	a.mu.Unlock()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("drift-v2: status %d", resp.StatusCode)
	}

	contracts := decodeDriftContracts(body)
	for _, c := range contracts {
		a.bus.Publish(bus.Message{
			Topic:   fmt.Sprintf("perp.drift.%s", c.Instrument),
			Payload: c,
		})
	}

	a.mu.Lock()
	a.state = "connected"
	a.lastUpdate = time.Now()
	a.mu.Unlock()
	return nil
}

// decodeDriftContracts parses Drift's REST response into a slice of
// canonical PerpetualSnapshot values. Supports the two shapes the
// indexer has shipped since v2:
//
//   (a) top-level array of contracts
//   (b) {"contracts":[...]} wrapper
//
// Fields are matched by common names (case-sensitive). Missing
// fields stay zero; the omit-empty rule on PerpetualSnapshot keeps
// the wire clean.
func decodeDriftContracts(body []byte) []feeds.PerpetualSnapshot {
	// Shape (a).
	var direct []driftContract
	if err := json.Unmarshal(body, &direct); err == nil && len(direct) > 0 {
		return mapDriftContracts(direct)
	}
	// Shape (b).
	var wrapped struct {
		Contracts []driftContract `json:"contracts"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		return mapDriftContracts(wrapped.Contracts)
	}
	return nil
}

type driftContract struct {
	BaseSymbol       string `json:"baseCurrency,omitempty"`
	BaseAlt          string `json:"baseSymbol,omitempty"`
	MarketIndex      int    `json:"marketIndex,omitempty"`
	OraclePrice      string `json:"oraclePrice,omitempty"`
	IndexPrice       string `json:"indexPrice,omitempty"`
	LastPrice        string `json:"lastPrice,omitempty"`
	Last24hAvgFunding string `json:"last24hAvgFunding,omitempty"`
	NextFunding      string `json:"nextFundingRate,omitempty"`
	OpenInterest     string `json:"openInterest,omitempty"`
	OpenInterestUSD  string `json:"openInterestUsd,omitempty"`
}

func mapDriftContracts(in []driftContract) []feeds.PerpetualSnapshot {
	out := make([]feeds.PerpetualSnapshot, 0, len(in))
	now := time.Now()
	parse := func(s string) float64 {
		if s == "" {
			return 0
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	for _, c := range in {
		sym := c.BaseSymbol
		if sym == "" {
			sym = c.BaseAlt
		}
		if sym == "" {
			continue
		}
		instrument := sym + "-PERP"
		mark := parse(c.LastPrice)
		oracle := parse(c.OraclePrice)
		if oracle == 0 {
			oracle = parse(c.IndexPrice)
		}
		snap := feeds.PerpetualSnapshot{
			Instrument:        instrument,
			Exchange:          "drift",
			Timestamp:         now,
			MarkPrice:         mark,
			IndexPrice:        oracle,
			FundingRate:       parse(c.Last24hAvgFunding),
			NextFundingRate:   parse(c.NextFunding),
			OpenInterestBase:  parse(c.OpenInterest),
			OpenInterestQuote: parse(c.OpenInterestUSD),
		}
		if snap.MarkPrice == 0 && snap.IndexPrice == 0 &&
			snap.OpenInterestBase == 0 && snap.OpenInterestQuote == 0 {
			continue
		}
		out = append(out, snap)
	}
	return out
}
