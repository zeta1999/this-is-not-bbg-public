// Package transport provides an HTTP/JSON gateway for clients that can't use
// raw sockets (e.g., React Native phone app, web browsers, desktop app).
package transport

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/notbbg/notbbg/server/internal/auth"
	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/datalake"
	"github.com/notbbg/notbbg/server/internal/daterange"
)

// HTTPGateway provides REST endpoints for subscribing and receiving data.
// Supports TLS and token-based authentication.
type HTTPGateway struct {
	bus             *bus.Bus
	addr            string
	authMgr         *auth.Manager
	certDir         string
	srv             *http.Server
	searchFn        func(query string, limit int) []byte // BM25 search function
	datalakeReader  *datalake.Reader // nil if datalake not configured
	dateRange       *daterange.Handler // nil if datalake not configured
}

// NewHTTPGateway creates a new HTTP gateway.
func NewHTTPGateway(b *bus.Bus, addr string, authMgr *auth.Manager) *HTTPGateway {
	if addr == "" {
		addr = "127.0.0.1:9474"
	}
	return &HTTPGateway{bus: b, addr: addr, authMgr: authMgr}
}

// SetCertDir sets the TLS certificate directory. If set, gateway serves HTTPS.
func (g *HTTPGateway) SetCertDir(dir string) { g.certDir = dir }

// SetSearchFn sets the BM25 search function for news search.
func (g *HTTPGateway) SetSearchFn(fn func(query string, limit int) []byte) { g.searchFn = fn }

// Start begins serving. Uses HTTPS if certDir is set, HTTP otherwise.
func (g *HTTPGateway) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Health endpoint — no auth required (used for connectivity check).
	mux.HandleFunc("/api/v1/health", g.handleHealth)

	// QR pairing — no auth (generates token).
	mux.HandleFunc("/api/v1/pair/qr", g.handlePairQR)

	// Token pairing — exchanges pairing token for session token (phone app).
	mux.HandleFunc("/api/v1/pair", g.handlePair)

	// Generate fresh phone token — requires auth (desktop/TUI calls this).
	mux.HandleFunc("/api/v1/pair/phone", g.requireAuth(g.handlePairPhone))

	// All data endpoints require auth.
	mux.HandleFunc("/api/v1/subscribe", g.requireAuth(g.handleSSE))
	mux.HandleFunc("/api/v1/snapshot", g.requireAuth(g.handleSnapshot))
	mux.HandleFunc("/api/v1/news/search", g.requireAuth(g.handleNewsSearch))
	mux.HandleFunc("/api/v1/agent/exec", g.requireAuth(g.handleAgentExec))
	mux.HandleFunc("/api/v1/plugin/input", g.requireAuth(g.handlePluginInput))
	mux.HandleFunc("/api/v1/history", g.requireAuth(g.handleHistory))
	mux.HandleFunc("/api/v1/datarange", g.requireAuth(g.handleDataRange))

	// Wrap with CORS middleware for Electron/browser access.
	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	})

	g.srv = &http.Server{Addr: g.addr, Handler: corsHandler}

	errCh := make(chan error, 1)

	if g.certDir != "" {
		// HTTPS mode.
		tlsCfg, err := loadOrGenerateHTTPSCert(g.certDir)
		if err != nil {
			return fmt.Errorf("https cert: %w", err)
		}
		g.srv.TLSConfig = tlsCfg
		slog.Info("https gateway listening", "addr", g.addr)
		go func() {
			if err := g.srv.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
				errCh <- err
			}
			close(errCh)
		}()
	} else {
		// HTTP mode (localhost only).
		slog.Info("http gateway listening (localhost only)", "addr", g.addr)
		go func() {
			if err := g.srv.ListenAndServe(); err != http.ErrServerClosed {
				errCh <- err
			}
			close(errCh)
		}()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return g.srv.Shutdown(shutdownCtx)
	}
}

