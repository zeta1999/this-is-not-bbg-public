// Package monitor provides feed health tracking and system metrics.
package monitor

import (
	"context"
	"log/slog"
	"runtime"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

// SystemMetrics captures runtime system stats.
type SystemMetrics struct {
	Goroutines  int
	HeapAllocMB float64
	HeapSysMB   float64
	NumGC       uint32
	Timestamp   time.Time
}

// HealthMonitor tracks system-level metrics and publishes them to the bus.
type HealthMonitor struct {
	bus      *bus.Bus
	interval time.Duration
}

// NewHealthMonitor creates a system health monitor.
func NewHealthMonitor(b *bus.Bus, interval time.Duration) *HealthMonitor {
	if interval == 0 {
		interval = 10 * time.Second
	}
	return &HealthMonitor{bus: b, interval: interval}
}

// Run starts the health monitoring loop. Blocks until ctx is cancelled.
func (h *HealthMonitor) Run(ctx context.Context) error {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			metrics := h.collect()
			h.bus.Publish(bus.Message{
				Topic:   "system.health",
				Payload: metrics,
			})
			slog.Debug("system health",
				"goroutines", metrics.Goroutines,
				"heap_mb", metrics.HeapAllocMB,
			)
		}
	}
}

func (h *HealthMonitor) collect() SystemMetrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return SystemMetrics{
		Goroutines:  runtime.NumGoroutine(),
		HeapAllocMB: float64(m.HeapAlloc) / 1024 / 1024,
		HeapSysMB:   float64(m.HeapSys) / 1024 / 1024,
		NumGC:       m.NumGC,
		Timestamp:   time.Now(),
	}
}
