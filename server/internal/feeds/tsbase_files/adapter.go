package tsbase_files

import (
	"context"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/feeds"
)

// Adapter wraps Tailer so the feed manager can supervise it like any
// other adapter.
type Adapter struct {
	t   *Tailer
	cfg Config

	mu         sync.RWMutex
	state      string
	lastUpdate time.Time
	lastRows   int64
}

// NewAdapter builds a feed manager-compatible adapter around a Tailer.
func NewAdapter(b *bus.Bus, cfg Config) *Adapter {
	return &Adapter{
		t:     New(b, cfg),
		cfg:   cfg,
		state: "disconnected",
	}
}

func (a *Adapter) Name() string { return "tsbase_files" }

func (a *Adapter) Status() feeds.AdapterStatus {
	rows := a.t.Rows()
	a.mu.Lock()
	if rows > a.lastRows {
		a.lastRows = rows
		a.lastUpdate = time.Now()
	}
	state := a.state
	lastUpdate := a.lastUpdate
	a.mu.Unlock()
	return feeds.AdapterStatus{
		Name:          "tsbase_files",
		State:         state,
		LastUpdate:    lastUpdate,
		BytesReceived: uint64(rows),
	}
}

func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	a.state = "connected"
	a.mu.Unlock()
	return a.t.Run(ctx)
}
