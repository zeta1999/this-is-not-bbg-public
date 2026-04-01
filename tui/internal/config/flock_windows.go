//go:build windows

package config

import "os"

// Windows: no flock available; use no-op locking.
// This is acceptable for single-user scenarios.

func lockShared(_ *os.File) error    { return nil }
func lockExclusive(_ *os.File) error { return nil }
func unlock(_ *os.File) error        { return nil }
