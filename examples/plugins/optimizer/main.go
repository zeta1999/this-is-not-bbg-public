// optimizer is a proof-of-concept portfolio optimization plugin.
// Implements equal-weight, min-variance, and max-Sharpe allocation
// using rolling return/covariance estimates from live OHLC data.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

type assetData struct {
	ticker  string
	prices  []float64
	returns []float64
}

type state struct {
	assets    map[string]*assetData
	objective string // "Equal Weight", "Min Variance", "Max Sharpe"
	window    int64
}

func defaultState() state {
	return state{
		assets:    make(map[string]*assetData),
		objective: "Equal Weight",
		window:    60,
	}
}

func main() {
	p := sdk.New("plugin.optimizer.screen")
	s := defaultState()

	p.Run(func(msg sdk.Message) {
		if msg.Topic == "plugin.optimizer.input" {
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
			a, ok := s.assets[payload.Instrument]
			if !ok {
				a = &assetData{ticker: payload.Instrument}
				s.assets[payload.Instrument] = a
			}
			a.prices = append(a.prices, payload.Close)
			if len(a.prices) > 500 {
				a.prices = a.prices[len(a.prices)-500:]
			}
			if len(a.prices) >= 2 {
				r := a.prices[len(a.prices)-1]/a.prices[len(a.prices)-2] - 1
				a.returns = append(a.returns, r)
				if len(a.returns) > 500 {
					a.returns = a.returns[len(a.returns)-500:]
				}
			}
		}

		p.UpdateCellGrid("OPTIMIZER", buildGrid(s), true)
	})
}

func applyInput(s *state, evt sdk.InputEvent) {
	r, c := evt.Address.Row, evt.Address.Col
	switch {
	case r == 0 && c == 1:
		s.objective = sdk.CellStringValue(evt.Value)
	case r == 0 && c == 2:
		s.window = int64(sdk.CellValue(evt.Value))
	}
}

func buildGrid(s state) []sdk.Cell {
	cells := []sdk.Cell{
		sdk.HeaderCell(0, 0, "PORTFOLIO OPTIMIZER"),
		sdk.EnumInputCell(0, 1, "Objective", s.objective, []sdk.EnumOption{
			{Value: "Equal Weight", Label: "Equal Weight"},
			{Value: "Min Variance", Label: "Min Variance"},
			{Value: "Max Sharpe", Label: "Max Sharpe"},
		}),
		sdk.IntegerInputCell(0, 2, "Window", s.window),
	}

	// Sort tickers.
	var tickers []string
	for t := range s.assets {
		tickers = append(tickers, t)
	}
	sort.Strings(tickers)

	// Filter to assets with enough data.
	window := int(s.window)
	var valid []string
	for _, t := range tickers {
		if len(s.assets[t].returns) >= window {
			valid = append(valid, t)
		}
	}

	if len(valid) < 2 {
		cells = append(cells, sdk.TextCell(2, 0,
			fmt.Sprintf("Collecting data... %d/%d instruments ready (need %d bars each)",
				len(valid), len(tickers), window),
			&sdk.CellStyle{Fg: "dim"}))
		return cells
	}

	// Compute weights based on objective.
	weights := computeWeights(s, valid, window)

	// Results table.
	cells = append(cells, sdk.SectionCell(2, 0, "── OPTIMAL WEIGHTS ──", 5))
	cells = append(cells,
		cell(3, 0, "Ticker", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 1, "Weight", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 2, "Ret (ann)", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 3, "Vol (ann)", &sdk.CellStyle{Bold: true, Fg: "dim"}),
		cell(3, 4, "Allocation", &sdk.CellStyle{Bold: true, Fg: "dim"}),
	)

	row := uint32(4)
	portRet := 0.0
	portVar := 0.0

	for i, t := range valid {
		a := s.assets[t]
		w := weights[i]
		rets := a.returns[len(a.returns)-window:]
		mean, std := meanStd(rets)
		annRet := mean * 365 * 24 * 60 * 100 // annualized %
		annVol := std * math.Sqrt(365*24*60) * 100

		portRet += w * annRet
		portVar += w * w * annVol * annVol // simplified (ignores cross-terms)

		barLen := int(math.Round(w * 20))
		bar := ""
		for j := 0; j < barLen; j++ {
			bar += "█"
		}

		cells = append(cells,
			sdk.TextCell(row, 0, t, &sdk.CellStyle{Fg: "cyan"}),
			sdk.NumberCell(row, 1, "", w*100, 1, "%"),
			sdk.NumberCell(row, 2, "", annRet, 1, "%"),
			sdk.NumberCell(row, 3, "", annVol, 1, "%"),
			sdk.TextCell(row, 4, bar, &sdk.CellStyle{Fg: "green"}),
		)
		row++
	}

	// Portfolio summary.
	row++
	cells = append(cells, sdk.SectionCell(row, 0, "── PORTFOLIO METRICS ──", 5))
	row++
	portVol := math.Sqrt(portVar)
	portSharpe := 0.0
	if portVol > 0 {
		portSharpe = portRet / portVol
	}

	cells = append(cells,
		sdk.NumberCell(row, 0, "Expected Return", portRet, 1, "%"),
		sdk.NumberCell(row, 1, "Volatility", portVol, 1, "%"),
		sdk.NumberCell(row, 2, "Sharpe Ratio", portSharpe, 2, ""),
		sdk.NumberCell(row, 3, "Assets", float64(len(valid)), 0, ""),
	)

	return cells
}

func computeWeights(s state, valid []string, window int) []float64 {
	n := len(valid)
	weights := make([]float64, n)

	switch s.objective {
	case "Equal Weight":
		for i := range weights {
			weights[i] = 1.0 / float64(n)
		}

	case "Min Variance":
		// Inverse-variance weighting.
		totalInvVar := 0.0
		invVars := make([]float64, n)
		for i, t := range valid {
			rets := s.assets[t].returns[len(s.assets[t].returns)-window:]
			_, std := meanStd(rets)
			if std > 0 {
				invVars[i] = 1.0 / (std * std)
			} else {
				invVars[i] = 1.0
			}
			totalInvVar += invVars[i]
		}
		for i := range weights {
			weights[i] = invVars[i] / totalInvVar
		}

	case "Max Sharpe":
		// Sharpe-ratio weighting (simplified).
		totalSharpe := 0.0
		sharpes := make([]float64, n)
		for i, t := range valid {
			rets := s.assets[t].returns[len(s.assets[t].returns)-window:]
			mean, std := meanStd(rets)
			if std > 0 {
				sharpes[i] = math.Max(mean/std, 0) // only positive Sharpe
			}
			totalSharpe += sharpes[i]
		}
		if totalSharpe > 0 {
			for i := range weights {
				weights[i] = sharpes[i] / totalSharpe
			}
		} else {
			// Fallback to equal weight.
			for i := range weights {
				weights[i] = 1.0 / float64(n)
			}
		}
	}

	return weights
}

func cell(row, col uint32, text string, style *sdk.CellStyle) sdk.Cell {
	return sdk.Cell{
		Address: sdk.CellAddress{Row: row, Col: col},
		Type:    "text",
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

var _ = fmt.Sprintf
