//go:build !windows

package config

import (
	"os"
	"syscall"
)

func lockShared(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_SH)
}

func lockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
