package app

import (
	"testing"

	"github.com/notbbg/notbbg/tui/internal/views"
)

// TestProcessCommand_LOBFilterSetsActive verifies that typing
// `/BTC bin` while on the LOB panel both stores the filter and
// jumps to the first matching key. Mirrors the OHLC behavior so the
// `/`-jump is consistent across panels.
func TestProcessCommand_LOBFilterSetsActive(t *testing.T) {
	m := New()
	m.activePanel = panelIndex(t, PanelLOB)
	m.lobKeys = []string{"BTCUSDT/binance", "ETHUSDT/binance", "SOLUSDT/coinbase"}
	m.lobData = map[string]*views.LOBData{
		"BTCUSDT/binance":  {Instrument: "BTCUSDT", Exchange: "binance"},
		"ETHUSDT/binance":  {Instrument: "ETHUSDT", Exchange: "binance"},
		"SOLUSDT/coinbase": {Instrument: "SOLUSDT", Exchange: "coinbase"},
	}

	m.processCommand("ETH bin")

	if m.lobSidebarFilter != "ETH bin" {
		t.Errorf("lobSidebarFilter = %q, want %q", m.lobSidebarFilter, "ETH bin")
	}
	if m.lobActiveIdx != 1 {
		t.Errorf("lobActiveIdx = %d, want 1 (ETHUSDT/binance)", m.lobActiveIdx)
	}
}

// TestProcessCommand_TradesFilterSetsActive does the same for TRADES.
func TestProcessCommand_TradesFilterSetsActive(t *testing.T) {
	m := New()
	m.activePanel = panelIndex(t, PanelTrades)
	m.tradeKeys = []string{"binance/BTCUSDT", "binance/ETHUSDT", "coinbase/SOLUSD"}
	m.tradeData = map[string]*views.TradeViewData{
		"binance/BTCUSDT":  {Agg: &views.TradeAggData{Instrument: "BTCUSDT", Exchange: "binance"}},
		"binance/ETHUSDT":  {Agg: &views.TradeAggData{Instrument: "ETHUSDT", Exchange: "binance"}},
		"coinbase/SOLUSD":  {Agg: &views.TradeAggData{Instrument: "SOLUSD", Exchange: "coinbase"}},
	}

	m.processCommand("SOL coin")

	if m.tradesSidebarFilter != "SOL coin" {
		t.Errorf("tradesSidebarFilter = %q, want %q", m.tradesSidebarFilter, "SOL coin")
	}
	if m.tradeActiveIdx != 2 {
		t.Errorf("tradeActiveIdx = %d, want 2 (coinbase/SOLUSD)", m.tradeActiveIdx)
	}
}

// TestProcessCommand_OHLCEmptyClearsFilter confirms the empty-input
// path clears the filter on every panel that supports one.
func TestProcessCommand_OHLCEmptyClearsFilter(t *testing.T) {
	m := New()
	m.activePanel = panelIndex(t, PanelLOB)
	m.lobSidebarFilter = "BTC bin"
	m.processCommand("")
	if m.lobSidebarFilter != "" {
		t.Errorf("empty input should clear lobSidebarFilter, got %q", m.lobSidebarFilter)
	}
}

// TestSearchInstrument_PrefixTokensAcrossSources confirms the
// generalised matcher resolves a key via any of the OHLC/LOB/Trades
// data maps so a `/`-jump works on every panel.
func TestSearchInstrument_PrefixTokensAcrossSources(t *testing.T) {
	m := New()
	m.lobData = map[string]*views.LOBData{
		"BTCUSDT/binance": {Instrument: "BTCUSDT", Exchange: "binance"},
		"ETHUSDT/kraken":  {Instrument: "ETHUSDT", Exchange: "kraken"},
	}
	keys := []string{"BTCUSDT/binance", "ETHUSDT/kraken"}

	if idx := m.searchInstrument(keys, "ETH kra"); idx != 1 {
		t.Errorf("searchInstrument(`ETH kra`) = %d, want 1", idx)
	}
	if idx := m.searchInstrument(keys, "btc bin"); idx != 0 {
		t.Errorf("searchInstrument(`btc bin`) = %d, want 0", idx)
	}
	if idx := m.searchInstrument(keys, "XRP"); idx != -1 {
		t.Errorf("searchInstrument(`XRP`) = %d, want -1 (no match)", idx)
	}
}

