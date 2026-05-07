package app

import (
	"strings"
	"testing"
)

// TestClampPanelScroll_NeverPastEOF guards the regression where
// pressing j past the bottom would strand panelScrollOff at a value
// that took just as many k presses to undo. Now every increment
// clamps so the bottom-most line stays at the bottom of the visible
// window.
func TestClampPanelScroll_NeverPastEOF(t *testing.T) {
	m := New()
	m.height = 24
	m.qrOverlay = strings.Repeat("line\n", 50) + "tail"

	m.panelScrollOff = 1 << 20 // simulate G
	m.clampPanelScroll()

	lines := strings.Count(m.qrOverlay, "\n") + 1 // 51
	visible := m.height - 5                       // 19
	want := lines - visible                       // 32
	if m.panelScrollOff != want {
		t.Errorf("clampPanelScroll after G: got %d, want %d (lines=%d visible=%d)",
			m.panelScrollOff, want, lines, visible)
	}
}

// TestClampPanelScroll_ShortContentSnapsToZero confirms that when
// content fits inside the window, G is a no-op rather than offsetting
// to a nonsense value.
func TestClampPanelScroll_ShortContentSnapsToZero(t *testing.T) {
	m := New()
	m.height = 24
	m.qrOverlay = "one\ntwo\nthree"

	m.panelScrollOff = 1 << 20
	m.clampPanelScroll()

	if m.panelScrollOff != 0 {
		t.Errorf("short content should clamp to 0, got %d", m.panelScrollOff)
	}
}

// TestScrollableJKNow_GatedToReadOnlySurfaces ensures j/k scroll
// only fires on read-only / overlay surfaces — market panels
// own j/k for instrument nav, NEWS for item nav, AGENT for its
// own scroll, plugin grids for the cell cursor.
func TestScrollableJKNow_GatedToReadOnlySurfaces(t *testing.T) {
	cases := []struct {
		panel string
		want  bool
	}{
		{PanelOHLC, false},
		{PanelLOB, false},
		{PanelTrades, false},
		{PanelNews, false},
		{PanelAlerts, false},
		{PanelAgent, false},
		{PanelSettings, true},
		{PanelMonitor, true},
		{PanelLog, true},
		{PanelSanity, true},
	}
	for _, tc := range cases {
		m := New()
		m.activePanel = panelIndex(t, tc.panel)
		if got := m.scrollableJKNow(); got != tc.want {
			t.Errorf("scrollableJKNow(%s) = %v, want %v", tc.panel, got, tc.want)
		}
	}
}

// TestScrollableJKNow_OverlayAlwaysScrolls — help / PAIR overlays
// take j/k regardless of which panel they sit on top of.
func TestScrollableJKNow_OverlayAlwaysScrolls(t *testing.T) {
	m := New()
	m.activePanel = panelIndex(t, PanelOHLC)
	m.qrOverlay = "any text"
	if !m.scrollableJKNow() {
		t.Fatal("overlay should accept j/k scroll regardless of panel")
	}
}

// TestScrollableNow_PageKeysWorkOnEveryPanel covers the page-step
// scroll surface — PgUp/PgDn/g/G work on every panel (overflow on
// OHLC's TF carousel, plugin grids, NEWS detail, etc.) since those
// keys don't collide with any panel-specific nav.
func TestScrollableNow_PageKeysWorkOnEveryPanel(t *testing.T) {
	for _, p := range panelList {
		m := New()
		m.activePanel = panelIndex(t, p)
		if !m.scrollableNow() {
			t.Errorf("scrollableNow(%s) should be true (page-step scroll works everywhere)", p)
		}
	}

	// Cmd mode is the only blocker — keystrokes go to the input.
	m := New()
	m.cmdMode = true
	if m.scrollableNow() {
		t.Error("cmd mode should suppress page-step scroll")
	}
}
