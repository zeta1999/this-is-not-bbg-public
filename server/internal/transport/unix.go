// Package transport provides Unix socket and TCP+TLS listeners for the server.
package transport

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
)

// FrameMaxSize is the maximum allowed protobuf frame size (16 MB).
const FrameMaxSize = 16 * 1024 * 1024

// ConnHandler is called for each new connection with a framed reader/writer.
type ConnHandler func(conn *FramedConn)

// FramedConn wraps a net.Conn with length-prefixed framing.
type FramedConn struct {
	net.Conn
}

// ReadFrame reads a length-prefixed frame: 4-byte big-endian length + payload.
func (fc *FramedConn) ReadFrame() ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(fc.Conn, lenBuf[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(lenBuf[:])
	if size > FrameMaxSize {
		return nil, fmt.Errorf("frame too large: %d bytes", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(fc.Conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// WriteFrame writes a length-prefixed frame.
func (fc *FramedConn) WriteFrame(data []byte) error {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := fc.Conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := fc.Conn.Write(data)
	return err
}

// UnixListener listens on a Unix domain socket.
type UnixListener struct {
	path    string
	ln      net.Listener
	handler ConnHandler
	wg      sync.WaitGroup
	done    chan struct{}
}

// NewUnixListener creates a listener on the given socket path.
func NewUnixListener(path string, handler ConnHandler) *UnixListener {
	return &UnixListener{
		path:    path,
		handler: handler,
		done:    make(chan struct{}),
	}
}

// Start begins accepting connections. Call Stop to shut down.
func (ul *UnixListener) Start() error {
	// Remove stale socket file.
	if err := os.Remove(ul.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", ul.path)
	if err != nil {
		return fmt.Errorf("listen unix %s: %w", ul.path, err)
	}
	ul.ln = ln

	// Set socket permissions.
	if err := os.Chmod(ul.path, 0700); err != nil {
		slog.Warn("chmod socket", "error", err)
	}

	slog.Info("unix socket listening", "path", ul.path)

	ul.wg.Add(1)
	go func() {
		defer ul.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ul.done:
					return
				default:
					slog.Error("accept", "error", err)
					continue
				}
			}
			ul.wg.Add(1)
			go func() {
				defer ul.wg.Done()
				defer conn.Close()
				ul.handler(&FramedConn{Conn: conn})
			}()
		}
	}()

	return nil
}

// Stop shuts down the listener and waits for connections to drain.
func (ul *UnixListener) Stop() error {
	close(ul.done)
	if ul.ln != nil {
		ul.ln.Close()
	}
	ul.wg.Wait()
	os.Remove(ul.path)
	return nil
}
