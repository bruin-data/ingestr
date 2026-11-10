package strategy

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestStreamingSpoolOptionsPreserveFiniteModeAndSourceNames(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		for _, multi := range []bool{false, true} {
			t.Run(testChunkModeName(continuous, multi), func(t *testing.T) {
				dest := &chunkedAdapterDestination{newCDCStateDestination()}
				manager, err := NewCDCStateManager(dest, "chunk-adapter", "raw.orders", "")
				require.NoError(t, err)
				cfg := config.DefaultConfig()
				cfg.Stream, cfg.CDCSnapshotSpool, cfg.FullRefresh = continuous, true, true
				cfg.SourceTable, cfg.DestTable = "public.orders", "raw.orders"
				cfg.PrimaryKeys = []string{"order_id"}
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
				require.NotNil(t, read.CDCSnapshotSpool)
				require.Equal(t, continuous, read.CDCSnapshotSpool.Continuous)
				require.Equal(t, "destination", read.CDCSnapshotSpool.WALStaging)
				require.Equal(t, int64(4096), read.CDCSnapshotSpool.WALBufferBytes)
				require.NotNil(t, read.CDCSnapshotSpool.Store)
				require.Equal(t, continuous, cfg.Stream)
				require.Empty(t, dest.swapCalls)
			})
		}
	}
}

func TestSnapshotSpoolCannotBeDisabledWithPendingJournal(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	store := newCDCChunkTestStore(t, dest, "spool-guard")
	cfg := config.DefaultConfig()
	require.NoError(t, ValidateCDCSnapshotSpoolResume(ctx, cfg, store.manager))
	require.NoError(t, store.Save(ctx, "normal-spool/bootstrap", []byte(`{"complete":false}`)))
	require.ErrorContains(t, ValidateCDCSnapshotSpoolResume(ctx, cfg, store.manager), "requires --cdc-snapshot-spool")
	cfg.CDCSnapshotSpool = true
	require.NoError(t, ValidateCDCSnapshotSpoolResume(ctx, cfg, store.manager))
	cfg.CDCSnapshotSpool, cfg.FullRefresh = false, true
	require.NoError(t, ValidateCDCSnapshotSpoolResume(ctx, cfg, store.manager))
	cfg.FullRefresh = false
	require.NoError(t, store.Save(ctx, "normal-spool/bootstrap", []byte(`{"complete":true}`)))
	require.NoError(t, ValidateCDCSnapshotSpoolResume(ctx, cfg, store.manager))
	require.NoError(t, store.Save(ctx, "normal-spool/bootstrap", []byte(`invalid`)))
	require.ErrorContains(t, ValidateCDCSnapshotSpoolResume(ctx, cfg, store.manager), "failed to decode")
}

func TestStreamingSpoolRequiresManagedAcknowledgments(t *testing.T) {
	cfg := &config.IngestConfig{CDCSnapshotSpool: true}
	dest := &chunkedAdapterDestination{newCDCStateDestination()}
	_, err := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge}).snapshotSpoolOptions(cfg, dest)
	require.ErrorContains(t, err, "destination-managed CDC state")
	cfg.CDCSnapshotSpool = false
	options, err := NewStreamingExecutor(StreamingOptions{}).snapshotSpoolOptions(cfg, &fakeDestination{})
	require.NoError(t, err)
	require.Nil(t, options)
}

func TestStreamingSpoolKeepsDirectSnapshotLoading(t *testing.T) {
	loop, dest, table := directStreamSnapshotLoop(t)
	loop.cfg.CDCSnapshotSpool = true
	require.True(t, canDirectStreamSnapshot(dest, loop.cfg, table))
}

func TestStreamingSpoolRefreshOptions(t *testing.T) {
	dest := &chunkedAdapterDestination{newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "spool-options", "raw.orders", "")
	require.NoError(t, err)
	manager.refresh = &cdcRefresh{ID: "active-refresh"}
	cfg := &config.IngestConfig{CDCSnapshotSpool: true, CDCWALStaging: "disk", CDCWALSpoolDir: "/persistent/spool", RefreshID: "historical-refresh"}
	executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge, StateManager: manager, Committer: &fakeCommitter{}})
	options, err := executor.snapshotSpoolOptions(cfg, dest)
	require.NoError(t, err)
	require.Equal(t, "active-refresh", options.RefreshID)
	require.Equal(t, "disk", options.WALStaging)
	require.Equal(t, "/persistent/spool", options.Directory)
	manager.refresh.complete = true
	options, err = executor.snapshotSpoolOptions(cfg, dest)
	require.NoError(t, err)
	require.Empty(t, options.RefreshID)
}

func TestStreamingSpoolResumePreservesPhysicalDestination(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "replaced"}[changed], func(t *testing.T) {
			loop, dest, identity := chunkResumeLoop(t)
			loop.cfg.CDCChunkedSnapshot, loop.cfg.CDCSnapshotSpool = false, true
			if changed {
				dest.incarnations["raw.items"] = "replacement"
			}
			marker := chunkResumeMarker("")
			marker.CDCSnapshotSpoolIdentity = identity
			records := make(chan source.RecordBatchResult, 2)
			records <- marker
			records <- source.RecordBatchResult{CommitToken: chunkResumeCompletion()}
			close(records)
			defer drainAndRelease(records)
			err := loop.run(t.Context(), records)
			if changed {
				require.ErrorContains(t, err, "changed before resuming")
				require.Empty(t, dest.recordedOrder())
				require.Empty(t, loop.opts.Committer.(*fakeCommitter).committed())
				return
			}
			require.NoError(t, err)
			require.False(t, dest.targetTruncated())
			require.Equal(t, softCheckpoint, resumedSnapshotPosition(t, dest))
		})
	}
}
