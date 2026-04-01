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

	tuiconfig "github.com/notbbg/notbbg/tui/internal/config"
	"github.com/notbbg/notbbg/tui/internal/views"
)

// Panel identifiers.
const (
	PanelOHLC    = "OHLC"
	PanelLOB     = "LOB"
	PanelNews    = "NEWS"
	PanelAlerts  = "ALERTS"
	PanelMonitor = "MON"
	PanelLog     = "LOG"
	PanelAgent   = "AGENT"
)

var panelList = []string{PanelOHLC, PanelLOB, PanelNews, PanelAlerts, PanelMonitor, PanelLog, PanelAgent}

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
    1-7                 Jump to panel: 1=OHLC 2=LOB 3=NEWS 4=ALERTS 5=MON 6=LOG 7=AGENT
    / or :              Enter command mode
    h or ?              Show this help
    ESC                 Close overlay / cancel command
    q                   Quit

  OHLC & LOB PANELS
    [ / ]  or  ← / →   Previous / next instrument
    - / +               Previous / next timeframe (OHLC only)

  OHLC & LOB SEARCH
    / then name         Search instruments (e.g. /SOL, /ETH, /yahoo, /nikkei)

  NEWS PANEL
    j / k  or  ↑ / ↓   Navigate headlines
    Enter               Toggle article detail view
    / then keyword      Filter by keyword, ticker, or source (e.g. /BTC, /CoinDesk)
    ESC                 Clear filter or close article

  COMMANDS (type / then command)
    BTC, ETH, SOL...    Jump to OHLC for that instrument
    LOB                 Order book panel
    NEWS                News feed panel
    ALERTS              Alerts panel
    MON                 Feed monitor panel
    LOG                 Server log panel
    ALERT SET <SYM> > <PRICE>    Create price alert (e.g. ALERT SET BTCUSDT > 100000)
    ALERT SET KEYWORD <word>     Create keyword alert
    PAIR                Show QR code for phone pairing
    HELP                Show commands summary

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

type tradePayload struct {
	Instrument string  `json:"Instrument"`
	Exchange   string  `json:"Exchange"`
	Price      float64 `json:"Price"`
	Quantity   float64 `json:"Quantity"`
	Side       string  `json:"Side"`
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

type serverDataMsg struct {
	wire wireMsg
}

type connectedMsg struct{}
type disconnectedMsg struct{}

// ---- Per-instrument OHLC state ----

type timeframeData struct {
	Candles    []views.Candle
	LastUpdate time.Time
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
	agentLines    []string
	agentScrollOff int // 0 = bottom (auto-scroll), >0 = lines from bottom

	// Server connection channels.
	dataCh   chan []byte  // raw frames from server
	statusCh chan string  // connection status updates
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
		if activePanel < 0 || activePanel >= len(panelList) {
			activePanel = 0
		}
	}

	// Seed OHLC instrument list from watchlist config.
	ohlcData := make(map[string]*instrumentData)
	var ohlcKeys []string
	if cfg != nil {
		for _, sym := range cfg.Watchlist {
			ohlcKeys = append(ohlcKeys, sym)
		}
	}

	return Model{
		cmdInput:    ti,
		activePanel: activePanel,
		clock:       time.Now(),
		statusMsg:   "Connecting...",
		dataCh:      make(chan []byte, 8192),
		statusCh:    make(chan string, 16),
		ohlcData:    ohlcData,
		ohlcKeys:    ohlcKeys,
		lobData:     make(map[string]*views.LOBData),
	}
}

// saveLayout persists the current panel to config.
func (m *Model) saveLayout() {
	cfg, _ := tuiconfig.Load("")
	if cfg == nil {
		cfg = &tuiconfig.UserConfig{}
	}
	cfg.Panels.ActivePanel = m.activePanel
	tuiconfig.Save("", cfg)
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
			m.msgCount += drained
			m.statusMsg = fmt.Sprintf("Live | %d msgs", m.msgCount)
		}
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
		if len(tfd.Candles) > 200 {
			tfd.Candles = tfd.Candles[len(tfd.Candles)-200:]
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
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		if !m.cmdMode {
			m.saveLayout()
			return m, tea.Quit
		}
	case "tab":
		m.activePanel = (m.activePanel + 1) % len(panelList)
		m.qrOverlay = ""
		return m, nil
	case "shift+tab":
		m.activePanel = (m.activePanel - 1 + len(panelList)) % len(panelList)
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

		if k := msg.String(); len(k) == 1 && k[0] >= '1' && k[0] <= '7' {
			m.activePanel = int(k[0]-'1') % len(panelList)
			return m, nil
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
		}
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
		fmt.Sscanf(val, "%f", &threshold)
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

	var tabParts []string
	for i, p := range panelList {
		if i == m.activePanel {
			tabParts = append(tabParts, activeTabStyle.Render(p))
		} else {
			tabParts = append(tabParts, inactiveTabStyle.Render(p))
		}
	}
	tabs := strings.Join(tabParts, "  ")

	clockStr := m.clock.Format("15:04:05")
	connStr := lipgloss.NewStyle().Foreground(connColor).Render(connStatus)

	topLeft := fmt.Sprintf(" NOTBBG  %s", tabs)
	topRight := fmt.Sprintf("%s  %s ", connStr, clockStr)
	topPad := m.width - lipgloss.Width(topLeft) - lipgloss.Width(topRight)
	if topPad < 1 {
		topPad = 1
	}
	topBar := topBarStyle.Width(m.width).Render(
		topLeft + strings.Repeat(" ", topPad) + topRight,
	)

	topBarHeight := lipgloss.Height(topBar)
	mainHeight := m.height - topBarHeight - 4 // bottom bar + newlines + safety
	if mainHeight < 1 {
		mainHeight = 1
	}
	var mainContent string
	if m.qrOverlay != "" {
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
		switch m.activePanel {
		case 0:
			panelHint = "  [/]:pair  -/+:timeframe"
		case 1:
			panelHint = "  [/]:pair"
		case 2:
			panelHint = "  j/k:nav  enter:read  /:search"
			if m.newsFilter != "" {
				panelHint += "  esc:clear"
			}
		case 6:
			panelHint = "  j/k:scroll  G:bottom  /:type  !:shell  enter:send"
		}
		bottomContent = fmt.Sprintf(" TAB:switch  /:cmd  1-7:panels%s  q:quit  |  %s", panelHint, m.statusMsg)
	}
	bottomBar := bottomBarStyle.Width(m.width).Render(bottomContent)

	return topBar + "\n" + main + "\n" + bottomBar
}

func (m Model) renderPanel(height int) string {
	panel := panelList[m.activePanel]

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
		if m.ohlcActiveIdx < len(m.ohlcKeys) {
			key := m.ohlcKeys[m.ohlcActiveIdx]
			if d, ok := m.ohlcData[key]; ok {
				instrument = d.Instrument
				exchange = d.Exchange
				tf = d.ActiveTF
				// Get candles for active timeframe.
				if tfd, ok := d.Timeframes[d.ActiveTF]; ok {
					candles = tfd.Candles
				}
				// Collect available timeframes.
				for k := range d.Timeframes {
					availableTFs = append(availableTFs, k)
				}
			} else {
				instrument = key
			}
		}
		return views.RenderOHLCWithSidebar(candles, m.width, height, instrument, exchange, tf, sidebar, availableTFs)

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
	}

	return ""
}
