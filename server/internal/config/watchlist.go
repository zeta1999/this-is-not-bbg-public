package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Watchlist is a personal company-tracking layer that sits on top of
// the main server config. It exists so that ../propaganda/investment
// research notes (or any external watchlist) can be folded into the
// running server's yahoo + rss feed lists without rewriting the main
// config or leaking trading activity into a tracked file.
//
// Loading is best-effort: a missing watchlist file is not an error;
// the server simply runs with whatever was already in the main
// config. When the file is present, MergeInto appends each company's
// yahoo symbol to FeedsConfig.World.YahooFinance.Symbols and each
// news_feed URL to FeedsConfig.RSS.Feeds, deduping both lists.
type Watchlist struct {
	Companies []WatchCompany `yaml:"companies"`
}

// WatchCompany is one tracked company. `yahoo` should be the ticker
// in Yahoo Finance's exchange-suffix form ("ETN", "6324.T",
// "035420.KS", "9880.HK", "MTRS.ST", "NG.L", "688256.SS", …);
// `ticker` is the bare symbol kept around for human-readable refs.
type WatchCompany struct {
	Name      string   `yaml:"name"`
	Ticker    string   `yaml:"ticker"`
	Yahoo     string   `yaml:"yahoo"`
	Theme     string   `yaml:"theme,omitempty"`
	NewsFeeds []string `yaml:"news_feeds,omitempty"`
	Notes     string   `yaml:"notes,omitempty"`
}

// LoadWatchlist reads a watchlist YAML if it exists. Returns
// (nil, nil) on a missing file so callers can tolerate absence.
func LoadWatchlist(path string) (*Watchlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read watchlist: %w", err)
	}
	var w Watchlist
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("parse watchlist: %w", err)
	}
	return &w, nil
}

// MergeInto folds the watchlist into the main config. Adds each
// non-empty yahoo symbol to YahooFinance.Symbols and each news feed
// URL to RSS.Feeds, then dedupes both lists in place. Enables the
// yahoo + rss adapters if they were off — otherwise the watchlist
// silently does nothing on a CEX-only config and that surprises the
// trader who just put 27 tickers in their watchlist.
func (w *Watchlist) MergeInto(c *Config) {
	if w == nil || c == nil {
		return
	}
	for _, co := range w.Companies {
		if co.Yahoo != "" {
			c.Feeds.World.YahooFinance.Symbols = append(c.Feeds.World.YahooFinance.Symbols, co.Yahoo)
		}
		for _, f := range co.NewsFeeds {
			if f != "" {
				c.Feeds.RSS.Feeds = append(c.Feeds.RSS.Feeds, f)
			}
		}
	}
	c.Feeds.World.YahooFinance.Symbols = dedupeStrings(c.Feeds.World.YahooFinance.Symbols)
	c.Feeds.RSS.Feeds = dedupeStrings(c.Feeds.RSS.Feeds)
	if len(c.Feeds.World.YahooFinance.Symbols) > 0 {
		c.Feeds.World.YahooFinance.Enabled = true
	}
	if len(c.Feeds.RSS.Feeds) > 0 {
		c.Feeds.RSS.Enabled = true
	}
}

// WatchlistPath returns the conventional watchlist location next to
// a given config file: same directory, name "watchlist.yaml".
func WatchlistPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "watchlist.yaml")
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
