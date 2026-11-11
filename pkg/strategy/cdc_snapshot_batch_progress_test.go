package strategy

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type progressBatchSnapshotDestination struct {
	*directBatchSnapshotDestination
	commitError      error
	failCommit       int
	commits          int
	committedRows    int64
	progressCalls    int
	beforeCheckpoint func(int)
}

func (d *progressBatchSnapshotDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	var pendingRows int64
	for result := range records {
		if result.Batch != nil {
			pendingRows += result.Batch.NumRows()
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
		token, ok := result.CommitToken.(source.CDCStateCommitToken)
		if !ok || len(token.SnapshotProgress) == 0 {
			continue
		}
		d.commits++
		if d.commits == d.failCommit {
			return d.commitError
		}
		d.committedRows += pendingRows
		pendingRows = 0
		if d.beforeCheckpoint != nil {
			d.beforeCheckpoint(d.commits)
		}
		if opts.CDCSnapshotProgress == nil {
			return errors.New("snapshot progress callback missing")
		}
		d.progressCalls++
		if err := opts.CDCSnapshotProgress(ctx, token); err != nil {
			return err
		}
	}
	d.committedRows += pendingRows
	return nil
}

func newProgressBatchSnapshotWriter(t *testing.T) (*cdcSnapshotBatchWriter, *progressBatchSnapshotDestination) {
	t.Helper()
	loop, base, _ := softReplacementLoop(t)
	dest := &progressBatchSnapshotDestination{directBatchSnapshotDestination: &directBatchSnapshotDestination{softReplaceDestination: base}}
	return &cdcSnapshotBatchWriter{
		dest: dest, cfg: config.DefaultConfig(), manager: loop.opts.StateManager,
		sourceTable: "public.items", expectedIncarnation: "incarnation:raw.items",
		table: mergeTableParams{
			DestTable: "raw.items", StagingTable: "raw.items_stage", Schema: keylessCDCSchema(), PrimaryKeys: []string{"id"}, IsCDC: true,
		},
	}, dest
}

func TestCDCSnapshotBatchProgressPersistsOnlyCoveredTable(t *testing.T) {
	writer, dest := newProgressBatchSnapshotWriter(t)
	require.NoError(t, writer.manager.RegisterTableIncarnation(t.Context(), "public.other", "raw.other", "200"))
	require.NoError(t, writer.manager.BindDestinationIncarnation(t.Context(), "public.other", "raw.other"))
	progress := snapshotRangeProgress()
	progress.Ranges[0].Key = []string{"1"}
	token := source.CDCStateCommitToken{
		SourceCommitToken: "native-WAL-acknowledgement", Position: "00000000/00000099",
		SnapshotPositions: map[string]string{"public.other": "00000000/00000099"},
		SnapshotProgress: map[string]source.CDCSnapshotProgress{
			"public.items": progress,
			"public.other": {Boundary: "invalid-other-table-progress"},
		},
	}
	interrupted := errors.New("snapshot interrupted")
	records := mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1),
		source.RecordBatchResult{CommitToken: token}, source.RecordBatchResult{Err: interrupted})
	require.ErrorIs(t, writer.write(t.Context(), records, func() {}), interrupted)
	durable, ok := persistedStreamingSnapshotProgress(t, dest.softReplaceDestination)
	require.True(t, ok)
	require.Equal(t, progress, durable)
	require.Equal(t, 1, dest.progressCalls)
	require.NotContains(t, writer.manager.knownComplete, "public.other")
	for _, entry := range writer.manager.entries {
		if entry.StateKind == cdcStateKindCheckpoint {
			require.NotEqual(t, token.Position, entry.Position, "snapshot progress must not advance the WAL checkpoint")
		}
		if entry.StateKind == cdcStateKindSnapshotProgress {
			require.NotEqual(t, "public.other", entry.SourceTable)
		}
	}
}

func TestCDCSnapshotBatchProgressFailuresKeepCommittedCursor(t *testing.T) {
	for _, phase := range []string{"commit", "persist", "target changed"} {
		t.Run(phase, func(t *testing.T) {
			writer, dest := newProgressBatchSnapshotWriter(t)
			progress := snapshotRangeProgress()
			progress.Ranges[1].Key, progress.Ranges[1].Complete = []string{"100"}, true
			want := progress.Clone()
			later := progress.Clone()
			later.Ranges[0].Key = []string{"2"}
			dest.beforeCheckpoint = func(commit int) {
				if commit != 2 {
					return
				}
				switch phase {
				case "persist":
					dest.failWrite = dest.cdcWrites + 1
				case "target changed":
					dest.stateMu.Lock()
					dest.incarnations["raw.items"] = "replacement"
					dest.stateMu.Unlock()
				}
			}
			if phase == "commit" {
				dest.failCommit, dest.commitError = 2, errors.New("transaction commit failed")
			}
			records := mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 100), snapshotRangeProgressResult(progress),
				directSnapshotBatch(t, "", 2), snapshotRangeProgressResult(later))
			err := writer.write(t.Context(), records, func() {})
			require.Error(t, err)
			if phase == "target changed" {
				require.ErrorContains(t, err, "replaced during its snapshot")
				_, ok := persistedStreamingSnapshotProgress(t, dest.softReplaceDestination)
				require.False(t, ok, "a replacement target must not reuse the original target's cursor")
				_, durable, ok := writer.manager.latestSnapshotProgress("public.items")
				require.True(t, ok)
				require.Equal(t, want, durable.progress())
			} else {
				durable, ok := persistedStreamingSnapshotProgress(t, dest.softReplaceDestination)
				require.True(t, ok)
				require.Equal(t, want, durable)
			}
			require.Empty(t, dest.retireCalls)
			require.Empty(t, resumedSnapshotPosition(t, dest.softReplaceDestination))
		})
	}
}