// requireAuth wraps a handler with token authentication.
// Accepts: Authorization: Bearer <token> header or ?token=<token> query param.
func (g *HTTPGateway) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := ""

		// Check Authorization header.
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}

		// Fall back to query parameter.
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		// Check NOTBBG_TOKEN cookie.
		if token == "" {
			if c, err := r.Cookie("notbbg_token"); err == nil {
				token = c.Value
			}
		}

		if token == "" {
			http.Error(w, `{"error":"authorization required"}`, http.StatusUnauthorized)
			return
		}

		// Validate token.
		if _, err := g.authMgr.Validate(token); err != nil {
			// Also try as pairing token (for first-time access).
			if sessionID, _, pairErr := g.authMgr.Pair(token, "http-client"); pairErr == nil {
				// Set cookie with session token for subsequent requests.
				http.SetCookie(w, &http.Cookie{
					Name:     "notbbg_token",
					Value:    sessionID,
					Path:     "/",
					HttpOnly: true,
					Secure:   g.certDir != "",
					SameSite: http.SameSiteStrictMode,
					MaxAge:   30 * 24 * 3600,
				})
				next(w, r)
				return
			}
			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// loadOrGenerateHTTPSCert loads or generates self-signed TLS certs for HTTPS.
func loadOrGenerateHTTPSCert(certDir string) (*tls.Config, error) {
	certFile := certDir + "/server.crt"
	keyFile := certDir + "/server.key"

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		// Certs will be generated by TLSListener if needed — just fail gracefully.
		return nil, fmt.Errorf("load cert %s: %w (run server with TCP enabled first to generate certs)", certFile, err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// handleSSE streams server-sent events for the given topic patterns.
// GET /api/v1/subscribe?patterns=ohlc.*.*,news,alert
func (g *HTTPGateway) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	patternsParam := r.URL.Query().Get("patterns")
	if patternsParam == "" {
		patternsParam = "ohlc.*.*,lob.*.*,news,alert,feed.status"
	}

	patterns := splitPatterns(patternsParam)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sub := g.bus.Subscribe(256, patterns...)
	defer g.bus.Unsubscribe(sub)

	// Send latest snapshot of key topics so late-joining clients get current state.
	// This ensures plugin.registry, feed.status, etc. are received even if published before SSE connect.
	for _, snapshot := range g.bus.LatestPerTopic(patterns...) {
		payload, err := json.Marshal(snapshot.Payload)
		if err != nil {
			continue
		}
		event := fmt.Sprintf("data: {\"_topic\":\"%s\",\"_payload\":%s}\n\n", snapshot.Topic, payload)
		_, _ = w.Write([]byte(event))
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.C:
			if !ok {
				return
			}
			payload, err := json.Marshal(msg.Payload)
			if err != nil {
				continue
			}
			// No named event — puts everything in onmessage for EventSource compatibility.
			event := fmt.Sprintf("data: {\"_topic\":\"%s\",\"_payload\":%s}\n\n", msg.Topic, payload)
			_, _ = w.Write([]byte(event))
			flusher.Flush()
		}
	}
}

// handleSnapshot returns recent data as a JSON array.
// GET /api/v1/snapshot?topic=ohlc.binance.BTCUSDT&limit=50
// GET /api/v1/snapshot?topic=lob.*.*&mode=latest  (one per instrument)
func (g *HTTPGateway) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	if topic == "" {
		http.Error(w, "topic required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// mode=latest: return only the most recent message per matching topic.
	// Ideal for LOB snapshots, feed status, etc.
	if r.URL.Query().Get("mode") == "latest" {
		msgs := g.bus.LatestPerTopic(topic)
		items := make([]any, len(msgs))
		for i, m := range msgs {
			items[i] = m.Payload
		}
		_ = json.NewEncoder(w).Encode(items)
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		_, _ = fmt.Sscanf(limitStr, "%d", &limit)
	}

	sub := g.bus.Subscribe(limit, topic)
	defer g.bus.Unsubscribe(sub)

	var items []any
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		timeout := time.After(2 * time.Second)
		for {
			select {
			case msg, ok := <-sub.C:
				if !ok {
					close(done)
					return
				}
				mu.Lock()
				items = append(items, msg.Payload)
				if len(items) >= limit {
					mu.Unlock()
					close(done)
					return
				}
				mu.Unlock()
			case <-timeout:
				close(done)
				return
			}
		}
	}()

	<-done

	_ = json.NewEncoder(w).Encode(items)
}

// handleHealth returns server health status.
func (g *HTTPGateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// handlePair exchanges a pairing token for a session token.
// POST /api/v1/pair  body: {"token":"..."}
// Returns: {"session":"<session-id>"} on success.
func (g *HTTPGateway) handlePair(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		http.Error(w, `{"error":"token required"}`, http.StatusBadRequest)
		return
	}

	// Try as session token first (already paired).
	if _, err := g.authMgr.Validate(body.Token); err == nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"session": body.Token})
		return
	}

	// Try as pairing token.
	sessionID, _, err := g.authMgr.Pair(body.Token, "phone")
	if err != nil {
		http.Error(w, `{"error":"invalid or expired token"}`, http.StatusForbidden)
		return
	}

	slog.Info("phone paired via /api/v1/pair", "session", sessionID[:8]+"...")
	_ = json.NewEncoder(w).Encode(map[string]string{"session": sessionID})
}

