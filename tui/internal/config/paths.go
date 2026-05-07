package config

import (
	"os"
	"path/filepath"
	"sync"
)

// HomeOverride, when non-empty, is used by ResolveHome as the notbbg
// root directory. Set once from the CLI's --home flag; reading is
// lock-free via the sync.Once initializer in ResolveHome.
var (
	homeOverride string
	homeOnce     sync.Once
	resolvedHome string
)

// SetHomeOverride sets the notbbg root directory override. Should be
// called before any ResolveHome lookup (i.e. at CLI bootstrap, before
// commands fan out).
func SetHomeOverride(path string) { homeOverride = path }

// ResolveHome returns the notbbg root. Resolution order:
//  1. Explicit override set via SetHomeOverride.
//  2. $XDG_CONFIG_HOME/notbbg if set.
//  3. $HOME/.config/notbbg.
// The result is memoized — unset or override changes after the first
// call are ignored.
func ResolveHome() string {
	homeOnce.Do(func() {
		if homeOverride != "" {
			resolvedHome = homeOverride
			return
		}
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			resolvedHome = filepath.Join(x, "notbbg")
			return
		}
		home, _ := os.UserHomeDir()
		resolvedHome = filepath.Join(home, ".config", "notbbg")
	})
	return resolvedHome
}

// Plugins returns the plugin directory under the resolved root.
func Plugins() string { return filepath.Join(ResolveHome(), "plugins") }
