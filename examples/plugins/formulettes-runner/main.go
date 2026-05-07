// formulettes-runner is the Phase-8 generic-plugin demo: shells out
// to a CLI (bs-cli, a tiny C++ wrapper around
// ../../../../Sibelius/formulettes/BlackScholes.h) on each input
// edit and surfaces the price + greeks as cells.
//
// Demonstrates: a plugin can integrate an arbitrary external CLI
// without changing the server / TUI / desktop / phone core, as long
// as the CLI takes argv + writes stdout.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

type inputs struct {
	spot    float64
	strike  float64
	vol     float64
	rate    float64
	time    float64
	optType string // "call" or "put"
}

type greeks struct {
	price float64
	delta float64
	gamma float64
	vega  float64
	theta float64
	rho   float64
	err   string // non-empty → bs-cli failed; cell shows the message
}

// Barrier-specific extra inputs. Shares spot/strike/vol/rate/time
// with `inputs` to keep the parameter strip stable when the user
// switches function — only the H / K / barrier-type cells change.
type barrierInputs struct {
	barrier   float64 // H
	rebate    float64 // K
	btype     string  // DOC / UOC / DOP / UOP / DIC / UIC / DIP / UIP
}

type barrierResult struct {
	price float64
	delta float64
	gamma float64
	vega  float64
	theta float64
	rho   float64
	btype string
	err   string
}

type state struct {
	function string // "bs" or "barrier"
	in       inputs
	gr       greeks
	bin      barrierInputs
	bout     barrierResult
}

func defaultState() state {
	return state{
		function: "bs",
		in:       inputs{spot: 100, strike: 100, vol: 0.20, rate: 0.05, time: 1.0, optType: "call"},
		bin:      barrierInputs{barrier: 80, rebate: 0, btype: "DOC"},
	}
}

func main() {
	p := sdk.New("plugin.formulettes-runner.screen")
	s := defaultState()
	recompute(&s)

	// stateMu protects `s` between the input-event goroutine that
	// `Run` invokes and the heartbeat goroutine below.
	var stateMu sync.Mutex

	// Initial publish happens before Run connects to the bus, so the
	// first UpdateCellGrid is a best-effort that may be dropped on
	// the floor if no subscriber is listening yet. The 5 s heartbeat
	// below republishes the current grid until something is reading
	// — once a panel subscribes the next tick paints it. That's the
	// fix for the "Plugin screen 'FORM' — waiting for data..." stale
	// state the user kept hitting on a fresh boot.
	p.UpdateCellGrid("FORM", buildGrid(s), true)

	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			stateMu.Lock()
			grid := buildGrid(s)
			stateMu.Unlock()
			p.UpdateCellGrid("FORM", grid, true)
		}
	}()

	p.Run(func(msg sdk.Message) {
		if msg.Topic != "plugin.formulettes-runner.input" {
			return
		}
		evt, ok := sdk.AsInputEvent(msg)
		if !ok {
			return
		}
		stateMu.Lock()
		applyInput(&s, evt)
		recompute(&s)
		grid := buildGrid(s)
		stateMu.Unlock()
		p.UpdateCellGrid("FORM", grid, true)
	})
}

// recompute dispatches to the right CLI subcommand for the active
// function and stamps the result back into state. Splits the
// "always run BS" assumption from the single-function days so the
// barrier mode doesn't accidentally invoke the BS path.
func recompute(s *state) {
	switch s.function {
	case "barrier":
		s.bout = computeBarrierOrError(s.in, s.bin)
	default:
		s.gr = computeOrError(s.in)
	}
}

func applyInput(s *state, evt sdk.InputEvent) {
	r, c := evt.Address.Row, evt.Address.Col
	switch {
	case r == 0 && c == 1:
		// Function selector — switching mode shouldn't carry stale
		// outputs from the other side, so blank both before
		// recompute lays down fresh values.
		s.function = sdk.CellStringValue(evt.Value)
		s.gr = greeks{}
		s.bout = barrierResult{}
	case r == 1 && c == 0:
		s.in.spot = sdk.CellValue(evt.Value)
	case r == 1 && c == 1:
		s.in.strike = sdk.CellValue(evt.Value)
	case r == 2 && c == 0:
		s.in.vol = sdk.CellValue(evt.Value)
	case r == 2 && c == 1:
		s.in.rate = sdk.CellValue(evt.Value)
	case r == 3 && c == 0:
		s.in.time = sdk.CellValue(evt.Value)
	case r == 3 && c == 1:
		s.in.optType = strings.ToLower(sdk.CellStringValue(evt.Value))
	// Barrier-only rows.
	case r == 4 && c == 0:
		s.bin.barrier = sdk.CellValue(evt.Value)
	case r == 4 && c == 1:
		s.bin.rebate = sdk.CellValue(evt.Value)
	case r == 4 && c == 2:
		s.bin.btype = strings.ToUpper(sdk.CellStringValue(evt.Value))
	}
}

