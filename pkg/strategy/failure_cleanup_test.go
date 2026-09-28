package strategy

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestStagingCleanupSingleFailure(t *testing.T) {
	for _, strategy := range []WriteStrategy{&ReplaceStrategy{}, &MergeStrategy{}} {
		for _, stage := range []string{"read", "write", "cancel", "merge", "swap", "normalised-prepare"} {
			t.Run(string(strategy.Name())+"/"+stage, func(t *testing.T) {
				if (stage == "swap" || stage == "normalised-prepare") && strategy.Name() != config.StrategyReplace {
					t.Skip("replace-only path")
				}
				job, src, dest := minimalJob()
				dest.liveTables = map[string]bool{job.Config.DestTable: true}
				src.readCh = mustClosedRecords()
				failure := errors.New("injected failure")
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				switch stage {
				case "read":
					src.readErr = failure
				case "write":
					dest.writeErr = failure
				case "cancel":
					dest.afterWrite = cancel
					dest.writeErr = context.Canceled
					failure = context.Canceled
				case "merge":
					dest.mergeErr = failure
				case "swap":
					dest.swapErr = failure
				case "normalised-prepare":
					job.Config.RunID = "test"
					table := GenerateNormalisedStagingTableName(job.Config.DestTable, "", "test")
					dest.prepareErrByTable = map[string]error{table: failure}
				}
				require.ErrorIs(t, strategy.Execute(ctx, job), failure)
				require.Equal(t, map[string]bool{job.Config.DestTable: true}, dest.liveTables)
				for i, err := range dest.dropContextErrors {
					require.NoError(t, err)
					require.True(t, dest.dropHasDeadline[i], "cleanup must be bounded")
				}
			})
		}
	}
}

type failingReadMultiSource struct {
	*announcingMultiTableSource
	err error
}

func (s *failingReadMultiSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.announcingMultiTableSource.ReadAll(ctx, opts)
}

func TestStagingCleanupMultiFailure(t *testing.T) {
	for _, strategy := range []MultiTableStrategy{&ReplaceStrategy{}, &MergeStrategy{}} {
		for _, stage := range []string{"prepare", "read", "write", "cancel", "merge", "swap", "direct-write"} {
			t.Run(string(strategy.(WriteStrategy).Name())+"/"+stage, func(t *testing.T) {
				if (stage == "swap" || stage == "direct-write") && strategy.(WriteStrategy).Name() != config.StrategyReplace {
					t.Skip("replace-only path")
				}
				a, b := newTableInfo("a"), newTableInfo("b")
				src := &failingReadMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
					tables:  []source.SourceTableInfo{a, b},
					records: mustClosedRecords(source.RecordBatchResult{TableName: "a"}, source.RecordBatchResult{TableName: "b"}),
				}}
				dest := &fakeDestination{liveTables: map[string]bool{"a": true, "b": true}}
				job := &MultiTableIngestionJob{Config: &config.IngestConfig{RunID: "test"}, Source: src, Destination: dest, Tables: src.tables}
				failure := errors.New("injected failure")
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				switch stage {
				case "prepare":
					failedTable := "b"
					if strategy.(WriteStrategy).Name() == config.StrategyReplace {
						failedTable = replaceStagingTableName(dest, "b", "", "test")
					}
					dest.prepareErrByTable = map[string]error{failedTable: failure}
				case "read":
					src.err = failure
				case "write", "direct-write":
					dest.writeErr = failure
					if stage == "direct-write" {
						job.Destination = &directReplaceDedupDestination{fakeDestination: dest}
					}
				case "merge":
					dest.mergeErr = failure
				case "swap":
					dest.swapErrByTable = map[string]error{"b": failure}
				case "cancel":
					dest.afterWrite = cancel
					dest.writeErr = context.Canceled
					failure = context.Canceled
				}
				require.ErrorContains(t, strategy.ExecuteMultiTable(ctx, job), failure.Error())
				require.Equal(t, map[string]bool{"a": true, "b": true}, dest.liveTables)
				require.NotContains(t, dest.dropCalls, "a")
				require.NotContains(t, dest.dropCalls, "b")
				if stage == "swap" {
					require.Len(t, dest.swapCalls, 2, "first table swapped before second table failed")
				}
			})
		}
	}
}

