package feeds

import "time"

// LiveWindow is how recent a market-data event has to be to count as
// "live" for downstream consumers (sanity, consistency, alerts).
// Bars / trades older than this are treated as historical replays
// and ignored by live evaluators — without this guard the backfill
// coordinator's bus republish would trigger a year-old PriceAbove
// alert or flag a venue's mid as $102K (last year's price) on
// startup.
//
// 5 min is intentionally generous: even a slow REST poll on a
// minutely feed should land well within this window. It matches
// the staleness threshold the sanity tick() and consistency
// check() already use on the read side, so the live window is the
// same on the way in and the way out.
const LiveWindow = 5 * time.Minute

// IsHistoricalReplay returns true if the message's bar/trade/snapshot
// timestamp is more than LiveWindow in the past. Returns false for
// payload types that don't carry a canonical timestamp (e.g. plain
// maps, alerts, plugin output) — those are caller-defined and
// can't be filtered by age generically.
func IsHistoricalReplay(payload any) bool {
	var ts time.Time
	switch v := payload.(type) {
	case OHLC:
		ts = v.Timestamp
	case Trade:
		ts = v.Timestamp
	case LOBSnapshot:
		ts = v.Timestamp
	default:
		return false
	}
	if ts.IsZero() {
		return false // no timestamp → don't filter; caller handles
	}
	return time.Since(ts) > LiveWindow
}
