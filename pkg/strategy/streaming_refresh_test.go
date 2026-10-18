package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type refreshRowsDestination struct {
	*cdcStateDestination
	rows map[string][]int64
}

func (d *refreshRowsDestination) WriteParallel(_ context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	for result := range records {
		if result.Batch != nil {
			ids := result.Batch.Column(0).(*array.Int64)
			d.mu.Lock()
			d.rows[opts.Table] = append(d.rows[opts.Table], ids.Int64Values()...)
			d.mu.Unlock()
			result.Batch.Release()
		}
	}
	return nil
}

func (d *refreshRowsDestination) TruncateTable(_ context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rows[table] = nil
	return nil
}

func (d *refreshRowsDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expected string) error {
	if err := d.cdcStateDestination.TruncateCDCTableIfIncarnation(ctx, table, expected); err != nil {
		return err
	}
	return d.TruncateTable(ctx, table)
}

func (d *refreshRowsDestination) MergeTable(_ context.Context, opts destination.MergeOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rows[opts.TargetTable] = append(d.rows[opts.TargetTable], d.rows[opts.StagingTable]...)
	return nil
}

func TestStreamingRefreshReplacesSnapshotAndContinuesInSameExecutor(t *testing.T) {
	for _, multi := range []bool{false, true} {
		name := "single"
		if multi {
			name = "multiple including empty table"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			dest := &refreshRowsDestination{cdcStateDestination: newCDCStateDestination(), rows: map[string][]int64{"raw.a": {99}, "raw.b": {99}}}
			manager, err := NewCDCStateManager(dest, "stream-refresh", "raw.a", "")
			require.NoError(t, err)
			require.NoError(t, manager.RegisterTable(ctx, "a", "raw.a"))
			if multi {
				require.NoError(t, manager.RegisterTable(ctx, "b", "raw.b"))
			}
			_, err = manager.PrepareRefresh(ctx, "one-operation", "scope")
			require.NoError(t, err)
			require.NoError(t, manager.BeginRun(ctx, true))
			cfg := config.DefaultConfig()
			cfg.Stream, cfg.FullRefresh = true, true
			cfg.SourceTable, cfg.DestTable = "a", "raw.a"
			cfg.PrimaryKeys = []string{"id"}
			cfg.NoLoadTimestamp, cfg.NoRunID = true, true
			tableSchema := streamTestSchema()
			tableSchema.Columns = append(tableSchema.Columns, schema.Column{Name: destination.CDCLSNColumn, DataType: schema.TypeString}, schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean})
			tag := ""
			if multi {
				tag = "a"
			}
			records := []source.RecordBatchResult{
				{TableName: tag, Truncate: true},
				{TableName: tag, Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
				{CommitToken: source.CDCStateCommitToken{Position: "00000000/00000020", SnapshotPositions: map[string]string{"a": "00000000/00000020"}}},
			}
			if multi {
				records = append(records, source.RecordBatchResult{TableName: "b", Truncate: true}, source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{Position: "00000000/00000020", SnapshotPositions: map[string]string{"b": "00000000/00000020"}}})
			}
			records = append(records,
				source.RecordBatchResult{TableName: tag, Batch: int64RecordBatch(t, "id", []int64{3}, nil)},
				source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{Position: "00000000/00000030"}},
			)
			executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, FlushInterval: time.Hour, FlushRecords: 100, StateManager: manager})
			if multi {
				tables := []source.SourceTableInfo{{Name: "a", Schema: tableSchema, PrimaryKeys: []string{"id"}}, {Name: "b", Schema: tableSchema, PrimaryKeys: []string{"id"}}}
				src := &announcingMultiTableSource{tables: tables, records: mustClosedRecords(records...)}
				err = executor.ExecuteMultiTable(ctx, &MultiTableIngestionJob{Config: cfg, Source: src, Destination: dest, Tables: tables, TableDestNames: map[string]string{"a": "raw.a", "b": "raw.b"}, CDCStateManager: manager})
				require.NoError(t, err)
				require.True(t, src.readOpts.FullRefresh)
				require.True(t, src.readOpts.CDCSnapshotReplace)
				require.True(t, src.readOpts.CDCManagedState)
				require.Empty(t, dest.rows["raw.b"])
			} else {
				src := &fakeSourceTable{name: "a", readCh: mustClosedRecords(records...)}
				err = executor.Execute(ctx, &IngestionJob{Config: cfg, Table: src, Destination: dest, Schema: tableSchema, SourceSchema: tableSchema, CDCStateManager: manager})
				require.NoError(t, err)
				require.True(t, src.readOpts.FullRefresh)
				require.True(t, src.readOpts.CDCSnapshotReplace)
				require.True(t, src.readOpts.CDCManagedState)
			}
			require.Equal(t, []int64{1, 3}, dest.rows["raw.a"])
			require.True(t, manager.refresh.complete)
			restarted, err := NewCDCStateManager(dest, "stream-refresh", "raw.a", "")
			require.NoError(t, err)
			require.NoError(t, restarted.RegisterTable(ctx, "a", "raw.a"))
			fresh, err := restarted.PrepareRefresh(ctx, "one-operation", "scope")
			require.NoError(t, err)
			require.False(t, fresh)
			position, err := restarted.ResumePosition(ctx, "a")
			require.NoError(t, err)
			require.Equal(t, "00000000/00000030", position)
		})
	}
}
