package main

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
	"github.com/notbbg/notbbg/server/internal/cache"
	"github.com/notbbg/notbbg/server/internal/transport"
)

// TestEndToEnd verifies the full data flow:
// start bus + cache + unix listener → connect client → subscribe → publish → receive.
func TestEndToEnd(t *testing.T) {
	// Setup.
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	dbPath := filepath.Join(dir, "test.db")

	msgBus := bus.New(100)
	store, err := cache.Open(dbPath, time.Hour)
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer store.Close()

	// Start Unix listener.
	ln := transport.NewUnixListener(sockPath, func(conn *transport.FramedConn) {
		// Simplified handleClient for testing.
		var sub *bus.Subscriber
		defer func() {
			if sub != nil {
				msgBus.Unsubscribe(sub)
			}
		}()

		for {
			frame, err := conn.ReadFrame()
			if err != nil {
				return
			}
			msg, err := transport.DecodeWireMsg(frame)
			if err != nil {
				continue
			}
			if msg.Type == transport.MsgSubscribe {
				if sub != nil {
					msgBus.Unsubscribe(sub)
				}
				sub = msgBus.Subscribe(64, msg.Patterns...)

				// Relay in background.
				go func() {
					for m := range sub.C {
						payload, _ := json.Marshal(m.Payload)
						wireMsg := &transport.WireMsg{
							Type:    transport.MsgUpdate,
							Topic:   m.Topic,
							Payload: payload,
						}
						data, _ := wireMsg.Encode()
						if err := conn.WriteFrame(data); err != nil {
							return
						}
					}
				}()
			}
		}
	})

	if err := ln.Start(); err != nil {
		t.Fatalf("start listener: %v", err)
	}
	defer func() { _ = ln.Stop() }()

	// Wait for socket.
	time.Sleep(50 * time.Millisecond)

	// Connect client.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	fc := &transport.FramedConn{Conn: conn}

	// Subscribe.
	subMsg := &transport.WireMsg{
		Type:     transport.MsgSubscribe,
		Patterns: []string{"test.*"},
	}
	data, _ := subMsg.Encode()
	if err := fc.WriteFrame(data); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Publish data on the bus.
	msgBus.Publish(bus.Message{
		Topic:   "test.hello",
		Payload: map[string]string{"msg": "world"},
	})

	// Read response.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	frame, err := fc.ReadFrame()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var received transport.WireMsg
	if err := json.Unmarshal(frame, &received); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if received.Type != transport.MsgUpdate {
		t.Errorf("expected update, got %s", received.Type)
	}
	if received.Topic != "test.hello" {
		t.Errorf("expected topic test.hello, got %s", received.Topic)
	}

	var payload map[string]string
	_ = json.Unmarshal(received.Payload, &payload)
	if payload["msg"] != "world" {
		t.Errorf("expected msg=world, got %s", payload["msg"])
	}

	// Verify non-matching topics are not received.
	msgBus.Publish(bus.Message{
		Topic:   "other.topic",
		Payload: "should not arrive",
	})

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, err = fc.ReadFrame()
	if err == nil {
		t.Error("expected timeout for non-matching topic, but received data")
	}
	// Accept either timeout or EOF — the point is no data arrived, not
	// which exact error we saw.
	_ = err
}
