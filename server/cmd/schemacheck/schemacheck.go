// Package main — schemacheck validates JSONL datalake records against
// notbbg.v1 proto schemas. Reports drift per topic prefix.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	pb "github.com/notbbg/notbbg/server/pkg/protocol/notbbg/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// record is the outer datalake envelope written by
// server/internal/datalake/writer.go.
type record struct {
	Topic     string          `json:"_topic"`
	Timestamp string          `json:"_timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// Report aggregates validation results by topic prefix.
type Report struct {
	Files   int
	Records int
	Buckets map[string]*Bucket
}

// Bucket holds counters + example mismatches for one topic prefix.
type Bucket struct {
	OK         int
	Mismatch   int
	Unknown    int
	Examples   []string // first N mismatch messages
	MaxExample int
}

func (r *Report) bucket(prefix string) *Bucket {
	if b, ok := r.Buckets[prefix]; ok {
		return b
	}
	b := &Bucket{MaxExample: 10}
	r.Buckets[prefix] = b
	return b
}

func (b *Bucket) addExample(msg string) {
	if len(b.Examples) < b.MaxExample {
		b.Examples = append(b.Examples, msg)
	}
}

// AnyMismatch reports whether any bucket saw a schema mismatch
// (including envelope parse failures).
func (r *Report) AnyMismatch() bool {
	for _, b := range r.Buckets {
		if b.Mismatch > 0 {
			return true
		}
	}
	return false
}

// NewReport returns an empty report.
func NewReport() *Report {
	return &Report{Buckets: make(map[string]*Bucket)}
}

// Scan reads JSONL from r and updates rep. source is used only for
// error messages. sample > 0 caps records read from this stream.
func Scan(rep *Report, src io.Reader, source string, sample int) error {
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	line := 0
	seen := 0
	for sc.Scan() {
		line++
		if sample > 0 && seen >= sample {
			break
		}
		seen++
		rep.Records++

		var rec record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			b := rep.bucket("__envelope__")
			b.Mismatch++
			b.addExample(fmt.Sprintf("%s:%d envelope parse: %v", source, line, err))
			continue
		}

		prefix := topicPrefix(rec.Topic)
		b := rep.bucket(prefix)

		msg := protoFor(rec.Topic)
		if msg == nil {
			b.Unknown++
			continue
		}
		if err := protojson.Unmarshal(rec.Payload, msg); err != nil {
			b.Mismatch++
			b.addExample(fmt.Sprintf("%s:%d topic=%s: %v", source, line, rec.Topic, trimErr(err)))
			continue
		}
		b.OK++
	}
	return sc.Err()
}

// topicPrefix returns the bus topic's first dotted component, e.g.
// "ohlc.binance.BTCUSDT" → "ohlc".
func topicPrefix(topic string) string {
	if i := strings.IndexByte(topic, '.'); i >= 0 {
		return topic[:i]
	}
	return topic
}

// protoFor returns a fresh zero-value proto message for the topic, or
// nil if the topic has no known schema (counted as "unknown", not a
// mismatch).
func protoFor(topic string) proto.Message {
	switch topicPrefix(topic) {
	case "ohlc":
		return &pb.OHLC{}
	case "lob":
		return &pb.LOBSnapshot{}
	case "trade":
		return &pb.Trade{}
	case "news":
		return &pb.NewsItem{}
	case "alert":
		return &pb.Alert{}
	case "feed":
		return &pb.FeedStatus{}
	}
	return nil
}

// trimErr keeps error lines short — protojson errors can be verbose.
func trimErr(err error) string {
	s := err.Error()
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// FormatReport writes a human-readable summary.
func FormatReport(w io.Writer, rep *Report) {
	fmt.Fprintf(w, "files scanned: %d\n", rep.Files)
	fmt.Fprintf(w, "records:       %d\n\n", rep.Records)

	var prefixes []string
	for p := range rep.Buckets {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)

	fmt.Fprintln(w, "by topic prefix:")
	for _, p := range prefixes {
		b := rep.Buckets[p]
		fmt.Fprintf(w, "  %-16s ok=%-6d mismatch=%-6d unknown=%-6d\n",
			p, b.OK, b.Mismatch, b.Unknown)
	}

	for _, p := range prefixes {
		b := rep.Buckets[p]
		if len(b.Examples) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n[%s] first mismatches:\n", p)
		for _, m := range b.Examples {
			fmt.Fprintf(w, "  %s\n", m)
		}
	}
}
