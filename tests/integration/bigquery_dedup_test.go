package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/pkg/destination"
	bqdest "github.com/bruin-data/ingestr/pkg/destination/bigquery"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/iterator"
)

// These tests validate BigQuery's swap, merge, and delete-insert dedup behavior
// against a real BigQuery dataset. The dedup contract being tested:
//
//   - Swap (replace strategy):    one row per primary key, latest by IncrementalKey wins
//   - Merge:                      one row per primary key in upserted batch
//   - DeleteInsert:               one row per primary key in re-inserted window
//
// Required env vars:
//   GONG_TEST_BIGQUERY_URI      e.g. bigquery://my-project?credentials_path=...
//   GONG_TEST_BIGQUERY_PROJECT  e.g. my-project
//
// A temporary dataset is created per test run and dropped at the end, so no
// pre-existing dataset is required.

func bqDedupSetup(t *testing.T) (*bqdest.BigQueryDestination, *bigquery.Client, string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	uri := os.Getenv("GONG_TEST_BIGQUERY_URI")
	project := os.Getenv("GONG_TEST_BIGQUERY_PROJECT")
	if uri == "" || project == "" {
		t.Skip("set GONG_TEST_BIGQUERY_URI and GONG_TEST_BIGQUERY_PROJECT to run")
	}

	ctx := context.Background()

	client, err := bigquery.NewClient(ctx, project)
	require.NoError(t, err)

	// Create a per-test-run dataset so we don't depend on pre-existing infra
	// and so concurrent runs don't collide.
	dataset := fmt.Sprintf("gong_dedup_test_%d", time.Now().UnixNano())
	require.NoError(t, client.Dataset(dataset).Create(ctx, &bigquery.DatasetMetadata{
		Location: os.Getenv("GONG_TEST_BIGQUERY_LOCATION"),
	}), "failed to create test dataset %s", dataset)
	t.Cleanup(func() {
		_ = client.Dataset(dataset).DeleteWithContents(ctx)
	})

	dest := bqdest.NewBigQueryDestination()
	require.NoError(t, dest.Connect(ctx, uri))

	return dest, client, project, dataset
}

func bqRunQuery(t *testing.T, ctx context.Context, client *bigquery.Client, sql string) [][]bigquery.Value {
	t.Helper()
	q := client.Query(sql)
	it, err := q.Read(ctx)
	require.NoError(t, err, "query failed: %s", sql)
	var rows [][]bigquery.Value
	for {
		var row []bigquery.Value
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		require.NoError(t, err)
		rows = append(rows, row)
	}
	return rows
}

func bqDropTables(ctx context.Context, client *bigquery.Client, dataset string, tables ...string) {
	for _, tbl := range tables {
		_ = client.Dataset(dataset).Table(tbl).Delete(ctx)
	}
}

// TestBigQuery_SwapTable_DedupsByPrimaryKeyWithIncrementalKey verifies that
// SwapTable picks the row with the latest IncrementalKey value per primary key.
func TestBigQuery_SwapTable_DedupsByPrimaryKeyWithIncrementalKey(t *testing.T) {
	dest, client, project, dataset := bqDedupSetup(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	stagingTbl := fmt.Sprintf("dedup_swap_staging_%s", suffix)
	targetTbl := fmt.Sprintf("dedup_swap_target_%s", suffix)
	defer bqDropTables(ctx, client, dataset, stagingTbl, targetTbl)
	defer func() { _ = dest.Close(ctx) }()

	createStaging := fmt.Sprintf(
		"CREATE TABLE `%s.%s.%s` (id INT64, name STRING, updated_at TIMESTAMP)",
		project, dataset, stagingTbl,
	)
	require.NoError(t, dest.Exec(ctx, createStaging))

	insertStaging := fmt.Sprintf(
		`INSERT INTO `+"`%s.%s.%s`"+` VALUES
		(1, 'A-old',   TIMESTAMP '2024-01-01 10:00:00 UTC'),
		(1, 'A-newer', TIMESTAMP '2024-06-15 09:00:00 UTC'),
		(2, 'B-newer', TIMESTAMP '2024-07-20 12:00:00 UTC'),
		(2, 'B-old',   TIMESTAMP '2024-02-01 10:00:00 UTC'),
		(3, 'C-only',  TIMESTAMP '2024-03-01 10:00:00 UTC')`,
		project, dataset, stagingTbl,
	)
	require.NoError(t, dest.Exec(ctx, insertStaging))

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable:   dataset + "." + stagingTbl,
		TargetTable:    dataset + "." + targetTbl,
		PrimaryKeys:    []string{"id"},
		IncrementalKey: "updated_at",
	}))

	rows := bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT id, name FROM `%s.%s.%s` ORDER BY id", project, dataset, targetTbl,
	))
	require.Len(t, rows, 3, "expected one row per primary key after swap dedup")

	want := map[int64]string{1: "A-newer", 2: "B-newer", 3: "C-only"}
	for _, r := range rows {
		id := r[0].(int64)
		name := r[1].(string)
		require.Equal(t, want[id], name, "id=%d should dedup to row with latest updated_at", id)
	}
}

