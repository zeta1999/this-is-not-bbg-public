package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveHome_OverrideWins(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/should/be/ignored")
	got := ResolveHome("/explicit/path")
	if got != "/explicit/path" {
		t.Fatalf("ResolveHome(override)=%q, want /explicit/path", got)
	}
}

func TestResolveHome_XDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	got := ResolveHome("")
	if got != "/xdg/notbbg" {
		t.Fatalf("ResolveHome()=%q, want /xdg/notbbg", got)
	}
}

func TestResolveHome_Default(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	got := ResolveHome("")
	want := filepath.Join(home, ".config", "notbbg")
	if got != want {
		t.Fatalf("ResolveHome()=%q, want %q", got, want)
	}
}

func TestSubpaths(t *testing.T) {
	root := "/tmp/nbbg"
	if got := Plugins(root); got != filepath.Join(root, "plugins") {
		t.Fatal(got)
	}
	if got := Certs(root); got != filepath.Join(root, "certs") {
		t.Fatal(got)
	}
	if got := ConfigFile(root); !strings.HasSuffix(got, "config.yaml") {
		t.Fatal(got)
	}
}
