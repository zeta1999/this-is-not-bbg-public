// Trade aggregator subscribes to raw trade streams and publishes:
// - trade.agg.<exchange>.<instrument> every aggWindow (default 1s): OHLCV + VWAP + quantiles
// - trade.snap.<exchange>.<instrument> every snapWindow (default 5s): last N individual trades
//
// Raw trades (trade.<exchange>.<instrument>) continue flowing to cache + datalake
// but are dropped from the client relay to avoid overwhelming TUI/desktop/phone.
package feeds

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

// TradeAgg holds aggregated trade statistics for one window period.
type TradeAgg struct {
	Instrument string    `json:"Instrument"`
	Exchange   string    `json:"Exchange"`
	Window     string    `json:"Window"` // "1s", "5s", etc.
	Timestamp  time.Time `json:"Timestamp"`

	Count      int64   `json:"Count"`
	Volume     float64 `json:"Volume"`
	BuyVolume  float64 `json:"BuyVolume"`
	SellVolume float64 `json:"SellVolume"`
	VWAP       float64 `json:"VWAP"`
	Open       float64 `json:"Open"`
	High       float64 `json:"High"`
	Low        float64 `json:"Low"`
	Close      float64 `json:"Close"`
	Turnover   float64 `json:"Turnover"` // sum(price * qty)

	P25 float64 `json:"P25"`
	P50 float64 `json:"P50"` // median
	P75 float64 `json:"P75"`
}

// TradeSnap holds recent individual trades for display.
type TradeSnap struct {
	Instrument string       `json:"Instrument"`
	Exchange   string       `json:"Exchange"`
	Trades     []TradeEntry `json:"Trades"`
}

// TradeEntry is a single trade in a snapshot.
type TradeEntry struct {
	Price     float64   `json:"Price"`
	Quantity  float64   `json:"Quantity"`
	Side      string    `json:"Side"`
	Timestamp time.Time `json:"Timestamp"`
}

// instrumentAcc accumulates trades for one instrument during a window.
type instrumentAcc struct {
	exchange   string
	instrument string
	prices     []float64 // for quantile computation
	trades     []TradeEntry
	agg        TradeAgg
}

// Aggregator computes rolling trade statistics.
type Aggregator struct {
	bus        *bus.Bus
	aggWindow  time.Duration
	snapWindow time.Duration
	snapSize   int // max trades in snapshot

	mu   sync.Mutex
	accs map[string]*instrumentAcc // key: exchange/instrument
	// Ring buffer of recent trades per instrument for snapshots.
	snaps map[string][]TradeEntry
}

// NewAggregator creates a trade aggregator.
func NewAggregator(b *bus.Bus) *Aggregator {
	return &Aggregator{
		bus:        b,
		aggWindow:  1 * time.Second,
		snapWindow: 5 * time.Second,
		snapSize:   20,
		accs:       make(map[string]*instrumentAcc),
		snaps:      make(map[string][]TradeEntry),
	}
}

// Run starts the aggregator. Blocks until ctx is cancelled.
func (a *Aggregator) Run(ctx context.Context) error {
	sub := a.bus.Subscribe(4096, "trade.*.*")
	defer a.bus.Unsubscribe(sub)

	aggTicker := time.NewTicker(a.aggWindow)
	defer aggTicker.Stop()

	snapTicker := time.NewTicker(a.snapWindow)
	defer snapTicker.Stop()

	slog.Info("trade aggregator started", "agg_window", a.aggWindow, "snap_window", a.snapWindow)

	for {
		select {
		case <-ctx.Done():
			return nil

		case msg, ok := <-sub.C:
			if !ok {
				return nil
			}
			// Skip aggregate/snap messages (avoid feedback loop).
			if strings.HasPrefix(msg.Topic, "trade.agg.") || strings.HasPrefix(msg.Topic, "trade.snap.") {
				continue
			}
			trade, ok := msg.Payload.(Trade)
			if !ok {
				continue
			}
			a.addTrade(trade)

		case <-aggTicker.C:
			a.publishAggregates()

		case <-snapTicker.C:
			a.publishSnapshots()
		}
	}
}

func (a *Aggregator) addTrade(t Trade) {
	key := t.Exchange + "/" + t.Instrument

	a.mu.Lock()
	defer a.mu.Unlock()

	acc, ok := a.accs[key]
	if !ok {
		acc = &instrumentAcc{
			exchange:   t.Exchange,
			instrument: t.Instrument,
		}
		acc.agg.Open = t.Price
		acc.agg.High = t.Price
		acc.agg.Low = t.Price
		a.accs[key] = acc
	}

	// Update aggregate.
	acc.agg.Count++
	acc.agg.Volume += t.Quantity
	acc.agg.Turnover += t.Price * t.Quantity
	acc.agg.Close = t.Price
	if t.Price > acc.agg.High {
		acc.agg.High = t.Price
	}
	if t.Price < acc.agg.Low || acc.agg.Low == 0 {
		acc.agg.Low = t.Price
	}
	if t.Side == "buy" {
		acc.agg.BuyVolume += t.Quantity
	} else {
		acc.agg.SellVolume += t.Quantity
	}
	acc.prices = append(acc.prices, t.Price)

	// Add to snapshot ring.
	entry := TradeEntry{
		Price:     t.Price,
		Quantity:  t.Quantity,
		Side:      t.Side,
		Timestamp: t.Timestamp,
	}
	acc.trades = append(acc.trades, entry)

	snap := a.snaps[key]
	snap = append(snap, entry)
	if len(snap) > a.snapSize {
		snap = snap[len(snap)-a.snapSize:]
	}
	a.snaps[key] = snap
}

func (a *Aggregator) publishAggregates() {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	for key, acc := range a.accs {
		if acc.agg.Count == 0 {
			continue
		}

		agg := acc.agg
		agg.Instrument = acc.instrument
		agg.Exchange = acc.exchange
		agg.Window = a.aggWindow.String()
		agg.Timestamp = now

		// VWAP.
		if agg.Volume > 0 {
			agg.VWAP = agg.Turnover / agg.Volume
		}

		// Quantiles.
		if len(acc.prices) > 0 {
			sorted := make([]float64, len(acc.prices))
			copy(sorted, acc.prices)
			sort.Float64s(sorted)
			agg.P25 = quantile(sorted, 0.25)
			agg.P50 = quantile(sorted, 0.50)
			agg.P75 = quantile(sorted, 0.75)
		}

		topic := fmt.Sprintf("trade.agg.%s.%s", acc.exchange, acc.instrument)
		a.bus.Publish(bus.Message{Topic: topic, Payload: agg})

		// Reset accumulator for next window.
		a.accs[key] = &instrumentAcc{
			exchange:   acc.exchange,
			instrument: acc.instrument,
		}
	}
}

func (a *Aggregator) publishSnapshots() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for key, trades := range a.snaps {
		if len(trades) == 0 {
			continue
		}
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		snap := TradeSnap{
			Instrument: parts[1],
			Exchange:   parts[0],
			Trades:     trades,
		}
		topic := fmt.Sprintf("trade.snap.%s.%s", parts[0], parts[1])
		a.bus.Publish(bus.Message{Topic: topic, Payload: snap})
	}
}

func quantile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper || upper >= len(sorted) {
		return sorted[lower]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
