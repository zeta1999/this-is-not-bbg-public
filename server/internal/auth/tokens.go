// Package auth implements token-based authentication with pairing and session management.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Rights bitfield for client permissions.
const (
	RightRead      uint32 = 0x01
	RightSubscribe uint32 = 0x02
	RightConfigure uint32 = 0x04
	RightAdmin     uint32 = 0x08
)

// Token represents an authentication token.
type Token struct {
	ID        string
	ClientName string
	Rights    uint32
	IssuedAt  time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// PairingToken is a one-time token for initial client pairing.
type PairingToken struct {
	Value     string
	Rights    uint32
	ExpiresAt time.Time
	Used      bool
}

// Manager handles token lifecycle: creation, validation, pairing, and revocation.
type Manager struct {
	mu            sync.RWMutex
	sessions      map[string]*Token        // session token ID -> Token
	pairingTokens map[string]*PairingToken // token value -> PairingToken
}

// NewManager creates a new auth manager.
func NewManager() *Manager {
	return &Manager{
		sessions:      make(map[string]*Token),
		pairingTokens: make(map[string]*PairingToken),
	}
}

// SeedToken registers a known pairing token value (e.g. from NOTBBG_TOKEN env var).
func (m *Manager) SeedToken(value string, rights uint32, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pairingTokens[value] = &PairingToken{
		Value:     value,
		Rights:    rights,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// GeneratePairingToken creates a one-time pairing token with the given rights and TTL.
func (m *Manager) GeneratePairingToken(rights uint32, ttl time.Duration) (string, error) {
	if ttl == 0 {
		ttl = 5 * time.Minute
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	value := hex.EncodeToString(b)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.pairingTokens[value] = &PairingToken{
		Value:     value,
		Rights:    rights,
		ExpiresAt: time.Now().Add(ttl),
	}

	return value, nil
}

// Pair validates a pairing token and creates a session token.
// Returns the session token ID and rights.
func (m *Manager) Pair(pairingValue, clientName string) (string, uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pt, ok := m.pairingTokens[pairingValue]
	if !ok {
		return "", 0, fmt.Errorf("invalid pairing token")
	}
	if pt.Used {
		return "", 0, fmt.Errorf("pairing token already used")
	}
	if time.Now().After(pt.ExpiresAt) {
		return "", 0, fmt.Errorf("pairing token expired")
	}

	pt.Used = true

	// Create session token.
	sessionID, err := generateTokenID()
	if err != nil {
		return "", 0, err
	}

	m.sessions[sessionID] = &Token{
		ID:         sessionID,
		ClientName: clientName,
		Rights:     pt.Rights,
		IssuedAt:   time.Now(),
		ExpiresAt:  time.Now().Add(30 * 24 * time.Hour), // 30 days
	}

	return sessionID, pt.Rights, nil
}

// Validate checks if a session token is valid and returns its rights.
func (m *Manager) Validate(sessionID string) (uint32, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	token, ok := m.sessions[sessionID]
	if !ok {
		return 0, fmt.Errorf("unknown session token")
	}
	if token.Revoked {
		return 0, fmt.Errorf("session token revoked")
	}
	if time.Now().After(token.ExpiresAt) {
		return 0, fmt.Errorf("session token expired")
	}

	return token.Rights, nil
}

// Revoke invalidates a session token.
func (m *Manager) Revoke(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if token, ok := m.sessions[sessionID]; ok {
		token.Revoked = true
	}
}

// Refresh extends a session token's expiry if still valid.
func (m *Manager) Refresh(sessionID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	token, ok := m.sessions[sessionID]
	if !ok {
		return "", fmt.Errorf("unknown session token")
	}
	if token.Revoked || time.Now().After(token.ExpiresAt) {
		return "", fmt.Errorf("token invalid")
	}

	// Create new session, revoke old.
	token.Revoked = true

	newID, err := generateTokenID()
	if err != nil {
		return "", err
	}

	m.sessions[newID] = &Token{
		ID:         newID,
		ClientName: token.ClientName,
		Rights:     token.Rights,
		IssuedAt:   time.Now(),
		ExpiresAt:  time.Now().Add(30 * 24 * time.Hour),
	}

	return newID, nil
}

// CleanExpired removes expired tokens.
func (m *Manager) CleanExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for k, pt := range m.pairingTokens {
		if now.After(pt.ExpiresAt) {
			delete(m.pairingTokens, k)
		}
	}
	for k, t := range m.sessions {
		if t.Revoked && now.After(t.ExpiresAt.Add(24*time.Hour)) {
			delete(m.sessions, k)
		}
	}
}

// GenerateTokenID creates a random 64-char hex token ID.
func GenerateTokenID() (string, error) {
	return generateTokenID()
}

func generateTokenID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
