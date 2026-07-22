package duckdb

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	srcduckdb "github.com/bruin-data/ingestr/pkg/source/duckdb"
)

type DuckLakeDestination struct {
	*DuckDBDestination
}

func NewDuckLakeDestination() *DuckLakeDestination {
	return &DuckLakeDestination{DuckDBDestination: NewDuckDBDestination()}
}

func (d *DuckLakeDestination) Schemes() []string { return []string{"ducklake"} }
func (d *DuckLakeDestination) GetScheme() string { return "ducklake" }

// A local file lock cannot fence mutations on a shared ducklake catalog, so
// leasing is rejected the same way the MotherDuck path is.
func (d *DuckLakeDestination) AcquireManagedCDCRunLease(context.Context, string) (source.ConnectorLease, error) {
	return nil, fmt.Errorf("DuckLake does not support local managed CDC run leases")
}

func (d *DuckLakeDestination) Connect(ctx context.Context, uri string) error {
	cfg, err := srcduckdb.ParseLakehouseURI(uri)
	if err != nil {
		return fmt.Errorf("ducklake: %w", err)
	}

	if err := d.DuckDBDestination.Connect(ctx, "duckdb://:memory:"); err != nil {
		return fmt.Errorf("ducklake: bootstrap duckdb: %w", err)
	}

	stmts, err := srcduckdb.NewLakehouseAttacher().GenerateAttachStatements(cfg, srcduckdb.AttachAlias)
	if err != nil {
		return fmt.Errorf("ducklake: %w", err)
	}
	for _, stmt := range stmts {
		config.Debug("[DUCKLAKE] %s", redactIfSecret(stmt))
		if err := d.Exec(ctx, stmt); err != nil {
			if srcduckdb.IsStorageProbe(stmt) {
				return srcduckdb.TranslateProbeError(cfg.Storage.Path, err)
			}
			return fmt.Errorf("ducklake: bootstrap failed at %q: %w", srcduckdb.FirstLine(stmt), err)
		}
	}
	return nil
}

func redactIfSecret(sql string) string {
	if len(sql) >= 25 && sql[:25] == "CREATE OR REPLACE SECRET " {
		return "CREATE OR REPLACE SECRET (redacted)"
	}
	return sql
}
