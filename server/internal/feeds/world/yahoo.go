package world

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// YahooFinanceAdapter fetches market data via Yahoo Finance v8 chart API.
type YahooFinanceAdapter struct {
	bus          *bus.Bus
	pollInterval time.Duration
	symbols      []string

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	bytesRecv  uint64
}

var defaultYahooSymbols = []string{
	"^GSPC", "^DJI", "^IXIC", "^RUT", "^VIX",
	"^STOXX50E", "^GDAXI", "^FTSE", "^FCHI",
	"^N225", "^TOPX", "^KS11",
	"GC=F", "SI=F", "CL=F", "NG=F",
	"DX-Y.NYB", "^TNX",
	"EURUSD=X", "GBPUSD=X", "USDJPY=X", "USDKRW=X",
}

func NewYahooFinanceAdapter(b *bus.Bus, pollInterval time.Duration, symbols []string) *YahooFinanceAdapter {
	if pollInterval == 0 {
		pollInterval = 60 * time.Second
	}
	if len(symbols) == 0 {
		symbols = defaultYahooSymbols
	}
	return &YahooFinanceAdapter{
		bus:          b,
		pollInterval: pollInterval,
		symbols:      symbols,
		state:        "disconnected",
	}
}

func (a *YahooFinanceAdapter) Name() string { return "yahoo_finance" }

func (a *YahooFinanceAdapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "yahoo_finance",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		LatencyMs:     0,
		ErrorCount:    a.errorCount,
		BytesReceived: a.bytesRecv,
	}
}

func (a *YahooFinanceAdapter) Start(ctx context.Context) error {
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

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Timestamps []int64 `json:"timestamp"`
			Meta       struct {
				Symbol             string  `json:"symbol"`
				RegularMarketPrice float64 `json:"regularMarketPrice"`
				ChartPreviousClose float64 `json:"chartPreviousClose"`
				RegularMarketVolume int64   `json:"regularMarketVolume"`
			} `json:"meta"`
			Indicators struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
					Volume []int64   `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// Yahoo timeframes to fetch: range → interval mapping.
var yahooTimeframes = []struct {
	rng      string // API range parameter
	interval string // API interval parameter
	tf       string // our timeframe label
}{
	{"5d", "15m", "15m"},
	{"5d", "1h", "1h"},
	{"1mo", "1d", "1d"},
}

func (a *YahooFinanceAdapter) fetch(ctx context.Context) {
	for i, sym := range a.symbols {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
		for _, tf := range yahooTimeframes {
			a.fetchSymbolTF(ctx, sym, tf.rng, tf.interval, tf.tf)
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
}

func (a *YahooFinanceAdapter) fetchSymbolTF(ctx context.Context, symbol, rng, interval, tf string) {
	apiURL := fmt.Sprintf("https://query2.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=%s",
		url.PathEscape(symbol), rng, interval)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.mu.Lock()
		a.errorCount++
		a.state = "error"
		a.mu.Unlock()
		slog.Warn("yahoo fetch error", "symbol", symbol, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		slog.Warn("yahoo bad status", "symbol", symbol, "status", resp.StatusCode)
		return
	}

	var data yahooChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		slog.Warn("yahoo decode error", "symbol", symbol, "error", err)
		return
	}

	if data.Chart.Error != nil {
		a.mu.Lock()
		a.errorCount++
		a.mu.Unlock()
		slog.Warn("yahoo api error", "symbol", symbol, "code", data.Chart.Error.Code)
		return
	}

	if len(data.Chart.Result) == 0 {
		return
	}

	result := data.Chart.Result[0]
	meta := result.Meta
	now := time.Now()
	published := 0

	// Publish historical candles from indicators.
	if len(result.Indicators.Quote) > 0 {
		q := result.Indicators.Quote[0]
		timestamps := result.Timestamps
		for i := range timestamps {
			if i >= len(q.Open) || i >= len(q.High) || i >= len(q.Low) || i >= len(q.Close) {
				break
			}
			if q.Open[i] == 0 && q.Close[i] == 0 {
				continue
			}
			var vol float64
			if i < len(q.Volume) {
				vol = float64(q.Volume[i])
			}
			a.bus.Publish(bus.Message{
				Topic: fmt.Sprintf("ohlc.yahoo.%s", meta.Symbol),
				Payload: feeds.OHLC{
					Instrument: meta.Symbol,
					Exchange:   "yahoo",
					Timeframe:  tf,
					Timestamp:  time.Unix(timestamps[i], 0),
					Open:       q.Open[i],
					High:       q.High[i],
					Low:        q.Low[i],
					Close:      q.Close[i],
					Volume:     vol,
				},
			})
			published++
		}
	}

	// Always publish current price as the latest candle.
	a.bus.Publish(bus.Message{
		Topic: fmt.Sprintf("ohlc.yahoo.%s", meta.Symbol),
		Payload: feeds.OHLC{
			Instrument: meta.Symbol,
			Exchange:   "yahoo",
			Timeframe:  "1d",
			Timestamp:  now,
			Open:       meta.RegularMarketPrice,
			High:       meta.RegularMarketPrice,
			Low:        meta.RegularMarketPrice,
			Close:      meta.RegularMarketPrice,
			Volume:     float64(meta.RegularMarketVolume),
		},
	})

	a.mu.Lock()
	a.lastUpdate = now
	a.state = "connected"
	a.bytesRecv += uint64(resp.ContentLength)
	a.mu.Unlock()

	slog.Debug("yahoo fetched", "symbol", meta.Symbol, "candles", published+1)
}
