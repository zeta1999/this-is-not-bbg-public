package views

import (
	"strings"
	"testing"
)

// renderCell is package-private; these tests live in the same
// package to keep the surface area small and exercise the cell
// type matrix directly.

func TestRenderCell_TextDefault(t *testing.T) {
	c := PluginCell{Type: "text", Text: "hello"}
	got := renderCell(c)
	if !strings.Contains(got, "hello") {
		t.Fatalf("text cell missing payload: %q", got)
	}
}

func TestRenderCell_ImageShowsAltAndOpenHint(t *testing.T) {
	c := PluginCell{Type: "image", Alt: "ohlc-png", Src: "/tmp/x.png"}
	got := renderCell(c)
	for _, want := range []string{"📎", "[IMG ohlc-png]", "/tmp/x.png", "(o:open)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("image cell missing %q in: %q", want, got)
		}
	}
}

func TestRenderCell_ImageDefaultAltWhenMissing(t *testing.T) {
	c := PluginCell{Type: "image", Src: "/tmp/x.png"}
	got := renderCell(c)
	if !strings.Contains(got, "[IMG img]") {
		t.Fatalf("missing default alt placeholder: %q", got)
	}
}

func TestRenderCell_ImageMissingSrc(t *testing.T) {
	c := PluginCell{Type: "image", Alt: "x"}
	got := renderCell(c)
	if !strings.Contains(got, "(missing src)") {
		t.Fatalf("expected '(missing src)' marker: %q", got)
	}
}
