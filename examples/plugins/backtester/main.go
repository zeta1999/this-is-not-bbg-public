// backtester is a proof-of-concept plugin implementing a simple SMA crossover
// strategy on live OHLC data. Demonstrates the cell grid with:
// - Script display (read-only for now, editable in Phase 7)
// - Configuration inputs (fast/slow period, ticker)
// - Live results (PnL, trades, Sharpe, max drawdown)
// - Trade log table
//
// This is a standalone Go backtester. The gpu-backtest Aria DSL engine
// will be integrated later via C bridge or sidecar process.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

type config struct {
	mode       string // "RT" or "Historical"
	fastPeriod int64
	slowPeriod int64
	ticker     string
	// Historical mode fields.
	startDate  string
	endDate    string
	dataSource string
}

type trade struct {
	price float64
	side  string // "BUY" or "SELL"
	pnl   float64
	idx   int
}

type historicalResult struct {
	pnl      float64
	sharpe   float64
	maxDD    float64
	trades   int
	volume   float64
	status   string // "idle", "running", "done", "error"
	errorMsg string
}

type state struct {
	cfg          config
	prices       []float64 // close price history
	position     float64   // +1 = long, -1 = short, 0 = flat
	entryPrice   float64
	trades       []trade
	equity       float64
	peakEquity   float64
	maxDD        float64
	template     string
	histResult   *historicalResult
	scriptName   string // filename (without path) for custom scripts
	customScript string // content of the custom script
}

const scriptsDir = ".config/this-is-not-bbg/scripts"

func scriptsDirPath() string {
	return filepath.Join(os.Getenv("HOME"), scriptsDir)
}

// listScripts returns filenames of .aria files in the scripts directory.
func listScripts() []string {
	dir := scriptsDirPath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".aria") || strings.HasSuffix(e.Name(), ".strat")) {
			names = append(names, e.Name())
		}
	}
	return names
}

// loadScript reads a script file from the scripts directory.
func loadScript(name string) string {
	data, err := os.ReadFile(filepath.Join(scriptsDirPath(), name))
	if err != nil {
		return ""
	}
	return string(data)
}

// saveScript writes a script file to the scripts directory.
func saveScript(name, content string) error {
	dir := scriptsDirPath()
	os.MkdirAll(dir, 0755)
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
}

func defaultState() state {
	return state{
		cfg: config{
			mode:       "RT",
			fastPeriod: 10,
			slowPeriod: 30,
			ticker:     "BTCUSDT",
			startDate:  "2024-01-01",
			endDate:    "2024-12-31",
			dataSource: "DuckDB",
		},
		template:   "SMA Crossover",
		scriptName: "untitled.strat",
	}
}

// resultCh receives async bt-engine results.
var resultCh = make(chan *historicalResult, 1)

func main() {
	p := sdk.New("plugin.backtester.screen")
	s := defaultState()

	p.Run(func(msg sdk.Message) {
		// Check for async results.
		select {
		case r := <-resultCh:
			s.histResult = r
		default:
		}

		switch {
		case msg.Topic == "plugin.backtester.input":
			var evt sdk.InputEvent
			if json.Unmarshal(msg.Payload, &evt) != nil {
				return
			}
			applyInput(&s, evt)

			// Trigger historical backtest when mode is Historical and not already running.
			if s.cfg.mode == "Historical" && (s.histResult == nil || s.histResult.status != "running") {
				s.histResult = &historicalResult{status: "running"}
				go func() {
					runHistoricalBacktest(&s)
					resultCh <- s.histResult
				}()
			}

		default:
			var payload struct {
				Instrument string  `json:"Instrument"`
				Close      float64 `json:"Close"`
			}
			if json.Unmarshal(msg.Payload, &payload) != nil {
				return
			}
			if payload.Instrument != s.cfg.ticker || payload.Close <= 0 {
				return
			}
			s.prices = append(s.prices, payload.Close)
			if len(s.prices) > 500 {
				s.prices = s.prices[len(s.prices)-500:]
			}
			runStrategy(&s, payload.Close)
		}

		p.UpdateCellGrid("BACKTEST", buildGrid(s), true)
	})
}

