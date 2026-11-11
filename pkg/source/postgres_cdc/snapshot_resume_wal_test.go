package postgres_cdc

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type snapshotResumeTestQuery func(context.Context, string, ...any) pgx.Row

func (query snapshotResumeTestQuery) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return query(ctx, sql, args...)
}

type snapshotResumeTestRow func(...any) error

func (row snapshotResumeTestRow) Scan(dest ...any) error { return row(dest...) }

func TestSnapshotResumeWALAvailable(t *testing.T) {
	queryErr := errors.New("slot catalog unavailable")
	for _, test := range []struct {
		name      string
		usable    bool
		confirmed string
		restart   string
		err       error
		available bool
	}{
		{name: "retains boundary", usable: true, confirmed: "0/20", restart: "0/10", available: true},
		{name: "exact boundary", usable: true, confirmed: "0/30", restart: "0/30", available: true},
		{name: "confirmed beyond boundary", usable: true, confirmed: "0/31", restart: "0/10"},
		{name: "restart beyond boundary", usable: true, confirmed: "0/20", restart: "0/31"},
		{name: "unusable slot", confirmed: "0/20", restart: "0/10"},
		{name: "missing confirmed position", usable: true, restart: "0/10"},
		{name: "missing restart position", usable: true, confirmed: "0/20"},
		{name: "malformed confirmed position", usable: true, confirmed: "invalid", restart: "0/10"},
		{name: "malformed restart position", usable: true, confirmed: "0/20", restart: "invalid"},
		{name: "missing slot", err: pgx.ErrNoRows},
		{name: "catalog failure", err: queryErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := snapshotResumeTestQuery(func(_ context.Context, _ string, args ...any) pgx.Row {
				require.Equal(t, []any{"resume_slot"}, args)
				return snapshotResumeTestRow(func(dest ...any) error {
					if test.err != nil {
						return test.err
					}
					*dest[0].(*bool) = test.usable
					*dest[1].(*string) = test.confirmed
					*dest[2].(*string) = test.restart
					return nil
				})
			})
			available, err := snapshotResumeWALAvailable(t.Context(), query, "resume_slot", 0x30)
			if errors.Is(test.err, queryErr) {
				require.ErrorIs(t, err, queryErr, "catalog failures must not trigger destructive recovery")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.available, available)
		})
	}
	available, err := snapshotResumeWALAvailable(t.Context(), nil, "resume_slot", 0)
	require.NoError(t, err)
	require.False(t, available)
}

func TestSnapshotResumeRetainsOriginalWALBoundary(t *testing.T) {
	reader := NewMultiTableCDCReader(nil, []source.SourceTableInfo{{Name: "public.anchor"}, {Name: "public.items"}}, CDCConfig{}, map[string]string{
		"public.anchor": "0/50",
	}, "")
	reader.snapshotResumes = map[string]source.CDCSnapshotProgress{
		"public.items": {Boundary: "0/20", Key: `["900"]`},
	}
	needsSnapshot, start := reader.determineResumeStrategy()
	require.False(t, needsSnapshot)
	require.Equal(t, pglogrepl.LSN(0x20), start)
	require.Equal(t, pglogrepl.LSN(0x50), reader.GetProcessedLSN("public.anchor"))
	require.Zero(t, reader.GetProcessedLSN("public.items"), "unfinished snapshot must still be selected for backfill")
	require.Equal(t, []source.SourceTableInfo{{Name: "public.items"}}, reader.tablesWithoutResumeState())

	reader.setSnapshotBoundary("public.items", 0x20)
	require.False(t, reader.ShouldFilterChange("public.items", 0x20), "boundary transactions remain eligible for repair")
	require.False(t, reader.ShouldFilterChange("public.items", 0x30), "changes behind saved keys must survive catch-up filtering")
	require.True(t, reader.ShouldFilterChange("public.anchor", 0x30), "completed tables retain their own durable filter")
}

func TestSnapshotResumeUsesOldestRequiredWAL(t *testing.T) {
	reader := NewMultiTableCDCReader(nil, nil, CDCConfig{}, map[string]string{"public.anchor": "0/10"}, "")
	reader.snapshotResumes = map[string]source.CDCSnapshotProgress{
		"public.first":  {Boundary: "0/30"},
		"public.second": {Boundary: "0/20"},
	}
	needsSnapshot, start := reader.determineResumeStrategy()
	require.False(t, needsSnapshot)
	require.Equal(t, pglogrepl.LSN(0x10), start)
}

func TestSnapshotResumeInvalidBoundaryReplacesSnapshot(t *testing.T) {
	for _, boundary := range []string{"", "invalid", "0/0"} {
		t.Run(boundary, func(t *testing.T) {
			reader := NewMultiTableCDCReader(nil, nil, CDCConfig{}, map[string]string{"public.anchor": "0/50"}, "")
			reader.snapshotResumes = map[string]source.CDCSnapshotProgress{"public.items": {Boundary: boundary}}
			needsSnapshot, start := reader.determineResumeStrategy()
			require.True(t, needsSnapshot)
			require.Zero(t, start)
			require.Empty(t, reader.processedLSNs)
		})
	}
}
