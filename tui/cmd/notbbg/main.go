// notbbg is the Bloomberg-style TUI and CLI client.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/notbbg/notbbg/tui/internal/agent"
	"github.com/notbbg/notbbg/tui/internal/app"
	"github.com/notbbg/notbbg/tui/internal/client"
	"github.com/notbbg/notbbg/tui/internal/commands"
	tuiconfig "github.com/notbbg/notbbg/tui/internal/config"

	"golang.org/x/term"
)

func main() {
	var socketPath string

	rootCmd := &cobra.Command{
		Use:   "notbbg",
		Short: "Bloomberg-style market terminal for casual traders",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check if collector is paired — decrypt token and pass to server.
			var collectorAddr, collectorToken string
			if cfg, err := tuiconfig.Load(""); err == nil && cfg != nil && cfg.Server.CollectorAddr != "" {
				collectorAddr = cfg.Server.CollectorAddr
				token := cfg.Server.CollectorToken
				if len(token) > 4 && token[:4] == "enc:" {
					password := os.Getenv("NOTBBG_PASSWORD")
					if password == "" {
						fmt.Fprintf(os.Stderr, "Password for collector %s: ", collectorAddr)
						pwBytes, err := readPassword()
						fmt.Fprintln(os.Stderr)
						if err != nil {
							return fmt.Errorf("read password: %w", err)
						}
						password = string(pwBytes)
					}
					decrypted, err := tuiconfig.DecryptToken(token, password)
					if err != nil {
						return fmt.Errorf("decrypt failed: %w", err)
					}
					token = decrypted
				}
				collectorToken = token
			}
			return runTUI(socketPath, collectorAddr, collectorToken)
		},
	}

	rootCmd.Flags().StringVarP(&socketPath, "socket", "s", "", "server socket path (default: auto-detect)")
	rootCmd.AddCommand(commands.NewExportCmd())
	rootCmd.AddCommand(commands.NewPluginCmd())
	rootCmd.AddCommand(commands.NewNewsCmd())
	rootCmd.AddCommand(commands.NewFeedsCmd())
	rootCmd.AddCommand(commands.NewHistoryCmd())
	rootCmd.AddCommand(commands.NewAgentCmd())
	rootCmd.AddCommand(commands.NewPairCollectorCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var subscribePatterns = []string{
	"ohlc.*.*", "lob.*.*", "trade.*.*",
	"news", "alert", "feed.status",
	"system.health", "indicator.*",
	"plugin.*", "plugin.*.*",
}

func runTUI(socketPath, collectorAddr, collectorToken string) error {
	// Silence TUI's own logging — it would corrupt the alt screen.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	m := app.New()
	dataCh := m.DataChan()
	statusCh := m.StatusChan()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build extra args for server auto-start.
	var serverExtraArgs []string
	if collectorAddr != "" && collectorToken != "" {
		serverExtraArgs = append(serverExtraArgs, "-collector", collectorAddr, "-collector-token", collectorToken)
	}

	sm := client.NewServerManager(socketPath, "notbbg-server", "server/configs/dev.yaml", serverExtraArgs...)

	// Wire log access so the LOG panel can read server logs.
	app.LogFunc = func() []string {
		return sm.Logs.Lines()
	}

	// Embedded terminal for AGENT panel.
	agentTerm := agent.NewTerminal(500)
	_ = agentTerm.Start("/bin/zsh", []string{
		"NOTBBG_SOCKET=/tmp/notbbg.sock",
		"PS1=$ ",
		"TERM=dumb",
	})
	defer agentTerm.Stop()

	app.AgentFunc = func() []string {
		return agentTerm.Lines()
	}
	app.AgentSendFunc = func(line string) {
		if strings.HasPrefix(line, "!") {
			// Shell command — send directly to bash.
			agentTerm.SendLine(strings.TrimPrefix(line, "!"))
		} else {
			// Send to claude -p, redirect stdin from /dev/null to avoid hanging.
			escaped := strings.ReplaceAll(line, "'", "'\\''")
			agentTerm.SendLine("claude -p '" + escaped + "' </dev/null")
		}
	}

	// Connection manager goroutine.
	go connectionLoop(ctx, socketPath, sm, dataCh, statusCh)

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()

	// TUI exited — clean up.
	cancel()
	sm.Kill()

	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

func connectionLoop(ctx context.Context, socketPath string, sm *client.ServerManager, dataCh chan<- []byte, statusCh chan<- string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		sendStatus(statusCh, "Connecting...")

		conn, _, err := client.ConnectWithRetry(ctx, socketPath, sm, func(s string) {
			sendStatus(statusCh, s)
		})
		if err != nil {
			return
		}

		sendStatus(statusCh, "connected")

		app.SendFrame = func(data []byte) {
			_ = conn.WriteFrame(data)
		}

		subMsg, _ := json.Marshal(map[string]any{
			"type":     "subscribe",
			"patterns": subscribePatterns,
		})
		if err := conn.WriteFrame(subMsg); err != nil {
			conn.Close()
			sendStatus(statusCh, "disconnected")
			continue
		}

		const creditInterval = 256
		received := 0
		creditBuf, _ := json.Marshal(map[string]any{"type": "credit", "credits": creditInterval})

		for {
			frame, err := conn.ReadFrame()
			if err != nil {
				break
			}

			// Blocking send with timeout safety valve.
			select {
			case dataCh <- frame:
			case <-time.After(100 * time.Millisecond):
				// TUI poll loop can't keep up — drop and continue.
			}

			received++
			if received >= creditInterval {
				_ = conn.WriteFrame(creditBuf)
				received = 0
			}
		}

		conn.Close()
		app.SendFrame = nil
		sendStatus(statusCh, "disconnected")

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func readPassword() ([]byte, error) {
	// Try golang.org/x/term first, fall back to simple read.
	fd := int(os.Stdin.Fd())
	if oldState, err := term.GetState(fd); err == nil {
		_ = oldState
		return term.ReadPassword(fd)
	}
	// Fallback: read line (not hidden).
	var buf [256]byte
	n, err := os.Stdin.Read(buf[:])
	if err != nil {
		return nil, err
	}
	return buf[:n-1], nil // strip newline
}

func sendStatus(ch chan<- string, s string) {
	select {
	case ch <- s:
	default:
	}
}
