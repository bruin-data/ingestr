package duckdbtest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var adbcLockMu sync.Mutex

func LockADBC(t *testing.T) {
	t.Helper()

	adbcLockMu.Lock()

	lockPath := filepath.Join(os.TempDir(), "ingestr-duckdb-adbc.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		adbcLockMu.Unlock()
		t.Fatalf("open DuckDB ADBC test lock: %v", err)
	}

	if err := lockADBCFile(lockFile); err != nil {
		_ = lockFile.Close()
		adbcLockMu.Unlock()
		t.Fatalf("lock DuckDB ADBC test lock: %v", err)
	}

	t.Cleanup(func() {
		if err := unlockADBCFile(lockFile); err != nil {
			t.Errorf("unlock DuckDB ADBC test lock: %v", err)
		}
		if err := lockFile.Close(); err != nil {
			t.Errorf("close DuckDB ADBC test lock: %v", err)
		}
		adbcLockMu.Unlock()
	})
}
