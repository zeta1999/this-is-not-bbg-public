// timeseries is a proof-of-concept time series analysis plugin.
// Computes live statistics on streaming OHLC data:
// - Rolling returns, volatility, Sharpe ratio
// - Correlation matrix between selected instruments
// - Simple anomaly detection (Z-score spikes)
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

type instrumentStats struct {
	ticker  string
	prices  []float64
	returns []float64
}

type state struct {
	instruments map[string]*instrumentStats
	window      int64 // lookback window for stats
	tool        string
}

func defaultState() state {
	return state{
		instruments: make(map[string]*instrumentStats),
		window:      60,
		tool:        "Statistics",
	}
}

func main() {
	p := sdk.New("plugin.timeseries.screen")
	s := defaultState()

	p.Run(func(msg sdk.Message) {
		if msg.Topic == "plugin.timeseries.input" {
			var evt sdk.InputEvent
			if json.Unmarshal(msg.Payload, &evt) != nil {
				return
			}
			applyInput(&s, evt)
		} else {
			var payload struct {
				Instrument string  `json:"Instrument"`
				Close      float64 `json:"Close"`
			}
			if json.Unmarshal(msg.Payload, &payload) != nil || payload.Close <= 0 {
				return
			}
			ticker := payload.Instrument
			inst, ok := s.instruments[ticker]
			if !ok {
				inst = &instrumentStats{ticker: ticker}
				s.instruments[ticker] = inst
			}
			inst.prices = append(inst.prices, payload.Close)
			if len(inst.prices) > 500 {
				inst.prices = inst.prices[len(inst.prices)-500:]
			}
			// Compute returns.
			if len(inst.prices) >= 2 {
				r := (inst.prices[len(inst.prices)-1] / inst.prices[len(inst.prices)-2]) - 1
				inst.returns = append(inst.returns, r)
				if len(inst.returns) > 500 {
					inst.returns = inst.returns[len(inst.returns)-500:]
				}
			}
		}

		p.UpdateCellGrid("TSERIES", buildGrid(s), true)
	})
}

func applyInput(s *state, evt sdk.InputEvent) {
	r, c := evt.Address.Row, evt.Address.Col
	switch {
	case r == 0 && c == 1:
		s.window = int64(sdk.CellValue(evt.Value))
	case r == 0 && c == 2:
		s.tool = sdk.CellStringValue(evt.Value)
	}
}

func buildGrid(s state) []sdk.Cell {
	cells := []sdk.Cell{
		sdk.HeaderCell(0, 0, "TIME SERIES"),
		sdk.IntegerInputCell(0, 1, "Window", s.window),
		sdk.EnumInputCell(0, 2, "Tool", s.tool, []sdk.EnumOption{
			{Value: "Statistics", Label: "Statistics"},
			{Value: "Correlation", Label: "Correlation"},
			{Value: "Anomalies", Label: "Anomalies"},
		}),
	}

	// Sort instruments by ticker.
	var tickers []string
	for t := range s.instruments {
		tickers = append(tickers, t)
	}
	sort.Strings(tickers)

	window := int(s.window)
	if window < 2 {
		window = 2
	}

	switch s.tool {
	case "Statistics":
		cells = append(cells, buildStatistics(s, tickers, window)...)
	case "Correlation":
		cells = append(cells, buildCorrelation(s, tickers, window)...)
	case "Anomalies":
		cells = append(cells, buildAnomalies(s, tickers, window)...)
	}

	return cells
}

func buildStatistics(s state, tickers []string, window int) []sdk.Cell {
	var cells []sdk.Cell
	cells = append(cells, sdk.SectionCell(2, 0, "── ROLLING STATISTICS ──", 5))
	cells = append(cells,
		cell(3, 0, "text", "Ticker", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 1, "text", "Last", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 2, "text", "Return", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 3, "text", "Vol (ann)", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 4, "text", "Sharpe", &sdk.CellStyle{Bold: true, Fg: "dim"}),
	)

	row := uint32(4)
	for _, t := range tickers {
		inst := s.instruments[t]
		n := len(inst.returns)
		if n < 2 {
			continue
		}
		w := window
		if w > n {
			w = n
		}
		rets := inst.returns[n-w:]

		mean, std := meanStd(rets)
		annVol := std * math.Sqrt(365*24*60) // 1m bars
		sharpe := 0.0
		if std > 0 {
			sharpe = mean / std * math.Sqrt(365*24*60)
		}

		last := inst.prices[len(inst.prices)-1]
		cumRet := (last/inst.prices[len(inst.prices)-w] - 1) * 100

		retDelta := ""
		if cumRet > 0 {
			retDelta = "up"
		} else if cumRet < 0 {
			retDelta = "down"
		}

		cells = append(cells,
			sdk.TextCell(row, 0, t, &sdk.CellStyle{Fg: "cyan"}),
			sdk.NumberCell(row, 1, "", last, 2, "$"),
			sdk.NumberCellWithDelta(row, 2, "", cumRet, 2, "%", retDelta),
			sdk.NumberCell(row, 3, "", annVol*100, 1, "%"),
			sdk.NumberCell(row, 4, "", sharpe, 2, ""),
		)
		row++
	}
	return cells
}

