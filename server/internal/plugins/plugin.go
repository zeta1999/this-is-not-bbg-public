// Package plugins implements a pipe-based plugin system for third-party data processors.
package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"

	"gopkg.in/yaml.v3"
)

// ScreenDef describes a screen (tab) that a plugin contributes.
type ScreenDef struct {
	ID    string `yaml:"id"    json:"id"`
	Label string `yaml:"label" json:"label"`
	Icon  string `yaml:"icon"  json:"icon"`
}

// ScreenInfo combines ScreenDef with runtime metadata.
type ScreenInfo struct {
	ID     string `json:"id"`
	Plugin string `json:"plugin"`
	Label  string `json:"label"`
	Icon   string `json:"icon"`
	Topic  string `json:"topic"` // "plugin.<name>.screen"
}

// Manifest describes a plugin's configuration.
type Manifest struct {
	Name         string      `yaml:"name"`
	Command      string      `yaml:"command"`
	Args         []string    `yaml:"args"`
	InputTopics  []string    `yaml:"input_topics"`
	OutputTopics []string    `yaml:"output_topics"`
	Screens      []ScreenDef `yaml:"screens"`

	// Liveness thresholds (seconds). If zero, defaults from
	// defaultStaleSec / defaultErrorSec apply. Plugin is marked
	// "stale" after HeartbeatStaleS seconds without stdout
	// activity, "error" after HeartbeatErrorS. Auto-restart on
	// exit is separate (always on, 5 s cooldown).
	HeartbeatStaleS int `yaml:"heartbeat_stale_s"`
	HeartbeatErrorS int `yaml:"heartbeat_error_s"`
}

const (
	defaultStaleSec = 60
	defaultErrorSec = 300
)

// PluginStatus is the shape published to the bus on `plugin.status`,
// mirroring feeds.AdapterStatus for MON-panel consumption.
type PluginStatus struct {
	Name         string    `json:"Name"`
	State        string    `json:"State"` // "connected" | "stale" | "error" | "stopped"
	LastActivity time.Time `json:"LastActivity"`
	PID          int       `json:"PID"`
	ErrorCount   uint64    `json:"ErrorCount"`
	Running      bool      `json:"Running"`
}

// Plugin represents a running or stopped plugin process.
type Plugin struct {
	manifest     Manifest
	dir          string // directory containing the plugin binary and manifest
	dirName      string // base(dir); stable reconcile key (manifest.Name can rename)
	manifestMod  time.Time
	cmd          *exec.Cmd
	bus          *bus.Bus
	cancel       context.CancelFunc
	running      bool
	lastActivity time.Time
	startedAt    time.Time
	silentNoted  bool // true once publishSilent has fired for this start
	errorCount   uint64
	pid          int
	mu           sync.Mutex

	// logFile tails the plugin's stderr to <dir>/notbbg.log so
	// `notbbg plugin logs <name>` has something to read.
	logFile *os.File
}

// PluginLogMaxBytes is the size threshold at which the plugin log
// rotates. One backup (`.log.1`) is retained; older history is
// dropped. Sized to keep the log useful for the last few minutes of
// high-verbosity output without growing unbounded.
const PluginLogMaxBytes int64 = 10 * 1024 * 1024

// LogFileName is the plugin-log file name, relative to the plugin
// directory. Exposed so the CLI (`notbbg plugin logs`) can locate
// the file without re-importing the server package.
const LogFileName = "notbbg.log"

// computePluginState maps (running, now-since-last-activity) pairs to
// a status string. Pure function — unit-tested without os/exec.
func computePluginState(running bool, sinceActivity time.Duration, staleS, errorS int) string {
	if !running {
		return "stopped"
	}
	if staleS <= 0 {
		staleS = defaultStaleSec
	}
	if errorS <= 0 {
		errorS = defaultErrorSec
	}
	sec := int(sinceActivity.Seconds())
	switch {
	case sec >= errorS:
		return "error"
	case sec >= staleS:
		return "stale"
	default:
		return "connected"
	}
}

