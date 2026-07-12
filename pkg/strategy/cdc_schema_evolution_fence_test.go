package strategy

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type recreatingSchemaEvolutionDestination struct {
	*cdcStateDestination

	evolutionMu          sync.Mutex
	fencedCalls          int
	unfencedCalls        int
	expectedIncarnations []string
	recreateBeforeFenced bool
	writeTokenMu         sync.Mutex
	committedWriteTokens map[source.DurableID]struct{}
}

func (*recreatingSchemaEvolutionDestination) SupportsIdempotentCommitTokenWrites() bool { return true }

func (d *recreatingSchemaEvolutionDestination) WriteParallel(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	d.writeTokenMu.Lock()
	defer d.writeTokenMu.Unlock()
	if d.committedWriteTokens == nil {
		d.committedWriteTokens = make(map[source.DurableID]struct{})
	}
	if _, duplicate := d.committedWriteTokens[opts.CommitToken]; opts.CommitToken != "" && duplicate {
		drainAndRelease(records)
		return nil
	}
	if err := d.fakeDestination.WriteParallel(ctx, records, opts); err != nil {
		return err
	}
	if opts.CommitToken != "" {
		d.committedWriteTokens[opts.CommitToken] = struct{}{}
	}
	return nil
}

func (d *recreatingSchemaEvolutionDestination) ApplySchemaEvolution(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
) ([]string, error) {
	d.evolutionMu.Lock()
	d.unfencedCalls++
	d.evolutionMu.Unlock()
	return d.fakeDestination.ApplySchemaEvolution(ctx, table, comparison)
}

func (d *recreatingSchemaEvolutionDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expectedIncarnation string,
) ([]string, error) {
	d.evolutionMu.Lock()
	d.fencedCalls++
	d.expectedIncarnations = append(d.expectedIncarnations, expectedIncarnation)
	recreate := d.recreateBeforeFenced
	d.recreateBeforeFenced = false
	d.evolutionMu.Unlock()

	d.stateMu.Lock()
	if recreate {
		d.incarnations[table] = "target-v2"
	}
	currentIncarnation := d.incarnations[table]
	d.stateMu.Unlock()
	if currentIncarnation != expectedIncarnation {
		return nil, fmt.Errorf("destination physical incarnation changed before schema evolution")
	}
	return d.fakeDestination.ApplySchemaEvolution(ctx, table, comparison)
}

func managedEvolutionPlan(table string) *schemaevolution.EvolutionPlan {
	return &schemaevolution.EvolutionPlan{
		Table: table,
		Comparison: &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{{
				Type:       schemaevolution.ChangeAddColumn,
				ColumnName: "new_column",
				NewColumn: schema.Column{
					Name: "new_column", DataType: schema.TypeString, Nullable: true,
				},
			}},
		},
	}
}

func managedEvolutionSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		},
		PrimaryKeys: []string{"id"},
	}
}

func newRecreatingEvolutionDestination(destTable string) *recreatingSchemaEvolutionDestination {
	base := newCDCStateDestination()
	base.incarnations[destTable] = "target-v1"
	return &recreatingSchemaEvolutionDestination{
		cdcStateDestination:  base,
		recreateBeforeFenced: true,
	}
}

func newManagedEvolutionStateManager(
	t *testing.T,
	dest *recreatingSchemaEvolutionDestination,
	sourceTable string,
	destTable string,
) *CDCStateManager {
	t.Helper()
	manager, err := NewCDCStateManager(dest, "managed-schema-evolution", destTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableState(t.Context(), sourceTable, destTable, "source-v1", "schema-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	return manager
}

func managedEvolutionJob(t *testing.T) (*IngestionJob, *fakeSourceTable, *recreatingSchemaEvolutionDestination) {
	t.Helper()
	job, src, _ := minimalJob()
	job.Schema = managedEvolutionSchema()
	job.SourceSchema = job.Schema
	job.Config.PrimaryKeys = []string{"id"}
	job.EvolutionPlan = managedEvolutionPlan(job.Config.DestTable)
	dest := newRecreatingEvolutionDestination(job.Config.DestTable)
	job.Destination = dest
	job.CDCStateManager = newManagedEvolutionStateManager(t, dest, job.Config.SourceTable, job.Config.DestTable)
	return job, src, dest
}

func requireRejectedRecreatedEvolution(t *testing.T, dest *recreatingSchemaEvolutionDestination) {
	t.Helper()
	dest.evolutionMu.Lock()
	defer dest.evolutionMu.Unlock()
	require.Equal(t, 1, dest.fencedCalls)
	require.Zero(t, dest.unfencedCalls)
	require.Equal(t, []string{"target-v1"}, dest.expectedIncarnations)
	require.Empty(t, dest.execCalls)
}

func TestRecreatingSchemaEvolutionDestinationDeduplicatesCommitTokens(t *testing.T) {
	dest := newRecreatingEvolutionDestination("raw.items")
	opts := destination.WriteOptions{Table: "raw.items", CommitToken: "data:0/10"}
	records := func() <-chan source.RecordBatchResult {
		return mustClosedRecords(source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)})
	}

	require.NoError(t, dest.WriteParallel(t.Context(), records(), opts))
	require.NoError(t, dest.WriteParallel(t.Context(), records(), opts))
	require.Len(t, dest.writeCalls, 1)
}

