// voltools is a proof-of-concept plugin implementing a simplified volatility
// surface viewer. Demonstrates:
// - Parameter matrix (FiveStar-style: ATM, Skew, Smile per maturity)
// - ASCII vol surface heatmap (maturity × moneyness → implied vol)
// - Companion curves (rates, dividends)
// - Calibration status
//
// Based on Phase 6 study of Ravel/Voltools. Uses synthetic data since
// Sibelius VolToolsAPI integration comes later.
package main

import (
	"encoding/json"
	"fmt"
	"math"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

type maturityRow struct {
	label string
	t     float64 // time in years
	fwd   float64
	atm   float64 // ATM vol
	skew  float64
	smile float64
}

type state struct {
	asset      string
	spot       float64
	rows       []maturityRow
	model      string // "FiveStar", "SABR", "SVI"
	calibrated bool
}

func defaultState() state {
	return state{
		asset: "BTCUSDT",
		spot:  68000,
		model: "FiveStar",
		rows: []maturityRow{
			{label: "1W", t: 7.0 / 365, fwd: 68050, atm: 0.72, skew: -0.08, smile: 0.04},
			{label: "1M", t: 30.0 / 365, fwd: 68200, atm: 0.65, skew: -0.06, smile: 0.03},
			{label: "3M", t: 90.0 / 365, fwd: 68800, atm: 0.60, skew: -0.05, smile: 0.025},
			{label: "6M", t: 180.0 / 365, fwd: 69500, atm: 0.55, skew: -0.04, smile: 0.02},
			{label: "1Y", t: 1.0, fwd: 71000, atm: 0.50, skew: -0.03, smile: 0.018},
			{label: "2Y", t: 2.0, fwd: 75000, atm: 0.48, skew: -0.025, smile: 0.015},
		},
	}
}

func main() {
	p := sdk.New("plugin.voltools.screen")
	s := defaultState()

	p.Run(func(msg sdk.Message) {
		if msg.Topic == "plugin.voltools.input" {
			var evt sdk.InputEvent
			if json.Unmarshal(msg.Payload, &evt) != nil {
				return
			}
			applyInput(&s, evt)
		} else {
			// Update spot from live feed.
			var payload struct {
				Instrument string  `json:"Instrument"`
				Close      float64 `json:"Close"`
			}
			if json.Unmarshal(msg.Payload, &payload) != nil {
				return
			}
			if payload.Instrument == s.asset && payload.Close > 0 {
				s.spot = payload.Close
			}
		}
		p.UpdateCellGrid("VOLTOOLS", buildGrid(s), true)
	})
}

func applyInput(s *state, evt sdk.InputEvent) {
	r, c := evt.Address.Row, evt.Address.Col
	switch {
	case r == 0 && c == 1:
		s.asset = sdk.CellStringValue(evt.Value)
	case r == 0 && c == 2:
		s.model = sdk.CellStringValue(evt.Value)
	}
	// Parameter matrix edits (rows start at row 3).
	if r >= 3 && r < 3+uint32(len(s.rows)) {
		idx := int(r) - 3
		switch c {
		case 1:
			s.rows[idx].fwd = sdk.CellValue(evt.Value)
		case 2:
			s.rows[idx].atm = sdk.CellValue(evt.Value)
		case 3:
			s.rows[idx].skew = sdk.CellValue(evt.Value)
		case 4:
			s.rows[idx].smile = sdk.CellValue(evt.Value)
		}
	}
}

// fiveStarVol computes implied vol at a given moneyness using the FiveStar model.
func fiveStarVol(row maturityRow, moneyness float64) float64 {
	// Simplified FiveStar: ATM + skew*(m-1) + smile*(m-1)^2
	m := moneyness // strike/fwd
	dm := m - 1.0
	vol := row.atm + row.skew*dm + row.smile*dm*dm
	if vol < 0.01 {
		vol = 0.01
	}
	return vol
}

func buildGrid(s state) []sdk.Cell {
	cells := []sdk.Cell{
		sdk.HeaderCell(0, 0, "VOL SURFACE"),
		sdk.EnumInputCell(0, 1, "Asset", s.asset, []sdk.EnumOption{
			{Value: "BTCUSDT", Label: "BTC"},
			{Value: "ETHUSDT", Label: "ETH"},
			{Value: "SOLUSDT", Label: "SOL"},
		}),
		sdk.EnumInputCell(0, 2, "Model", s.model, []sdk.EnumOption{
			{Value: "FiveStar", Label: "FiveStar"},
			{Value: "SABR", Label: "SABR"},
			{Value: "SVI", Label: "SVI"},
		}),
		sdk.NumberCell(0, 3, "Spot", s.spot, 2, "$"),
	}

	// Parameter matrix.
	cells = append(cells, sdk.SectionCell(2, 0, "── PARAMETERS ──", 5))
	cells = append(cells,
		sdk.Cell{Address: sdk.CellAddress{Row: 2, Col: 0}, Type: "text", Text: "Expiry", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 2, Col: 1}, Type: "text", Text: "Fwd", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 2, Col: 2}, Type: "text", Text: "ATM Vol", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 2, Col: 3}, Type: "text", Text: "Skew", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 2, Col: 4}, Type: "text", Text: "Smile", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
	)

	for i, row := range s.rows {
		r := uint32(3 + i)
		cells = append(cells,
			sdk.TextCell(r, 0, row.label, &sdk.CellStyle{Fg: "cyan"}),
			sdk.DecimalInputCell(r, 1, "", row.fwd, 0),
			sdk.DecimalInputCell(r, 2, "", row.atm, 4),
			sdk.DecimalInputCell(r, 3, "", row.skew, 4),
			sdk.DecimalInputCell(r, 4, "", row.smile, 4),
		)
	}

	// Vol surface heatmap.
	surfRow := uint32(3 + len(s.rows) + 1)
	cells = append(cells, sdk.SectionCell(surfRow, 0, "── IMPLIED VOL SURFACE (%) ──", 5))
	surfRow++

	// Moneyness columns.
	moneynesses := []float64{0.85, 0.90, 0.95, 1.00, 1.05, 1.10, 1.15}
	moneyLabels := []string{"85%", "90%", "95%", "ATM", "105%", "110%", "115%"}
	cells = append(cells, sdk.TextCell(surfRow, 0, "", nil))
	for i, label := range moneyLabels {
		if uint32(1+i) > 4 {
			break // limit to terminal width
		}
		cells = append(cells, sdk.Cell{
			Address: sdk.CellAddress{Row: surfRow, Col: uint32(1 + i)},
			Type:    "text",
			Text:    label,
			Style:   &sdk.CellStyle{Bold: true, Fg: "dim"},
		})
	}
	surfRow++

	// Vol values per maturity.
	for _, row := range s.rows {
		cells = append(cells, sdk.TextCell(surfRow, 0, row.label, &sdk.CellStyle{Fg: "cyan"}))
		for i, m := range moneynesses {
			if uint32(1+i) > 4 {
				break
			}
			vol := fiveStarVol(row, m) * 100
			// Color by vol level.
			fg := "green"
			if vol > 60 {
				fg = "yellow"
			}
			if vol > 80 {
				fg = "red"
			}
			cells = append(cells, sdk.Cell{
				Address:   sdk.CellAddress{Row: surfRow, Col: uint32(1 + i)},
				Type:      "number",
				Value:     vol,
				Precision: 1,
				Unit:      "%",
				Style:     &sdk.CellStyle{Fg: fg},
			})
		}
		surfRow++
	}

	// 3D vol surface component (rendered by desktop, ignored by TUI).
	surfRow++
	volGrid := make([][]float64, len(s.rows))
	for i, row := range s.rows {
		volGrid[i] = make([]float64, len(moneynesses))
		for j, m := range moneynesses {
			volGrid[i][j] = fiveStarVol(row, m) * 100
		}
	}
	maturities := make([]string, len(s.rows))
	for i, row := range s.rows {
		maturities[i] = row.label
	}
	surfPayload, _ := json.Marshal(map[string]any{
		"maturities": maturities,
		"moneyness":  moneynesses,
		"vols":       volGrid,
		"spot":       s.spot,
		"asset":      s.asset,
	})
	cells = append(cells, sdk.VolSurfaceCell(surfRow, 0, string(surfPayload), 5))
	surfRow += 2

	// Calibration status.
	surfRow++
	cells = append(cells, sdk.SectionCell(surfRow, 0, "── CALIBRATION ──", 5))
	surfRow++

	status := "Not calibrated"
	statusFg := "yellow"
	if s.calibrated {
		status = "Calibrated (OK)"
		statusFg = "green"
	}
	cells = append(cells,
		sdk.TextCell(surfRow, 0, status, &sdk.CellStyle{Fg: statusFg}),
	)

	// Term structure sparkline (ATM vol per maturity).
	surfRow += 2
	cells = append(cells, sdk.SectionCell(surfRow, 0, "── ATM TERM STRUCTURE ──", 5))
	surfRow++

	maxVol := 0.0
	for _, row := range s.rows {
		if row.atm > maxVol {
			maxVol = row.atm
		}
	}

	for _, row := range s.rows {
		barLen := int(math.Round(row.atm / maxVol * 20))
		bar := ""
		for j := 0; j < barLen; j++ {
			bar += "█"
		}
		cells = append(cells,
			sdk.TextCell(surfRow, 0, row.label, &sdk.CellStyle{Fg: "cyan"}),
			sdk.NumberCell(surfRow, 1, "", row.atm*100, 1, "%"),
			sdk.TextCell(surfRow, 2, bar, &sdk.CellStyle{Fg: "green"}),
		)
		surfRow++
	}

	return cells
}

// Suppress unused import warning.
var _ = fmt.Sprintf
