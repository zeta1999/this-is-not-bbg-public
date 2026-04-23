package ravel

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// Config controls the Ravel directory watcher.
type Config struct {
	Path         string
	PollInterval time.Duration
	TopicPrefix  string
}

// Adapter implements feeds.Adapter. It scans a directory and decodes
// any .json / .xlsx files into Ravel Points on each pass.
type Adapter struct {
	cfg Config
	b   *bus.Bus

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	errorCount uint64
	seen       map[string]time.Time
	points     uint64
}

func NewAdapter(b *bus.Bus, cfg Config) *Adapter {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.TopicPrefix == "" {
		cfg.TopicPrefix = "ravel."
	}
	return &Adapter{
		cfg:   cfg,
		b:     b,
		state: "disconnected",
		seen:  make(map[string]time.Time),
	}
}

func (a *Adapter) Name() string { return "ravel" }

func (a *Adapter) Status() feeds.AdapterStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return feeds.AdapterStatus{
		Name:          "ravel",
		State:         a.state,
		LastUpdate:    a.lastUpdate,
		ErrorCount:    a.errorCount,
		BytesReceived: a.points,
	}
}

func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()

	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	a.scan(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.scan(ctx)
		}
	}
}

func (a *Adapter) scan(ctx context.Context) {
	if a.cfg.Path == "" {
		return
	}
	err := filepath.WalkDir(a.cfg.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".json" && ext != ".xlsx" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}

		a.mu.Lock()
		prev, ok := a.seen[path]
		fresh := !ok || info.ModTime().After(prev)
		if fresh {
			a.seen[path] = info.ModTime()
		}
		a.mu.Unlock()
		if !fresh {
			return nil
		}

		var points []Point
		var perr error
		switch ext {
		case ".json":
			points, perr = ParseFile(path)
		case ".xlsx":
			points, perr = ParseXLSX(path)
		}
		if perr != nil {
			slog.Warn("ravel parse", "path", path, "error", perr)
			a.mu.Lock()
			a.errorCount++
			a.mu.Unlock()
			return nil
		}
		a.publish(points, filepath.Base(path))
		return nil
	})
	if err != nil {
		slog.Debug("ravel scan error", "error", err)
	}
}

func (a *Adapter) publish(points []Point, file string) {
	if len(points) == 0 {
		return
	}
	now := time.Now()
	for _, p := range points {
		topic := a.cfg.TopicPrefix + topicForPoint(p)
		a.b.Publish(bus.Message{
			Topic: topic,
			Payload: map[string]any{
				"dims":  p.Dims,
				"t":     p.T,
				"value": p.Value.Scalar,
				"raw":   p.Value.Raw,
				"file":  file,
			},
		})
	}
	a.mu.Lock()
	a.points += uint64(len(points))
	a.lastUpdate = now
	a.mu.Unlock()
}

func topicForPoint(p Point) string {
	if t, ok := p.Dims["type"]; ok && t != "" {
		return t
	}
	return "unknown"
}