func TestManagedCDCEvolutionWithoutChangesDoesNotRequireFencedEvolver(t *testing.T) {
	dest := &fakeDestination{}
	plan := &schemaevolution.EvolutionPlan{
		Table:      "raw.items",
		Comparison: &schemaevolution.SchemaComparison{},
	}

	require.NoError(t, applyEvolutionPlanIfIncarnation(t.Context(), dest, plan, "target-v1"))
	require.Empty(t, dest.execCalls)
}

func TestManagedCDCAppendRejectsSchemaEvolutionAfterTargetRecreation(t *testing.T) {
	job, src, dest := managedEvolutionJob(t)

	err := (&AppendStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "incarnation changed before schema evolution")
	require.False(t, src.readCalled)
	requireRejectedRecreatedEvolution(t, dest)
}

func TestManagedCDCMergeRejectsSchemaEvolutionAfterTargetRecreation(t *testing.T) {
	job, src, dest := managedEvolutionJob(t)
	src.readCh = mustClosedRecords()

	err := (&MergeStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "incarnation changed before schema evolution")
	require.True(t, src.readCalled)
	require.Empty(t, dest.mergeCalls)
	requireRejectedRecreatedEvolution(t, dest)
}

func managedMultiTableEvolutionJob(t *testing.T) (*MultiTableIngestionJob, *announcingMultiTableSource, *recreatingSchemaEvolutionDestination) {
	t.Helper()
	sourceTable := "public.items"
	destTable := "raw.items"
	tableSchema := managedEvolutionSchema()
	table := source.SourceTableInfo{Name: sourceTable, Schema: tableSchema, PrimaryKeys: []string{"id"}}
	dest := newRecreatingEvolutionDestination(destTable)
	manager := newManagedEvolutionStateManager(t, dest, sourceTable, destTable)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: mustClosedRecords()}
	return &MultiTableIngestionJob{
		Config:          &config.IngestConfig{NoLoadTimestamp: true},
		Source:          src,
		Destination:     dest,
		Tables:          []source.SourceTableInfo{table},
		TableDestNames:  map[string]string{sourceTable: destTable},
		EvolutionPlans:  map[string]*schemaevolution.EvolutionPlan{sourceTable: managedEvolutionPlan(destTable)},
		CDCStateManager: manager,
	}, src, dest
}

func TestManagedCDCMultiTableAppendRejectsSchemaEvolutionAfterTargetRecreation(t *testing.T) {
	job, src, dest := managedMultiTableEvolutionJob(t)

	err := (&AppendStrategy{}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "incarnation changed before schema evolution")
	require.Empty(t, src.readOpts.KnownTables)
	requireRejectedRecreatedEvolution(t, dest)
}

func TestManagedCDCMultiTableMergeRejectsSchemaEvolutionAfterTargetRecreation(t *testing.T) {
	job, src, dest := managedMultiTableEvolutionJob(t)

	err := (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "incarnation changed before schema evolution")
	require.Empty(t, src.readOpts.KnownTables)
	requireRejectedRecreatedEvolution(t, dest)
}

func TestManagedCDCStreamingStartupRejectsSchemaEvolutionAfterTargetRecreation(t *testing.T) {
	job, src, dest := managedEvolutionJob(t)
	job.Config.Stream = true

	err := NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, StateManager: job.CDCStateManager,
	}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "incarnation changed before schema evolution")
	require.False(t, src.readCalled)
	requireRejectedRecreatedEvolution(t, dest)
}

func TestManagedCDCMultiTableStreamingStartupRejectsSchemaEvolutionAfterTargetRecreation(t *testing.T) {
	job, src, dest := managedMultiTableEvolutionJob(t)
	job.Config.Stream = true

	err := NewStreamingExecutor(StreamingOptions{
		Strategy: config.StrategyMerge, StateManager: job.CDCStateManager,
	}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "incarnation changed before schema evolution")
	require.Empty(t, src.readOpts.KnownTables)
	requireRejectedRecreatedEvolution(t, dest)
}

func TestManagedCDCStreamingReannouncementRejectsSchemaEvolutionAfterTargetRecreation(t *testing.T) {
	sourceTable := "public.items"
	destTable := "raw.items"
	dest := newRecreatingEvolutionDestination(destTable)
	manager := newManagedEvolutionStateManager(t, dest, sourceTable, destTable)
	require.NoError(t, manager.BindDestinationIncarnation(t.Context(), sourceTable, destTable))
	initialSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}, PrimaryKeys: []string{"id"}}
	refreshedSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "new_column", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}
	st := &streamTableState{
		destTable: destTable, stagingTable: "raw.items_staging", schema: initialSchema, primaryKeys: []string{"id"},
	}
	loop := newFlushLoop(dest, &config.IngestConfig{NoLoadTimestamp: true}, StreamingOptions{
		Strategy: config.StrategyMerge, StateManager: manager, FlushInterval: time.Hour,
	}, map[string]*streamTableState{sourceTable: st})
	loop.evolveTable = func(context.Context, string, *schema.TableSchema) error {
		t.Fatal("managed CDC re-announcement called the unfenced evolution callback")
		return nil
	}
	prepareCallsBeforeRefresh := len(dest.prepareCalls)

	err := loop.refreshTableSchema(t.Context(), source.SourceTableInfo{
		Name: sourceTable, Schema: refreshedSchema, PrimaryKeys: []string{"id"}, Incarnation: "source-v2", SchemaFingerprint: "schema-v2",
	}, st)
	require.ErrorContains(t, err, "incarnation changed before schema evolution")
	require.Len(t, dest.prepareCalls, prepareCallsBeforeRefresh)
	requireRejectedRecreatedEvolution(t, dest)
}
