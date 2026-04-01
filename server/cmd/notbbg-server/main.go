// notbbg-server is the headless data ingestion and distribution server.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"path/filepath"

	"github.com/notbbg/notbbg/server/internal/alerts"
	cronpkg "github.com/notbbg/notbbg/server/internal/cron"
	"github.com/notbbg/notbbg/server/internal/datalake"
	"github.com/notbbg/notbbg/server/internal/auth"
	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/cache"
	"github.com/notbbg/notbbg/server/internal/config"
	"github.com/notbbg/notbbg/server/internal/feeds"
	"github.com/notbbg/notbbg/server/internal/feeds/ccxt"
	"github.com/notbbg/notbbg/server/internal/feeds/dex"
	"github.com/notbbg/notbbg/server/internal/feeds/rss"
	"github.com/notbbg/notbbg/server/internal/feeds/world"
	"github.com/notbbg/notbbg/server/internal/monitor"
	"github.com/notbbg/notbbg/server/internal/transport"
)

func main() {
	configPath := flag.String("config", "server/configs/dev.yaml", "path to config file")
	encConfigPath := flag.String("enc-config", "", "path to encrypted config (for API keys, secrets)")
	initSecrets := flag.Bool("init-secrets", false, "create an empty encrypted secrets file and exit")
	collectorAddr := flag.String("collector", "", "push data to remote collector (e.g. ajax:9473)")
	collectorToken := flag.String("collector-token", "", "pairing token for remote collector")
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

	// Load encrypted config if specified (API keys, webhook URLs, etc.).
	if *encConfigPath != "" {
		password := config.PromptPassword("Enter password for encrypted config: ")
		encCfg, err := config.LoadEncrypted(*encConfigPath, []byte(password))
		if err != nil {
			slog.Error("decrypt config", "error", err)
			os.Exit(1)
		}
		// Apply encrypted values to config.
		for name, key := range encCfg.APIKeys {
			for i := range cfg.Feeds.Exchanges {
				if cfg.Feeds.Exchanges[i].Name == name {
					slog.Info("loaded API key from encrypted config", "exchange", name)
					_ = key // Keys stored in exchange-specific fields when needed.
				}
			}
		}
		if encCfg.SlackWebhook != "" || encCfg.TelegramToken != "" {
			slog.Info("loaded notification credentials from encrypted config")
		}
		_ = encCfg
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Message bus.
	msgBus := bus.New(500) // Larger ring buffer for snapshot history.

	// Rewire slog to also publish to bus (for desktop/phone LOG tab).
	slog.SetDefault(slog.New(bus.NewBusLogHandler(
		config.NewRedactHandler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		msgBus, "server.log",
	)))

	// Cache — retry up to 10 times (another process may still be releasing the lock).
	var store *cache.Store
	for attempt := 1; attempt <= 10; attempt++ {
		store, err = cache.Open(cfg.Cache.DBPath, cfg.Cache.DefaultTTL)
		if err == nil {
			break
		}
		slog.Warn("open cache failed, retrying", "error", err, "attempt", attempt)
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	if store == nil {
		slog.Error("open cache failed after retries", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	g, ctx := errgroup.WithContext(ctx)

	// Cache eviction loop.
	maxCacheSizeBytes := int64(cfg.Cache.MaxSizeMB) * 1024 * 1024
	g.Go(func() error {
		ticker := time.NewTicker(cfg.Cache.EvictionInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := store.Evict(); err != nil {
					slog.Error("cache eviction", "error", err)
				}
				sizeMB := store.DBSizeBytes() / (1024 * 1024)
				if maxCacheSizeBytes > 0 && store.DBSizeBytes() > maxCacheSizeBytes {
					slog.Warn("cache size exceeds limit, evicting aggressively", "size_mb", sizeMB, "max_mb", cfg.Cache.MaxSizeMB)
					store.Evict()
				}
				slog.Debug("cache size", "size_mb", sizeMB)
			}
		}
	})

	// Auth manager (needed before listeners). Restore persisted tokens.
	authMgr := auth.NewManager()
	authMgr.SetupPersistence(store.DB())
	if envToken := os.Getenv("NOTBBG_TOKEN"); envToken != "" {
		authMgr.SeedToken(envToken, auth.RightRead|auth.RightSubscribe|auth.RightConfigure|auth.RightAdmin, 5*time.Minute)
		slog.Info("seeded pairing token from environment")
	}
	// Generate session tokens for desktop and phone HTTP clients.
	// These are session tokens (not pairing) so they can be reused across reconnects.
	desktopSessionID, _ := auth.GenerateTokenID()
	authMgr.SeedSession(desktopSessionID, "desktop-app", auth.RightRead|auth.RightSubscribe)
	if err := os.WriteFile("/tmp/notbbg-desktop.token", []byte(desktopSessionID), 0600); err != nil {
		slog.Warn("failed to write desktop token", "error", err)
	} else {
		slog.Info("desktop session token written to /tmp/notbbg-desktop.token")
	}

	phoneSessionID, _ := auth.GenerateTokenID()
	authMgr.SeedSession(phoneSessionID, "phone-app", auth.RightRead|auth.RightSubscribe)
	if err := os.WriteFile("/tmp/notbbg-phone.token", []byte(phoneSessionID), 0600); err != nil {
		slog.Warn("failed to write phone token", "error", err)
	} else {
		slog.Info("phone session token written to /tmp/notbbg-phone.token")
	}

	// Support NOTBBG_SECRET_FILE: read shared secret from a file (e.g. tmpfs-mounted).
	if secretFile := os.Getenv("NOTBBG_SECRET_FILE"); secretFile != "" {
		secret, err := os.ReadFile(secretFile)
		if err != nil {
			slog.Error("read secret file", "path", secretFile, "error", err)
		} else {
			token := strings.TrimSpace(string(secret))
			if token != "" {
				authMgr.SeedToken(token, auth.RightRead|auth.RightSubscribe|auth.RightConfigure|auth.RightAdmin, 24*time.Hour)
				slog.Info("seeded secret from file", "path", secretFile)
			}
		}
	}

	// Alert engine (needed before listeners for create_alert handling).
	alertEngine := alerts.NewEngine(msgBus)
	g.Go(func() error {
		return alertEngine.Run(ctx)
	})

	// News search indexer.
	searchIndex := cache.NewSearchIndex()
	newsIndexer := cache.NewIndexer(searchIndex, msgBus)
	g.Go(func() error {
		return newsIndexer.Run(ctx)
	})

	// Unix socket listener.
	unixLn := transport.NewUnixListener(cfg.Server.UnixSocket, func(conn *transport.FramedConn) {
		handleClient(ctx, conn, msgBus, store, authMgr, alertEngine)
	})
	g.Go(func() error {
		if err := unixLn.Start(); err != nil {
			return err
		}
		<-ctx.Done()
		return unixLn.Stop()
	})

	// Feed manager.
	feedMgr := feeds.NewManager(msgBus)
	for _, exCfg := range cfg.Feeds.Exchanges {
		if !exCfg.Enabled {
			continue
		}
		switch exCfg.Name {
		case "binance":
			adapter := ccxt.NewBinanceAdapter(
				msgBus, exCfg.Symbols, exCfg.FeedTypes,
				exCfg.WSEndpoint, exCfg.RESTBase, exCfg.RateLimit,
			)
			feedMgr.Register(adapter)

			// Historical backfill.
			if exCfg.BackfillDays > 0 {
				g.Go(func() error {
					return adapter.BackfillHistory(ctx, exCfg.BackfillDays, exCfg.Timeframes)
				})
			}
		case "coinbase":
			adapter := ccxt.NewCoinbaseAdapter(
				msgBus, exCfg.Symbols, exCfg.FeedTypes,
				exCfg.WSEndpoint, exCfg.RESTBase, exCfg.RateLimit,
			)
			feedMgr.Register(adapter)
		case "kraken":
			adapter := ccxt.NewKrakenAdapter(
				msgBus, exCfg.Symbols, exCfg.FeedTypes,
				exCfg.WSEndpoint, exCfg.RESTBase, exCfg.RateLimit,
			)
			feedMgr.Register(adapter)
		case "okx":
			adapter := ccxt.NewOKXAdapter(msgBus, exCfg.Symbols, exCfg.FeedTypes, exCfg.WSEndpoint)
			feedMgr.Register(adapter)
		case "bybit":
			adapter := ccxt.NewBybitAdapter(msgBus, exCfg.Symbols, exCfg.FeedTypes, exCfg.WSEndpoint)
			feedMgr.Register(adapter)
		case "bitget":
			adapter := ccxt.NewBitgetAdapter(msgBus, exCfg.Symbols, exCfg.FeedTypes, exCfg.WSEndpoint)
			feedMgr.Register(adapter)
		default:
			slog.Warn("unknown exchange adapter, skipping", "name", exCfg.Name)
		}
	}

	// World feed adapters.
	if cfg.Feeds.World.CoinGecko.Enabled {
		feedMgr.Register(world.NewCoinGeckoAdapter(msgBus, cfg.Feeds.World.CoinGecko.PollInterval))
	}
	if cfg.Feeds.World.AlternativeMe.Enabled {
		feedMgr.Register(world.NewFearGreedAdapter(msgBus, cfg.Feeds.World.AlternativeMe.PollInterval))
	}
	if cfg.Feeds.World.MempoolSpace.Enabled {
		feedMgr.Register(world.NewMempoolAdapter(msgBus, cfg.Feeds.World.MempoolSpace.PollInterval))
	}
	if cfg.Feeds.World.YahooFinance.Enabled {
		feedMgr.Register(world.NewYahooFinanceAdapter(msgBus, cfg.Feeds.World.YahooFinance.PollInterval, cfg.Feeds.World.YahooFinance.Symbols))
	}
	if cfg.Feeds.World.FRED.Enabled {
		feedMgr.Register(world.NewFREDAdapter(msgBus, cfg.Feeds.World.FRED.APIKey, cfg.Feeds.World.FRED.PollInterval))
	}

	// RSS feeds.
	if cfg.Feeds.RSS.Enabled {
		feedMgr.Register(rss.NewAdapter(msgBus, cfg.Feeds.RSS.Feeds, cfg.Feeds.RSS.PollInterval))
	}

	// DEX adapters.
	if cfg.Feeds.DEX.Uniswap.Enabled {
		feedMgr.Register(dex.NewUniswapAdapter(msgBus, cfg.Feeds.DEX.Uniswap.PollInterval))
	}
	if cfg.Feeds.DEX.Raydium.Enabled {
		feedMgr.Register(dex.NewRaydiumAdapter(msgBus, cfg.Feeds.DEX.Raydium.PollInterval))
	}
	if cfg.Feeds.DEX.Jupiter.Enabled {
		feedMgr.Register(dex.NewJupiterAdapter(msgBus, cfg.Feeds.DEX.Jupiter.PollInterval))
	}
	if cfg.Feeds.DEX.Hyperliquid.Enabled {
		feedMgr.Register(dex.NewHyperliquidAdapter(msgBus, cfg.Feeds.DEX.Hyperliquid.PollInterval))
	}
	if cfg.Feeds.DEX.GMX.Enabled {
		feedMgr.Register(dex.NewGMXAdapter(msgBus, cfg.Feeds.DEX.GMX.PollInterval))
	}
	if cfg.Feeds.DEX.DYDX.Enabled {
		feedMgr.Register(dex.NewDYDXAdapter(msgBus, cfg.Feeds.DEX.DYDX.PollInterval))
	}
	if cfg.Feeds.DEX.Drift.Enabled {
		feedMgr.Register(dex.NewDriftAdapter(msgBus, cfg.Feeds.DEX.Drift.PollInterval))
	}
	if cfg.Feeds.DEX.Serum.Enabled {
		feedMgr.Register(dex.NewSerumAdapter(msgBus, cfg.Feeds.DEX.Serum.PollInterval))
	}
	if cfg.Feeds.DEX.Orca.Enabled {
		feedMgr.Register(dex.NewOrcaAdapter(msgBus, cfg.Feeds.DEX.Orca.PollInterval))
	}
	if cfg.Feeds.DEX.PancakeSwap.Enabled {
		feedMgr.Register(dex.NewPancakeSwapAdapter(msgBus, cfg.Feeds.DEX.PancakeSwap.PollInterval))
	}
	if cfg.Feeds.DEX.Curve.Enabled {
		feedMgr.Register(dex.NewCurveAdapter(msgBus, cfg.Feeds.DEX.Curve.PollInterval))
	}

	// Start all feeds.
	g.Go(func() error {
		return feedMgr.StartAll(ctx, 10*time.Second)
	})

	// System health monitor.
	healthMon := monitor.NewHealthMonitor(msgBus, 10*time.Second)
	g.Go(func() error {
		return healthMon.Run(ctx)
	})

	// Cross-exchange consistency checker.
	consistencyChecker := monitor.NewConsistencyChecker(
		msgBus, cfg.Alerts.ConsistencyThreshold, cfg.Alerts.CheckInterval,
	)
	g.Go(func() error {
		return consistencyChecker.Run(ctx)
	})

	// Cache writer: persist bus messages to BBolt.
	cacheWriter := cache.NewWriter(store, msgBus)
	g.Go(func() error {
		return cacheWriter.Run(ctx)
	})

	// Datalake writer.
	if cfg.Datalake.Enabled && cfg.Datalake.Path != "" {
		dlWriter := datalake.New(msgBus, datalake.Config{
			Path:     cfg.Datalake.Path,
			Enabled:  true,
			Topics:   cfg.Datalake.Topics,
			Format:   cfg.Datalake.Format,
			Rotation: cfg.Datalake.Rotation,
		})
		g.Go(func() error {
			return dlWriter.Run(ctx)
		})
	}

	// Cron scheduler.
	cronSched := cronpkg.New()
	cronSched.RegisterAction("cache_evict", func(ctx context.Context, job *cronpkg.Job) error {
		return store.Evict()
	})
	cronSched.RegisterAction("log_stats", func(ctx context.Context, job *cronpkg.Job) error {
		slog.Info("cron: cache stats", "size_mb", store.DBSizeBytes()/(1024*1024))
		return nil
	})
	cronSched.RegisterAction("save_tokens", func(ctx context.Context, job *cronpkg.Job) error {
		authMgr.CleanExpired()
		authMgr.SaveTokens(store.DB())
		return nil
	})
	for _, jc := range cfg.Cron {
		cronSched.AddJob(cronpkg.Job{
			Name: jc.Name, Schedule: jc.Schedule,
			Action: jc.Action, Args: jc.Args, Enabled: jc.Enabled,
		})
	}
	g.Go(func() error {
		return cronSched.Run(ctx)
	})

	// TCP+TLS listener.
	if cfg.Server.EnableTCP {
		home, _ := os.UserHomeDir()
		certDir := filepath.Join(home, ".config", "notbbg", "certs")
		tlsLn := transport.NewTLSListener(cfg.Server.TCPAddr, certDir, func(conn *transport.FramedConn) {
			handleClient(ctx, conn, msgBus, store, authMgr, alertEngine)
		})
		g.Go(func() error {
			if err := tlsLn.Start(); err != nil {
				return err
			}
			<-ctx.Done()
			return tlsLn.Stop()
		})
	}

	// HTTP/SSE gateway for phone/web/desktop clients.
	if cfg.Server.EnableHTTP {
		httpGW := transport.NewHTTPGateway(msgBus, cfg.Server.HTTPAddr, authMgr)
		httpGW.SetSearchFn(searchIndex.SearchJSON)
		// Enable HTTPS only for non-localhost addresses (LAN/remote access).
		if !strings.HasPrefix(cfg.Server.HTTPAddr, "127.0.0.1") && !strings.HasPrefix(cfg.Server.HTTPAddr, "localhost") {
			home, _ := os.UserHomeDir()
			httpCertDir := filepath.Join(home, ".config", "notbbg", "certs")
			if _, err := os.Stat(filepath.Join(httpCertDir, "server.crt")); err == nil {
				httpGW.SetCertDir(httpCertDir)
			}
		}
		g.Go(func() error {
			return httpGW.Start(ctx)
		})
	}

	// Push data to remote collector if configured.
	if *collectorAddr != "" {
		token := *collectorToken
		if token == "" {
			token = os.Getenv("NOTBBG_COLLECTOR_TOKEN")
		}
		if token == "" {
			slog.Error("--collector-token required (or NOTBBG_COLLECTOR_TOKEN env)")
			os.Exit(1)
		}
		g.Go(func() error {
			return pushToCollector(ctx, *collectorAddr, token, msgBus)
		})
	}

	slog.Info("notbbg-server started",
		"unix_socket", cfg.Server.UnixSocket,
		"tcp_enabled", cfg.Server.EnableTCP,
		"http_enabled", cfg.Server.EnableHTTP,
		"collector", *collectorAddr,
	)

	if err := g.Wait(); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("notbbg-server stopped")
}

func handleRequest(req *transport.WireMsg, sub **bus.Subscriber, authenticated *bool, conn *transport.FramedConn, msgBus *bus.Bus, authMgr *auth.Manager, alertEngine *alerts.Engine, relay *clientRelay, store *cache.Store) {
	switch req.Type {
	case transport.MsgSubscribe:
		if *sub != nil {
			msgBus.Unsubscribe(*sub)
		}
		patterns := req.Patterns
		if len(patterns) == 0 {
			patterns = []string{"*.*.*", "news", "alert", "feed.status", "system.health", "indicator.*", "agent.suggestion"}
		}
		*sub = msgBus.Subscribe(4096, patterns...)
		slog.Info("client subscribed", "patterns", patterns)

	case transport.MsgPair:
		sessionID, _, err := authMgr.Pair(req.Token, req.ClientName)
		if err != nil {
			resp := &transport.WireMsg{Type: transport.MsgPairFail, Error: err.Error()}
			data, _ := resp.Encode()
			conn.WriteFrame(data)
		} else {
			*authenticated = true
			resp := &transport.WireMsg{Type: transport.MsgPairOK, SessionID: sessionID}
			data, _ := resp.Encode()
			conn.WriteFrame(data)
			slog.Info("client paired", "name", req.ClientName, "session", sessionID[:8])
		}

	case transport.MsgCreateAlert:
		var alertReq struct {
			Type       int     `json:"type"`
			Instrument string  `json:"instrument"`
			Threshold  float64 `json:"threshold"`
			Keyword    string  `json:"keyword"`
		}
		if json.Unmarshal(req.Payload, &alertReq) == nil {
			id := alertEngine.Add(alerts.Alert{
				Type:       alerts.AlertType(alertReq.Type),
				Instrument: alertReq.Instrument,
				Threshold:  alertReq.Threshold,
				Keyword:    alertReq.Keyword,
			})
			resp := &transport.WireMsg{Type: transport.MsgAlertCreated, SessionID: id}
			data, _ := resp.Encode()
			conn.WriteFrame(data)
		}

	case transport.MsgQuery:
		if store != nil {
			handleQuery(req, conn, store)
		}

	case transport.MsgCredit:
		if relay != nil && req.Credits > 0 {
			relay.addCredits(req.Credits)
		}

	case transport.MsgPing:
		pong := &transport.WireMsg{Type: transport.MsgPong}
		data, _ := pong.Encode()
		conn.WriteFrame(data)
	}
}

// handleQuery processes a cache query request.
// Query format: "bucket/prefix" (e.g. "ohlc/binance/BTCUSDT")
func handleQuery(req *transport.WireMsg, conn *transport.FramedConn, store *cache.Store) {
	parts := strings.SplitN(req.Query, "/", 2)
	if len(parts) < 1 {
		resp := &transport.WireMsg{Type: transport.MsgQueryResult, Error: "query format: bucket/prefix"}
		data, _ := resp.Encode()
		conn.WriteFrame(data)
		return
	}

	bucket := parts[0]
	prefix := ""
	if len(parts) > 1 {
		prefix = parts[1]
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	var results []json.RawMessage
	err := store.Scan(bucket, prefix, func(key string, data []byte) error {
		if len(results) >= limit {
			return fmt.Errorf("limit reached")
		}
		results = append(results, json.RawMessage(data))
		return nil
	})
	if err != nil && err.Error() != "limit reached" {
		resp := &transport.WireMsg{Type: transport.MsgQueryResult, Error: err.Error()}
		data, _ := resp.Encode()
		conn.WriteFrame(data)
		return
	}

	payload, _ := json.Marshal(results)
	resp := &transport.WireMsg{
		Type:    transport.MsgQueryResult,
		Topic:   req.Query,
		Payload: payload,
	}
	data, _ := resp.Encode()
	conn.WriteFrame(data)

	slog.Debug("query served", "query", req.Query, "results", len(results))
}

func handleClient(ctx context.Context, conn *transport.FramedConn, msgBus *bus.Bus, store *cache.Store, authMgr *auth.Manager, alertEngine *alerts.Engine) {
	slog.Info("client connected", "remote", conn.RemoteAddr())
	defer slog.Info("client disconnected", "remote", conn.RemoteAddr())

	var sub *bus.Subscriber
	authenticated := false
	_ = authenticated
	relay := newClientRelay(conn)

	defer func() {
		if sub != nil {
			msgBus.Unsubscribe(sub)
		}
	}()

	// Read loop: handle client requests in a goroutine.
	reqCh := make(chan *transport.WireMsg, 16)
	go func() {
		defer close(reqCh)
		for {
			frame, err := conn.ReadFrame()
			if err != nil {
				return
			}
			msg, err := transport.DecodeWireMsg(frame)
			if err != nil {
				slog.Debug("bad frame", "error", err)
				continue
			}
			select {
			case reqCh <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for subscription before entering relay loop.
	for sub == nil {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-reqCh:
			if !ok {
				return
			}
			handleRequest(req, &sub, &authenticated, conn, msgBus, authMgr, alertEngine, relay, store)
		}
	}

	// Start splitter: bus subscriber → realtime/bulk channels.
	relayCtx, relayCancel := context.WithCancel(ctx)
	defer relayCancel()
	go relay.splitter(relayCtx, sub)

	// Start sender in background, capture errors.
	senderErr := make(chan error, 1)
	go func() {
		senderErr <- relay.sender(relayCtx)
	}()

	// Handle client requests until disconnect or sender error.
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-senderErr:
			if err != nil {
				slog.Debug("relay sender error", "error", err)
			}
			return
		case req, ok := <-reqCh:
			if !ok {
				return
			}
			handleRequest(req, &sub, &authenticated, conn, msgBus, authMgr, alertEngine, relay, store)
		}
	}
}
