package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestParseBtEngineSummary covers the JSON-summary parsing path that
// runHistoricalBacktest funnels stdout through. Asserts the cell-grid
// inputs (pnl/sharpe/maxDD/trades) all flow non-zero from a canned
// payload matching gpu-backtest's `output.rs` shape.
func TestParseBtEngineSummary(t *testing.T) {
	canned := []byte(`{
	  "tickers": [{"ticker":"BTCUSDT","pnl":150.5,"sharpe":1.8,"max_drawdown":0.12,"num_trades":42,"total_volume":3500,"pnl_curve_len":1000}],
	  "aggregate": {"pnl":150.5,"sharpe":1.8,"max_drawdown":0.12,"num_trades":42,"total_volume":3500,"pnl_curve_len":1000}
	}`)

	r, err := parseBtEngineSummary(canned)
	if err != nil {
		t.Fatalf("parseBtEngineSummary: %v", err)
	}
	if r.status != "done" {
		t.Fatalf("status: want done, got %q", r.status)
	}
	if r.pnl == 0 || r.sharpe == 0 || r.maxDD == 0 || r.trades == 0 {
		t.Fatalf("expected non-zero metrics, got %+v", r)
	}
	if r.pnl != 150.5 || r.sharpe != 1.8 || r.maxDD != 0.12 || r.trades != 42 {
		t.Fatalf("metrics drift: %+v", r)
	}
}

func TestParseBtEngineSummary_BadJSON(t *testing.T) {
	r, err := parseBtEngineSummary([]byte("not json"))
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if r.status != "error" || !strings.HasPrefix(r.errorMsg, "parse output:") {
		t.Fatalf("expected error result, got %+v", r)
	}
}

// TestRunHistoricalBacktest_StubEngine spawns a stub bt-engine that
// emits the canned JSON, then verifies runHistoricalBacktest funnels
// it through into state.histResult. Skips on Windows (uses a shell
// script as the stub binary).
func TestRunHistoricalBacktest_StubEngine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub uses /bin/sh")
	}

	dir := t.TempDir()
	stub := filepath.Join(dir, "bt-engine-stub.sh")
	script := `#!/bin/sh
cat <<'EOF'
{"tickers":[{"ticker":"X","pnl":99,"sharpe":1.1,"max_drawdown":0.05,"num_trades":7,"total_volume":21,"pnl_curve_len":10}],
 "aggregate":{"pnl":99,"sharpe":1.1,"max_drawdown":0.05,"num_trades":7,"total_volume":21,"pnl_curve_len":10}}
EOF
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BT_ENGINE_PATH", stub)
	// Smoke-check exec works (catches `noexec` tmpdirs, etc.).
	if _, err := exec.CommandContext(context.Background(), stub).Output(); err != nil {
		t.Skipf("stub binary not executable in temp dir: %v", err)
	}

	s := defaultState()
	s.cfg.mode = "Historical"
	runHistoricalBacktest(&s)

	if s.histResult == nil {
		t.Fatal("histResult is nil")
	}
	if s.histResult.status != "done" {
		t.Fatalf("status: want done, got %q (err=%q)", s.histResult.status, s.histResult.errorMsg)
	}
	if s.histResult.pnl != 99 || s.histResult.sharpe != 1.1 || s.histResult.maxDD != 0.05 || s.histResult.trades != 7 {
		t.Fatalf("metrics drift: %+v", *s.histResult)
	}
}

// TestGenerateTOML_HasDuckDBPath asserts the generated TOML includes a
// duckdb_path line — bt-engine's DataConfig requires it (config.rs).
// Without this, bt-engine's TOML parse fails before any data is read.
func TestGenerateTOML_HasDuckDBPath(t *testing.T) {
	cfg := defaultState().cfg
	toml := generateTOML(cfg, "/tmp/strat.aria")
	if !strings.Contains(toml, "duckdb_path") {
		t.Fatalf("generated TOML missing duckdb_path:\n%s", toml)
	}
	if !strings.Contains(toml, "json_summary = true") {
		t.Fatalf("generated TOML missing json_summary:\n%s", toml)
	}
}

// TestBtEngineDuckDBPath_EnvOverride checks the env-override path so
// operators can point bt-engine at their own dataset without rebuilding
// the plugin.
func TestBtEngineDuckDBPath_EnvOverride(t *testing.T) {
	t.Setenv("BT_ENGINE_DUCKDB_PATH", "/tmp/custom.duckdb")
	if got := btEngineDuckDBPath(); got != "/tmp/custom.duckdb" {
		t.Fatalf("env override ignored: got %q", got)
	}
}
