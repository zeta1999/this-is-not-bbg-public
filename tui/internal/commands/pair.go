package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	tuiconfig "github.com/notbbg/notbbg/tui/internal/config"
)

// NewPairCollectorCmd creates the pair-collector command.
func NewPairCollectorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pair-collector <host:port> <token>",
		Short: "Pair with a remote collector and save connection",
		Long: `Save a remote collector address and pairing token to your config.
The token is encrypted with a password (Argon2id + AES-256-GCM).

When you run 'notbbg', the TUI auto-starts the server with the
collector push flags, so data is backed up automatically.

Setup flow:
  1. Remote:  notbbg-collector -pair                → get token
  2. Remote:  NOTBBG_TOKEN=<token> notbbg-collector → start collector
  3. Local:   notbbg pair-collector ajax:9473 <token>
  4. Local:   notbbg                                → auto-starts server + collector push

To forget:  notbbg pair-collector --forget`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			forget, _ := cmd.Flags().GetBool("forget")
			if forget {
				return forgetCollector()
			}
			if len(args) < 2 {
				return fmt.Errorf("usage: notbbg pair-collector <host:port> <token>")
			}
			return saveCollector(args[0], args[1])
		},
	}
	cmd.Flags().Bool("forget", false, "remove saved collector connection")
	return cmd
}

func saveCollector(addr, token string) error {
	// Get password from env or prompt.
	password := os.Getenv("NOTBBG_PASSWORD")
	if password == "" {
		fmt.Fprint(os.Stderr, "Set a password to encrypt the collector token: ")
		pwBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		password = string(pwBytes)
	}
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}

	encToken, err := tuiconfig.EncryptToken(token, password)
	if err != nil {
		return fmt.Errorf("encrypt token: %w", err)
	}

	cfg, _ := tuiconfig.Load("")
	if cfg == nil {
		cfg = &tuiconfig.UserConfig{}
	}
	cfg.Server.CollectorAddr = addr
	cfg.Server.CollectorToken = encToken
	if err := tuiconfig.Save("", cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Collector paired: %s (token encrypted)\n", addr)
	fmt.Fprintln(os.Stderr, "Run 'notbbg' — server will auto-push to collector.")
	return nil
}

func forgetCollector() error {
	cfg, _ := tuiconfig.Load("")
	if cfg == nil {
		fmt.Fprintln(os.Stderr, "No config found.")
		return nil
	}
	if cfg.Server.CollectorAddr == "" {
		fmt.Fprintln(os.Stderr, "No collector paired.")
		return nil
	}
	old := cfg.Server.CollectorAddr
	cfg.Server.CollectorAddr = ""
	cfg.Server.CollectorToken = ""
	if err := tuiconfig.Save("", cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Forgot collector %s.\n", old)
	return nil
}
