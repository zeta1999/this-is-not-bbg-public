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
	GUI       GUISettings       `yaml:"gui"`
	Watchlist []string          `yaml:"watchlist"`
	Alerts    []AlertSetting    `yaml:"alerts"`
}

// GUISettings groups client-side rendering knobs that apply to both
// the TUI and the desktop app. See DATA-PLAN.md §3.
type GUISettings struct {
	Cache GUICacheSettings `yaml:"cache"`
}

// GUICacheSettings caps per-panel client memory so that switching
// instruments or timeframes never grows the panel without bound.
// Values chosen to keep the whole GUI working set under a few MiB
// for typical use.
type GUICacheSettings struct {
	// OHLCRowsPerInstrument caps the OHLC rows retained per
	// (instrument, timeframe) key. Excess rows are evicted in
	// timestamp order (oldest first).
	OHLCRowsPerInstrument int `yaml:"ohlc_rows_per_instrument"`
	// LOBDepthLevels caps the number of levels shown on each side of
	// the book.
	LOBDepthLevels int `yaml:"lob_depth_levels"`
	// TradesRing caps the trade tape buffer per instrument (ring).
	TradesRing int `yaml:"trades_ring"`
}

// Defaults for GUICacheSettings used when the user hasn't specified
// a value in config.yaml.
const (
	DefaultOHLCRowsPerInstrument = 10000
	DefaultLOBDepthLevels        = 50
	DefaultTradesRing            = 2000
)

// WithDefaults fills in any zero fields with built-in defaults.
func (g GUICacheSettings) WithDefaults() GUICacheSettings {
	if g.OHLCRowsPerInstrument <= 0 {
		g.OHLCRowsPerInstrument = DefaultOHLCRowsPerInstrument
	}
	if g.LOBDepthLevels <= 0 {
		g.LOBDepthLevels = DefaultLOBDepthLevels
	}
	if g.TradesRing <= 0 {
		g.TradesRing = DefaultTradesRing
	}
	return g
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
	defer func() { _ = unlock(f) }()

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
	defer func() { _ = unlock(f) }()

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
		GUI: GUISettings{
			Cache: GUICacheSettings{}.WithDefaults(),
		},
		Watchlist: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"},
	}
}
