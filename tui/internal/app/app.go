// Package app implements the bubbletea application model for the Bloomberg-style TUI.
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/notbbg/notbbg/tui/internal/client"
	tuiconfig "github.com/notbbg/notbbg/tui/internal/config"
	"github.com/notbbg/notbbg/tui/internal/views"
)

// Panel identifiers.
const (
	PanelOHLC    = "OHLC"
	PanelLOB     = "LOB"
	PanelTrades  = "TRADES"
	PanelNews    = "NEWS"
	PanelAlerts  = "ALERTS"
	PanelSanity  = "SANITY"
	PanelMonitor = "MON"
	PanelLog     = "LOG"
	PanelAgent   = "AGENT"
	PanelSettings = "SETTINGS"
)

var panelList = []string{PanelOHLC, PanelLOB, PanelTrades, PanelNews, PanelAlerts, PanelSanity, PanelMonitor, PanelLog, PanelAgent, PanelSettings}

// panelContextHelp returns a per-tab help overlay for the currently
// active panel. Bound to `?` (global `h` still shows the full help
// text). Core panels pull from panelHelp + a tailored key-hint
// block; plugin panels summarise the plugin's input/output cell
// counts and the standard edit keybindings.
func (m Model) panelContextHelp() string {
	panels := m.allPanels()
	if len(panels) == 0 {
		return helpText
	}
	idx := m.activePanel
	if idx < 0 || idx >= len(panels) {
		idx = 0
	}
	name := panels[idx]

	if h, ok := panelHelp[name]; ok {
		// Core tab.
		var b strings.Builder
		b.WriteString("\n  ")
		b.WriteString(name)
		b.WriteString(" — ")
		b.WriteString(h[0])
		b.WriteString("\n\n")
		if h[1] != "" {
			b.WriteString("  Keys: ")
			b.WriteString(h[1])
			b.WriteString("\n\n")
		}
		switch name {
		case PanelOHLC:
			b.WriteString("  [ / ]  or  ← / →   Prev/next instrument\n")
			b.WriteString("  - / +               Prev/next timeframe\n")
			b.WriteString("  H                   Load 24 h history (non-blocking)\n")
			b.WriteString("  / <tokens> Enter    Filter sidebar (e.g. `BTC bin`)\n")
			b.WriteString("  ESC                 Clear sidebar filter\n")
		case PanelLOB:
			b.WriteString("  [ / ]               Prev/next instrument\n")
			b.WriteString("  / <tokens> Enter    Filter sidebar (e.g. `BTC bin`)\n")
			b.WriteString("  ESC                 Clear sidebar filter\n")
		case PanelTrades:
			b.WriteString("  [ / ]               Prev/next instrument\n")
			b.WriteString("  / <tokens> Enter    Filter sidebar (e.g. `ETH coin`)\n")
			b.WriteString("  ESC                 Clear sidebar filter\n")
		case PanelNews:
			b.WriteString("  j / k               Scroll\n")
			b.WriteString("  Enter               Open article URL\n")
			b.WriteString("  /                   Search headlines\n")
		case PanelAgent:
			b.WriteString("  /                   Start typing a prompt\n")
			b.WriteString("  Enter               Send to `claude -p`\n")
			b.WriteString("  j / k / G           Scroll / jump to bottom\n")
			b.WriteString("  o / O               Open latest NOTBBG:/path artifact\n")
		}
		b.WriteString("\n  Press h for the full global help, ESC to close.\n")
		return b.String()
	}

	// Plugin panel — look up its screen data.
	for _, s := range m.pluginScreens {
		if s.ID != name {
			continue
		}
		var b strings.Builder
		b.WriteString("\n  ")
		b.WriteString(s.Label)
		if s.Label != s.ID {
			b.WriteString(" (")
			b.WriteString(s.ID)
			b.WriteString(")")
		}
		b.WriteString("\n\n  Plugin screen served on topic: ")
		b.WriteString(s.Topic)
		b.WriteString("\n")
		if s.CellGrid {
			var inputs, outputs int
			for _, c := range s.Cells {
				switch c.Type {
				case "input", "enum", "selection", "script":
					inputs++
				default:
					outputs++
				}
			}
			b.WriteString(fmt.Sprintf("  Cells: %d input + %d output\n", inputs, outputs))
			b.WriteString("\n  Keys:\n")
			b.WriteString("    ↑ / ↓ / ← / →  or  j / k / h / l   Move cell cursor\n")
			b.WriteString("    Enter                               Begin editing\n")
			b.WriteString("    Esc                                 Cancel edit\n")
			b.WriteString("    Up / Down (on enum cells)           Cycle enum value\n")
			b.WriteString("    Tab (on selection cells)            Autocomplete ticker\n")
			b.WriteString("    X                                   Cancel the plugin's current job\n")
		} else {
			b.WriteString(fmt.Sprintf("  Streamed lines: %d buffered\n", len(s.Lines)))
		}
		b.WriteString("\n  Press h for the full global help, ESC to close.\n")
		return b.String()
	}

	// Fallback for unknown tab.
	return helpText
}

// panelHelp maps panel names to short description + key hints.
var panelHelp = map[string][2]string{
	PanelOHLC:    {"Candlestick charts with multi-timeframe OHLCV data", "[/]:pair  -/+:tf  /:filter  H:history"},
	PanelLOB:     {"Live limit order book depth (bids/asks)", "[/]:pair  /:filter  h:help"},
	PanelTrades:  {"Aggregated trade stats: VWAP, volume, quantiles", "[/]:pair  /:filter  h:help"},
	PanelNews:    {"Crypto news feed from RSS/GDELT sources", "j/k:nav  enter:read  /:search"},
	PanelAlerts:  {"Price and volume alerts", ""},
	PanelSanity:  {"Cross-venue median + per-venue mid divergence", "j/k:scroll  PgUp/PgDn  g/G:top/bottom"},
	PanelMonitor: {"Data feed health and connection status", "j/k:scroll  PgUp/PgDn  g/G:top/bottom"},
	PanelLog:     {"Server log output", "j/k:scroll  PgUp/PgDn  g/G:top/bottom"},
	PanelAgent:   {"AI agent terminal for analysis queries", "j/k:scroll  G:bottom  /:type  enter:send"},
	PanelSettings: {"Read-only snapshot of the resolved config", ""},
}

// Bloomberg color palette.
var (
	colorAmber = lipgloss.Color("#FF8C00")
	colorGreen = lipgloss.Color("#00FF00")
	colorRed   = lipgloss.Color("#FF4444")
	colorWhite = lipgloss.Color("#FFFFFF")
	colorDim   = lipgloss.Color("#666666")
	colorBg    = lipgloss.Color("#000000")

	topBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1A1A1A")).
			Foreground(colorAmber).
			Bold(true).
			Padding(0, 1)

	bottomBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1A1A1A")).
			Foreground(colorWhite).
			Padding(0, 1)

	activeTabStyle = lipgloss.NewStyle().
			Foreground(colorAmber).
			Bold(true).
			Underline(true)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	mainAreaStyle = lipgloss.NewStyle().
			Background(colorBg).
			Foreground(colorWhite)
)

const helpText = `
  NOTBBG — Bloomberg-style terminal for casual traders

  NAVIGATION
    TAB / Shift+TAB     Next / previous panel
    Alt+1-9             Jump to panel by number
    / or :              Enter command mode
    h                   Show this (global) help
    ?                   Show help for the current tab
    ESC                 Close overlay / cancel command
    q                   Quit

  CORE PANELS
    OHLC       Candlestick charts, multi-timeframe        [/]:pair  -/+:tf  /:filter
    LOB        Limit order book depth (bids/asks)         [/]:pair  /:filter
    TRADES     Aggregated trade stats, VWAP, quantiles    [/]:pair  /:filter
    NEWS       Crypto news feed                           j/k:nav  enter:read  /:search
    ALERTS     Price and volume alerts
    SANITY     Cross-venue median + outliers              j/k:scroll
    MON        Feed health and connection status
    LOG        Server log output
    AGENT      AI agent terminal                          j/k:scroll  enter:send  O:open artifact
    SETTINGS   Read-only snapshot of resolved config

  SCROLL  (overflowing panels: SETTINGS, MON, LOG, and overlays)
    j / k   or  Down / Up         Step by line
    PgDn / PgUp  or  Ctrl+F / B    Step by page
    g / G                          Jump to top / bottom

  PLUGIN PANELS (loaded from ~/.config/notbbg/plugins/)
    Navigate input cells: j/k or arrows   Edit: Enter   Cancel: Esc
    Enum inputs: up/down to cycle         Script: Enter opens editor (Ctrl+S saves)
    Selection inputs: Tab autocompletes against the live ticker list
    Cancel running job on active plugin: X (sends PluginJobCancel)

  OHLC & LOB & TRADES
    [ / ]  or  ← / →   Previous / next instrument
    - / +               Previous / next timeframe (OHLC only)
    / <tokens> Enter    Filter sidebar by instrument / exchange (e.g. 'BTC bin')
    ESC                 Clear sidebar filter
    H                   Load 24h history via DataRange (OHLC, non-blocking)

  COMMANDS (type / then command)
    BTC, ETH, SOL...    Jump to OHLC for that instrument
    LOB, TRADES, NEWS   Jump to panel
    ALERT SET <SYM> > <PRICE>    Create price alert
    ALERT SET KEYWORD <word>     Create keyword alert
    PAIR                Show QR code for phone pairing
    HELP                Show this help

  Press ESC to close this help.
`

// ---- Wire types (matching server transport.WireMsg) ----

type wireMsg struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type ohlcPayload struct {
	Instrument string  `json:"Instrument"`
	Exchange   string  `json:"Exchange"`
	Timeframe  string  `json:"Timeframe"`
	Timestamp  string  `json:"Timestamp"`
	Open       float64 `json:"Open"`
	High       float64 `json:"High"`
	Low        float64 `json:"Low"`
	Close      float64 `json:"Close"`
	Volume     float64 `json:"Volume"`
}

type lobPayload struct {
	Instrument string `json:"Instrument"`
	Exchange   string `json:"Exchange"`
	Bids       []struct {
		Price    float64 `json:"Price"`
		Quantity float64 `json:"Quantity"`
	} `json:"Bids"`
	Asks []struct {
		Price    float64 `json:"Price"`
		Quantity float64 `json:"Quantity"`
	} `json:"Asks"`
}

type feedStatusPayload struct {
	Name          string  `json:"Name"`
	State         string  `json:"State"`
	LastUpdate    string  `json:"LastUpdate"`
	LatencyMs     float64 `json:"LatencyMs"`
	ErrorCount    uint64  `json:"ErrorCount"`
	BytesReceived uint64  `json:"BytesReceived"`
}

type pluginStatusPayload struct {
	Name         string `json:"Name"`
	State        string `json:"State"`
	LastActivity string `json:"LastActivity"`
	PID          int    `json:"PID"`
	ErrorCount   uint64 `json:"ErrorCount"`
	Running      bool   `json:"Running"`
}

// ---- Bubbletea messages ----

type connectedMsg struct{}
type disconnectedMsg struct{}

// ---- Per-instrument OHLC state ----

type timeframeData struct {
	Candles    []views.Candle
	LastUpdate time.Time

	// Loading state for progressive DataRange fetches (press H on
	// OHLC). Loading flips true when a fetch starts; LoadSeq counts
	// chunks received so the view can show "⟳ loading N".
	Loading bool
	LoadSeq int32
	// AutoFetchedAt records the last auto-backfill attempt for
	// this (instrument, tf). Throttles the auto-fetch loop so a
	// failing fetch doesn't hammer the REST gateway.
	AutoFetchedAt time.Time
	// Progressive backfill state. ProgressIdx is the number of
	// chunks already fetched (newest-first). ProgressTotal is the
	// target chunk count for autoFetchWindow. Used to fire the
	// next older chunk on each chunk-EOF, so the chart fills
	// right-to-left (now → past) and the trader sees the most
	// recent bar first instead of waiting on a big batch.
	ProgressIdx   int
	ProgressTotal int
}

type instrumentData struct {
	Instrument  string
	Exchange    string
	Timeframes  map[string]*timeframeData // "1m", "5m", "1h", "1d"
	ActiveTF    string                    // currently viewed timeframe
	LastUpdate  time.Time
}

func instrumentKey(instrument, exchange string) string {
	return instrument + "/" + exchange
}

// LogFunc is a callback to fetch server log lines. Set by main.
var LogFunc func() []string


