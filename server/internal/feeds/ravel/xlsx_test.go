package ravel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// writeFixtureXLSX creates a minimal XLSX with a header row plus two
// data rows: one numeric scalar + some dims, and one with a date.
func writeFixtureXLSX(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "rates.xlsx")
	f := excelize.NewFile()
	// New files default to "Sheet1"; rename.
	_ = f.SetSheetName("Sheet1", "Rates")
	rows := [][]any{
		{"curve", "maturity", "t", "r"},
		{"JPY6M", "2020-03-01", 0.416, 0.01},
		{"KRW3M", "2020-06-01", 0.668, 0.012},
	}
	for i, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		_ = f.SetSheetRow("Rates", cell, &row)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseXLSX_HeadersBecomeDimsNumericFirstIsScalar(t *testing.T) {
	dir := t.TempDir()
	path := writeFixtureXLSX(t, dir)

	pts, err := ParseXLSX(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("got %d points, want 2", len(pts))
	}
	first := pts[0]
	if first.Dims["curve"] != "JPY6M" {
		t.Errorf("curve dim: %q", first.Dims["curve"])
	}
	if first.Dims["source"] != "ravel" || first.Dims["sheet"] != "Rates" {
		t.Errorf("meta dims: %+v", first.Dims)
	}
	if first.Dims["type"] != "rate" {
		t.Errorf("type: %q (wanted canonical 'rate' from 'Rates' sheet)", first.Dims["type"])
	}
	if first.Value.Scalar != 0.416 {
		t.Errorf("scalar: %v (wanted first numeric column)", first.Value.Scalar)
	}
	if first.T.IsZero() {
		t.Error("T should have been parsed from maturity date")
	}
}

func TestParseXLSX_SkipsEmptyRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.xlsx")
	f := excelize.NewFile()
	_ = f.SetSheetName("Sheet1", "Rates")
	// Just a header row.
	_ = f.SetSheetRow("Rates", "A1", &[]any{"curve", "r"})
	// Blank row 2, data row 3.
	_ = f.SetSheetRow("Rates", "A3", &[]any{"X", 0.01})
	_ = f.SaveAs(path)

	pts, err := ParseXLSX(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 {
		t.Errorf("got %d, want 1", len(pts))
	}
}

func TestParseXLSX_RealFixtureIfPresent(t *testing.T) {
	dir := sampleDir(t)
	path := filepath.Join(dir, "Rates_All.xlsx")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("skip: %v", err)
	}
	pts, err := ParseXLSX(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pts) == 0 {
		t.Error("real fixture produced no points")
	}
	// Spot-check metadata.
	if pts[0].Dims["source"] != "ravel" {
		t.Errorf("source missing: %+v", pts[0].Dims)
	}
}