func applyInput(s *state, evt sdk.InputEvent) {
	r, c := evt.Address.Row, evt.Address.Col
	switch {
	case r == 0 && c == 1:
		s.template = sdk.CellStringValue(evt.Value)
		if s.template == "Custom" {
			// Load script if a file is selected.
			if s.scriptName != "" {
				content := loadScript(s.scriptName)
				if content != "" {
					s.customScript = content
				}
			}
		}
	case r == 0 && c == 2:
		s.cfg.mode = sdk.CellStringValue(evt.Value)
	case r == 1 && c == 0:
		if s.template == "Custom" {
			// Script name selector.
			s.scriptName = sdk.CellStringValue(evt.Value)
			content := loadScript(s.scriptName)
			if content != "" {
				s.customScript = content
			}
		} else {
			s.cfg.fastPeriod = int64(sdk.CellValue(evt.Value))
		}
	case r == 1 && c == 1:
		if s.template == "Custom" {
			// New script name.
			name := sdk.CellStringValue(evt.Value)
			if name != "" {
				if !strings.HasSuffix(name, ".strat") && !strings.HasSuffix(name, ".aria") {
					name += ".strat"
				}
				s.scriptName = name
			}
		} else {
			s.cfg.slowPeriod = int64(sdk.CellValue(evt.Value))
		}
	case r == 2 && c == 0:
		s.cfg.startDate = sdk.CellStringValue(evt.Value)
	case r == 2 && c == 1:
		s.cfg.endDate = sdk.CellStringValue(evt.Value)
	}

	// Handle script editor save (the script cell address varies by mode).
	val := sdk.CellStringValue(evt.Value)
	if len(val) > 50 && strings.Contains(val, "\n") {
		// Looks like script content from the editor.
		s.customScript = val
		if s.scriptName != "" {
			saveScript(s.scriptName, val)
		}
	}
}

func runStrategy(s *state, price float64) {
	n := len(s.prices)
	fast := int(s.cfg.fastPeriod)
	slow := int(s.cfg.slowPeriod)
	if n < slow {
		return
	}

	smaFast := sma(s.prices, fast)
	smaSlow := sma(s.prices, slow)

	// Signal: fast crosses above slow → buy, below → sell.
	var signal float64
	if smaFast > smaSlow {
		signal = 1
	} else {
		signal = -1
	}

	if signal != s.position {
		// Close existing position.
		if s.position != 0 {
			pnl := (price - s.entryPrice) * s.position
			s.equity += pnl
			s.trades = append(s.trades, trade{
				price: price,
				side:  map[bool]string{true: "SELL", false: "BUY"}[s.position > 0],
				pnl:   pnl,
				idx:   n,
			})
		}
		// Open new position.
		s.position = signal
		s.entryPrice = price
		side := "BUY"
		if signal < 0 {
			side = "SELL"
		}
		s.trades = append(s.trades, trade{
			price: price,
			side:  side,
			pnl:   0,
			idx:   n,
		})
	}

	// Track drawdown.
	if s.equity > s.peakEquity {
		s.peakEquity = s.equity
	}
	if s.peakEquity > 0 {
		dd := (s.peakEquity - s.equity) / s.peakEquity
		if dd > s.maxDD {
			s.maxDD = dd
		}
	}
}

func sma(prices []float64, period int) float64 {
	n := len(prices)
	if n < period {
		return 0
	}
	sum := 0.0
	for i := n - period; i < n; i++ {
		sum += prices[i]
	}
	return sum / float64(period)
}