func TestCDCSnapshotBatchInitialPlanIsDurableBeforeRows(t *testing.T) {
	writer, dest := newProgressBatchSnapshotWriter(t)
	progress := snapshotRangeProgress()
	interrupted := errors.New("interrupted before reading rows")
	records := mustClosedRecords(softMarker(), snapshotRangeProgressResult(progress), source.RecordBatchResult{Err: interrupted})
	require.ErrorIs(t, writer.write(t.Context(), records, func() {}), interrupted)
	durable, ok := persistedStreamingSnapshotProgress(t, dest.softReplaceDestination)
	require.True(t, ok)
	require.Equal(t, progress, durable)
	require.Zero(t, dest.commits)
	require.Zero(t, dest.progressCalls)
	require.Empty(t, dest.retireCalls)
}

func TestCDCSnapshotBatchProgressDoesNotRecertifyPriorSnapshot(t *testing.T) {
	writer, dest := newProgressBatchSnapshotWriter(t)
	position, err := writer.manager.ResumePosition(t.Context(), "public.items")
	require.NoError(t, err)
	require.Equal(t, "00000000/00000020", position)
	require.Contains(t, writer.manager.knownComplete, "public.items")
	progress := snapshotRangeProgress()
	progress.Ranges[0].Key = []string{"1"}
	records := mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 1), snapshotRangeProgressResult(progress),
		source.RecordBatchResult{Err: errors.New("snapshot interrupted")})
	require.Error(t, writer.write(t.Context(), records, func() {}))
	require.NotContains(t, writer.manager.knownComplete, "public.items")
	require.Empty(t, resumedSnapshotPosition(t, dest.softReplaceDestination))
	for _, entry := range writer.manager.entries {
		if entry.SourceTable == "public.items" && entry.StateKind == cdcStateKindSnapshot && entry.Status == destination.CDCStateStatusComplete {
			_, _, _, epoch := decodeCDCStatePositionWithSchema(entry.Position)
			require.NotEqual(t, writer.manager.snapshotEpochs["public.items"], epoch, "partial progress cannot complete the replacement epoch")
		}
	}
}

func TestCDCSnapshotBatchResumesCommittedRangeProgress(t *testing.T) {
	writer, dest := newProgressBatchSnapshotWriter(t)
	progress := snapshotRangeProgress()
	progress.Ranges[1].Key, progress.Ranges[1].Complete = []string{"100"}, true
	interrupted := errors.New("snapshot interrupted")
	first := mustClosedRecords(softMarker(), directSnapshotBatch(t, "", 100), snapshotRangeProgressResult(progress), source.RecordBatchResult{Err: interrupted})
	require.ErrorIs(t, writer.write(t.Context(), first, func() {}), interrupted)
	manager, err := NewCDCStateManager(dest.softReplaceDestination, "stream-soft-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"))
	durable, ok, err := manager.SnapshotResume(t.Context(), "public.items")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, progress, durable)
	require.NoError(t, manager.BeginRun(t.Context(), false))
	writer.manager, writer.snapshotBoundary, writer.retireSnapshot = manager, "", false
	dest.maxLSN = softSnapshotBoundary
	marker := softMarker()
	marker.CDCSnapshotResumed = true
	progress.Ranges[0].Key = []string{"2"}
	second := mustClosedRecords(marker, directSnapshotBatch(t, "", 2), snapshotRangeProgressResult(progress), source.RecordBatchResult{Err: interrupted})
	require.ErrorIs(t, writer.write(t.Context(), second, func() {}), interrupted)
	durable, ok = persistedStreamingSnapshotProgress(t, dest.softReplaceDestination)
	require.True(t, ok)
	require.Equal(t, progress, durable)
	require.False(t, dest.targetTruncated())
	require.EqualValues(t, 2, dest.committedRows)
	require.Empty(t, dest.retireCalls)
	require.Empty(t, resumedSnapshotPosition(t, dest.softReplaceDestination))
}

func TestCDCSnapshotBatchRejectsTargetReplacementBeforeSnapshot(t *testing.T) {
	writer, dest := newProgressBatchSnapshotWriter(t)
	dest.incarnations["raw.items"] = "replacement"
	records := mustClosedRecords(softMarker(), snapshotRangeProgressResult(snapshotRangeProgress()))
	require.ErrorContains(t, writer.write(t.Context(), records, func() {}), "changed before its snapshot")
	require.Zero(t, dest.commits)
	require.False(t, dest.targetTruncated())
	require.Empty(t, dest.retireCalls)
	_, ok := persistedStreamingSnapshotProgress(t, dest.softReplaceDestination)
	require.False(t, ok)
}
