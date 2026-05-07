package monitor

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// PricesSanityChecker publishes a periodic cross-venue "does this
// number make sense?" snapshot for a curated list of popular pairs.
// It's a sibling to ConsistencyChecker: where consistency fires an
// alert on any divergent pairwise comparison, sanity computes the
// median across every healthy venue for a configured instrument and
// flags outliers against the consensus.
//
// The intent is NO FREEZE-aligned: the tool is the trader's last
// line of defense during a trading-system failure, so if a single
// venue silently goes stale or mispriced the operator sees it
// against the rest of the market at a glance, not buried in logs.
//
// Output topic: `sanity.prices` — one message per configured pair
// per tick, with the venues array, the median, and per-venue delta
// and outlier flags.
type PricesSanityChecker struct {
	bus          *bus.Bus
	interval     time.Duration
	thresholdPct float64 // default; an outlier is any venue whose |delta/median|*100 exceeds this
	pairs        map[string]struct{}

	mu     sync.RWMutex
	latest map[string]map[string]venueEntry // instrument -> exchange -> mid + ts
}

type venueEntry struct {
	Mid       float64
	Timestamp time.Time
}

// SanityVenue is the per-venue row in the published snapshot. Kept
// as a typed struct (not map[string]any) so downstream subscribers
// can decode without guessing field names.
type SanityVenue struct {
	Exchange   string  `json:"exchange"`
	Mid        float64 `json:"mid"`
	DeltaPct   float64 `json:"delta_pct"` // signed; positive = above median
	AgeSeconds int64   `json:"age_seconds"`
	Outlier    bool    `json:"outlier"`
}

// SanitySnapshot is the envelope published on `sanity.prices`.
type SanitySnapshot struct {
	Instrument   string        `json:"instrument"`
	Median       float64       `json:"median"`
	ThresholdPct float64       `json:"threshold_pct"`
	VenueCount   int           `json:"venue_count"`
	OutlierCount int           `json:"outlier_count"`
	Venues       []SanityVenue `json:"venues"`
	Timestamp    time.Time     `json:"timestamp"`
}

// NewPricesSanityChecker constructs a checker with the given
// interval (default 30s when zero), default threshold percentage
// (default 0.5% when zero), and curated pair allowlist (empty list
// disables the checker).
func NewPricesSanityChecker(b *bus.Bus, interval time.Duration, thresholdPct float64, pairs []string) *PricesSanityChecker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if thresholdPct <= 0 {
		thresholdPct = 0.5
	}
	set := make(map[string]struct{}, len(pairs))
	for _, p := range pairs {
		if p != "" {
			set[p] = struct{}{}
		}
	}
	return &PricesSanityChecker{
		bus:          b,
		interval:     interval,
		thresholdPct: thresholdPct,
		pairs:        set,
		latest:       make(map[string]map[string]venueEntry),
	}
}

// Run subscribes to OHLC + LOB streams, keeps the latest mid per
// venue, and every interval publishes one SanitySnapshot per
// configured pair to `sanity.prices`. Blocks until ctx is cancelled.
// Returns nil on shutdown — consistent with the other monitors.
//
// If no pairs are configured the checker is a no-op (still blocks
// on ctx, so the errgroup stays happy).
func (sc *PricesSanityChecker) Run(ctx context.Context) error {
	if len(sc.pairs) == 0 {
		slog.Info("sanity checker disabled — no pairs configured")
		<-ctx.Done()
		return nil
	}

	sub := sc.bus.Subscribe(256, "ohlc.*.*", "lob.*.*")
	defer sc.bus.Unsubscribe(sub)

	ticker := time.NewTicker(sc.interval)
	defer ticker.Stop()

	slog.Info("sanity checker started",
		"pairs", len(sc.pairs),
		"interval", sc.interval,
		"threshold_pct", sc.thresholdPct)

	for {
		select {
		case <-ctx.Done():
			return nil

		case msg, ok := <-sub.C:
			if !ok {
				return nil
			}
			sc.absorb(msg)

		case <-ticker.C:
			sc.tick()
		}
	}
}

