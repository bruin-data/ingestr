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

// attachedCatalog writes into a catalog ATTACHed to an in-memory DuckDB
// (e.g. DuckLake), which the ADBC bulk-append path cannot target directly.
type attachedCatalog struct {
	name  string
	alias string

	// writeMu serializes the staging sequence: it toggles the shared
	// connection's default catalog, so two writes must never interleave.
	writeMu sync.Mutex
	// stageSeq makes each staging table name unique, even for the same target
	// or targets whose names sanitize identically.
	stageSeq atomic.Uint64
}

// connectAttached opens an in-memory DuckDB and runs the bootstrap statements
// that attach the remote catalog; onErr may rewrite a statement's error.
func (d *DuckDBDestination) connectAttached(ctx context.Context, name string, stmts []string, onErr func(stmt string, err error) error) error {
	if err := d.Connect(ctx, "duckdb://:memory:"); err != nil {
		return fmt.Errorf("%s: bootstrap duckdb: %w", name, err)
	}

	tag := strings.ToUpper(name)
	for _, stmt := range stmts {
		config.Debug("[%s] %s", tag, srcduckdb.RedactSecret(stmt))
		if err := d.Exec(ctx, stmt); err != nil {
			if onErr != nil {
				return onErr(stmt, err)
			}
			return fmt.Errorf("%s: %w", name, srcduckdb.BootstrapError(stmt, err))
		}
	}
	return nil
}

// writeViaMemoryStage stages the Arrow stream in a regular in-memory DuckDB
// table via ADBC bulk-append, then copies it into the attached table with SQL.
func (a *attachedCatalog) writeViaMemoryStage(ctx context.Context, d *DuckDBDestination, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()

	target := opts.Table
	memName := fmt.Sprintf("%s_%d", memStageName(target, a.name), a.stageSeq.Add(1))
	memQualified := "memory.main." + memName

	// Restore and cleanup must survive ctx cancellation, or the connection stays
	// on `memory` and the stage table leaks.
	cleanupCtx := context.WithoutCancel(ctx)

	if err := d.exec(ctx, fmt.Sprintf(
		"CREATE OR REPLACE TABLE %s AS SELECT * FROM %s WHERE 1=0",
		memQualified, destination.QuoteTableName(target),
	)); err != nil {
		return fmt.Errorf("%s: create in-memory stage: %w", a.name, err)
	}
	defer func() { _ = d.exec(cleanupCtx, "DROP TABLE IF EXISTS "+memQualified) }()

	// DuckDB rejects the ADBC target_catalog option, so switch the default
	// catalog to `memory`, load the rows there, then switch back.
	if err := d.exec(ctx, "USE memory"); err != nil {
		return fmt.Errorf("%s: switch to memory catalog: %w", a.name, err)
	}
	restore := func() error { return d.exec(cleanupCtx, "USE "+a.alias) }

	memOpts := opts
	memOpts.Table = "main." + memName // schema-qualified only: no ADBC catalog option
	if err := d.Write(ctx, records, memOpts); err != nil {
		_ = restore()
		return fmt.Errorf("%s: load in-memory stage: %w", a.name, err)
	}
	if err := restore(); err != nil {
		return fmt.Errorf("%s: restore catalog: %w", a.name, err)
	}

	if err := d.exec(ctx, fmt.Sprintf(
		"INSERT INTO %s SELECT * FROM %s",
		destination.QuoteTableName(target), memQualified,
	)); err != nil {
		return fmt.Errorf("%s: copy stage into %s table: %w", a.name, a.name, err)
	}
	return nil
}

// memStageName derives a collision-safe in-memory staging table name from the
// (already unique) target table name.
func memStageName(target, name string) string {
	stage := duckTable(target).Table + "__" + name + "_memstage"
	var b strings.Builder
	b.Grow(len(stage))
	for _, r := range stage {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
