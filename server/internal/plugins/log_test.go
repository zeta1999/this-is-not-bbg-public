package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenPluginLog_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	f, err := openPluginLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("hello\n"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, LogFileName))
	if err != nil || string(got) != "hello\n" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestOpenPluginLog_AppendsAcrossReopens(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		f, err := openPluginLog(dir)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString("line\n")
		_ = f.Close()
	}
	got, _ := os.ReadFile(filepath.Join(dir, LogFileName))
	if want := "line\nline\nline\n"; string(got) != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestOpenPluginLog_RotatesAtCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LogFileName)
	// Preload a file at or above the cap.
	if err := os.WriteFile(path, []byte(strings.Repeat("x", int(PluginLogMaxBytes))), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := openPluginLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, _ = f.WriteString("fresh\n")

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("expected rotated backup: %v", err)
	}
	if int64(len(backup)) != PluginLogMaxBytes {
		t.Fatalf("backup size=%d, want %d", len(backup), PluginLogMaxBytes)
	}
	cur, _ := os.ReadFile(path)
	if string(cur) != "fresh\n" {
		t.Fatalf("active log after rotation=%q, want fresh\\n", cur)
	}
}