// handlePairQR generates a one-time pairing token and returns a QR code PNG.
// GET /api/v1/pair/qr?host=192.168.1.10&port=9473
func (g *HTTPGateway) handlePairQR(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if host == "" {
		host = r.Host
		for i, c := range host {
			if c == ':' {
				host = host[:i]
				break
			}
		}
	}
	port := 9473
	if portStr := r.URL.Query().Get("port"); portStr != "" {
		_, _ = fmt.Sscanf(portStr, "%d", &port)
	}

	token, err := g.authMgr.GeneratePairingToken(
		auth.RightRead|auth.RightSubscribe, 5*time.Minute,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	png, err := auth.GenerateQRPNG(host, port, token, 256)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// handlePairPhone generates a fresh phone session token.
// POST /api/v1/pair/phone (requires auth — called by desktop/TUI).
// Returns: {"token":"...", "qr":"<base64 PNG>"}
func (g *HTTPGateway) handlePairPhone(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Generate a new phone session token.
	sessionID, err := auth.GenerateTokenID()
	if err != nil {
		http.Error(w, `{"error":"token generation failed"}`, http.StatusInternalServerError)
		return
	}
	g.authMgr.SeedSession(sessionID, "phone-app", auth.RightRead|auth.RightSubscribe)

	// Write to file for manual access.
	_ = os.WriteFile("/tmp/notbbg-phone.token", []byte(sessionID), 0600)

	// Build QR payload: URL + token for the phone to auto-pair.
	httpURL := "http://" + g.addr
	qrPayload, _ := json.Marshal(map[string]string{"url": httpURL, "token": sessionID})
	qrPNG, err := qrcode.Encode(string(qrPayload), qrcode.Medium, 256)
	qrB64 := ""
	if err == nil {
		qrB64 = base64.StdEncoding.EncodeToString(qrPNG)
	}

	slog.Info("generated fresh phone token", "session", sessionID[:8]+"...")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token": sessionID,
		"qr":    qrB64,
	})
}

// handleNewsSearch searches news via BM25 index or bus ring buffer fallback.
// GET /api/v1/news/search?q=solana&limit=100
func (g *HTTPGateway) handleNewsSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, `{"error":"q parameter required"}`, http.StatusBadRequest)
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		_, _ = fmt.Sscanf(limitStr, "%d", &limit)
	}

	// Try BM25 search function if available.
	if g.searchFn != nil {
		data := g.searchFn(query, limit)
		if data != nil && string(data) != "null" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}
	}

	// Fallback: search bus ring buffer.
	sub := g.bus.Subscribe(1024, "news")
	defer g.bus.Unsubscribe(sub)

	var results []any
	deadline := time.After(2 * time.Second)

	for len(results) < limit {
		select {
		case msg, ok := <-sub.C:
			if !ok {
				goto done
			}
			// Check if payload matches query.
			payloadBytes, err := json.Marshal(msg.Payload)
			if err != nil {
				continue
			}
			if strings.Contains(strings.ToLower(string(payloadBytes)), query) {
				results = append(results, msg.Payload)
			}
		case <-deadline:
			goto done
		}
	}
done:

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

// handleAgentExec runs claude -p with the given prompt and returns the response.
// POST /api/v1/agent/exec  body: {"prompt": "describe this project"}
func (g *HTTPGateway) handleAgentExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		http.Error(w, `{"error":"prompt required"}`, http.StatusBadRequest)
		return
	}

	// Run claude -p with the prompt. Timeout 60s.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", req.Prompt)
	cmd.Dir = "."
	cmd.Stdin = nil
	output, err := cmd.CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": string(output),
			"error":    err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"response": string(output),
	})
}

