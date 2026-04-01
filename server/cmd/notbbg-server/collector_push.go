package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/transport"
)

// pushToCollector connects to a remote collector, authenticates, then pushes
// all bus messages over TLS+PQC. Uses session token for reconnects, falls back
// to original pairing token if session is rejected (collector restarted).
func pushToCollector(ctx context.Context, addr, originalToken string, msgBus *bus.Bus) error {
	sessionToken := ""

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Try session token first, fall back to original.
		token := sessionToken
		isSession := true
		if token == "" {
			token = originalToken
			isSession = false
		}

		newSession, err := pushSession(ctx, addr, token, isSession, msgBus)
		if ctx.Err() != nil {
			return nil
		}

		if newSession != "" {
			sessionToken = newSession
		}

		if err != nil {
			// If session auth failed, clear it so we retry with original token.
			if isSession && sessionToken != "" {
				slog.Info("session rejected, will retry with pairing token")
				sessionToken = ""
			}
			slog.Warn("collector push failed, retrying in 5s", "addr", addr, "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func pushSession(ctx context.Context, addr, token string, isSession bool, msgBus *bus.Bus) (string, error) {
	slog.Info("connecting to collector", "addr", addr, "session_reconnect", isSession)

	// TLS connect.
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tlsConn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	})
	if err != nil {
		return "", fmt.Errorf("tls connect: %w", err)
	}
	defer tlsConn.Close()

	conn := &transport.FramedConn{Conn: tlsConn}

	// PQC handshake.
	pqcKey, err := transport.PQCHandshakeClient(conn)
	if err != nil {
		return "", fmt.Errorf("pqc handshake: %w", err)
	}
	_ = pqcKey
	slog.Info("pqc handshake complete with collector", "kem", "ML-KEM-768")

	// Send pair or session reconnect.
	msgType := transport.MsgPair
	if isSession {
		msgType = transport.MsgPair // same message type, but token is a session ID
	}
	pairMsg := &transport.WireMsg{
		Type:       msgType,
		Token:      token,
		ClientName: "notbbg-server",
	}
	if isSession {
		pairMsg.SessionID = token // use SessionID field for reconnects
		pairMsg.Token = ""
	}
	pairData, _ := pairMsg.Encode()
	if err := conn.WriteFrame(pairData); err != nil {
		return "", fmt.Errorf("send pair: %w", err)
	}

	// Read pair response.
	frame, err := conn.ReadFrame()
	if err != nil {
		return "", fmt.Errorf("read pair response: %w", err)
	}
	var pairResp transport.WireMsg
	if json.Unmarshal(frame, &pairResp) != nil {
		return "", fmt.Errorf("parse pair response")
	}
	if pairResp.Type == transport.MsgPairFail {
		return "", fmt.Errorf("pairing rejected: %s", pairResp.Error)
	}
	if pairResp.Type != transport.MsgPairOK {
		return "", fmt.Errorf("unexpected response: %s", pairResp.Type)
	}

	sessionID := pairResp.SessionID
	slog.Info("paired with collector", "addr", addr, "session", sessionID[:8])

	// Subscribe to all topics on our bus.
	sub := msgBus.Subscribe(8192, "ohlc.*.*", "lob.*.*", "trade.*.*", "news", "alert", "feed.status", "indicator.*", "perp.*.*")
	defer msgBus.Unsubscribe(sub)

	// Push loop.
	pushed := 0
	for {
		select {
		case <-ctx.Done():
			slog.Info("collector push stopped", "pushed", pushed)
			return sessionID, nil

		case msg, ok := <-sub.C:
			if !ok {
				return sessionID, nil
			}

			payload, err := json.Marshal(msg.Payload)
			if err != nil {
				continue
			}
			wireMsg := &transport.WireMsg{
				Type:    transport.MsgUpdate,
				Topic:   msg.Topic,
				Payload: payload,
			}
			data, _ := wireMsg.Encode()

			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteFrame(data); err != nil {
				return sessionID, fmt.Errorf("write to collector: %w", err)
			}
			conn.SetWriteDeadline(time.Time{})

			pushed++
			if pushed%1000 == 0 {
				slog.Info("pushed to collector", "messages", pushed)
			}
		}
	}
}
