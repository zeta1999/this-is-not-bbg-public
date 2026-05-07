package plugins

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

// writeManifest drops a minimal manifest.yaml in a plugin dir and
// returns the absolute path to the plugin directory. No command is
// set — Reconcile's load-path doesn't try to start processes, only
// LoadAll + Start do.
func writeManifest(t *testing.T, root, dirName, name string) string {
	t.Helper()
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("name: " + name + "\ncommand: /bin/false\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReconcile_DiscoversNewDir(t *testing.T) {
	root := t.TempDir()
	b := bus.New(8)
	m := NewManager(b, root)

	// Initial load — empty.
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	if got := len(m.plugins); got != 0 {
		t.Fatalf("pre-reconcile plugins=%d, want 0", got)
	}

	// Drop in a new plugin.
	writeManifest(t, root, "hello", "hello-world")

	// Reconcile should detect and load it. (Start will fail because
	// the command is /bin/false and it'll exit immediately, but the
	// plugin is still registered.)
	if err := m.Reconcile(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.plugins["hello-world"]; !ok {
		t.Fatalf("new plugin not registered; plugins=%v", keys(m.plugins))
	}
	// Allow the child exec to settle so the test doesn't race its own
	// background goroutines during teardown.
	_ = m.Stop("hello-world")
}

func TestReconcile_StopsRemovedDir(t *testing.T) {
	root := t.TempDir()
	b := bus.New(8)
	m := NewManager(b, root)

	writeManifest(t, root, "goodbye", "goodbye")
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.plugins["goodbye"]; !ok {
		t.Fatalf("plugin not loaded: %v", keys(m.plugins))
	}

	// Remove the dir.
	if err := os.RemoveAll(filepath.Join(root, "goodbye")); err != nil {
		t.Fatal(err)
	}

	if err := m.Reconcile(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.plugins["goodbye"]; ok {
		t.Fatalf("removed plugin still present: %v", keys(m.plugins))
	}
}

func TestReconcile_RestartsOnManifestChange(t *testing.T) {
	root := t.TempDir()
	b := bus.New(8)
	m := NewManager(b, root)

	writeManifest(t, root, "shift", "shift-v1")
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.plugins["shift-v1"]; !ok {
		t.Fatalf("initial plugin not present: %v", keys(m.plugins))
	}

	// Force a later modtime so Reconcile notices.
	time.Sleep(10 * time.Millisecond)
	writeManifest(t, root, "shift", "shift-v2")
	// Touch to guarantee modtime > first write on fast filesystems.
	future := time.Now().Add(1 * time.Second)
	_ = os.Chtimes(filepath.Join(root, "shift", "manifest.yaml"), future, future)

	if err := m.Reconcile(); err != nil {
		t.Fatal(err)
	}

	if _, ok := m.plugins["shift-v1"]; ok {
		t.Errorf("old name still present after rename")
	}
	if _, ok := m.plugins["shift-v2"]; !ok {
		t.Errorf("new name not present: %v", keys(m.plugins))
	}
	_ = m.Stop("shift-v2")
}

func keys(m map[string]*Plugin) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