func buildGrid(s state) []sdk.Cell {
	n := len(s.prices)
	fast := int(s.cfg.fastPeriod)
	slow := int(s.cfg.slowPeriod)

	// Compute live metrics.
	var unrealizedPnL float64
	if s.position != 0 && n > 0 {
		unrealizedPnL = (s.prices[n-1] - s.entryPrice) * s.position
	}
	totalPnL := s.equity + unrealizedPnL

	// Sharpe (simplified: mean trade PnL / stdev).
	sharpe := 0.0
	if len(s.trades) > 2 {
		var pnls []float64
		for _, t := range s.trades {
			if t.pnl != 0 {
				pnls = append(pnls, t.pnl)
			}
		}
		if len(pnls) > 1 {
			mean, std := meanStd(pnls)
			if std > 0 {
				sharpe = mean / std * math.Sqrt(252)
			}
		}
	}

	winRate := 0.0
	closedTrades := 0
	wins := 0
	for _, t := range s.trades {
		if t.pnl != 0 {
			closedTrades++
			if t.pnl > 0 {
				wins++
			}
		}
	}
	if closedTrades > 0 {
		winRate = float64(wins) / float64(closedTrades) * 100
	}

	pos := "FLAT"
	if s.position > 0 {
		pos = "LONG"
	} else if s.position < 0 {
		pos = "SHORT"
	}

	// Strategy script — template-generated or custom.
	var scriptText string
	if s.template == "Custom" {
		scriptText = s.customScript
		if scriptText == "" {
			scriptText = "// Write your Aria strategy here\nsignal x = sma(close, 10)\nstrategy custom {\n  when x > 0 -> buy(1.0)\n}"
		}
	} else {
		scriptText = fmt.Sprintf(
			"signal fast = sma(close, %d)\nsignal slow = sma(close, %d)\nwhen cross_up(fast,slow) -> buy\nwhen cross_down(fast,slow) -> sell",
			s.cfg.fastPeriod, s.cfg.slowPeriod)
	}

	cells := []sdk.Cell{
		// Row 0: Header + template + mode.
		sdk.HeaderCell(0, 0, "BACKTESTER"),
		sdk.EnumInputCell(0, 1, "Template", s.template, []sdk.EnumOption{
			{Value: "SMA Crossover", Label: "SMA Crossover"},
			{Value: "Mean Reversion", Label: "Mean Reversion"},
			{Value: "Momentum", Label: "Momentum"},
			{Value: "Custom", Label: "Custom"},
		}),
		sdk.EnumInputCell(0, 2, "Mode", s.cfg.mode, []sdk.EnumOption{
			{Value: "RT", Label: "RT (Live)"},
			{Value: "Historical", Label: "Historical"},
		}),
	}

	if s.template == "Custom" {
		// Row 1: Script name picker + new name input.
		scripts := listScripts()
		opts := []sdk.EnumOption{{Value: s.scriptName, Label: s.scriptName}}
		for _, name := range scripts {
			if name != s.scriptName {
				opts = append(opts, sdk.EnumOption{Value: name, Label: name})
			}
		}
		cells = append(cells,
			sdk.EnumInputCell(1, 0, "Script", s.scriptName, opts),
			sdk.StringInputCell(1, 1, "New Name", "", "my_strategy.strat"),
		)
	} else {
		// Row 1: Parameters for built-in templates.
		cells = append(cells,
			sdk.IntegerInputCell(1, 0, "Fast Period", s.cfg.fastPeriod),
			sdk.IntegerInputCell(1, 1, "Slow Period", s.cfg.slowPeriod),
		)
	}

	// Row 2: Historical-only fields.
	startCell := sdk.StringInputCell(2, 0, "Start", s.cfg.startDate, "YYYY-MM-DD")
	startCell.VisibleWhen = "R0C2=Historical"
	endCell := sdk.StringInputCell(2, 1, "End", s.cfg.endDate, "YYYY-MM-DD")
	endCell.VisibleWhen = "R0C2=Historical"
	cells = append(cells, startCell, endCell)

	// Historical results section (if available).
	if s.cfg.mode == "Historical" && s.histResult != nil {
		cells = append(cells, sdk.SectionCell(3, 0, "── HISTORICAL RESULTS (bt-engine) ──", 4))
		if s.histResult.status == "running" {
			cells = append(cells, sdk.ProgressBarCell(4, 0, "Backtest", 0.5, "Running bt-engine..."))
		} else if s.histResult.status == "error" {
			cells = append(cells, sdk.TextCell(4, 0, "Error: "+s.histResult.errorMsg, &sdk.CellStyle{Fg: "red"}))
		} else if s.histResult.status == "done" {
			hDelta := ""
			if s.histResult.pnl > 0 {
				hDelta = "up"
			} else if s.histResult.pnl < 0 {
				hDelta = "down"
			}
			cells = append(cells,
				sdk.NumberCellWithDelta(4, 0, "PnL", s.histResult.pnl, 2, "$", hDelta),
				sdk.NumberCell(4, 1, "Sharpe", s.histResult.sharpe, 2, ""),
				sdk.NumberCell(5, 0, "Max DD", s.histResult.maxDD*100, 1, "%"),
				sdk.NumberCell(5, 1, "Trades", float64(s.histResult.trades), 0, ""),
			)
		}
	}

	// Strategy + status (offset rows to make room).
	baseRow := uint32(3)
	if s.cfg.mode == "Historical" {
		baseRow = 7
	}

	scriptCell := sdk.Cell{
		Address: sdk.CellAddress{Row: baseRow + 1, Col: 0},
		Type:    "input_script",
		Label:   "Strategy",
		Value:   scriptText,
		ColSpan: 3,
	}
	cells = append(cells,
		sdk.SectionCell(baseRow, 0, "── STRATEGY ──", 4),
		scriptCell,
		sdk.SectionCell(baseRow+3, 0, "── STATUS ──", 4),
		sdk.TextCell(baseRow+4, 0, fmt.Sprintf("Bars: %d", n), nil),
		sdk.TextCell(baseRow+4, 1, fmt.Sprintf("Position: %s", pos), nil),
	)

	rOff := baseRow + 4 // offset for remaining rows

	// SMA values.
	if n >= slow {
		cells = append(cells,
			sdk.NumberCell(rOff+1, 0, "SMA Fast", sma(s.prices, fast), 2, ""),
			sdk.NumberCell(rOff+1, 1, "SMA Slow", sma(s.prices, slow), 2, ""),
		)
	}

	// Results section.
	cells = append(cells, sdk.SectionCell(rOff+3, 0, "── RESULTS ──", 4))

	pnlDelta := ""
	if totalPnL > 0 {
		pnlDelta = "up"
	} else if totalPnL < 0 {
		pnlDelta = "down"
	}

	cells = append(cells,
		sdk.NumberCellWithDelta(rOff+4, 0, "Total PnL", totalPnL, 2, "$", pnlDelta),
		sdk.NumberCell(rOff+4, 1, "Sharpe", sharpe, 2, ""),
		sdk.NumberCell(rOff+5, 0, "Max Drawdown", s.maxDD*100, 1, "%"),
		sdk.NumberCell(rOff+5, 1, "Win Rate", winRate, 1, "%"),
		sdk.NumberCell(rOff+6, 0, "Trades", float64(closedTrades), 0, ""),
		sdk.NumberCell(rOff+6, 1, "Unrealized", unrealizedPnL, 2, "$"),
	)

	// Trade log (last 8 trades).
	tLogRow := rOff + 8
	cells = append(cells, sdk.SectionCell(tLogRow, 0, "── TRADE LOG ──", 4))
	cells = append(cells,
		sdk.Cell{Address: sdk.CellAddress{Row: tLogRow + 1, Col: 0}, Type: "text", Text: "Side", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: tLogRow + 1, Col: 1}, Type: "text", Text: "Price", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: tLogRow + 1, Col: 2}, Type: "text", Text: "PnL", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
	)

	start := 0
	if len(s.trades) > 8 {
		start = len(s.trades) - 8
	}
	for i, t := range s.trades[start:] {
		row := tLogRow + 2 + uint32(i)
		sideStyle := &sdk.CellStyle{Fg: "green"}
		if t.side == "SELL" {
			sideStyle = &sdk.CellStyle{Fg: "red"}
		}
		cells = append(cells,
			sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 0}, Type: "text", Text: t.side, Style: sideStyle},
			sdk.NumberCell(row, 1, "", t.price, 2, ""),
		)
		if t.pnl != 0 {
			delta := "up"
			if t.pnl < 0 {
				delta = "down"
			}
			cells = append(cells, sdk.NumberCellWithDelta(row, 2, "", t.pnl, 2, "$", delta))
		}
	}

	return cells
}

