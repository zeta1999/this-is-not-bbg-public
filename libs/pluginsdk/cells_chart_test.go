package pluginsdk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChartCell_JSONShape(t *testing.T) {
	c := ChartCell(1, 2, "PnL", []ChartSeries{
		LineSeries("cum", []float64{0, 1, 2, 3}),
	})
	if c.Type != "chart" {
		t.Fatalf("Type=%q, want chart", c.Type)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{`"type":"chart"`, `"label":"PnL"`, `"kind":"line"`, `[0,1,2,3]`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in JSON: %s", want, s)
		}
	}
}

func TestTableCell_JSONShape(t *testing.T) {
	cols := []TableColumn{{Header: "Sym"}, {Header: "Qty", Align: "right"}}
	rows := [][]string{
		{"BTCUSDT", "1.5"},
		{"ETHUSDT", "12.0"},
	}
	c := TableCell(3, 4, "Positions", cols, rows)
	if c.Type != "table" {
		t.Fatalf("Type=%q, want table", c.Type)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{`"type":"table"`, `"header":"Sym"`, `"align":"right"`, `"BTCUSDT"`, `"ETHUSDT"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in JSON: %s", want, s)
		}
	}
}

func TestTableCell_OmitsEmptyRows(t *testing.T) {
	c := TableCell(0, 0, "", []TableColumn{{Header: "x"}}, nil)
	data, _ := json.Marshal(c)
	if strings.Contains(string(data), `"rows"`) {
		t.Fatalf("empty rows should be omitted, got %s", data)
	}
}
