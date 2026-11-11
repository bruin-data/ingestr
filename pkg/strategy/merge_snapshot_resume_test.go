package strategy

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestMergeSingleTableSnapshotResumeReadOptions(t *testing.T) {
	for _, tc := range []struct {
		name            string
		fullRefresh     bool
		resumeLSN       string
		keepStaging     bool
		invalidProgress bool
		wantResume      bool
		wantRead        bool
	}{
		{name: "committed ranges", wantResume: true, wantRead: true},
		{name: "full refresh", fullRefresh: true, wantRead: true},
		{name: "WAL resume", resumeLSN: "00000000/00000099", wantRead: true},
		{name: "staging snapshot", keepStaging: true, wantRead: true},
		{name: "invalid progress", invalidProgress: true},
		{name: "full refresh skips invalid progress", fullRefresh: true, invalidProgress: true, wantRead: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer, dest := newProgressBatchSnapshotWriter(t)
			progress := snapshotRangeProgress()
			progress.Ranges[0].Key = []string{"7"}
			progress.Ranges[1].Key, progress.Ranges[1].Complete = []string{"100"}, true
			interrupted := errors.New("snapshot interrupted")
			require.ErrorIs(t, writer.write(t.Context(), mustClosedRecords(
				softMarker(), directSnapshotBatch(t, "", 7), directSnapshotBatch(t, "", 100),
				snapshotRangeProgressResult(progress), source.RecordBatchResult{Err: interrupted},
			), func() {}), interrupted)

			manager, err := NewCDCStateManager(dest, "stream-soft-replacement", "raw.items", "")
			require.NoError(t, err)
			require.NoError(t, manager.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"))
			require.NoError(t, manager.BeginRun(t.Context(), false))
			if tc.invalidProgress {
				entry, state, ok := manager.latestSnapshotProgress("public.items")
				require.True(t, ok)
				state.Version = 99
				state.Sequence++
				encoded, err := json.Marshal(state)
				require.NoError(t, err)
				entry.EventID, entry.Position = "unsupported-snapshot-progress", string(encoded)
				manager.entries = append(manager.entries, entry)
			}
			job := directSnapshotBatchJob(dest, nil)
			job.CDCStateManager = manager
			job.Config.FullRefresh, job.Config.KeepStaging = tc.fullRefresh, tc.keepStaging
			job.Config.CDCResumeLSN = tc.resumeLSN
			src := job.Table.(*fakeSourceTable)
			readStopped := errors.New("stop after capturing read options")
			src.readErr = readStopped

			err = (&MergeStrategy{}).Execute(t.Context(), job)

			require.Equal(t, tc.wantRead, src.readCalled)
			if !tc.wantRead {
				require.ErrorContains(t, err, "read snapshot progress for public.items")
				require.ErrorContains(t, err, "invalid or unsupported saved snapshot progress")
				return
			}
			require.ErrorIs(t, err, readStopped)
			require.Equal(t, tc.fullRefresh, src.readOpts.FullRefresh)
			require.Equal(t, tc.resumeLSN, src.readOpts.CDCResumeLSN)
			require.True(t, src.readOpts.CDCManagedState)
			require.Equal(t, !tc.keepStaging, src.readOpts.CDCSnapshotProgress)
			if tc.wantResume {
				require.NotNil(t, src.readOpts.CDCSnapshotResume)
				require.Equal(t, progress, *src.readOpts.CDCSnapshotResume)
			} else {
				require.Nil(t, src.readOpts.CDCSnapshotResume)
			}
		})
	}
}
