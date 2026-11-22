package adbc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	dbc "github.com/columnar-tech/dbc"
	dbcconfig "github.com/columnar-tech/dbc/config"
	"github.com/gofrs/flock"
)

var dbcInstallMu sync.Mutex

// InstallDriver installs an ADBC driver using the native dbc client library.
// A file lock serialises installs across processes; isLoaded is re-checked under it.
func InstallDriver(ctx context.Context, driverName string, isLoaded func() bool) error {
	dbcInstallMu.Lock()
	defer dbcInstallMu.Unlock()

	cfg := dbcInstallConfig()
	if cfg.Err != nil {
		return fmt.Errorf("failed to load dbc config: %w", cfg.Err)
	}

	loc, err := dbcconfig.EnsureLocation(cfg)
	if err != nil {
		return fmt.Errorf("failed to prepare ADBC driver directory: %w", err)
	}

	lock := flock.New(filepath.Join(loc, "."+driverName+".install.lock"))
	if _, err := lock.TryLockContext(ctx, 200*time.Millisecond); err != nil {
		return fmt.Errorf("failed to acquire %s driver install lock: %w", driverName, err)
	}
	defer func() { _ = lock.Unlock() }()

	if isLoaded() {
		config.Debug("[ADBC] %s driver was installed by another process", driverName)
		return nil
	}

	client, err := dbc.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create dbc client: %w", err)
	}

	config.Debug("[ADBC] Installing %s driver via dbc client...", driverName)
	if _, err := client.Install(ctx, cfg, driverName); err != nil {
		return fmt.Errorf("failed to install %s ADBC driver: %w", driverName, err)
	}

	return nil
}

func dbcInstallConfig() dbcconfig.Config {
	configs := dbcconfig.Get()
	// dbc's env-level location also picks up $VIRTUAL_ENV and $CONDA_PREFIX,
	// but the ADBC driver manager only searches ADBC_DRIVER_PATH and the
	// user/system config dirs — so only honor env-level when ADBC_DRIVER_PATH
	// is explicitly set.
	if os.Getenv("ADBC_DRIVER_PATH") != "" {
		if cfg := configs[dbcconfig.ConfigEnv]; cfg.Location != "" {
			return cfg
		}
	}
	return configs[dbcconfig.ConfigUser]
}
