package auth

import (
	"testing"
	"time"
)

func TestPairAndValidate(t *testing.T) {
	m := NewManager()

	// Generate pairing token.
	pairingValue, err := m.GeneratePairingToken(RightRead|RightSubscribe, 5*time.Minute)
	if err != nil {
		t.Fatalf("generate pairing: %v", err)
	}

	// Pair.
	sessionID, rights, err := m.Pair(pairingValue, "test-client")
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	if rights != RightRead|RightSubscribe {
		t.Errorf("expected rights 0x03, got 0x%x", rights)
	}

	// Validate.
	validRights, err := m.Validate(sessionID)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validRights != rights {
		t.Errorf("rights mismatch")
	}

	// Pairing token can't be reused.
	_, _, err = m.Pair(pairingValue, "another-client")
	if err == nil {
		t.Fatal("expected error reusing pairing token")
	}
}

func TestExpiredPairingToken(t *testing.T) {
	m := NewManager()

	pairingValue, _ := m.GeneratePairingToken(RightRead, 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	_, _, err := m.Pair(pairingValue, "client")
	if err == nil {
		t.Fatal("expected error for expired pairing token")
	}
}

func TestRevokeSession(t *testing.T) {
	m := NewManager()
	pt, _ := m.GeneratePairingToken(RightAdmin, 5*time.Minute)
	sid, _, _ := m.Pair(pt, "admin")

	m.Revoke(sid)

	_, err := m.Validate(sid)
	if err == nil {
		t.Fatal("expected error for revoked session")
	}
}

func TestRefreshSession(t *testing.T) {
	m := NewManager()
	pt, _ := m.GeneratePairingToken(RightRead|RightSubscribe, 5*time.Minute)
	oldSID, _, _ := m.Pair(pt, "client")

	newSID, err := m.Refresh(oldSID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// Old session revoked.
	_, err = m.Validate(oldSID)
	if err == nil {
		t.Fatal("old session should be revoked")
	}

	// New session valid.
	_, err = m.Validate(newSID)
	if err != nil {
		t.Fatalf("new session invalid: %v", err)
	}
}
