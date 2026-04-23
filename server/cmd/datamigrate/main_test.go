package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeLegacyFile creates a file under the old
// type=/exchange=/instrument=/year=/… layout.
func makeLegacyFile(t *testing.T, root, topic, date, content string) string {
	t.Helper()
	parts := strings.SplitN(topic, ".", 3)
	var dir string
	switch len(parts) {
	case 3:
		dir = filepath.Join(root,
			"type="+parts[0],
			"exchange="+parts[1],
			"instrument="+parts[2],
			"year="+date[:4], "month="+date[4:6], "day="+date[6:8])
	case 2:
		dir = filepath.Join(root,
			"type="+parts[0],
			"source="+parts[1],
			"year="+date[:4], "month="+date[4:6], "day="+date[6:8])
	default:
		dir = filepath.Join(root, "type="+parts[0],
			"year="+date[:4], "month="+date[4:6], "day="+date[6:8])
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "data.jsonl")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRewritePath_ThreeLevelTopic(t *testing.T) {
	root := t.TempDir()
	makeLegacyFile(t, root, "ohlc.binance.BTCUSDT", "20260422", "x\n")

	oldPath := filepath.Join(root, "type=ohlc", "exchange=binance", "instrument=BTCUSDT",
		"year=2026", "month=04", "day=22", "data.jsonl")
	got, ok := rewritePath(root, oldPath)
	if !ok {
		t.Fatal("expected rewrite")
	}
	wantSuffix := filepath.Join(
		"dims=exchange=binance;instrument=BTCUSDT;type=ohlc",
		"year=2026", "month=04", "day=22", "data.jsonl",
	)
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("got %q, want suffix %q", got, wantSuffix)
	}
}

func TestRewritePath_TwoLevelTopic(t *testing.T) {
	root := "/r"
	old := filepath.Join(root, "type=news", "source=reuters",
		"year=2026", "month=04", "day=22", "data.jsonl")
	got, ok := rewritePath(root, old)
	if !ok {
		t.Fatal("expected rewrite")
	}
	wantSuffix := filepath.Join(
		"dims=source=reuters;type=news",
		"year=2026", "month=04", "day=22", "data.jsonl",
	)
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("got %q, want suffix %q", got, wantSuffix)
	}
}

func TestRewritePath_AlreadyCanonical(t *testing.T) {
	root := "/r"
	new := filepath.Join(root, "dims=exchange=binance;instrument=X;type=ohlc",
		"year=2026", "month=04", "day=22", "data.jsonl")
	_, ok := rewritePath(root, new)
	if ok {
		t.Errorf("already-canonical path should not be rewritten")
	}
}

func TestBuildPlan_OnlyListsLegacyFiles(t *testing.T) {
	root := t.TempDir()
	makeLegacyFile(t, root, "ohlc.binance.BTCUSDT", "20260422", "a\n")
	makeLegacyFile(t, root, "trade.binance.ETHUSDT", "20260422", "b\n")

	// Drop in an already-canonical file that should be ignored.
	newDir := filepath.Join(root,
		"dims=exchange=binance;instrument=XRPUSDT;type=ohlc",
		"year=2026", "month=04", "day=22")
	_ = os.MkdirAll(newDir, 0o755)
	_ = os.WriteFile(filepath.Join(newDir, "data.jsonl"), []byte("c\n"), 0o644)

	plan, err := buildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 {
		t.Errorf("plan has %d moves, want 2", len(plan))
	}
}

func TestApply_MovesFile(t *testing.T) {
	root := t.TempDir()
	src := makeLegacyFile(t, root, "ohlc.binance.BTCUSDT", "20260422", "payload\n")

	dst, ok := rewritePath(root, src)
	if !ok {
		t.Fatal("rewrite failed")
	}

	if r := apply1(move{Src: src, Dst: dst}); r != resultDone {
		t.Fatalf("apply1 returned %d", r)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src still exists: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "payload\n" {
		t.Errorf("dst read: %q, err=%v", data, err)
	}
}

func TestApply_AppendsWhenDstExists(t *testing.T) {
	root := t.TempDir()
	src := makeLegacyFile(t, root, "ohlc.binance.BTCUSDT", "20260422", "from-src\n")
	dst, _ := rewritePath(root, src)

	// Pre-seed dst so apply1 has to merge.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("from-dst\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if r := apply1(move{Src: src, Dst: dst}); r != resultDone {
		t.Fatalf("apply1: %d", r)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "from-dst\nfrom-src\n" {
		t.Errorf("concat: %q", got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("src should be removed after append")
	}
}

func TestPruneEmptyDirs(t *testing.T) {
	root := t.TempDir()
	// Build an empty nested tree and a sibling with a file.
	emptyDir := filepath.Join(root, "type=x", "exchange=y", "instrument=z")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keepDir := filepath.Join(root, "keep")
	_ = os.MkdirAll(keepDir, 0o755)
	_ = os.WriteFile(filepath.Join(keepDir, "f"), []byte("."), 0o644)

	pruneEmptyDirs(root)

	if _, err := os.Stat(emptyDir); !os.IsNotExist(err) {
		t.Error("empty dir should be removed")
	}
	if _, err := os.Stat(keepDir); err != nil {
		t.Error("non-empty dir was removed")
	}
}
