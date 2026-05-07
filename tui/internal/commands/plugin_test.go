package commands

import (
	"os"
	"path/filepath"
	"testing"

	tuiconfig "github.com/notbbg/notbbg/tui/internal/config"
)

// TestCopyDir_RecursivelyCopies exercises the install helper
// directly — a full install run would require intercepting tuiconfig
// resolution, which is overkill here.
func TestCopyDir_RecursivelyCopies(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dest")

	// Build a miniature plugin dir: manifest + nested file.
	if err := os.WriteFile(filepath.Join(src, "manifest.yaml"), []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "entrypoint.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dst, "manifest.yaml")); err != nil || string(got) != "name: demo\n" {
		t.Fatalf("manifest copy: err=%v got=%q", err, got)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "bin", "entrypoint.sh")); err != nil || len(got) == 0 {
		t.Fatalf("nested file copy: err=%v got=%q", err, got)
	}
}

func TestInstall_RefusesWhenDestExists(t *testing.T) {
	// Redirect tuiconfig.Plugins() to an isolated root. SetHomeOverride
	// memoizes once globally, so we do the override before any Plugins()
	// call in this test process.
	pluginsRoot := filepath.Join(t.TempDir(), "home")
	tuiconfig.SetHomeOverride(pluginsRoot)
	// Touch the path so Plugins() bakes into cache with our root.
	_ = tuiconfig.Plugins()

	// Create the would-be destination first.
	if err := os.MkdirAll(filepath.Join(tuiconfig.Plugins(), "dup"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Prepare a valid source.
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "manifest.yaml"), []byte("name: dup\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := pluginInstallCmd()
	cmd.SetArgs([]string{src, "dup"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error on existing destination, got nil")
	}
}