func buildCorrelation(s state, tickers []string, window int) []sdk.Cell {
	var cells []sdk.Cell
	cells = append(cells, sdk.SectionCell(2, 0, "── CORRELATION MATRIX ──", 5))

	// Filter to tickers with enough data.
	var valid []string
	for _, t := range tickers {
		if len(s.instruments[t].returns) >= window {
			valid = append(valid, t)
		}
	}
	if len(valid) < 2 {
		cells = append(cells, sdk.TextCell(3, 0, "Need 2+ instruments with sufficient data", &sdk.CellStyle{Fg: "dim"}))
		return cells
	}
	if len(valid) > 5 {
		valid = valid[:5] // limit for display
	}

	// Column headers.
	for i, t := range valid {
		short := t
		if len(short) > 6 {
			short = short[:6]
		}
		cells = append(cells, cell(3, uint32(1+i), "text", short, &sdk.CellStyle{Bold: true, Fg: "dim"}))
	}

	// Matrix cells.
	for i, t1 := range valid {
		row := uint32(4 + i)
		short := t1
		if len(short) > 6 {
			short = short[:6]
		}
		cells = append(cells, sdk.TextCell(row, 0, short, &sdk.CellStyle{Fg: "cyan"}))
		r1 := s.instruments[t1].returns
		n1 := len(r1)
		for j, t2 := range valid {
			r2 := s.instruments[t2].returns
			n2 := len(r2)
			w := window
			if w > n1 {
				w = n1
			}
			if w > n2 {
				w = n2
			}
			corr := correlation(r1[n1-w:], r2[n2-w:])
			fg := "white"
			if corr > 0.7 {
				fg = "green"
			} else if corr < -0.3 {
				fg = "red"
			}
			cells = append(cells, sdk.Cell{
				Address:   sdk.CellAddress{Row: row, Col: uint32(1 + j)},
				Type:      "number",
				Value:     corr,
				Precision: 3,
				Style:     &sdk.CellStyle{Fg: fg},
			})
		}
	}
	return cells
}

func buildAnomalies(s state, tickers []string, window int) []sdk.Cell {
	var cells []sdk.Cell
	cells = append(cells, sdk.SectionCell(2, 0, "── ANOMALIES (|Z| > 2) ──", 5))
	cells = append(cells,
		cell(3, 0, "text", "Ticker", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 1, "text", "Last Return", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 2, "text", "Z-Score", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 3, "text", "Direction", &sdk.CellStyle{Bold: true, Fg: "dim"}),
	)

	row := uint32(4)
	for _, t := range tickers {
		inst := s.instruments[t]
		n := len(inst.returns)
		if n < window {
			continue
		}
		w := window
		rets := inst.returns[n-w:]
		mean, std := meanStd(rets)
		if std == 0 {
			continue
		}
		lastRet := inst.returns[n-1]
		z := (lastRet - mean) / std
		if math.Abs(z) < 2.0 {
			continue
		}

		dir := "SPIKE UP"
		fg := "green"
		if z < 0 {
			dir = "SPIKE DOWN"
			fg = "red"
		}

		cells = append(cells,
			sdk.TextCell(row, 0, t, &sdk.CellStyle{Fg: "cyan"}),
			sdk.NumberCell(row, 1, "", lastRet*100, 4, "%"),
			sdk.NumberCell(row, 2, "", z, 2, ""),
			sdk.TextCell(row, 3, dir, &sdk.CellStyle{Fg: fg}),
		)
		row++
	}

	if row == 4 {
		cells = append(cells, sdk.TextCell(4, 0, "No anomalies detected", &sdk.CellStyle{Fg: "dim"}))
	}
	return cells
}

func cell(row, col uint32, typ, text string, style *sdk.CellStyle) sdk.Cell {
	return sdk.Cell{
		Address: sdk.CellAddress{Row: row, Col: col},
		Type:    typ,
		Text:    text,
		Style:   style,
	}
}

func meanStd(vals []float64) (float64, float64) {
	n := float64(len(vals))
	if n == 0 {
		return 0, 0
	}
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

func correlation(a, b []float64) float64 {
	n := len(a)
	if n != len(b) || n < 2 {
		return 0
	}
	ma, sa := meanStd(a)
	mb, sb := meanStd(b)
	if sa == 0 || sb == 0 {
		return 0
	}
	cov := 0.0
	for i := 0; i < n; i++ {
		cov += (a[i] - ma) * (b[i] - mb)
	}
	cov /= float64(n)
	return cov / (sa * sb)
}

var _ = fmt.Sprintf
