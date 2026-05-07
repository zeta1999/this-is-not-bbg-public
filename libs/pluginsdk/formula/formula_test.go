package formula

import (
	"math"
	"testing"

	"github.com/notbbg/notbbg/libs/pluginsdk"
)

func addr(r, c uint32) pluginsdk.CellAddress { return pluginsdk.CellAddress{Row: r, Col: c} }

func numCell(r, c uint32, v float64) pluginsdk.Cell {
	return pluginsdk.Cell{Address: addr(r, c), Type: "number", Value: v}
}

func formCell(r, c uint32, expr string) pluginsdk.Cell {
	return pluginsdk.Cell{Address: addr(r, c), Type: "formula", Expression: expr}
}

func assertClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRecalc_WritesValuesInPlace(t *testing.T) {
	cells := []pluginsdk.Cell{
		numCell(0, 0, 10),
		numCell(0, 1, 4),
		formCell(1, 0, "=R0C0 + R0C1"),
		formCell(1, 1, "=R0C0 * R0C1"),
		formCell(2, 0, "=SQRT(R1C1)"),
	}
	errs := Recalc(cells)
	if len(errs) > 0 {
		t.Fatalf("unexpected errs: %+v", errs)
	}
	assertClose(t, cells[2].Value.(float64), 14)
	assertClose(t, cells[3].Value.(float64), 40)
	assertClose(t, cells[4].Value.(float64), math.Sqrt(40))
}

func TestRecalc_PreservesPriorValueOnError(t *testing.T) {
	cells := []pluginsdk.Cell{
		numCell(0, 0, 10),
		// Bad ref — the dependency cell doesn't exist.
		pluginsdk.Cell{
			Address:    addr(1, 0),
			Type:       "formula",
			Expression: "=R9C9 + 1",
			Value:      99.0, // pre-existing cached value
		},
	}
	errs := Recalc(cells)
	if len(errs) == 0 {
		t.Fatal("expected bad_ref error")
	}
	if got := cells[1].Value.(float64); got != 99.0 {
		t.Fatalf("Recalc clobbered cached value on error: got %v want 99", got)
	}
}

func TestEval_LiteralsAndArithmetic(t *testing.T) {
	cells := []pluginsdk.Cell{
		formCell(0, 0, "=1 + 2 * 3"),
		formCell(0, 1, "=(1 + 2) * 3"),
		formCell(0, 2, "=2^3"),
		formCell(0, 3, "=2^3^2"), // right-assoc: 2^(3^2) = 2^9 = 512
		formCell(0, 4, "=-5 + 3"),
		formCell(0, 5, "=10 / 4"),
	}
	vals, errs := EvalAll(cells)
	if len(errs) > 0 {
		t.Fatalf("unexpected errs: %+v", errs)
	}
	assertClose(t, vals[addr(0, 0)], 7)
	assertClose(t, vals[addr(0, 1)], 9)
	assertClose(t, vals[addr(0, 2)], 8)
	assertClose(t, vals[addr(0, 3)], 512)
	assertClose(t, vals[addr(0, 4)], -2)
	assertClose(t, vals[addr(0, 5)], 2.5)
}

func TestEval_CellRefs(t *testing.T) {
	cells := []pluginsdk.Cell{
		numCell(0, 0, 10),
		numCell(0, 1, 3),
		formCell(1, 0, "=R0C0 + R0C1"),
		formCell(1, 1, "=R1C0 * 2"),
		formCell(1, 2, "=R0C0 / R0C1"),
	}
	vals, errs := EvalAll(cells)
	if len(errs) > 0 {
		t.Fatalf("unexpected errs: %+v", errs)
	}
	assertClose(t, vals[addr(1, 0)], 13)
	assertClose(t, vals[addr(1, 1)], 26)
	assertClose(t, vals[addr(1, 2)], 10.0/3.0)
}

func TestEval_Functions(t *testing.T) {
	cells := []pluginsdk.Cell{
		formCell(0, 0, "=MIN(3, 1, 2)"),
		formCell(0, 1, "=MAX(3, 1, 2)"),
		formCell(0, 2, "=ABS(-7.5)"),
		formCell(0, 3, "=SQRT(16)"),
		formCell(0, 4, "=LN(EXP(1))"),
		formCell(0, 5, "=LOG(1000)"),
		formCell(0, 6, "=ROUND(1.2349, 2)"),
		formCell(0, 7, "=SUM(1, 2, 3, 4)"),
		formCell(0, 8, "=AVG(2, 4, 6)"),
	}
	vals, errs := EvalAll(cells)
	if len(errs) > 0 {
		t.Fatalf("unexpected errs: %+v", errs)
	}
	assertClose(t, vals[addr(0, 0)], 1)
	assertClose(t, vals[addr(0, 1)], 3)
	assertClose(t, vals[addr(0, 2)], 7.5)
	assertClose(t, vals[addr(0, 3)], 4)
	assertClose(t, vals[addr(0, 4)], 1)
	assertClose(t, vals[addr(0, 5)], 3)
	assertClose(t, vals[addr(0, 6)], 1.23)
	assertClose(t, vals[addr(0, 7)], 10)
	assertClose(t, vals[addr(0, 8)], 4)
}

func TestEval_TopologicalRecalc(t *testing.T) {
	// B depends on A, C depends on B. All three are formula cells.
	cells := []pluginsdk.Cell{
		numCell(0, 0, 5),       // A_input
		formCell(1, 0, "=R0C0 * 2"),  // A = 10
		formCell(2, 0, "=R1C0 + 1"),  // B = A + 1 = 11
		formCell(3, 0, "=R2C0^2"),    // C = B^2 = 121
	}
	vals, errs := EvalAll(cells)
	if len(errs) > 0 {
		t.Fatalf("unexpected errs: %+v", errs)
	}
	assertClose(t, vals[addr(1, 0)], 10)
	assertClose(t, vals[addr(2, 0)], 11)
	assertClose(t, vals[addr(3, 0)], 121)
}

func TestEval_CycleDetected(t *testing.T) {
	// A = B + 1, B = A + 1 — classic 2-cycle.
	cells := []pluginsdk.Cell{
		formCell(0, 0, "=R0C1 + 1"),
		formCell(0, 1, "=R0C0 + 1"),
	}
	_, errs := EvalAll(cells)
	if len(errs) != 2 {
		t.Fatalf("expected 2 cycle errors, got %+v", errs)
	}
	for _, e := range errs {
		if e.Reason != "cycle" {
			t.Fatalf("unexpected err reason %q", e.Reason)
		}
	}
}

func TestEval_DivByZero(t *testing.T) {
	cells := []pluginsdk.Cell{
		numCell(0, 0, 1),
		numCell(0, 1, 0),
		formCell(1, 0, "=R0C0 / R0C1"),
	}
	_, errs := EvalAll(cells)
	if len(errs) != 1 || errs[0].Reason != "divzero" {
		t.Fatalf("expected divzero error, got %+v", errs)
	}
}

func TestEval_ParseError(t *testing.T) {
	cells := []pluginsdk.Cell{
		formCell(0, 0, "=1 +"),
	}
	_, errs := EvalAll(cells)
	if len(errs) != 1 || errs[0].Reason != "parse" {
		t.Fatalf("expected parse error, got %+v", errs)
	}
}

func TestEval_BadRef(t *testing.T) {
	cells := []pluginsdk.Cell{
		formCell(0, 0, "=R5C5 + 1"),
	}
	_, errs := EvalAll(cells)
	if len(errs) != 1 || errs[0].Reason != "bad_ref" {
		t.Fatalf("expected bad_ref, got %+v", errs)
	}
}
