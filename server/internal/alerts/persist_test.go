package alerts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/notbbg/notbbg/server/internal/bus"
)

func TestPersist_AddDismissReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.json")

	// Fresh engine, enable persistence, add two alerts, dismiss one.
	b := bus.New(8)
	e := NewEngine(b)
	if err := e.EnablePersistence(path); err != nil {
		t.Fatal(err)
	}
	id1 := e.Add(Alert{Type: PriceAbove, Instrument: "BTCUSDT", Threshold: 100000})
	id2 := e.Add(Alert{Type: Keyword, Keyword: "hack"})
	e.Dismiss(id1)

	// File should exist and contain both alerts.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}

	// Fresh engine — loads the same file, sees both alerts with
	// id1 dismissed.
	e2 := NewEngine(b)
	if err := e2.EnablePersistence(path); err != nil {
		t.Fatal(err)
	}
	all := e2.List()
	if len(all) != 2 {
		t.Fatalf("loaded %d alerts, want 2", len(all))
	}
	foundDismissed := 0
	for _, a := range all {
		if a.ID == id1 && a.Status == "dismissed" {
			foundDismissed++
		}
		if a.ID == id2 && a.Status != "active" {
			t.Errorf("id2 status=%q, want active", a.Status)
		}
	}
	if foundDismissed != 1 {
		t.Errorf("dismissed alert not restored; got %d", foundDismissed)
	}

	// nextID should survive: next Add must yield alert-3 (first two
	// used 1 and 2).
	id3 := e2.Add(Alert{Type: PriceBelow, Instrument: "ETHUSDT", Threshold: 1000})
	if id3 != "alert-3" {
		t.Errorf("after reload, next id=%q, want alert-3", id3)
	}
}

func TestPersist_MissingFileIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	b := bus.New(8)
	e := NewEngine(b)
	if err := e.EnablePersistence(path); err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if n := len(e.List()); n != 0 {
		t.Fatalf("expected empty, got %d alerts", n)
	}
}

func TestPersist_CorruptFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := bus.New(8)
	e := NewEngine(b)
	if err := e.EnablePersistence(path); err == nil {
		t.Fatal("expected parse error on corrupt file")
	}
}
