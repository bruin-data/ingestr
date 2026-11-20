package postgres_cdc

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestReadWALRetention(t *testing.T) {
	retained, safe, limit, unlimited := float64(128), int64(896), int64(1024), int64(-1)
	reserved, unreserved, lost := "reserved", "unreserved", "lost"
	zero := int64(0)
	for _, test := range []struct {
		name     string
		retained *float64
		safe     *int64
		limit    *int64
		status   *string
	}{
		{name: "bounded", retained: &retained, safe: &safe, limit: &limit, status: &reserved},
		{name: "unlimited", retained: &retained, limit: &unlimited, status: &reserved},
		{name: "lost", retained: &retained, limit: &limit, status: &lost},
		{name: "no safe WAL remains", retained: &retained, safe: &zero, limit: &limit, status: &unreserved},
		{name: "no WAL reserved", limit: &limit},
		{name: "retention columns unavailable", retained: &retained},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := snapshotResumeTestQuery(func(_ context.Context, sql string, args ...any) pgx.Row {
				require.Equal(t, []any{"actual_slot"}, args)
				require.Contains(t, sql, "s.database = current_database()")
				return snapshotResumeTestRow(func(dest ...any) error {
					*dest[0].(**float64) = test.retained
					*dest[1].(**int64) = test.safe
					*dest[2].(**int64) = test.limit
					*dest[3].(**string) = test.status
					return nil
				})
			})
			snapshot, err := readWALRetention(t.Context(), query, "actual_slot")
			require.NoError(t, err)
			require.Equal(t, "postgres_cdc", snapshot.Source)
			require.Equal(t, "actual_slot", snapshot.Slot)
			require.Equal(t, test.retained, snapshot.RetainedBytes)
			require.Equal(t, test.safe, snapshot.SafeBytes)
			require.Equal(t, test.limit, snapshot.MaxKeepBytes)
			if test.status == nil {
				require.Empty(t, snapshot.Status)
			} else {
				require.Equal(t, *test.status, snapshot.Status)
			}
		})
	}
}

func TestReadWALRetentionFailureDiscardsPartialObservation(t *testing.T) {
	for _, queryErr := range []error{pgx.ErrNoRows, context.DeadlineExceeded, errors.New("permission denied")} {
		t.Run(queryErr.Error(), func(t *testing.T) {
			query := snapshotResumeTestQuery(func(context.Context, string, ...any) pgx.Row {
				return snapshotResumeTestRow(func(dest ...any) error {
					partial := float64(128)
					*dest[0].(**float64) = &partial
					return queryErr
				})
			})
			snapshot, err := readWALRetention(t.Context(), query, "missing_slot")
			require.ErrorIs(t, err, queryErr)
			require.Equal(t, source.WALRetentionSnapshot{Source: "postgres_cdc", Slot: "missing_slot"}, snapshot)
		})
	}
}

func TestWALRetentionUsesResolvedSlotAndClearsOnClose(t *testing.T) {
	src := NewPostgresCDCSource()
	src.cdcConfig.SlotName = "configured_slot"
	snapshot, err := src.WALRetention(t.Context())
	require.NoError(t, err)
	require.Equal(t, source.WALRetentionSnapshot{Source: "postgres_cdc"}, snapshot)

	src.setWALRetentionSlot("resolved_legacy_slot")
	snapshot, err = src.WALRetention(t.Context())
	require.ErrorContains(t, err, "not connected")
	require.Equal(t, "resolved_legacy_slot", snapshot.Slot)

	require.NoError(t, src.Close(t.Context()))
	snapshot, err = src.WALRetention(t.Context())
	require.NoError(t, err)
	require.Equal(t, source.WALRetentionSnapshot{Source: "postgres_cdc"}, snapshot)
}

func TestWALRetentionClearsOnFailedReconnect(t *testing.T) {
	src := NewPostgresCDCSource()
	src.setWALRetentionSlot("previous_slot")
	err := src.Connect(t.Context(), "://invalid")
	require.Error(t, err)
	snapshot, err := src.WALRetention(t.Context())
	require.NoError(t, err)
	require.Equal(t, source.WALRetentionSnapshot{Source: "postgres_cdc"}, snapshot)
}
