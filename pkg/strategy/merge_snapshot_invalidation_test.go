package strategy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type batchSnapshotDestination struct {
	*cdcStateDestination
	orderMu         sync.Mutex
	order           []string
	targetsToReset  map[string]bool
	partialPosition string
	mergeFailure    error
	afterStateWrite func()
}

func (d *batchSnapshotDestination) record(operation string) {
	d.orderMu.Lock()
	d.order = append(d.order, operation)
	d.orderMu.Unlock()
}

func (d *batchSnapshotDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.record("state")
	if err := d.cdcStateDestination.WriteCDCState(ctx, records, opts); err != nil {
		return err
	}
	if d.afterStateWrite != nil {
		d.afterStateWrite()
	}
	return nil
}

func (d *batchSnapshotDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.fakeDestination.WriteParallel(ctx, records, opts)
}

func (d *batchSnapshotDestination) TruncateTable(_ context.Context, table string) error {
	if d.targetsToReset[table] {
		d.record("truncate:" + table)
	}
	d.mu.Lock()
	d.truncateCalls = append(d.truncateCalls, table)
	d.mu.Unlock()
	return nil
}

func (d *batchSnapshotDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expected string) error {
	if err := d.cdcStateDestination.TruncateCDCTableIfIncarnation(ctx, table, expected); err != nil {
		return err
	}
	return d.TruncateTable(ctx, table)
}

func (d *batchSnapshotDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	d.record("merge:" + opts.TargetTable)
	if err := d.fakeDestination.MergeTable(ctx, opts); err != nil {
		return err
	}
	if d.partialPosition != "" {
		d.stateMu.Lock()
		d.maxLSNs[opts.TargetTable] = d.partialPosition
		d.stateMu.Unlock()
	}
	return d.mergeFailure
}

type atomicBatchSnapshotDestination struct{ *batchSnapshotDestination }

func (d *atomicBatchSnapshotDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	d.record("atomic")
	for _, merge := range merges {
		if merge.Truncate {
			if err := d.TruncateCDCTableIfIncarnation(ctx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation); err != nil {
				return err
			}
		}
		if err := d.MergeTable(ctx, merge.Options); err != nil {
			return err
		}
	}
	return nil
}

type batchSnapshotRun struct {
	dest    *batchSnapshotDestination
	manager *CDCStateManager
	tables  map[string]string
	execute func() error
}

func newBatchSnapshotRun(t *testing.T, mode string) batchSnapshotRun {
	t.Helper()
	tables := map[string]string{"public.orders": "raw.orders"}
	tableInfos := []source.SourceTableInfo{{Name: "public.orders", Schema: keylessCDCSchema(), PrimaryKeys: []string{"id"}}}
	if mode != "single" {
		tables["public.items"] = "raw.items"
		tableInfos = append(tableInfos, source.SourceTableInfo{Name: "public.items", Schema: keylessCDCSchema(), PrimaryKeys: []string{"id"}})
	}
	dest := &batchSnapshotDestination{
		cdcStateDestination: newCDCStateDestination(),
		targetsToReset:      make(map[string]bool),
	}
	dest.defaultMaxLSN = "00000000/00000020"
	snapshots := make(map[string]string, len(tables))
	for sourceTable, destTable := range tables {
		snapshots[sourceTable] = "00000000/00000010"
		dest.targetsToReset[destTable] = true
	}
	var target destination.Destination = dest
	if mode == "atomic" {
		target = &atomicBatchSnapshotDestination{batchSnapshotDestination: dest}
	}
	completeCDCStateRun(t, target, "batch-snapshot", tables, "00000000/00000020", snapshots)
	manager := restartedCDCStateManager(t, target, "batch-snapshot", tables)
	for sourceTable := range tables {
		require.Equal(t, "00000000/00000020", mustKeyedResumePosition(t, manager, sourceTable))
	}
	require.NoError(t, manager.BeginRun(t.Context(), false))
	dest.order = nil
	results := make([]source.RecordBatchResult, 0, len(tables))
	for _, table := range tableInfos {
		results = append(results, source.RecordBatchResult{TableName: table.Name, Truncate: true})
	}
	records := mustClosedRecords(results...)
	run := batchSnapshotRun{dest: dest, manager: manager, tables: tables}
	if mode == "single" {
		job, src, _ := minimalJob()
		job.Config.SourceTable = "public.orders"
		job.Config.DestTable = "raw.orders"
		job.Config.PrimaryKeys = []string{"id"}
		job.Schema = tableInfos[0].Schema
		job.SourceSchema = job.Schema
		job.Destination = target
		job.CDCStateManager = manager
		src.readCh = records
		run.execute = func() error { return (&MergeStrategy{}).Execute(t.Context(), job) }
	} else {
		job := &MultiTableIngestionJob{
			Config:          &config.IngestConfig{},
			Source:          &announcingMultiTableSource{tables: tableInfos, records: records},
			Destination:     target,
			Tables:          tableInfos,
			TableDestNames:  tables,
			CDCStateManager: manager,
		}
		run.execute = func() error { return (&MergeStrategy{}).ExecuteMultiTable(t.Context(), job) }
	}
	return run
}

