package strategy

import (
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func cdcReplaceJob() (*IngestionJob, *fakeSourceTable, *fakeDestination) {
	job, src, dest := minimalJob()
	job.Config.FullRefresh = true
	job.Config.CDCDeleteBehavior = config.CDCDeleteHard
	job.Config.IncrementalKey = destination.CDCLSNColumn
	job.Schema.IncrementalKey = destination.CDCLSNColumn
	job.Schema.Columns = append(job.Schema.Columns,
		schema.Column{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		schema.Column{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
	)
	src.readCh = mustClosedRecords()
	return job, src, dest
}

func TestReplaceStrategyHardDeleteNormalizesBeforeSwap(t *testing.T) {
	for _, unique := range []bool{false, true} {
		name := "uncertain keys"
		if unique {
			name = "unique keys"
		}
		t.Run(name, func(t *testing.T) {
			job, src, dest := cdcReplaceJob()
			src.primaryKeysUnique = unique
			require.NoError(t, (&ReplaceStrategy{}).Execute(t.Context(), job))

			require.Len(t, dest.prepareCalls, 2)
			raw, normalized := dest.prepareCalls[0], dest.prepareCalls[1]
			require.Empty(t, raw.PrimaryKeys)
			require.True(t, raw.CDCMode)
			require.Equal(t, job.Config.PrimaryKeys, raw.CDCKeys)
			require.True(t, normalized.CDCMode)
			require.Equal(t, job.Config.PrimaryKeys, normalized.CDCKeys)
			require.NotContains(t, normalized.Schema.ColumnNames(), destination.CDCUnchangedColsColumn)
			require.Len(t, dest.mergeCalls, 1)
			merge := dest.mergeCalls[0]
			require.True(t, merge.CDCHardDelete)
			require.Equal(t, destination.CDCLSNColumn, merge.IncrementalKey)
			require.Equal(t, raw.Table, merge.StagingTable)
			require.Equal(t, normalized.Table, merge.TargetTable)
			require.NotContains(t, merge.Columns, destination.CDCUnchangedColsColumn)
			require.Len(t, dest.swapOptions, 1)
			require.Equal(t, normalized.Table, dest.swapOptions[0].StagingTable)
			require.Empty(t, dest.swapOptions[0].PrimaryKeys)
			require.NotContains(t, dest.swapOptions[0].Schema.ColumnNames(), destination.CDCUnchangedColsColumn)
		})
	}
}

func TestReplaceStrategyPreservesUniqueKeyFastPathWithoutHardCDC(t *testing.T) {
	for _, softCDC := range []bool{false, true} {
		name := "non CDC"
		if softCDC {
			name = "soft CDC"
		}
		t.Run(name, func(t *testing.T) {
			job, src, dest := minimalJob()
			job.Config.CDCDeleteBehavior = config.CDCDeleteHard
			if softCDC {
				job, src, dest = cdcReplaceJob()
				job.Config.CDCDeleteBehavior = config.CDCDeleteSoft
			}
			src.primaryKeysUnique = true
			src.readCh = mustClosedRecords()
			require.NoError(t, (&ReplaceStrategy{}).Execute(t.Context(), job))
			require.Len(t, dest.prepareCalls, 1)
			require.Empty(t, dest.mergeCalls)
		})
	}
}

func TestReplaceStrategyMultiTableHardDeleteNormalizesBeforeSwap(t *testing.T) {
	job, _, dest := cdcReplaceJob()
	table := source.SourceTableInfo{Name: "public.items", Schema: job.Schema, PrimaryKeys: job.Config.PrimaryKeys}
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: mustClosedRecords()}
	multiJob := &MultiTableIngestionJob{
		Config:         job.Config,
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{table.Name: "landing.items"},
	}

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), multiJob))
	require.Len(t, dest.prepareCalls, 2)
	require.True(t, dest.prepareCalls[0].CDCMode)
	require.Equal(t, table.PrimaryKeys, dest.prepareCalls[0].CDCKeys)
	require.Len(t, dest.mergeCalls, 1)
	require.True(t, dest.mergeCalls[0].CDCHardDelete)
	require.Equal(t, destination.CDCLSNColumn, dest.mergeCalls[0].IncrementalKey)
	require.Len(t, dest.swapOptions, 1)
	require.Equal(t, dest.mergeCalls[0].TargetTable, dest.swapOptions[0].StagingTable)
	require.NotContains(t, dest.swapOptions[0].Schema.ColumnNames(), destination.CDCUnchangedColsColumn)
}

type cdcAtomicTruncateInsertDestination struct {
	*atomicTruncateInsertDestination
}

func (d *cdcAtomicTruncateInsertDestination) SupportsMergeStrategy() bool { return true }

func TestInPlaceReplaceHardDeleteNormalizesBeforeAtomicFinalize(t *testing.T) {
	job, src, base := cdcReplaceJob()
	src.primaryKeysUnique = true
	dest := &cdcAtomicTruncateInsertDestination{atomicTruncateInsertDestination: &atomicTruncateInsertDestination{
		truncateCapableDestination: &truncateCapableDestination{fakeDestination: base},
	}}
	job.Destination = dest
	require.NoError(t, (&inPlaceReplaceStrategy{}).Execute(t.Context(), job))

	require.Len(t, base.prepareCalls, 3)
	require.True(t, base.prepareCalls[1].CDCMode)
	require.Len(t, base.mergeCalls, 1)
	require.True(t, base.mergeCalls[0].CDCHardDelete)
	require.Len(t, dest.finalizeCalls, 1)
	finalize := dest.finalizeCalls[0]
	require.Equal(t, base.mergeCalls[0].TargetTable, finalize.StagingTable)
	require.True(t, finalize.StagingPrimaryKeysUnique)
	require.NotContains(t, finalize.Columns, destination.CDCUnchangedColsColumn)
	require.Empty(t, base.truncateCalls)
	require.Contains(t, base.dropCalls, finalize.StagingTable)
}

func TestInPlaceReplaceHardDeleteFailurePreservesTargetAndCleansStaging(t *testing.T) {
	for _, mergeFails := range []bool{true, false} {
		name := "finalize fails"
		if mergeFails {
			name = "normalization fails"
		}
		t.Run(name, func(t *testing.T) {
			job, _, base := cdcReplaceJob()
			dest := &cdcAtomicTruncateInsertDestination{atomicTruncateInsertDestination: &atomicTruncateInsertDestination{
				truncateCapableDestination: &truncateCapableDestination{fakeDestination: base},
			}}
			job.Destination = dest
			failure := errors.New("operation failed")
			if mergeFails {
				base.mergeErr = failure
			} else {
				dest.finalizeErr = failure
			}

			require.ErrorIs(t, (&inPlaceReplaceStrategy{}).Execute(t.Context(), job), failure)
			require.Empty(t, base.truncateCalls)
			require.Len(t, base.mergeCalls, 1)
			require.Contains(t, base.dropCalls, base.mergeCalls[0].StagingTable)
			require.Contains(t, base.dropCalls, base.mergeCalls[0].TargetTable)
			if mergeFails {
				require.Empty(t, dest.finalizeCalls)
			}
		})
	}
}
