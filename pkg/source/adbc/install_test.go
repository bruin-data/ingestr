package adbc

import (
	"context"
	"path/filepath"
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

func TestInstallDriverWaitsForInstallLock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ADBC_DRIVER_PATH", dir)

	held := flock.New(filepath.Join(dir, ".snowflake.install.lock"))
	if err := held.Lock(); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	defer func() { _ = held.Unlock() }()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	called := false
	err := InstallDriver(ctx, "snowflake", func() bool {
		called = true
		return true
	})
	if err == nil {
		t.Fatal("InstallDriver() succeeded while another holder owned the install lock")
	}
	if called {
		t.Fatal("isLoaded called before the install lock was acquired")
	}
}
