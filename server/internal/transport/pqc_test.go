package transport

import (
	"bytes"
	"testing"
)

func TestPQCKeyExchange(t *testing.T) {
	// Server generates key pair.
	serverKeys, err := GeneratePQCKeys()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}

	pubKeyBytes, err := serverKeys.PublicKeyBytes()
	if err != nil {
		t.Fatalf("public key bytes: %v", err)
	}
	if len(pubKeyBytes) == 0 {
		t.Fatal("empty public key")
	}

	// Client encapsulates shared secret.
	ciphertext, clientKey, err := Encapsulate(pubKeyBytes)
	if err != nil {
		t.Fatalf("encapsulate: %v", err)
	}

	// Server decapsulates.
	serverKey, err := serverKeys.Decapsulate(ciphertext)
	if err != nil {
		t.Fatalf("decapsulate: %v", err)
	}

	// Keys must match.
	if !bytes.Equal(clientKey, serverKey) {
		t.Fatal("shared keys do not match")
	}

	if len(clientKey) != 32 {
		t.Errorf("expected 32-byte key, got %d", len(clientKey))
	}
}

func TestPQCEncryptDecrypt(t *testing.T) {
	// Generate shared key via key exchange.
	serverKeys, _ := GeneratePQCKeys()
	pkBytes, _ := serverKeys.PublicKeyBytes()
	ct, key, _ := Encapsulate(pkBytes)
	serverKey, _ := serverKeys.Decapsulate(ct)

	plaintext := []byte("secret market data")

	// Encrypt with client key.
	encrypted, err := PQCEncrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Decrypt with server key (same key).
	decrypted, err := PQCDecrypt(serverKey, encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("decrypted data mismatch: got %q", decrypted)
	}
}

func TestPQCDecryptWrongKey(t *testing.T) {
	serverKeys, _ := GeneratePQCKeys()
	pkBytes, _ := serverKeys.PublicKeyBytes()
	_, key, _ := Encapsulate(pkBytes)

	encrypted, _ := PQCEncrypt(key, []byte("secret"))

	// Try decrypting with a different key.
	otherKeys, _ := GeneratePQCKeys()
	otherPK, _ := otherKeys.PublicKeyBytes()
	_, otherKey, _ := Encapsulate(otherPK)

	_, err := PQCDecrypt(otherKey, encrypted)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}