// ---- Model ----

// Model is the top-level bubbletea model.
type Model struct {
	width       int
	height      int
	activePanel int
	cmdInput    textinput.Model
	cmdMode     bool
	connected   bool
	clock       time.Time
	statusMsg   string
	msgCount    int

	// QR overlay (shown by PAIR command).
	qrOverlay string

	// OHLC per-instrument state.
	ohlcData          map[string]*instrumentData // key = "INSTRUMENT/exchange"
	ohlcKeys          []string                   // ordered keys (watchlist first, then discovery order)
	ohlcActiveIdx     int                        // index into ohlcKeys
	ohlcSidebarFilter string                     // live-filters sidebar; empty = show all

	// LOB per-instrument state.
	lobData          map[string]*views.LOBData // key = "INSTRUMENT/exchange"
	lobKeys          []string
	lobActiveIdx     int
	lobSidebarFilter string // mirrors ohlcSidebarFilter; empty = show all

	// News state.
	newsItems      []views.NewsItem
	newsSelectedIdx int
	newsDetail     bool   // viewing article detail
	newsFilter     string // active filter text
	// newsPreviewCache holds fetched-and-stripped article bodies
	// keyed by URL. The detail view shows the cached preview when
	// available, or "loading…" / the fetch error otherwise. Cap is
	// soft — entries are small and a busy session won't fill 256.
	newsPreviewCache map[string]*views.NewsPreview
	alertItems     []views.AlertEntry
	feedStatuses   []views.FeedStatusEntry
	pluginStatuses []views.PluginStatusEntry

	// Backpressure stats surfaced on MON (Phase 6).
	busStats  views.BusStatsEntry
	walStats  []views.WALStatsEntry

	// Cross-venue sanity snapshots, keyed by instrument. Server
	// publishes one event per pair on `sanity.prices`; we keep the
	// latest per key and re-render on each update.
	sanitySnapshots map[string]*views.SanitySnapshot

	// Remote-server log lines received over `server.log` topic.
	// LogFunc on Model surfaces these alongside any locally-launched
	// server's stdout buffer when the TUI is paired to a remote
	// server (sm.Logs is empty there).
	remoteLogLines []string

	// Agent output.
	agentScrollOff int // 0 = bottom (auto-scroll), >0 = lines from bottom

	// Generic panel scroll for read-only / overflowing views
	// (SETTINGS, MON, LOG, and the help / pair / context-help
	// overlays). 0 = top. Reset on any panel switch and whenever
	// an overlay opens or closes — keeps the cursor predictable
	// when navigating between tabs. AGENT and NEWS keep their own
	// scroll state because they have richer per-panel semantics.
	panelScrollOff int

	// Trade aggregates.
	tradeData           map[string]*views.TradeViewData // key: exchange/instrument
	tradeKeys           []string
	tradeActiveIdx      int
	tradesSidebarFilter string // mirrors ohlcSidebarFilter; empty = show all

	// GUI cache limits — populated from config at startup so hot paths
	// don't have to reload config. Zero values clamped to defaults.
	guiCache tuiconfig.GUICacheSettings

	// Plugin screens (dynamic tabs from plugins).
	pluginScreens  []views.PluginScreenData          // registered screens
	pluginData     map[string][]views.PluginStyledLine // topic → latest lines
	cellInput      textinput.Model                    // text input for editing plugin cells
	cellEditing    bool                               // true when editing a cell
	scriptEditor   *views.ScriptEditor                // nil when not editing a script
	scriptScreenID string                             // which screen the editor belongs to
	scriptRow      uint32                             // row of the script cell being edited
	scriptCol      uint32                             // col of the script cell being edited

	// Server connection channels.
	dataCh   chan []byte  // raw frames from server
	statusCh chan string  // connection status updates

	// Progressive history loading (DataRange) — the fetch goroutine
	// streams chunks here; the pollData loop drains and merges them
	// into ohlcData. nil client means feature disabled (e.g. HTTP
	// gateway off).
	histCh     chan historyEvent
	historyCli *client.DataRangeClient
}

// historyEvent is one DataRange chunk turned into app-level state.
// key+tf identify which timeframeData to update; new candles are
// already parsed so the main loop stays simple.
type historyEvent struct {
	key     string
	tf      string
	candles []views.Candle
	eof     bool
	err     error
}

// AgentFunc is a callback to fetch agent output lines. Set by main.
var AgentFunc func() []string

// AgentSendFunc sends a line of input to the embedded agent terminal. Set by main.
var AgentSendFunc func(line string)

// New creates the initial TUI model, restoring saved panel layout.
func New() Model {
	// Wire the alignToNow trace hook into the same /tmp/notbbg-ohlc.log
	// the realtime + history paths use. alignDebug fires per render
	// for the active panel's instrument only — flooding isn't a risk
	// at the 16 ms tick.
	views.SetAlignDebug(ohlcDebug)

	ti := textinput.New()
	ti.Placeholder = "Type command... (TAB to switch panels, / for command)"
	ti.CharLimit = 256
	ti.Width = 80

	activePanel := 0
	cfg, _ := tuiconfig.Load("")
	if cfg != nil {
		activePanel = cfg.Panels.ActivePanel
		// Clamp to core panel count; plugin panels may not exist at startup.
		if activePanel < 0 || activePanel >= len(panelList) {
			activePanel = 0
		}
	}

	// Seed OHLC instrument list from watchlist config.
	ohlcData := make(map[string]*instrumentData)
	var ohlcKeys []string
	if cfg != nil {
		ohlcKeys = append(ohlcKeys, cfg.Watchlist...)
	}

	ci := textinput.New()
	ci.CharLimit = 64
	ci.Width = 20

	var guiCache tuiconfig.GUICacheSettings
	if cfg != nil {
		guiCache = cfg.GUI.Cache
	}
	guiCache = guiCache.WithDefaults()

	return Model{
		cmdInput:    ti,
		cellInput:   ci,
		activePanel: activePanel,
		clock:       time.Now(),
		statusMsg:   "Connecting...",
		dataCh:      make(chan []byte, 8192),
		statusCh:    make(chan string, 16),
		ohlcData:        ohlcData,
		ohlcKeys:        ohlcKeys,
		lobData:         make(map[string]*views.LOBData),
		tradeData:       make(map[string]*views.TradeViewData),
		pluginData:      make(map[string][]views.PluginStyledLine),
		sanitySnapshots:  make(map[string]*views.SanitySnapshot),
		newsPreviewCache: make(map[string]*views.NewsPreview),
		guiCache:    guiCache,
		histCh:      make(chan historyEvent, 64),
		historyCli:  client.NewDataRangeClient(""),
	}
}

// allPanels returns the core panels plus any registered plugin screen IDs.
func (m Model) allPanels() []string {
	panels := make([]string, len(panelList))
	copy(panels, panelList)
	for _, s := range m.pluginScreens {
		panels = append(panels, s.ID)
	}
	return panels
}

// panelName resolves an active-panel index back to the canonical
// panel constant (PanelOHLC, PanelLOB, ...). Returns "" when the
// index is out of bounds. Use this everywhere instead of bare
// integer comparisons — the panel order has shifted twice already
// (TRADES insertion + plugin screens) and hardcoded indices kept
// silently drifting onto the wrong tab.
func (m Model) panelName(i int) string {
	panels := m.allPanels()
	if i < 0 || i >= len(panels) {
		return ""
	}
	return panels[i]
}

// activePanelName is shorthand for panelName(activePanel) — used in
// every key handler to gate behaviour on the currently-rendered tab.
func (m Model) activePanelName() string { return m.panelName(m.activePanel) }

// saveLayout persists the current panel to config.
func (m *Model) saveLayout() {
	cfg, _ := tuiconfig.Load("")
	if cfg == nil {
		cfg = &tuiconfig.UserConfig{}
	}
	cfg.Panels.ActivePanel = m.activePanel
	_ = tuiconfig.Save("", cfg)
}

// DataChan returns the channel to push raw server frames into the model.
func (m *Model) DataChan() chan<- []byte {
	return m.dataCh
}

// StatusChan returns the channel for connection status updates.
func (m *Model) StatusChan() chan<- string {
	return m.statusCh
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type pollDataMsg struct{}

func pollDataCmd() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(_ time.Time) tea.Msg {
		return pollDataMsg{}
	})
}

// newsPreviewMsg is delivered when an article fetch completes (or
// fails). Body is the stripped-to-text content for "ready" results;
// ErrMsg holds the human-readable failure reason for "error".
type newsPreviewMsg struct {
	URL    string
	Status string
	Body   string
	ErrMsg string
}

// fetchNewsPreviewCmd returns a tea.Cmd that GETs the article URL,
// strips HTML to text, and dispatches a newsPreviewMsg. Capped at
// 10s timeout + 2 MiB body so a slow / huge page can't stall the
// model goroutine. Concurrent fetches for the same URL are the
// caller's problem to deduplicate (we track that via the cache's
// "loading" entry).
func fetchNewsPreviewCmd(url string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return newsPreviewMsg{URL: url, Status: "error", ErrMsg: err.Error()}
		}
		req.Header.Set("User-Agent", "notbbg-tui/0.2 (article-preview)")
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		resp, err := client.Do(req)
		if err != nil {
			return newsPreviewMsg{URL: url, Status: "error", ErrMsg: err.Error()}
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return newsPreviewMsg{URL: url, Status: "error", ErrMsg: resp.Status}
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		if err != nil {
			return newsPreviewMsg{URL: url, Status: "error", ErrMsg: err.Error()}
		}
		text := views.StripHTML(string(body))
		// Trim the boilerplate-heavy front matter most sites prefix
		// (nav, cookie banners). Heuristic: drop everything before
		// the first paragraph longer than 200 chars, which usually
		// lands on the article lead. Safe-falls through unchanged
		// when no such paragraph exists.
		if idx := firstLongParaIdx(text, 200); idx > 0 {
			text = text[idx:]
		}
		return newsPreviewMsg{URL: url, Status: "ready", Body: text}
	}
}