func TestBatchMergeInvalidatesSnapshotsBeforeTargetMutation(t *testing.T) {
	for _, mode := range []string{"single", "multi", "atomic"} {
		t.Run(mode, func(t *testing.T) {
			run := newBatchSnapshotRun(t, mode)
			require.NoError(t, run.execute())
			require.Greater(t, len(run.dest.order), len(run.tables))
			for _, operation := range run.dest.order[:len(run.tables)] {
				require.Equal(t, "state", operation)
			}
			for sourceTable, destTable := range run.tables {
				require.Contains(t, run.dest.order, "truncate:"+destTable)
				require.Contains(t, run.dest.order, "merge:"+destTable)
				bound, err := run.manager.BoundDestinationIncarnation(sourceTable)
				require.NoError(t, err)
				require.Equal(t, "incarnation:"+destTable, bound)
			}
		})
	}
}

func TestBatchMergeInvalidationFailurePreventsTargetMutation(t *testing.T) {
	for _, mode := range []string{"single", "multi", "atomic"} {
		t.Run(mode, func(t *testing.T) {
			run := newBatchSnapshotRun(t, mode)
			run.dest.failWrite = run.dest.cdcWrites + len(run.tables)
			require.ErrorContains(t, run.execute(), "injected CDC state write failure")
			require.Len(t, run.dest.order, len(run.tables))
			for _, operation := range run.dest.order {
				require.Equal(t, "state", operation)
			}
			require.Empty(t, run.dest.mergeCalls)
			for _, table := range run.dest.truncateCalls {
				require.False(t, run.dest.targetsToReset[table], "target %s truncated after invalidation failed", table)
			}
		})
	}
}

func TestBatchMergeInterruptedReplacementCannotResumeOlderCheckpoint(t *testing.T) {
	for _, mode := range []string{"single", "multi", "atomic"} {
		for _, position := range []string{"00000000/00000010", "00000000/00000020"} {
			t.Run(fmt.Sprintf("%s/%s", mode, position), func(t *testing.T) {
				run := newBatchSnapshotRun(t, mode)
				run.dest.partialPosition = position
				run.dest.mergeFailure = errors.New("interrupted replacement")
				require.ErrorIs(t, run.execute(), run.dest.mergeFailure)
				restarted := restartedCDCStateManager(t, run.dest, "batch-snapshot", run.tables)
				for sourceTable, destTable := range run.tables {
					if slices.Contains(run.dest.order, "merge:"+destTable) {
						require.Equal(t, position, run.dest.maxLSNs[destTable])
					}
					require.Empty(t, mustKeyedResumePosition(t, restarted, sourceTable))
				}
			})
		}
	}
}

func TestBatchMergeTruncateCompletionRestoresResumability(t *testing.T) {
	for _, mode := range []string{"single", "multi", "atomic"} {
		for _, freshSnapshot := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fresh_snapshot=%t", mode, freshSnapshot), func(t *testing.T) {
				run := newBatchSnapshotRun(t, mode)
				require.NoError(t, run.execute())
				token := source.CDCStateCommitToken{Position: "00000000/00000030"}
				if freshSnapshot {
					token.SnapshotPositions = make(map[string]string, len(run.tables))
					for sourceTable := range run.tables {
						token.SnapshotPositions[sourceTable] = token.Position
					}
				}
				require.NoError(t, run.manager.Persist(t.Context(), token))
				run.dest.defaultMaxLSN = token.Position
				restarted := restartedCDCStateManager(t, run.dest, "batch-snapshot", run.tables)
				for sourceTable := range run.tables {
					require.Equal(t, token.Position, mustKeyedResumePosition(t, restarted, sourceTable))
				}
			})
		}
	}
}

func TestBatchMergeInvalidationKeepsOriginalDestinationPin(t *testing.T) {
	for _, mode := range []string{"single", "multi", "atomic"} {
		t.Run(mode, func(t *testing.T) {
			run := newBatchSnapshotRun(t, mode)
			run.dest.afterStateWrite = func() {
				run.dest.stateMu.Lock()
				defer run.dest.stateMu.Unlock()
				for _, destTable := range run.tables {
					run.dest.incarnations[destTable] = "replacement"
				}
			}
			require.ErrorContains(t, run.execute(), "physical incarnation changed")
			require.Empty(t, run.dest.mergeCalls)
			for _, operation := range run.dest.order {
				require.False(t, strings.HasPrefix(operation, "truncate:"))
			}
		})
	}
}
