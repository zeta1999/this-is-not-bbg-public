package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ServerManager manages the lifecycle of a child server process.
type ServerManager struct {
	binName    string
	configPath string
	socketPath string
	extraArgs  []string // extra flags passed to the server (e.g. -collector)

	mu   sync.Mutex
	cmd  *exec.Cmd
	done chan struct{} // closed when cmd.Wait() returns
	Logs *LogBuffer
}

// NewServerManager creates a manager that can start/restart the server binary.
func NewServerManager(socketPath, serverBin, configPath string, extraArgs ...string) *ServerManager {
	if serverBin == "" {
		serverBin = "notbbg-server"
	}
	if socketPath == "" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			socketPath = xdg + "/notbbg.sock"
		} else {
			socketPath = "/tmp/notbbg.sock"
		}
	}
	return &ServerManager{
		binName:    serverBin,
		configPath: configPath,
		socketPath: socketPath,
		extraArgs:  extraArgs,
		Logs:       NewLogBuffer(500),
	}
}

// EnsureRunning starts the server if it's not already running.
// Returns true if we own (manage) the server process.
func (sm *ServerManager) EnsureRunning() (bool, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Already running?
	if sm.cmd != nil && sm.done != nil {
		select {
		case <-sm.done:
			// Process exited — fall through to restart.
			sm.cmd = nil
			sm.done = nil
		default:
			// Still alive.
			return true, nil
		}
	}

	// Clean up stale socket before starting a new server.
	os.Remove(sm.socketPath)

	binPath, err := findServerBin(sm.binName)
	if err != nil {
		return false, fmt.Errorf("server binary not found: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return false, fmt.Errorf("generate token: %w", err)
	}

	args := []string{}
	if sm.configPath != "" {
		args = append(args, "-config", sm.configPath)
	}
	args = append(args, sm.extraArgs...)

	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), "NOTBBG_TOKEN="+token)
	logWriter := sm.Logs.Writer()
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("start server: %w", err)
	}

	slog.Info("server started", "pid", cmd.Process.Pid, "path", binPath)
	sm.cmd = cmd
	sm.done = make(chan struct{})

	go func() {
		_ = cmd.Wait()
		close(sm.done)
	}()

	return true, nil
}

// Kill terminates the managed server process and waits for it to exit.
func (sm *ServerManager) Kill() {
	sm.mu.Lock()
	cmd := sm.cmd
	done := sm.done
	sm.mu.Unlock()

	if cmd == nil || cmd.Process == nil || done == nil {
		return
	}

	// Try graceful shutdown first (SIGTERM).
	slog.Info("stopping managed server", "pid", cmd.Process.Pid)
	_ = cmd.Process.Signal(syscall.SIGTERM)

	// Wait up to 3 seconds for graceful exit.
	select {
	case <-done:
		slog.Info("server stopped gracefully")
		sm.mu.Lock()
		sm.cmd = nil
		sm.done = nil
		sm.mu.Unlock()
		return
	case <-time.After(3 * time.Second):
	}

	// Force kill.
	slog.Warn("server didn't stop gracefully, sending SIGKILL")
	_ = cmd.Process.Kill()

	// Wait for process to fully exit (release file locks, etc).
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		slog.Error("server process didn't exit after SIGKILL")
	}

	sm.mu.Lock()
	sm.cmd = nil
	sm.done = nil
	sm.mu.Unlock()
}

// ConnectWithRetry attempts to connect to the server, optionally starting it.
// Retries until context is cancelled.
func ConnectWithRetry(ctx context.Context, socketPath string, sm *ServerManager, onStatus func(string)) (*Client, bool, error) {
	managed := false

	for attempt := 1; ; attempt++ {
		select {
		case <-ctx.Done():
			return nil, managed, ctx.Err()
		default:
		}

		// Try connecting.
		c, err := ConnectUnix(socketPath)
		if err == nil {
			return c, managed, nil
		}

		// No server — start one if we have a manager.
		if sm != nil && !managed {
			if onStatus != nil {
				onStatus("Starting server...")
			}
			ok, startErr := sm.EnsureRunning()
			if startErr != nil {
				slog.Warn("server start failed", "error", startErr)
				if onStatus != nil {
					onStatus(fmt.Sprintf("Server start failed: %s", startErr))
				}
			} else if ok {
				managed = true
			}
		} else if sm != nil && managed {
			// We manage the server and it may have died — restart.
			if onStatus != nil {
				onStatus("Restarting server...")
			}
			_, _ = sm.EnsureRunning()
		}

		// Backoff: 200ms for first 25 attempts (5s), then 1s, cap at 5s.
		delay := 200 * time.Millisecond
		if attempt > 25 {
			delay = time.Duration(min(attempt-24, 5)) * time.Second
		}
		if onStatus != nil && attempt%5 == 0 {
			onStatus(fmt.Sprintf("Connecting... (attempt %d)", attempt))
		}

		select {
		case <-ctx.Done():
			return nil, managed, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func findServerBin(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	if self, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(self), name)
		if _, err := os.Stat(sibling); err == nil {
			return sibling, nil
		}
	}

	if p, err := filepath.Abs(filepath.Join("bin", name)); err == nil {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("%s not found in PATH, next to binary, or in ./bin/", name)
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
