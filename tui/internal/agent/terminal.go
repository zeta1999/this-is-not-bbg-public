package agent

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// Terminal is an embedded interactive terminal backed by a real PTY.
// This allows interactive programs (claude, vim, etc.) to work properly.
type Terminal struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	ptmx     *os.File // PTY master
	cancel   context.CancelFunc
	lines    []string
	maxLines int
	running  bool
}

// NewTerminal creates an embedded terminal.
func NewTerminal(maxLines int) *Terminal {
	if maxLines <= 0 {
		maxLines = 500
	}
	return &Terminal{maxLines: maxLines}
}

// Start launches the shell process with a real PTY.
func (t *Terminal) Start(shell string, env []string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return nil
	}

	if shell == "" {
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel

	cmd := exec.CommandContext(ctx, shell)
	cmd.Env = append(os.Environ(), env...)

	// Start with a real PTY so interactive programs work.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		cancel()
		return err
	}

	// Set terminal size.
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120})

	t.cmd = cmd
	t.ptmx = ptmx
	t.running = true
	t.lines = append(t.lines, "$ "+shell)

	// Read PTY output.
	go func() {
		reader := bufio.NewReader(ptmx)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				// Strip trailing newline and carriage returns.
				clean := stripControlChars(line)
				if clean != "" {
					t.appendLine(clean)
				}
			}
			if err != nil {
				break
			}
		}
		t.mu.Lock()
		t.running = false
		t.mu.Unlock()
		t.appendLine("[process exited]")
	}()

	// Wait for exit.
	go func() {
		_ = cmd.Wait()
		ptmx.Close()
	}()

	return nil
}

// SendLine writes a line of input to the PTY.
func (t *Terminal) SendLine(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ptmx == nil || !t.running {
		return
	}
	t.lines = append(t.lines, "$ "+line)
	if len(t.lines) > t.maxLines {
		t.lines = t.lines[len(t.lines)-t.maxLines:]
	}
	_, _ = io.WriteString(t.ptmx, line+"\n")
}

// Lines returns a copy of all output lines.
func (t *Terminal) Lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.lines))
	copy(out, t.lines)
	return out
}

// IsRunning returns whether the shell is still alive.
func (t *Terminal) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

// Stop kills the shell process.
func (t *Terminal) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
	if t.ptmx != nil {
		t.ptmx.Close()
	}
	t.running = false
}

func (t *Terminal) appendLine(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = append(t.lines, line)
	if len(t.lines) > t.maxLines {
		t.lines = t.lines[len(t.lines)-t.maxLines:]
	}
}

// stripControlChars removes ANSI escape sequences and control characters.
func stripControlChars(s string) string {
	var out []byte
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			// Skip ANSI escape sequence.
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
					i++
				}
				if i < len(s) {
					i++ // skip final letter
				}
			}
			continue
		}
		if s[i] == '\r' || s[i] == '\n' {
			i++
			continue
		}
		if s[i] < 0x20 && s[i] != '\t' {
			i++
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}
