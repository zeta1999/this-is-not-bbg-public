package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/notbbg/notbbg/server/pkg/protocol/notbbg/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// fixtureFor maps a fixture filename (without extension) to a fresh
// zero-value proto. New fixture files MUST register here so the
// corpus test exercises them; an unregistered file causes a hard
// failure (silent skip would defeat the point of the conformance
// guarantee).
func fixtureFor(name string) proto.Message {
	switch name {
	case "ohlc":
		return &pb.OHLC{}
	case "trade":
		return &pb.Trade{}
	case "lob_snapshot":
		return &pb.LOBSnapshot{}
	case "news_item":
		return &pb.NewsItem{}
	case "feed_status":
		return &pb.FeedStatus{}
	case "plugin_status":
		return &pb.PluginStatus{}
	case "sanity_snapshot":
		return &pb.SanitySnapshot{}
	case "generic_row":
		return &pb.GenericRow{}
	}
	return nil
}

// fixtureRoot resolves the path to docs/schema-fixtures/ from this
// test file. The test runs from server/cmd/schemacheck — go up four
// levels to repo root, then into docs/schema-fixtures.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", "..", "docs", "schema-fixtures"))
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("fixtureRoot not found: %v (cwd=%s)", err, wd)
	}
	return root
}

// TestCorpus_RoundTrip walks docs/schema-fixtures/*.jsonl and asserts
// that every line:
//  1. protojson-decodes into the registered proto.
//  2. wire-marshal + wire-unmarshal returns proto.Equal.
//  3. protojson-marshal + protojson-unmarshal returns proto.Equal.
//
// Failure mode is a single line per breakage with file:line:topic so
// regenerated fixtures point you at the broken record without
// scrolling the diff.
func TestCorpus_RoundTrip(t *testing.T) {
	root := fixtureRoot(t)

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	saw := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".jsonl")
		newMsg := fixtureFor(base)
		if newMsg == nil {
			t.Errorf("%s: no proto registered in fixtureFor — add a case", e.Name())
			continue
		}

		f, err := os.Open(filepath.Join(root, e.Name()))
		if err != nil {
			t.Errorf("open %s: %v", e.Name(), err)
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		line := 0
		for sc.Scan() {
			line++
			raw := sc.Bytes()
			if len(strings.TrimSpace(string(raw))) == 0 {
				continue
			}
			saw++

			// 1. protojson decode.
			//    Use a fresh instance so each line is independent.
			msg1 := proto.Clone(newMsg)
			proto.Reset(msg1)
			if err := protojson.Unmarshal(raw, msg1); err != nil {
				t.Errorf("%s:%d protojson decode: %v", e.Name(), line, err)
				continue
			}

			// 2. wire round-trip.
			wire, err := proto.Marshal(msg1)
			if err != nil {
				t.Errorf("%s:%d wire marshal: %v", e.Name(), line, err)
				continue
			}
			msg2 := proto.Clone(newMsg)
			proto.Reset(msg2)
			if err := proto.Unmarshal(wire, msg2); err != nil {
				t.Errorf("%s:%d wire unmarshal: %v", e.Name(), line, err)
				continue
			}
			if !proto.Equal(msg1, msg2) {
				t.Errorf("%s:%d wire round-trip drift", e.Name(), line)
			}

			// 3. protojson round-trip.
			jsonOut, err := protojson.Marshal(msg1)
			if err != nil {
				t.Errorf("%s:%d protojson marshal: %v", e.Name(), line, err)
				continue
			}
			msg3 := proto.Clone(newMsg)
			proto.Reset(msg3)
			if err := protojson.Unmarshal(jsonOut, msg3); err != nil {
				t.Errorf("%s:%d protojson re-decode: %v", e.Name(), line, err)
				continue
			}
			if !proto.Equal(msg1, msg3) {
				t.Errorf("%s:%d protojson round-trip drift", e.Name(), line)
			}
		}
		_ = f.Close()
		if err := sc.Err(); err != nil {
			t.Errorf("%s: scan: %v", e.Name(), err)
		}
	}

	if saw == 0 {
		t.Fatal("no fixture lines exercised — corpus test is dormant")
	}
	t.Logf("corpus check: %d records across %d fixture files", saw, countJSONL(root))
}

func countJSONL(root string) int {
	entries, _ := os.ReadDir(root)
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			n++
		}
	}
	return n
}