func buildGrid(s state) []sdk.Cell {
	header := "BLACK-SCHOLES (formulettes)"
	if s.function == "barrier" {
		header = "BARRIER OPTION (formulettes)"
	}
	cells := []sdk.Cell{
		sdk.HeaderCell(0, 0, header),
		sdk.EnumInputCell(0, 1, "Function", s.function, []sdk.EnumOption{
			{Value: "bs", Label: "Black-Scholes"},
			{Value: "barrier", Label: "Barrier"},
		}),
		sdk.SectionCell(0, 2, "── INPUTS ──", 2),

		sdk.DecimalInputCell(1, 0, "Spot", s.in.spot, 4),
		sdk.DecimalInputCell(1, 1, "Strike", s.in.strike, 4),
		sdk.DecimalInputCell(2, 0, "Vol", s.in.vol, 4),
		sdk.DecimalInputCell(2, 1, "Rate", s.in.rate, 4),
		sdk.DecimalInputCell(3, 0, "Time (years)", s.in.time, 4),
		sdk.EnumInputCell(3, 1, "Type", s.in.optType, []sdk.EnumOption{
			{Value: "call", Label: "Call"},
			{Value: "put", Label: "Put"},
		}),
	}

	// Barrier-only inputs sit on row 4. Hidden when in BS mode so
	// the barrier numbers don't show as ghost cells. The cell-cursor
	// nav stops short of row 4 in BS mode automatically because
	// there are no input cells there.
	if s.function == "barrier" {
		cells = append(cells,
			sdk.DecimalInputCell(4, 0, "Barrier H", s.bin.barrier, 4),
			sdk.DecimalInputCell(4, 1, "Rebate K", s.bin.rebate, 4),
			sdk.EnumInputCell(4, 2, "Type", s.bin.btype, []sdk.EnumOption{
				{Value: "DOC", Label: "DOC (down-out call)"},
				{Value: "UOC", Label: "UOC (up-out call)"},
				{Value: "DOP", Label: "DOP (down-out put)"},
				{Value: "UOP", Label: "UOP (up-out put)"},
				{Value: "DIC", Label: "DIC (down-in call)"},
				{Value: "UIC", Label: "UIC (up-in call)"},
				{Value: "DIP", Label: "DIP (down-in put)"},
				{Value: "UIP", Label: "UIP (up-in put)"},
			}),
		)
	}

	cells = append(cells, sdk.SectionCell(5, 0, "── OUTPUTS ──", 4))

	switch s.function {
	case "barrier":
		if s.bout.err != "" {
			cells = append(cells, sdk.TextCell(6, 0, "Error: "+s.bout.err, &sdk.CellStyle{Fg: "red"}))
			return cells
		}
		cells = append(cells,
			sdk.NumberCell(6, 0, "Price", s.bout.price, 4, ""),
			sdk.NumberCell(6, 1, "Delta", s.bout.delta, 4, ""),
			sdk.NumberCell(7, 0, "Gamma", s.bout.gamma, 6, ""),
			sdk.NumberCell(7, 1, "Vega", s.bout.vega, 4, ""),
			sdk.NumberCell(8, 0, "Theta (per year)", s.bout.theta, 4, ""),
			sdk.NumberCell(8, 1, "Rho", s.bout.rho, 4, ""),
			sdk.TextCell(9, 0, "Type "+s.bout.btype, &sdk.CellStyle{Fg: "dim"}),
		)
	default:
		if s.gr.err != "" {
			cells = append(cells, sdk.TextCell(6, 0, "Error: "+s.gr.err, &sdk.CellStyle{Fg: "red"}))
			return cells
		}
		cells = append(cells,
			sdk.NumberCell(6, 0, "Price", s.gr.price, 4, ""),
			sdk.NumberCell(6, 1, "Delta", s.gr.delta, 4, ""),
			sdk.NumberCell(7, 0, "Gamma", s.gr.gamma, 6, ""),
			sdk.NumberCell(7, 1, "Vega", s.gr.vega, 4, ""),
			sdk.NumberCell(8, 0, "Theta (per year)", s.gr.theta, 4, ""),
			sdk.NumberCell(8, 1, "Rho", s.gr.rho, 4, ""),
		)
	}
	return cells
}

