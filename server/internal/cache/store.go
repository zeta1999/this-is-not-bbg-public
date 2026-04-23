// Package cache provides a BBolt-backed time-series cache with TTL-based eviction.
package cache

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

var bucketNames = []string{"ohlc", "trades", "lob_snapshots", "news", "alerts"}

// Store is an embedded BBolt cache for time-series data.
type Store struct {
	db         *bolt.DB
	defaultTTL time.Duration
	mu         sync.RWMutex
	ttls       map[string]time.Duration // per-bucket TTL overrides
}

// Open creates or opens a BBolt database at the given path.
func Open(path string, defaultTTL time.Duration) (*Store, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}

	// Create buckets.
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range bucketNames {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("create bucket %s: %w", name, err)
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	return &Store{
		db:         db,
		defaultTTL: defaultTTL,
		ttls:       make(map[string]time.Duration),
	}, nil
}

// SetTTL sets a per-bucket TTL override.
func (s *Store) SetTTL(bucket string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttls[bucket] = ttl
}

func (s *Store) getTTL(bucket string) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ttl, ok := s.ttls[bucket]; ok {
		return ttl
	}
	return s.defaultTTL
}

// Put stores a value with a composite key in the given bucket.
// The key should encode the time-series identity (e.g., "binance/BTCUSD/1m/1700000000").
// Data is stored as raw bytes (typically serialized protobuf).
func (s *Store) Put(bucket, key string, data []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucket)
		}
		// Prefix with insertion timestamp for TTL eviction.
		record := make([]byte, 8+len(data))
		binary.BigEndian.PutUint64(record[:8], uint64(time.Now().UnixMilli()))
		copy(record[8:], data)
		return b.Put([]byte(key), record)
	})
}

// Get retrieves a value by bucket and key. Returns nil if not found or expired.
func (s *Store) Get(bucket, key string) ([]byte, error) {
	var result []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucket)
		}
		v := b.Get([]byte(key))
		if v == nil {
			return nil
		}
		if len(v) < 8 {
			return nil
		}
		insertedAt := time.UnixMilli(int64(binary.BigEndian.Uint64(v[:8])))
		if time.Since(insertedAt) > s.getTTL(bucket) {
			return nil // expired
		}
		result = make([]byte, len(v)-8)
		copy(result, v[8:])
		return nil
	})
	return result, err
}

// Scan iterates over all keys in a bucket with the given prefix.
// The callback receives the key (without prefix) and the raw data (without timestamp header).
// Expired entries are skipped.
func (s *Store) Scan(bucket, prefix string, fn func(key string, data []byte) error) error {
	ttl := s.getTTL(bucket)
	return s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucket)
		}
		c := b.Cursor()
		pfx := []byte(prefix)
		for k, v := c.Seek(pfx); k != nil; k, v = c.Next() {
			if len(k) < len(pfx) || string(k[:len(pfx)]) != prefix {
				break
			}
			if len(v) < 8 {
				continue
			}
			insertedAt := time.UnixMilli(int64(binary.BigEndian.Uint64(v[:8])))
			if time.Since(insertedAt) > ttl {
				continue
			}
			if err := fn(string(k), v[8:]); err != nil {
				return err
			}
		}
		return nil
	})
}

// Evict removes all entries older than their bucket's TTL.
// Call this periodically (e.g., hourly).
func (s *Store) Evict() error {
	for _, name := range bucketNames {
		ttl := s.getTTL(name)
		cutoff := time.Now().Add(-ttl).UnixMilli()

		err := s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(name))
			if b == nil {
				return nil
			}
			var toDelete [][]byte
			err := b.ForEach(func(k, v []byte) error {
				if len(v) < 8 {
					toDelete = append(toDelete, k)
					return nil
				}
				ts := int64(binary.BigEndian.Uint64(v[:8]))
				if ts < cutoff {
					toDelete = append(toDelete, k)
				}
				return nil
			})
			if err != nil {
				return err
			}
			for _, k := range toDelete {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
			if len(toDelete) > 0 {
				slog.Info("evicted entries", "bucket", name, "count", len(toDelete))
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("evict bucket %s: %w", name, err)
		}
	}
	return nil
}

// DBSizeBytes returns the current database file size in bytes.
func (s *Store) DBSizeBytes() int64 {
	var size int64
	_ = s.db.View(func(tx *bolt.Tx) error {
		size = tx.Size()
		return nil
	})
	return size
}

// DB returns the underlying BBolt database for direct access (e.g. auth token persistence).
func (s *Store) DB() *bolt.DB {
	return s.db
}

// Close closes the underlying BBolt database.
func (s *Store) Close() error {
	return s.db.Close()
}
