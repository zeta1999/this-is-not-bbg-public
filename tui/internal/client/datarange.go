package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DataRangeRecord mirrors the server's daterange.Record shape
// (Topic + Timestamp + raw JSON payload bytes).
type DataRangeRecord struct {
	Topic     string          `json:"_topic"`
	Timestamp string          `json:"_timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// DataRangeChunk mirrors the server's daterange.Chunk; matches the
// NDJSON on the wire (fields use Go struct casing because the server
// json-encodes the struct directly).
type DataRangeChunk struct {
	CorrelationID string            `json:"CorrelationID"`
	Records       []DataRangeRecord `json:"Records"`
	Seq           int32             `json:"Seq"`
	EOF           bool              `json:"EOF"`
}

// DataRangeClient fetches historical windows via the HTTP NDJSON
// endpoint exposed by the server (see server/internal/transport).
type DataRangeClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewDataRangeClient reads /tmp/notbbg-tui.token, writes by the
// server at startup, and targets the local gateway. If the token
// file is missing this still returns a client — calls will fail
// with an auth error, which the UI can surface.
func NewDataRangeClient(baseURL string) *DataRangeClient {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:9474"
	}
	tok, _ := os.ReadFile("/tmp/notbbg-tui.token")
	return &DataRangeClient{
		baseURL: baseURL,
		token:   strings.TrimSpace(string(tok)),
		http:    &http.Client{Timeout: 0}, // 0 = no timeout; context handles it
	}
}

// Fetch streams chunks for the given window. `sink` is called once
// per NDJSON line; returning an error aborts the stream (equivalent
// to context cancellation). An EOF chunk is always sent last on a
// successful stream — callers can flip "loading" off when they see
// it.
func (c *DataRangeClient) Fetch(ctx context.Context, topic string, from, to time.Time, correlationID string, maxRecords int, sink func(DataRangeChunk) error) error {
	q := url.Values{}
	q.Set("topic", topic)
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))
	if correlationID != "" {
		q.Set("correlation_id", correlationID)
	}
	if maxRecords > 0 {
		q.Set("max_records", strconv.Itoa(maxRecords))
	}
	if c.token != "" {
		q.Set("token", c.token)
	}

	reqURL := c.baseURL + "/api/v1/datarange?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("datarange http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("datarange http %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var chunk DataRangeChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			// malformed line — skip but keep the stream going; the
			// server's terminal error chunk uses a different shape
			// (correlation_id + reason) which we ignore here and let
			// the caller treat as implicit EOF.
			continue
		}
		if err := sink(chunk); err != nil {
			return err
		}
	}
	return sc.Err()
}
