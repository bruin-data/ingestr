package adbc

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	dbc "github.com/columnar-tech/dbc"
	dbcconfig "github.com/columnar-tech/dbc/config"
)

var dbcInstallMu sync.Mutex

func InstallDriver(ctx context.Context, driverName string) error {
	dbcInstallMu.Lock()
	defer dbcInstallMu.Unlock()

	cfg := dbcInstallConfig()
	if cfg.Err != nil {
		return fmt.Errorf("failed to load dbc config: %w", cfg.Err)
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
