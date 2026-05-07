package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeStubBSCLI lays down a /bin/sh script that prints a fixed BS
// JSON payload. Returns the path. Tests t.Setenv BS_CLI_PATH at it.
//
// The script supports the production --repl invocation: when called
// with `--repl` it loops over stdin lines, re-binds positional args
// from each line via `set --`, and runs the test-provided body
// once per line. Test bodies that `exit` will kill the loop, which
// is the same observable behaviour as the old per-call exec.
func writeStubBSCLI(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub uses /bin/sh")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "bs-cli-stub.sh")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--repl\" ]; then\n" +
		"  while IFS= read -r line; do\n" +
		"    set -- $line\n" +
		body + "\n" +
		"  done\n" +
		"  exit 0\n" +
		"fi\n" +
		body + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

func TestComputeOrError_GoodResponse(t *testing.T) {
	stub := writeStubBSCLI(t, `cat <<'EOF'
{"price":10.45,"delta":0.63,"gamma":0.018,"vega":37.5,"theta":-6.41,"rho":53.2}
EOF`)
	t.Setenv("BS_CLI_PATH", stub)

	got := computeOrError(inputs{spot: 100, strike: 100, vol: 0.2, rate: 0.05, time: 1, optType: "call"})
	if got.err != "" {
		t.Fatalf("unexpected error: %q", got.err)
	}
	if got.price != 10.45 || got.delta != 0.63 {
		t.Fatalf("metric drift: %+v", got)
	}
}

func TestComputeOrError_BadInputs(t *testing.T) {
	got := computeOrError(inputs{spot: -1, strike: 100, vol: 0.2, rate: 0.05, time: 1, optType: "call"})
	if got.err == "" {
		t.Fatal("expected validation error for negative spot")
	}

	got = computeOrError(inputs{spot: 100, strike: 100, vol: 0, rate: 0.05, time: 1, optType: "call"})
	if got.err == "" {
		t.Fatal("expected validation error for zero vol")
	}
}

func TestComputeOrError_StubFailsLoud(t *testing.T) {
	stub := writeStubBSCLI(t, `echo 'boom' >&2; exit 7`)
	t.Setenv("BS_CLI_PATH", stub)

	got := computeOrError(inputs{spot: 100, strike: 100, vol: 0.2, rate: 0.05, time: 1, optType: "call"})
	if got.err == "" {
		t.Fatal("expected error from failing bs-cli")
	}
}

func TestComputeOrError_BadJSON(t *testing.T) {
	stub := writeStubBSCLI(t, `echo "not json"`)
	t.Setenv("BS_CLI_PATH", stub)

	got := computeOrError(inputs{spot: 100, strike: 100, vol: 0.2, rate: 0.05, time: 1, optType: "call"})
	if got.err == "" {
		t.Fatal("expected parse error")
	}
}

func TestBuildGrid_HasInputAndOutputSections(t *testing.T) {
	s := defaultState()
	s.gr = greeks{price: 1, delta: 1, gamma: 1, vega: 1, theta: 1, rho: 1}
	cells := buildGrid(s)
	if len(cells) == 0 {
		t.Fatal("empty grid")
	}
	// Sanity: first cell carries the BLACK-SCHOLES header text
	// (HeaderCell is implemented as a styled text cell).
	if cells[0].Text == "" {
		t.Fatalf("first cell has no text: %+v", cells[0])
	}
}

func TestBuildGrid_ErrorRendersNoMetrics(t *testing.T) {
	s := defaultState()
	s.gr = greeks{err: "boom"}
	cells := buildGrid(s)
	for _, c := range cells {
		if c.Type == "number" {
			t.Fatalf("should not show number cells when error set: %+v", c)
		}
	}
}

// TestComputeBarrier_PicksRightSubcommand verifies that the barrier
// path drives bs-cli with `barrier <type>` as argv[1..2] — the
// stub re-prints argv so we can assert dispatch.
func TestComputeBarrier_PicksRightSubcommand(t *testing.T) {
	stub := writeStubBSCLI(t, `
case "$1" in
  barrier)
    # Echo the type back into the JSON so the test can assert it.
    printf '{"price":12.5,"type":"%s"}\n' "$2"
    ;;
  *)
    echo "stub got non-barrier first arg: $1" >&2
    exit 99
    ;;
esac
`)
	t.Setenv("BS_CLI_PATH", stub)

	in := inputs{spot: 100, strike: 100, vol: 0.2, rate: 0.05, time: 1, optType: "call"}
	bin := barrierInputs{barrier: 80, rebate: 0, btype: "DOC"}
	got := computeBarrierOrError(in, bin)
	if got.err != "" {
		t.Fatalf("unexpected error: %q", got.err)
	}
	if got.price != 12.5 {
		t.Fatalf("price drift: %+v", got)
	}
	if got.btype != "DOC" {
		t.Fatalf("type drift: %+v", got)
	}
}

// TestComputeBarrier_RejectsUnknownType keeps the validation tight
// — any future typo in the GUI enum should surface a useful error
// instead of the C++ side bailing with a cryptic non-zero status.
func TestComputeBarrier_RejectsUnknownType(t *testing.T) {
	got := computeBarrierOrError(
		inputs{spot: 100, strike: 100, vol: 0.2, rate: 0.05, time: 1, optType: "call"},
		barrierInputs{barrier: 80, rebate: 0, btype: "XYZ"},
	)
	if got.err == "" {
		t.Fatal("expected validation error for unknown barrier type")
	}
}

// TestBuildGrid_BarrierModeShowsBarrierInputs confirms the cell
// grid swaps in the H / K / type cells when function == "barrier".
func TestBuildGrid_BarrierModeShowsBarrierInputs(t *testing.T) {
	s := defaultState()
	s.function = "barrier"
	s.bout = barrierResult{price: 1.23, btype: "DOC"}
	cells := buildGrid(s)
	hasBarrierLabel := false
	for _, c := range cells {
		if c.Label == "Barrier H" {
			hasBarrierLabel = true
			break
		}
	}
	if !hasBarrierLabel {
		t.Fatal("barrier mode should expose the H input cell")
	}
}
