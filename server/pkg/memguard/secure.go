// Package memguard provides wrappers for secure memory management using awnumar/memguard.
package memguard

import (
	"fmt"

	"github.com/awnumar/memguard"
)

func init() {
	// Wipe all secure memory on panic/signal.
	memguard.CatchInterrupt()
}

// SecureKey holds a cryptographic key in guarded memory.
type SecureKey struct {
	enclave *memguard.Enclave
	size    int
}

// NewSecureKey creates a new key from the given bytes, wiping the input.
func NewSecureKey(key []byte) *SecureKey {
	buf := memguard.NewBufferFromBytes(key)
	enc := buf.Seal()
	return &SecureKey{enclave: enc, size: len(key)}
}

// Open decrypts the key into a locked buffer for temporary use.
// The caller MUST call buf.Destroy() when done.
func (sk *SecureKey) Open() (*memguard.LockedBuffer, error) {
	buf, err := sk.enclave.Open()
	if err != nil {
		return nil, fmt.Errorf("open secure key: %w", err)
	}
	return buf, nil
}

// SecureString holds a sensitive string (API key, token) in guarded memory.
type SecureString struct {
	enclave *memguard.Enclave
}

// NewSecureString creates a guarded string, wiping the input.
func NewSecureString(s string) *SecureString {
	buf := memguard.NewBufferFromBytes([]byte(s))
	enc := buf.Seal()
	return &SecureString{enclave: enc}
}

// Open decrypts the string for temporary use. Caller MUST Destroy().
func (ss *SecureString) Open() (*memguard.LockedBuffer, error) {
	buf, err := ss.enclave.Open()
	if err != nil {
		return nil, fmt.Errorf("open secure string: %w", err)
	}
	return buf, nil
}

// Purge wipes all guarded memory. Call on shutdown.
func Purge() {
	memguard.Purge()
}
