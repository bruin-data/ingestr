package strategy

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type postgresInPlaceReplaceDestination struct {
	*fakeDestination
	finalizeCalls     []destination.TruncateInsertFromStagingOptions
	execsAtFinalize   []int
	finalizeErrByDest map[string]error
}

func (d *postgresInPlaceReplaceDestination) GetScheme() string { return "postgres" }

func (d *postgresInPlaceReplaceDestination) TruncateInsertFromStaging(_ context.Context, opts destination.TruncateInsertFromStagingOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.finalizeCalls = append(d.finalizeCalls, opts)
	d.execsAtFinalize = append(d.execsAtFinalize, len(d.execCalls))
	return d.finalizeErrByDest[opts.TargetTable]
}

func multiTableInPlaceReplaceJob(tables ...source.SourceTableInfo) (*MultiTableIngestionJob, *postgresInPlaceReplaceDestination) {
	dest := &postgresInPlaceReplaceDestination{fakeDestination: &fakeDestination{}}
	destNames := make(map[string]string, len(tables))
	for _, table := range tables {
		destNames[table.Name] = "landing." + table.Name[len("public."):]
	}
	src := &announcingMultiTableSource{tables: tables, records: mustClosedRecords()}
	return &MultiTableIngestionJob{
		Config:         &config.IngestConfig{FullRefresh: true},
		Source:         src,
		Destination:    dest,
		Tables:         tables,
		TableDestNames: destNames,
	}, dest
}

func TestReplaceStrategy_ExecuteMultiTable_PostgresReplacesInPlace(t *testing.T) {
	orders, users := newTableInfo("public.orders"), newTableInfo("public.users")
	job, dest := multiTableInPlaceReplaceJob(orders, users)
	job.EvolutionPlans = map[string]*schemaevolution.EvolutionPlan{
		orders.Name: {Table: "landing.orders", Comparison: &schemaevolution.SchemaComparison{
			Changes:    []schemaevolution.SchemaChange{{Type: schemaevolution.ChangeAddColumn, ColumnName: "note"}},
			HasChanges: true,
		}},
	}

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), job))

	require.Empty(t, dest.swapOptions)
	targetPrepares := map[string]destination.PrepareOptions{}
	for _, opts := range dest.prepareCalls {
		if opts.Table == "landing.orders" || opts.Table == "landing.users" {
			targetPrepares[opts.Table] = opts
		}
	}
	require.Len(t, targetPrepares, 2)
	for _, opts := range targetPrepares {
		require.False(t, opts.DropFirst)
		require.True(t, opts.PrimaryKeysOnlyOnCreate)
		require.Equal(t, []string{"id"}, opts.PrimaryKeys)
	}

	require.Len(t, dest.finalizeCalls, 2)
	stagingTables := make([]string, 0, len(dest.finalizeCalls))
	for i, finalize := range dest.finalizeCalls {
		require.Equal(t, job.GetDestTableName(job.Tables[i].Name), finalize.TargetTable)
		require.NotEqual(t, finalize.TargetTable, finalize.StagingTable)
		require.Equal(t, []string{"id"}, finalize.PrimaryKeys)
		require.True(t, finalize.ReplacePrimaryKey)
		require.False(t, finalize.StagingPrimaryKeysUnique)
		stagingTables = append(stagingTables, finalize.StagingTable)
	}
	require.Len(t, dest.execCalls, 1)
	require.Contains(t, dest.execCalls[0].sql, "ALTER TABLE landing.orders ADD COLUMN note")
	require.Equal(t, 1, dest.execsAtFinalize[0], "schema evolution must run before the in-place replacement")
	require.ElementsMatch(t, stagingTables, dest.dropCalls)
}

func TestReplaceStrategy_ExecuteMultiTable_PostgresInPlaceKeepStaging(t *testing.T) {
	job, dest := multiTableInPlaceReplaceJob(newTableInfo("public.orders"))
	job.Config.KeepStaging = true

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), job))
	require.Len(t, dest.finalizeCalls, 1)
	require.Empty(t, dest.dropCalls)
}