// Status returns a snapshot of the plugin's current health.
func (p *Plugin) Status() PluginStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	since := time.Since(p.lastActivity)
	if p.lastActivity.IsZero() {
		since = 0 // never active yet, don't falsely flag as stale
	}
	return PluginStatus{
		Name:         p.manifest.Name,
		State:        computePluginState(p.running, since, p.manifest.HeartbeatStaleS, p.manifest.HeartbeatErrorS),
		LastActivity: p.lastActivity,
		PID:          p.pid,
		ErrorCount:   p.errorCount,
		Running:      p.running,
	}
}

// Statuses returns the current status of every loaded plugin.
func (m *Manager) Statuses() []PluginStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PluginStatus, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, p.Status())
	}
	return out
}

// Manager manages plugin lifecycles.
type Manager struct {
	bus     *bus.Bus
	dir     string // plugin directory (e.g. ~/.config/notbbg/plugins/)
	plugins map[string]*Plugin
	mu      sync.RWMutex
}

// NewManager creates a plugin manager.
func NewManager(b *bus.Bus, pluginDir string) *Manager {
	return &Manager{
		bus:     b,
		dir:     pluginDir,
		plugins: make(map[string]*Plugin),
	}
}

// LoadAll discovers and loads plugin manifests from the plugin directory.
func (m *Manager) LoadAll() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No plugins directory.
		}
		return fmt.Errorf("read plugin dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := m.loadOne(entry.Name()); err != nil {
			slog.Debug("skip plugin dir", "name", entry.Name(), "error", err)
		}
	}
	return nil
}

// loadOne reads the manifest for a single plugin dir (relative name
// under m.dir), registers the Plugin in m.plugins keyed by its
// manifest name, and returns the loaded Plugin. Called by LoadAll on
// startup and by Reconcile on hot-reload.
func (m *Manager) loadOne(dirName string) (*Plugin, error) {
	manifestPath := filepath.Join(m.dir, dirName, "manifest.yaml")
	fi, err := os.Stat(manifestPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid plugin manifest %s: %w", dirName, err)
	}
	if manifest.Name == "" {
		manifest.Name = dirName
	}

	p := &Plugin{
		manifest:    manifest,
		dir:         filepath.Join(m.dir, dirName),
		dirName:     dirName,
		manifestMod: fi.ModTime(),
		bus:         m.bus,
	}

	m.mu.Lock()
	m.plugins[manifest.Name] = p
	m.mu.Unlock()

	slog.Info("loaded plugin", "name", manifest.Name, "dir", dirName)
	return p, nil
}

// pluginByDir returns the loaded Plugin whose source dir matches
// dirName, or nil. Caller holds no lock — we take an RLock inside.
func (m *Manager) pluginByDir(dirName string) (string, *Plugin) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, p := range m.plugins {
		if p.dirName == dirName {
			return name, p
		}
	}
	return "", nil
}

// Reconcile compares the on-disk plugin dir to the currently loaded
// set and converges: start newly-added plugins, stop removed ones,
// restart any whose manifest.yaml modtime changed. Emits exactly one
// PublishRegistry call at the end if anything changed.
func (m *Manager) Reconcile() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reconcile: readdir: %w", err)
	}

	// Snapshot what's on disk now.
	onDisk := make(map[string]time.Time)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(m.dir, e.Name(), "manifest.yaml")
		fi, err := os.Stat(manifestPath)
		if err != nil {
			continue
		}
		onDisk[e.Name()] = fi.ModTime()
	}

	// Snapshot what's loaded.
	m.mu.RLock()
	loaded := make(map[string]*Plugin, len(m.plugins))
	for name, p := range m.plugins {
		loaded[name] = p
	}
	m.mu.RUnlock()

	changed := false

	// Removals + modtime-triggered restarts.
	for name, p := range loaded {
		curMod, present := onDisk[p.dirName]
		if !present {
			slog.Info("plugin removed", "name", name, "dir", p.dirName)
			p.Stop()
			m.mu.Lock()
			delete(m.plugins, name)
			m.mu.Unlock()
			changed = true
			continue
		}
		if !curMod.Equal(p.manifestMod) {
			slog.Info("plugin manifest changed, restarting", "name", name)
			p.Stop()
			m.mu.Lock()
			delete(m.plugins, name)
			m.mu.Unlock()
			np, err := m.loadOne(p.dirName)
			if err != nil {
				slog.Warn("reload plugin", "dir", p.dirName, "error", err)
				continue
			}
			if err := np.Start(); err != nil {
				slog.Warn("start after reload", "name", np.manifest.Name, "error", err)
			}
			changed = true
		}
	}

	// Additions.
	for dirName := range onDisk {
		if _, existing := m.pluginByDir(dirName); existing != nil {
			continue
		}
		p, err := m.loadOne(dirName)
		if err != nil {
			slog.Debug("skip new plugin dir", "dir", dirName, "error", err)
			continue
		}
		if err := p.Start(); err != nil {
			slog.Warn("start new plugin", "name", p.manifest.Name, "error", err)
			continue
		}
		slog.Info("new plugin started", "name", p.manifest.Name, "dir", dirName)
		changed = true
	}

	if changed {
		m.PublishRegistry()
	}
	return nil
}

