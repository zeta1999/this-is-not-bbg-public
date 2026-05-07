// Package persistq implements a bounded on-disk append-only queue
// used as a write-ahead log between the bus and durable writers
// (BBolt cache, datalake JSONL). It absorbs short bursts of slow
// downstream I/O so the bus subscriber never blocks and the writer
// never silently drops a message that fits within the WAL budget.
//
// Semantics:
//
//   - Enqueue appends a record; returns ErrFull when the total
//     on-disk byte budget would be exceeded. Callers count drops
//     and surface them; the WAL itself is otherwise best-effort.
//   - Dequeue blocks until a record is available (or Close is
//     called), returning records in enqueue order.
//   - Records are framed with a 4-byte little-endian length prefix.
//   - Storage is a directory of segment files (`NNNNNNNN.seg`,
//     monotonically increasing IDs) plus a `cursor` metadata file
//     recording the reader's (segment, offset). Dropping old data
//     under cap pressure deletes the oldest fully-drained segment.
//   - Reopening a queue on the same directory resumes from the
//     persisted cursor. Segments strictly before the cursor's
//     segment are garbage-collected on open.
//
// Crash safety: segments are append-only; a torn write at the tail
// is detected on Dequeue (length prefix points past EOF) and
// terminates iteration for that segment. The cursor file is written
// with a small rename-after-write dance so a half-written cursor
// never survives. We do NOT fsync on every Enqueue — the queue is
// explicitly a best-effort buffer, not a durability substitute.
package persistq

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
)

// Default sizes — chosen to match the backpressure proposal §6.4 +
// decision log row 2 (512 MB prod, 128 MB dev). Segment size picks
// a round 50 MB so a full queue occupies ~10 segments — small enough
// that drop-oldest operates with reasonable granularity.
const (
	DefaultCapBytes     int64 = 512 * 1024 * 1024
	DefaultSegmentBytes int64 = 50 * 1024 * 1024
	headerBytes               = 4
	cursorFileName            = "cursor"
	segmentSuffix             = ".seg"
)

// ErrFull is returned by Enqueue when appending the record would
// push the total on-disk size past the queue's byte budget.
var ErrFull = errors.New("persistq: queue full")

// ErrClosed is returned by Enqueue/Dequeue after Close has been called.
var ErrClosed = errors.New("persistq: closed")

// Options configures Open.
type Options struct {
	// Dir is the directory where segment + cursor files live.
	// Created (0o755) if it does not exist.
	Dir string

	// CapBytes is the maximum on-disk byte budget. When an Enqueue
	// would exceed it, the oldest already-drained segment is
	// deleted. If no drained segments exist (all data still
	// unread), Enqueue returns ErrFull and the caller increments
	// a drop counter. Defaults to DefaultCapBytes.
	CapBytes int64

	// SegmentBytes is the target size at which the active segment
	// is rotated. Defaults to DefaultSegmentBytes.
	SegmentBytes int64
}

// Queue is a bounded disk-backed append-only queue with a single
// producer and single consumer. Enqueue and Dequeue may be called
// concurrently from different goroutines; each method is
// self-serialized.
type Queue struct {
	dir          string
	capBytes     int64
	segmentBytes int64

	mu       sync.Mutex
	closed   bool
	notify   chan struct{} // size 1 signal; writer->reader

	// segments holds the sorted list of segment IDs that currently
	// exist on disk, oldest first. The last element is the active
	// write segment.
	segments []uint32
	active   *os.File
	activeSz int64

	// cursor — reader position (segment, offset-into-that-segment
	// measured from start of file in bytes). cursorSeg is the
	// minimum element of segments when non-empty.
	cursorSeg uint32
	cursorOff int64

	// readHandle is the currently open read segment file; lazily
	// opened by Dequeue and reopened across rotations.
	readHandle *os.File

	dropped atomic.Uint64
	enqueued atomic.Uint64
	dequeued atomic.Uint64
}

