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

// QuackDestination writes to a remote DuckDB server over the Quack protocol.
// SQL runs on the server via the attached catalog's query() macro; rows are
// bulk-appended into a local in-memory table and copied over with INSERT.
type QuackDestination struct {
	*DuckDBDestination

	writeMu  sync.Mutex
	stageSeq atomic.Uint64
}

func NewQuackDestination() *QuackDestination {
	return &QuackDestination{DuckDBDestination: NewDuckDBDestination()}
}

func (d *QuackDestination) Schemes() []string { return []string{"quack"} }
func (d *QuackDestination) GetScheme() string { return "quack" }

func (d *QuackDestination) AcquireManagedCDCRunLease(context.Context, string) (source.ConnectorLease, error) {
	return nil, fmt.Errorf("quack does not support local managed CDC run leases")
}

func (d *QuackDestination) ValidateManagedCDCState() error {
	return fmt.Errorf("quack does not expose a process-wide managed CDC run lease")
}

func (d *QuackDestination) Connect(ctx context.Context, uri string) error {
	cfg, err := srcduckdb.ParseQuackURI(uri)
	if err != nil {
		return err
	}

	if err := d.DuckDBDestination.Connect(ctx, "duckdb://:memory:"); err != nil {
		return fmt.Errorf("quack: bootstrap duckdb: %w", err)
	}

	for _, stmt := range srcduckdb.QuackBootstrapStatements(cfg, srcduckdb.QuackAttachAlias) {
		if !strings.HasPrefix(stmt, "ATTACH") {
			config.Debug("[QUACK] %s", stmt)
		}
		if err := d.execLocal(ctx, stmt); err != nil {
			_ = d.Close(ctx)
			return srcduckdb.WrapQuackBootstrapError(cfg, stmt, err)
		}
	}

	d.rewriteSQL = func(query string, args []any) string {
		return srcduckdb.QuackRemoteSQL(srcduckdb.QuackAttachAlias, query, args...)
	}
	if err := d.exec(ctx, "SET TimeZone='UTC'"); err != nil {
		_ = d.Close(ctx)
		return fmt.Errorf("quack: failed to set server session timezone to UTC: %w", err)
	}
	return nil
}

// The attachment exposes only the server's default database, so the bulk copy
// can't reach tables in other databases.
func (d *QuackDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := checkQuackTable(opts.Table); err != nil {
		return err
	}
	return d.DuckDBDestination.PrepareTable(ctx, opts)
}

func checkQuackTable(table string) error {
	if tn := duckTable(table); tn.Catalog != "" {
		return fmt.Errorf("quack: destination table %q must be schema.table; database-qualified names are not supported", table)
	}
	return nil
}

func (d *QuackDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeViaMemoryStage(ctx, records, opts)
}

func (d *QuackDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeViaMemoryStage(ctx, records, opts)
}

func (d *QuackDestination) writeViaMemoryStage(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := checkQuackTable(opts.Table); err != nil {
		return err
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	tn := duckTable(opts.Table)
	remoteTarget := srcduckdb.QuackAttachAlias + "." + quoteIdentifier(canonicalDuckDBSchema(tn.Schema)) + "." + quoteIdentifier(tn.Table)
	memName := fmt.Sprintf("%s_%d", duckLakeMemStageName(opts.Table), d.stageSeq.Add(1))
	memQualified := "memory.main." + quoteIdentifier(memName)

	// The target was created through query(), so refresh the client's catalog cache first.
	if err := d.execLocal(ctx, "FROM quack_clear_cache()"); err != nil {
		return fmt.Errorf("quack: clear catalog cache: %w", err)
	}
	if err := d.execLocal(ctx, fmt.Sprintf(
		"CREATE OR REPLACE TABLE %s AS SELECT * FROM %s WHERE 1=0", memQualified, remoteTarget,
	)); err != nil {
		return fmt.Errorf("quack: create in-memory stage: %w", err)
	}
	defer func() { _ = d.execLocal(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+memQualified) }()

	memOpts := opts
	memOpts.Table = "main." + quoteIdentifier(memName)
	if err := d.DuckDBDestination.Write(ctx, records, memOpts); err != nil {
		return fmt.Errorf("quack: load in-memory stage: %w", err)
	}

	if err := d.execLocal(ctx, fmt.Sprintf("INSERT INTO %s SELECT * FROM %s", remoteTarget, memQualified)); err != nil {
		return fmt.Errorf("quack: copy stage to server: %w", err)
	}
	return nil
}