// Start launches a plugin by name.
func (m *Manager) Start(name string) error {
	m.mu.RLock()
	p, ok := m.plugins[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	return p.Start()
}

// Stop stops a plugin by name.
func (m *Manager) Stop(name string) error {
	m.mu.RLock()
	p, ok := m.plugins[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	p.Stop()
	return nil
}

// List returns all loaded plugin names and their running status.
func (m *Manager) List() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]bool)
	for name, p := range m.plugins {
		p.mu.Lock()
		result[name] = p.running
		p.mu.Unlock()
	}
	return result
}

// Screens returns the merged list of screen definitions from all loaded plugins.
func (m *Manager) Screens() []ScreenInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var screens []ScreenInfo
	for _, p := range m.plugins {
		for _, s := range p.manifest.Screens {
			screens = append(screens, ScreenInfo{
				ID:     s.ID,
				Plugin: p.manifest.Name,
				Label:  s.Label,
				Icon:   s.Icon,
				Topic:  "plugin." + p.manifest.Name + ".screen",
			})
		}
	}
	return screens
}

// PublishRegistry sends the current screen registry to the bus so clients
// can discover available plugin tabs.
func (m *Manager) PublishRegistry() {
	screens := m.Screens()
	m.bus.Publish(bus.Message{
		Topic:   "plugin.registry",
		Payload: map[string]any{"screens": screens},
	})
	slog.Debug("published plugin registry", "screens", len(screens))
}

// StartAll starts all loaded plugins and publishes the screen registry.
// Also starts a status publish loop that emits one plugin.status
// message per plugin every statusInterval (default 10 s). Mirrors
// feeds.Manager.StartAll so MON panels see plugins with the same
// freshness / staleness semantics as feeds.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.RLock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		if err := m.Start(name); err != nil {
			slog.Warn("failed to start plugin", "name", name, "error", err)
			// Surface the failure to the GUI as a synthetic cell-grid
			// per declared screen — without this, every spawn failure
			// (missing venv, wrong command, permissions) presents as
			// "Plugin screen X — waiting for data..." forever and the
			// operator has no idea anything is wrong.
			m.publishStartError(name, err)
		}
	}
	m.PublishRegistry()

	// Status publish loop. Runs until ctx is cancelled.
	statusDone := make(chan struct{})
	go func() {
		defer close(statusDone)
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		m.publishStatuses() // one tick at start so MON isn't empty
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.publishStatuses()
			}
		}
	}()

	// Block until context is cancelled.
	<-ctx.Done()
	<-statusDone
	// Stop all plugins.
	for _, name := range names {
		_ = m.Stop(name)
	}
	return nil
}

// Watch polls the plugin dir for add/remove/manifest-change every
// interval and calls Reconcile. Blocks until ctx cancels. Default
// interval is 5s if zero is passed.
func (m *Manager) Watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.Reconcile(); err != nil {
				slog.Warn("plugin reconcile", "error", err)
			}
		}
	}
}

