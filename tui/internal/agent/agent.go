// Package agent provides the LLM agent integration for the TUI.
// Agents connect as standard clients, subscribe to data topics, and push
// suggestions to the agent.suggestion topic per SKILLS.md.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Suggestion represents an agent recommendation.
type Suggestion struct {
	Type       string    `json:"type"`       // "news_highlight", "anomaly", "summary", "correlation_break"
	Timestamp  time.Time `json:"timestamp"`
	Instrument string    `json:"instrument,omitempty"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Urgency    string    `json:"urgency"` // "high", "medium", "low"
	Metadata   any       `json:"metadata,omitempty"`
}

// Runner manages an external agent process.
type Runner struct {
	skillName  string
	command    string
	args       []string
	socketPath string

	mu          sync.RWMutex
	running     bool
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	suggestions []Suggestion
	outputLines []string // raw output lines for terminal display
}

// NewRunner creates a runner for the given skill.
func NewRunner(skillName, command string, args []string, socketPath string) *Runner {
	return &Runner{
		skillName:  skillName,
		command:    command,
		args:       args,
		socketPath: socketPath,
	}
}

// Start launches the agent process.
func (r *Runner) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return fmt.Errorf("agent %s already running", r.skillName)
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	cmd := exec.CommandContext(ctx, r.command, r.args...)
	cmd.Env = append(os.Environ(),
		"NOTBBG_SOCKET="+r.socketPath,
		"NOTBBG_SKILL="+r.skillName,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start agent %s: %w", r.skillName, err)
	}

	r.cmd = cmd
	r.running = true

	slog.Info("agent started", "skill", r.skillName, "pid", cmd.Process.Pid)

	// Capture stderr lines for terminal display.
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			r.appendLine(line)
		}
	}()

	// Read suggestions from stdout (one JSON per line).
	go func() {
		dec := json.NewDecoder(stdout)
		for dec.More() {
			var s Suggestion
			if err := dec.Decode(&s); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Debug("agent output parse error", "skill", r.skillName, "error", err)
				continue
			}

			r.mu.Lock()
			r.suggestions = append(r.suggestions, s)
			if len(r.suggestions) > 100 {
				r.suggestions = r.suggestions[len(r.suggestions)-100:]
			}
			r.mu.Unlock()

			// Also add to output lines for display.
			r.appendLine(fmt.Sprintf("SUGGESTION [%s] %s: %s", s.Urgency, s.Type, s.Title))

			slog.Info("agent suggestion", "skill", r.skillName, "type", s.Type, "title", s.Title)
		}

		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		r.appendLine(fmt.Sprintf("[agent %s exited]", r.skillName))
	}()

	r.appendLine(fmt.Sprintf("[agent %s started, pid %d]", r.skillName, cmd.Process.Pid))
	return nil
}

func (r *Runner) appendLine(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputLines = append(r.outputLines, line)
	if len(r.outputLines) > 500 {
		r.outputLines = r.outputLines[len(r.outputLines)-500:]
	}
}

// OutputLines returns the terminal output lines for display.
func (r *Runner) OutputLines() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.outputLines))
	copy(out, r.outputLines)
	return out
}

// Stop terminates the agent process.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel != nil {
		r.cancel()
	}
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
		_ = r.cmd.Wait()
	}
	r.running = false
	slog.Info("agent stopped", "skill", r.skillName)
}

// Suggestions returns recent suggestions from this agent.
func (r *Runner) Suggestions() []Suggestion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Suggestion, len(r.suggestions))
	copy(out, r.suggestions)
	return out
}

// IsRunning returns whether the agent is currently active.
func (r *Runner) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// Manager manages multiple agent runners.
type Manager struct {
	agents map[string]*Runner
	mu     sync.RWMutex
}

// NewManager creates an agent manager.
func NewManager() *Manager {
	return &Manager{
		agents: make(map[string]*Runner),
	}
}

// Register adds an agent runner.
func (m *Manager) Register(name string, r *Runner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents[name] = r
}

// Start launches an agent by name.
func (m *Manager) Start(name string) error {
	m.mu.RLock()
	r, ok := m.agents[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown agent: %s", name)
	}
	return r.Start()
}

// Stop terminates an agent by name.
func (m *Manager) Stop(name string) {
	m.mu.RLock()
	r, ok := m.agents[name]
	m.mu.RUnlock()
	if ok {
		r.Stop()
	}
}

// AllSuggestions returns recent suggestions from all agents.
func (m *Manager) AllSuggestions() []Suggestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []Suggestion
	for _, r := range m.agents {
		all = append(all, r.Suggestions()...)
	}
	return all
}

// AllOutputLines returns combined terminal output from all agents.
func (m *Manager) AllOutputLines() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []string
	for _, r := range m.agents {
		all = append(all, r.OutputLines()...)
	}
	return all
}

// Names returns registered agent names.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var names []string
	for n := range m.agents {
		names = append(names, n)
	}
	return names
}
