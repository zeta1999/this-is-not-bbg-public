// pricer is a proof-of-concept plugin demonstrating the cell grid system.
// It implements Black-Scholes pricing with input cells for market parameters
// and output cells for price, vega, and implied volatility.
package main

import (
	"encoding/json"
	"math"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

// Market parameters (current values from input cells).
type params struct {
	isCall       bool
	spot         float64
	strike       float64
	vol          float64
	rate         float64
	maturity     float64
	carry        float64
	instrument   string // "Vanilla", "Barrier", "Aria Script"
	ariaScript   string // custom Aria payoff script
}

func defaultParams() params {
	return params{
		isCall:     true,
		spot:       68000.0,
		strike:     68000.0,
		vol:        0.60,
		rate:       0.05,
		maturity:   0.25,
		carry:      0.05,
		instrument: "Vanilla",
	}
}

func main() {
	p := sdk.New("plugin.pricer.screen")
	state := defaultParams()
	strikeSet := false // auto-set strike to first spot price

	p.Run(func(msg sdk.Message) {
		switch {
		case msg.Topic == "plugin.pricer.input":
			var evt sdk.InputEvent
			if json.Unmarshal(msg.Payload, &evt) != nil {
				return
			}
			applyInput(&state, evt)
			strikeSet = true // user touched inputs, stop auto-setting

		default:
			var payload struct {
				Instrument string  `json:"Instrument"`
				Close      float64 `json:"Close"`
			}
			if json.Unmarshal(msg.Payload, &payload) != nil {
				return
			}
			if payload.Instrument == "BTCUSDT" && payload.Close > 0 {
				state.spot = payload.Close
				if !strikeSet {
					// Auto-set strike to ATM on first price.
					state.strike = math.Round(payload.Close/100) * 100
					strikeSet = true
				}
			}
		}

		p.UpdateCellGrid("PRICER", buildGrid(state), true)
	})
}

func applyInput(s *params, evt sdk.InputEvent) {
	r, c := evt.Address.Row, evt.Address.Col
	switch {
	case r == 0 && c == 1: // Instrument type
		s.instrument = sdk.CellStringValue(evt.Value)
	case r == 1 && c == 0: // Option type
		s.isCall = sdk.CellStringValue(evt.Value) == "Call"
	case r == 2 && c == 0: // Spot
		s.spot = sdk.CellValue(evt.Value)
	case r == 2 && c == 1: // Strike
		s.strike = sdk.CellValue(evt.Value)
	case r == 3 && c == 0: // Vol
		s.vol = sdk.CellValue(evt.Value)
	case r == 3 && c == 1: // Rate
		s.rate = sdk.CellValue(evt.Value)
	case r == 4 && c == 0: // Maturity
		s.maturity = sdk.CellValue(evt.Value)
	case r == 4 && c == 1: // Carry
		s.carry = sdk.CellValue(evt.Value)
	case r == 10 && c == 0: // Aria script content
		s.ariaScript = sdk.CellStringValue(evt.Value)
	}
}

func buildGrid(s params) []sdk.Cell {
	// Compute analytics.
	price := bs(s.isCall, s.spot, s.strike, s.maturity, s.rate, s.carry, s.vol)
	vega := bsVega(s.spot, s.strike, s.maturity, s.rate, s.carry, s.vol)

	// Implied vol from the computed price (should round-trip to s.vol).
	iv := bsImplied(s.isCall, s.spot, s.strike, s.maturity, s.rate, s.carry, price)

	optType := "Call"
	if !s.isCall {
		optType = "Put"
	}

	cells := []sdk.Cell{
		// Row 0: Header + instrument selector.
		sdk.HeaderCell(0, 0, "PRICER"),
		sdk.EnumInputCell(0, 1, "Instrument", s.instrument, []sdk.EnumOption{
			{Value: "Vanilla", Label: "Vanilla"},
			{Value: "Barrier", Label: "Barrier"},
			{Value: "Basket", Label: "Basket"},
			{Value: "Quanto", Label: "Quanto"},
			{Value: "Aria Script", Label: "Aria Script"},
		}),

		// Row 1: Option type.
		sdk.EnumInputCell(1, 0, "Option Type", optType, []sdk.EnumOption{
			{Value: "Call", Label: "Call"},
			{Value: "Put", Label: "Put"},
		}),

		// Row 2: Spot + Strike.
		sdk.DecimalInputCell(2, 0, "Spot (S)", s.spot, 2),
		sdk.DecimalInputCell(2, 1, "Strike (X)", s.strike, 2),

		// Row 3: Vol + Rate.
		sdk.DecimalInputCell(3, 0, "Vol (σ)", s.vol, 4),
		sdk.DecimalInputCell(3, 1, "Rate (r)", s.rate, 4),

		// Row 4: Maturity + Carry.
		sdk.DecimalInputCell(4, 0, "Maturity (T)", s.maturity, 2),
		sdk.DecimalInputCell(4, 1, "Carry (b)", s.carry, 4),

		// Row 5: separator.
		sdk.SectionCell(6, 0, "── RESULTS ──", 4),

		// Row 7: Price + Implied Vol.
		sdk.NumberCell(7, 0, "Price", price, 4, ""),
		sdk.NumberCell(7, 1, "Implied Vol", iv, 4, ""),

		// Row 8: Vega.
		sdk.NumberCell(8, 0, "Vega", vega, 4, ""),
	}

	// Scenario grid: spot bump × vol bump.
	// Aria Script mode: show script editor cell.
	if s.instrument == "Aria Script" {
		script := s.ariaScript
		if script == "" {
			script = "let K = 100.0\nlet S = spot(\"BTC-USD\")\ncontract\n  one(USD)\n  |> scale(max(S - K, 0.0))\n  |> when(2025-12-31)"
		}
		cells = append(cells,
			sdk.SectionCell(9, 0, "── ARIA PAYOFF SCRIPT ──", 4),
			sdk.ScriptInputCell(10, 0, "Payoff", script, 3),
		)
	}

	scenBase := uint32(12)
	if s.instrument == "Aria Script" {
		scenBase = 14 // shift down for script editor
	}
	cells = append(cells, sdk.SectionCell(scenBase, 0, "── SCENARIOS (PnL vs base) ──", 4))

	// Header row for vol bumps.
	cells = append(cells, sdk.TextCell(scenBase+1, 0, "", nil))
	volBumps := []float64{-0.05, 0, 0.05}
	volLabels := []string{"σ-5%", "σ+0%", "σ+5%"}
	for i, label := range volLabels {
		cells = append(cells, sdk.Cell{
			Address: sdk.CellAddress{Row: scenBase + 1, Col: uint32(1 + i)},
			Type:    "text",
			Text:    label,
			Style:   &sdk.CellStyle{Bold: true, Fg: "dim"},
		})
	}

	// Scenario rows for spot bumps.
	spotBumps := []float64{-0.10, 0, 0.10}
	spotLabels := []string{"S-10%", "S+0%", "S+10%"}
	for si, sBump := range spotBumps {
		row := scenBase + 2 + uint32(si)
		cells = append(cells, sdk.Cell{
			Address: sdk.CellAddress{Row: row, Col: 0},
			Type:    "text",
			Text:    spotLabels[si],
			Style:   &sdk.CellStyle{Bold: true, Fg: "dim"},
		})
		for vi, vBump := range volBumps {
			bumpedSpot := s.spot * (1 + sBump)
			bumpedVol := s.vol + vBump
			if bumpedVol < 0.001 {
				bumpedVol = 0.001
			}
			scenarioPrice := bs(s.isCall, bumpedSpot, s.strike, s.maturity, s.rate, s.carry, bumpedVol)
			pnl := scenarioPrice - price

			delta := ""
			if pnl > 0 {
				delta = "up"
			} else if pnl < 0 {
				delta = "down"
			}

			cells = append(cells, sdk.NumberCellWithDelta(row, uint32(1+vi), "", pnl, 2, "", delta))
		}
	}

	return cells
}

// ---------------------------------------------------------------------------
// Black-Scholes analytics (pure Go, no external dependencies).
// ---------------------------------------------------------------------------

func bs(isCall bool, S, X, T, r, b, v float64) float64 {
	if T <= 0 || v <= 0 {
		if isCall {
			return math.Max(S*math.Exp((b-r)*T)-X*math.Exp(-r*T), 0)
		}
		return math.Max(X*math.Exp(-r*T)-S*math.Exp((b-r)*T), 0)
	}
	d1 := (math.Log(S/X) + (b+v*v/2)*T) / (v * math.Sqrt(T))
	d2 := d1 - v*math.Sqrt(T)
	if isCall {
		return S*math.Exp((b-r)*T)*cdf(d1) - X*math.Exp(-r*T)*cdf(d2)
	}
	return X*math.Exp(-r*T)*cdf(-d2) - S*math.Exp((b-r)*T)*cdf(-d1)
}

func bsVega(S, X, T, r, b, v float64) float64 {
	if T <= 0 || v <= 0 {
		return 0
	}
	d1 := (math.Log(S/X) + (b+v*v/2)*T) / (v * math.Sqrt(T))
	return S * math.Exp((b-r)*T) * pdf(d1) * math.Sqrt(T)
}

func bsImplied(isCall bool, S, X, T, r, b, targetPrice float64) float64 {
	// Newton-Raphson with bisection fallback.
	lo, hi := 0.001, 5.0
	vol := 0.25 // initial guess

	for i := 0; i < 100; i++ {
		price := bs(isCall, S, X, T, r, b, vol)
		vega := bsVega(S, X, T, r, b, vol)
		diff := price - targetPrice

		if math.Abs(diff) < 1e-10 {
			return vol
		}

		if vega > 1e-10 {
			newVol := vol - diff/vega
			if newVol > lo && newVol < hi {
				vol = newVol
				continue
			}
		}

		// Bisection fallback.
		mid := (lo + hi) / 2
		midPrice := bs(isCall, S, X, T, r, b, mid)
		if (midPrice - targetPrice) > 0 {
			hi = mid
		} else {
			lo = mid
		}
		vol = (lo + hi) / 2
	}
	return vol
}

// Standard normal CDF (Abramowitz & Stegun approximation).
func cdf(x float64) float64 {
	if x < -10 {
		return 0
	}
	if x > 10 {
		return 1
	}
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// Standard normal PDF.
func pdf(x float64) float64 {
	return math.Exp(-x*x/2) / math.Sqrt(2*math.Pi)
}