// absorb is the hot path — keep it branch-light so we don't become
// a bottleneck on high-volume topics. We only care about pairs in
// the allowlist; everything else is ignored.
func (sc *PricesSanityChecker) absorb(msg bus.Message) {
	var instrument, exchange string
	var mid float64
	var ts time.Time

	switch v := msg.Payload.(type) {
	case feeds.OHLC:
		if _, ok := sc.pairs[v.Instrument]; !ok {
			return
		}
		instrument, exchange, mid, ts = v.Instrument, v.Exchange, v.Close, v.Timestamp
	case feeds.LOBSnapshot:
		if _, ok := sc.pairs[v.Instrument]; !ok {
			return
		}
		if len(v.Bids) == 0 || len(v.Asks) == 0 {
			return
		}
		instrument = v.Instrument
		exchange = v.Exchange
		mid = (v.Bids[0].Price + v.Asks[0].Price) / 2
		ts = v.Timestamp
	default:
		return
	}
	if mid <= 0 || exchange == "" {
		return
	}
	// Reject historical replays. The backfill coordinator republishes
	// year-old bars on the live ohlc.* topic; without this guard the
	// panel would flag binance at $102K (last year's price) as the
	// venue's "current" mid. See feeds.IsHistoricalReplay — same
	// guard used by the alerts engine and the consistency checker so
	// the live window is consistent across all live consumers.
	if feeds.IsHistoricalReplay(msg.Payload) {
		return
	}

	sc.mu.Lock()
	byEx, ok := sc.latest[instrument]
	if !ok {
		byEx = make(map[string]venueEntry)
		sc.latest[instrument] = byEx
	}
	// Don't let an older bar overwrite a newer one. Out-of-order
	// arrivals (e.g. a slow REST poll racing a fast WS tick) would
	// otherwise regress the venue's mid to the older value.
	if existing, ok := byEx[exchange]; ok && !ts.IsZero() && !existing.Timestamp.IsZero() && ts.Before(existing.Timestamp) {
		sc.mu.Unlock()
		return
	}
	byEx[exchange] = venueEntry{Mid: mid, Timestamp: ts}
	sc.mu.Unlock()
}

// tick computes one SanitySnapshot per pair from the accumulated
// latest-mids and publishes it. Stale venues (>5 min old) are
// dropped from the median but still emitted with Outlier=true so
// the UI can render "this venue is stale" rather than silently hide
// the disconnect.
func (sc *PricesSanityChecker) tick() {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	const staleAfter = 5 * time.Minute
	now := time.Now()

	for instrument := range sc.pairs {
		byEx, ok := sc.latest[instrument]
		if !ok || len(byEx) == 0 {
			continue
		}

		// Collect fresh mids for the median; remember stale entries
		// so they appear in the Venues array with Outlier=true.
		type row struct {
			exchange string
			mid      float64
			age      time.Duration
			stale    bool
		}
		rows := make([]row, 0, len(byEx))
		freshMids := make([]float64, 0, len(byEx))
		for ex, entry := range byEx {
			age := now.Sub(entry.Timestamp)
			stale := age > staleAfter
			if !stale {
				freshMids = append(freshMids, entry.Mid)
			}
			rows = append(rows, row{ex, entry.Mid, age, stale})
		}
		if len(freshMids) == 0 {
			continue // nothing to compare against
		}

		median := median(freshMids)
		if median <= 0 {
			continue
		}

		// Sort for deterministic output order (stable across snapshots).
		sort.Slice(rows, func(i, j int) bool { return rows[i].exchange < rows[j].exchange })

		venues := make([]SanityVenue, 0, len(rows))
		outliers := 0
		for _, r := range rows {
			delta := (r.mid - median) / median * 100
			outlier := r.stale || math.Abs(delta) > sc.thresholdPct
			if outlier {
				outliers++
			}
			venues = append(venues, SanityVenue{
				Exchange:   r.exchange,
				Mid:        r.mid,
				DeltaPct:   delta,
				AgeSeconds: int64(r.age.Seconds()),
				Outlier:    outlier,
			})
		}

		snap := SanitySnapshot{
			Instrument:   instrument,
			Median:       median,
			ThresholdPct: sc.thresholdPct,
			VenueCount:   len(rows),
			OutlierCount: outliers,
			Venues:       venues,
			Timestamp:    now,
		}
		sc.bus.Publish(bus.Message{
			Topic:   "sanity.prices",
			Payload: snap,
		})

		if outliers > 0 {
			slog.Warn("sanity outlier",
				"instrument", instrument,
				"median", median,
				"venues", len(rows),
				"outliers", outliers)
		}
	}
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	ys := make([]float64, len(xs))
	copy(ys, xs)
	sort.Float64s(ys)
	n := len(ys)
	if n%2 == 1 {
		return ys[n/2]
	}
	return (ys[n/2-1] + ys[n/2]) / 2
}
