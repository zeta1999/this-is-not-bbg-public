package world

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// FREDAdapter fetches economic data from the Federal Reserve Economic Data API.
type FREDAdapter struct {
	bus          *bus.Bus
	apiKey       string
	pollInterval time.Duration
	series       []string // FRED series IDs to track

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

func NewFREDAdapter(b *bus.Bus, apiKey string, pollInterval time.Duration) *FREDAdapter {
	if pollInterval == 0 {
		pollInterval = time.Hour
	}
	return &FREDAdapter{
		bus:          b,
		apiKey:       apiKey,
		pollInterval: pollInterval,
		series: []string{
			"DFF",       // Federal Funds Rate
			"T10YIE",    // 10-Year Breakeven Inflation
			"BAMLH0A0HYM2", // High Yield Bond Spread
			"DTWEXBGS",  // Trade Weighted US Dollar Index
			"VIXCLS",    // VIX
			"SP500",     // S&P 500
			"UNRATE",    // Unemployment Rate
			"CPIAUCSL",  // CPI
			"M2SL",      // M2 Money Supply
		},
		state: "disconnected",
	}
}

func (a *FREDAdapter) Name() string { return "fred" }

func (a *FREDAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "fred",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *FREDAdapter) Start(ctx context.Context) error {
	if a.apiKey == "" {
		slog.Warn("FRED adapter disabled: no API key configured")
		a.mu.Lock()
		a.state = "stale"
		a.mu.Unlock()
		<-ctx.Done()
		return nil
	}

	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	a.fetchAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.fetchAll(ctx)
		}
	}
}

type fredObservations struct {
	Observations []struct {
		Date  string `json:"date"`
		Value string `json:"value"`
	} `json:"observations"`
}

func (a *FREDAdapter) fetchAll(ctx context.Context) {
	for _, series := range a.series {
		select {
		case <-ctx.Done():
			return
		default:
		}

		a.fetchSeries(ctx, series)
		time.Sleep(500 * time.Millisecond) // Rate limit
	}
}

func (a *FREDAdapter) fetchSeries(ctx context.Context, seriesID string) {
	url := fmt.Sprintf(
		"https://api.stlouisfed.org/fred/series/observations?series_id=%s&api_key=%s&file_type=json&sort_order=desc&limit=1",
		seriesID, a.apiKey,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		slog.Debug("fred fetch error", "series", seriesID, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		return
	}

	var data fredObservations
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		return
	}

	if len(data.Observations) > 0 {
		obs := data.Observations[0]
		value, err := strconv.ParseFloat(obs.Value, 64)
		if err != nil {
			return // "." means no data
		}

		date, _ := time.Parse("2006-01-02", obs.Date)

		a.bus.Publish(bus.Message{
			Topic: fmt.Sprintf("indicator.fred.%s", seriesID),
			Payload: feeds.OHLC{
				Instrument: seriesID,
				Exchange:   "fred",
				Timeframe:  "1d",
				Timestamp:  date,
				Close:      value,
			},
		})
	}

	a.mu.Lock()
	a.lastUpdate = time.Now()
	a.state = "connected"
	a.mu.Unlock()
}
