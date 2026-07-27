package duckdb

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	srcduckdb "github.com/bruin-data/ingestr/pkg/source/duckdb"
)

type DuckLakeDestination struct {
	*DuckDBDestination

	// writeMu serializes the whole create/load/copy/drop staging sequence. The
	// sequence toggles the shared connection's default catalog (USE memory /
	// USE ducklake_catalog), so two DuckLake writes must never interleave.
	writeMu sync.Mutex
	// stageSeq makes each staging table name unique, even for the same target
	// or targets whose names sanitize identically.
	stageSeq atomic.Uint64
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

// Write and WriteParallel stage the Arrow stream in a regular in-memory DuckDB
// table, then copy it into the DuckLake table with a SQL INSERT ... SELECT.
//
// The embedded DuckDBDestination writes via ADBC bulk-append (the DuckDB
// appender), which cannot target tables in a DuckLake catalog. SQL writes
// into DuckLake tables work,so we ADBC-load into a plain in-memory table
// and then hand the rows to DuckLake via SQL.
func (d *DuckLakeDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeViaMemoryStage(ctx, records, opts)
}

func (d *DuckLakeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeViaMemoryStage(ctx, records, opts)
}

func (d *DuckLakeDestination) writeViaMemoryStage(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	// The sequence below toggles the shared connection's default catalog, so it
	// must run start-to-finish without another DuckLake write interleaving.
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	target := opts.Table
	memName := fmt.Sprintf("%s_%d", duckLakeMemStageName(target), d.stageSeq.Add(1))
	memQualified := "memory.main." + memName

	// Restore and cleanup must run even if ctx is cancelled mid-write, otherwise
	// the connection can stay pointed at `memory` and the stage table leaks for
	// the connection's lifetime.
	cleanupCtx := context.WithoutCancel(ctx)

	// Mirror the DuckLake table's schema into a plain in-memory table.
	if err := d.exec(ctx, fmt.Sprintf(
		"CREATE OR REPLACE TABLE %s AS SELECT * FROM %s WHERE 1=0",
		memQualified, destination.QuoteTableName(target),
	)); err != nil {
		return fmt.Errorf("ducklake: create in-memory stage: %w", err)
	}
	defer func() { _ = d.exec(cleanupCtx, "DROP TABLE IF EXISTS "+memQualified) }()

	// ADBC bulk-append cannot target a DuckLake table, and DuckDB rejects the
	// ADBC target_catalog option — so switch the default catalog to `memory`,
	// load the rows there via the embedded ADBC path, then switch back.
	if err := d.exec(ctx, "USE memory"); err != nil {
		return fmt.Errorf("ducklake: switch to memory catalog: %w", err)
	}
	restore := func() error { return d.exec(cleanupCtx, "USE "+srcduckdb.AttachAlias) }

	memOpts := opts
	memOpts.Table = "main." + memName // schema-qualified only: no ADBC catalog option
	if err := d.DuckDBDestination.Write(ctx, records, memOpts); err != nil {
		_ = restore()
		return fmt.Errorf("ducklake: load in-memory stage: %w", err)
	}
	if err := restore(); err != nil {
		return fmt.Errorf("ducklake: restore catalog: %w", err)
	}

	if err := d.exec(ctx, fmt.Sprintf(
		"INSERT INTO %s SELECT * FROM %s",
		destination.QuoteTableName(target), memQualified,
	)); err != nil {
		return fmt.Errorf("ducklake: copy stage into ducklake table: %w", err)
	}
	return nil
}

// duckLakeMemStageName derives a collision-safe in-memory staging table name
// from the (already unique) target table name.
func duckLakeMemStageName(target string) string {
	name := duckTable(target).Table + "__ducklake_memstage"
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
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
