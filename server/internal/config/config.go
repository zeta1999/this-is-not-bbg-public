// Package config handles server configuration loading and validation.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig    `yaml:"server"`
	Feeds    FeedsConfig     `yaml:"feeds"`
	Cache    CacheConfig     `yaml:"cache"`
	Alerts   AlertsConfig    `yaml:"alerts"`
	Cron     []CronJobConfig `yaml:"cron,omitempty"`
	Datalake DatalakeConfig  `yaml:"datalake,omitempty"`
}

type DatalakeConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Path     string   `yaml:"path"`     // root directory
	Topics   []string `yaml:"topics"`   // glob patterns (default: all)
	Format   string   `yaml:"format"`   // "jsonl" (default)
	Rotation string   `yaml:"rotation"` // "daily" (default) or "hourly"
}

type ServerConfig struct {
	UnixSocket string `yaml:"unix_socket"` // default: $XDG_RUNTIME_DIR/notbbg.sock
	TCPAddr    string `yaml:"tcp_addr"`    // e.g. ":9473"
	EnableTCP  bool   `yaml:"enable_tcp"`
	HTTPAddr   string `yaml:"http_addr"`   // e.g. ":9474"
	EnableHTTP bool   `yaml:"enable_http"` // HTTP/SSE gateway for phone/web clients
}

type FeedsConfig struct {
	Exchanges   []ExchangeConfig `yaml:"exchanges"`
	DEX         DEXConfig        `yaml:"dex"`
	World       WorldFeedsConfig `yaml:"world"`
	RSS         RSSConfig        `yaml:"rss"`
	Sibelius    FileFeedConfig   `yaml:"sibelius,omitempty"`
	Ravel       FileFeedConfig   `yaml:"ravel,omitempty"`
	TSBaseFiles FileFeedConfig   `yaml:"tsbase_files,omitempty"`
}

// FileFeedConfig is the shared shape for directory-watching adapters
// (Sibelius JSON, Ravel JSON/XLSX, ts-base CSV/JSONL).
type FileFeedConfig struct {
	Enabled      bool          `yaml:"enabled"`
	Path         string        `yaml:"path"`
	PollInterval time.Duration `yaml:"poll_interval"`
	TopicPrefix  string        `yaml:"topic_prefix,omitempty"`
}

type DEXConfig struct {
	Uniswap      FeedSourceConfig `yaml:"uniswap"`
	Raydium      FeedSourceConfig `yaml:"raydium"`
	Jupiter      FeedSourceConfig `yaml:"jupiter"`
	Hyperliquid  FeedSourceConfig `yaml:"hyperliquid"`
	GMX          FeedSourceConfig `yaml:"gmx"`
	DYDX         FeedSourceConfig `yaml:"dydx"`
	Drift        FeedSourceConfig `yaml:"drift"`
	Serum        FeedSourceConfig `yaml:"serum"`
	Orca         FeedSourceConfig `yaml:"orca"`
	PancakeSwap  FeedSourceConfig `yaml:"pancakeswap"`
	Curve        FeedSourceConfig `yaml:"curve"`
}

type ExchangeConfig struct {
	Name        string        `yaml:"name"`    // e.g. "binance"
	Enabled     bool          `yaml:"enabled"`
	Symbols     []string      `yaml:"symbols"` // e.g. ["BTCUSD", "ETHUSD"]
	FeedTypes   []string      `yaml:"feed_types"` // "ohlc", "trades", "orderbook"
	RateLimit   int           `yaml:"rate_limit"`  // requests per second
	WSEndpoint  string        `yaml:"ws_endpoint"`
	RESTBase    string        `yaml:"rest_base"`
	BackfillDays int          `yaml:"backfill_days"` // historical OHLC backfill
	Timeframes  []string      `yaml:"timeframes"`    // e.g. ["1m", "5m", "1h", "1d"]
}

type WorldFeedsConfig struct {
	YahooFinance  FeedSourceConfig `yaml:"yahoo_finance"`
	CoinGecko     FeedSourceConfig `yaml:"coingecko"`
	FRED          FeedSourceConfig `yaml:"fred"`
	EIA           FeedSourceConfig `yaml:"eia"`
	AlternativeMe FeedSourceConfig `yaml:"alternative_me"`
	MempoolSpace  FeedSourceConfig `yaml:"mempool_space"`
}

type FeedSourceConfig struct {
	Enabled      bool          `yaml:"enabled"`
	PollInterval time.Duration `yaml:"poll_interval"`
	CacheTTL     time.Duration `yaml:"cache_ttl"`
	APIKey       string        `yaml:"api_key,omitempty"` // will move to encrypted config in Phase 5
	Symbols      []string      `yaml:"symbols,omitempty"` // optional symbol list (e.g. Yahoo Finance tickers)
}

type RSSConfig struct {
	Enabled      bool          `yaml:"enabled"`
	PollInterval time.Duration `yaml:"poll_interval"`
	Feeds        []string      `yaml:"feeds"` // RSS feed URLs
}

type CacheConfig struct {
	DBPath           string        `yaml:"db_path"`            // BBolt database file
	EvictionInterval time.Duration `yaml:"eviction_interval"`
	DefaultTTL       time.Duration `yaml:"default_ttl"`
	MaxSizeMB        int           `yaml:"max_size_mb"`        // max DB size in MB (0 = unlimited)
}

type CronJobConfig struct {
	Name     string   `yaml:"name"`
	Schedule string   `yaml:"schedule"` // "@every 5m" or "*/5 * * * *"
	Action   string   `yaml:"action"`   // "cache_evict", "export_data", etc.
	Args     []string `yaml:"args,omitempty"`
	Enabled  bool     `yaml:"enabled"`
}

type AlertsConfig struct {
	ConsistencyThreshold float64       `yaml:"consistency_threshold"` // cross-exchange divergence %
	CheckInterval        time.Duration `yaml:"check_interval"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.UnixSocket == "" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			c.Server.UnixSocket = xdg + "/notbbg.sock"
		} else {
			c.Server.UnixSocket = "/tmp/notbbg.sock"
		}
	}
	if c.Server.TCPAddr == "" {
		c.Server.TCPAddr = ":9473"
	}
	if c.Cache.DBPath == "" {
		c.Cache.DBPath = "notbbg.db"
	}
	if c.Cache.EvictionInterval == 0 {
		c.Cache.EvictionInterval = time.Hour
	}
	if c.Cache.DefaultTTL == 0 {
		c.Cache.DefaultTTL = 24 * time.Hour
	}
	if c.Alerts.ConsistencyThreshold == 0 {
		c.Alerts.ConsistencyThreshold = 0.5
	}
	if c.Alerts.CheckInterval == 0 {
		c.Alerts.CheckInterval = 30 * time.Second
	}
}

func (c *Config) validate() error {
	// Collector configs have no feeds — only datalake. Skip feed validation.
	if c.Datalake.Enabled {
		return nil
	}
	if len(c.Feeds.Exchanges) == 0 && !c.Feeds.World.YahooFinance.Enabled &&
		!c.Feeds.World.CoinGecko.Enabled && !c.Feeds.RSS.Enabled &&
		!c.Feeds.Sibelius.Enabled && !c.Feeds.Ravel.Enabled && !c.Feeds.TSBaseFiles.Enabled {
		return fmt.Errorf("at least one feed source must be configured")
	}
	return nil
}
