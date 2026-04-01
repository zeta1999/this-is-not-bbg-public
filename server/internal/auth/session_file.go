package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

// sessionFileKey derives a simple key for session file encryption.
// Uses the pairing token itself as key material (it's already high entropy).
func sessionFileKey(secret []byte) []byte {
	// Pad/truncate to 32 bytes.
	key := make([]byte, chacha20poly1305.KeySize)
	copy(key, secret)
	return key
}

// SaveSessionFile encrypts and saves a session token to a file.
func SaveSessionFile(path, sessionToken, encryptionKey string) error {
	key := sessionFileKey([]byte(encryptionKey))
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := aead.Seal(nonce, nonce, []byte(sessionToken), nil)
	return os.WriteFile(path, []byte(hex.EncodeToString(ciphertext)), 0600)
}

// LoadSessionFile decrypts and loads a session token from a file.
func LoadSessionFile(path, encryptionKey string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	ciphertext, err := hex.DecodeString(string(data))
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	key := sessionFileKey([]byte(encryptionKey))
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < aead.NonceSize() {
		return "", fmt.Errorf("too short")
	}
	plaintext, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

// SeedSession creates a session token directly (for restored sessions).
func (m *Manager) SeedSession(sessionID, clientName string, rights uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.sessions[sessionID] = &Token{
		ID:         sessionID,
		ClientName: clientName,
		Rights:     rights,
		IssuedAt:   now,
		ExpiresAt:  now.Add(30 * 24 * time.Hour),
	}
	slog.Info("session token seeded", "session", sessionID[:min(8, len(sessionID))])
}
