// Package paths centralizes notbbg's filesystem-layout rules.
//
// Resolution order for the root notbbg directory:
//
//  1. Explicit override (e.g. the --home CLI flag).
//  2. $XDG_CONFIG_HOME/notbbg if set.
//  3. $HOME/.config/notbbg (including macOS, per the 2026-04-24
//     decision to favour Linux-convention parity over
//     ~/Library/Application Support).
//
// Sub-paths (certs, plugins, cache db, datalake) all hang off the
// resolved root so a single --home flag moves the whole tree.
package paths

import (
	"os"
	"path/filepath"
)

// ResolveHome returns the notbbg root directory. See the package
// comment for the resolution order. An empty override falls through
// to XDG + home.
func ResolveHome(override string) string {
	if override != "" {
		return override
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "notbbg")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "notbbg")
}

// Plugins returns the plugin directory under the resolved root.
func Plugins(home string) string { return filepath.Join(home, "plugins") }

// Certs returns the TLS certificate directory under the resolved root.
func Certs(home string) string { return filepath.Join(home, "certs") }

// ConfigFile returns the default server config path under the resolved root.
func ConfigFile(home string) string { return filepath.Join(home, "config.yaml") }
