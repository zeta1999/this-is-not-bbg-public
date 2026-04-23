// oms is a proof-of-concept Order Management System plugin.
// Demonstrates order entry inputs, positions table, and execution log.
// This is a simulated OMS — no real exchange connectivity.
package main

import (
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

type order struct {
	id     int
	ticker string
	side   string // "BUY" or "SELL"
	qty    float64
	price  float64
	status string // "OPEN", "FILLED", "CANCELLED"
	ts     time.Time
}

type position struct {
	ticker   string
	qty      float64
	avgPrice float64
	last     float64
}

type state struct {
	// Order entry.
	entryTicker string
	entrySide   string
	entryQty    float64
	entryPrice  float64

	orders    []order
	positions map[string]*position
	nextID    int
	prices    map[string]float64
}

func defaultState() state {
	return state{
		entryTicker: "BTCUSDT",
		entrySide:   "BUY",
		entryQty:    0.01,
		entryPrice:  0, // 0 = market
		positions:   make(map[string]*position),
		prices:      make(map[string]float64),
	}
}

func main() {
	p := sdk.New("plugin.oms.screen")
	s := defaultState()

	p.Run(func(msg sdk.Message) {
		switch {
		case msg.Topic == "plugin.oms.input":
			var evt sdk.InputEvent
			if json.Unmarshal(msg.Payload, &evt) != nil {
				return
			}
			applyInput(&s, evt)

		default:
			var payload struct {
				Instrument string  `json:"Instrument"`
				Close      float64 `json:"Close"`
				Price      float64 `json:"Price"`
			}
			if json.Unmarshal(msg.Payload, &payload) != nil {
				return
			}
			if payload.Close > 0 {
				s.prices[payload.Instrument] = payload.Close
			} else if payload.Price > 0 {
				s.prices[payload.Instrument] = payload.Price
			}
			// Update position last prices.
			for _, pos := range s.positions {
				if p, ok := s.prices[pos.ticker]; ok {
					pos.last = p
				}
			}
		}

		p.UpdateCellGrid("OMS", buildGrid(s), true)
	})
}

func applyInput(s *state, evt sdk.InputEvent) {
	r, c := evt.Address.Row, evt.Address.Col
	switch {
	case r == 1 && c == 0:
		s.entryTicker = sdk.CellStringValue(evt.Value)
	case r == 1 && c == 1:
		s.entrySide = sdk.CellStringValue(evt.Value)
	case r == 1 && c == 2:
		s.entryQty = sdk.CellValue(evt.Value)
	case r == 1 && c == 3:
		s.entryPrice = sdk.CellValue(evt.Value)
	case r == 2 && c == 0:
		// "Submit" action — simulate order fill.
		submitOrder(s)
	}
}

func submitOrder(s *state) {
	s.nextID++
	price := s.entryPrice
	if price == 0 {
		// Market order: use last known price.
		if p, ok := s.prices[s.entryTicker]; ok {
			price = p
		} else {
			price = 68000 // fallback
		}
	}

	o := order{
		id:     s.nextID,
		ticker: s.entryTicker,
		side:   s.entrySide,
		qty:    s.entryQty,
		price:  price,
		status: "FILLED",
		ts:     time.Now(),
	}
	s.orders = append(s.orders, o)

	// Update position.
	pos, ok := s.positions[o.ticker]
	if !ok {
		pos = &position{ticker: o.ticker}
		s.positions[o.ticker] = pos
	}
	if o.side == "BUY" {
		totalCost := pos.avgPrice*pos.qty + o.price*o.qty
		pos.qty += o.qty
		if pos.qty > 0 {
			pos.avgPrice = totalCost / pos.qty
		}
	} else {
		pos.qty -= o.qty
		if pos.qty <= 0 {
			pos.qty = 0
			pos.avgPrice = 0
		}
	}
	if p, ok := s.prices[o.ticker]; ok {
		pos.last = p
	}
}

func buildGrid(s state) []sdk.Cell {
	cells := []sdk.Cell{
		sdk.HeaderCell(0, 0, "ORDER MANAGEMENT"),
	}

	// Order entry row.
	cells = append(cells, sdk.SectionCell(1, 0, "── ORDER ENTRY ──", 5))
	cells = append(cells,
		sdk.StringInputCell(2, 0, "Ticker", s.entryTicker, "BTCUSDT"),
		sdk.EnumInputCell(2, 1, "Side", s.entrySide, []sdk.EnumOption{
			{Value: "BUY", Label: "BUY"},
			{Value: "SELL", Label: "SELL"},
		}),
		sdk.DecimalInputCell(2, 2, "Qty", s.entryQty, 4),
		sdk.DecimalInputCell(2, 3, "Price", s.entryPrice, 2),
	)
	// Submit hint (acts as a trigger cell).
	cells = append(cells, sdk.TextCell(3, 0, "Enter on Ticker → submit order", &sdk.CellStyle{Fg: "dim"}))

	// Positions table.
	cells = append(cells, sdk.SectionCell(5, 0, "── POSITIONS ──", 5))
	cells = append(cells,
		sdk.Cell{Address: sdk.CellAddress{Row: 6, Col: 0}, Type: "text", Text: "Ticker", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 6, Col: 1}, Type: "text", Text: "Qty", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 6, Col: 2}, Type: "text", Text: "Avg Price", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 6, Col: 3}, Type: "text", Text: "Last", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 6, Col: 4}, Type: "text", Text: "P&L", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
	)
	row := uint32(7)
	for _, pos := range s.positions {
		if pos.qty == 0 {
			continue
		}
		pnl := (pos.last - pos.avgPrice) * pos.qty
		delta := ""
		if pnl > 0 {
			delta = "up"
		} else if pnl < 0 {
			delta = "down"
		}
		cells = append(cells,
			sdk.TextCell(row, 0, pos.ticker, &sdk.CellStyle{Fg: "cyan"}),
			sdk.NumberCell(row, 1, "", pos.qty, 4, ""),
			sdk.NumberCell(row, 2, "", pos.avgPrice, 2, "$"),
			sdk.NumberCell(row, 3, "", pos.last, 2, "$"),
			sdk.NumberCellWithDelta(row, 4, "", pnl, 2, "$", delta),
		)
		row++
	}

	// Execution log (last 10 orders).
	row++
	cells = append(cells, sdk.SectionCell(row, 0, "── EXECUTION LOG ──", 5))
	row++
	cells = append(cells,
		sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 0}, Type: "text", Text: "ID", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 1}, Type: "text", Text: "Side", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 2}, Type: "text", Text: "Price", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 3}, Type: "text", Text: "Qty", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 4}, Type: "text", Text: "Status", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
	)
	row++

	start := 0
	if len(s.orders) > 10 {
		start = len(s.orders) - 10
	}
	for _, o := range s.orders[start:] {
		sideStyle := &sdk.CellStyle{Fg: "green"}
		if o.side == "SELL" {
			sideStyle = &sdk.CellStyle{Fg: "red"}
		}
		cells = append(cells,
			sdk.TextCell(row, 0, fmt.Sprintf("#%d", o.id), nil),
			sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 1}, Type: "text", Text: o.side, Style: sideStyle},
			sdk.NumberCell(row, 2, "", o.price, 2, "$"),
			sdk.NumberCell(row, 3, "", o.qty, 4, ""),
			sdk.TextCell(row, 4, o.status, &sdk.CellStyle{Fg: "green"}),
		)
		row++
	}

	return cells
}