// TestBigQuery_SwapTable_DedupsByPrimaryKeyArbitraryTiebreaker verifies that
// when no IncrementalKey is provided, SwapTable still collapses duplicate PKs
// to one row per key (just non-deterministically).
func TestBigQuery_SwapTable_DedupsByPrimaryKeyArbitraryTiebreaker(t *testing.T) {
	dest, client, project, dataset := bqDedupSetup(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	stagingTbl := fmt.Sprintf("dedup_swap_arb_staging_%s", suffix)
	targetTbl := fmt.Sprintf("dedup_swap_arb_target_%s", suffix)
	defer bqDropTables(ctx, client, dataset, stagingTbl, targetTbl)
	defer func() { _ = dest.Close(ctx) }()

	createStaging := fmt.Sprintf(
		"CREATE TABLE `%s.%s.%s` (id INT64, name STRING)",
		project, dataset, stagingTbl,
	)
	require.NoError(t, dest.Exec(ctx, createStaging))

	insertStaging := fmt.Sprintf(`INSERT INTO `+"`%s.%s.%s`"+` VALUES
		(1, 'A'), (1, 'A-dup'),
		(2, 'B'), (2, 'B-dup'),
		(3, 'C')`, project, dataset, stagingTbl)
	require.NoError(t, dest.Exec(ctx, insertStaging))

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable: dataset + "." + stagingTbl,
		TargetTable:  dataset + "." + targetTbl,
		PrimaryKeys:  []string{"id"},
	}))

	rows := bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT COUNT(*) FROM `%s.%s.%s`", project, dataset, targetTbl,
	))
	require.Len(t, rows, 1)
	require.EqualValues(t, 3, rows[0][0], "expected one row per primary key")
}

// TestBigQuery_SwapTable_CompositePrimaryKey verifies dedup works correctly
// when the primary key is composite, requiring both columns to match.
func TestBigQuery_SwapTable_CompositePrimaryKey(t *testing.T) {
	dest, client, project, dataset := bqDedupSetup(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	stagingTbl := fmt.Sprintf("dedup_swap_comp_staging_%s", suffix)
	targetTbl := fmt.Sprintf("dedup_swap_comp_target_%s", suffix)
	defer bqDropTables(ctx, client, dataset, stagingTbl, targetTbl)
	defer func() { _ = dest.Close(ctx) }()

	createStaging := fmt.Sprintf(
		"CREATE TABLE `%s.%s.%s` (tenant_id INT64, user_id INT64, payload STRING)",
		project, dataset, stagingTbl,
	)
	require.NoError(t, dest.Exec(ctx, createStaging))

	insertStaging := fmt.Sprintf(`INSERT INTO `+"`%s.%s.%s`"+` VALUES
		(1, 100, 'a'), (1, 100, 'a-dup'),
		(1, 200, 'b'),
		(2, 100, 'c'), (2, 100, 'c-dup')`, project, dataset, stagingTbl)
	require.NoError(t, dest.Exec(ctx, insertStaging))

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable: dataset + "." + stagingTbl,
		TargetTable:  dataset + "." + targetTbl,
		PrimaryKeys:  []string{"tenant_id", "user_id"},
	}))

	rows := bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT COUNT(*) FROM `%s.%s.%s`", project, dataset, targetTbl,
	))
	require.Len(t, rows, 1)
	require.EqualValues(t, 3, rows[0][0],
		"expected 3 distinct (tenant_id, user_id) groups: (1,100), (1,200), (2,100)")
}

