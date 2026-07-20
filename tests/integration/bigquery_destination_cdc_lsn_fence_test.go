//go:build integration

package integration

import (
	"fmt"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

func TestBigQueryDestinationCDCMergeDoesNotRegressTargetLSN(t *testing.T) {
	dest, client, project, dataset := bqDedupSetup(t)
	ctx := t.Context()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	targetTable := "cdc_lsn_target_" + suffix
	stagingTable := "cdc_lsn_staging_" + suffix
	defer bqDropTables(ctx, client, dataset, stagingTable, targetTable)
	defer func() { _ = dest.Close(ctx) }()

	setup := []string{
		fmt.Sprintf(`CREATE TABLE %s (
			id INT64,
			payload STRING,
			_cdc_lsn STRING,
			_cdc_deleted BOOL,
			_cdc_synced_at TIMESTAMP
		)`, bqQualifiedTable(project, dataset, targetTable)),
		fmt.Sprintf(`CREATE TABLE %s (
			id INT64,
			payload STRING,
			_cdc_lsn STRING,
			_cdc_deleted BOOL,
			_cdc_synced_at TIMESTAMP,
			_cdc_unchanged_cols STRING
		)`, bqQualifiedTable(project, dataset, stagingTable)),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'newer-active', '00000000000000000030', false, TIMESTAMP '2026-01-03 00:00:00 UTC'),
			(2, 'newer-deleted', '00000000000000000030', true, TIMESTAMP '2026-01-03 00:00:00 UTC'),
			(3, 'legacy', NULL, false, TIMESTAMP '2026-01-01 00:00:00 UTC'),
			(6, 'same-active', '00000000000000000010', false, TIMESTAMP '2026-01-01 00:00:00 UTC'),
			(7, 'same-deleted', '00000000000000000010', true, TIMESTAMP '2026-01-01 00:00:00 UTC'),
			(8, 'tie-delete', '00000000000000000010', false, TIMESTAMP '2026-01-01 00:00:00 UTC'),
			(9, 'toast-newer', '00000000000000000030', false, TIMESTAMP '2026-01-03 00:00:00 UTC'),
			(11, 'known-lsn', '00000000000000000030', false, TIMESTAMP '2026-01-03 00:00:00 UTC')`, bqQualifiedTable(project, dataset, targetTable)),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'stale-active', '00000000000000000020', false, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(1, NULL, '00000000000000000025', true, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(2, 'stale-resurrection', '00000000000000000020', false, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(3, 'first-cdc-update', '00000000000000000010', false, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(4, 'first-insert', '00000000000000000010', false, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(5, NULL, '00000000000000000010', true, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(6, 'same-replay', '00000000000000000010', false, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(7, 'same-resurrection', '00000000000000000010', false, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(8, NULL, '00000000000000000010', true, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(9, NULL, '00000000000000000020', false, TIMESTAMP '2026-01-02 00:00:00 UTC', '["payload"]'),
			(10, 'insert-then-delete', '00000000000000000010', false, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(10, NULL, '00000000000000000010', true, TIMESTAMP '2026-01-02 00:00:00 UTC', '[]'),
			(11, 'null-lsn-regression', NULL, false, TIMESTAMP '2026-01-04 00:00:00 UTC', '[]')`, bqQualifiedTable(project, dataset, stagingTable)),
	}
	for _, statement := range setup {
		require.NoError(t, dest.Exec(ctx, statement))
	}

	opts := destination.MergeOptions{
		TargetTable:  dataset + "." + targetTable,
		StagingTable: dataset + "." + stagingTable,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
	}
	require.NoError(t, dest.MergeTable(ctx, opts))

	expected := map[int64]bigQueryCDCRow{
		1:  {"newer-active", "00000000000000000030", false, "2026-01-03"},
		2:  {"newer-deleted", "00000000000000000030", true, "2026-01-03"},
		3:  {"first-cdc-update", "00000000000000000010", false, "2026-01-02"},
		4:  {"first-insert", "00000000000000000010", false, "2026-01-02"},
		6:  {"same-active", "00000000000000000010", false, "2026-01-01"},
		7:  {"same-deleted", "00000000000000000010", true, "2026-01-01"},
		8:  {"tie-delete", "00000000000000000010", true, "2026-01-02"},
		9:  {"toast-newer", "00000000000000000030", false, "2026-01-03"},
		10: {"insert-then-delete", "00000000000000000010", true, "2026-01-02"},
		11: {"known-lsn", "00000000000000000030", false, "2026-01-03"},
	}
	rows := bqRunQuery(t, ctx, client, fmt.Sprintf(`
		SELECT id, payload, COALESCE(_cdc_lsn, ''), _cdc_deleted, FORMAT_TIMESTAMP('%%F', _cdc_synced_at)
		FROM %s ORDER BY id
	`, bqQualifiedTable(project, dataset, targetTable)))
	require.Len(t, rows, len(expected))
	for _, row := range rows {
		id := row[0].(int64)
		want := expected[id]
		require.Equal(t, want.payload, row[1], "id %d payload", id)
		require.Equal(t, want.lsn, row[2], "id %d LSN", id)
		require.Equal(t, want.deleted, row[3], "id %d deleted", id)
		require.Equal(t, want.synced, row[4], "id %d synced timestamp", id)
	}

	require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+bqQualifiedTable(project, dataset, stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'newest', '00000000000000000040', false, TIMESTAMP '2026-01-04 00:00:00 UTC', '[]')`, bqQualifiedTable(project, dataset, stagingTable))))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertBigQueryCDCRow(t, client, project, dataset, targetTable, bigQueryCDCRow{"newest", "00000000000000000040", false, ""})

	require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+bqQualifiedTable(project, dataset, stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, NULL, '00000000000000000040', true, TIMESTAMP '2026-01-04 00:00:00 UTC', '[]')`, bqQualifiedTable(project, dataset, stagingTable))))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertBigQueryCDCRow(t, client, project, dataset, targetTable, bigQueryCDCRow{"newest", "00000000000000000040", true, ""})

	require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+bqQualifiedTable(project, dataset, stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, NULL, '00000000000000000050', false, TIMESTAMP '2026-01-05 00:00:00 UTC', '["payload"]')`, bqQualifiedTable(project, dataset, stagingTable))))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertBigQueryCDCRow(t, client, project, dataset, targetTable, bigQueryCDCRow{"newest", "00000000000000000050", false, ""})

	require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+bqQualifiedTable(project, dataset, stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'newer-outside-predicate', '00000000000000000060', false, TIMESTAMP '2026-01-06 00:00:00 UTC', '[]')`, bqQualifiedTable(project, dataset, stagingTable))))
	opts.IncrementalPredicate = "t.`id` > 100"
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertBigQueryCDCRow(t, client, project, dataset, targetTable, bigQueryCDCRow{"newest", "00000000000000000050", false, ""})
	count := bqRunQuery(t, ctx, client, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id = 1", bqQualifiedTable(project, dataset, targetTable)))
	require.EqualValues(t, 1, count[0][0])
}

type bigQueryCDCRow struct {
	payload string
	lsn     string
	deleted bool
	synced  string
}

func assertBigQueryCDCRow(t *testing.T, client *bigquery.Client, project, dataset, table string, want bigQueryCDCRow) {
	t.Helper()
	rows := bqRunQuery(t, t.Context(), client, fmt.Sprintf("SELECT payload, _cdc_lsn, _cdc_deleted FROM %s WHERE id = 1", bqQualifiedTable(project, dataset, table)))
	require.Len(t, rows, 1)
	require.Equal(t, want.payload, rows[0][0])
	require.Equal(t, want.lsn, rows[0][1])
	require.Equal(t, want.deleted, rows[0][2])
}

func bqQualifiedTable(project, dataset, table string) string {
	return fmt.Sprintf("`%s.%s.%s`", project, dataset, table)
}
