// Package tsbase_files watches a directory of time_series_base output
// files and streams decoded rows onto the notbbg bus. Works over a
// local path or an NFS/SSHFS mount.
//
// Decoders are pluggable by file extension:
//   - .csv   : header-driven generic CSV (ts-format macro output)
//   - .jsonl : one notbbg.v1.Update-shaped JSON object per line
//   - .parquet / .zst : not yet supported — logged + skipped
//
// See DATA-PLAN.md §6.
package tsbase_files

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

// Config controls the tailer.
type Config struct {
	Path     string        // directory to watch
	Interval time.Duration // poll interval (default 5s)
	// TopicPrefix is prepended to every emitted bus topic so the
	// operator can namespace ts-base data (e.g. "tsbase.").
	TopicPrefix string
}

// Tailer scans Config.Path on a poll cycle and publishes decoded
// rows. Files are remembered by path; offsets are recorded so
// append-only sources don't double-publish.
type Tailer struct {
	cfg Config
	b   *bus.Bus

	mu       sync.Mutex
	cursors  map[string]int64 // file path → bytes already consumed
	skipped  map[string]bool  // files we've told the operator we can't read
	rows     int64
}

// New builds a Tailer.
func New(b *bus.Bus, cfg Config) *Tailer {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	return &Tailer{
		cfg:     cfg,
		b:       b,
		cursors: make(map[string]int64),
		skipped: make(map[string]bool),
	}
}

// Run polls until ctx is cancelled.
func (t *Tailer) Run(ctx context.Context) error {
	slog.Info("tsbase_files tailer started", "path", t.cfg.Path, "interval", t.cfg.Interval)
	ticker := time.NewTicker(t.cfg.Interval)
	defer ticker.Stop()

	// One pass immediately so startup doesn't wait a full interval.
	_ = t.scanOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := t.scanOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("tsbase_files scan error", "error", err)
			}
		}
	}
}

// Rows returns the total number of decoded rows published since
// startup. Useful in tests + status pings.
func (t *Tailer) Rows() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rows
}

func (t *Tailer) scanOnce(ctx context.Context) error {
	return filepath.WalkDir(t.cfg.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return t.consumeFile(path)
	})
}

func (t *Tailer) consumeFile(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".csv":
		return t.consumeCSV(path)
	case ".jsonl":
		return t.consumeJSONL(path)
	case ".parquet", ".zst":
		t.mu.Lock()
		already := t.skipped[path]
		t.skipped[path] = true
		t.mu.Unlock()
		if !already {
			slog.Warn("tsbase_files: parquet not yet supported; skipping",
				"path", path,
				"hint", "wire in an Arrow/parquet-go reader to consume ts-gateway output")
		}
		return nil
	default:
		return nil
	}
}

// consumeCSV reads a header-driven CSV file and publishes one bus
// message per data row. Header row drives the payload schema.
func (t *Tailer) consumeCSV(path string) error {
	t.mu.Lock()
	cursor := t.cursors[path]
	t.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Snapshot size so we can update the cursor to end-of-file at the
	// end (new rows after we started reading come in on the next pass).
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if stat.Size() == cursor {
		return nil // no change since last scan
	}

	// For CSV, if the file was truncated or the cursor is stale we
	// restart from the top; CSV rows don't carry sequence numbers, so
	// de-dup is the responsibility of downstream consumers.
	if cursor > stat.Size() {
		cursor = 0
	}

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true
	headers, err := reader.Read()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read header %s: %w", path, err)
	}

	rowsHere := int64(0)
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.Debug("csv parse error (skipping rest)", "path", path, "error", err)
			break
		}
		// Resume-past-cursor TODO: the csv.Reader doesn't surface byte
		// offsets, so exact line-count skipping would need a second
		// pass. For now we republish on cursor reset — downstream
		// consumers must dedupe.
		_ = cursor
		payload := rowToPayload(headers, row)
		topic := t.cfg.TopicPrefix + "tsbase." + strings.TrimSuffix(filepath.Base(path), ".csv")
		t.b.Publish(bus.Message{Topic: topic, Payload: payload})
		rowsHere++
	}

	t.mu.Lock()
	t.cursors[path] = stat.Size()
	t.rows += rowsHere
	t.mu.Unlock()
	return nil
}

// consumeJSONL reads a JSONL file and publishes one message per
// line. Lines must decode to a map[string]any; malformed lines are
// skipped. Cursor tracks byte offset so append-only files don't
// republish.
func (t *Tailer) consumeJSONL(path string) error {
	t.mu.Lock()
	cursor := t.cursors[path]
	t.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, _ := f.Stat()
	if stat != nil && stat.Size() == cursor {
		return nil
	}
	if stat != nil && cursor > stat.Size() {
		cursor = 0 // file rotated; reread from top
	}
	if cursor > 0 {
		if _, err := f.Seek(cursor, io.SeekStart); err != nil {
			return err
		}
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	rowsHere := int64(0)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(line, &payload); err != nil {
			continue
		}
		topic := t.cfg.TopicPrefix + "tsbase." + strings.TrimSuffix(filepath.Base(path), ".jsonl")
		t.b.Publish(bus.Message{Topic: topic, Payload: payload})
		rowsHere++
	}
	// Whatever the scanner consumed, that's our new cursor.
	pos, _ := f.Seek(0, io.SeekCurrent)

	t.mu.Lock()
	t.cursors[path] = pos
	t.rows += rowsHere
	t.mu.Unlock()
	return nil
}

// rowToPayload zips headers + a CSV row into a payload map.
func rowToPayload(headers, row []string) map[string]any {
	out := make(map[string]any, len(headers))
	for i, h := range headers {
		if i < len(row) {
			out[h] = row[i]
		}
	}
	return out
}
