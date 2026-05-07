package views

import (
	"strings"
	"testing"
)

// TestRenderQRBlock_NonEmpty confirms the encoder produces a
// half-block string for a typical pairing payload.
func TestRenderQRBlock_NonEmpty(t *testing.T) {
	payload := `{"url":"http://localhost:9474","token":"abc123"}`
	out := RenderQRBlock(payload)
	if out == "" {
		t.Fatal("RenderQRBlock returned empty string")
	}
	if strings.HasPrefix(out, "QR encode error") {
		t.Fatalf("encoder errored: %s", out)
	}
	// Half-block characters are the only graphical glyphs we emit.
	// At least one of them should be present.
	if !strings.ContainsAny(out, "█▀▄") {
		t.Errorf("expected half-block glyph in output, got:\n%s", out)
	}
}

// TestHalfBlockBitmap_Patterns walks the four (top,bot) cases — full
// / upper / lower / blank — using a tiny bitmap so the glyph
// selection is unambiguous.
func TestHalfBlockBitmap_Patterns(t *testing.T) {
	bitmap := [][]bool{
		{true, false, true, false},
		{true, true, false, false},
	}
	out := halfBlockBitmap(bitmap)
	// Row 0: top from bitmap[0], bot from bitmap[1].
	// (T=true,B=true)→█  (T=false,B=true)→▄  (T=true,B=false)→▀  (T=false,B=false)→' '
	wantRow := "█▄▀ \n"
	if out != wantRow {
		t.Errorf("halfBlockBitmap got %q, want %q", out, wantRow)
	}
}

// TestHalfBlockBitmap_OddHeight handles a bitmap with an odd row
// count — the trailing row gets paired with all-false (blank).
func TestHalfBlockBitmap_OddHeight(t *testing.T) {
	bitmap := [][]bool{
		{true, false},
		{false, true},
		{true, false},
	}
	out := halfBlockBitmap(bitmap)
	// Two output rows: first pairs rows 0+1, second pairs row 2 with empty.
	want := "▀▄\n▀ \n"
	if out != want {
		t.Errorf("odd-height halfBlockBitmap got %q, want %q", out, want)
	}
}
