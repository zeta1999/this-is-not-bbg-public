// Package datalake provides append-only data persistence in Hive-partitioned
// folder structures. All bus messages matching configured topics are written
// to JSONL files organized by type/exchange/instrument/date.
package datalake

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/persistq"
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

// extractEventTime pulls the canonical event time out of a
// marshalled payload. Tries common field names (Timestamp,
// timestamp, OpenTime). When none is present or parses, falls
// back to fallbackNow — typically the WAL ingest instant.
//
// This exists because the WAL envelope's _timestamp is what
// DataRange queries filter on; making it the event time (bar
// open, trade fill) instead of the ingest time means a backfill
// replay of historical bars lands at the correct point in the
// time window, not all bunched at "now".
func extractEventTime(payloadJSON []byte, fallback time.Time) time.Time {
	var probe struct {
		Timestamp1 string `json:"Timestamp"`
		Timestamp2 string `json:"timestamp"`
		OpenTime   string `json:"open_time"`
	}
	if err := json.Unmarshal(payloadJSON, &probe); err != nil {
		return fallback
	}
	for _, raw := range []string{probe.Timestamp1, probe.Timestamp2, probe.OpenTime} {
		if raw == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil && !t.IsZero() {
			return t
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil && !t.IsZero() {
			return t
		}
	}
	return fallback
}

// Config for the datalake writer.
type Config struct {
	Path     string   `yaml:"path"`     // root directory (e.g. "/data/notbbg")
	Enabled  bool     `yaml:"enabled"`
	Topics   []string `yaml:"topics"`   // glob patterns to capture (default: all)
	Format   string   `yaml:"format"`   // "jsonl" (default) or "csv"
	Rotation string   `yaml:"rotation"` // "daily" (default) or "hourly"

	// Compression controls zstd compression on write. Empty or "none"
	// leaves payloads as plain .jsonl. "zstd" compresses matching
	// topics and writes them to .jsonl.zst. The reader transparently
	// handles both suffixes.
	Compression    string   `yaml:"compression"`
	CompressTopics []string `yaml:"compress_topics"` // glob patterns; empty = all topics compressed when compression enabled
}

// openFile tracks one live file handle + optional zstd encoder. The
// encoder wraps the raw *os.File when compression is enabled for the
// matching topic. Close() flushes the encoder (writing the final
// zstd frame) before closing the underlying file.
type openFile struct {
	f   *os.File
	enc *zstd.Encoder // nil for plain .jsonl
}

func (of *openFile) writer() io.Writer {
	if of.enc != nil {
		return of.enc
	}
	return of.f
}

func (of *openFile) Close() {
	if of.enc != nil {
		_ = of.enc.Close() // flushes final frame
	}
	_ = of.f.Close()
}

// WALStats is the payload published on `wal.datalake.stats` so the
// MON panel can surface WAL depth and drop counts without scraping
// logs. Mirrors cache.WALStats but tracks `Written` (total lines
// appended) instead of `Enqueued` — datalake increments the counter
// only once a line has been handed to the file handle.
type WALStats struct {
	Name        string `json:"name"`
	Written     int64  `json:"written"`
	BusDropped  uint64 `json:"bus_dropped"`
	WALDropped  uint64 `json:"wal_dropped"`
	BytesOnDisk int64  `json:"bytes_on_disk"`
	Segments    int    `json:"segments"`
	Enabled     bool   `json:"enabled"`
}

// Writer subscribes to the bus and appends all matching messages to disk.
type Writer struct {
	bus         *bus.Bus
	basePath    string
	topics      []string
	rotation    string
	compression string   // "" / "none" / "zstd"
	compressPat []string // glob patterns (empty = all topics when compression enabled)

	walDir   string
	walBytes int64

	mu      sync.Mutex
	files   map[string]*openFile // keyed by resolved file path
	written atomic.Int64
}

// EnableWAL configures the writer to route traffic through a
// disk-spill WAL rooted at dir with the given byte budget. When set,
// the bus subscriber drains into the WAL (never blocks on file I/O)
// and a separate drainer goroutine flushes the WAL into the
// partitioned JSONL files.
func (w *Writer) EnableWAL(dir string, capBytes int64) *Writer {
	w.walDir = dir
	w.walBytes = capBytes
	return w
}

// New creates a datalake writer.
func New(b *bus.Bus, cfg Config) *Writer {
	topics := cfg.Topics
	if len(topics) == 0 {
		topics = []string{"ohlc.*.*", "ohlc-historical.*.*", "lob.*.*", "trade.*.*", "news", "perp.*.*", "liquidation.*.*", "indicator.*"}
	}
	rotation := cfg.Rotation
	if rotation == "" {
		rotation = "daily"
	}
	compression := strings.ToLower(cfg.Compression)
	if compression == "none" {
		compression = ""
	}
	return &Writer{
		bus:         b,
		basePath:    cfg.Path,
		topics:      topics,
		rotation:    rotation,
		compression: compression,
		compressPat: cfg.CompressTopics,
		files:       make(map[string]*openFile),
	}
}

// shouldCompress reports whether the given topic should land in a
// .jsonl.zst file under the current config. Empty compressPat means
// "all topics compressed when compression is on"; otherwise any
// pattern match selects the topic.
func (w *Writer) shouldCompress(topic string) bool {
	if w.compression != "zstd" {
		return false
	}
	if len(w.compressPat) == 0 {
		return true
	}
	for _, pat := range w.compressPat {
		if matchTopic(pat, topic) {
			return true
		}
	}
	return false
}

// matchTopic does a lightweight glob match: `*` is any segment,
// segments split by `.`. Same semantics as the bus subscribe
// patterns.
func matchTopic(pattern, topic string) bool {
	pp := strings.Split(pattern, ".")
	tt := strings.Split(topic, ".")
	if len(pp) != len(tt) {
		return false
	}
	for i := range pp {
		if pp[i] == "*" {
			continue
		}
		if pp[i] != tt[i] {
			return false
		}
	}
	return true
}

// Run starts the datalake writer. Blocks until ctx is cancelled.
func (w *Writer) Run(ctx context.Context) error {
	sub := w.bus.Subscribe(8192, w.topics...)
	defer w.bus.Unsubscribe(sub)
	defer w.closeAll()

	slog.Info("datalake writer started",
		"path", w.basePath, "topics", w.topics, "wal", w.walDir != "")

	if w.walDir != "" {
		return w.runWAL(ctx, sub)
	}
	return w.runDirect(ctx, sub)
}

// runDirect is the pre-Phase-5 behavior: partition + append inline.
func (w *Writer) runDirect(ctx context.Context, sub *bus.Subscriber) error {
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

// runWAL splits into producer (bus → WAL) + drainer (WAL → files).
// The WAL absorbs fsync / slow-NAS stalls up to its byte budget so
// the bus subscriber never blocks and the publisher can't drop a
// message that fits within the budget.
func (w *Writer) runWAL(ctx context.Context, sub *bus.Subscriber) error {
	q, err := persistq.Open(persistq.Options{
		Dir:      w.walDir,
		CapBytes: w.walBytes,
	})
	if err != nil {
		return fmt.Errorf("datalake writer: open WAL: %w", err)
	}
	defer q.Close()

	drainerCtx, cancelDrainer := context.WithCancel(context.Background())
	drainerDone := make(chan struct{})
	go func() {
		defer close(drainerDone)
		w.drainWAL(drainerCtx, q)
	}()

	var walDropped uint64
	statsTicker := time.NewTicker(60 * time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Allow the drainer up to 2s to flush; then cancel.
			shutdown := time.NewTimer(2 * time.Second)
			defer shutdown.Stop()
			select {
			case <-shutdown.C:
			case <-drainerDone:
			}
			cancelDrainer()
			<-drainerDone
			slog.Info("datalake writer stopped",
				"written", w.written.Load(),
				"bus_dropped", sub.Dropped(),
				"wal_dropped", walDropped)
			return nil

		case msg, ok := <-sub.C:
			if !ok {
				cancelDrainer()
				<-drainerDone
				return nil
			}
			path, line, ok := w.buildLine(msg)
			if !ok {
				continue
			}
			frame := encodeWALFrame(path, line)
			if err := q.Enqueue(frame); err != nil {
				if errors.Is(err, persistq.ErrFull) {
					walDropped++
					continue
				}
				slog.Debug("datalake WAL enqueue error", "error", err)
			}

		case <-statsTicker.C:
			st := q.Stats()
			busDropped := sub.Dropped()
			slog.Info("datalake stats (WAL)",
				"written", w.written.Load(),
				"open_files", len(w.files),
				"bus_dropped", busDropped,
				"wal_dropped", walDropped,
				"wal_bytes", st.BytesOnDisk,
				"wal_segments", st.Segments)
			w.bus.Publish(bus.Message{
				Topic: "wal.datalake.stats",
				Payload: WALStats{
					Name:        "datalake",
					Written:     w.written.Load(),
					BusDropped:  busDropped,
					WALDropped:  walDropped,
					BytesOnDisk: st.BytesOnDisk,
					Segments:    st.Segments,
					Enabled:     true,
				},
			})
		}
	}
}

// drainWAL reads frames from the WAL and appends them to the right
// partitioned file. Pure I/O — the file path was already resolved by
// buildLine at producer time.
func (w *Writer) drainWAL(ctx context.Context, q *persistq.Queue) {
	for {
		payload, err := q.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, persistq.ErrClosed) {
				return
			}
			slog.Debug("datalake WAL dequeue error", "error", err)
			continue
		}
		path, line, ok := decodeWALFrame(payload)
		if !ok {
			slog.Debug("datalake WAL bad frame")
			continue
		}
		w.appendLine(path, line)
	}
}