// TestBigQuery_MergeTable_DedupsDuplicateStagingPKs verifies that MergeTable
// upserts at most one row per primary key even when staging has duplicates.
func TestBigQuery_MergeTable_DedupsDuplicateStagingPKs(t *testing.T) {
	dest, client, project, dataset := bqDedupSetup(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	stagingTbl := fmt.Sprintf("dedup_merge_staging_%s", suffix)
	targetTbl := fmt.Sprintf("dedup_merge_target_%s", suffix)
	defer bqDropTables(ctx, client, dataset, stagingTbl, targetTbl)
	defer func() { _ = dest.Close(ctx) }()

	for _, tbl := range []string{stagingTbl, targetTbl} {
		require.NoError(t, dest.Exec(ctx, fmt.Sprintf(
			"CREATE TABLE `%s.%s.%s` (id INT64, name STRING)",
			project, dataset, tbl,
		)))
	}
	// Pre-existing target row to test that it stays untouched when staging has no PK match.
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(
		"INSERT INTO `%s.%s.%s` VALUES (99, 'pre-existing')",
		project, dataset, targetTbl,
	)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO `+"`%s.%s.%s`"+` VALUES
		(1, 'A'), (1, 'A-dup'),
		(2, 'B'), (2, 'B-dup'),
		(3, 'C')`, project, dataset, stagingTbl)))

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: dataset + "." + stagingTbl,
		TargetTable:  dataset + "." + targetTbl,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name"},
	}))

	rows := bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT COUNT(*) FROM `%s.%s.%s`", project, dataset, targetTbl,
	))
	require.Len(t, rows, 1)
	// 3 from staging (deduped from 5) + 1 pre-existing = 4
	require.EqualValues(t, 4, rows[0][0], "expected target to have 3 deduped rows + 1 pre-existing")

	// Pre-existing row must not be touched.
	rows = bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT name FROM `%s.%s.%s` WHERE id = 99", project, dataset, targetTbl,
	))
	require.Len(t, rows, 1)
	require.Equal(t, "pre-existing", rows[0][0])
}

// TestBigQuery_DeleteInsertTable_DedupsDuplicateStagingPKs verifies that
// DeleteInsertTable inserts one row per primary key from staging into target,
// even when staging has duplicate PKs in the same incremental window.
func TestBigQuery_DeleteInsertTable_DedupsDuplicateStagingPKs(t *testing.T) {
	dest, client, project, dataset := bqDedupSetup(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	stagingTbl := fmt.Sprintf("dedup_di_staging_%s", suffix)
	targetTbl := fmt.Sprintf("dedup_di_target_%s", suffix)
	defer bqDropTables(ctx, client, dataset, stagingTbl, targetTbl)
	defer func() { _ = dest.Close(ctx) }()

	for _, tbl := range []string{stagingTbl, targetTbl} {
		require.NoError(t, dest.Exec(ctx, fmt.Sprintf(
			"CREATE TABLE `%s.%s.%s` (id INT64, ts INT64, name STRING)",
			project, dataset, tbl,
		)))
	}
	// Pre-existing target row OUTSIDE the delete window — must survive.
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(
		"INSERT INTO `%s.%s.%s` VALUES (99, 999, 'outside-window')",
		project, dataset, targetTbl,
	)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO `+"`%s.%s.%s`"+` VALUES
		(1, 100, 'A'), (1, 101, 'A-dup'),
		(2, 100, 'B'), (2, 102, 'B-dup'),
		(3, 100, 'C')`, project, dataset, stagingTbl)))

	require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		StagingTable:       dataset + "." + stagingTbl,
		TargetTable:        dataset + "." + targetTbl,
		IncrementalKey:     "ts",
		IncrementalKeyType: schema.TypeInt64,
		IntervalStart:      int64(0),
		IntervalEnd:        int64(200),
		Columns:            []string{"id", "ts", "name"},
		PrimaryKeys:        []string{"id"},
	}))

	rows := bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT COUNT(*) FROM `%s.%s.%s`", project, dataset, targetTbl,
	))
	require.Len(t, rows, 1)
	// 3 deduped from staging + 1 outside-window pre-existing = 4
	require.EqualValues(t, 4, rows[0][0], "expected 3 deduped rows + 1 outside-window pre-existing")

	rows = bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT name FROM `%s.%s.%s` WHERE id = 99", project, dataset, targetTbl,
	))
	require.Len(t, rows, 1, "outside-window row must survive delete-insert")
	require.Equal(t, "outside-window", rows[0][0])

	rows = bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT COUNT(DISTINCT id) FROM `%s.%s.%s` WHERE ts BETWEEN 0 AND 200",
		project, dataset, targetTbl,
	))
	require.Len(t, rows, 1)
	require.EqualValues(t, 3, rows[0][0], "expected 3 distinct ids inside the delete-insert window")
}