func TestAppendStrategyMultiTable(t *testing.T) {
	for _, stage := range []string{"empty", "success", "prepare", "read", "write"} {
		t.Run(stage, func(t *testing.T) {
			a, b := newTableInfo("a"), newTableInfo("b")
			src := &failingReadMultiSource{announcingMultiTableSource: &announcingMultiTableSource{
				tables:  []source.SourceTableInfo{a, b},
				records: mustClosedRecords(source.RecordBatchResult{TableName: "a"}, source.RecordBatchResult{TableName: "b"}),
			}}
			dest := &fakeDestination{liveTables: map[string]bool{"out_a": true, "out_b": true}}
			job := &MultiTableIngestionJob{
				Config: &config.IngestConfig{ExtractParallelism: 3, LoaderFileSize: 987},
				Source: src, Destination: dest, Tables: src.tables,
				TableDestNames: map[string]string{"a": "out_a", "b": "out_b"},
			}
			failure := errors.New("injected failure")
			switch stage {
			case "empty":
				job.Tables = nil
			case "prepare":
				dest.prepareErrByTable = map[string]error{"out_b": failure}
			case "read":
				src.err = failure
			case "write":
				dest.writeErr = failure
			}
			err := (&AppendStrategy{}).ExecuteMultiTable(t.Context(), job)
			if stage == "empty" || stage == "success" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, failure.Error())
			}
			require.Empty(t, dest.dropCalls)
			require.Empty(t, dest.mergeCalls)
			require.Empty(t, dest.swapCalls)
			require.Equal(t, map[string]bool{"out_a": true, "out_b": true}, dest.liveTables)
			if stage == "success" {
				require.Len(t, dest.prepareCalls, 2)
				require.Len(t, dest.writeCalls, 2)
				var tables []string
				for _, prep := range dest.prepareCalls {
					require.False(t, prep.DropFirst)
					require.Equal(t, []string{"id"}, prep.PrimaryKeys)
				}
				for _, write := range dest.writeCalls {
					tables = append(tables, write.Table)
					require.False(t, write.StagingTable)
					require.Equal(t, 987, write.LoaderFileSize)
				}
				require.ElementsMatch(t, []string{"out_a", "out_b"}, tables)
				require.Equal(t, 3, src.readOpts.Parallelism)
			} else if stage != "write" {
				require.Empty(t, dest.writeCalls)
			}
		})
	}
}

func TestStagingCleanupDropFailurePreservesPrimaryError(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.RunID = "test"
	src.readCh = mustClosedRecords()
	failure := errors.New("merge failed")
	dest.mergeErr = failure
	normalised := GenerateNormalisedStagingTableName(job.Config.DestTable, "", "test")
	dest.dropErrByTable = map[string]error{normalised: errors.New("drop failed")}
	require.ErrorIs(t, (&ReplaceStrategy{}).Execute(t.Context(), job), failure)
	require.ElementsMatch(t, []string{normalised, replaceStagingTableName(dest, job.Config.DestTable, "", "test")}, dest.dropCalls)
}

func TestMergeCleanupKeepStagingAndLeaseLoss(t *testing.T) {
	for _, leaseLost := range []bool{false, true} {
		job, src, dest := minimalJob()
		job.Config.KeepStaging = true
		src.readCh = mustClosedRecords()
		ctx, cancel := context.WithCancelCause(t.Context())
		if leaseLost {
			dest.afterWrite = func() { cancel(source.ErrConnectorLeaseLost) }
		}
		err := (&MergeStrategy{}).Execute(ctx, job)
		cancel(nil)
		if leaseLost {
			require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
			require.Empty(t, dest.mergeCalls)
		} else {
			require.NoError(t, err)
			require.Len(t, dest.mergeCalls, 1)
		}
		require.Empty(t, dest.dropCalls)
	}
}

func TestDeleteInsertStrategyPropagatesDestinationError(t *testing.T) {
	job, src, dest := minimalJob()
	src.readCh = singleBatchRecords(t, 17, 42)
	dest.deleteInsertErr = errors.New("delete+insert failed")
	require.ErrorIs(t, (&DeleteInsertStrategy{}).Execute(t.Context(), job), dest.deleteInsertErr)
	require.Len(t, dest.diCalls, 1)
	require.Equal(t, job.Config.DestTable, dest.diCalls[0].TargetTable)
	require.NotEqual(t, job.Config.DestTable, dest.diCalls[0].StagingTable)
}
