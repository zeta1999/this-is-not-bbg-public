// portfolio is a proof-of-concept plugin implementing a simple portfolio
// tracker. Demonstrates the cell grid with a multi-asset table of holdings,
// live P&L from OHLC feed, and aggregated metrics.
package main

import (
	"encoding/json"
	"fmt"
	"math"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

type holding struct {
	ticker   string
	quantity float64
	avgCost  float64
	last     float64
}

type state struct {
	holdings []holding
}

func defaultState() state {
	return state{
		holdings: []holding{
			{ticker: "BTCUSDT", quantity: 0.5, avgCost: 65000},
			{ticker: "ETHUSDT", quantity: 5.0, avgCost: 3200},
			{ticker: "SOLUSDT", quantity: 100.0, avgCost: 140},
		},
	}
}

func main() {
	p := sdk.New("plugin.portfolio.screen")
	s := defaultState()

	p.Run(func(msg sdk.Message) {
		if msg.Topic == "plugin.portfolio.input" {
			return // input handling later
		}

		var payload struct {
			Instrument string  `json:"Instrument"`
			Close      float64 `json:"Close"`
		}
		if json.Unmarshal(msg.Payload, &payload) != nil || payload.Close <= 0 {
			return
		}

		for i := range s.holdings {
			if s.holdings[i].ticker == payload.Instrument {
				s.holdings[i].last = payload.Close
			}
		}

		p.UpdateCellGrid("PORTFOLIO", buildGrid(s), true)
	})
}

func buildGrid(s state) []sdk.Cell {
	cells := []sdk.Cell{
		sdk.HeaderCell(0, 0, "PORTFOLIO"),
	}

	// Holdings table header.
	cells = append(cells, sdk.SectionCell(2, 0, "── HOLDINGS ──", 5))
	headers := []string{"Ticker", "Qty", "Avg Cost", "Last", "P&L"}
	for i, h := range headers {
		cells = append(cells, sdk.Cell{
			Address: sdk.CellAddress{Row: 3, Col: uint32(i)},
			Type:    "text",
			Text:    h,
			Style:   &sdk.CellStyle{Bold: true, Fg: "dim"},
		})
	}

	var totalValue, totalCost float64

	for i, h := range s.holdings {
		row := uint32(4 + i)
		cells = append(cells, sdk.TextCell(row, 0, h.ticker, &sdk.CellStyle{Fg: "cyan"}))
		cells = append(cells, sdk.NumberCell(row, 1, "", h.quantity, 4, ""))
		cells = append(cells, sdk.NumberCell(row, 2, "", h.avgCost, 2, "$"))

		if h.last > 0 {
			cells = append(cells, sdk.NumberCell(row, 3, "", h.last, 2, "$"))
			pnl := (h.last - h.avgCost) * h.quantity
			delta := ""
			if pnl > 0 {
				delta = "up"
			} else if pnl < 0 {
				delta = "down"
			}
			cells = append(cells, sdk.NumberCellWithDelta(row, 4, "", pnl, 2, "$", delta))
			totalValue += h.last * h.quantity
			totalCost += h.avgCost * h.quantity
		} else {
			cells = append(cells, sdk.TextCell(row, 3, "---", &sdk.CellStyle{Fg: "dim"}))
			cells = append(cells, sdk.TextCell(row, 4, "---", &sdk.CellStyle{Fg: "dim"}))
			totalCost += h.avgCost * h.quantity
		}
	}

	// Summary row.
	summaryRow := uint32(4 + len(s.holdings) + 1)
	cells = append(cells, sdk.SectionCell(summaryRow, 0, "── SUMMARY ──", 5))

	totalPnL := totalValue - totalCost
	pnlPct := 0.0
	if totalCost > 0 {
		pnlPct = totalPnL / totalCost * 100
	}

	delta := ""
	if totalPnL > 0 {
		delta = "up"
	} else if totalPnL < 0 {
		delta = "down"
	}

	cells = append(cells,
		sdk.NumberCell(summaryRow+1, 0, "NAV", totalValue, 2, "$"),
		sdk.NumberCell(summaryRow+1, 1, "Cost", totalCost, 2, "$"),
		sdk.NumberCellWithDelta(summaryRow+1, 2, "P&L", totalPnL, 2, "$", delta),
		sdk.NumberCellWithDelta(summaryRow+1, 3, "Return", pnlPct, 2, "%", delta),
	)

	// Allocation breakdown.
	allocRow := summaryRow + 3
	cells = append(cells, sdk.SectionCell(allocRow, 0, "── ALLOCATION ──", 5))
	cells = append(cells,
		sdk.Cell{Address: sdk.CellAddress{Row: allocRow + 1, Col: 0}, Type: "text", Text: "Ticker", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: allocRow + 1, Col: 1}, Type: "text", Text: "Value", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: allocRow + 1, Col: 2}, Type: "text", Text: "Weight", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
	)

	for i, h := range s.holdings {
		row := allocRow + 2 + uint32(i)
		val := h.last * h.quantity
		weight := 0.0
		if totalValue > 0 {
			weight = val / totalValue * 100
		}
		// Simple ASCII bar.
		barLen := int(math.Round(weight / 5))
		bar := ""
		for j := 0; j < barLen; j++ {
			bar += "█"
		}

		cells = append(cells,
			sdk.TextCell(row, 0, h.ticker, &sdk.CellStyle{Fg: "cyan"}),
			sdk.NumberCell(row, 1, "", val, 2, "$"),
			sdk.NumberCell(row, 2, "", weight, 1, "%"),
			sdk.TextCell(row, 3, bar, &sdk.CellStyle{Fg: "green"}),
		)
	}

	return cells
}

func init() {
	// silence unused import
	_ = fmt.Sprintf
}
