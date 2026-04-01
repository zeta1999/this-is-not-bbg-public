// Package monitor provides cross-exchange consistency checks.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// ConsistencyChecker compares prices for the same instrument across exchanges
// and publishes alerts when divergence exceeds a threshold.
type ConsistencyChecker struct {
	bus       *bus.Bus
	threshold float64 // percentage, e.g. 0.5 means 0.5%
	interval  time.Duration

	mu     sync.RWMutex
	prices map[string]map[string]priceEntry // instrument -> exchange -> last price
}

type priceEntry struct {
	Price     float64
	Timestamp time.Time
}

// NewConsistencyChecker creates a checker with the given threshold and check interval.
func NewConsistencyChecker(b *bus.Bus, thresholdPct float64, interval time.Duration) *ConsistencyChecker {
	return &ConsistencyChecker{
		bus:       b,
		threshold: thresholdPct,
		interval:  interval,
		prices:    make(map[string]map[string]priceEntry),
	}
}

// Run subscribes to OHLC and LOB updates, tracks latest prices, and periodically
// checks for cross-exchange divergence. Blocks until ctx is cancelled.
func (cc *ConsistencyChecker) Run(ctx context.Context) error {
	sub := cc.bus.Subscribe(256, "ohlc.*.*", "lob.*.*")
	defer cc.bus.Unsubscribe(sub)

	ticker := time.NewTicker(cc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case msg, ok := <-sub.C:
			if !ok {
				return nil
			}
			cc.updatePrice(msg)

		case <-ticker.C:
			cc.check()
		}
	}
}

func (cc *ConsistencyChecker) updatePrice(msg bus.Message) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	var instrument, exchange string
	var price float64
	var ts time.Time

	switch v := msg.Payload.(type) {
	case feeds.OHLC:
		instrument = v.Instrument
		exchange = v.Exchange
		price = v.Close
		ts = v.Timestamp
	case feeds.LOBSnapshot:
		instrument = v.Instrument
		exchange = v.Exchange
		ts = v.Timestamp
		if len(v.Bids) > 0 && len(v.Asks) > 0 {
			price = (v.Bids[0].Price + v.Asks[0].Price) / 2 // mid-price
		}
	default:
		return
	}

	if price == 0 {
		return
	}

	if _, ok := cc.prices[instrument]; !ok {
		cc.prices[instrument] = make(map[string]priceEntry)
	}
	cc.prices[instrument][exchange] = priceEntry{Price: price, Timestamp: ts}
}

func (cc *ConsistencyChecker) check() {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	staleThreshold := 5 * time.Minute

	for instrument, exchanges := range cc.prices {
		if len(exchanges) < 2 {
			continue
		}

		// Collect non-stale prices.
		var prices []struct {
			exchange string
			price    float64
		}
		for ex, entry := range exchanges {
			if time.Since(entry.Timestamp) < staleThreshold {
				prices = append(prices, struct {
					exchange string
					price    float64
				}{ex, entry.Price})
			}
		}

		if len(prices) < 2 {
			continue
		}

		// Check all pairs for divergence.
		for i := 0; i < len(prices); i++ {
			for j := i + 1; j < len(prices); j++ {
				avg := (prices[i].price + prices[j].price) / 2
				if avg == 0 {
					continue
				}
				divergencePct := math.Abs(prices[i].price-prices[j].price) / avg * 100

				if divergencePct > cc.threshold {
					slog.Warn("cross-exchange divergence",
						"instrument", instrument,
						"exchange_a", prices[i].exchange,
						"price_a", prices[i].price,
						"exchange_b", prices[j].exchange,
						"price_b", prices[j].price,
						"divergence_pct", fmt.Sprintf("%.3f", divergencePct),
					)

					cc.bus.Publish(bus.Message{
						Topic: "alert",
						Payload: map[string]any{
							"type":          "consistency",
							"instrument":    instrument,
							"exchange_a":    prices[i].exchange,
							"price_a":       prices[i].price,
							"exchange_b":    prices[j].exchange,
							"price_b":       prices[j].price,
							"divergence_pct": divergencePct,
						},
					})
				}
			}
		}
	}
}
