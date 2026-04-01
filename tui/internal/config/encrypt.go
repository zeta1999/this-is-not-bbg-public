package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// DeriveKey derives an AES-256 key from a password using Argon2id.
func DeriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
}

// EncryptToken encrypts a token with a password-derived key.
// Returns "enc:<hex(salt+nonce+ciphertext)>".
func EncryptToken(plaintext, password string) (string, error) {
	if plaintext == "" || password == "" {
		return plaintext, nil
	}

	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}

	key := DeriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	// Format: salt (16 bytes) + nonce+ciphertext
	result := append(salt, ciphertext...)
	return "enc:" + hex.EncodeToString(result), nil
}

// DecryptToken decrypts a token using a password.
// If not encrypted (no "enc:" prefix), returns as-is.
func DecryptToken(stored, password string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if len(stored) < 4 || stored[:4] != "enc:" {
		return stored, nil // plaintext (legacy)
	}

	data, err := hex.DecodeString(stored[4:])
	if err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	if len(data) < 16 {
		return "", fmt.Errorf("token too short")
	}

	salt := data[:16]
	rest := data[16:]

	key := DeriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(rest) < nonceSize {
		return "", fmt.Errorf("token too short")
	}

	plaintext, err := gcm.Open(nil, rest[:nonceSize], rest[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("wrong password or corrupted token")
	}
	return string(plaintext), nil
}
