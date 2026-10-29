package duckdb

import (
	"context"
	"fmt"
	"sync"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	srcduckdb "github.com/bruin-data/ingestr/pkg/source/duckdb"
)

type DuckLakeDestination struct {
	*DuckDBDestination
	catalog attachedCatalog

	// layouts tracks prepared table layouts for swap replay.
	layouts        map[string]duckLakeTableLayout
	pendingLayouts map[string]struct{}
	layoutsMu      sync.Mutex
}

func NewDuckLakeDestination() *DuckLakeDestination {
	d := &DuckLakeDestination{
		DuckDBDestination: NewDuckDBDestination(),
		catalog:           attachedCatalog{name: "ducklake", alias: srcduckdb.AttachAlias},
	}
	d.onTargetRecreated = d.reapplyLayoutOnSwap
	d.onSchemaEvolvedLocked = d.execPendingLayoutLocked
	d.onStagingConsumed = d.forgetLayout
	return d
}

func (d *DuckLakeDestination) Schemes() []string { return []string{"ducklake"} }
func (d *DuckLakeDestination) GetScheme() string { return "ducklake" }

// A local file lock cannot fence mutations on a shared ducklake catalog, so
// leasing is rejected the same way the MotherDuck path is.
func (d *DuckLakeDestination) AcquireManagedCDCRunLease(context.Context, string) (source.ConnectorLease, error) {
	return nil, fmt.Errorf("DuckLake does not support local managed CDC run leases")
}

func (d *DuckLakeDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.RequirePrimaryKeyMatch {
		return fmt.Errorf("DuckLake does not support physical primary key constraints")
	}
	// Keys remain logical merge/dedup keys; DuckLake cannot enforce them in DDL.
	opts.PrimaryKeys = nil
	layout, err := buildDuckLakeTableLayout(opts)
	if err != nil {
		return err
	}
	if err := d.DuckDBDestination.PrepareTable(ctx, opts); err != nil {
		return err
	}
	d.rememberLayout(opts.Table, layout)
	if layout.empty() {
		return nil
	}

	d.markLayoutPending(opts.Table)
	preparedSchema, err := d.GetTableSchema(ctx, opts.Table)
	if err != nil {
		return fmt.Errorf("ducklake: inspect table before applying layout: %w", err)
	}
	if !duckLakeLayoutAppliesToSchema(layout, preparedSchema) {
		return nil
	}
	return d.applyPendingLayout(ctx, opts.Table)
}

func (d *DuckLakeDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	warnings, err := d.DuckDBDestination.ApplySchemaEvolution(ctx, table, comparison)
	if err != nil {
		return warnings, err
	}
	if err := d.applyPendingLayout(ctx, table); err != nil {
		return warnings, err
	}
	return warnings, nil
}

func (d *DuckLakeDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expectedIncarnation string,
) ([]string, string, error) {
	warnings, resultIncarnation, err := d.DuckDBDestination.ApplySchemaEvolutionIfIncarnation(ctx, table, comparison, expectedIncarnation)
	if err != nil {
		return warnings, resultIncarnation, err
	}
	d.clearPendingLayout(table)
	return warnings, resultIncarnation, nil
}

// reapplyLayoutOnSwap configures a recreated target before rows are copied in.
func (d *DuckLakeDestination) reapplyLayoutOnSwap(ctx context.Context, stagingTable, targetTable string) error {
	return d.execLayoutStatements(ctx, targetTable, d.layoutForSwap(stagingTable, targetTable), false)
}

// layoutForSwap replays the staging layout against the target table.
func (d *DuckLakeDestination) layoutForSwap(stagingTable, targetTable string) []duckLakeLayoutStatement {
	layout, ok := d.lookupLayout(stagingTable)
	if !ok {
		return nil
	}
	return duckLakeLayoutStatements(targetTable, layout)
}

func (d *DuckLakeDestination) rememberLayout(table string, layout duckLakeTableLayout) {
	d.layoutsMu.Lock()
	defer d.layoutsMu.Unlock()
	// An empty layout clears any spec recorded by an earlier run.
	if layout.empty() {
		delete(d.layouts, table)
		delete(d.pendingLayouts, table)
		return
	}
	if d.layouts == nil {
		d.layouts = make(map[string]duckLakeTableLayout)
	}
	d.layouts[table] = layout
}

// forgetLayout drops a table's recorded layout once its staging table is gone,
// so long-lived destinations don't accumulate stale per-run entries.
func (d *DuckLakeDestination) forgetLayout(table string) {
	d.layoutsMu.Lock()
	defer d.layoutsMu.Unlock()
	delete(d.layouts, table)
	delete(d.pendingLayouts, table)
}

func (d *DuckLakeDestination) markLayoutPending(table string) {
	d.layoutsMu.Lock()
	defer d.layoutsMu.Unlock()
	if d.pendingLayouts == nil {
		d.pendingLayouts = make(map[string]struct{})
	}
	d.pendingLayouts[table] = struct{}{}
}

func (d *DuckLakeDestination) applyPendingLayout(ctx context.Context, table string) error {
	layout, pending := d.pendingLayout(table)
	if !pending {
		return nil
	}
	if err := d.applyTableLayout(ctx, table, layout); err != nil {
		return err
	}
	d.clearPendingLayout(table)
	return nil
}

func (d *DuckLakeDestination) execPendingLayoutLocked(ctx context.Context, table string) error {
	layout, pending := d.pendingLayout(table)
	if !pending {
		return nil
	}
	return d.execTableLayout(ctx, table, layout)
}

func (d *DuckLakeDestination) pendingLayout(table string) (duckLakeTableLayout, bool) {
	d.layoutsMu.Lock()
	defer d.layoutsMu.Unlock()
	layout, remembered := d.layouts[table]
	_, pending := d.pendingLayouts[table]
	return layout, remembered && pending
}

func (d *DuckLakeDestination) clearPendingLayout(table string) {
	d.layoutsMu.Lock()
	defer d.layoutsMu.Unlock()
	delete(d.pendingLayouts, table)
}

func (d *DuckLakeDestination) lookupLayout(table string) (duckLakeTableLayout, bool) {
	d.layoutsMu.Lock()
	defer d.layoutsMu.Unlock()
	layout, ok := d.layouts[table]
	return layout, ok
}

// Write and WriteParallel go through an in-memory stage: the DuckDB appender
// cannot target tables in a DuckLake catalog, but SQL writes into them work.
func (d *DuckLakeDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.catalog.writeViaMemoryStage(ctx, d.DuckDBDestination, records, opts)
}

func (d *DuckLakeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.catalog.writeViaMemoryStage(ctx, d.DuckDBDestination, records, opts)
}

func (d *DuckLakeDestination) Connect(ctx context.Context, uri string) error {
	cfg, err := srcduckdb.ParseLakehouseURI(uri)
	if err != nil {
		return fmt.Errorf("ducklake: %w", err)
	}

	stmts, err := srcduckdb.NewLakehouseAttacher().GenerateAttachStatements(cfg, srcduckdb.AttachAlias)
	if err != nil {
		return fmt.Errorf("ducklake: %w", err)
	}

	return d.connectAttached(ctx, "ducklake", stmts, func(stmt string, err error) error {
		if srcduckdb.IsStorageProbe(stmt) {
			return srcduckdb.TranslateProbeError(cfg.Storage.Path, err)
		}
		return fmt.Errorf("ducklake: %w", srcduckdb.BootstrapError(stmt, err))
	})
}