func TestReplaceStrategy_ExecuteMultiTable_PostgresInPlaceFailureDropsUnfinalizedStaging(t *testing.T) {
	orders, users := newTableInfo("public.orders"), newTableInfo("public.users")
	job, dest := multiTableInPlaceReplaceJob(orders, users)
	failure := errors.New("truncate blocked by foreign key")
	dest.finalizeErrByDest = map[string]error{"landing.orders": failure}

	require.ErrorIs(t, (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), job), failure)
	require.Len(t, dest.finalizeCalls, 1)

	var staging []string
	for _, opts := range dest.prepareCalls {
		if opts.DropFirst {
			staging = append(staging, opts.Table)
		}
	}
	require.Len(t, staging, 2)
	require.ElementsMatch(t, staging, dest.dropCalls)
}

func TestReplaceStrategy_ExecuteMultiTable_PostgresInPlaceHardDeleteNormalizes(t *testing.T) {
	job, _, _ := cdcReplaceJob()
	table := source.SourceTableInfo{Name: "public.items", Schema: job.Schema, PrimaryKeys: job.Config.PrimaryKeys}
	multiJob, dest := multiTableInPlaceReplaceJob(table)
	multiJob.Config = job.Config

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), multiJob))

	var target *destination.PrepareOptions
	for i := range dest.prepareCalls {
		if dest.prepareCalls[i].Table == "landing.items" {
			target = &dest.prepareCalls[i]
		}
	}
	require.NotNil(t, target)
	require.True(t, target.CDCMode)
	require.Equal(t, table.PrimaryKeys, target.CDCKeys)
	require.NotContains(t, target.Schema.ColumnNames(), destination.CDCUnchangedColsColumn)

	require.Len(t, dest.mergeCalls, 1)
	merge := dest.mergeCalls[0]
	require.True(t, merge.CDCHardDelete)
	require.Len(t, dest.finalizeCalls, 1)
	finalize := dest.finalizeCalls[0]
	require.Equal(t, merge.TargetTable, finalize.StagingTable)
	require.True(t, finalize.StagingPrimaryKeysUnique)
	require.NotContains(t, finalize.Columns, destination.CDCUnchangedColsColumn)
	require.Contains(t, finalize.Columns, destination.CDCDeletedColumn)
	require.Contains(t, dest.dropCalls, merge.StagingTable)
	require.Contains(t, dest.dropCalls, finalize.StagingTable)
	require.Empty(t, dest.swapOptions)
}

func TestReplaceStrategy_ExecuteMultiTable_NonPostgresStillSwaps(t *testing.T) {
	table := source.SourceTableInfo{Name: "public.orders", Schema: &schema.TableSchema{
		Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}},
	}, PrimaryKeys: []string{"id"}}
	dest := &fakeDestination{}
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: mustClosedRecords()}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{table.Name: "landing.orders"},
	}

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), job))
	require.Len(t, dest.swapOptions, 1)
	for _, opts := range dest.prepareCalls {
		require.False(t, opts.PrimaryKeysOnlyOnCreate)
	}
}

func TestReplaceStrategy_ExecuteMultiTable_PostgresInPlaceSoftDeleteCDCNormalizes(t *testing.T) {
	job, _, _ := cdcReplaceJob()
	job.Config.CDCDeleteBehavior = config.CDCDeleteSoft
	table := source.SourceTableInfo{Name: "public.items", Schema: job.Schema, PrimaryKeys: job.Config.PrimaryKeys}
	multiJob, dest := multiTableInPlaceReplaceJob(table)
	multiJob.Config = job.Config

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), multiJob))

	require.Len(t, dest.mergeCalls, 1)
	merge := dest.mergeCalls[0]
	require.False(t, merge.CDCHardDelete)
	require.Contains(t, merge.Columns, destination.CDCDeletedColumn)
	require.Len(t, dest.finalizeCalls, 1)
	finalize := dest.finalizeCalls[0]
	require.Equal(t, merge.TargetTable, finalize.StagingTable)
	require.True(t, finalize.StagingPrimaryKeysUnique)
	require.Contains(t, dest.dropCalls, finalize.StagingTable)
}
