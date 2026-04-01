// notbbg-collector is a passive data receiver that runs on a remote machine.
// It accepts authenticated connections from a notbbg-server, receives market
// data over TLS+PQC, and writes everything to a local datalake.
//
// The server pushes data TO the collector. The collector does NOT run feeds.
//
// Usage:
//
//	notbbg-collector -config collector.yaml           # start receiver
//	notbbg-collector -pair                            # generate pairing token
//	notbbg-collector -init-secrets -enc-config s.enc  # init encrypted config
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/notbbg/notbbg/server/internal/auth"
	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/config"
	"github.com/notbbg/notbbg/server/internal/datalake"
	"github.com/notbbg/notbbg/server/internal/transport"
)

func main() {
	configPath := flag.String("config", "server/configs/dev.yaml", "path to config file")
	encConfigPath := flag.String("enc-config", "", "path to encrypted config")
	pairMode := flag.Bool("pair", false, "generate a one-time pairing token and exit")
	initSecrets := flag.Bool("init-secrets", false, "create an empty encrypted secrets file and exit")
	flag.Parse()

	// Init secrets mode.
	if *initSecrets {
		path := *encConfigPath
		if path == "" {
			path = "configs/secrets.enc"
		}
		password := config.PromptPassword("Set password for secrets file: ")
		if password == "" {
			fmt.Fprintln(os.Stderr, "Password cannot be empty.")
			os.Exit(1)
		}
		emptyCfg := &config.EncryptedConfig{
			APIKeys:    make(map[string]string),
			APISecrets: make(map[string]string),
		}
		if err := config.SaveEncrypted(path, emptyCfg, []byte(password)); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Created encrypted secrets file: %s\n", path)
		return
	}

	slog.SetDefault(slog.New(config.NewRedactHandler(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	// Encrypted config.
	if *encConfigPath != "" {
		password := config.PromptPassword("Enter password for encrypted config: ")
		if _, err := config.LoadEncrypted(*encConfigPath, []byte(password)); err != nil {
			slog.Error("decrypt config", "error", err)
			os.Exit(1)
		}
	}

	// Auth manager.
	authMgr := auth.NewManager()

	if envToken := os.Getenv("NOTBBG_TOKEN"); envToken != "" {
		authMgr.SeedToken(envToken, auth.RightRead|auth.RightSubscribe|auth.RightAdmin, 5*time.Minute)
		slog.Info("seeded pairing token from environment")
	}
	if secretFile := os.Getenv("NOTBBG_SECRET_FILE"); secretFile != "" {
		secret, err := os.ReadFile(secretFile)
		if err != nil {
			slog.Error("read secret file", "path", secretFile, "error", err)
		} else {
			token := strings.TrimSpace(string(secret))
			if token != "" {
				authMgr.SeedToken(token, auth.RightRead|auth.RightSubscribe|auth.RightAdmin, 24*time.Hour)
				slog.Info("seeded secret from file", "path", secretFile)
			}
		}
	}

	// Restore previously saved session token (survives collector restart).
	sessionFile := "/tmp/notbbg-collector-session.enc"
	encKey := os.Getenv("NOTBBG_TOKEN") // use pairing token as encryption key
	if encKey == "" {
		encKey = "notbbg-default-key"
	}
	if tok, err := auth.LoadSessionFile(sessionFile, encKey); err == nil && tok != "" {
		authMgr.SeedSession(tok, "notbbg-server", auth.RightRead|auth.RightSubscribe)
		slog.Info("restored encrypted session from file")
	}

	// Pair mode: generate a token and print connection info.
	// The same token must be seeded when starting the collector via NOTBBG_TOKEN.
	if *pairMode {
		token, err := authMgr.GeneratePairingToken(auth.RightRead|auth.RightSubscribe, 10*time.Minute)
		if err != nil {
			slog.Error("generate token", "error", err)
			os.Exit(1)
		}
		connStr := map[string]any{
			"host":  "0.0.0.0",
			"port":  9473,
			"token": token,
			"type":  "collector",
		}
		if cfg.Server.TCPAddr != "" {
			connStr["addr"] = cfg.Server.TCPAddr
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(connStr)
		fmt.Fprintf(os.Stderr, "\nStart collector with this token:\n")
		fmt.Fprintf(os.Stderr, "  NOTBBG_TOKEN=%s ./bin/notbbg-collector -config %s\n\n", token, *configPath)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Internal bus for received data → datalake.
	msgBus := bus.New(100)

	g, ctx := errgroup.WithContext(ctx)

	// Datalake writer.
	dlPath := cfg.Datalake.Path
	if dlPath == "" {
		dlPath = "/tmp/notbbg-datalake"
	}
	dlWriter := datalake.New(msgBus, datalake.Config{
		Path: dlPath, Enabled: true,
		Topics:   cfg.Datalake.Topics,
		Rotation: cfg.Datalake.Rotation,
	})
	g.Go(func() error { return dlWriter.Run(ctx) })

	// TCP+TLS listener — accepts server connections.
	addr := cfg.Server.TCPAddr
	if addr == "" {
		addr = ":9473"
	}
	home, _ := os.UserHomeDir()
	certDir := filepath.Join(home, ".config", "notbbg", "certs")

	tlsLn := transport.NewTLSListener(addr, certDir, func(conn *transport.FramedConn) {
		handleServerConnection(ctx, conn, msgBus, authMgr, sessionFile, encKey)
	})
	g.Go(func() error {
		if err := tlsLn.Start(); err != nil {
			return err
		}
		<-ctx.Done()
		return tlsLn.Stop()
	})

	slog.Info("notbbg-collector started (passive receiver)",
		"addr", addr,
		"datalake", dlPath,
	)

	if err := g.Wait(); err != nil {
		slog.Error("collector error", "error", err)
		os.Exit(1)
	}
	slog.Info("notbbg-collector stopped")
}

// handleServerConnection receives data from a notbbg-server.
// Requires PQC handshake + token authentication before accepting data.
func handleServerConnection(ctx context.Context, conn *transport.FramedConn, msgBus *bus.Bus, authMgr *auth.Manager, sessionFile, encKey string) {
	slog.Info("server connecting", "remote", conn.RemoteAddr())
	defer slog.Info("server disconnected", "remote", conn.RemoteAddr())

	// PQC handshake.
	pqcKey, err := transport.PQCHandshakeServer(conn)
	if err != nil {
		slog.Warn("pqc handshake failed", "remote", conn.RemoteAddr(), "error", err)
		return
	}
	_ = pqcKey
	slog.Info("pqc session established", "remote", conn.RemoteAddr(), "kem", "ML-KEM-768")

	// Read first message — must be a pair request with valid token.
	frame, err := conn.ReadFrame()
	if err != nil {
		slog.Warn("no pair request", "error", err)
		return
	}
	msg, err := transport.DecodeWireMsg(frame)
	if err != nil || msg.Type != transport.MsgPair {
		slog.Warn("expected pair message", "got", msg.Type)
		resp := &transport.WireMsg{Type: transport.MsgPairFail, Error: "pair required"}
		data, _ := resp.Encode()
		conn.WriteFrame(data)
		return
	}

	// Validate: either pairing token (first time) or session ID (reconnect).
	var sessionID string
	if msg.SessionID != "" {
		// Session reconnect — validate existing session.
		if _, err := authMgr.Validate(msg.SessionID); err != nil {
			slog.Warn("session reconnect rejected", "error", err, "remote", conn.RemoteAddr())
			resp := &transport.WireMsg{Type: transport.MsgPairFail, Error: err.Error()}
			data, _ := resp.Encode()
			conn.WriteFrame(data)
			return
		}
		sessionID = msg.SessionID
		slog.Info("server reconnected with session", "remote", conn.RemoteAddr(), "session", sessionID[:8])
	} else {
		// First-time pairing with one-time token.
		var pairErr error
		sessionID, _, pairErr = authMgr.Pair(msg.Token, msg.ClientName)
		if pairErr != nil {
			slog.Warn("pairing rejected", "error", pairErr, "remote", conn.RemoteAddr())
			resp := &transport.WireMsg{Type: transport.MsgPairFail, Error: pairErr.Error()}
			data, _ := resp.Encode()
			conn.WriteFrame(data)
			return
		}
		slog.Info("server paired successfully", "remote", conn.RemoteAddr(), "session", sessionID[:8])

		// Persist session token encrypted so it survives collector restart.
		if err := auth.SaveSessionFile(sessionFile, sessionID, encKey); err != nil {
			slog.Warn("failed to save session file", "error", err)
		} else {
			slog.Info("session token saved to file (encrypted)")
		}
	}

	resp := &transport.WireMsg{Type: transport.MsgPairOK, SessionID: sessionID}
	data, _ := resp.Encode()
	conn.WriteFrame(data)

	// Receive data loop — server pushes updates.
	received := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		frame, err := conn.ReadFrame()
		if err != nil {
			slog.Debug("server read error", "error", err)
			return
		}

		var wireMsg transport.WireMsg
		if json.Unmarshal(frame, &wireMsg) != nil || wireMsg.Type != transport.MsgUpdate {
			continue
		}

		// Republish to internal bus → datalake writer picks it up.
		var payload any
		json.Unmarshal(wireMsg.Payload, &payload)
		msgBus.Publish(bus.Message{
			Topic:   wireMsg.Topic,
			Payload: payload,
		})
		received++

		if received%1000 == 0 {
			slog.Info("collector received", "messages", received)
		}
	}
}
