// Package datalake provides append-only data persistence in Hive-partitioned
// folder structures. All bus messages matching configured topics are written
// to JSONL files organized by type/exchange/instrument/date.
package datalake

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// marshalPayload renders a bus payload to JSON. Proto messages go
// through protojson so the output is canonical protobuf JSON that
// round-trips via protojson.Unmarshal (what schemacheck verifies);
// other payloads (plain structs, maps, strings) fall back to
// encoding/json.
func marshalPayload(payload any) ([]byte, error) {
	if m, ok := payload.(proto.Message); ok {
		return protojson.Marshal(m)
	}
	return json.Marshal(payload)
}

// Config for the datalake writer.
type Config struct {
	Path     string   `yaml:"path"`     // root directory (e.g. "/data/notbbg")
	Enabled  bool     `yaml:"enabled"`
	Topics   []string `yaml:"topics"`   // glob patterns to capture (default: all)
	Format   string   `yaml:"format"`   // "jsonl" (default) or "csv"
	Rotation string   `yaml:"rotation"` // "daily" (default) or "hourly"
}

// Writer subscribes to the bus and appends all matching messages to disk.
type Writer struct {
	bus      *bus.Bus
	basePath string
	topics   []string
	rotation string

	mu       sync.Mutex
	files    map[string]*os.File // open file handles keyed by path
	written  atomic.Int64
}

// New creates a datalake writer.
func New(b *bus.Bus, cfg Config) *Writer {
	topics := cfg.Topics
	if len(topics) == 0 {
		topics = []string{"ohlc.*.*", "lob.*.*", "trade.*.*", "news", "perp.*.*", "indicator.*"}
	}
	rotation := cfg.Rotation
	if rotation == "" {
		rotation = "daily"
	}
	return &Writer{
		bus:      b,
		basePath: cfg.Path,
		topics:   topics,
		rotation: rotation,
		files:    make(map[string]*os.File),
	}
}

// Run starts the datalake writer. Blocks until ctx is cancelled.
func (w *Writer) Run(ctx context.Context) error {
	sub := w.bus.Subscribe(8192, w.topics...)
	defer w.bus.Unsubscribe(sub)
	defer w.closeAll()

	slog.Info("datalake writer started", "path", w.basePath, "topics", w.topics)

	statsTicker := time.NewTicker(60 * time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("datalake writer stopped",
				"written", w.written.Load(),
				"bus_dropped", sub.Dropped())
			return nil

		case msg, ok := <-sub.C:
			if !ok {
				return nil
			}
			w.writeMessage(msg)

		case <-statsTicker.C:
			slog.Info("datalake stats",
				"written", w.written.Load(),
				"open_files", len(w.files),
				"bus_dropped", sub.Dropped())
		}
	}
}

func (w *Writer) writeMessage(msg bus.Message) {
	// Determine partition path from topic.
	// ohlc.binance.BTCUSDT → type=ohlc/exchange=binance/instrument=BTCUSDT/2026/03/29/data.jsonl
	// news → type=news/2026/03/29/data.jsonl
	parts := strings.SplitN(msg.Topic, ".", 3)

	var partPath string
	switch len(parts) {
	case 3:
		partPath = filepath.Join(
			fmt.Sprintf("type=%s", parts[0]),
			fmt.Sprintf("exchange=%s", parts[1]),
			fmt.Sprintf("instrument=%s", parts[2]),
		)
	case 2:
		partPath = filepath.Join(
			fmt.Sprintf("type=%s", parts[0]),
			fmt.Sprintf("source=%s", parts[1]),
		)
	default:
		partPath = fmt.Sprintf("type=%s", msg.Topic)
	}

	now := time.Now()
	var datePart string
	if w.rotation == "hourly" {
		datePart = filepath.Join(
			fmt.Sprintf("year=%d", now.Year()),
			fmt.Sprintf("month=%02d", now.Month()),
			fmt.Sprintf("day=%02d", now.Day()),
			fmt.Sprintf("hour=%02d", now.Hour()),
		)
	} else {
		datePart = filepath.Join(
			fmt.Sprintf("year=%d", now.Year()),
			fmt.Sprintf("month=%02d", now.Month()),
			fmt.Sprintf("day=%02d", now.Day()),
		)
	}

	filePath := filepath.Join(w.basePath, partPath, datePart, "data.jsonl")

	// Build the record: topic + timestamp + payload. Payload is encoded
	// via marshalPayload so proto messages land in canonical protojson.
	payloadJSON, err := marshalPayload(msg.Payload)
	if err != nil {
		slog.Debug("datalake marshal error", "topic", msg.Topic, "error", err)
		return
	}
	// Build the envelope with a pre-marshalled payload so we don't
	// double-encode.
	envelope := struct {
		Topic     string          `json:"_topic"`
		Timestamp string          `json:"_timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}{
		Topic:     msg.Topic,
		Timestamp: now.Format(time.RFC3339Nano),
		Payload:   payloadJSON,
	}
	line, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	line = append(line, '\n')

	// Write to file (append-only).
	f, err := w.getFile(filePath)
	if err != nil {
		slog.Debug("datalake write error", "path", filePath, "error", err)
		return
	}

	w.mu.Lock()
	if _, err := f.Write(line); err != nil {
		slog.Debug("datalake append error", "path", filePath, "error", err)
	}
	w.mu.Unlock()
	w.written.Add(1)
}

func (w *Writer) getFile(path string) (*os.File, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if f, ok := w.files[path]; ok {
		return f, nil
	}

	// Create directory.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	w.files[path] = f

	// Prune old file handles (keep max 100 open).
	if len(w.files) > 100 {
		for k, old := range w.files {
			if k != path {
				old.Close()
				delete(w.files, k)
				break
			}
		}
	}

	return f, nil
}

func (w *Writer) closeAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, f := range w.files {
		f.Close()
	}
	w.files = make(map[string]*os.File)
}
