package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.enc")

	original := &EncryptedConfig{
		APIKeys:       map[string]string{"binance": "test-api-key-123"},
		APISecrets:    map[string]string{"binance": "test-secret-456"},
		SlackWebhook:  "https://hooks.slack.com/test",
		TelegramToken: "bot123:ABC",
		TelegramChat:  "-100123",
	}

	password := []byte("test-password-789")

	// Save.
	if err := SaveEncrypted(path, original, password); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify file exists and has restricted permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}

	// Load with correct password.
	loaded, err := LoadEncrypted(path, password)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.APIKeys["binance"] != original.APIKeys["binance"] {
		t.Errorf("API key mismatch: got %q", loaded.APIKeys["binance"])
	}
	if loaded.APISecrets["binance"] != original.APISecrets["binance"] {
		t.Errorf("API secret mismatch")
	}
	if loaded.SlackWebhook != original.SlackWebhook {
		t.Errorf("Slack webhook mismatch")
	}
	if loaded.TelegramToken != original.TelegramToken {
		t.Errorf("Telegram token mismatch")
	}

	// Load with wrong password.
	_, err = LoadEncrypted(path, []byte("wrong-password"))
	if err == nil {
		t.Fatal("expected error with wrong password")
	}
}
