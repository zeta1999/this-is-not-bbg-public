package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadSessionFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.enc")
	key := "test-encryption-key-1234567890ab"
	sessionToken := "abc123def456session"

	// Save.
	if err := SaveSessionFile(path, sessionToken, key); err != nil {
		t.Fatalf("save: %v", err)
	}

	// File should exist and not be plaintext.
	data, _ := os.ReadFile(path)
	if string(data) == sessionToken {
		t.Fatal("session token stored in plaintext")
	}

	// Load.
	loaded, err := LoadSessionFile(path, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded != sessionToken {
		t.Fatalf("got %q, want %q", loaded, sessionToken)
	}

	// Wrong key should fail.
	_, err = LoadSessionFile(path, "wrong-key-wrong-key-wrong-key-00")
	if err == nil {
		t.Fatal("expected error with wrong key")
	}
}

func TestSeedSession(t *testing.T) {
	m := NewManager()

	m.SeedSession("session123", "test-client", RightRead|RightSubscribe)

	rights, err := m.Validate("session123")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if rights != RightRead|RightSubscribe {
		t.Fatalf("rights: got %d, want %d", rights, RightRead|RightSubscribe)
	}

	// Unknown session should fail.
	_, err = m.Validate("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
}
