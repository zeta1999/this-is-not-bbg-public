package datalake

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Reader scans datalake JSONL files by Hive-partitioned path.
type Reader struct {
	basePath string
}

// NewReader creates a datalake reader.
func NewReader(basePath string) *Reader {
	return &Reader{basePath: basePath}
}

// Record is a single datalake record.
type Record struct {
	Topic     string          `json:"_topic"`
	Timestamp string          `json:"_timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// Query reads records from datalake files matching the given filters.
// Returns up to `limit` records, newest first.
func (r *Reader) Query(dataType, exchange, instrument string, start, end time.Time, limit int) ([]Record, error) {
	if limit <= 0 {
		limit = 1000
	}

	// Build partition path pattern.
	var partPath string
	switch {
	case exchange != "" && instrument != "":
		partPath = filepath.Join(
			fmt.Sprintf("type=%s", dataType),
			fmt.Sprintf("exchange=%s", exchange),
			fmt.Sprintf("instrument=%s", instrument),
		)
	case exchange != "":
		partPath = filepath.Join(
			fmt.Sprintf("type=%s", dataType),
			fmt.Sprintf("exchange=%s", exchange),
		)
	default:
		partPath = fmt.Sprintf("type=%s", dataType)
	}

	// Enumerate date directories in the range.
	var files []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		datePart := filepath.Join(
			fmt.Sprintf("year=%d", d.Year()),
			fmt.Sprintf("month=%02d", d.Month()),
			fmt.Sprintf("day=%02d", d.Day()),
		)
		// Check for both plain and compressed files.
		for _, name := range []string{"data.jsonl", "data.jsonl.zst"} {
			path := filepath.Join(r.basePath, partPath, datePart, name)
			if _, err := os.Stat(path); err == nil {
				files = append(files, path)
			}
		}
	}

	// Read files, collect records.
	var records []Record
	for _, path := range files {
		recs, err := readJSONLFile(path, limit-len(records))
		if err != nil {
			continue // skip unreadable files
		}
		records = append(records, recs...)
		if len(records) >= limit {
			break
		}
	}

	// Trim to limit.
	if len(records) > limit {
		records = records[len(records)-limit:]
	}

	return records, nil
}

// readJSONLFile reads up to maxRecords from a JSONL file.
// Supports .jsonl (plain) — .jsonl.zst (compressed) support TODO.
func readJSONLFile(path string, maxRecords int) ([]Record, error) {
	if strings.HasSuffix(path, ".zst") {
		// TODO: zstd decompression
		return nil, fmt.Errorf("zstd not yet supported")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() && len(records) < maxRecords {
		var rec Record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}

	return records, scanner.Err()
}

// ListInstruments returns available instruments for a data type.
func (r *Reader) ListInstruments(dataType string) ([]string, error) {
	typePath := filepath.Join(r.basePath, fmt.Sprintf("type=%s", dataType))
	entries, err := os.ReadDir(typePath)
	if err != nil {
		return nil, err
	}

	var instruments []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "exchange=") {
			exchange := strings.TrimPrefix(name, "exchange=")
			// List instruments under this exchange.
			exPath := filepath.Join(typePath, name)
			instEntries, err := os.ReadDir(exPath)
			if err != nil {
				continue
			}
			for _, ie := range instEntries {
				if ie.IsDir() && strings.HasPrefix(ie.Name(), "instrument=") {
					inst := strings.TrimPrefix(ie.Name(), "instrument=")
					instruments = append(instruments, exchange+"/"+inst)
				}
			}
		}
	}
	return instruments, nil
}
