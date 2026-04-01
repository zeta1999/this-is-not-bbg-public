package config

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// EncryptedConfig stores sensitive configuration fields.
type EncryptedConfig struct {
	APIKeys       map[string]string `json:"api_keys"`       // exchange -> key
	APISecrets    map[string]string `json:"api_secrets"`     // exchange -> secret
	SlackWebhook  string            `json:"slack_webhook"`
	TelegramToken string            `json:"telegram_token"`
	TelegramChat  string            `json:"telegram_chat"`
}

// encryptedFile is the on-disk format.
type encryptedFile struct {
	Salt       []byte `json:"salt"`        // 32 bytes
	Nonce      []byte `json:"nonce"`       // 24 bytes (XChaCha20-Poly1305)
	Ciphertext []byte `json:"ciphertext"`
	VDFRounds  int    `json:"vdf_rounds"`  // iterated SHA-512 rounds
}

const (
	defaultArgonTime    = 3
	defaultArgonMemory  = 256 * 1024 // 256 MB in KiB
	defaultArgonThreads = 4
	defaultArgonKeyLen  = 32
	defaultVDFRounds    = 1000
)

// DeriveKey derives a symmetric key from a password using Argon2id + VDF.
func DeriveKey(password, salt []byte, vdfRounds int) []byte {
	// Argon2id first pass.
	key := argon2.IDKey(password, salt, defaultArgonTime, defaultArgonMemory, defaultArgonThreads, defaultArgonKeyLen)

	// VDF: iterated SHA-512 for additional time-hardening.
	if vdfRounds <= 0 {
		vdfRounds = defaultVDFRounds
	}
	for i := 0; i < vdfRounds; i++ {
		h := sha512.Sum512(key)
		key = h[:defaultArgonKeyLen]
	}

	return key
}

// SaveEncrypted encrypts and writes the config to disk.
func SaveEncrypted(path string, cfg *EncryptedConfig, password []byte) error {
	// Generate salt and nonce.
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}

	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	// Derive key.
	key := DeriveKey(password, salt, defaultVDFRounds)

	// Encrypt.
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}

	plaintext, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	// Write file.
	ef := encryptedFile{
		Salt:       salt,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		VDFRounds:  defaultVDFRounds,
	}

	data, err := json.Marshal(ef)
	if err != nil {
		return fmt.Errorf("marshal file: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// LoadEncrypted decrypts and reads the config from disk.
func LoadEncrypted(path string, password []byte) (*EncryptedConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var ef encryptedFile
	if err := json.Unmarshal(data, &ef); err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}

	// Derive key.
	key := DeriveKey(password, ef.Salt, ef.VDFRounds)

	// Decrypt.
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	plaintext, err := aead.Open(nil, ef.Nonce, ef.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt config (wrong password?): %w", err)
	}

	cfg := &EncryptedConfig{}
	if err := json.Unmarshal(plaintext, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}
