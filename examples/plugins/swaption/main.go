// swaption is a proof-of-concept plugin for swaption cube visualization.
// Generates synthetic swaption vol data (tenors × expiries × strikes → vol)
// and emits it as a swaption_cube ComponentCell for the desktop 3D viewer.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

type state struct {
	asset  string
	model  string
	tenors []string // swap tenors
}

func defaultState() state {
	return state{
		asset:  "USD IR",
		model:  "SABR",
		tenors: []string{"1Y", "2Y", "5Y", "10Y", "20Y", "30Y"},
	}
}

func main() {
	p := sdk.New("plugin.swaption.screen")
	s := defaultState()
	sent := false

	// Swaption cube doesn't need live feeds — emit on first message or timer.
	go func() {
		// Initial emit after 2s.
		time.Sleep(2 * time.Second)
		p.UpdateCellGrid("SWAPTION", buildGrid(s), true)
		sent = true
	}()

	p.Run(func(msg sdk.Message) {
		if msg.Topic == "plugin.swaption.input" {
			var evt sdk.InputEvent
			if json.Unmarshal(msg.Payload, &evt) != nil {
				return
			}
			applyInput(&s, evt)
		}
		if !sent {
			sent = true
		}
		p.UpdateCellGrid("SWAPTION", buildGrid(s), true)
	})
}

func applyInput(s *state, evt sdk.InputEvent) {
	r, c := evt.Address.Row, evt.Address.Col
	switch {
	case r == 0 && c == 1:
		s.model = sdk.CellStringValue(evt.Value)
	}
}

// generateSwaptionVols generates a synthetic swaption vol cube.
func generateSwaptionVols(s state) (expiries []string, strikes []float64, vols [][][]float64) {
	expiries = []string{"1M", "3M", "6M", "1Y", "2Y", "5Y", "10Y"}
	strikes = []float64{-200, -100, -50, 0, 50, 100, 200} // basis points from ATM

	vols = make([][][]float64, len(s.tenors))
	for ti, _ := range s.tenors {
		vols[ti] = make([][]float64, len(expiries))
		for ei := range expiries {
			vols[ti][ei] = make([]float64, len(strikes))
			for si, strike := range strikes {
				// Synthetic vol: base + tenor effect + expiry effect + smile
				base := 15.0 + float64(ti)*0.8 // longer tenor = higher vol
				expiryEffect := -float64(ei) * 0.3 // longer expiry = slightly lower
				smile := 0.02 * strike * strike / 10000 // quadratic smile
				skew := -0.005 * strike * float64(ti) / 5 // skew increases with tenor
				vol := base + expiryEffect + smile + skew
				vol += 0.5 * math.Sin(float64(ti+ei+si)*0.3) // some variation
				if vol < 5 {
					vol = 5
				}
				vols[ti][ei][si] = vol
			}
		}
	}
	return
}

func buildGrid(s state) []sdk.Cell {
	cells := []sdk.Cell{
		sdk.HeaderCell(0, 0, "SWAPTION CUBE"),
		sdk.EnumInputCell(0, 1, "Model", s.model, []sdk.EnumOption{
			{Value: "SABR", Label: "SABR"},
			{Value: "Normal", Label: "Normal"},
			{Value: "SVI", Label: "SVI"},
		}),
		sdk.TextCell(0, 2, s.asset, &sdk.CellStyle{Fg: "cyan"}),
	}

	// Generate vol cube.
	expiries, strikes, vols := generateSwaptionVols(s)

	// Emit the 3D component cell.
	cubePayload, _ := json.Marshal(map[string]any{
		"tenors":   s.tenors,
		"expiries": expiries,
		"strikes":  strikes,
		"vols":     vols,
		"asset":    s.asset,
	})
	cells = append(cells, sdk.SwaptionCubeCell(2, 0, string(cubePayload), 5))

	// Also emit a parameter table (TUI-friendly).
	cells = append(cells, sdk.SectionCell(4, 0, "── CUBE PARAMETERS ──", 5))
	cells = append(cells,
		sdk.Cell{Address: sdk.CellAddress{Row: 5, Col: 0}, Type: "text", Text: "Tenors", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 5, Col: 1}, Type: "text", Text: "Expiries", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 5, Col: 2}, Type: "text", Text: "Strikes", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 5, Col: 3}, Type: "text", Text: "Points", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
	)

	totalPoints := len(s.tenors) * len(expiries) * len(strikes)
	tenorStr := ""
	for i, t := range s.tenors {
		if i > 0 { tenorStr += ", " }
		tenorStr += t
	}
	expiryStr := ""
	for i, e := range expiries {
		if i > 0 { expiryStr += ", " }
		expiryStr += e
	}
	strikeStr := ""
	for i, st := range strikes {
		if i > 0 { strikeStr += ", " }
		if st >= 0 {
			strikeStr += "+"
		}
		strikeStr += fmt.Sprintf("%.0f", st)
	}

	cells = append(cells,
		sdk.TextCell(6, 0, tenorStr, nil),
		sdk.TextCell(6, 1, expiryStr, nil),
		sdk.TextCell(6, 2, strikeStr+"bp", nil),
		sdk.NumberCell(6, 3, "", float64(totalPoints), 0, ""),
	)

	// ATM vol slice (strike=0) for quick reference.
	cells = append(cells, sdk.SectionCell(8, 0, "── ATM VOL (strike=0bp) ──", 5))
	cells = append(cells, sdk.TextCell(9, 0, "", nil))
	for i, t := range s.tenors {
		if i >= 4 { break }
		cells = append(cells, sdk.Cell{
			Address: sdk.CellAddress{Row: 9, Col: uint32(1 + i)},
			Type: "text", Text: t, Style: &sdk.CellStyle{Bold: true, Fg: "dim"},
		})
	}

	atmIdx := 3 // strike=0 is at index 3
	for ei, exp := range expiries {
		row := uint32(10 + ei)
		cells = append(cells, sdk.TextCell(row, 0, exp, &sdk.CellStyle{Fg: "cyan"}))
		for ti := range s.tenors {
			if ti >= 4 { break }
			vol := vols[ti][ei][atmIdx]
			fg := "green"
			if vol > 20 { fg = "yellow" }
			if vol > 30 { fg = "red" }
			cells = append(cells, sdk.Cell{
				Address:   sdk.CellAddress{Row: row, Col: uint32(1 + ti)},
				Type:      "number",
				Value:     vol,
				Precision: 1,
				Unit:      "%",
				Style:     &sdk.CellStyle{Fg: fg},
			})
		}
	}

	return cells
}