// firstLongParaIdx scans the cleaned-text body and returns the
// byte offset of the first paragraph whose length crosses the
// threshold. -1 when none qualifies.
func firstLongParaIdx(text string, threshold int) int {
	cursor := 0
	for _, p := range strings.Split(text, "\n") {
		if len(p) >= threshold {
			return cursor
		}
		cursor += len(p) + 1 // +1 for the newline
	}
	return -1
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), textinput.Blink, pollDataCmd())
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.cmdInput.Width = msg.Width - 4
		return m, nil

	case newsPreviewMsg:
		if m.newsPreviewCache == nil {
			m.newsPreviewCache = make(map[string]*views.NewsPreview)
		}
		m.newsPreviewCache[msg.URL] = &views.NewsPreview{
			Status: msg.Status, Body: msg.Body, ErrMsg: msg.ErrMsg,
		}
		return m, nil

	case tickMsg:
		m.clock = time.Time(msg)
		return m, tickCmd()

	case pollDataMsg:
		// Drain connection status updates.
		for {
			select {
			case s := <-m.statusCh:
				switch s {
				case "connected":
					m.connected = true
					m.statusMsg = "Connected"
				case "disconnected":
					m.connected = false
					m.statusMsg = "Disconnected — reconnecting..."
				default:
					m.statusMsg = s
				}
			default:
				goto statusDone
			}
		}
	statusDone:

		// Drain all pending data frames.
		drained := 0
		for {
			select {
			case raw := <-m.dataCh:
				var wire wireMsg
				if json.Unmarshal(raw, &wire) == nil {
					m.handleServerData(wire)
					drained++
				}
			default:
				goto dataDone
			}
		}
	dataDone:
		if drained > 0 {
			m.connected = true
			if m.msgCount < 50000 {
				m.msgCount += drained
			}
			if m.msgCount >= 50000 {
				m.statusMsg = fmt.Sprintf("Live | %d+ msgs", 50000)
			} else {
				m.statusMsg = fmt.Sprintf("Live | %d msgs", m.msgCount)
			}
		}

		// Drain progressive history chunks (non-blocking). Runs on the
		// same 16ms tick so the UI stays responsive while DataRange
		// streams.
		for {
			select {
			case ev := <-m.histCh:
				m.applyHistoryEvent(ev)
			default:
				goto histDone
			}
		}
	histDone:

		// Auto-backfill the active OHLC instrument's TF when its
		// candle store is too thin to fill the visible chart. Trader
		// shouldn't have to press H — the chart populates itself.
		// Call is idempotent + throttled inside.
		m.AutoLoadHistoryIfNeeded()

		return m, pollDataCmd()

	case connectedMsg:
		m.connected = true
		m.statusMsg = "Connected"
		return m, nil

	case disconnectedMsg:
		m.connected = false
		m.statusMsg = "Disconnected"
		return m, nil
	}

	if m.cmdMode {
		var cmd tea.Cmd
		m.cmdInput, cmd = m.cmdInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) handleServerData(wire wireMsg) {
	if wire.Type != "update" {
		return
	}

	topic := wire.Topic

	switch {
	case strings.HasPrefix(topic, "ohlc."):
		var p ohlcPayload
		if json.Unmarshal(wire.Payload, &p) != nil {
			return
		}
		key := instrumentKey(p.Instrument, p.Exchange)
		d, ok := m.ohlcData[key]
		if !ok {
			d = &instrumentData{
				Instrument: p.Instrument,
				Exchange:   p.Exchange,
				Timeframes: make(map[string]*timeframeData),
				ActiveTF:   p.Timeframe,
			}
			m.ohlcData[key] = d
			// Insert into keys list. If the instrument matches a watchlist placeholder, replace it.
			replaced := false
			for i, k := range m.ohlcKeys {
				if strings.EqualFold(k, p.Instrument) {
					m.ohlcKeys[i] = key
					replaced = true
					break
				}
			}
			if !replaced {
				m.ohlcKeys = append(m.ohlcKeys, key)
			}
		}
		d.LastUpdate = time.Now()

		// Store candles per timeframe.
		tf := p.Timeframe
		if tf == "" {
			tf = "1m"
		}
		tfd, ok := d.Timeframes[tf]
		if !ok {
			tfd = &timeframeData{}
			d.Timeframes[tf] = tfd
		}
		tfd.LastUpdate = time.Now()
		ts, err := time.Parse(time.RFC3339Nano, p.Timestamp)
		if err != nil || ts.IsZero() {
			// A candle without a real timestamp is useless — the
			// time-anchored chart would either skip it (alignToNow
			// drops zero-ts bars) or, worse, place it at epoch zero
			// and blow out the chart range. Drop at ingest so the
			// upstream feed has to surface the gap explicitly.
			return
		}
		candle := views.Candle{
			Timestamp: ts,
			Open:      p.Open,
			High:      p.High,
			Low:       p.Low,
			Close:     p.Close,
			Volume:    p.Volume,
		}
		// Insert/replace in time-sorted order. The trader-sees-now
		// invariant requires the chart's right edge to map to the
		// most recent bar by *timestamp*, never by array position —
		// otherwise a backfill chunk arriving after realtime ticks
		// pushes old bars to the array tail and the chart silently
		// "replays the past". Ingest path enforces the order so the
		// renderer can trust candles[len-1].Timestamp == newest.
		tfd.Candles = upsertCandle(tfd.Candles, candle)
		cap := m.guiCache.OHLCRowsPerInstrument
		if cap <= 0 {
			cap = tuiconfig.DefaultOHLCRowsPerInstrument
		}
		if len(tfd.Candles) > cap {
			tfd.Candles = tfd.Candles[len(tfd.Candles)-cap:]
		}
		// Debug — trace realtime ohlc ingest for the watched
		// instruments only. /tmp/notbbg-ohlc.log
		if len(tfd.Candles) > 0 && ohlcDebugMatch(p.Exchange, p.Instrument) {
			first := tfd.Candles[0].Timestamp.UTC().Format("01-02 15:04:05")
			last := tfd.Candles[len(tfd.Candles)-1].Timestamp.UTC().Format("01-02 15:04:05")
			ohlcDebug("RT  %s/%s/%s ts=%s len=%d span=[%s..%s]",
				p.Exchange, p.Instrument, tf,
				ts.UTC().Format("01-02 15:04:05"),
				len(tfd.Candles), first, last)
		}

	case strings.HasPrefix(topic, "lob."):
		var p lobPayload
		if json.Unmarshal(wire.Payload, &p) != nil {
			return
		}
		key := instrumentKey(p.Instrument, p.Exchange)
		d, exists := m.lobData[key]
		if !exists {
			d = &views.LOBData{Instrument: p.Instrument, Exchange: p.Exchange}
			m.lobData[key] = d
			m.lobKeys = append(m.lobKeys, key)
		}
		d.Instrument = p.Instrument
		d.Exchange = p.Exchange
		d.Bids = make([]views.LOBLevel, len(p.Bids))
		for i, b := range p.Bids {
			d.Bids[i] = views.LOBLevel{Price: b.Price, Quantity: b.Quantity}
		}
		d.Asks = make([]views.LOBLevel, len(p.Asks))
		for i, a := range p.Asks {
			d.Asks[i] = views.LOBLevel{Price: a.Price, Quantity: a.Quantity}
		}
		d.LastUpdate = time.Now()

	case topic == "news":
		var raw struct {
			Title     string   `json:"Title"`
			Body      string   `json:"Body"`
			Source    string   `json:"Source"`
			URL       string   `json:"URL"`
			Published string   `json:"Published"`
			Tickers   []string `json:"Tickers"`
		}
		if json.Unmarshal(wire.Payload, &raw) != nil {
			return
		}
		ts := time.Now()
		if t, err := time.Parse(time.RFC3339, raw.Published); err == nil {
			ts = t
		}
		item := views.NewsItem{
			Title:     raw.Title,
			Source:    raw.Source,
			Timestamp: ts,
			Tickers:   raw.Tickers,
			Body:      raw.Body,
			URL:       raw.URL,
		}
		if item.Title != "" {
			// Snapshot fetch + RSS first-poll can deliver historical
			// items out of order; sort defensively by Timestamp DESC
			// so the panel always shows newest-first regardless of
			// arrival order. Cap is a memory-bound safety net only —
			// users explicitly want long history visible, so it sits
			// well above any plausible session count.
			m.newsItems = append([]views.NewsItem{item}, m.newsItems...)
			sort.SliceStable(m.newsItems, func(i, j int) bool {
				return m.newsItems[i].Timestamp.After(m.newsItems[j].Timestamp)
			})
			if len(m.newsItems) > 10000 {
				m.newsItems = m.newsItems[:10000]
			}
		}

	case topic == "feed.status":
		var p feedStatusPayload
		if json.Unmarshal(wire.Payload, &p) != nil {
			return
		}
		ts, _ := time.Parse(time.RFC3339, p.LastUpdate)
		entry := views.FeedStatusEntry{
			Name:       p.Name,
			State:      p.State,
			LastUpdate: ts,
			LatencyMs:  p.LatencyMs,
			ErrorCount: p.ErrorCount,
		}
		// Update or append.
		found := false
		for i, e := range m.feedStatuses {
			if e.Name == p.Name {
				m.feedStatuses[i] = entry
				found = true
				break
			}
		}
		if !found {
			m.feedStatuses = append(m.feedStatuses, entry)
		}

	case topic == "plugin.status":
		var p pluginStatusPayload
		if json.Unmarshal(wire.Payload, &p) != nil {
			return
		}
		ts, _ := time.Parse(time.RFC3339, p.LastActivity)
		entry := views.PluginStatusEntry{
			Name:         p.Name,
			State:        p.State,
			LastActivity: ts,
			PID:          p.PID,
			ErrorCount:   p.ErrorCount,
			Running:      p.Running,
		}
		found := false
		for i, e := range m.pluginStatuses {
			if e.Name == p.Name {
				m.pluginStatuses[i] = entry
				found = true
				break
			}
		}
		if !found {
			m.pluginStatuses = append(m.pluginStatuses, entry)
		}

	case topic == "sanity.prices":
		var p views.SanitySnapshot
		if json.Unmarshal(wire.Payload, &p) != nil {
			return
		}
		if p.Instrument == "" {
			return
		}
		// Stamp local clock for the "Ns ago" display — server's
		// timestamp is RFC3339 but we want clock-skew-resistant age.
		p.LastUpdate = time.Now()
		m.sanitySnapshots[p.Instrument] = &p

	case topic == "server.log":
		var p struct {
			Time    string `json:"Time"`
			Level   string `json:"Level"`
			Message string `json:"Message"`
		}
		if json.Unmarshal(wire.Payload, &p) == nil {
			line := fmt.Sprintf("%s [%s] %s", p.Time, p.Level, p.Message)
			m.remoteLogLines = append(m.remoteLogLines, line)
			if len(m.remoteLogLines) > 500 {
				m.remoteLogLines = m.remoteLogLines[len(m.remoteLogLines)-500:]
			}
		}

	case topic == "bus.stats":
		var p struct {
			Subscribers int    `json:"subscribers"`
			Topics      int    `json:"topics"`
			Dropped     uint64 `json:"dropped"`
		}
		if json.Unmarshal(wire.Payload, &p) == nil {
			m.busStats = views.BusStatsEntry{
				Subscribers: p.Subscribers,
				Topics:      p.Topics,
				Dropped:     p.Dropped,
				LastUpdate:  time.Now(),
			}
		}

	case topic == "wal.cache.stats" || topic == "wal.datalake.stats":
		var p struct {
			Name        string `json:"name"`
			Enqueued    uint64 `json:"enqueued"`
			Written     int64  `json:"written"`
			BusDropped  uint64 `json:"bus_dropped"`
			WALDropped  uint64 `json:"wal_dropped"`
			BytesOnDisk int64  `json:"bytes_on_disk"`
			Segments    int    `json:"segments"`
		}
		if json.Unmarshal(wire.Payload, &p) == nil && p.Name != "" {
			entry := views.WALStatsEntry{
				Name:        p.Name,
				Enqueued:    p.Enqueued,
				Written:     p.Written,
				BusDropped:  p.BusDropped,
				WALDropped:  p.WALDropped,
				BytesOnDisk: p.BytesOnDisk,
				Segments:    p.Segments,
				LastUpdate:  time.Now(),
			}
			replaced := false
			for i, w := range m.walStats {
				if w.Name == entry.Name {
					m.walStats[i] = entry
					replaced = true
					break
				}
			}
			if !replaced {
				m.walStats = append(m.walStats, entry)
			}
		}

	case strings.HasPrefix(topic, "trade.agg."):
		var agg struct {
			Instrument string  `json:"Instrument"`
			Exchange   string  `json:"Exchange"`
			Count      int64   `json:"Count"`
			Volume     float64 `json:"Volume"`
			BuyVolume  float64 `json:"BuyVolume"`
			SellVolume float64 `json:"SellVolume"`
			VWAP       float64 `json:"VWAP"`
			Open       float64 `json:"Open"`
			High       float64 `json:"High"`
			Low        float64 `json:"Low"`
			Close      float64 `json:"Close"`
			Turnover   float64 `json:"Turnover"`
			P25        float64 `json:"P25"`
			P50        float64 `json:"P50"`
			P75        float64 `json:"P75"`
		}
		if json.Unmarshal(wire.Payload, &agg) != nil {
			return
		}
		key := agg.Exchange + "/" + agg.Instrument
		td, ok := m.tradeData[key]
		if !ok {
			td = &views.TradeViewData{}
			m.tradeData[key] = td
			m.tradeKeys = append(m.tradeKeys, key)
		}
		td.Agg = &views.TradeAggData{
			Instrument: agg.Instrument, Exchange: agg.Exchange,
			Count: agg.Count, Volume: agg.Volume,
			BuyVolume: agg.BuyVolume, SellVolume: agg.SellVolume,
			VWAP: agg.VWAP, Open: agg.Open, High: agg.High, Low: agg.Low, Close: agg.Close,
			Turnover: agg.Turnover, P25: agg.P25, P50: agg.P50, P75: agg.P75,
		}

	case strings.HasPrefix(topic, "trade.snap."):
		var snap struct {
			Instrument string `json:"Instrument"`
			Exchange   string `json:"Exchange"`
			Trades     []struct {
				Price     float64 `json:"Price"`
				Quantity  float64 `json:"Quantity"`
				Side      string  `json:"Side"`
				Timestamp string  `json:"Timestamp"`
			} `json:"Trades"`
		}
		if json.Unmarshal(wire.Payload, &snap) != nil {
			return
		}
		key := snap.Exchange + "/" + snap.Instrument
		td, ok := m.tradeData[key]
		if !ok {
			td = &views.TradeViewData{}
			m.tradeData[key] = td
			m.tradeKeys = append(m.tradeKeys, key)
		}
		td.Trades = nil
		for _, t := range snap.Trades {
			ts, _ := time.Parse(time.RFC3339Nano, t.Timestamp)
			td.Trades = append(td.Trades, views.TradeSnapEntry{
				Price: t.Price, Quantity: t.Quantity, Side: t.Side, Timestamp: ts,
			})
		}

	case topic == "alert":
		var raw map[string]any
		if json.Unmarshal(wire.Payload, &raw) != nil {
			return
		}
		entry := views.AlertEntry{
			ID:         fmt.Sprint(raw["ID"]),
			Type:       fmt.Sprint(raw["Type"]),
			Instrument: fmt.Sprint(raw["Instrument"]),
			Status:     fmt.Sprint(raw["Status"]),
			CreatedAt:  time.Now(),
		}
		m.alertItems = append([]views.AlertEntry{entry}, m.alertItems...)

	case topic == "plugin.registry":
		// Parse plugin screen registry.
		var reg struct {
			Screens []struct {
				ID     string `json:"id"`
				Plugin string `json:"plugin"`
				Label  string `json:"label"`
				Icon   string `json:"icon"`
				Topic  string `json:"topic"`
			} `json:"screens"`
		}
		if json.Unmarshal(wire.Payload, &reg) != nil {
			return
		}
		m.pluginScreens = nil
		for _, s := range reg.Screens {
			m.pluginScreens = append(m.pluginScreens, views.PluginScreenData{
				ID:        s.ID,
				Label:     s.Label,
				Topic:     s.Topic,
				CursorRow: -1,
				CursorCol: -1,
			})
		}

	case strings.HasPrefix(topic, "plugin.") && strings.HasSuffix(topic, ".screen"):
		// Try cell grid update first (version=cellgrid/v1).
		var gridUpdate struct {
			ScreenID    string             `json:"screen_id"`
			Version     string             `json:"version"`
			Cells       []views.PluginCell `json:"cells"`
			FullReplace bool               `json:"full_replace"`
		}
		if json.Unmarshal(wire.Payload, &gridUpdate) == nil && gridUpdate.Version == "cellgrid/v1" {
			for i := range m.pluginScreens {
				if m.pluginScreens[i].Topic == topic {
					m.pluginScreens[i].CellGrid = true
					if gridUpdate.FullReplace {
						m.pluginScreens[i].Cells = gridUpdate.Cells
					} else {
						m.pluginScreens[i].Cells = mergeCells(m.pluginScreens[i].Cells, gridUpdate.Cells)
					}
					break
				}
			}
			return
		}

		// Legacy styled-line update.
		var update struct {
			ScreenID string                   `json:"screen_id"`
			Lines    []views.PluginStyledLine `json:"lines"`
		}
		if json.Unmarshal(wire.Payload, &update) != nil {
			return
		}
		m.pluginData[topic] = update.Lines
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Script editor overlay intercepts all keys.
	if m.scriptEditor != nil {
		return m.handleScriptEditorKey(msg)
	}

	switch msg.String() {
	case "ctrl+c", "q":
		if !m.cmdMode {
			m.saveLayout()
			return m, tea.Quit
		}
	case "tab":
		m.activePanel = (m.activePanel + 1) % len(m.allPanels())
		m.qrOverlay = ""
		m.panelScrollOff = 0
		return m, nil
	case "shift+tab":
		m.activePanel = (m.activePanel - 1 + len(m.allPanels())) % len(m.allPanels())
		m.panelScrollOff = 0
		return m, nil
	case "/", ":":
		if !m.cmdMode {
			m.cmdMode = true
			m.cmdInput.Focus()
			// On AGENT panel, set placeholder to shell prompt.
			if m.activePanelName() == PanelAgent {
				m.cmdInput.Placeholder = "Ask claude anything..."
			} else {
				m.cmdInput.Placeholder = "Type command... (TAB to switch panels, / for command)"
			}
			return m, textinput.Blink
		}
	case "esc":
		if m.cmdMode {
			m.cmdMode = false
			m.cmdInput.Blur()
			m.cmdInput.Reset()
			return m, nil
		}
		if m.qrOverlay != "" {
			m.qrOverlay = ""
			m.panelScrollOff = 0
			return m, nil
		}
	case "enter":
		if m.cmdMode {
			input := m.cmdInput.Value()
			// On AGENT panel, send input and stay in command mode for more input.
			if m.activePanelName() == PanelAgent && AgentSendFunc != nil {
				AgentSendFunc(input)
				m.cmdInput.Reset()
				m.agentScrollOff = 0 // auto-scroll to bottom to see response
				return m, nil
			}
			m.processCommand(input)
			m.cmdMode = false
			m.cmdInput.Blur()
			m.cmdInput.Reset()
			return m, nil
		}
	}

	if !m.cmdMode {
		switch msg.String() {
		case "h":
			m.qrOverlay = helpText
			m.panelScrollOff = 0
			return m, nil
		case "?":
			m.qrOverlay = m.panelContextHelp()
			m.panelScrollOff = 0
			return m, nil
		}

		// alt+1 through alt+9 jump to panels (digit keys reserved for cell
		// editing; terminals don't emit distinct codes for ctrl+digit, so
		// alt+digit is the only modifier+digit combo that works portably).
		if strings.HasPrefix(msg.String(), "alt+") && len(msg.String()) == 5 {
			ch := msg.String()[4]
			if ch >= '1' && ch <= '9' {
				idx := int(ch - '1')
				if idx < len(m.allPanels()) {
					m.activePanel = idx
					m.panelScrollOff = 0
				}
				return m, nil
			}
		}

		// j/k single-line scroll — narrow set of read-only panels +
		// any active overlay. j/k is taken elsewhere for nav/cursor
		// (OHLC/LOB/TRADES instrument step, NEWS item nav, AGENT
		// scroll, plugin grid cursor) so we only bind it where it
		// won't shadow another action.
		if !m.cmdMode && m.scrollableJKNow() {
			switch msg.String() {
			case "j", "down":
				m.panelScrollOff++
				m.clampPanelScroll()
				return m, nil
			case "k", "up":
				if m.panelScrollOff > 0 {
					m.panelScrollOff--
				}
				return m, nil
			}
		}

		// Page-step + jump scroll — works on every panel that has
		// content overflowing the visible window. PgUp/PgDn/Ctrl+F/
		// Ctrl+B/g/G don't collide with any panel-specific nav so
		// the active surface (panel content or overlay) scrolls
		// wherever there's overflow. This is the "/PAIR + most
		// screens should slide up/down" fix from 2026-04-28
		// ski.txt.
		if !m.cmdMode && m.scrollableNow() {
			switch msg.String() {
			case "pgdown", "ctrl+f":
				m.panelScrollOff += 10
				m.clampPanelScroll()
				return m, nil
			case "pgup", "ctrl+b":
				m.panelScrollOff -= 10
				if m.panelScrollOff < 0 {
					m.panelScrollOff = 0
				}
				return m, nil
			case "g":
				m.panelScrollOff = 0
				return m, nil
			case "G":
				// Jump to bottom — clamp picks the largest valid
				// offset given the current content + window size.
				m.panelScrollOff = 1 << 20
				m.clampPanelScroll()
				return m, nil
			}
		}

		// Agent terminal scrolling.
		if m.activePanelName() == PanelAgent && !m.cmdMode {
			switch msg.String() {
			case "up", "k":
				m.agentScrollOff += 3
				return m, nil
			case "down", "j":
				m.agentScrollOff -= 3
				if m.agentScrollOff < 0 {
					m.agentScrollOff = 0
				}
				return m, nil
			case "pgup":
				m.agentScrollOff += 20
				return m, nil
			case "pgdown":
				m.agentScrollOff -= 20
				if m.agentScrollOff < 0 {
					m.agentScrollOff = 0
				}
				return m, nil
			case "G":
				m.agentScrollOff = 0 // jump to bottom
				return m, nil
			case "o", "O":
				// Open the latest NOTBBG:<path> artifact the agent
				// emitted, via the platform's default app (macOS
				// `open`, Linux `xdg-open`). No-op when no artifact
				// is known yet.
				if AgentFunc != nil {
					if path := latestAgentArtifact(AgentFunc()); path != "" {
						if err := openArtifact(path); err != nil {
							m.statusMsg = fmt.Sprintf("open %s: %v", path, err)
						} else {
							m.statusMsg = fmt.Sprintf("opened %s", path)
						}
					} else {
						m.statusMsg = "no NOTBBG: artifact in agent output yet"
					}
				}
				return m, nil
			}
		}

		// News navigation. Use the panel-name lookup rather than a
		// hardcoded index so adding TRADES (or any future panel)
		// in front of NEWS doesn't silently shift the gate onto the
		// wrong tab. Hit when activePanel resolves to PanelNews.
		if m.panelName(m.activePanel) == PanelNews {
			switch msg.String() {
			case "up", "k":
				if m.newsDetail {
					break
				}
				if m.newsSelectedIdx > 0 {
					m.newsSelectedIdx--
				}
				return m, nil
			case "down", "j":
				if m.newsDetail {
					break
				}
				max := len(m.newsItems) - 1
				if m.newsFilter != "" {
					max = len(views.FilterNews(m.newsItems, m.newsFilter)) - 1
				}
				if m.newsSelectedIdx < max {
					m.newsSelectedIdx++
				}
				return m, nil
			case "enter":
				m.newsDetail = !m.newsDetail
				m.panelScrollOff = 0
				if m.newsDetail {
					// Kick off article fetch for the selected item.
					// Cache check + insert "loading" marker so
					// subsequent re-enters don't refire the fetch
					// while one is in flight.
					filtered := m.newsItems
					if m.newsFilter != "" {
						filtered = views.FilterNews(m.newsItems, m.newsFilter)
					}
					if m.newsSelectedIdx >= 0 && m.newsSelectedIdx < len(filtered) {
						url := filtered[m.newsSelectedIdx].URL
						if url != "" {
							if m.newsPreviewCache == nil {
								m.newsPreviewCache = make(map[string]*views.NewsPreview)
							}
							if _, cached := m.newsPreviewCache[url]; !cached {
								m.newsPreviewCache[url] = &views.NewsPreview{Status: "loading"}
								return m, fetchNewsPreviewCmd(url)
							}
						}
					}
				}
				return m, nil
			case "esc":
				if m.newsDetail {
					m.newsDetail = false
					return m, nil
				}
				if m.newsFilter != "" {
					m.newsFilter = ""
					m.newsSelectedIdx = 0
					return m, nil
				}
			}
		}

		// ESC on OHLC / LOB / TRADES clears that panel's sidebar
		// filter, matching NEWS behavior. Each panel tracks its own
		// filter so clearing one doesn't blow away the others.
		if msg.String() == "esc" {
			switch m.activePanelName() {
			case PanelOHLC:
				if m.ohlcSidebarFilter != "" {
					m.ohlcSidebarFilter = ""
					m.statusMsg = "OHLC filter cleared"
					return m, nil
				}
			case PanelLOB:
				if m.lobSidebarFilter != "" {
					m.lobSidebarFilter = ""
					m.statusMsg = "LOB filter cleared"
					return m, nil
				}
			case PanelTrades:
				if m.tradesSidebarFilter != "" {
					m.tradesSidebarFilter = ""
					m.statusMsg = "Trades filter cleared"
					return m, nil
				}
			}
		}

		// Instrument navigation — gated on the panel NAME so adding
		// or reordering panels never silently breaks step keys. When
		// a sidebar filter is set, [/] walks only filtered entries
		// (so typing `/SOL` then [ / ] cycles SOL pairs across
		// venues, never accidentally jumping to BTCUSDT).
		switch msg.String() {
		case "[", "left":
			switch m.activePanelName() {
			case PanelOHLC:
				m.ohlcActiveIdx = m.stepActive(m.ohlcKeys, m.ohlcActiveIdx, -1, m.ohlcSidebarFilter)
			case PanelLOB:
				m.lobActiveIdx = m.stepActive(m.lobKeys, m.lobActiveIdx, -1, m.lobSidebarFilter)
			case PanelTrades:
				m.tradeActiveIdx = m.stepActive(m.tradeKeys, m.tradeActiveIdx, -1, m.tradesSidebarFilter)
			}
			return m, nil
		case "]", "right":
			switch m.activePanelName() {
			case PanelOHLC:
				m.ohlcActiveIdx = m.stepActive(m.ohlcKeys, m.ohlcActiveIdx, +1, m.ohlcSidebarFilter)
			case PanelLOB:
				m.lobActiveIdx = m.stepActive(m.lobKeys, m.lobActiveIdx, +1, m.lobSidebarFilter)
			case PanelTrades:
				m.tradeActiveIdx = m.stepActive(m.tradeKeys, m.tradeActiveIdx, +1, m.tradesSidebarFilter)
			}
			return m, nil
		case "-", "{":
			if m.activePanelName() == PanelOHLC {
				m.cycleTF(-1)
			}
			return m, nil
		case "=", "+", "}":
			if m.activePanelName() == PanelOHLC {
				m.cycleTF(1)
			}
			return m, nil
		case "H":
			// Progressive history load over /api/v1/datarange (async).
			// The UI stays fully responsive while chunks stream in.
			if m.activePanelName() == PanelOHLC {
				// 0 = use the per-TF auto schedule, fetching the
				// recent past first then progressively deeper.
				m.startHistoryLoad(0)
				m.statusMsg = "Loading history (recent past first)…"
			}
			return m, nil
		}
	}

	// Plugin cell navigation and editing.
	pluginIdx := m.activePanel - len(panelList)
	if pluginIdx >= 0 && pluginIdx < len(m.pluginScreens) {
		screen := &m.pluginScreens[pluginIdx]
		if screen.CellGrid {
			return m.handlePluginCellKey(screen, msg)
		}
	}

	if m.cmdMode {
		var cmd tea.Cmd
		m.cmdInput, cmd = m.cmdInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handlePluginCellKey(screen *views.PluginScreenData, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If editing, delegate to cell input or handle enum cycling.
	if m.cellEditing {
		cell := screen.CellAt(screen.CursorRow, screen.CursorCol)
		if cell != nil && cell.Type == "input_enum" {
			switch msg.String() {
			case "up", "k":
				if screen.EnumIdx > 0 {
					screen.EnumIdx--
					screen.EditValue = cell.Options[screen.EnumIdx].Label
				}
				return m, nil
			case "down", "j":
				if screen.EnumIdx < len(cell.Options)-1 {
					screen.EnumIdx++
					screen.EditValue = cell.Options[screen.EnumIdx].Label
				}
				return m, nil
			case "enter":
				m.cellEditing = false
				screen.Editing = false
				val := cell.Options[screen.EnumIdx].Value
				m.sendPluginInput(screen, uint32(screen.CursorRow), uint32(screen.CursorCol), val)
				return m, nil
			case "esc":
				m.cellEditing = false
				screen.Editing = false
				return m, nil
			}
			return m, nil
		}

		// Non-enum: text input mode.
		isSelection := cell != nil && cell.Type == "input_selection"
		switch msg.String() {
		case "enter":
			m.cellEditing = false
			screen.Editing = false
			m.cellInput.Blur()
			val := m.cellInput.Value()
			m.sendPluginInput(screen, uint32(screen.CursorRow), uint32(screen.CursorCol), val)
			return m, nil
		case "esc":
			m.cellEditing = false
			screen.Editing = false
			m.cellInput.Blur()
			return m, nil
		case "tab":
			// Tab-complete for selection cells: extend the input to
			// the longest common prefix of matching tickers from
			// m.ohlcKeys, and surface the top few matches in status.
			if isSelection {
				completed, matches := completeSelection(m.cellInput.Value(), m.ohlcKeys)
				if completed != "" {
					m.cellInput.SetValue(completed)
					m.cellInput.CursorEnd()
					screen.EditValue = completed
				}
				switch {
				case len(matches) == 0:
					m.statusMsg = "no matches"
				case len(matches) == 1:
					m.statusMsg = fmt.Sprintf("1 match: %s", matches[0])
				default:
					head := matches
					if len(head) > 6 {
						head = head[:6]
					}
					m.statusMsg = fmt.Sprintf("%d matches: %s", len(matches), strings.Join(head, " "))
				}
				return m, nil
			}
			// Non-selection: fall through to default input handling.
			fallthrough
		default:
			var cmd tea.Cmd
			m.cellInput, cmd = m.cellInput.Update(msg)
			screen.EditValue = m.cellInput.Value()
			return m, cmd
		}
	}

	// Not editing — navigation mode. Try to land the cursor on the
	// first input cell so subsequent nav keys have somewhere to step.
	// This is best-effort: pure-image plugins (PLOT/ohlc-png) have no
	// input cells, so SelectFirstInput leaves CursorRow at -1. Don't
	// early-return on that case — global keys like o/y (open/yank
	// image) and X (cancel job) still need to fire even when there's
	// nothing to step a cursor to.
	if screen.CursorRow < 0 {
		screen.SelectFirstInput()
	}

	switch msg.String() {
	case "X":
		// Ask the plugin to cancel whatever job it has in flight.
		// job_id "*" is a wildcard the plugin SDK leaves for the
		// plugin to interpret; the backtester treats any cancel as
		// "kill the current bt-engine subprocess", which is what
		// an operator looking at a stuck progress bar wants.
		m.sendPluginJobCancel(screen.Topic, "*")
		return m, nil
	case "up", "k":
		r, c, ok := screen.NextInputCell(-1)
		if ok {
			screen.CursorRow, screen.CursorCol = r, c
		}
		return m, nil
	case "down", "j", "tab":
		r, c, ok := screen.NextInputCell(1)
		if ok {
			screen.CursorRow, screen.CursorCol = r, c
		}
		return m, nil
	case "shift+tab":
		r, c, ok := screen.NextInputCell(-1)
		if ok {
			screen.CursorRow, screen.CursorCol = r, c
		}
		return m, nil
	case "o", "O":
		// Open image cell under cursor via the platform's default
		// viewer (`open` on macOS, `xdg-open` on Linux). Mirrors the
		// agent panel `o`/`O` shortcut so plugin grids that emit
		// image cells (matplotlib PNG, vol surface SVG, …) are
		// reachable without a desktop GUI.
		// Try cell under cursor first, then fall back to the first
		// image cell on the screen — pure-image plugins (PLOT/ohlc-png)
		// have no input cells, so the cursor never lands and the
		// under-cursor lookup is always nil.
		img := screen.CellAt(screen.CursorRow, screen.CursorCol)
		if img == nil || img.Type != "image" {
			img = nil
			for i := range screen.Cells {
				if screen.Cells[i].Type == "image" {
					img = &screen.Cells[i]
					break
				}
			}
		}
		if img == nil {
			m.statusMsg = "no image cell on this screen"
			return m, nil
		}
		path := strings.TrimPrefix(img.Src, "NOTBBG:")
		if path == "" {
			m.statusMsg = "image cell has no src"
			return m, nil
		}
		if err := openArtifact(path); err != nil {
			m.statusMsg = fmt.Sprintf("open %s: %v", path, err)
		} else {
			m.statusMsg = fmt.Sprintf("opened %s", path)
		}
		return m, nil
	case "y", "Y":
		// Yank the image path to clipboard via the platform's
		// pasteboard CLI (pbcopy on macOS, xclip/xsel on Linux,
		// clip on Windows). Lets the operator paste the path
		// elsewhere — Finder, file manager, scp target — without
		// having to read it off the screen letter by letter.
		// Status line also echoes the full path so it's visible
		// even when no clipboard utility is available.
		img := screen.CellAt(screen.CursorRow, screen.CursorCol)
		if img == nil || img.Type != "image" {
			img = nil
			for i := range screen.Cells {
				if screen.Cells[i].Type == "image" {
					img = &screen.Cells[i]
					break
				}
			}
		}
		if img == nil {
			m.statusMsg = "no image cell on this screen"
			return m, nil
		}
		path := strings.TrimPrefix(img.Src, "NOTBBG:")
		if path == "" {
			m.statusMsg = "image cell has no src"
			return m, nil
		}
		if err := copyToClipboard(path); err != nil {
			m.statusMsg = fmt.Sprintf("path: %s (clipboard: %v)", path, err)
		} else {
			m.statusMsg = fmt.Sprintf("copied path: %s", path)
		}
		return m, nil
	case "enter":
		cell := screen.CellAt(screen.CursorRow, screen.CursorCol)
		if cell != nil && views.IsInputCell(cell) {
			if cell.Type == "input_script" {
				// Open full-screen script editor.
				val, _ := cell.Value.(string)
				lang := "aria-strategy"
				if cell.Label != "" && strings.Contains(strings.ToLower(cell.Label), "payoff") {
					lang = "aria-payoff"
				}
				m.scriptEditor = views.NewScriptEditor(cell.Label, lang, val)
				m.scriptScreenID = screen.ID
				m.scriptRow = uint32(screen.CursorRow)
				m.scriptCol = uint32(screen.CursorCol)
				return m, nil
			}
			m.cellEditing = true
			screen.Editing = true
			if cell.Type == "input_enum" {
				screen.EnumIdx = 0
				curVal := fmt.Sprintf("%v", cell.Value)
				for i, opt := range cell.Options {
					if opt.Value == curVal {
						screen.EnumIdx = i
						break
					}
				}
				screen.EditValue = cell.Options[screen.EnumIdx].Label
			} else {
				curVal := fmt.Sprintf("%v", cell.Value)
				m.cellInput.SetValue(curVal)
				m.cellInput.Focus()
				screen.EditValue = curVal
				return m, textinput.Blink
			}
		}
		return m, nil
	}

	if m.cmdMode {
		var cmd tea.Cmd
		m.cmdInput, cmd = m.cmdInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// Ordered timeframes for cycling.
var tfOrder = []string{"1m", "5m", "15m", "1h", "4h", "1d"}

func (m *Model) cycleTF(dir int) {
	if m.ohlcActiveIdx >= len(m.ohlcKeys) {
		return
	}
	key := m.ohlcKeys[m.ohlcActiveIdx]
	d, ok := m.ohlcData[key]
	if !ok || len(d.Timeframes) == 0 {
		return
	}

	// Build sorted list of available TFs in canonical order.
	var avail []string
	for _, tf := range tfOrder {
		if _, ok := d.Timeframes[tf]; ok {
			avail = append(avail, tf)
		}
	}
	// Add any non-standard TFs at the end.
	for tf := range d.Timeframes {
		found := false
		for _, a := range avail {
			if a == tf {
				found = true
				break
			}
		}
		if !found {
			avail = append(avail, tf)
		}
	}

	if len(avail) == 0 {
		return
	}

	// Find current index.
	cur := 0
	for i, tf := range avail {
		if tf == d.ActiveTF {
			cur = i
			break
		}
	}

	next := (cur + dir + len(avail)) % len(avail)
	d.ActiveTF = avail[next]
	m.statusMsg = fmt.Sprintf("Timeframe: %s", d.ActiveTF)
}

// scrollableJKNow reports whether the active surface accepts the
// j/k single-line scroll keys. Narrow by design — j/k is taken
// for nav/cursor on OHLC/LOB/TRADES (instrument step), NEWS (item
// nav), AGENT (output scroll, handled separately), plugin grids
// (cell cursor). Read-only / overflow surfaces get j/k.
func (m Model) scrollableJKNow() bool {
	if m.qrOverlay != "" {
		return true
	}
	switch m.activePanelName() {
	case PanelSettings, PanelMonitor, PanelLog, PanelSanity:
		return true
	}
	return false
}

// scrollableNow reports whether PgUp/PgDn/Ctrl+F/Ctrl+B/g/G
// scrolling is active. Wider than scrollableJKNow — page-step keys
// don't collide with any panel's nav so we let them scroll the
// active surface anywhere there's content overflowing the visible
// window. /PAIR + help overlays + plugin grids + NEWS detail
// articles all benefit from this.
func (m Model) scrollableNow() bool {
	return !m.cmdMode
}

// scrollContentForClamp re-renders the active surface and reports
// the line count, so keystroke handlers can clamp panelScrollOff
// against the real bottom. View is value-receiver and can't write
// the clamp back into m, so we do it here at input time. The
// generous mainHeight (height*4) defeats any internal clip in the
// renderer (LOG auto-tails to the visible window).
func (m Model) scrollContentLines() int {
	var content string
	if m.qrOverlay != "" {
		content = m.qrOverlay
	} else {
		probeHeight := m.height * 4
		if probeHeight < 64 {
			probeHeight = 64
		}
		content = m.renderPanel(probeHeight)
	}
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

// clampPanelScroll caps panelScrollOff so the bottom-most line
// remains visible. Approximates the visible window from m.height —
// the View math (mainHeight = height - 5) is mirrored loosely so we
// don't have to thread mainHeight through every input handler.
func (m *Model) clampPanelScroll() {
	if m.panelScrollOff <= 0 {
		m.panelScrollOff = 0
		return
	}
	visible := m.height - 5
	if visible < 1 {
		visible = 1
	}
	lines := m.scrollContentLines()
	maxOff := lines - visible
	if maxOff < 0 {
		maxOff = 0
	}
	if m.panelScrollOff > maxOff {
		m.panelScrollOff = maxOff
	}
}

// stepActive moves activeIdx by `dir` (±1) over `keys`, skipping
// entries that don't match the active filter. With no filter this
// is a plain wrap-around step. Returns the new index. Empty keys
// list returns 0 (no-op). Looks up instrument/exchange from the
// same data maps as searchInstrument so the matcher is consistent.
func (m *Model) stepActive(keys []string, activeIdx, dir int, filter string) int {
	n := len(keys)
	if n == 0 {
		return 0
	}
	filter = strings.TrimSpace(filter)
	step := func(i int) int { return (i + dir + n) % n }
	// No filter → simple wrap step.
	if filter == "" {
		return step(activeIdx)
	}
	// Filtered → walk up to n-1 steps looking for the next match.
	// Bail out at full circle (no match) and leave the index put.
	for i := 0; i < n; i++ {
		next := step(activeIdx)
		activeIdx = next
		key := keys[next]
		instr, ex := key, ""
		if d, ok := m.ohlcData[key]; ok {
			instr, ex = d.Instrument, d.Exchange
		} else if d, ok := m.lobData[key]; ok {
			instr, ex = d.Instrument, d.Exchange
		} else if d, ok := m.tradeData[key]; ok && d.Agg != nil {
			instr, ex = d.Agg.Instrument, d.Agg.Exchange
		}
		if views.MatchesTokenQuery(filter, key, instr, ex) {
			return next
		}
	}
	return activeIdx
}

// searchInstrument finds the first key matching the multi-token
// query. Tokens are whitespace-separated and each must appear (case
// -insensitive substring) in the key, the instrument, or the
// exchange — so `"BTC bin"` resolves BTCUSDT/binance regardless of
// which panel built `keys`. Resolves instrument/exchange from
// ohlcData ∪ lobData ∪ tradeData so the same helper drives `/`-jump
// on every panel.
func (m *Model) searchInstrument(keys []string, query string) int {
	if strings.TrimSpace(query) == "" {
		return -1
	}
	for i, key := range keys {
		instr, ex := key, ""
		if d, ok := m.ohlcData[key]; ok {
			instr, ex = d.Instrument, d.Exchange
		} else if d, ok := m.lobData[key]; ok {
			instr, ex = d.Instrument, d.Exchange
		} else if d, ok := m.tradeData[key]; ok && d.Agg != nil {
			instr, ex = d.Agg.Instrument, d.Agg.Exchange
		}
		if views.MatchesTokenQuery(query, key, instr, ex) {
			return i
		}
	}
	return -1
}

// SendChan returns a channel for sending commands to the server (set by main).
var SendFrame func(data []byte)

// processCommand routes the `/`-bar input. Order matters:
//
//  1. Multi-word prefix commands (AGENT <text>, ALERT SET <args>) —
//     consumed before anything else so an explicit verb wins.
//  2. Single-word panel names (OHLC/LOB/TRADES/NEWS/...) resolved
//     against m.allPanels() — never hardcoded indices, since the
//     panel order has shifted twice (TRADES insertion + plugin
//     screens) and the old hardcoded routes silently drifted onto
//     the wrong tab.
//  3. PAIR / HELP overlays.
//  4. Per-panel sidebar filter on the four filterable panels
//     (OHLC/LOB/TRADES/NEWS). This is what fixes `/BTC` on LOB
//     hijacking to OHLC — the BTC/ETH shortcut below now only
//     fires when the active panel can't filter.
//  5. Single-token BTC*/ETH* from a non-filterable panel — kept as
//     a convenience jump to OHLC.
//  6. Otherwise: unknown.
func (m *Model) processCommand(input string) {
	cmd := strings.TrimSpace(strings.ToUpper(input))
	isMultiWord := strings.ContainsAny(cmd, " \t")

	// 1. Multi-word prefix commands.
	switch {
	case strings.HasPrefix(cmd, "AGENT "):
		if idx := m.findPanel(PanelAgent); idx >= 0 {
			m.activePanel = idx
			m.panelScrollOff = 0
		}
		m.statusMsg = fmt.Sprintf("Agent: %s (use external runner)", strings.TrimPrefix(cmd, "AGENT "))
		return
	case strings.HasPrefix(cmd, "ALERT SET "):
		m.processAlertSet(cmd[10:])
		return
	}

	// 2. Single-word panel-name commands. Resolves via allPanels()
	//    so panel-order shifts (TRADES, SANITY, plugin screens)
	//    can't desync the routing.
	if !isMultiWord && cmd != "" {
		if idx := m.findPanel(cmd); idx >= 0 {
			m.activePanel = idx
			m.panelScrollOff = 0
			m.statusMsg = fmt.Sprintf("Switched to %s", cmd)
			return
		}
	}

	// 3. Overlay/global commands.
	switch cmd {
	case "PAIR":
		m.qrOverlay = m.generatePhonePairOverlay()
		m.panelScrollOff = 0
		m.statusMsg = "Phone pairing token generated"
		return
	case "HELP":
		m.qrOverlay = helpText
		m.panelScrollOff = 0
		return
	}

	// 4. Per-panel sidebar filter — same `/…<Enter>` UX on every
	//    filterable panel. Empty input clears the filter; non-empty
	//    sets it and jumps active to the first match. Multi-token
	//    queries (`BTC bin`) narrow against instrument + exchange
	//    + key via MatchesTokenQuery.
	switch m.activePanelName() {
	case PanelNews:
		m.newsFilter = strings.TrimSpace(input)
		m.newsSelectedIdx = 0
		m.newsDetail = false
		m.statusMsg = fmt.Sprintf("News filter: %s", m.newsFilter)
		return
	case PanelOHLC:
		m.ohlcSidebarFilter = strings.TrimSpace(input)
		if m.ohlcSidebarFilter == "" {
			m.statusMsg = "OHLC filter cleared"
			return
		}
		if idx := m.searchInstrument(m.ohlcKeys, input); idx >= 0 {
			m.ohlcActiveIdx = idx
			m.statusMsg = fmt.Sprintf("OHLC: %s (filter: %s)", m.ohlcKeys[idx], m.ohlcSidebarFilter)
		} else {
			m.statusMsg = fmt.Sprintf("OHLC filter: %s (no match)", m.ohlcSidebarFilter)
		}
		return
	case PanelLOB:
		m.lobSidebarFilter = strings.TrimSpace(input)
		if m.lobSidebarFilter == "" {
			m.statusMsg = "LOB filter cleared"
			return
		}
		if idx := m.searchInstrument(m.lobKeys, input); idx >= 0 {
			m.lobActiveIdx = idx
			m.statusMsg = fmt.Sprintf("LOB: %s (filter: %s)", m.lobKeys[idx], m.lobSidebarFilter)
		} else {
			m.statusMsg = fmt.Sprintf("LOB filter: %s (no match)", m.lobSidebarFilter)
		}
		return
	case PanelTrades:
		m.tradesSidebarFilter = strings.TrimSpace(input)
		if m.tradesSidebarFilter == "" {
			m.statusMsg = "Trades filter cleared"
			return
		}
		if idx := m.searchInstrument(m.tradeKeys, input); idx >= 0 {
			m.tradeActiveIdx = idx
			m.statusMsg = fmt.Sprintf("Trades: %s (filter: %s)", m.tradeKeys[idx], m.tradesSidebarFilter)
		} else {
			m.statusMsg = fmt.Sprintf("Trades filter: %s (no match)", m.tradesSidebarFilter)
		}
		return
	}

	// 5. Single-token BTC*/ETH* from a non-filterable panel —
	//    convenience jump to OHLC. Multi-word would have been
	//    consumed as a filter on whichever panel is active.
	if !isMultiWord && (strings.HasPrefix(cmd, "BTC") || strings.HasPrefix(cmd, "ETH")) {
		if idx := m.findPanel(PanelOHLC); idx >= 0 {
			m.activePanel = idx
			m.panelScrollOff = 0
		}
		m.statusMsg = fmt.Sprintf("OHLC: %s", cmd)
		return
	}

	m.statusMsg = fmt.Sprintf("Unknown: %s", cmd)
}

// upsertCandle inserts c into sorted by Timestamp ASC, replacing
// any existing entry with the same Timestamp (so a closing bar
// that gets re-published with the final High/Low/Close updates in
// place instead of duplicating). Zero timestamps degrade to plain
// append — the renderer falls back to even-spacing in that case
// rather than mis-anchoring the right edge to a wrong slot.
func upsertCandle(arr []views.Candle, c views.Candle) []views.Candle {
	if c.Timestamp.IsZero() {
		return append(arr, c)
	}
	// Fast paths: empty, append-newest (the realtime-tick hot path).
	if len(arr) == 0 {
		return append(arr, c)
	}
	last := arr[len(arr)-1]
	if !last.Timestamp.IsZero() && c.Timestamp.After(last.Timestamp) {
		return append(arr, c)
	}
	if !last.Timestamp.IsZero() && c.Timestamp.Equal(last.Timestamp) {
		arr[len(arr)-1] = c
		return arr
	}
	// Slow path: backfill chunks arriving out of order. Binary search.
	lo, hi := 0, len(arr)
	for lo < hi {
		mid := (lo + hi) / 2
		mt := arr[mid].Timestamp
		if mt.IsZero() || mt.Before(c.Timestamp) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(arr) && arr[lo].Timestamp.Equal(c.Timestamp) {
		arr[lo] = c
		return arr
	}
	arr = append(arr, views.Candle{}) // grow
	copy(arr[lo+1:], arr[lo:])
	arr[lo] = c
	return arr
}

// findPanel returns the index of the named panel in m.allPanels(),
// or -1 when not present. Resolves case-insensitively against the
// canonical panel constant (PanelOHLC, PanelLOB, ...), the plugin
// screen ID (e.g. "FORM"), AND the plugin Label (e.g.
// "FORMULETTES" — short enough to type, more memorable than the
// 4-char tab abbreviation). The label match means `/FORMULETTES`
// jumps to the formulettes plugin even if its tab ID is "FORM".
func (m *Model) findPanel(name string) int {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if upper == "" {
		return -1
	}
	panels := m.allPanels()
	for i, p := range panels {
		if strings.ToUpper(p) == upper {
			return i
		}
	}
	// Plugin label fallback. Plugin screens are appended after the
	// core panelList in allPanels(), so the index offset is fixed.
	for j, ps := range m.pluginScreens {
		if strings.ToUpper(ps.Label) == upper {
			return len(panelList) + j
		}
	}
	return -1
}

// generatePhonePairOverlay fetches/generates a phone token and
// returns the overlay text — including a half-block QR code the
// phone camera can scan, encoded as the same {"url","token"} JSON
// the desktop PairModal already serves. Falls back to a plain text
// instructions block when the HTTP gateway isn't reachable.
func (m *Model) generatePhonePairOverlay() string {
	// Try to generate a fresh token via the HTTP endpoint.
	desktopToken, _ := os.ReadFile("/tmp/notbbg-desktop.token")
	token := ""
	serverURL := "http://localhost:9474"

	if len(desktopToken) > 0 {
		resp, err := http.Post(
			fmt.Sprintf("%s/api/v1/pair/phone?token=%s", serverURL, string(desktopToken)),
			"application/json", nil,
		)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				body, _ := io.ReadAll(resp.Body)
				var data map[string]string
				if json.Unmarshal(body, &data) == nil {
					token = data["token"]
				}
			}
		}
	}

	// Fall back to reading existing file.
	if token == "" {
		if t, err := os.ReadFile("/tmp/notbbg-phone.token"); err == nil {
			token = strings.TrimSpace(string(t))
		}
	}

	if token == "" {
		return "PHONE PAIRING\n\nFailed to generate token.\nMake sure the HTTP gateway is running on :9474."
	}

	// Build the QR payload. Same shape as server/internal/transport/
	// http.go:handlePairPhone so the phone scanner reads one format
	// regardless of which surface generated it.
	payload, _ := json.Marshal(map[string]string{
		"url":   serverURL,
		"token": token,
	})
	qr := views.RenderQRBlock(string(payload))

	var b strings.Builder
	b.WriteString("PHONE PAIRING\n")
	b.WriteString("═══════════════════════════════════════════════════════\n\n")
	b.WriteString("Scan this QR with the phone app (Settings → Scan QR):\n\n")
	b.WriteString(qr)
	b.WriteString("\nOr paste the token by hand:\n\n  ")
	b.WriteString(token)
	b.WriteString("\n\n═══════════════════════════════════════════════════════\n\n")
	b.WriteString("Steps:\n")
	b.WriteString("  1. Open phone app → Settings tab\n")
	b.WriteString("  2. Tap Scan QR  (or paste server URL + token)\n")
	b.WriteString("  3. Press PAIR\n\n")
	b.WriteString("Token also saved to /tmp/notbbg-phone.token\n")
	b.WriteString("Each PAIR command generates a fresh token.\n\n")
	b.WriteString("Press ESC or Enter to close. j/k to scroll if the QR overflows.")
	return b.String()
}

func (m *Model) processAlertSet(args string) {
	// Parse: "BTCUSDT > 100000" or "BTCUSDT < 50000" or "KEYWORD bitcoin"
	parts := strings.Fields(args)
	if len(parts) < 3 {
		m.statusMsg = "Usage: ALERT SET <instrument> > <price> | ALERT SET KEYWORD <word>"
		return
	}

	var alertType int
	var instrument string
	var threshold float64
	var keyword string

	if parts[0] == "KEYWORD" {
		alertType = 4 // Keyword
		keyword = strings.Join(parts[1:], " ")
	} else {
		instrument = parts[0]
		op := parts[1]
		val := parts[2]
		_, _ = fmt.Sscanf(val, "%f", &threshold)
		switch op {
		case ">":
			alertType = 1 // PriceAbove
		case "<":
			alertType = 2 // PriceBelow
		default:
			m.statusMsg = fmt.Sprintf("Unknown operator: %s (use > or <)", op)
			return
		}
	}

	if SendFrame != nil {
		payload, _ := json.Marshal(map[string]any{
			"type":       alertType,
			"instrument": instrument,
			"threshold":  threshold,
			"keyword":    keyword,
		})
		msg, _ := json.Marshal(map[string]any{
			"type":    "create_alert",
			"payload": json.RawMessage(payload),
		})
		SendFrame(msg)
		m.statusMsg = fmt.Sprintf("Alert created: %s", args)
	} else {
		m.statusMsg = "Not connected — can't create alert"
	}
	m.activePanel = 3 // switch to alerts panel
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	connStatus := "DISCONNECTED"
	connColor := colorRed
	if m.connected {
		connStatus = "CONNECTED"
		connColor = colorGreen
	}

	allPanels := m.allPanels()

	// Scrollable tab bar: fit as many tabs as terminal width allows.
	clockStr := m.clock.Format("15:04:05")
	connStr := lipgloss.NewStyle().Foreground(connColor).Render(connStatus)
	rightPart := fmt.Sprintf("%s  %s ", connStr, clockStr)
	rightW := lipgloss.Width(rightPart)
	availW := m.width - 10 - rightW // " NOTBBG  " prefix + right side

	// Determine visible tab range centered on active panel.
	var tabParts []string
	tabStart := 0
	for {
		// Try rendering from tabStart.
		tabParts = nil
		totalW := 0
		for i := tabStart; i < len(allPanels); i++ {
			p := allPanels[i]
			var rendered string
			if i == m.activePanel {
				rendered = activeTabStyle.Render(p)
			} else {
				rendered = inactiveTabStyle.Render(p)
			}
			w := lipgloss.Width(rendered) + 2 // separator
			if totalW+w > availW && len(tabParts) > 0 {
				break
			}
			tabParts = append(tabParts, rendered)
			totalW += w
		}
		// Ensure active panel is visible.
		if m.activePanel >= tabStart && m.activePanel < tabStart+len(tabParts) {
			break
		}
		tabStart++
		if tabStart >= len(allPanels) {
			break
		}
	}

	tabOverflow := lipgloss.NewStyle().Foreground(colorDim)
	prefix := ""
	if tabStart > 0 {
		prefix = tabOverflow.Render("◀ ")
	}
	suffix := ""
	if tabStart+len(tabParts) < len(allPanels) {
		suffix = tabOverflow.Render(" ▸")
	}
	tabs := prefix + strings.Join(tabParts, "  ") + suffix

	topLeft := fmt.Sprintf(" NOTBBG  %s", tabs)
	topPad := m.width - lipgloss.Width(topLeft) - lipgloss.Width(rightPart)
	if topPad < 1 {
		topPad = 1
	}
	topBar := topBarStyle.Width(m.width).Render(
		topLeft + strings.Repeat(" ", topPad) + rightPart,
	)

	topBarHeight := lipgloss.Height(topBar)
	mainHeight := m.height - topBarHeight - 4 // bottom bar + newlines + safety
	if mainHeight < 1 {
		mainHeight = 1
	}
	var mainContent string
	if m.scriptEditor != nil {
		mainContent = views.RenderScriptEditor(m.scriptEditor, m.width, mainHeight)
	} else if m.qrOverlay != "" {
		mainContent = lipgloss.NewStyle().Foreground(colorAmber).Render(m.qrOverlay)
	} else {
		mainContent = m.renderPanel(mainHeight)
	}

	// Apply panel scroll for SETTINGS / MON / LOG and overlays —
	// hand-rolled because lipgloss has no viewport offset. Skip
	// from the top so j/k step downward through content that
	// otherwise overflowed the panel and got truncated by the
	// outer Height-clipped style. State-clamping happens on
	// keystroke (see clampPanelScroll) so the visual offset and
	// stored offset never drift.
	if m.panelScrollOff > 0 && !m.cmdMode {
		lines := strings.Split(mainContent, "\n")
		off := m.panelScrollOff
		if maxOff := len(lines) - 1; off > maxOff {
			off = maxOff
		}
		if off > 0 {
			mainContent = strings.Join(lines[off:], "\n")
		}
	}
	main := mainAreaStyle.Width(m.width).Height(mainHeight).Render(mainContent)

	var bottomContent string
	if m.cmdMode {
		bottomContent = " > " + m.cmdInput.View()
	} else {
		panelHint := ""
		if m.activePanel < len(allPanels) {
			panelName := allPanels[m.activePanel]
			if h, ok := panelHelp[panelName]; ok && h[1] != "" {
				panelHint = "  " + h[1]
			}
			// Plugin screens get cell-editing hints.
			pluginIdx := m.activePanel - len(panelList)
			// Scroll-position indicator for the read-only j/k panels.
		// Sanity / mon / log all advertise `j/k:scroll` but the user
		// gets no feedback that pressing j actually moved anything if
		// the content doesn't change visibly (e.g. the SANITY card a
		// few lines down looks the same as the previous one), so they
		// concluded "scroll doesn't work" even though the offset was
		// updating. Showing "↕ N/M" right after the panel hint makes
		// the keystroke effect impossible to miss.
		if h, ok := panelHelp[panelName]; ok && h[1] != "" && m.scrollableJKNow() {
			lines := m.scrollContentLines()
			visible := m.height - topBarHeight - 4
			if visible < 1 {
				visible = 1
			}
			maxOff := lines - visible
			if maxOff < 0 {
				maxOff = 0
			}
			if maxOff > 0 {
				panelHint += fmt.Sprintf("  ↕ %d/%d", m.panelScrollOff, maxOff)
			}
		}
		if pluginIdx >= 0 && pluginIdx < len(m.pluginScreens) && m.pluginScreens[pluginIdx].CellGrid {
				// Surface o/y when an image cell is on the screen —
				// pure-image plugins (PLOT/ohlc-png) have no input cells
				// so the cursor never lands and the operator can't see
				// from the hint that there's a way to access the file.
				hasImage := false
				for _, c := range m.pluginScreens[pluginIdx].Cells {
					if c.Type == "image" {
						hasImage = true
						break
					}
				}
				if hasImage {
					panelHint = "  j/k:nav  enter:edit  o:open  y:copy-path  esc:cancel"
				} else {
					panelHint = "  j/k:nav  enter:edit  esc:cancel"
				}
			}
		}
		panelCount := len(allPanels)
		bottomContent = fmt.Sprintf(" TAB:switch  /:cmd  ^1-^%d:panels%s  q:quit  |  %s", panelCount, panelHint, m.statusMsg)
	}
	bottomBar := bottomBarStyle.Width(m.width).Render(bottomContent)

	return topBar + "\n" + main + "\n" + bottomBar
}

// collectSettings builds the row list shown on the SETTINGS tab.
// Reads tuiconfig + the resolved home dir every render — trivially
// cheap, and it picks up changes made via `notbbg` CLI subcommands
// (e.g. `pair-collector`) without a TUI restart.
func collectSettings() []views.SettingRow {
	home := tuiconfig.ResolveHome()
	cfg, _ := tuiconfig.Load("")
	if cfg == nil {
		cfg = &tuiconfig.UserConfig{}
	}
	gui := cfg.GUI.Cache.WithDefaults()

	collectorAddr := cfg.Server.CollectorAddr
	if collectorAddr == "" {
		collectorAddr = "(unset)"
	}
	collectorToken := "(unset)"
	switch {
	case strings.HasPrefix(cfg.Server.CollectorToken, "enc:"):
		collectorToken = "enc:… (decrypt on launch)"
	case cfg.Server.CollectorToken != "":
		collectorToken = "plaintext (set)"
	}
	lastServer := cfg.Server.LastServer
	if lastServer == "" {
		lastServer = "(none)"
	}
	socket := cfg.Server.SocketPath
	if socket == "" {
		socket = "(auto-detect)"
	}
	layout := cfg.Panels.Layout
	if layout == "" {
		layout = "(default)"
	}

	return []views.SettingRow{
		{Section: "PATHS", Key: "home", Value: home},
		{Section: "PATHS", Key: "config", Value: tuiconfig.DefaultPath()},
		{Section: "PATHS", Key: "plugins", Value: tuiconfig.Plugins()},
		{Section: "SERVER", Key: "socket_path", Value: socket},
		{Section: "SERVER", Key: "last_server", Value: lastServer},
		{Section: "SERVER", Key: "auto_start", Value: boolStr(cfg.Server.AutoStart)},
		{Section: "SERVER", Key: "collector_addr", Value: collectorAddr},
		{Section: "SERVER", Key: "collector_token", Value: collectorToken},
		{Section: "PANELS", Key: "active_panel", Value: fmt.Sprintf("%d", cfg.Panels.ActivePanel)},
		{Section: "PANELS", Key: "layout", Value: layout},
		{Section: "GUI CACHE", Key: "ohlc_rows_per_instrument", Value: fmt.Sprintf("%d", gui.OHLCRowsPerInstrument)},
		{Section: "GUI CACHE", Key: "lob_depth_levels", Value: fmt.Sprintf("%d", gui.LOBDepthLevels)},
		{Section: "GUI CACHE", Key: "trades_ring", Value: fmt.Sprintf("%d", gui.TradesRing)},
		{Section: "WATCHLIST", Key: "count", Value: fmt.Sprintf("%d", len(cfg.Watchlist))},
		{Section: "WATCHLIST", Key: "symbols", Value: joinOrNone(cfg.Watchlist)},
		{Section: "ALERTS", Key: "rules_configured", Value: fmt.Sprintf("%d", len(cfg.Alerts))},
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func joinOrNone(xs []string) string {
	if len(xs) == 0 {
		return "(none)"
	}
	return strings.Join(xs, ", ")
}

// completeSelection implements Tab-completion for input_selection
// cells against a universe of tickers (typically m.ohlcKeys).
//
// It returns:
//   - completed: the longest common prefix of candidates matching
//     the current partial value case-insensitively. Empty when
//     there are no matches. When exactly one candidate matches,
//     that candidate is returned verbatim.
//   - matches: all candidates matching the partial, uppercased for
//     display.
//
// Matching is prefix-based (a typed "BTC" matches "BTCUSDT") so
// Tab progressively narrows a long list instead of folding unrelated
// symbols together.
func completeSelection(partial string, universe []string) (string, []string) {
	p := strings.ToUpper(strings.TrimSpace(partial))
	var matches []string
	for _, k := range universe {
		if strings.HasPrefix(strings.ToUpper(k), p) {
			matches = append(matches, strings.ToUpper(k))
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) == 1 {
		return matches[0], matches
	}
	// Longest common prefix.
	lcp := matches[0]
	for _, m := range matches[1:] {
		lcp = commonPrefix(lcp, m)
		if lcp == "" {
			break
		}
	}
	if len(lcp) <= len(p) {
		return "", matches // nothing more to extend
	}
	return lcp, matches
}

func commonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}

// latestAgentArtifact scans agent output lines for the last token
// starting with `NOTBBG:` and returns the path portion (everything
// after the prefix up to whitespace). Returns "" when no tag is
// present. Used by the agent panel's `O` shortcut.
func latestAgentArtifact(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		for _, tok := range strings.Fields(lines[i]) {
			if !strings.HasPrefix(tok, "NOTBBG:") {
				continue
			}
			path := strings.TrimPrefix(tok, "NOTBBG:")
			path = strings.TrimRight(path, ".,;:)")
			if path != "" {
				return path
			}
		}
	}
	return ""
}

// openArtifact hands the path to the platform's default viewer.
// macOS uses `open`, Linux uses `xdg-open`. Returns the exec error
// if the command is missing or fails.
func openArtifact(path string) error {
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	return osExec(cmd, path)
}

// copyToClipboard pipes the given text into the platform's clipboard
// utility — pbcopy on darwin, xclip/xsel on linux, clip on windows.
// Used by the plugin grid `y` shortcut so the operator can yank the
// path of an IMG cell to paste elsewhere (Finder Go-To, scp, …)
// without having to read it off the status line.
func copyToClipboard(text string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "pbcopy"
	case "windows":
		name = "clip"
	default:
		// Prefer xclip; fall back to xsel.
		if _, err := exec.LookPath("xclip"); err == nil {
			name = "xclip"
			args = []string{"-selection", "clipboard"}
		} else {
			name = "xsel"
			args = []string{"--clipboard", "--input"}
		}
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// osExec is a tiny wrapper so tests can shim out process execution.
var osExec = func(cmd string, args ...string) error {
	return exec.Command(cmd, args...).Start()
}

func (m Model) renderPanel(height int) string {
	allPanels := m.allPanels()
	if m.activePanel >= len(allPanels) {
		m.activePanel = 0
	}
	panel := allPanels[m.activePanel]

	switch panel {
	case PanelOHLC:
		// Build sidebar entries for all known instruments, narrowing
		// to entries matching the live filter if set. Matching is
		// token-aware (whitespace splits the query, all tokens must
		// hit at least one of key+instrument+exchange) so operators
		// can type `BTC bin` and reach BTCUSDT/binance directly.
		filter := strings.TrimSpace(m.ohlcSidebarFilter)
		var sidebar []views.OHLCSidebarEntry
		for i, key := range m.ohlcKeys {
			entry := views.OHLCSidebarEntry{
				Label:  key,
				Active: i == m.ohlcActiveIdx,
			}
			if d, ok := m.ohlcData[key]; ok {
				entry.Label = d.Instrument
				entry.Exchange = d.Exchange
				entry.LastUpdate = d.LastUpdate
				// Get price from active timeframe.
				if tfd, ok := d.Timeframes[d.ActiveTF]; ok && len(tfd.Candles) > 0 {
					entry.LastPrice = tfd.Candles[len(tfd.Candles)-1].Close
				}
			}
			if filter != "" && !views.MatchesTokenQuery(filter, key, entry.Label, entry.Exchange) {
				continue
			}
			sidebar = append(sidebar, entry)
		}

		// Get active instrument data.
		var candles []views.Candle
		instrument, exchange, tf := "—", "", ""
		var availableTFs []string
		loadingHint := ""
		if m.ohlcActiveIdx < len(m.ohlcKeys) {
			key := m.ohlcKeys[m.ohlcActiveIdx]
			if d, ok := m.ohlcData[key]; ok {
				instrument = d.Instrument
				exchange = d.Exchange
				tf = d.ActiveTF
				// Get candles for active timeframe.
				if tfd, ok := d.Timeframes[d.ActiveTF]; ok {
					candles = tfd.Candles
					if tfd.Loading {
						// Progressive load — chunk 1 is "now-chunkWindow → now",
						// chunk 2 is one chunkWindow earlier, etc. ProgressIdx/
						// Total tells the trader how far back we've reached.
						loadingHint = fmt.Sprintf(
							"downloading %s history — chunk %d/%d (%d bars stored)",
							d.ActiveTF, tfd.ProgressIdx+1, tfd.ProgressTotal, len(tfd.Candles),
						)
					}
				}
				// Collect available timeframes.
				for k := range d.Timeframes {
					availableTFs = append(availableTFs, k)
				}
			} else {
				instrument = key
			}
		}
		return views.RenderOHLCWithSidebar(candles, m.width, height, instrument, exchange, tf, sidebar, availableTFs, loadingHint)

	case PanelLOB:
		filter := strings.TrimSpace(m.lobSidebarFilter)
		var sidebar []views.LOBSidebarEntry
		for i, key := range m.lobKeys {
			entry := views.LOBSidebarEntry{Label: key, Active: i == m.lobActiveIdx}
			if d, ok := m.lobData[key]; ok {
				entry.Label = d.Instrument
				entry.Exchange = d.Exchange
				entry.LastUpdate = d.LastUpdate
				if len(d.Bids) > 0 && len(d.Asks) > 0 {
					entry.Spread = d.Asks[0].Price - d.Bids[0].Price
				}
			}
			if filter != "" && !views.MatchesTokenQuery(filter, key, entry.Label, entry.Exchange) {
				continue
			}
			sidebar = append(sidebar, entry)
		}
		var activeData *views.LOBData
		if m.lobActiveIdx < len(m.lobKeys) {
			activeData = m.lobData[m.lobKeys[m.lobActiveIdx]]
		}
		return views.RenderLOB(activeData, m.width, height, sidebar)

	case PanelTrades:
		return views.RenderTrades(m.tradeData, m.tradeKeys, m.tradeActiveIdx, m.tradesSidebarFilter, m.width, height)

	case PanelNews:
		return views.RenderNewsWithPreview(m.newsItems, m.newsFilter, m.width, height, m.newsSelectedIdx, m.newsDetail, m.newsPreviewCache, m.panelScrollOff)

	case PanelAlerts:
		return views.RenderAlerts(m.alertItems, m.width, height)

	case PanelSanity:
		return views.RenderSanity(m.sanitySnapshots, m.width, height)

	case PanelMonitor:
		return views.RenderMonitorFull(m.feedStatuses, m.pluginStatuses, m.busStats, m.walStats, m.width, height)

	case PanelLog:
		// Merge locally-launched server logs (sm.Logs via LogFunc)
		// with anything received over `server.log` while paired to a
		// remote server. Remote lines are appended; the renderer
		// caps display height anyway.
		var lines []string
		if LogFunc != nil {
			lines = LogFunc()
		}
		if len(m.remoteLogLines) > 0 {
			lines = append(lines, m.remoteLogLines...)
		}
		return views.RenderLog(lines, m.width, height)

	case PanelAgent:
		var lines []string
		if AgentFunc != nil {
			lines = AgentFunc()
		}
		return views.RenderAgent(lines, m.width, height, m.agentScrollOff)

	case PanelSettings:
		return views.RenderSettings(collectSettings(), m.width, height)

	default:
		// Check if this is a plugin screen.
		for _, ps := range m.pluginScreens {
			if ps.ID == panel {
				screen := ps
				if lines, ok := m.pluginData[ps.Topic]; ok {
					screen.Lines = lines
				}
				return views.RenderPluginScreen(screen, m.width, height)
			}
		}
	}

	return ""
}

// mergeCells applies incremental cell updates to an existing grid.
// Updated cells replace existing cells at the same address; new cells are appended.
func mergeCells(existing, updates []views.PluginCell) []views.PluginCell {
	// Index existing cells by address for O(1) lookup.
	idx := make(map[uint64]int, len(existing))
	for i, c := range existing {
		key := uint64(c.Address.Row)<<32 | uint64(c.Address.Col)
		idx[key] = i
	}
	for _, c := range updates {
		key := uint64(c.Address.Row)<<32 | uint64(c.Address.Col)
		if i, ok := idx[key]; ok {
			existing[i] = c
		} else {
			idx[key] = len(existing)
			existing = append(existing, c)
		}
	}
	return existing
}

// sendPluginJobCancel emits a MsgPluginJobCancel wire frame for
// the plugin matching screenTopic. jobID may be a real id the
// plugin published in a progress cell, or "*" meaning "whatever
// job is running". Returns silently when disconnected.
func (m *Model) sendPluginJobCancel(screenTopic, jobID string) {
	if SendFrame == nil {
		m.statusMsg = "Not connected"
		return
	}
	if jobID == "" {
		jobID = "*"
	}
	msg, _ := json.Marshal(map[string]any{
		"type":   "plugin_job_cancel",
		"topic":  screenTopic,
		"job_id": jobID,
	})
	SendFrame(msg)
	m.statusMsg = fmt.Sprintf("Cancel sent: %s job=%s", screenTopic, jobID)
}

// sendPluginInput sends a cell edit event to the server for routing to the plugin.
func (m *Model) sendPluginInput(screen *views.PluginScreenData, row, col uint32, rawValue string) {
	if SendFrame == nil {
		m.statusMsg = "Not connected"
		return
	}

	// Parse value based on cell type.
	cell := screen.CellAt(int(row), int(col))
	var value any = rawValue
	if cell != nil {
		switch cell.Type {
		case "input_decimal":
			var f float64
			_, _ = fmt.Sscanf(rawValue, "%f", &f)
			value = f
		case "input_integer":
			var n int64
			_, _ = fmt.Sscanf(rawValue, "%d", &n)
			value = n
		}
	}

	evt := map[string]any{
		"screen_id": screen.ID,
		"address":   map[string]any{"row": row, "col": col},
		"value":     value,
	}
	payload, _ := json.Marshal(evt)
	msg, _ := json.Marshal(map[string]any{
		"type":    "plugin_input",
		"topic":   screen.Topic,
		"payload": json.RawMessage(payload),
	})
	SendFrame(msg)
	m.statusMsg = fmt.Sprintf("Sent: %s R%dC%d = %v", screen.ID, row, col, value)
}

// handleScriptEditorKey handles keys when the script editor overlay is open.
func (m Model) handleScriptEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.scriptEditor

	// File picker mode.
	if e.FilePicker {
		switch msg.String() {
		case "up", "k":
			if e.FilePickerIdx > 0 {
				e.FilePickerIdx--
			}
		case "down", "j":
			if e.FilePickerIdx < len(e.FileList)-1 {
				e.FilePickerIdx++
			}
		case "enter":
			if f := e.SelectedFile(); f != "" {
				path := e.ScriptsDir + "/" + f
				data, err := os.ReadFile(path)
				if err == nil {
					e.Lines = strings.Split(string(data), "\n")
					if len(e.Lines) == 0 {
						e.Lines = []string{""}
					}
					e.CursorR = 0
					e.CursorC = 0
					e.ScrollOff = 0
					e.Title = f
					m.statusMsg = fmt.Sprintf("Loaded: %s", f)
				}
			}
			e.CloseFilePicker()
		case "esc":
			e.CloseFilePicker()
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+s":
		// Save and close: send script text to plugin.
		text := e.Text()
		for i := range m.pluginScreens {
			if m.pluginScreens[i].ID == m.scriptScreenID {
				m.sendPluginInput(&m.pluginScreens[i], m.scriptRow, m.scriptCol, text)
				break
			}
		}
		m.scriptEditor = nil
		m.statusMsg = "Script saved"
		return m, nil
	case "ctrl+o":
		// Open file picker — list .aria and .strat files from scripts dir.
		scriptsDir := os.Getenv("HOME") + "/.config/this-is-not-bbg/scripts"
		entries, _ := os.ReadDir(scriptsDir)
		var files []string
		for _, entry := range entries {
			if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".aria") || strings.HasSuffix(entry.Name(), ".strat")) {
				files = append(files, entry.Name())
			}
		}
		e.OpenFilePicker(files, scriptsDir)
		return m, nil
	case "esc":
		m.scriptEditor = nil
		m.statusMsg = "Script editor cancelled"
		return m, nil
	case "up":
		e.Up()
	case "down":
		e.Down()
	case "left":
		e.Left()
	case "right":
		e.Right()
	case "home":
		e.Home()
	case "end":
		e.End()
	case "enter":
		e.Enter()
	case "backspace":
		e.Backspace()
	case "delete":
		e.Delete()
	case "tab":
		e.InsertTab()
	default:
		if msg.Type == tea.KeyRunes {
			for _, r := range msg.Runes {
				e.InsertChar(r)
			}
		}
	}
	return m, nil
}