// bsCLIPath resolves the bs-cli binary. Override via BS_CLI_PATH for
// tests that want to point at a stub.
func bsCLIPath() string {
	if p := os.Getenv("BS_CLI_PATH"); p != "" {
		return p
	}
	// Default: same directory as the plugin binary.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, "bs-cli")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "./bs-cli"
}

// bsREPL is a long-lived bs-cli child running in --repl mode. The
// previous implementation re-exec'd bs-cli on every keystroke;
// macOS dyld + codesign verification adds ~450 ms per spawn, so
// the FORM panel's "pending" indicator stayed lit for half a
// second on every input edit even though the actual BS pricing is
// microseconds. Reusing one process makes recomputes feel instant.
type bsREPL struct {
	cmd    *exec.Cmd
	path   string // path bs-cli was spawned with (so a BS_CLI_PATH env change respawns)
	stdin  io.WriteCloser
	stdout *bufio.Reader
	dead   bool
}

var (
	replMu   sync.Mutex
	replInst *bsREPL
)

// dead reports whether the child has exited (call returned EOF /
// pipe error already, or ProcessState set). A dead REPL forces a
// respawn on the next request.
func (r *bsREPL) isDead() bool {
	if r == nil {
		return true
	}
	if r.dead {
		return true
	}
	if r.cmd != nil && r.cmd.ProcessState != nil {
		return true
	}
	return false
}

// getREPL returns the singleton bs-cli REPL, spawning a new one if
// none exists, the existing one died, or BS_CLI_PATH changed
// (matters in tests that swap the path between cases). On spawn
// failure the error propagates to the caller — a stale instance is
// dropped before returning so the next call re-attempts a clean
// spawn instead of inheriting a half-initialised child.
func getREPL() (*bsREPL, error) {
	replMu.Lock()
	defer replMu.Unlock()
	path := bsCLIPath()
	if replInst != nil && !replInst.isDead() && replInst.path == path {
		return replInst, nil
	}
	// Tear down the previous instance if any. Best-effort — even if
	// Kill fails (already exited) we replace the singleton so a
	// subsequent caller doesn't talk to dead pipes.
	if replInst != nil {
		_ = replInst.stdin.Close()
		if replInst.cmd != nil && replInst.cmd.Process != nil {
			_ = replInst.cmd.Process.Kill()
			_, _ = replInst.cmd.Process.Wait()
		}
		replInst = nil
	}

	cmd := exec.Command(path, "--repl")
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("bs-cli stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("bs-cli stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("bs-cli start: %w", err)
	}
	replInst = &bsREPL{
		cmd:    cmd,
		path:   path,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}
	return replInst, nil
}

// call sends one whitespace-separated request line and returns the
// response line. Serialised by replMu so concurrent input events
// don't interleave on the shared stdio. On any IO error the REPL
// is marked dead so the next caller respawns — a half-broken pipe
// shouldn't poison every subsequent keystroke.
func (r *bsREPL) call(req string) ([]byte, error) {
	replMu.Lock()
	defer replMu.Unlock()
	if _, err := io.WriteString(r.stdin, req+"\n"); err != nil {
		r.dead = true
		return nil, fmt.Errorf("bs-cli write: %w", err)
	}
	line, err := r.stdout.ReadBytes('\n')
	if err != nil {
		r.dead = true
		if len(line) == 0 {
			return nil, fmt.Errorf("bs-cli read: %w", err)
		}
		// Partial line + EOF — still surface an error since the
		// caller relies on a full line for JSON parsing, but
		// include what we got for diagnostics.
		return nil, fmt.Errorf("bs-cli read (partial %q): %w", line, err)
	}
	return line, nil
}


