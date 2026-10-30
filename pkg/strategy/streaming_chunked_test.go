package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type chunkedAdapterDestination struct{ *cdcStateDestination }

func (*chunkedAdapterDestination) SupportsCDCMerge() bool { return true }

func (d *chunkedAdapterDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, options destination.WriteOptions) error {
	return d.fakeDestination.WriteParallel(ctx, records, options)
}

func (d *chunkedAdapterDestination) TruncateTable(_ context.Context, table string) error {
	d.truncateCalls = append(d.truncateCalls, table)
	return nil
}

func TestStreamingChunkedOptionsPreserveFiniteModeAndSourceNames(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		for _, multi := range []bool{false, true} {
			t.Run(testChunkModeName(continuous, multi), func(t *testing.T) {
				dest := &chunkedAdapterDestination{newCDCStateDestination()}
				manager, err := NewCDCStateManager(dest, "chunk-adapter", "raw.orders", "")
				require.NoError(t, err)
				cfg := config.DefaultConfig()
				cfg.Stream, cfg.CDCChunkedSnapshot, cfg.FullRefresh = continuous, true, true
				cfg.SourceTable, cfg.DestTable = "public.orders", "raw.orders"
				cfg.PrimaryKeys = []string{"order_id"}
				cfg.CDCSnapshotChunkSize = 17
				cfg.CDCSnapshotChunkTimeout = 2 * time.Second
				cfg.CDCWALStaging = "destination"
				cfg.CDCWALBufferBytes = 4096
				cfg.NoLoadTimestamp, cfg.NoRunID = true, true
				sourceSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "OrderID", DataType: schema.TypeInt64}}, PrimaryKeys: []string{"OrderID"}}
				targetSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "order_id", DataType: schema.TypeInt64}}, PrimaryKeys: []string{"order_id"}}
				executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, StateManager: manager, Committer: &fakeCommitter{}, FlushInterval: time.Hour, FlushRecords: 100})
				var read source.ReadOptions
				if multi {
					src := &announcingMultiTableSource{records: mustClosedRecords()}
					err = executor.ExecuteMultiTable(t.Context(), &MultiTableIngestionJob{
						Config: cfg, Source: src, Destination: dest,
						Tables:         []source.SourceTableInfo{{Name: cfg.SourceTable, Schema: targetSchema, PrimaryKeys: targetSchema.PrimaryKeys}},
						TableDestNames: map[string]string{cfg.SourceTable: cfg.DestTable},
					})
					read = src.readOpts.ReadOptions
				} else {
					src := &fakeSourceTable{readCh: mustClosedRecords()}
					err = executor.Execute(t.Context(), &IngestionJob{Config: cfg, Table: src, Destination: dest, Schema: targetSchema, SourceSchema: sourceSchema})
					read = src.readOpts
					require.Same(t, sourceSchema, read.Schema)
					require.Equal(t, "OrderID", read.Schema.PrimaryKeys[0])
				}
				require.NoError(t, err)
				require.True(t, read.Streaming)
				require.True(t, read.FullRefresh)
				require.NotNil(t, read.CDCChunkedSnapshot)
				require.Equal(t, continuous, read.CDCChunkedSnapshot.Continuous)
				require.Equal(t, 17, read.CDCChunkedSnapshot.ChunkSize)
				require.Equal(t, 2*time.Second, read.CDCChunkedSnapshot.ChunkTimeout)
				require.Equal(t, "destination", read.CDCChunkedSnapshot.WALStaging)
				require.Equal(t, int64(4096), read.CDCChunkedSnapshot.WALBufferBytes)
				require.NotNil(t, read.CDCChunkedSnapshot.Store)
				require.Equal(t, continuous, cfg.Stream)
				require.Empty(t, dest.swapCalls)
			})
		}
	}
}

func testChunkModeName(continuous, multi bool) string {
	name := "finite"
	if continuous {
		name = "continuous"
	}
	if multi {
		return name + "/multi"
	}
	return name + "/single"
}