func splitPatterns(s string) []string {
	var patterns []string
	current := ""
	for _, c := range s {
		if c == ',' {
			if current != "" {
				patterns = append(patterns, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		patterns = append(patterns, current)
	}
	return patterns
}

// handlePluginInput receives cell edit events from desktop/phone and publishes to bus.
// POST /api/v1/plugin/input
// body: {"topic": "plugin.pricer.screen", "screen_id": "PRICER", "address": {"row":2,"col":1}, "value": 70000}
func (g *HTTPGateway) handlePluginInput(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Topic    string `json:"topic"`
		ScreenID string `json:"screen_id"`
		Address  struct {
			Row uint32 `json:"row"`
			Col uint32 `json:"col"`
		} `json:"address"`
		Value any `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Derive input topic from screen topic.
	inputTopic := strings.TrimSuffix(req.Topic, ".screen") + ".input"
	g.bus.Publish(bus.Message{
		Topic: inputTopic,
		Payload: map[string]any{
			"screen_id": req.ScreenID,
			"address":   map[string]any{"row": req.Address.Row, "col": req.Address.Col},
			"value":     req.Value,
		},
	})

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// SetDatalakeReader sets the datalake reader for historical queries.
func (g *HTTPGateway) SetDatalakeReader(r *datalake.Reader) {
	g.datalakeReader = r
}

// SetDateRangeHandler wires the streaming DataRange handler. The HTTP
// endpoint at /api/v1/datarange mirrors the notbbg.v1.DataService
// GetDataRange RPC shape — the same server logic, exposed over HTTP
// so phone/desktop clients can consume it without gRPC tooling.
func (g *HTTPGateway) SetDateRangeHandler(h *daterange.Handler) {
	g.dateRange = h
}

// handleDataRange streams NDJSON chunks mirroring the proto
// GetDataRangeResponse shape. Query params:
//
//	topic=ohlc.binance.BTCUSDT  (required)
//	from=2026-04-01T00:00:00Z   (required, RFC3339)
//	to=2026-04-02T00:00:00Z     (required, RFC3339)
//	resolution=1m               (optional)
//	correlation_id=<string>     (optional, echoed in each chunk)
//	max_records=5000            (optional; 0 = server default)
//
// Response is one JSON object per line. The final line has "eof":true.
// Client-side cancellation is supported by closing the connection.
func (g *HTTPGateway) handleDataRange(w http.ResponseWriter, r *http.Request) {
	if g.dateRange == nil {
		http.Error(w, `{"error":"datarange not configured"}`, http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	fromStr := q.Get("from")
	toStr := q.Get("to")

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		http.Error(w, `{"error":"bad from (need RFC3339)"}`, http.StatusBadRequest)
		return
	}
	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		http.Error(w, `{"error":"bad to (need RFC3339)"}`, http.StatusBadRequest)
		return
	}
	maxRecords := 0
	if s := q.Get("max_records"); s != "" {
		_, _ = fmt.Sscanf(s, "%d", &maxRecords)
	}
	req := daterange.Request{
		Topic:         q.Get("topic"),
		From:          from,
		To:            to,
		Resolution:    q.Get("resolution"),
		CorrelationID: q.Get("correlation_id"),
		MaxRecords:    maxRecords,
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)

	err = g.dateRange.Serve(r.Context(), req, func(c daterange.Chunk) error {
		if err := enc.Encode(c); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		// Stream already started — write a terminal error line. The
		// client parses NDJSON so trailing malformed output on failure
		// is not helpful; stick with the proto DataRangeError shape.
		_ = enc.Encode(daterange.Error{
			CorrelationID: req.CorrelationID,
			Reason:        err.Error(),
		})
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// handleHistory serves historical data from the datalake.
// GET /api/v1/history?type=ohlc&exchange=binance&instrument=BTCUSDT&start=2026-04-01&end=2026-04-07&limit=1000
func (g *HTTPGateway) handleHistory(w http.ResponseWriter, r *http.Request) {
	if g.datalakeReader == nil {
		http.Error(w, `{"error":"datalake not configured"}`, http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	dataType := q.Get("type")
	exchange := q.Get("exchange")
	instrument := q.Get("instrument")
	startStr := q.Get("start")
	endStr := q.Get("end")
	limitStr := q.Get("limit")

	if dataType == "" {
		http.Error(w, `{"error":"type parameter required (ohlc, trade, lob)"}`, http.StatusBadRequest)
		return
	}

	start := time.Now().AddDate(0, 0, -1) // default: last 24h
	end := time.Now()
	if startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t
		}
	}

	limit := 1000
	if limitStr != "" {
		_, _ = fmt.Sscanf(limitStr, "%d", &limit)
	}

	records, err := g.datalakeReader.Query(dataType, exchange, instrument, start, end, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(records)
}
