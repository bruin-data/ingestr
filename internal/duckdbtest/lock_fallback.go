//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package duckdbtest

import "os"

func lockADBCFile(_ *os.File) error {
	return nil
}

func unlockADBCFile(_ *os.File) error {
	return nil
}