func (m *Manager) publishStatuses() {
	for _, s := range m.Statuses() {
		m.bus.Publish(bus.Message{
			Topic:   "plugin.status",
			Payload: s,
		})
	}
	// Silent-screen check: any plugin that's been running >10s
	// without producing a single output line gets a synthetic
	// "alive but silent" cellgrid. Without this the user sees the
	// TUI's "waiting for data..." stub indefinitely and can't tell
	// process-stuck-but-up from screen-misregistered. Fires once
	// per start cycle (silentNoted gate); if the plugin later
	// emits, normal cells overwrite the synthetic.
	const silenceThreshold = 10 * time.Second
	m.mu.RLock()
	plugins := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		plugins = append(plugins, p)
	}
	m.mu.RUnlock()
	now := time.Now()
	for _, p := range plugins {
		p.mu.Lock()
		running := p.running
		started := p.startedAt
		lastAct := p.lastActivity
		noted := p.silentNoted
		hasScreens := len(p.manifest.Screens) > 0
		p.mu.Unlock()
		if !running || !hasScreens || started.IsZero() {
			continue
		}
		// "Silent" = no stdout activity past the start instant.
		// lastActivity was set to start time; scanner.Scan updates
		// it on every line, so still==startedAt means no output.
		if !lastAct.Equal(started) {
			continue
		}
		if noted {
			continue
		}
		if now.Sub(started) >= silenceThreshold {
			p.publishSilent(now.Sub(started))
			p.mu.Lock()
			p.silentNoted = true
			p.mu.Unlock()
		}
	}
}

// publishStartError emits a synthetic CellGridUpdate on each
// declared screen of the failed plugin so the GUI shows the actual
// reason instead of an indefinite "waiting for data..." stub. The
// payload mirrors the pluginsdk.CellGridUpdate shape so existing
// renderers handle it without change.
func (m *Manager) publishStartError(name string, startErr error) {
	m.mu.RLock()
	p, ok := m.plugins[name]
	m.mu.RUnlock()
	if !ok || p == nil {
		return
	}
	p.publishStartError(startErr)
}

// publishStartError on Plugin is the same surface, callable from
// the auto-restart path which only has *Plugin (no Manager handle).
func (p *Plugin) publishStartError(startErr error) {
	topic := "plugin." + p.manifest.Name + ".screen"
	for _, sc := range p.manifest.Screens {
		cells := []map[string]any{
			{
				"address": map[string]int{"row": 0, "col": 0},
				"type":    "header",
				"text":    sc.Label,
				"style":   map[string]any{"bold": true},
			},
			{
				"address": map[string]int{"row": 1, "col": 0},
				"type":    "text",
				"text":    "Plugin failed to start: " + startErr.Error(),
				"style":   map[string]any{"fg": "red"},
			},
			{
				"address": map[string]int{"row": 2, "col": 0},
				"type":    "text",
				"text":    "See " + filepath.Join(p.dir, LogFileName) + " for stderr.",
				"style":   map[string]any{"fg": "dim"},
			},
		}
		p.bus.Publish(bus.Message{
			Topic: topic,
			Payload: map[string]any{
				"screen_id":    sc.ID,
				"cells":        cells,
				"full_replace": true,
				"version":      "cellgrid/v1",
			},
		})
	}
}

// publishSilent emits a synthetic CellGridUpdate when the plugin
// is alive but has been silent past silenceThreshold. The trader
// sees "process up, no output yet" with the pid + uptime instead
// of an indefinite "waiting for data..." stub. Distinct from
// publishStartError so the renderer can keep them visually apart
// (red = dead, dim = quiet).
func (p *Plugin) publishSilent(silenceFor time.Duration) {
	topic := "plugin." + p.manifest.Name + ".screen"
	pidStr := "n/a"
	if p.pid != 0 {
		pidStr = fmt.Sprintf("%d", p.pid)
	}
	for _, sc := range p.manifest.Screens {
		cells := []map[string]any{
			{
				"address": map[string]int{"row": 0, "col": 0},
				"type":    "header",
				"text":    sc.Label,
				"style":   map[string]any{"bold": true},
			},
			{
				"address": map[string]int{"row": 1, "col": 0},
				"type":    "text",
				"text":    fmt.Sprintf("Plugin alive, no output yet (pid %s, silent %s).", pidStr, silenceFor.Truncate(time.Second)),
				"style":   map[string]any{"fg": "amber"},
			},
			{
				"address": map[string]int{"row": 2, "col": 0},
				"type":    "text",
				"text":    "tail " + filepath.Join(p.dir, LogFileName) + " for stderr.",
				"style":   map[string]any{"fg": "dim"},
			},
		}
		p.bus.Publish(bus.Message{
			Topic: topic,
			Payload: map[string]any{
				"screen_id":    sc.ID,
				"cells":        cells,
				"full_replace": true,
				"version":      "cellgrid/v1",
			},
		})
	}
}

