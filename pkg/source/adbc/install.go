package adbc

import (
	"context"
	"fmt"
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	dbc "github.com/columnar-tech/dbc"
	dbcconfig "github.com/columnar-tech/dbc/config"
)

var dbcInstallMu sync.Mutex

// InstallDriver installs an ADBC driver using the native dbc client library.
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
	if cfg := configs[dbcconfig.ConfigEnv]; cfg.Location != "" {
		return cfg
	}
	return configs[dbcconfig.ConfigUser]
}
