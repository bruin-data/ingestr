//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	postgresdest "github.com/bruin-data/ingestr/pkg/destination/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

func TestPostgresDestinationCDCMergeDoesNotRegressTargetLSN(t *testing.T) {
	if pgDest.uri == "" {
		t.Skip("shared postgres destination container not available")
	}

	ctx := t.Context()
	targetTable := "public.cdc_lsn_target_" + uniqueSuffix()
	stagingTable := "public.cdc_lsn_staging_" + uniqueSuffix()
	dest := postgresdest.NewPostgresDestination()
	require.NoError(t, dest.Connect(ctx, pgDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s, %s", targetTable, stagingTable))
	})

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id bigint PRIMARY KEY,
			payload text,
			_cdc_lsn text,
			_cdc_deleted boolean,
			_cdc_synced_at timestamptz
		);
		CREATE TABLE %s (
			id bigint,
			payload text,
			_cdc_lsn text,
			_cdc_deleted boolean,
			_cdc_synced_at timestamptz,
			_cdc_unchanged_cols jsonb
		);
		INSERT INTO %s VALUES
			(1, 'newer-active', '00000000/00000030', false, '2026-01-03'),
			(2, 'newer-deleted', '00000000/00000030', true, '2026-01-03'),
			(3, 'legacy', NULL, false, '2026-01-01'),
			(6, 'same-active', '00000000/00000010', false, '2026-01-01'),
			(7, 'same-deleted', '00000000/00000010', true, '2026-01-01'),
			(8, 'tie-delete', '00000000/00000010', false, '2026-01-01'),
			(9, 'toast-newer', '00000000/00000030', false, '2026-01-03');
		INSERT INTO %s VALUES
			(1, 'stale-active', '00000000/00000020', false, '2026-01-02', '[]'),
			(1, NULL, '00000000/00000025', true, '2026-01-02', '[]'),
			(2, 'stale-resurrection', '00000000/00000020', false, '2026-01-02', '[]'),
			(3, 'first-cdc-update', '00000000/00000010', false, '2026-01-02', '[]'),
			(4, 'first-insert', '00000000/00000010', false, '2026-01-02', '[]'),
			(5, 'insert-then-delete', '00000000/00000010', false, '2026-01-02', '[]'),
			(5, NULL, '00000000/00000010', true, '2026-01-02', '[]'),
			(6, 'same-replay', '00000000/00000010', false, '2026-01-02', '[]'),
			(7, 'same-resurrection', '00000000/00000010', false, '2026-01-02', '[]'),
			(8, NULL, '00000000/00000010', true, '2026-01-02', '[]'),
			(9, NULL, '00000000/00000020', false, '2026-01-02', '["payload"]');
	`, targetTable, stagingTable, targetTable, stagingTable)))

	opts := destination.MergeOptions{
		TargetTable:  targetTable,
		StagingTable: stagingTable,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
	}
	require.NoError(t, dest.MergeTable(ctx, opts))

	expected := map[int64]struct {
		payload string
		lsn     string
		deleted bool
		synced  string
	}{
		1: {"newer-active", "00000000/00000030", false, "2026-01-03"},
		2: {"newer-deleted", "00000000/00000030", true, "2026-01-03"},
		3: {"first-cdc-update", "00000000/00000010", false, "2026-01-02"},
		4: {"first-insert", "00000000/00000010", false, "2026-01-02"},
		5: {"insert-then-delete", "00000000/00000010", true, "2026-01-02"},
		6: {"same-active", "00000000/00000010", false, "2026-01-01"},
		7: {"same-deleted", "00000000/00000010", true, "2026-01-01"},
		8: {"tie-delete", "00000000/00000010", true, "2026-01-02"},
		9: {"toast-newer", "00000000/00000030", false, "2026-01-03"},
	}
	for id, want := range expected {
		var payload, lsn, synced string
		var deleted bool
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT payload, COALESCE(_cdc_lsn, ''), _cdc_deleted, to_char(_cdc_synced_at, 'YYYY-MM-DD')
			FROM %s WHERE id = $1
		`, targetTable), id).Scan(&payload, &lsn, &deleted, &synced))
		require.Equal(t, want.payload, payload, "id %d payload", id)
		require.Equal(t, want.lsn, lsn, "id %d LSN", id)
		require.Equal(t, want.deleted, deleted, "id %d deleted", id)
		require.Equal(t, want.synced, synced, "id %d synced timestamp", id)
	}

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`
		TRUNCATE %s;
		INSERT INTO %s VALUES (1, 'newest', '00000000/00000040', false, '2026-01-04', '[]');
	`, stagingTable, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	var payload, lsn string
	var deleted bool
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT payload, _cdc_lsn, _cdc_deleted FROM %s WHERE id = 1`, targetTable)).Scan(&payload, &lsn, &deleted))
	require.Equal(t, "newest", payload)
	require.Equal(t, "00000000/00000040", lsn)
	require.False(t, deleted)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`
		TRUNCATE %s;
		INSERT INTO %s VALUES (1, NULL, '00000000/00000040', true, '2026-01-04', '[]');
	`, stagingTable, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT payload, _cdc_lsn, _cdc_deleted FROM %s WHERE id = 1`, targetTable)).Scan(&payload, &lsn, &deleted))
	require.Equal(t, "newest", payload)
	require.Equal(t, "00000000/00000040", lsn)
	require.True(t, deleted)
}