// Start launches the plugin process and begins piping messages.
func (p *Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("plugin %q already running", p.manifest.Name)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	cmd := exec.CommandContext(ctx, p.manifest.Command, p.manifest.Args...)
	cmd.Dir = p.dir // run from plugin directory so ./binary works
	slog.Info("plugin starting", "name", p.manifest.Name, "cmd", p.manifest.Command, "dir", p.dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	// Tee stderr to a per-plugin log file (rotated at PluginLogMaxBytes)
	// so `notbbg plugin logs <name>` has a durable view; the server's
	// own stderr continues to see the live stream.
	logF, err := openPluginLog(p.dir)
	if err != nil {
		slog.Warn("plugin log open failed; stderr only",
			"name", p.manifest.Name, "error", err)
		cmd.Stderr = os.Stderr
	} else {
		p.logFile = logF
		cmd.Stderr = io.MultiWriter(os.Stderr, logF)
	}

	if err := cmd.Start(); err != nil {
		if p.logFile != nil {
			_ = p.logFile.Close()
			p.logFile = nil
		}
		cancel()
		return fmt.Errorf("start plugin %q: %w", p.manifest.Name, err)
	}

	p.cmd = cmd
	p.running = true
	p.pid = cmd.Process.Pid
	p.lastActivity = time.Now() // fresh start counts as liveness
	p.startedAt = time.Now()
	p.silentNoted = false

	slog.Info("plugin started", "name", p.manifest.Name, "pid", cmd.Process.Pid)

	// Pipe bus messages matching input topics to stdin.
	sub := p.bus.Subscribe(256, p.manifest.InputTopics...)
	go func() {
		defer stdin.Close()
		enc := json.NewEncoder(stdin)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-sub.C:
				if !ok {
					return
				}
				if err := enc.Encode(msg); err != nil {
					return
				}
			}
		}
	}()

	// Read stdout and publish to output topics.
	go func() {
		defer p.bus.Unsubscribe(sub)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			raw := scanner.Bytes()
			// Any successful stdout line counts as liveness even if
			// the JSON doesn't parse — the process is producing
			// output, so mark it alive. Parse errors bump the error
			// counter but don't blank last-activity.
			p.mu.Lock()
			p.lastActivity = time.Now()
			p.mu.Unlock()

			var msg bus.Message
			if err := json.Unmarshal(raw, &msg); err != nil {
				p.mu.Lock()
				p.errorCount++
				p.mu.Unlock()
				slog.Warn("plugin output parse error", "name", p.manifest.Name, "error", err)
				continue
			}
			p.bus.Publish(msg)
		}

		// Process exited. Auto-restart with backoff.
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()

		if ctx.Err() == nil { // Not intentionally stopped.
			slog.Warn("plugin exited, restarting", "name", p.manifest.Name)
			time.Sleep(5 * time.Second)
			if err := p.Start(); err != nil {
				// Surface restart failures the same way StartAll
				// does — otherwise an exit-loop hides under repeated
				// "plugin starting" log lines and the GUI shows
				// "waiting for data..." indefinitely.
				p.publishStartError(err)
			}
		}
	}()

	return nil
}

// Stop terminates the plugin process.
func (p *Plugin) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
	}
	if p.logFile != nil {
		_ = p.logFile.Close()
		p.logFile = nil
	}
	p.running = false
	slog.Info("plugin stopped", "name", p.manifest.Name)
}

// openPluginLog opens <dir>/notbbg.log for append, rotating to
// <dir>/notbbg.log.1 if the active file is at or beyond
// PluginLogMaxBytes. Returns a file handle ready for write.
func openPluginLog(dir string) (*os.File, error) {
	path := filepath.Join(dir, LogFileName)
	if fi, err := os.Stat(path); err == nil && fi.Size() >= PluginLogMaxBytes {
		backup := path + ".1"
		_ = os.Remove(backup)
		_ = os.Rename(path, backup)
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
