package transport

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
)

// PQC handshake message types.
const (
	pqcMsgPubKey     byte = 0x01 // server → client: ML-KEM-768 public key
	pqcMsgCiphertext byte = 0x02 // client → server: KEM ciphertext
	pqcMsgReady      byte = 0x03 // server → client: handshake complete
)

// PQCHandshakeServer performs the server side of the PQC key exchange.
// Called after TLS is established. Returns the shared symmetric key.
func PQCHandshakeServer(conn *FramedConn) ([]byte, error) {
	// Generate ephemeral KEM keys.
	keys, err := GeneratePQCKeys()
	if err != nil {
		return nil, fmt.Errorf("pqc keygen: %w", err)
	}

	// Send public key to client.
	pkBytes, err := keys.PublicKeyBytes()
	if err != nil {
		return nil, err
	}
	if err := writePQCMsg(conn, pqcMsgPubKey, pkBytes); err != nil {
		return nil, fmt.Errorf("send pubkey: %w", err)
	}

	// Read ciphertext from client.
	msgType, ct, err := readPQCMsg(conn)
	if err != nil {
		return nil, fmt.Errorf("read ciphertext: %w", err)
	}
	if msgType != pqcMsgCiphertext {
		return nil, fmt.Errorf("expected ciphertext (0x02), got 0x%02x", msgType)
	}

	// Decapsulate to get shared secret.
	sharedKey, err := keys.Decapsulate(ct)
	if err != nil {
		return nil, fmt.Errorf("decapsulate: %w", err)
	}

	// Send ready signal.
	if err := writePQCMsg(conn, pqcMsgReady, nil); err != nil {
		return nil, fmt.Errorf("send ready: %w", err)
	}

	slog.Info("pqc handshake complete (server)", "kem", "ML-KEM-768")
	return sharedKey, nil
}

// PQCHandshakeClient performs the client side of the PQC key exchange.
// Called after TLS is established. Returns the shared symmetric key.
func PQCHandshakeClient(conn *FramedConn) ([]byte, error) {
	msgType, pkBytes, err := readPQCMsg(conn)
	if err != nil {
		return nil, fmt.Errorf("read pubkey: %w", err)
	}
	if msgType != pqcMsgPubKey {
		return nil, fmt.Errorf("expected pubkey (0x01), got 0x%02x", msgType)
	}

	// Encapsulate: generate ciphertext + shared secret.
	ct, sharedKey, err := Encapsulate(pkBytes)
	if err != nil {
		return nil, fmt.Errorf("encapsulate: %w", err)
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

	slog.Info("pqc handshake complete (client)", "kem", "ML-KEM-768")
	return sharedKey, nil
}

// Wire format: [1 byte type] [4 byte big-endian length] [payload]
func writePQCMsg(conn *FramedConn, msgType byte, payload []byte) error {
	header := make([]byte, 5)
	header[0] = msgType
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if _, err := conn.Conn.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := conn.Conn.Write(payload)
		return err
	}
	return nil
}

func readPQCMsg(conn *FramedConn) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn.Conn, header); err != nil {
		return 0, nil, err
	}
	msgType := header[0]
	size := binary.BigEndian.Uint32(header[1:])
	if size > 4*1024*1024 { // 4MB max for PQC messages
		return 0, nil, fmt.Errorf("pqc message too large: %d", size)
	}
	if size == 0 {
		return msgType, nil, nil
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(conn.Conn, payload); err != nil {
		return 0, nil, err
	}
	return msgType, payload, nil
}
