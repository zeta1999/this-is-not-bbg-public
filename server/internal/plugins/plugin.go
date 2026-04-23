// Package plugins implements a pipe-based plugin system for third-party data processors.
package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
}

// Plugin represents a running or stopped plugin process.
type Plugin struct {
	manifest Manifest
	dir      string // directory containing the plugin binary and manifest
	cmd      *exec.Cmd
	bus      *bus.Bus
	cancel   context.CancelFunc
	running  bool
	mu       sync.Mutex
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
		manifestPath := filepath.Join(m.dir, entry.Name(), "manifest.yaml")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			slog.Debug("skip plugin dir", "name", entry.Name(), "error", err)
			continue
		}

		var manifest Manifest
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			slog.Warn("invalid plugin manifest", "name", entry.Name(), "error", err)
			continue
		}

		if manifest.Name == "" {
			manifest.Name = entry.Name()
		}

		m.mu.Lock()
		m.plugins[manifest.Name] = &Plugin{
			manifest: manifest,
			dir:      filepath.Join(m.dir, entry.Name()),
			bus:      m.bus,
		}
		m.mu.Unlock()

		slog.Info("loaded plugin", "name", manifest.Name)
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
		}
	}
	m.PublishRegistry()

	// Block until context is cancelled.
	<-ctx.Done()
	// Stop all plugins.
	for _, name := range names {
		_ = m.Stop(name)
	}
	return nil
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
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start plugin %q: %w", p.manifest.Name, err)
	}

	p.cmd = cmd
	p.running = true

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
			var msg bus.Message
			if err := json.Unmarshal(raw, &msg); err != nil {
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
			_ = p.Start()
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
	p.running = false
	slog.Info("plugin stopped", "name", p.manifest.Name)
}
