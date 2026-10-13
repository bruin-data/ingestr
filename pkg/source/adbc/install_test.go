package adbc

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestInstallDriverSkipsWhenLoadedUnderLock(t *testing.T) {
	t.Setenv("ADBC_DRIVER_PATH", t.TempDir())

	calls := 0
	err := InstallDriver(context.Background(), "snowflake", func() bool {
		calls++
		return true
	})
	if err != nil {
		t.Fatalf("InstallDriver() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("isLoaded called %d times, want 1", calls)
	}
}

func TestInstallDriverWaitsForInstallLockThenSkips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ADBC_DRIVER_PATH", dir)

	held := flock.New(filepath.Join(dir, ".snowflake.install.lock"))
	if err := held.Lock(); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- InstallDriver(context.Background(), "snowflake", func() bool {
			calls.Add(1)
			return true
		})
	}()

	select {
	case err := <-done:
		t.Fatalf("InstallDriver() returned %v while another holder owned the install lock", err)
	case <-time.After(500 * time.Millisecond):
	}
	if calls.Load() != 0 {
		t.Fatal("isLoaded called before the install lock was acquired")
	}

	if err := held.Unlock(); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("InstallDriver() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("InstallDriver() did not continue after the install lock was released")
	}
	if calls.Load() != 1 {
		t.Fatalf("isLoaded called %d times, want 1", calls.Load())
	}
}
