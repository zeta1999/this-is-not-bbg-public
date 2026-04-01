package auth

import (
	"encoding/json"
	"log/slog"

	bolt "go.etcd.io/bbolt"
)

const tokenBucket = "auth_sessions"

// PersistTo saves all session tokens to a BBolt database.
func (m *Manager) PersistTo(db *bolt.DB) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(tokenBucket))
		if err != nil {
			return err
		}

		for id, token := range m.sessions {
			if token.Revoked {
				continue
			}
			data, err := json.Marshal(token)
			if err != nil {
				continue
			}
			b.Put([]byte(id), data)
		}
		return nil
	})
}

// LoadFrom restores session tokens from a BBolt database.
func (m *Manager) LoadFrom(db *bolt.DB) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(tokenBucket))
		if b == nil {
			return nil // no tokens saved yet
		}

		loaded := 0
		return b.ForEach(func(k, v []byte) error {
			var token Token
			if err := json.Unmarshal(v, &token); err != nil {
				return nil // skip corrupt entries
			}
			m.sessions[string(k)] = &token
			loaded++
			return nil
		})
	})
}

// SetupPersistence loads tokens from db and registers a cleanup/save function.
func (m *Manager) SetupPersistence(db *bolt.DB) {
	if err := m.LoadFrom(db); err != nil {
		slog.Warn("failed to load tokens from db", "error", err)
	} else {
		count := 0
		m.mu.RLock()
		count = len(m.sessions)
		m.mu.RUnlock()
		if count > 0 {
			slog.Info("restored session tokens from db", "count", count)
		}
	}
}

// SaveTokens persists current sessions to the database.
func (m *Manager) SaveTokens(db *bolt.DB) {
	if err := m.PersistTo(db); err != nil {
		slog.Warn("failed to persist tokens", "error", err)
	}
}