func TestStreamingChunkedRejectsUnsupportedTablesBeforePreparation(t *testing.T) {
	for _, nullable := range []bool{false, true} {
		t.Run(testChunkModeName(nullable, true), func(t *testing.T) {
			dest := &chunkedAdapterDestination{newCDCStateDestination()}
			manager, err := NewCDCStateManager(dest, "chunk-adapter", "raw.orders", "")
			require.NoError(t, err)
			cfg := config.DefaultConfig()
			cfg.CDCChunkedSnapshot = true
			invalid := streamTestSchema()
			keys := []string(nil)
			if nullable {
				keys = []string{"id"}
				invalid.Columns[0].Nullable = true
			}
			executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, StateManager: manager, Committer: &fakeCommitter{}})
			err = executor.ExecuteMultiTable(t.Context(), &MultiTableIngestionJob{
				Config: cfg, Destination: dest,
				Tables: []source.SourceTableInfo{
					{Name: "valid", Schema: streamTestSchema(), PrimaryKeys: []string{"id"}},
					{Name: "invalid", Schema: invalid, PrimaryKeys: keys},
				},
			})
			require.Error(t, err)
			require.Empty(t, dest.prepareCalls)
		})
	}
}

type chunkedCommitFunc func(context.Context, any) error

func (f chunkedCommitFunc) CommitStream(ctx context.Context, token any) error { return f(ctx, token) }

func TestStreamingCDCFlushAcknowledgesOnlyAfterDataAndState(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	dest := &chunkedAdapterDestination{newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "chunk-flush", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(ctx, "public.orders", "raw.orders"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.Persist(ctx, source.CDCStateCommitToken{SnapshotPositions: map[string]string{"public.orders": "00000000/00000010"}}))
	acknowledged := make(chan struct{})
	committer := chunkedCommitFunc(func(ctx context.Context, token any) error {
		if len(dest.mergeCalls) != 1 {
			return errors.New("source acknowledged before data merge")
		}
		position, err := manager.ResumePosition(ctx, "public.orders")
		if err != nil || position != "00000000/00000020" {
			return errors.New("source acknowledged before checkpoint persistence")
		}
		if state, ok := token.(source.CDCStateCommitToken); !ok || state.SourceCommitToken != "chunk-1" {
			return errors.New("source checkpoint token was lost")
		}
		close(acknowledged)
		return nil
	})
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	records <- source.RecordBatchResult{CDCFlush: true, CommitToken: source.CDCStateCommitToken{Position: "00000000/00000020", SourceCommitToken: "chunk-1"}}
	go func() {
		select {
		case <-acknowledged:
		case <-ctx.Done():
		}
		close(records)
	}()
	loop := newFlushLoop(dest, config.DefaultConfig(), StreamingOptions{
		Strategy: config.StrategyMerge, StateManager: manager, Committer: committer, FlushInterval: time.Hour, FlushRecords: 1_000_000,
	}, map[string]*streamTableState{"": {destTable: "raw.orders", stagingTable: "staging", schema: streamTestSchema(), primaryKeys: []string{"id"}}})
	require.NoError(t, loop.run(ctx, records))
	require.NoError(t, ctx.Err(), "CDCFlush waited for source closure or the normal flush timer")
	select {
	case <-acknowledged:
	default:
		t.Fatal("chunk checkpoint was not acknowledged")
	}
}

func TestStreamingChunkedRequiresManagedAcknowledgments(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.CDCChunkedSnapshot = true
	dest := &chunkedAdapterDestination{newCDCStateDestination()}
	_, err := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge}).chunkedSnapshotOptions(cfg, dest)
	require.ErrorContains(t, err, "destination-managed CDC state")
	require.Empty(t, dest.prepareCalls)
	cfg.CDCChunkedSnapshot = false
	options, err := NewStreamingExecutor(StreamingOptions{}).chunkedSnapshotOptions(cfg, &fakeDestination{})
	require.NoError(t, err)
	require.Nil(t, options)
}
