//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package duckdbtest

import (
	"os"
	"syscall"
)

func lockADBCFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockADBCFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
