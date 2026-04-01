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
type OHLC struct {
	Instrument string
	Exchange   string
	Timeframe  string
	Timestamp  time.Time
	Open       float64
	High       float64
	Low        float64
	Close      float64
	Volume     float64
}

// Trade represents a single executed trade.
type Trade struct {
	Instrument string
	Exchange   string
	Timestamp  time.Time
	Price      float64
	Quantity   float64
	Side       string // "buy" or "sell"
	TradeID    string
}

// LOBLevel represents a price level in the order book.
type LOBLevel struct {
	Price    float64
	Quantity float64
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