// computeOrError sends one request to the long-lived bs-cli REPL.
// Errors (bad inputs, REPL spawn failure, malformed stdout) become
// greeks.err so the UI shows a single message instead of stale
// numbers.
func computeOrError(in inputs) greeks {
	if in.time <= 0 || in.vol <= 0 || in.spot <= 0 || in.strike <= 0 {
		return greeks{err: "spot, strike, vol, time must all be > 0"}
	}
	optType := in.optType
	if optType != "call" && optType != "put" {
		optType = "call"
	}

	repl, err := getREPL()
	if err != nil {
		return greeks{err: err.Error()}
	}
	req := strings.Join([]string{
		fmtFloat(in.spot),
		fmtFloat(in.strike),
		fmtFloat(in.vol),
		fmtFloat(in.rate),
		fmtFloat(in.time),
		optType,
		fmtFloat(in.rate), // b = r → no dividend / cost-of-carry
	}, " ")
	out, err := repl.call(req)
	if err != nil {
		return greeks{err: "bs-cli: " + err.Error()}
	}

	var raw struct {
		Price float64 `json:"price"`
		Delta float64 `json:"delta"`
		Gamma float64 `json:"gamma"`
		Vega  float64 `json:"vega"`
		Theta float64 `json:"theta"`
		Rho   float64 `json:"rho"`
		Error string  `json:"error"`
	}
	if jerr := json.Unmarshal(out, &raw); jerr != nil {
		return greeks{err: "parse: " + jerr.Error()}
	}
	if raw.Error != "" {
		return greeks{err: raw.Error}
	}
	return greeks{
		price: raw.Price,
		delta: raw.Delta,
		gamma: raw.Gamma,
		vega:  raw.Vega,
		theta: raw.Theta,
		rho:   raw.Rho,
	}
}

func fmtFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// computeBarrierOrError shells out to bs-cli's "barrier" subcommand.
// Returns the price under the chosen barrier formula; the C++ side
// validates inputs and refuses negative S/X/H/T/sigma.
var validBarrierTypes = map[string]bool{
	"DOC": true, "UOC": true, "DOP": true, "UOP": true,
	"DIC": true, "UIC": true, "DIP": true, "UIP": true,
}

func computeBarrierOrError(in inputs, bin barrierInputs) barrierResult {
	if in.time <= 0 || in.vol <= 0 || in.spot <= 0 || in.strike <= 0 {
		return barrierResult{btype: bin.btype, err: "spot, strike, vol, time must all be > 0"}
	}
	if bin.barrier <= 0 {
		return barrierResult{btype: bin.btype, err: "barrier H must be > 0"}
	}
	btype := strings.ToUpper(strings.TrimSpace(bin.btype))
	if !validBarrierTypes[btype] {
		return barrierResult{btype: btype, err: "unknown barrier type: " + btype}
	}

	repl, err := getREPL()
	if err != nil {
		return barrierResult{btype: btype, err: err.Error()}
	}
	req := strings.Join([]string{
		"barrier", btype,
		fmtFloat(in.spot),
		fmtFloat(in.strike),
		fmtFloat(bin.barrier),
		fmtFloat(bin.rebate),
		fmtFloat(in.rate), // b = r → no dividend / cost-of-carry
		fmtFloat(in.rate),
		fmtFloat(in.time),
		fmtFloat(in.vol),
	}, " ")
	out, err := repl.call(req)
	if err != nil {
		return barrierResult{btype: btype, err: "bs-cli barrier: " + err.Error()}
	}

	var raw struct {
		Price float64 `json:"price"`
		Delta float64 `json:"delta"`
		Gamma float64 `json:"gamma"`
		Vega  float64 `json:"vega"`
		Theta float64 `json:"theta"`
		Rho   float64 `json:"rho"`
		Type  string  `json:"type"`
		Error string  `json:"error"`
	}
	if jerr := json.Unmarshal(out, &raw); jerr != nil {
		return barrierResult{btype: btype, err: "parse: " + jerr.Error()}
	}
	if raw.Error != "" {
		return barrierResult{btype: btype, err: raw.Error}
	}
	return barrierResult{
		price: raw.Price,
		delta: raw.Delta,
		gamma: raw.Gamma,
		vega:  raw.Vega,
		theta: raw.Theta,
		rho:   raw.Rho,
		btype: raw.Type,
	}
}
