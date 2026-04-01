// Package client handles connecting to the notbbg server via Unix socket or TCP.
package client

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const frameMaxSize = 16 * 1024 * 1024

// Client connects to a notbbg server.
type Client struct {
	conn   net.Conn
	pqcKey []byte // PQC-derived symmetric key (nil for local unix socket)
}

// ConnectUnix connects via Unix domain socket.
func ConnectUnix(path string) (*Client, error) {
	if path == "" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			path = xdg + "/notbbg.sock"
		} else {
			path = "/tmp/notbbg.sock"
		}
	}

	conn, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect unix %s: %w", path, err)
	}
	return &Client{conn: conn}, nil
}

// ConnectTCP connects via TCP (for remote/LAN servers).
func ConnectTCP(addr string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect tcp %s: %w", addr, err)
	}
	return &Client{conn: conn}, nil
}

// ConnectTLS connects via TCP+TLS and performs PQC key exchange.
// The connection is protected by both TLS 1.3 and ML-KEM-768.
func ConnectTLS(addr string) (*Client, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		InsecureSkipVerify: true, // self-signed certs from collector
		MinVersion:         tls.VersionTLS13,
	})
	if err != nil {
		return nil, fmt.Errorf("connect tls %s: %w", addr, err)
	}

	// PQC handshake on top of TLS: ML-KEM-768 key exchange.
	pqcKey, err := PQCHandshake(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("pqc handshake: %w", err)
	}

	return &Client{conn: conn, pqcKey: pqcKey}, nil
}

// WriteFrame sends a length-prefixed frame.
func (c *Client) WriteFrame(data []byte) error {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := c.conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := c.conn.Write(data)
	return err
}

// ReadFrame reads a length-prefixed frame.
func (c *Client) ReadFrame() ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(c.conn, lenBuf[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(lenBuf[:])
	if size > frameMaxSize {
		return nil, fmt.Errorf("frame too large: %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