func TestBigQuery_MergeTable_NullSafeCompositePrimaryKey(t *testing.T) {
	dest, client, project, dataset := bqDedupSetup(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	stagingTbl := fmt.Sprintf("merge_null_pk_staging_%s", suffix)
	targetTbl := fmt.Sprintf("merge_null_pk_target_%s", suffix)
	defer bqDropTables(ctx, client, dataset, stagingTbl, targetTbl)
	defer func() { _ = dest.Close(ctx) }()

	for _, tbl := range []string{stagingTbl, targetTbl} {
		require.NoError(t, dest.Exec(ctx, fmt.Sprintf(
			"CREATE TABLE `%s.%s.%s` (tenant_id INT64, user_id INT64, value STRING)",
			project, dataset, tbl,
		)))
	}

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO `+"`%s.%s.%s`"+` VALUES
		(NULL, 100, 'target-null-tenant'),
		(1,    NULL, 'target-null-user'),
		(1,    100,  'target-both-set')`,
		project, dataset, targetTbl,
	)))

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO `+"`%s.%s.%s`"+` VALUES
		(NULL, 100, 'staging-null-tenant-updated'),
		(1,    NULL, 'staging-null-user-updated'),
		(1,    100,  'staging-both-set-updated'),
		(NULL, NULL, 'staging-both-null-new')`,
		project, dataset, stagingTbl,
	)))

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: dataset + "." + stagingTbl,
		TargetTable:  dataset + "." + targetTbl,
		PrimaryKeys:  []string{"tenant_id", "user_id"},
		Columns:      []string{"tenant_id", "user_id", "value"},
	}))

	rows := bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT COUNT(*) FROM `%s.%s.%s`", project, dataset, targetTbl,
	))
	require.Len(t, rows, 1)
	require.EqualValues(t, 4, rows[0][0],
		"expected 3 updated rows + 1 inserted (NULL,NULL); NULL PKs must match, not duplicate")

	rows = bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT value FROM `%s.%s.%s` WHERE tenant_id IS NULL AND user_id = 100",
		project, dataset, targetTbl,
	))
	require.Len(t, rows, 1, "row with NULL tenant_id must be updated, not duplicated")
	require.Equal(t, "staging-null-tenant-updated", rows[0][0])

	rows = bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT value FROM `%s.%s.%s` WHERE tenant_id = 1 AND user_id IS NULL",
		project, dataset, targetTbl,
	))
	require.Len(t, rows, 1, "row with NULL user_id must be updated, not duplicated")
	require.Equal(t, "staging-null-user-updated", rows[0][0])

	rows = bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT value FROM `%s.%s.%s` WHERE tenant_id = 1 AND user_id = 100",
		project, dataset, targetTbl,
	))
	require.Len(t, rows, 1)
	require.Equal(t, "staging-both-set-updated", rows[0][0])

	rows = bqRunQuery(t, ctx, client, fmt.Sprintf(
		"SELECT value FROM `%s.%s.%s` WHERE tenant_id IS NULL AND user_id IS NULL",
		project, dataset, targetTbl,
	))
	require.Len(t, rows, 1, "row with both PKs NULL must be inserted exactly once")
	require.Equal(t, "staging-both-null-new", rows[0][0])
}