// Open prepares a Queue rooted at opts.Dir, creating the directory
// if needed and recovering state from any existing segments +
// cursor file.
func Open(opts Options) (*Queue, error) {
	if opts.CapBytes <= 0 {
		opts.CapBytes = DefaultCapBytes
	}
	if opts.SegmentBytes <= 0 {
		opts.SegmentBytes = DefaultSegmentBytes
	}
	if opts.SegmentBytes > opts.CapBytes {
		opts.SegmentBytes = opts.CapBytes
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("persistq: mkdir %s: %w", opts.Dir, err)
	}

	q := &Queue{
		dir:          opts.Dir,
		capBytes:     opts.CapBytes,
		segmentBytes: opts.SegmentBytes,
		notify:       make(chan struct{}, 1),
	}

	if err := q.scanSegments(); err != nil {
		return nil, err
	}
	if err := q.loadCursor(); err != nil {
		return nil, err
	}
	q.gcSegmentsBeforeCursor()

	if len(q.segments) == 0 {
		if err := q.startNewSegmentLocked(0); err != nil {
			return nil, err
		}
	} else {
		id := q.segments[len(q.segments)-1]
		f, err := os.OpenFile(q.segmentPath(id), os.O_RDWR, 0o644)
		if err != nil {
			return nil, fmt.Errorf("persistq: open active: %w", err)
		}
		sz, err := f.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, err
		}
		q.active = f
		q.activeSz = sz
	}
	return q, nil
}

// Enqueue appends payload to the queue. Returns ErrFull if the
// queue's byte budget would be exceeded; returns ErrClosed after Close.
func (q *Queue) Enqueue(payload []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrClosed
	}

	recordSize := int64(headerBytes + len(payload))
	if recordSize > q.segmentBytes {
		// Single record larger than a segment — refuse; this is an
		// API misuse, not a capacity condition.
		return fmt.Errorf("persistq: record size %d exceeds segment %d", recordSize, q.segmentBytes)
	}

	// Rotate segment if it wouldn't fit.
	if q.activeSz+recordSize > q.segmentBytes {
		nextID := q.segments[len(q.segments)-1] + 1
		if err := q.rotateLocked(nextID); err != nil {
			return err
		}
	}

	// Enforce capacity by dropping fully-drained oldest segments.
	for q.totalBytesLocked()+recordSize > q.capBytes {
		if !q.dropOldestDrainedLocked() {
			// Nothing drained — bump drop counter and bail.
			q.dropped.Add(1)
			return ErrFull
		}
	}

	var hdr [headerBytes]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := q.active.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := q.active.Write(payload); err != nil {
		return err
	}
	q.activeSz += recordSize
	q.enqueued.Add(1)

	select {
	case q.notify <- struct{}{}:
	default:
	}
	return nil
}

// Dequeue blocks until a record is available and returns it. Returns
// ErrClosed after Close, or ctx.Err() if the context is cancelled
// while waiting.
func (q *Queue) Dequeue(ctx context.Context) ([]byte, error) {
	for {
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			return nil, ErrClosed
		}
		data, err := q.tryReadLocked()
		if err != nil {
			q.mu.Unlock()
			return nil, err
		}
		if data != nil {
			q.persistCursorLocked()
			q.mu.Unlock()
			q.dequeued.Add(1)
			return data, nil
		}
		q.mu.Unlock()

		select {
		case <-q.notify:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Stats returns a snapshot of the queue's counters.
type Stats struct {
	Enqueued  uint64
	Dequeued  uint64
	Dropped   uint64
	BytesOnDisk int64
	Segments  int
}

// Stats returns a snapshot of current counters and sizing.
func (q *Queue) Stats() Stats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return Stats{
		Enqueued:    q.enqueued.Load(),
		Dequeued:    q.dequeued.Load(),
		Dropped:     q.dropped.Load(),
		BytesOnDisk: q.totalBytesLocked(),
		Segments:    len(q.segments),
	}
}

// Close flushes metadata and releases handles. Blocked Dequeue
// callers return ErrClosed. Safe to call multiple times.
func (q *Queue) Close() error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true
	close(q.notify)
	q.persistCursorLocked()
	if q.active != nil {
		_ = q.active.Close()
		q.active = nil
	}
	if q.readHandle != nil {
		_ = q.readHandle.Close()
		q.readHandle = nil
	}
	q.mu.Unlock()
	return nil
}

// --- internal helpers ---

func (q *Queue) segmentPath(id uint32) string {
	return filepath.Join(q.dir, fmt.Sprintf("%08d%s", id, segmentSuffix))
}

func (q *Queue) cursorPath() string {
	return filepath.Join(q.dir, cursorFileName)
}

func (q *Queue) scanSegments() error {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return fmt.Errorf("persistq: readdir: %w", err)
	}
	q.segments = q.segments[:0]
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != segmentSuffix {
			continue
		}
		base := name[:len(name)-len(segmentSuffix)]
		n, err := strconv.ParseUint(base, 10, 32)
		if err != nil {
			continue
		}
		q.segments = append(q.segments, uint32(n))
	}
	sort.Slice(q.segments, func(i, j int) bool { return q.segments[i] < q.segments[j] })
	return nil
}