// encodeWALFrame packs (path, line) into a single record body.
// Layout: [uint16 LE pathLen][path bytes][line bytes]. Line is
// newline-terminated JSONL and is never modified.
func encodeWALFrame(path string, line []byte) []byte {
	out := make([]byte, 2+len(path)+len(line))
	binary.LittleEndian.PutUint16(out[:2], uint16(len(path)))
	copy(out[2:2+len(path)], path)
	copy(out[2+len(path):], line)
	return out
}

func decodeWALFrame(buf []byte) (string, []byte, bool) {
	if len(buf) < 2 {
		return "", nil, false
	}
	pathLen := int(binary.LittleEndian.Uint16(buf[:2]))
	if len(buf) < 2+pathLen {
		return "", nil, false
	}
	return string(buf[2 : 2+pathLen]), buf[2+pathLen:], true
}

// buildLine resolves a message's target file path and renders the
// JSONL line. Split out so both the direct and WAL paths share the
// same routing + encoding logic.
func (w *Writer) buildLine(msg bus.Message) (string, []byte, bool) {
	// Historical replays land here under `ohlc-historical.<ex>.<ins>`
	// but should persist into the same `type=ohlc` partition as live
	// data. Otherwise DataRange queries against `topic=ohlc.X.Y`
	// would miss them. Normalize before split.
	topic := msg.Topic
	if strings.HasPrefix(topic, "ohlc-historical.") {
		topic = "ohlc." + strings.TrimPrefix(topic, "ohlc-historical.")
		msg.Topic = topic
	}
	parts := strings.SplitN(topic, ".", 3)

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

	payloadJSON, err := marshalPayload(msg.Payload)
	if err != nil {
		slog.Debug("datalake marshal error", "topic", msg.Topic, "error", err)
		return "", nil, false
	}
	// _timestamp must be the *event* time so DataRange queries
	// over a time window return what the trader expects. Backfill
	// replays publish events from the past with their original
	// time on the payload; using "now" here would lump them all
	// at the ingest instant and break any time-range query.
	// extractEventTime tries the payload's canonical time field
	// (Timestamp for OHLC/Trade/etc.); falls back to "now" only
	// when the payload is anonymous or has no time at all.
	now := time.Now()
	envelopeTs := extractEventTime(payloadJSON, now)
	// Partition by EVENT time too — DataRange's reader iterates
	// `year=Y/month=M/day=D/` directories between [from, to] and
	// reads each matching file. If we partitioned by *write* time,
	// a year-old historical bar written today would land in
	// today's directory, and a `Load 24h` query against
	// `[yesterday, today]` would find it (and show last year's
	// price as if it were 24 h old). Bug #29 from the OHLC pipeline
	// stack. extractEventTime above already canonicalises the bar
	// time; reuse it here so envelope and partition agree.
	partTime := envelopeTs
	if partTime.IsZero() {
		partTime = now
	}
	var datePart string
	if w.rotation == "hourly" {
		datePart = filepath.Join(
			fmt.Sprintf("year=%d", partTime.Year()),
			fmt.Sprintf("month=%02d", partTime.Month()),
			fmt.Sprintf("day=%02d", partTime.Day()),
			fmt.Sprintf("hour=%02d", partTime.Hour()),
		)
	} else {
		datePart = filepath.Join(
			fmt.Sprintf("year=%d", partTime.Year()),
			fmt.Sprintf("month=%02d", partTime.Month()),
			fmt.Sprintf("day=%02d", partTime.Day()),
		)
	}

	fileName := "data.jsonl"
	if w.shouldCompress(msg.Topic) {
		fileName = "data.jsonl.zst"
	}
	filePath := filepath.Join(w.basePath, partPath, datePart, fileName)
	envelope := struct {
		Topic     string          `json:"_topic"`
		Timestamp string          `json:"_timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}{
		Topic:     msg.Topic,
		Timestamp: envelopeTs.Format(time.RFC3339Nano),
		Payload:   payloadJSON,
	}
	line, err := json.Marshal(envelope)
	if err != nil {
		return "", nil, false
	}
	line = append(line, '\n')
	return filePath, line, true
}

// appendLine opens the target file (lazily) and appends one record.
// Direct-mode callers supply a freshly-built line; WAL-drainer
// callers replay a line that was built at producer time.
func (w *Writer) appendLine(filePath string, line []byte) {
	of, err := w.getFile(filePath)
	if err != nil {
		slog.Debug("datalake write error", "path", filePath, "error", err)
		return
	}
	w.mu.Lock()
	if _, err := of.writer().Write(line); err != nil {
		slog.Debug("datalake append error", "path", filePath, "error", err)
	}
	w.mu.Unlock()
	w.written.Add(1)
}

// writeMessage is the direct-mode entry point: build the line and
// append to its target file.
func (w *Writer) writeMessage(msg bus.Message) {
	path, line, ok := w.buildLine(msg)
	if !ok {
		return
	}
	w.appendLine(path, line)
}

func (w *Writer) getFile(path string) (*openFile, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if of, ok := w.files[path]; ok {
		return of, nil
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

	of := &openFile{f: f}
	if strings.HasSuffix(path, ".zst") {
		// zstd on top of an append-opened file: each open writes a new
		// frame (or a continuation frame on rotation). The reader
		// concatenates frames transparently.
		enc, encErr := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if encErr != nil {
			_ = f.Close()
			return nil, fmt.Errorf("zstd encoder %s: %w", path, encErr)
		}
		of.enc = enc
	}

	w.files[path] = of

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

	return of, nil
}

func (w *Writer) closeAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, of := range w.files {
		of.Close()
	}
	w.files = make(map[string]*openFile)
}