// TestStepActive_WithFilterSkipsNonMatches confirms the regression
// from the 2026-04-28 brief: typing `/SOL` then pressing ] on
// TRADES should walk only SOL entries across venues, never landing
// on BTCUSDT. Without filter the step is a plain wrap-around.
func TestStepActive_WithFilterSkipsNonMatches(t *testing.T) {
	m := New()
	m.tradeKeys = []string{
		"binance/BTCUSDT",
		"binance/SOLUSDT",
		"coinbase/BTC-USD",
		"coinbase/SOL-USD",
		"binance/ETHUSDT",
	}
	m.tradeData = map[string]*views.TradeViewData{
		"binance/BTCUSDT":  {Agg: &views.TradeAggData{Instrument: "BTCUSDT", Exchange: "binance"}},
		"binance/SOLUSDT":  {Agg: &views.TradeAggData{Instrument: "SOLUSDT", Exchange: "binance"}},
		"coinbase/BTC-USD": {Agg: &views.TradeAggData{Instrument: "BTC-USD", Exchange: "coinbase"}},
		"coinbase/SOL-USD": {Agg: &views.TradeAggData{Instrument: "SOL-USD", Exchange: "coinbase"}},
		"binance/ETHUSDT":  {Agg: &views.TradeAggData{Instrument: "ETHUSDT", Exchange: "binance"}},
	}

	// Start at SOLUSDT@binance with filter "SOL".
	idx := m.stepActive(m.tradeKeys, 1 /* SOLUSDT */, +1, "SOL")
	if idx != 3 {
		t.Errorf("step forward from SOL@binance with filter SOL = %d (%q), want 3 (SOL-USD@coinbase)",
			idx, m.tradeKeys[idx])
	}

	// Step back from SOL@coinbase with filter SOL → SOL@binance.
	idx = m.stepActive(m.tradeKeys, 3, -1, "SOL")
	if idx != 1 {
		t.Errorf("step back from SOL@coinbase with filter SOL = %d (%q), want 1",
			idx, m.tradeKeys[idx])
	}

	// No filter → plain wrap step.
	if got := m.stepActive(m.tradeKeys, 4, +1, ""); got != 0 {
		t.Errorf("unfiltered step from end = %d, want 0 (wrap)", got)
	}
}

// TestStepActive_NoMatchesLeavesIndexPut keeps the active selection
// stable when the user sets a filter that doesn't match any key —
// otherwise nav would silently zero the index.
func TestStepActive_NoMatchesLeavesIndexPut(t *testing.T) {
	m := New()
	m.tradeKeys = []string{"binance/BTCUSDT", "binance/ETHUSDT"}
	m.tradeData = map[string]*views.TradeViewData{
		"binance/BTCUSDT": {Agg: &views.TradeAggData{Instrument: "BTCUSDT", Exchange: "binance"}},
		"binance/ETHUSDT": {Agg: &views.TradeAggData{Instrument: "ETHUSDT", Exchange: "binance"}},
	}
	if got := m.stepActive(m.tradeKeys, 1, +1, "ZZZ"); got != 1 {
		t.Errorf("stepActive with no-match filter should leave idx put, got %d", got)
	}
}

func panelIndex(t *testing.T, name string) int {
	t.Helper()
	for i, p := range panelList {
		if p == name {
			return i
		}
	}
	t.Fatalf("panel %s not in panelList", name)
	return 0
}
