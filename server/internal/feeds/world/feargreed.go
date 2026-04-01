package world

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// FearGreedAdapter fetches the Crypto Fear & Greed Index from alternative.me.
type FearGreedAdapter struct {
	bus          *bus.Bus
	pollInterval time.Duration
	mu           sync.RWMutex
	state        string
	lastUpdate   time.Time
	errorCount   uint64
	bytesRecv    uint64
}

func NewFearGreedAdapter(b *bus.Bus, pollInterval time.Duration) *FearGreedAdapter {
	if pollInterval == 0 {
		pollInterval = 5 * time.Minute
	}
	return &FearGreedAdapter{
		bus:          b,
		pollInterval: pollInterval,
		state:        "disconnected",
	}
}

func (a *FearGreedAdapter) Name() string { return "fear_greed" }

func (a *FearGreedAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "fear_greed",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *FearGreedAdapter) Start(ctx context.Context) error {
	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	a.fetch(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.fetch(ctx)
		}
	}
}

type fearGreedResponse struct {
	Data []struct {
		Value     string `json:"value"`
		Class     string `json:"value_classification"`
		Timestamp string `json:"timestamp"`
	} `json:"data"`
}

func (a *FearGreedAdapter) fetch(ctx context.Context) {
	url := "https://api.alternative.me/fng/?limit=1"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.mu.Lock()
		a.errorCount++
		a.state = "error"
		a.mu.Unlock()
		slog.Debug("fear_greed fetch error", "error", err)
		return
	}
	defer resp.Body.Close()

	var result fearGreedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		return
	}

	if len(result.Data) > 0 {
		d := result.Data[0]
		value, _ := strconv.ParseFloat(d.Value, 64)
		ts, _ := strconv.ParseInt(d.Timestamp, 10, 64)

		// Publish as an OHLC-like message on a special topic.
		a.bus.Publish(bus.Message{
			Topic: "indicator.fear_greed",
			Payload: feeds.OHLC{
				Instrument: "FEAR_GREED_INDEX",
				Exchange:   "alternative.me",
				Timeframe:  "1d",
				Timestamp:  time.Unix(ts, 0),
				Close:      value,
			},
		})
	}

	a.mu.Lock()
	a.lastUpdate = time.Now()
	a.state = "connected"
	a.bytesRecv += uint64(resp.ContentLength)
	a.mu.Unlock()

	slog.Debug("fear_greed fetched")
}
