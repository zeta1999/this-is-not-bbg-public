// Package feeds provides the feed manager and adapter interfaces for data ingestion.
package feeds

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

// Adapter is the interface that exchange and data source adapters must implement.
type Adapter interface {
	// Name returns the unique identifier for this adapter.
	Name() string
	// Start begins data ingestion. It should block until ctx is cancelled.
	Start(ctx context.Context) error
	// Status returns the current health status.
	Status() AdapterStatus
}

// AdapterStatus represents the health of an adapter.
type AdapterStatus struct {
	Name          string
	State         string // "connected", "reconnecting", "stale", "error"
	LastUpdate    time.Time
	LatencyMs     float64
	ErrorCount    uint64
	BytesReceived uint64
}

// OHLC represents a canonical candlestick.
//
// double fields keep the fast-math path; *Decimal fields carry the
// exchange-committed string for crypto fidelity (audit, backtester,
// OMS — see feedback_decimal_required.md). Adapters populate
// decimals from the raw json.Number; "" is allowed and means "this
// venue didn't expose a string for this field".
type OHLC struct {
	Instrument    string
	Exchange      string
	Timeframe     string
	Timestamp     time.Time
	Open          float64
	High          float64
	Low           float64
	Close         float64
	Volume        float64
	OpenDecimal   string `json:",omitempty"`
	HighDecimal   string `json:",omitempty"`
	LowDecimal    string `json:",omitempty"`
	CloseDecimal  string `json:",omitempty"`
	VolumeDecimal string `json:",omitempty"`
}

// Trade represents a single executed trade.
type Trade struct {
	Instrument      string
	Exchange        string
	Timestamp       time.Time
	Price           float64
	Quantity        float64
	Side            string // "buy" or "sell"
	TradeID         string
	PriceDecimal    string `json:",omitempty"`
	QuantityDecimal string `json:",omitempty"`
}

// LOBLevel represents a price level in the order book.
type LOBLevel struct {
	Price           float64
	Quantity        float64
	OrderCount      uint32 // populated where venue exposes (e.g. Bybit's [px,sz,n]); zero otherwise
	PriceDecimal    string `json:",omitempty"`
	QuantityDecimal string `json:",omitempty"`
}

// LOBSnapshot represents a snapshot of the order book.
type LOBSnapshot struct {
	Instrument     string
	Exchange       string
	Timestamp      time.Time
	SequenceNumber uint64
	Bids           []LOBLevel
	Asks           []LOBLevel
}

// PerpetualSnapshot is the canonical payload for perpetual-futures
// snapshots published on `perp.<exchange>.<symbol>`. Replaces the
// ad-hoc map[string]float64 payloads individual venue adapters used
// before the 2026-04-24 canonicalisation. Each field is optional at
// the wire level — an adapter publishes only what the venue exposes.
type PerpetualSnapshot struct {
	Instrument string    `json:"Instrument"`
	Exchange   string    `json:"Exchange"`
	Timestamp  time.Time `json:"Timestamp"`

	// Mark/index/funding triad. All floats are in the instrument's
	// quote currency (or fraction-of-one for FundingRate).
	MarkPrice       float64 `json:"MarkPrice,omitempty"`
	IndexPrice      float64 `json:"IndexPrice,omitempty"`
	FundingRate     float64 `json:"FundingRate,omitempty"`      // per 8-hour period by convention
	NextFundingRate float64 `json:"NextFundingRate,omitempty"`
	NextFundingTime int64   `json:"NextFundingTime,omitempty"`  // unix millis

	// Open interest is the number of open contracts — either in base
	// units (`OpenInterestBase`) or quote (`OpenInterestQuote`);
	// adapters populate whichever the venue publishes.
	OpenInterestBase  float64 `json:"OpenInterestBase,omitempty"`
	OpenInterestQuote float64 `json:"OpenInterestQuote,omitempty"`

	// Most recent liquidation on this instrument (informational; full
	// liquidation streams should be published to a dedicated
	// `liquidation.<exchange>.<symbol>` topic, not in the snapshot).
	LastLiquidationSide     string  `json:"LastLiquidationSide,omitempty"`     // "buy" | "sell"
	LastLiquidationPrice    float64 `json:"LastLiquidationPrice,omitempty"`
	LastLiquidationQuantity float64 `json:"LastLiquidationQuantity,omitempty"`
}

