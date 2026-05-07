package app

import (
	"testing"

	"github.com/notbbg/notbbg/tui/internal/views"
)

// TestPanelName_LookupMatchesAllPanels documents the canonical panel
// order. If a contributor inserts or reorders entries in panelList
// without updating the integer-vs-name lookup, this test fails and
// the news/trades nav drift bug from 2026-04-28 cannot recur.
func TestPanelName_LookupMatchesAllPanels(t *testing.T) {
	m := Model{}
	want := []string{
		PanelOHLC, PanelLOB, PanelTrades, PanelNews,
		PanelAlerts, PanelSanity, PanelMonitor, PanelLog, PanelAgent, PanelSettings,
	}
	got := m.allPanels()
	if len(got) != len(want) {
		t.Fatalf("allPanels len=%d want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("panel[%d] = %q, want %q", i, got[i], w)
		}
		if name := m.panelName(i); name != w {
			t.Errorf("panelName(%d) = %q, want %q", i, name, w)
		}
	}
}

func TestPanelName_OutOfBoundsReturnsEmpty(t *testing.T) {
	m := Model{}
	if got := m.panelName(-1); got != "" {
		t.Errorf("panelName(-1) = %q, want empty", got)
	}
	if got := m.panelName(99); got != "" {
		t.Errorf("panelName(99) = %q, want empty", got)
	}
}

// TestPanelName_PluginScreensAppendAfterCorePanels guards the
// plugin tab insertion contract: plugin screens come after every
// core panel, never in front of one. If someone changes that, key
// handlers gated on PanelXxx names will silently misroute.
func TestPanelName_PluginScreensAppendAfterCorePanels(t *testing.T) {
	m := Model{
		pluginScreens: []views.PluginScreenData{
			{ID: "PRICER"},
			{ID: "BACKTEST"},
		},
	}
	all := m.allPanels()
	if len(all) != len(panelList)+2 {
		t.Fatalf("allPanels with 2 plugin screens len=%d, want %d", len(all), len(panelList)+2)
	}
	if all[len(panelList)] != "PRICER" || all[len(panelList)+1] != "BACKTEST" {
		t.Fatalf("plugin screens not appended: %v", all)
	}
}

// TestNewsPanelIndex_DriftDoesNotBreakNav captures the actual
// regression observed in the screenshot: TRADES was inserted as
// panel 2, which shifted NEWS to panel 3, but the news key handler
// was still gated on `activePanel == 2`. Encoding the relationship
// "j/k navigates news when activePanelName == PanelNews" via the
// helper makes the gate self-correcting.
func TestNewsPanelIndex_DriftDoesNotBreakNav(t *testing.T) {
	m := Model{}
	// Resolve where NEWS lives in the canonical panelList.
	newsIdx := -1
	for i, p := range panelList {
		if p == PanelNews {
			newsIdx = i
			break
		}
	}
	if newsIdx < 0 {
		t.Fatal("PanelNews missing from panelList")
	}
	m.activePanel = newsIdx
	if m.activePanelName() != PanelNews {
		t.Fatalf("activePanelName at idx %d = %q, want %q",
			newsIdx, m.activePanelName(), PanelNews)
	}
	// Same idx must NOT collide with TRADES / ALERTS regardless of
	// where in the list NEWS ends up.
	if m.activePanelName() == PanelTrades || m.activePanelName() == PanelAlerts {
		t.Fatalf("NEWS index collides with sibling panel: %s", m.activePanelName())
	}
}
