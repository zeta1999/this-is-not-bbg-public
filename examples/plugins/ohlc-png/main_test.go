// Tests for the ohlc-png Python plugin. Run with `go test`.
//
// The Python script's protocol-conformance is the load-bearing
// thing: the Go server reads JSON-per-line back out of stdout, so
// any drift breaks the plugin pipeline silently.
package ohlcpng_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// pythonOK reports whether `python3` is on PATH and importable —
// otherwise skip subprocess tests rather than fail in environments
// without Python (e.g. the Go-only CI image).
func pythonOK(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("plugin uses POSIX-style stdin streaming; skipping on Windows")
	}
	p, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	return p
}

func mainPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("main.py")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestOHLCPNG_FirstFrameIsWaiting asserts the plugin emits a
// "Waiting for ohlc.binance.BTCUSDT…" cell grid before any data
// arrives — keeps the PLOT tab populated on cold start.
func TestOHLCPNG_FirstFrameIsWaiting(t *testing.T) {
	py := pythonOK(t)
	cmd := exec.Command(py, mainPath(t))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	r := bufio.NewReader(stdout)
	deadline := time.After(3 * time.Second)
	got := make(chan []byte, 1)
	go func() {
		line, _ := r.ReadBytes('\n')
		got <- line
	}()
	select {
	case <-deadline:
		t.Fatal("no first frame within 3s")
	case line := <-got:
		var msg struct {
			Topic   string          `json:"Topic"`
			Payload json.RawMessage `json:"Payload"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			t.Fatalf("bad JSON: %v\n%s", err, line)
		}
		if msg.Topic != "plugin.ohlc-png.screen" {
			t.Fatalf("unexpected topic %q", msg.Topic)
		}
		if !bytes.Contains(line, []byte("Waiting")) {
			t.Fatalf("expected waiting text, got: %s", line)
		}
	}
}

// TestOHLCPNG_RendersOnFirstBar feeds one OHLC frame and asserts
// the next stdout line is a CellGridUpdate that includes either
// an `image` cell (matplotlib present) OR the `matplotlib not
// installed` red fallback (matplotlib absent). Either path is
// "the plugin handled the bar correctly" — both are observable
// outputs the GUIs render today.
func TestOHLCPNG_RendersOnFirstBar(t *testing.T) {
	py := pythonOK(t)
	cmd := exec.Command(py, mainPath(t))
	cmd.Env = append(cmd.Environ(), "OHLC_PNG_HEADLESS=1", "PLOT_REFRESH_BARS=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	// Feed a canned OHLC bar.
	bar := map[string]any{
		"Topic": "ohlc.binance.BTCUSDT",
		"Payload": map[string]any{
			"Instrument": "BTCUSDT",
			"Exchange":   "binance",
			"Close":      65000.5,
			"Open":       64900.0,
			"High":       65100.0,
			"Low":        64800.0,
			"Volume":     12.3,
			"Timeframe":  "1m",
		},
	}
	enc := json.NewEncoder(stdin)
	if err := enc.Encode(bar); err != nil {
		t.Fatal(err)
	}

	// Drain stdout, looking for either the rendered image cell or
	// the matplotlib-missing fallback. Skip the initial waiting frame.
	r := bufio.NewReader(stdout)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break
		}
		if !bytes.Contains(line, []byte("plugin.ohlc-png.screen")) {
			continue
		}
		if bytes.Contains(line, []byte("Waiting")) {
			continue // initial frame
		}
		if bytes.Contains(line, []byte(`"type": "image"`)) ||
			bytes.Contains(line, []byte(`"type":"image"`)) ||
			bytes.Contains(line, []byte("matplotlib not installed")) {
			return // ✅ either rendered or surfaced the missing-dep
		}
		t.Logf("unexpected frame: %s", strings.TrimSpace(string(line)))
	}
	t.Fatal("no image-or-fallback frame within 8s")
}
