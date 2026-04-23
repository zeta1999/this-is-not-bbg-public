// Package app implements the bubbletea application model for the Bloomberg-style TUI.
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	PanelMonitor = "MON"
	PanelLog     = "LOG"
	PanelAgent   = "AGENT"
)

var panelList = []string{PanelOHLC, PanelLOB, PanelTrades, PanelNews, PanelAlerts, PanelMonitor, PanelLog, PanelAgent}

// panelHelp maps panel names to short description + key hints.
var panelHelp = map[string][2]string{
	PanelOHLC:    {"Candlestick charts with multi-timeframe OHLCV data", "[/]:pair  -/+:timeframe  H:history  h:help"},
	PanelLOB:     {"Live limit order book depth (bids/asks)", "[/]:pair  h:help"},
	PanelTrades:  {"Aggregated trade stats: VWAP, volume, quantiles", "[/]:pair  h:help"},
	PanelNews:    {"Crypto news feed from RSS/GDELT sources", "j/k:nav  enter:read  /:search"},
	PanelAlerts:  {"Price and volume alerts", ""},
	PanelMonitor: {"Data feed health and connection status", ""},
	PanelLog:     {"Server log output", ""},
	PanelAgent:   {"AI agent terminal for analysis queries", "j/k:scroll  G:bottom  /:type  enter:send"},
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
    Ctrl+1-9            Jump to panel by number
    / or :              Enter command mode
    h or ?              Show this help
    ESC                 Close overlay / cancel command
    q                   Quit

  CORE PANELS
    OHLC       Candlestick charts, multi-timeframe        [/]:pair  -/+:tf
    LOB        Limit order book depth (bids/asks)         [/]:pair
    TRADES     Aggregated trade stats, VWAP, quantiles    [/]:pair
    NEWS       Crypto news feed                           j/k:nav  enter:read  /:search
    ALERTS     Price and volume alerts
    MON        Feed health and connection status
    LOG        Server log output
    AGENT      AI agent terminal                          j/k:scroll  enter:send

  PLUGIN PANELS (loaded from ~/.config/notbbg/plugins/)
    Navigate input cells: j/k or arrows   Edit: Enter   Cancel: Esc
    Enum inputs: up/down to cycle         Script: Enter opens editor (Ctrl+S saves)

  OHLC & LOB & TRADES
    [ / ]  or  ← / →   Previous / next instrument
    - / +               Previous / next timeframe (OHLC only)
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
	ohlcData      map[string]*instrumentData // key = "INSTRUMENT/exchange"
	ohlcKeys      []string                   // ordered keys (watchlist first, then discovery order)
	ohlcActiveIdx int                        // index into ohlcKeys

	// LOB per-instrument state.
	lobData      map[string]*views.LOBData // key = "INSTRUMENT/exchange"
	lobKeys      []string
	lobActiveIdx int

	// News state.
	newsItems      []views.NewsItem
	newsSelectedIdx int
	newsDetail     bool   // viewing article detail
	newsFilter     string // active filter text
	alertItems   []views.AlertEntry
	feedStatuses []views.FeedStatusEntry

	// Agent output.
	agentScrollOff int // 0 = bottom (auto-scroll), >0 = lines from bottom

	// Trade aggregates.
	tradeData      map[string]*views.TradeViewData // key: exchange/instrument
	tradeKeys      []string
	tradeActiveIdx int

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
		ohlcData:    ohlcData,
		ohlcKeys:    ohlcKeys,
		lobData:     make(map[string]*views.LOBData),
		tradeData:   make(map[string]*views.TradeViewData),
		pluginData:  make(map[string][]views.PluginStyledLine),
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
		candle := views.Candle{Open: p.Open, High: p.High, Low: p.Low, Close: p.Close, Volume: p.Volume}
		tfd.Candles = append(tfd.Candles, candle)
		cap := m.guiCache.OHLCRowsPerInstrument
		if cap <= 0 {
			cap = tuiconfig.DefaultOHLCRowsPerInstrument
		}
		if len(tfd.Candles) > cap {
			tfd.Candles = tfd.Candles[len(tfd.Candles)-cap:]
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
			m.newsItems = append([]views.NewsItem{item}, m.newsItems...)
			if len(m.newsItems) > 200 {
				m.newsItems = m.newsItems[:200]
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
		return m, nil
	case "shift+tab":
		m.activePanel = (m.activePanel - 1 + len(m.allPanels())) % len(m.allPanels())
		return m, nil
	case "/", ":":
		if !m.cmdMode {
			m.cmdMode = true
			m.cmdInput.Focus()
			// On AGENT panel, set placeholder to shell prompt.
			if m.activePanel == 6 {
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
			return m, nil
		}
	case "enter":
		if m.cmdMode {
			input := m.cmdInput.Value()
			// On AGENT panel, send input and stay in command mode for more input.
			if m.activePanel == 6 && AgentSendFunc != nil {
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
		case "h", "?":
			m.qrOverlay = helpText
			return m, nil
		}

		// ctrl+1 through ctrl+9 jump to panels (number keys reserved for cell editing).
		if strings.HasPrefix(msg.String(), "ctrl+") && len(msg.String()) == 6 {
			ch := msg.String()[5]
			if ch >= '1' && ch <= '9' {
				idx := int(ch - '1')
				if idx < len(m.allPanels()) {
					m.activePanel = idx
				}
				return m, nil
			}
		}

		// Agent terminal scrolling.
		if m.activePanel == 6 && !m.cmdMode {
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
			}
		}

		// News navigation.
		if m.activePanel == 2 {
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

		// Instrument navigation on OHLC and LOB panels.
		switch msg.String() {
		case "[", "left":
			switch m.activePanel {
			case 0:
				if len(m.ohlcKeys) > 0 {
					m.ohlcActiveIdx = (m.ohlcActiveIdx - 1 + len(m.ohlcKeys)) % len(m.ohlcKeys)
				}
			case 1:
				if len(m.lobKeys) > 0 {
					m.lobActiveIdx = (m.lobActiveIdx - 1 + len(m.lobKeys)) % len(m.lobKeys)
				}
			case 2:
				if len(m.tradeKeys) > 0 {
					m.tradeActiveIdx = (m.tradeActiveIdx - 1 + len(m.tradeKeys)) % len(m.tradeKeys)
				}
			}
			return m, nil
		case "]", "right":
			switch m.activePanel {
			case 0:
				if len(m.ohlcKeys) > 0 {
					m.ohlcActiveIdx = (m.ohlcActiveIdx + 1) % len(m.ohlcKeys)
				}
			case 1:
				if len(m.lobKeys) > 0 {
					m.lobActiveIdx = (m.lobActiveIdx + 1) % len(m.lobKeys)
				}
			case 2:
				if len(m.tradeKeys) > 0 {
					m.tradeActiveIdx = (m.tradeActiveIdx + 1) % len(m.tradeKeys)
				}
			}
			return m, nil
		case "-", "{":
			if m.activePanel == 0 {
				m.cycleTF(-1)
			}
			return m, nil
		case "=", "+", "}":
			if m.activePanel == 0 {
				m.cycleTF(1)
			}
			return m, nil
		case "H":
			// Progressive history load over /api/v1/datarange (async).
			// The UI stays fully responsive while chunks stream in.
			if m.activePanel == 0 {
				m.startHistoryLoad(24 * time.Hour)
				m.statusMsg = "Loading history (24h)…"
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
		default:
			var cmd tea.Cmd
			m.cellInput, cmd = m.cellInput.Update(msg)
			screen.EditValue = m.cellInput.Value()
			return m, cmd
		}
	}

	// Not editing — navigation mode.
	if screen.CursorRow < 0 {
		screen.SelectFirstInput()
		return m, nil
	}

	switch msg.String() {
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

// searchInstrument finds the first key in the list that matches the query (case-insensitive substring).
func (m *Model) searchInstrument(keys []string, query string) int {
	q := strings.ToUpper(strings.TrimSpace(query))
	if q == "" {
		return -1
	}
	for i, key := range keys {
		upper := strings.ToUpper(key)
		// Also check the instrument data for richer matching.
		if strings.Contains(upper, q) {
			return i
		}
		// Check instrument name in ohlcData (key might be "BTCUSDT/binance").
		if d, ok := m.ohlcData[key]; ok {
			if strings.Contains(strings.ToUpper(d.Instrument), q) || strings.Contains(strings.ToUpper(d.Exchange), q) {
				return i
			}
		}
	}
	return -1
}

// SendChan returns a channel for sending commands to the server (set by main).
var SendFrame func(data []byte)

func (m *Model) processCommand(input string) {
	cmd := strings.TrimSpace(strings.ToUpper(input))
	switch {
	case cmd == "OHLC" || strings.HasPrefix(cmd, "BTC") || strings.HasPrefix(cmd, "ETH"):
		m.activePanel = 0
		m.statusMsg = fmt.Sprintf("OHLC: %s", cmd)
	case cmd == "LOB":
		m.activePanel = 1
	case cmd == "NEWS":
		m.activePanel = 2
	case cmd == "ALERTS":
		m.activePanel = 3
	case cmd == "MON":
		m.activePanel = 4
	case cmd == "LOG":
		m.activePanel = 5
	case cmd == "AGENT":
		m.activePanel = 6
	case strings.HasPrefix(cmd, "AGENT "):
		m.activePanel = 6
		m.statusMsg = fmt.Sprintf("Agent: %s (use external runner)", strings.TrimPrefix(cmd, "AGENT "))

	case strings.HasPrefix(cmd, "ALERT SET "):
		m.processAlertSet(cmd[10:])

	case cmd == "PAIR":
		m.qrOverlay = m.generatePhonePairOverlay()
		m.statusMsg = "Phone pairing token generated"

	case cmd == "HELP":
		m.qrOverlay = helpText

	default:
		// If on NEWS panel, treat unknown commands as search filter.
		if m.activePanel == 2 {
			m.newsFilter = strings.TrimSpace(input)
			m.newsSelectedIdx = 0
			m.newsDetail = false
			m.statusMsg = fmt.Sprintf("News filter: %s", m.newsFilter)
			return
		}
		// If on OHLC panel, search instruments.
		if m.activePanel == 0 {
			if idx := m.searchInstrument(m.ohlcKeys, input); idx >= 0 {
				m.ohlcActiveIdx = idx
				m.statusMsg = fmt.Sprintf("OHLC: %s", m.ohlcKeys[idx])
				return
			}
		}
		// If on LOB panel, search instruments.
		if m.activePanel == 1 {
			if idx := m.searchInstrument(m.lobKeys, input); idx >= 0 {
				m.lobActiveIdx = idx
				m.statusMsg = fmt.Sprintf("LOB: %s", m.lobKeys[idx])
				return
			}
		}
		m.statusMsg = fmt.Sprintf("Unknown: %s", cmd)
	}
}

// generatePhonePairOverlay fetches/generates a phone token and returns overlay text.
func (m *Model) generatePhonePairOverlay() string {
	// Try to generate a fresh token via the HTTP endpoint.
	desktopToken, _ := os.ReadFile("/tmp/notbbg-desktop.token")
	token := ""

	if len(desktopToken) > 0 {
		resp, err := http.Post(
			fmt.Sprintf("http://localhost:9474/api/v1/pair/phone?token=%s", string(desktopToken)),
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

	return fmt.Sprintf(
		"PHONE PAIRING\n"+
			"═══════════════════════════════════════════════════════\n\n"+
			"Phone session token (paste in phone Settings → Token):\n\n"+
			"  %s\n\n"+
			"═══════════════════════════════════════════════════════\n\n"+
			"Steps:\n"+
			"  1. Open phone app → Settings tab\n"+
			"  2. Set server URL (use your machine's IP for LAN)\n"+
			"  3. Paste the token above\n"+
			"  4. Press PAIR\n\n"+
			"Token is also saved to: /tmp/notbbg-phone.token\n"+
			"Each PAIR command generates a fresh token.\n\n"+
			"Press ESC or Enter to close.",
		token,
	)
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
			if pluginIdx >= 0 && pluginIdx < len(m.pluginScreens) && m.pluginScreens[pluginIdx].CellGrid {
				panelHint = "  j/k:nav  enter:edit  esc:cancel"
			}
		}
		panelCount := len(allPanels)
		bottomContent = fmt.Sprintf(" TAB:switch  /:cmd  ^1-^%d:panels%s  q:quit  |  %s", panelCount, panelHint, m.statusMsg)
	}
	bottomBar := bottomBarStyle.Width(m.width).Render(bottomContent)

	return topBar + "\n" + main + "\n" + bottomBar
}

func (m Model) renderPanel(height int) string {
	allPanels := m.allPanels()
	if m.activePanel >= len(allPanels) {
		m.activePanel = 0
	}
	panel := allPanels[m.activePanel]

	switch panel {
	case PanelOHLC:
		// Build sidebar entries for all known instruments.
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
						loadingHint = fmt.Sprintf("loading (%d)", tfd.LoadSeq)
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
			sidebar = append(sidebar, entry)
		}
		var activeData *views.LOBData
		if m.lobActiveIdx < len(m.lobKeys) {
			activeData = m.lobData[m.lobKeys[m.lobActiveIdx]]
		}
		return views.RenderLOB(activeData, m.width, height, sidebar)

	case PanelTrades:
		return views.RenderTrades(m.tradeData, m.tradeKeys, m.tradeActiveIdx, m.width, height)

	case PanelNews:
		return views.RenderNews(m.newsItems, m.newsFilter, m.width, height, m.newsSelectedIdx, m.newsDetail)

	case PanelAlerts:
		return views.RenderAlerts(m.alertItems, m.width, height)

	case PanelMonitor:
		return views.RenderMonitor(m.feedStatuses, m.width, height)

	case PanelLog:
		var lines []string
		if LogFunc != nil {
			lines = LogFunc()
		}
		return views.RenderLog(lines, m.width, height)

	case PanelAgent:
		var lines []string
		if AgentFunc != nil {
			lines = AgentFunc()
		}
		return views.RenderAgent(lines, m.width, height, m.agentScrollOff)

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