func (q *Queue) loadCursor() error {
	data, err := os.ReadFile(q.cursorPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if len(q.segments) > 0 {
				q.cursorSeg = q.segments[0]
			}
			return nil
		}
		return fmt.Errorf("persistq: read cursor: %w", err)
	}
	var segID uint32
	var off int64
	if _, err := fmt.Sscanf(string(data), "%d %d", &segID, &off); err != nil {
		return fmt.Errorf("persistq: parse cursor: %w", err)
	}
	q.cursorSeg = segID
	q.cursorOff = off
	return nil
}

func (q *Queue) persistCursorLocked() {
	tmp := q.cursorPath() + ".tmp"
	data := []byte(fmt.Sprintf("%d %d\n", q.cursorSeg, q.cursorOff))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, q.cursorPath())
}

func (q *Queue) gcSegmentsBeforeCursor() {
	kept := q.segments[:0]
	for _, id := range q.segments {
		if id < q.cursorSeg {
			_ = os.Remove(q.segmentPath(id))
			continue
		}
		kept = append(kept, id)
	}
	q.segments = kept
}

func (q *Queue) startNewSegmentLocked(id uint32) error {
	f, err := os.OpenFile(q.segmentPath(id), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("persistq: create segment %d: %w", id, err)
	}
	q.active = f
	q.activeSz = 0
	q.segments = append(q.segments, id)
	if len(q.segments) == 1 {
		q.cursorSeg = id
		q.cursorOff = 0
	}
	return nil
}

func (q *Queue) rotateLocked(nextID uint32) error {
	if err := q.active.Sync(); err != nil {
		return err
	}
	if err := q.active.Close(); err != nil {
		return err
	}
	q.active = nil
	return q.startNewSegmentLocked(nextID)
}

func (q *Queue) totalBytesLocked() int64 {
	var total int64
	for _, id := range q.segments {
		if id == q.segments[len(q.segments)-1] {
			total += q.activeSz
			continue
		}
		fi, err := os.Stat(q.segmentPath(id))
		if err != nil {
			continue
		}
		total += fi.Size()
	}
	return total
}

// dropOldestDrainedLocked deletes the oldest segment if it is fully
// behind the reader cursor. Returns true if a segment was dropped.
func (q *Queue) dropOldestDrainedLocked() bool {
	if len(q.segments) < 2 {
		return false
	}
	oldest := q.segments[0]
	// Only safe to drop if the reader has moved past this segment.
	if oldest >= q.cursorSeg {
		return false
	}
	_ = os.Remove(q.segmentPath(oldest))
	q.segments = q.segments[1:]
	return true
}

// tryReadLocked attempts to read one record from the cursor segment
// without blocking. Returns (data, nil) on success, (nil, nil) when
// the queue is empty, or (nil, err) on a real I/O error.
func (q *Queue) tryReadLocked() ([]byte, error) {
	for {
		if len(q.segments) == 0 {
			return nil, nil
		}
		// Ensure the read handle points at cursorSeg.
		if q.readHandle == nil {
			f, err := os.Open(q.segmentPath(q.cursorSeg))
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					// Segment was GC'd under us; advance cursor.
					if !q.advanceCursorToNextLocked() {
						return nil, nil
					}
					continue
				}
				return nil, err
			}
			q.readHandle = f
		}

		if _, err := q.readHandle.Seek(q.cursorOff, io.SeekStart); err != nil {
			return nil, err
		}

		var hdr [headerBytes]byte
		n, err := io.ReadFull(q.readHandle, hdr[:])
		if err == io.EOF || (err == io.ErrUnexpectedEOF && n < headerBytes) {
			// End of this segment — advance to next if it exists.
			if !q.advanceCursorToNextLocked() {
				return nil, nil
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		size := int64(binary.LittleEndian.Uint32(hdr[:]))
		payload := make([]byte, size)
		_, err = io.ReadFull(q.readHandle, payload)
		if err != nil {
			// Torn write — stop here, wait for more data.
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil, nil
			}
			return nil, err
		}
		q.cursorOff += int64(headerBytes) + size
		return payload, nil
	}
}

// advanceCursorToNextLocked moves the cursor to the next segment
// (after cursorSeg). Returns true if advanced, false if cursorSeg is
// already at the active tail. Closes the current read handle.
func (q *Queue) advanceCursorToNextLocked() bool {
	var idx = -1
	for i, id := range q.segments {
		if id == q.cursorSeg {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(q.segments) {
		return false
	}
	if q.readHandle != nil {
		_ = q.readHandle.Close()
		q.readHandle = nil
	}
	q.cursorSeg = q.segments[idx+1]
	q.cursorOff = 0
	return true
}
