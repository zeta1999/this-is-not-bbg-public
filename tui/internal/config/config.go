// Package config provides shared configuration with file-lock synchronization
// between concurrent TUI and CLI processes.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultPath returns the default config file path.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "notbbg", "config.yaml")
}

// UserConfig holds user preferences shared between TUI and CLI.
type UserConfig struct {
	Server    ServerSettings    `yaml:"server"`
	Panels    PanelSettings     `yaml:"panels"`
	Watchlist []string          `yaml:"watchlist"`
	Alerts    []AlertSetting    `yaml:"alerts"`
}

type ServerSettings struct {
	SocketPath     string `yaml:"socket_path"`
	LastServer     string `yaml:"last_server"` // cached LAN server address
	AutoStart      bool   `yaml:"auto_start"`
	CollectorAddr  string `yaml:"collector_addr,omitempty"`  // remote collector (e.g. "ajax:9473")
	CollectorToken string `yaml:"collector_token,omitempty"` // encrypted pairing token
}

type PanelSettings struct {
	ActivePanel int    `yaml:"active_panel"`
	Layout      string `yaml:"layout"` // saved layout tag
}

type AlertSetting struct {
	Type       string  `yaml:"type"`
	Instrument string  `yaml:"instrument"`
	Condition  string  `yaml:"condition"`
	Threshold  float64 `yaml:"threshold"`
}

// Load reads the config file with a shared (read) lock.
func Load(path string) (*UserConfig, error) {
	if path == "" {
		path = DefaultPath()
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	// Shared (read) lock.
	if err := lockShared(f); err != nil {
		return nil, fmt.Errorf("flock shared: %w", err)
	}
	defer unlock(f)

	cfg := &UserConfig{}
	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

// Save writes the config file with an exclusive (write) lock.
func Save(path string, cfg *UserConfig) error {
	if path == "" {
		path = DefaultPath()
	}

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("mkdir config: %w", err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("open config for write: %w", err)
	}
	defer f.Close()

	// Exclusive (write) lock.
	if err := lockExclusive(f); err != nil {
		return fmt.Errorf("flock exclusive: %w", err)
	}
	defer unlock(f)

	// Truncate and write.
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	return yaml.NewEncoder(f).Encode(cfg)
}

func defaultConfig() *UserConfig {
	return &UserConfig{
		Server: ServerSettings{
			AutoStart: true,
		},
		Watchlist: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"},
	}
}
