package world

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// MempoolAdapter fetches Bitcoin network data from mempool.space.
type MempoolAdapter struct {
	bus          *bus.Bus
	pollInterval time.Duration
	mu           sync.RWMutex
	state        string
	lastUpdate   time.Time
	errorCount   uint64
	bytesRecv    uint64
}

func NewMempoolAdapter(b *bus.Bus, pollInterval time.Duration) *MempoolAdapter {
	if pollInterval == 0 {
		pollInterval = 60 * time.Second
	}
	return &MempoolAdapter{
		bus:          b,
		pollInterval: pollInterval,
		state:        "disconnected",
	}
}

func (a *MempoolAdapter) Name() string { return "mempool_space" }

func (a *MempoolAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "mempool_space",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *MempoolAdapter) Start(ctx context.Context) error {
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

type mempoolHashrate struct {
	CurrentHashrate float64 `json:"currentHashrate"`
	CurrentDifficulty float64 `json:"currentDifficulty"`
}

type mempoolFees struct {
	FastestFee  int `json:"fastestFee"`
	HalfHourFee int `json:"halfHourFee"`
	HourFee     int `json:"hourFee"`
	EconomyFee  int `json:"economyFee"`
	MinimumFee  int `json:"minimumFee"`
}

func (a *MempoolAdapter) fetch(ctx context.Context) {
	now := time.Now()

	// Fetch recommended fees.
	fees, err := fetchJSON[mempoolFees](ctx, "https://mempool.space/api/v1/fees/recommended")
	if err != nil {
		a.mu.Lock()
		a.errorCount++
		a.state = "error"
		a.mu.Unlock()
		slog.Debug("mempool fees error", "error", err)
		return
	}

	a.bus.Publish(bus.Message{
		Topic: "indicator.btc_fees",
		Payload: feeds.OHLC{
			Instrument: "BTC_FEE_SAT_VB",
			Exchange:   "mempool.space",
			Timeframe:  "spot",
			Timestamp:  now,
			Open:       float64(fees.EconomyFee),
			High:       float64(fees.FastestFee),
			Low:        float64(fees.MinimumFee),
			Close:      float64(fees.HalfHourFee),
		},
	})

	// Fetch hashrate.
	hr, err := fetchJSON[mempoolHashrate](ctx, "https://mempool.space/api/v1/mining/hashrate/1d")
	if err != nil {
		slog.Debug("mempool hashrate error", "error", err)
	} else {
		a.bus.Publish(bus.Message{
			Topic: "indicator.btc_hashrate",
			Payload: feeds.OHLC{
				Instrument: "BTC_HASHRATE",
				Exchange:   "mempool.space",
				Timeframe:  "1d",
				Timestamp:  now,
				Close:      hr.CurrentHashrate,
			},
		})
	}

	a.mu.Lock()
	a.lastUpdate = now
	a.state = "connected"
	a.mu.Unlock()
}

func fetchJSON[T any](ctx context.Context, url string) (T, error) {
	var result T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return result, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}
