// Package alerts implements the alert engine for price, volume, and keyword triggers.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// AlertType enumerates alert trigger types.
type AlertType int

const (
	PriceAbove AlertType = iota + 1
	PriceBelow
	VolumeSpike
	Keyword
	FeedDown
)

// Alert is a configured alert rule.
type Alert struct {
	ID         string
	Type       AlertType
	Instrument string  // empty for keyword/feed alerts
	Threshold  float64 // price level or volume multiplier
	Keyword    string  // for keyword alerts
	Status     string  // "active", "triggered", "dismissed"
	CreatedAt  time.Time
	TriggeredAt time.Time
}

// Engine evaluates alerts against incoming market data.
type Engine struct {
	bus    *bus.Bus
	mu     sync.RWMutex
	alerts map[string]*Alert
	nextID int

	// persistPath, when non-empty, causes Add/Dismiss to atomically
	// rewrite a JSON snapshot at that path. LoadFromFile hydrates
	// the engine from the same file at startup so rules survive a
	// restart (alert *state* — Status / TriggeredAt — rolls forward
	// with the process; matches the single-shot semantics).
	persistPath string

	// Track recent volumes for spike detection.
	recentVolumes map[string][]float64 // instrument -> last N volumes
}

// NewEngine creates an alert engine.
func NewEngine(b *bus.Bus) *Engine {
	return &Engine{
		bus:           b,
		alerts:        make(map[string]*Alert),
		recentVolumes: make(map[string][]float64),
	}
}

// EnablePersistence configures disk persistence. Rules are loaded
// from path immediately (missing file is a no-op, not an error);
// subsequent Add/Dismiss calls rewrite the file atomically via a
// tmp-file + rename dance.
func (e *Engine) EnablePersistence(path string) error {
	e.persistPath = path
	return e.loadLocked()
}

func (e *Engine) loadLocked() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	data, err := os.ReadFile(e.persistPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("alerts: load %s: %w", e.persistPath, err)
	}
	var snapshot struct {
		NextID int             `json:"next_id"`
		Alerts map[string]*Alert `json:"alerts"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("alerts: parse %s: %w", e.persistPath, err)
	}
	if snapshot.Alerts != nil {
		e.alerts = snapshot.Alerts
	}
	if snapshot.NextID > e.nextID {
		e.nextID = snapshot.NextID
	}
	slog.Info("alerts loaded", "count", len(e.alerts), "path", e.persistPath)
	return nil
}

// saveLocked writes the current alert map atomically. Caller holds
// e.mu (any lock mode — we only read e.alerts + e.nextID).
func (e *Engine) saveLocked() {
	if e.persistPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(e.persistPath), 0o755); err != nil {
		slog.Warn("alerts: mkdir", "error", err)
		return
	}
	snapshot := struct {
		NextID int               `json:"next_id"`
		Alerts map[string]*Alert `json:"alerts"`
	}{NextID: e.nextID, Alerts: e.alerts}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		slog.Warn("alerts: marshal", "error", err)
		return
	}
	tmp := e.persistPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Warn("alerts: write tmp", "error", err)
		return
	}
	if err := os.Rename(tmp, e.persistPath); err != nil {
		slog.Warn("alerts: rename", "error", err)
	}
}

// Add creates a new alert and returns its ID.
func (e *Engine) Add(a Alert) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.nextID++
	a.ID = fmt.Sprintf("alert-%d", e.nextID)
	a.Status = "active"
	a.CreatedAt = time.Now()
	e.alerts[a.ID] = &a
	slog.Info("alert created", "id", a.ID, "type", a.Type, "instrument", a.Instrument)
	e.saveLocked()
	return a.ID
}

// Dismiss marks an alert as dismissed.
func (e *Engine) Dismiss(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if a, ok := e.alerts[id]; ok {
		a.Status = "dismissed"
		e.saveLocked()
	}
}

// List returns all alerts.
func (e *Engine) List() []Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var out []Alert
	for _, a := range e.alerts {
		out = append(out, *a)
	}
	return out
}

// Run subscribes to market data and evaluates alerts. Blocks until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	sub := e.bus.Subscribe(512, "ohlc.*.*", "trade.*.*", "news")
	defer e.bus.Unsubscribe(sub)

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-sub.C:
			if !ok {
				return nil
			}
			e.evaluate(msg)
		}
	}
}

func (e *Engine) evaluate(msg bus.Message) {
	// Reject historical replays. The backfill coordinator
	// republishes year-old bars on the live ohlc.* topic at startup;
	// without this guard a `PriceAbove 80000` alert the trader set
	// yesterday would trigger on a kline from when BTC was at $102K
	// last year. Staleness filter mirrors what sanity + consistency
	// do — see feeds.LiveWindow for the threshold.
	if feeds.IsHistoricalReplay(msg.Payload) {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, a := range e.alerts {
		if a.Status != "active" {
			continue
		}

		triggered := false

		switch v := msg.Payload.(type) {
		case feeds.OHLC:
			if a.Instrument != "" && !strings.EqualFold(a.Instrument, v.Instrument) {
				continue
			}
			switch a.Type {
			case PriceAbove:
				triggered = v.Close > a.Threshold
			case PriceBelow:
				triggered = v.Close < a.Threshold
			case VolumeSpike:
				triggered = e.checkVolumeSpike(v.Instrument, v.Volume, a.Threshold)
			}

		case feeds.Trade:
			if a.Instrument != "" && !strings.EqualFold(a.Instrument, v.Instrument) {
				continue
			}
			switch a.Type {
			case PriceAbove:
				triggered = v.Price > a.Threshold
			case PriceBelow:
				triggered = v.Price < a.Threshold
			}

		case map[string]any:
			// News items come as maps for now.
			if a.Type == Keyword && a.Keyword != "" {
				if title, ok := v["title"].(string); ok {
					triggered = strings.Contains(strings.ToLower(title), strings.ToLower(a.Keyword))
				}
			}
		}

		if triggered {
			a.Status = "triggered"
			a.TriggeredAt = time.Now()
			slog.Info("alert triggered", "id", a.ID, "type", a.Type, "instrument", a.Instrument)

			e.bus.Publish(bus.Message{
				Topic:   "alert",
				Payload: *a,
			})
		}
	}
}

func (e *Engine) checkVolumeSpike(instrument string, volume, multiplier float64) bool {
	vols := e.recentVolumes[instrument]
	vols = append(vols, volume)
	if len(vols) > 20 {
		vols = vols[len(vols)-20:]
	}
	e.recentVolumes[instrument] = vols

	if len(vols) < 5 {
		return false
	}

	// Average of previous volumes (excluding current).
	sum := 0.0
	for _, v := range vols[:len(vols)-1] {
		sum += v
	}
	avg := sum / float64(len(vols)-1)

	return avg > 0 && volume > avg*multiplier
}
