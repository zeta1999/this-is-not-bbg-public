// formula-demo is a minimal plugin that demonstrates Phase 2.2's
// formula engine. It declares two input cells (a, b) and three
// formula cells (sum, product, sqrt(product)) using R<row>C<col>
// references, and calls formula.Recalc on every input event so the
// computed values flow to the GUIs unchanged.
package main

import (
	"encoding/json"
	"time"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
	"github.com/notbbg/notbbg/libs/pluginsdk/formula"
)

type state struct {
	a float64
	b float64
}

func main() {
	p := sdk.New("plugin.formula-demo.screen")
	st := state{a: 10, b: 4}

	publish := func() {
		cells := buildGrid(st)
		// Recalc mutates Value in place for every formula cell.
		// Errors here would surface as cells keeping their cached
		// value; for a 3-formula demo we ignore them.
		_ = formula.Recalc(cells)
		p.UpdateCellGrid("FORMULA", cells, true)
	}

	publish()

	// Re-emit the cell grid every 30s. Without this the plugin
	// would idle past the server's 300s heartbeat threshold (it
	// only emits on input events and there's no input stream),
	// so the MON panel would show this plugin as "error" even
	// though it's healthy.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			publish()
		}
	}()

	p.Run(func(msg sdk.Message) {
		if msg.Topic != "plugin.formula-demo.input" {
			return
		}
		var evt sdk.InputEvent
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			return
		}
		switch {
		case evt.Address.Row == 1 && evt.Address.Col == 1:
			st.a = sdk.CellValue(evt.Value)
		case evt.Address.Row == 2 && evt.Address.Col == 1:
			st.b = sdk.CellValue(evt.Value)
		}
		publish()
	})
}

func buildGrid(s state) []sdk.Cell {
	return []sdk.Cell{
		sdk.HeaderCell(0, 0, "FORMULA DEMO"),

		sdk.TextCell(1, 0, "a", nil),
		sdk.DecimalInputCell(1, 1, "", s.a, 4),

		sdk.TextCell(2, 0, "b", nil),
		sdk.DecimalInputCell(2, 1, "", s.b, 4),

		sdk.TextCell(4, 0, "a + b", nil),
		sdk.FormulaCell(4, 1, "", "=R1C1 + R2C1", 0, 4),

		sdk.TextCell(5, 0, "a * b", nil),
		sdk.FormulaCell(5, 1, "", "=R1C1 * R2C1", 0, 4),

		sdk.TextCell(6, 0, "sqrt(a*b)", nil),
		sdk.FormulaCell(6, 1, "", "=SQRT(R5C1)", 0, 4),
	}
}
