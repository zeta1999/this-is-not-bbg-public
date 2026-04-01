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

// Manifest describes a plugin's configuration.
type Manifest struct {
	Name         string   `yaml:"name"`
	Command      string   `yaml:"command"`
	Args         []string `yaml:"args"`
	InputTopics  []string `yaml:"input_topics"`
	OutputTopics []string `yaml:"output_topics"`
}

// Plugin represents a running or stopped plugin process.
type Plugin struct {
	manifest Manifest
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
			var msg bus.Message
			if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
				slog.Debug("plugin output parse error", "name", p.manifest.Name, "error", err)
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
			p.Start()
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
		p.cmd.Process.Kill()
		p.cmd.Wait()
	}
	p.running = false
	slog.Info("plugin stopped", "name", p.manifest.Name)
}
