// Package daterange implements server-side streaming of historical
// data windows (GetDataRangeRequest → stream GetDataRangeResponse).
// See DATA-PLAN.md §3.
//
// The types mirror proto/notbbg/v1/server.proto's GetDataRangeRequest,
// GetDataRangeResponse, DataRangeError. grpc.go adapts between the
// Go types here and the regenerated proto types.
package daterange

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/notbbg/notbbg/server/internal/datalake"
)

// Request mirrors notbbg.v1.GetDataRangeRequest.
type Request struct {
	Topic         string
	From          time.Time
	To            time.Time
	Resolution    string
	CorrelationID string
	MaxRecords    int
}

// Chunk mirrors notbbg.v1.GetDataRangeResponse.
type Chunk struct {
	CorrelationID string
	Records       []Record
	Seq           int32
	EOF           bool
}

// Record is one historical record. It holds the raw topic +
// timestamp + payload bytes so any proto type (OHLC, Trade, LOB, ...)
// can be carried without the daterange package pulling the whole
// proto surface.
type Record struct {
	Topic     string          `json:"_topic"`
	Timestamp string          `json:"_timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// Error mirrors notbbg.v1.DataRangeError. It is sent before the
// server closes the stream when something goes wrong mid-query.
type Error struct {
	CorrelationID string
	Reason        string
}

// defaults
const (
	defaultMaxRecords  = 5000
	defaultChunkSize   = 500 // records per chunk sent to the client
	maxReasonableLimit = 100_000
)

// Handler answers DataRangeRequests. Cache lookups are TODO; today
// every request falls through to the datalake reader. The interface
// is kept narrow so callers can swap in a fake.
type Handler struct {
	reader *datalake.Reader
}

// New builds a Handler backed by a datalake root.
func New(datalakePath string) *Handler {
	return &Handler{reader: datalake.NewReader(datalakePath)}
}

// Sink receives streamed chunks. Return an error to abort the stream.
type Sink func(Chunk) error

// Serve answers one DataRangeRequest. It calls sink once per chunk
// plus one final chunk with EOF=true. Cancellation via ctx stops
// streaming as soon as the current chunk returns.
func (h *Handler) Serve(ctx context.Context, req Request, sink Sink) error {
	if err := req.validate(); err != nil {
		return err
	}

	dt, exchange, instrument, err := parseTopic(req.Topic)
	if err != nil {
		return err
	}

	limit := req.MaxRecords
	if limit <= 0 || limit > maxReasonableLimit {
		limit = defaultMaxRecords
	}

	records, err := h.reader.Query(dt, exchange, instrument, req.From, req.To, limit)
	if err != nil {
		return fmt.Errorf("datalake query: %w", err)
	}

	var seq int32
	for i := 0; i < len(records); i += defaultChunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := i + defaultChunkSize
		if end > len(records) {
			end = len(records)
		}

		chunk := Chunk{
			CorrelationID: req.CorrelationID,
			Records:       recordsFromDatalake(records[i:end]),
			Seq:           seq,
			EOF:           false,
		}
		if err := sink(chunk); err != nil {
			return err
		}
		seq++
	}

	// Final EOF marker (always sent — a client loading indicator can
	// then flip to "done" even when the window is empty).
	return sink(Chunk{
		CorrelationID: req.CorrelationID,
		Seq:           seq,
		EOF:           true,
	})
}

func (r *Request) validate() error {
	if strings.TrimSpace(r.Topic) == "" {
		return fmt.Errorf("topic required")
	}
	if r.From.IsZero() || r.To.IsZero() {
		return fmt.Errorf("from and to required")
	}
	if r.To.Before(r.From) {
		return fmt.Errorf("to must not be before from")
	}
	return nil
}

// parseTopic splits "type.exchange.instrument" or "type" into the
// partition components the datalake reader expects. Bare topics
// (e.g. "news") map to type-only queries.
func parseTopic(topic string) (dt, exchange, instrument string, err error) {
	parts := strings.SplitN(topic, ".", 3)
	switch len(parts) {
	case 1:
		return parts[0], "", "", nil
	case 2:
		return parts[0], parts[1], "", nil
	case 3:
		return parts[0], parts[1], parts[2], nil
	}
	return "", "", "", fmt.Errorf("invalid topic %q", topic)
}

// recordsFromDatalake converts datalake.Record → daterange.Record
// (same fields, different package to keep the dependency one-way).
func recordsFromDatalake(in []datalake.Record) []Record {
	out := make([]Record, len(in))
	for i, r := range in {
		out[i] = Record{Topic: r.Topic, Timestamp: r.Timestamp, Payload: r.Payload}
	}
	return out
}