func meanStd(vals []float64) (float64, float64) {
	n := float64(len(vals))
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	mean := sum / n
	sumSq := 0.0
	for _, v := range vals {
		d := v - mean
		sumSq += d * d
	}
	return mean, math.Sqrt(sumSq / n)
}

// ---------------------------------------------------------------------------
// bt-engine subprocess integration (Historical mode).
// ---------------------------------------------------------------------------

// btEnginePath resolves the path to the bt-engine binary.
func btEnginePath() string {
	if p := os.Getenv("BT_ENGINE_PATH"); p != "" {
		return p
	}
	// Fallback: look relative to the working directory.
	candidates := []string{
		"../gpu-backtest/target/release/bt-engine",
		filepath.Join(os.Getenv("HOME"), "work/gpu-backtest/target/release/bt-engine"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "bt-engine" // hope it's in PATH
}

// generateTOML creates a TOML config for bt-engine.
func generateTOML(cfg config, strategyPath string) string {
	return fmt.Sprintf(`[general]
tickers = ["%s"]
start_time = "%s"
end_time = "%s"
mode = "ohlc"

[data]
frequency = "1m"

[strategy]
file = "%s"

[risk]
max_position = 1.0
max_drawdown = 0.30

[execution]
volume_multiplier = 0.10
default_quantity = 0.01

[output]
json_summary = true
`, cfg.ticker, cfg.startDate, cfg.endDate, strategyPath)
}

// generateAriaStrategy creates an Aria strategy DSL file from parameters.
func generateAriaStrategy(cfg config) string {
	return fmt.Sprintf(`signal sma_fast = sma(close, %d)
signal sma_slow = sma(close, %d)
signal trend_up = cross_up(sma_fast, sma_slow)
signal trend_down = cross_down(sma_fast, sma_slow)

strategy sma_crossover {
    when trend_up  -> buy(1.0)
    when trend_down -> sell(1.0)
}
`, cfg.fastPeriod, cfg.slowPeriod)
}

// runHistoricalBacktest invokes bt-engine as a subprocess and returns results.
func runHistoricalBacktest(s *state) {
	s.histResult = &historicalResult{status: "running"}

	// Write temp strategy file.
	stratFile, err := os.CreateTemp("", "bt-strategy-*.aria")
	if err != nil {
		s.histResult = &historicalResult{status: "error", errorMsg: err.Error()}
		return
	}
	stratFile.WriteString(generateAriaStrategy(s.cfg))
	stratFile.Close()
	defer os.Remove(stratFile.Name())

	// Write temp TOML config.
	configFile, err := os.CreateTemp("", "bt-config-*.toml")
	if err != nil {
		s.histResult = &historicalResult{status: "error", errorMsg: err.Error()}
		return
	}
	configFile.WriteString(generateTOML(s.cfg, stratFile.Name()))
	configFile.Close()
	defer os.Remove(configFile.Name())

	// Invoke bt-engine.
	cmd := exec.Command(btEnginePath(), configFile.Name())
	output, err := cmd.Output()
	if err != nil {
		stderr := ""
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		s.histResult = &historicalResult{
			status:   "error",
			errorMsg: fmt.Sprintf("%v: %s", err, stderr),
		}
		return
	}

	// Parse JSON output.
	var result struct {
		Aggregate struct {
			PnL         float64 `json:"pnl"`
			Sharpe      float64 `json:"sharpe"`
			MaxDrawdown float64 `json:"max_drawdown"`
			NumTrades   int     `json:"num_trades"`
			TotalVolume float64 `json:"total_volume"`
		} `json:"aggregate"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		s.histResult = &historicalResult{status: "error", errorMsg: "parse output: " + err.Error()}
		return
	}

	s.histResult = &historicalResult{
		status: "done",
		pnl:    result.Aggregate.PnL,
		sharpe: result.Aggregate.Sharpe,
		maxDD:  result.Aggregate.MaxDrawdown,
		trades: result.Aggregate.NumTrades,
		volume: result.Aggregate.TotalVolume,
	}
}
