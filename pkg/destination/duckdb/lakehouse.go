package duckdb

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
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
