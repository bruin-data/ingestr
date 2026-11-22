package mssql

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func expectCTRefreshSnapshot(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery("snapshot_isolation_state").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectQuery("CHANGE_TRACKING_CURRENT_VERSION").WillReturnRows(ctVersionRows(7))
	mock.ExpectQuery("FROM \\[dbo\\]\\.\\[items\\]").WillReturnRows(rows)
}

func expectCTRefreshPoll(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery("snapshot_isolation_state").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectQuery("CHANGE_TRACKING_MIN_VALID_VERSION").WillReturnRows(ctVersionRows(0))
	mock.ExpectQuery("CHANGE_TRACKING_CURRENT_VERSION").WillReturnRows(ctVersionRows(9))
	mock.ExpectQuery("CHANGETABLE").WithArgs(int64(7), int64(9)).WillReturnRows(rows)
	mock.ExpectCommit()
	expectCTIsolationReset(mock)
	mock.ExpectQuery("snapshot_isolation_state").WillReturnError(errors.New("poll stopped"))
}

func TestChangeTrackingManagedRefreshSnapshotsThenStreams(t *testing.T) {
	for _, tc := range []struct {
		name        string
		empty       bool
		fullRefresh bool
	}{
		{name: "new refresh", fullRefresh: true},
		{name: "empty refresh", fullRefresh: true, empty: true},
		{name: "interrupted refresh retry"},
		{name: "empty refresh retry", empty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			src := &MSSQLChangeTrackingSource{
				MSSQLSource: MSSQLSource{db: db},
				ctConfig:    ctConfig{PollInterval: time.Millisecond},
			}
			rows := ctChangeRows(1, 7)
			if tc.empty {
				rows = ctEmptyChangeRows()
			}
			expectCTRefreshSnapshot(mock, rows)
			mock.ExpectCommit()
			expectCTIsolationReset(mock)
			expectCTRefreshPoll(mock, ctChangeRows(2, 9))

			results, err := newCTStreamTestTable(src).Read(t.Context(), source.ReadOptions{
				PageSize: 100, Streaming: true, CDCSnapshotReplace: true, CDCManagedState: true, FullRefresh: tc.fullRefresh,
			})
			require.NoError(t, err)
			var events []string
			var tokens []source.CDCStateCommitToken
			for res := range results {
				switch {
				case res.Err != nil:
					require.ErrorContains(t, res.Err, "poll stopped")
					events = append(events, "stop")
				case res.Truncate:
					events = append(events, "truncate")
				case res.Batch != nil:
					events = append(events, "data")
					assert.EqualValues(t, 1, res.Batch.NumRows())
					res.Batch.Release()
				case res.CommitToken != nil:
					token, ok := res.CommitToken.(source.CDCStateCommitToken)
					require.True(t, ok)
					tokens = append(tokens, token)
					if len(token.SnapshotPositions) > 0 {
						events = append(events, "snapshot complete")
					} else {
						events = append(events, "checkpoint")
					}
				}
			}
			wantEvents := []string{"truncate", "data", "snapshot complete", "data", "checkpoint", "stop"}
			if tc.empty {
				wantEvents = []string{"truncate", "snapshot complete", "data", "checkpoint", "stop"}
			}
			require.Equal(t, wantEvents, events)
			require.Len(t, tokens, 2)
			assert.Equal(t, formatCTVersion(7), tokens[0].Position)
			assert.Equal(t, map[string]string{"dbo.items": formatCTVersion(7)}, tokens[0].SnapshotPositions)
			assert.Equal(t, formatCTVersion(9), tokens[1].Position)
			assert.Empty(t, tokens[1].SnapshotPositions)

			lag, ok := src.ReplicationLag()
			require.True(t, ok)
			assert.Equal(t, formatCTVersion(0), lag.DurablePosition, "reading a snapshot does not acknowledge its destination writes")
			require.NoError(t, src.CommitStream(t.Context(), tokens[0]))
			lag, ok = src.ReplicationLag()
			require.True(t, ok)
			assert.Equal(t, formatCTVersion(7), lag.DurablePosition)
			require.NoError(t, src.CommitStream(t.Context(), tokens[1]))
			lag, ok = src.ReplicationLag()
			require.True(t, ok)
			assert.True(t, lag.CaughtUp)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChangeTrackingFailedManagedSnapshotDoesNotCertifyPartialData(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	src := &MSSQLChangeTrackingSource{MSSQLSource: MSSQLSource{db: db}}
	expectCTRefreshSnapshot(mock, ctChangeRows(1, 7))
	mock.ExpectCommit().WillReturnError(errors.New("snapshot commit failed"))
	expectCTIsolationReset(mock)

	results, err := newCTStreamTestTable(src).Read(t.Context(), source.ReadOptions{
		PageSize: 100, Streaming: true, CDCSnapshotReplace: true, CDCManagedState: true, FullRefresh: true,
	})
	require.NoError(t, err)
	var resets, rows, failures int
	for res := range results {
		assert.Nil(t, res.CommitToken, "partial snapshot rows cannot authorize resume")
		if res.Truncate {
			resets++
		}
		if res.Batch != nil {
			rows += int(res.Batch.NumRows())
			res.Batch.Release()
		}
		if res.Err != nil {
			failures++
			assert.ErrorContains(t, res.Err, "snapshot commit failed")
		}
	}
	assert.Equal(t, 1, resets)
	assert.Equal(t, 1, rows)
	assert.Equal(t, 1, failures)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChangeTrackingManagedResumeDoesNotResetOrRestampEmptyTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	src := &MSSQLChangeTrackingSource{
		MSSQLSource: MSSQLSource{db: db},
		ctConfig:    ctConfig{PollInterval: time.Millisecond},
	}
	expectCTRefreshPoll(mock, ctEmptyChangeRows())

	results, err := newCTStreamTestTable(src).Read(t.Context(), source.ReadOptions{
		PageSize: 100, Streaming: true, CDCSnapshotReplace: true, CDCManagedState: true, CDCResumeLSN: formatCTVersion(7),
	})
	require.NoError(t, err)
	var checkpoints, failures int
	for res := range results {
		assert.False(t, res.Truncate)
		if res.Batch != nil {
			res.Batch.Release()
			t.Error("managed empty polls must advance metadata without writing synthetic rows")
		}
		if res.CommitToken != nil {
			checkpoints++
			token, ok := res.CommitToken.(source.CDCStateCommitToken)
			require.True(t, ok)
			assert.Equal(t, formatCTVersion(9), token.Position)
			assert.Empty(t, token.SnapshotPositions)
		}
		if res.Err != nil {
			failures++
			assert.ErrorContains(t, res.Err, "poll stopped")
		}
	}
	assert.Equal(t, 1, checkpoints)
	assert.Equal(t, 1, failures)
	lag, ok := src.ReplicationLag()
	require.True(t, ok)
	assert.Equal(t, formatCTVersion(7), lag.DurablePosition)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChangeTrackingCommitStreamAcceptsManagedAndLegacyTokens(t *testing.T) {
	src := NewMSSQLChangeTrackingSource()
	src.lag.streaming.Store(true)
	require.NoError(t, src.CommitStream(t.Context(), source.CDCStateCommitToken{Position: formatCTVersion(0)}))
	require.NoError(t, src.CommitStream(t.Context(), source.CDCStateCommitToken{Position: formatCTVersion(7)}))
	require.NoError(t, src.CommitStream(t.Context(), ctCommitToken{version: 9}))
	for _, position := range []string{"", "invalid"} {
		require.ErrorContains(t, src.CommitStream(t.Context(), source.CDCStateCommitToken{Position: position}), "invalid")
	}
	lag, ok := src.ReplicationLag()
	require.True(t, ok)
	assert.Equal(t, formatCTVersion(9), lag.DurablePosition)
}

func TestChangeTrackingManagedBatchState(t *testing.T) {
	for _, tc := range []struct {
		name        string
		resume      bool
		empty       bool
		fullRefresh bool
		stagingSwap bool
	}{
		{name: "snapshot and changes"},
		{name: "empty snapshot and changes", empty: true},
		{name: "empty full refresh", empty: true, fullRefresh: true},
		{name: "staged full refresh", fullRefresh: true, stagingSwap: true},
		{name: "resume", resume: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			src := &MSSQLChangeTrackingSource{MSSQLSource: MSSQLSource{db: db}}
			opts := source.ReadOptions{
				PageSize: 100, CDCSnapshotReplace: !tc.stagingSwap, CDCManagedState: true, FullRefresh: tc.fullRefresh,
			}
			if tc.resume {
				opts.CDCResumeLSN = formatCTVersion(7)
			} else {
				rows := ctChangeRows(1, 7)
				if tc.empty {
					rows = ctEmptyChangeRows()
				}
				expectCTRefreshSnapshot(mock, rows)
				mock.ExpectCommit()
				expectCTIsolationReset(mock)
			}
			if !tc.fullRefresh {
				mock.ExpectQuery("snapshot_isolation_state").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(1))
				mock.ExpectBegin()
				mock.ExpectQuery("CHANGE_TRACKING_MIN_VALID_VERSION").WillReturnRows(ctVersionRows(0))
				mock.ExpectQuery("CHANGE_TRACKING_CURRENT_VERSION").WillReturnRows(ctVersionRows(9))
				mock.ExpectQuery("CHANGETABLE").WithArgs(int64(7), int64(9)).WillReturnRows(ctChangeRows(2, 9))
				mock.ExpectCommit()
				expectCTIsolationReset(mock)
			}

			results, err := newCTStreamTestTable(src).Read(t.Context(), opts)
			require.NoError(t, err)
			var truncates int
			for res := range results {
				require.NoError(t, res.Err)
				assert.Nil(t, res.CommitToken, "batch state is exposed only after the complete read succeeds")
				if res.Truncate {
					truncates++
				}
				if res.Batch != nil {
					res.Batch.Release()
				}
			}
			if tc.resume || tc.stagingSwap {
				assert.Zero(t, truncates)
			} else {
				assert.Equal(t, 1, truncates)
			}
			state := src.CDCState()
			if tc.fullRefresh {
				assert.Equal(t, formatCTVersion(7), state.Position)
			} else {
				assert.Equal(t, formatCTVersion(9), state.Position)
			}
			if tc.resume {
				assert.Empty(t, state.SnapshotPositions)
			} else {
				assert.Equal(t, map[string]string{"dbo.items": formatCTVersion(7)}, state.SnapshotPositions)
				state.SnapshotPositions["dbo.items"] = "changed"
				assert.Equal(t, formatCTVersion(7), src.CDCState().SnapshotPositions["dbo.items"], "callers must not mutate the source's completed certificate")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChangeTrackingManagedBatchFailureDoesNotExposePreviousOrPartialState(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	src := &MSSQLChangeTrackingSource{MSSQLSource: MSSQLSource{db: db}}
	table := newCTStreamTestTable(src)
	opts := source.ReadOptions{PageSize: 100, CDCSnapshotReplace: true, CDCManagedState: true, FullRefresh: true}

	expectCTRefreshSnapshot(mock, ctEmptyChangeRows())
	mock.ExpectCommit()
	expectCTIsolationReset(mock)
	results, err := table.Read(t.Context(), opts)
	require.NoError(t, err)
	for res := range results {
		require.NoError(t, res.Err)
		if res.Batch != nil {
			res.Batch.Release()
		}
	}
	require.Equal(t, formatCTVersion(7), src.CDCState().Position)

	opts.FullRefresh = false
	expectCTRefreshSnapshot(mock, ctChangeRows(1, 7))
	mock.ExpectCommit()
	expectCTIsolationReset(mock)
	mock.ExpectQuery("snapshot_isolation_state").WillReturnError(errors.New("poll failed"))
	results, err = table.Read(t.Context(), opts)
	require.NoError(t, err)
	var failures int
	for res := range results {
		if res.Batch != nil {
			res.Batch.Release()
		}
		if res.Err != nil {
			failures++
			assert.ErrorContains(t, res.Err, "poll failed")
		}
	}
	require.Equal(t, 1, failures)
	require.Equal(t, source.CDCStateCommitToken{}, src.CDCState())
	require.NoError(t, mock.ExpectationsWereMet())
}