// LiquidationEvent is the canonical payload for the dedicated
// `liquidation.<exchange>.<symbol>` stream. One event per wire
// update — adapters that batch multiple liquidations in a single
// WS frame publish one bus message per row so downstream consumers
// can filter without re-splitting.
//
// `Side` is the *taker* side of the liquidation (the side that got
// forcibly closed): "buy" means a short was liquidated (forced
// buy-to-cover), "sell" means a long was liquidated.
type LiquidationEvent struct {
	Instrument string    `json:"Instrument"`
	Exchange   string    `json:"Exchange"`
	Timestamp  time.Time `json:"Timestamp"`
	Side       string    `json:"Side"`
	Price      float64   `json:"Price"`
	Quantity   float64   `json:"Quantity,omitempty"`
	Notional   float64   `json:"Notional,omitempty"`
}

// BackfillRequest is a user-visible ask for historical data that the
// live feeds don't hold in memory. Served by adapters that implement
// Backfiller.
type BackfillRequest struct {
	Instrument string
	Exchange   string
	Timeframe  string // "1m", "1h", "1d" for OHLC
	From       time.Time
	To         time.Time
	Limit      int // 0 = adapter default cap
}

// Backfiller is an optional capability — adapters that can fetch
// historical OHLC pages (e.g. CCXT fetchOHLCV) implement it. The
// coordinator in internal/feeds/backfill type-asserts on this.
type Backfiller interface {
	BackfillHistorical(ctx context.Context, req BackfillRequest) ([]OHLC, error)
}

// Manager supervises all feed adapters, handling lifecycle and health reporting.
type Manager struct {
	bus      *bus.Bus
	adapters map[string]Adapter
	mu       sync.RWMutex
}

// NewManager creates a feed manager connected to the given bus.
func NewManager(b *bus.Bus) *Manager {
	return &Manager{
		bus:      b,
		adapters: make(map[string]Adapter),
	}
}

// Register adds an adapter to the manager.
func (m *Manager) Register(adapter Adapter) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := adapter.Name()
	if _, exists := m.adapters[name]; exists {
		return fmt.Errorf("adapter %q already registered", name)
	}
	m.adapters[name] = adapter
	slog.Info("registered feed adapter", "name", name)
	return nil
}

// StartAll launches all registered adapters in separate goroutines.
// Blocks until ctx is cancelled. Publishes FeedStatus every statusInterval.
func (m *Manager) StartAll(ctx context.Context, statusInterval time.Duration) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if statusInterval == 0 {
		statusInterval = 10 * time.Second
	}

	var wg sync.WaitGroup

	// Start each adapter.
	for _, adapter := range m.adapters {
		a := adapter
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("starting adapter", "name", a.Name())
			if err := a.Start(ctx); err != nil && ctx.Err() == nil {
				slog.Error("adapter failed", "name", a.Name(), "error", err)
			}
		}()
	}

	// Status reporting loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(statusInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.publishStatus()
			}
		}
	}()

	wg.Wait()
	return nil
}

func (m *Manager) publishStatus() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, adapter := range m.adapters {
		status := adapter.Status()
		m.bus.Publish(bus.Message{
			Topic:   "feed.status",
			Payload: status,
		})
	}
}

// Statuses returns the current status of all adapters.
func (m *Manager) Statuses() []AdapterStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []AdapterStatus
	for _, adapter := range m.adapters {
		statuses = append(statuses, adapter.Status())
	}
	return statuses
}
