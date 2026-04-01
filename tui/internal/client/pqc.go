package client

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/cloudflare/circl/kem/mlkem/mlkem768"
	"golang.org/x/crypto/hkdf"
	"crypto/sha256"
	"golang.org/x/crypto/chacha20poly1305"
)

var kemScheme = mlkem768.Scheme()

// PQC handshake message types (must match server).
const (
	pqcMsgPubKey     byte = 0x01
	pqcMsgCiphertext byte = 0x02
	pqcMsgReady      byte = 0x03
)

// PQCHandshake performs the client side of the ML-KEM-768 key exchange.
// Called after TLS is established. Returns the shared symmetric key.
func PQCHandshake(conn net.Conn) ([]byte, error) {
	// Read server's public key.
	msgType, pkBytes, err := readPQCMsg(conn)
	if err != nil {
		return nil, fmt.Errorf("read pubkey: %w", err)
	}
	if msgType != pqcMsgPubKey {
		return nil, fmt.Errorf("expected pubkey (0x01), got 0x%02x", msgType)
	}

	// Encapsulate: generate ciphertext + shared secret.
	pk, err := kemScheme.UnmarshalBinaryPublicKey(pkBytes)
	if err != nil {
		return nil, fmt.Errorf("unmarshal pubkey: %w", err)
	}
	ct, ss, err := kemScheme.Encapsulate(pk)
	if err != nil {
		return nil, fmt.Errorf("encapsulate: %w", err)
	}

	// Derive symmetric key.
	sharedKey, err := deriveKey(ss)
	if err != nil {
		return nil, err
	}

	// Send ciphertext to server.
	if err := writePQCMsg(conn, pqcMsgCiphertext, ct); err != nil {
		return nil, fmt.Errorf("send ciphertext: %w", err)
	}

	// Read ready signal.
	msgType, _, err = readPQCMsg(conn)
	if err != nil {
		return nil, fmt.Errorf("read ready: %w", err)
	}
	if msgType != pqcMsgReady {
		return nil, fmt.Errorf("expected ready (0x03), got 0x%02x", msgType)
	}

	slog.Debug("pqc handshake complete (client)", "kem", "ML-KEM-768")
	return sharedKey, nil
}

func deriveKey(sharedSecret []byte) ([]byte, error) {
	info := []byte("notbbg-pqc-v1")
	hk := hkdf.New(sha256.New, sharedSecret, nil, info)
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hk, key); err != nil {
		return nil, fmt.Errorf("hkdf derive: %w", err)
	}
	return key, nil
}

func writePQCMsg(conn net.Conn, msgType byte, payload []byte) error {
	header := make([]byte, 5)
	header[0] = msgType
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if _, err := conn.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := conn.Write(payload)
		return err
	}
	return nil
}

func readPQCMsg(conn net.Conn) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}
	msgType := header[0]
	size := binary.BigEndian.Uint32(header[1:])
	if size > 4*1024*1024 {
		return 0, nil, fmt.Errorf("pqc message too large: %d", size)
	}
	if size == 0 {
		return msgType, nil, nil
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return 0, nil, err
	}
	return msgType, payload, nil
}
