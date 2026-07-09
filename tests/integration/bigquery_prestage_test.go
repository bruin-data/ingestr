//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/iterator"
)

// The pre-staging tests validate that BigQuery loads fed from extract-time
// pre-staged JSONL files produce byte-identical tables to the buffer-replay
// path, and that configurations the pre-stager cannot handle fall back to
// replay with correct results.

type bqPreStageEnv struct {
	uri     string
	project string
	dataset string
}

func bigqueryPreStageEnv(t *testing.T) bqPreStageEnv {
	t.Helper()
	uri := os.Getenv("GONG_TEST_BIGQUERY_URI")
	project := os.Getenv("GONG_TEST_BIGQUERY_PROJECT")
	dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
	if uri == "" || project == "" || dataset == "" {
		t.Skip("Set GONG_TEST_BIGQUERY_URI, GONG_TEST_BIGQUERY_PROJECT and GONG_TEST_BIGQUERY_DATASET to run BigQuery pre-staging tests")
	}
	return bqPreStageEnv{uri: uri, project: project, dataset: dataset}
}

func writeJSONLFixture(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.jsonl")
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	return fmt.Sprintf("jsonl://%s", abs)
}

func dropBQTable(ctx context.Context, t *testing.T, env bqPreStageEnv, table string) {
	t.Helper()
	client, err := bigquery.NewClient(ctx, env.project)
	if err != nil {
		return
	}
	defer func() { _ = client.Close() }()
	_ = client.Dataset(env.dataset).Table(table).Delete(ctx)
}

// bqRowsWithoutLoadTS returns all rows as JSON maps ordered by the given
// column, asserting every row carries a non-empty _ingestr_loaded_at and
// removing it so two runs at different times compare equal.
func bqRowsWithoutLoadTS(ctx context.Context, t *testing.T, env bqPreStageEnv, table, orderBy string) []map[string]any {
	t.Helper()
	client, err := bigquery.NewClient(ctx, env.project)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	q := client.Query(fmt.Sprintf(
		"SELECT TO_JSON_STRING(t) AS j FROM `%s.%s.%s` AS t ORDER BY %s",
		env.project, env.dataset, table, orderBy,
	))
	it, err := q.Read(ctx)
	require.NoError(t, err)

	var rows []map[string]any
	for {
		var row struct {
			J string
		}
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(row.J), &decoded))
		loadedAt, ok := decoded["_ingestr_loaded_at"].(string)
		require.True(t, ok, "row missing _ingestr_loaded_at: %s", row.J)
		require.NotEmpty(t, loadedAt)
		delete(decoded, "_ingestr_loaded_at")
		rows = append(rows, decoded)
	}
	return rows
}

func runBQIngest(ctx context.Context, t *testing.T, cfg *config.IngestConfig) {
	t.Helper()
	require.NoError(t, pipeline.New(cfg).Run(ctx))
}

// preStageMergeFixture exercises the common MongoDB-shaped payloads: camelCase
// keys (snake_case renames), nested documents and arrays (JSON columns), a
// column that first appears mid-extract, an always-null column (dropped by
// inference), booleans, floats, and integers. PageSize=2 forces multiple
// batches so the late column genuinely appears after staging has started.
var preStageMergeFixture = []string{
	`{"id": 1, "userName": "alice", "isActive": true, "score": 1.5, "payload": {"a": 1, "b": ["x", "y"]}, "ghost": null}`,
	`{"id": 2, "userName": "bob", "isActive": false, "score": 2.0, "payload": {"a": 2}, "ghost": null}`,
	`{"id": 3, "userName": "carol", "isActive": true, "score": 3.25, "payload": [1, 2, 3], "ghost": null}`,
	`{"id": 4, "userName": "dave", "isActive": false, "score": 4.0, "payload": {"nested": {"deep": true}}, "lateCol": "first", "ghost": null}`,
	`{"id": 5, "userName": "erin", "isActive": true, "score": 5.5, "lateCol": "second", "ghost": null}`,
}

func TestBigQueryPreStage_MergeMatchesReplayPath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	env := bigqueryPreStageEnv(t)
	sourceURI := writeJSONLFixture(t, preStageMergeFixture)

	suffix := uniqueSuffix()
	tables := map[bool]string{
		false: "prestage_merge_on_" + suffix,
		true:  "prestage_merge_off_" + suffix,
	}

	for disable, table := range tables {
		t.Cleanup(func() { dropBQTable(ctx, t, env, table) })
		runBQIngest(ctx, t, &config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "prestage_source",
			DestURI:             env.uri,
			DestTable:           env.dataset + "." + table,
			IncrementalStrategy: config.StrategyMerge,
			PrimaryKeys:         []string{"id"},
			PageSize:            2,
			LoaderFileSize:      2,
			DisablePreStaging:   disable,
		})
	}

	preStagedRows := bqRowsWithoutLoadTS(ctx, t, env, tables[false], "id")
	replayRows := bqRowsWithoutLoadTS(ctx, t, env, tables[true], "id")

	require.Len(t, preStagedRows, len(preStageMergeFixture))
	require.Equal(t, replayRows, preStagedRows, "pre-staged path must produce the same table as the replay path")

	// The always-null column must have been dropped by inference on both paths.
	for _, row := range preStagedRows {
		_, exists := row["ghost"]
		require.False(t, exists, "all-null column must be dropped")
	}
	// camelCase keys must be renamed to snake_case on both paths.
	require.Contains(t, preStagedRows[0], "user_name")
	require.Contains(t, preStagedRows[3], "late_col")
}

func TestBigQueryPreStage_ReplaceAndAppendMatchReplayPath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	env := bigqueryPreStageEnv(t)
	sourceURI := writeJSONLFixture(t, preStageMergeFixture)

	for _, strategy := range []config.IncrementalStrategy{config.StrategyReplace, config.StrategyAppend} {
		t.Run(string(strategy), func(t *testing.T) {
			suffix := uniqueSuffix()
			tables := map[bool]string{
				false: fmt.Sprintf("prestage_%s_on_%s", strategy, suffix),
				true:  fmt.Sprintf("prestage_%s_off_%s", strategy, suffix),
			}

			for disable, table := range tables {
				t.Cleanup(func() { dropBQTable(ctx, t, env, table) })
				runBQIngest(ctx, t, &config.IngestConfig{
					SourceURI:           sourceURI,
					SourceTable:         "prestage_source",
					DestURI:             env.uri,
					DestTable:           env.dataset + "." + table,
					IncrementalStrategy: strategy,
					PageSize:            2,
					LoaderFileSize:      2,
					DisablePreStaging:   disable,
				})
			}

			preStagedRows := bqRowsWithoutLoadTS(ctx, t, env, tables[false], "id")
			replayRows := bqRowsWithoutLoadTS(ctx, t, env, tables[true], "id")
			require.Len(t, preStagedRows, len(preStageMergeFixture))
			require.Equal(t, replayRows, preStagedRows)
		})
	}
}

// Type promotion mid-extract (int64 → string) must disable the pre-staged
// files (early files carry raw JSON numbers that cannot load into a STRING
// column) and fall back to the replay path transparently.
func TestBigQueryPreStage_TypePromotionFallsBackCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	env := bigqueryPreStageEnv(t)

	sourceURI := writeJSONLFixture(t, []string{
		`{"id": 1, "value": 100}`,
		`{"id": 2, "value": 200}`,
		`{"id": 3, "value": "not-a-number"}`,
	})

	table := "prestage_promotion_" + uniqueSuffix()
	t.Cleanup(func() { dropBQTable(ctx, t, env, table) })

	runBQIngest(ctx, t, &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "prestage_source",
		DestURI:             env.uri,
		DestTable:           env.dataset + "." + table,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
		PageSize:            2,
	})

	rows := bqRowsWithoutLoadTS(ctx, t, env, table, "id")
	require.Len(t, rows, 3)
	require.Equal(t, "100", rows[0]["value"])
	require.Equal(t, "200", rows[1]["value"])
	require.Equal(t, "not-a-number", rows[2]["value"])
}

// Date-like strings resolve to TIMESTAMP columns through ingestr's lenient
// parser; the raw strings in pre-staged files may not be BigQuery-parseable,
// so this configuration must fall back to replay and still load correctly.
func TestBigQueryPreStage_TemporalStringsFallBackCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	env := bigqueryPreStageEnv(t)

	sourceURI := writeJSONLFixture(t, []string{
		`{"id": 1, "seenAt": "2026-07-01T10:00:00Z"}`,
		`{"id": 2, "seenAt": "2026-07-01 11:30:00"}`,
	})

	table := "prestage_temporal_" + uniqueSuffix()
	t.Cleanup(func() { dropBQTable(ctx, t, env, table) })

	runBQIngest(ctx, t, &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "prestage_source",
		DestURI:             env.uri,
		DestTable:           env.dataset + "." + table,
		IncrementalStrategy: config.StrategyReplace,
		PageSize:            1,
	})

	rows := bqRowsWithoutLoadTS(ctx, t, env, table, "id")
	require.Len(t, rows, 2)
	require.Equal(t, "2026-07-01T10:00:00Z", rows[0]["seen_at"])
	require.Equal(t, "2026-07-01T11:30:00Z", rows[1]["seen_at"])
}

// A second merge run against an existing destination table skips pre-staging
// (auto naming detection needs the inferred schema) and must upsert correctly
// through the replay path.
func TestBigQueryPreStage_IncrementalMergeSecondRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	env := bigqueryPreStageEnv(t)

	table := "prestage_incr_" + uniqueSuffix()
	t.Cleanup(func() { dropBQTable(ctx, t, env, table) })

	initial := writeJSONLFixture(t, []string{
		`{"id": 1, "userName": "alice", "version": 1}`,
		`{"id": 2, "userName": "bob", "version": 1}`,
	})
	update := writeJSONLFixture(t, []string{
		`{"id": 2, "userName": "bob-updated", "version": 2}`,
		`{"id": 3, "userName": "carol", "version": 1}`,
	})

	baseCfg := func(sourceURI string) *config.IngestConfig {
		return &config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "prestage_source",
			DestURI:             env.uri,
			DestTable:           env.dataset + "." + table,
			IncrementalStrategy: config.StrategyMerge,
			PrimaryKeys:         []string{"id"},
			PageSize:            2,
		}
	}

	runBQIngest(ctx, t, baseCfg(initial))
	runBQIngest(ctx, t, baseCfg(update))

	rows := bqRowsWithoutLoadTS(ctx, t, env, table, "id")
	require.Len(t, rows, 3)
	require.Equal(t, "alice", rows[0]["user_name"])
	require.Equal(t, "bob-updated", rows[1]["user_name"])
	require.Equal(t, float64(2), rows[1]["version"])
	require.Equal(t, "carol", rows[2]["user_name"])
}

// GCS staging bucket variant: pre-staged files are written directly to GCS
// during extract. Requires GONG_TEST_BIGQUERY_STAGING_BUCKET (gs://...).
func TestBigQueryPreStage_GCSStagingBucket(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	env := bigqueryPreStageEnv(t)
	bucket := os.Getenv("GONG_TEST_BIGQUERY_STAGING_BUCKET")
	if bucket == "" {
		t.Skip("Set GONG_TEST_BIGQUERY_STAGING_BUCKET to run the GCS pre-staging test")
	}

	sourceURI := writeJSONLFixture(t, preStageMergeFixture)
	table := "prestage_gcs_" + uniqueSuffix()
	t.Cleanup(func() { dropBQTable(ctx, t, env, table) })

	runBQIngest(ctx, t, &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "prestage_source",
		DestURI:             env.uri,
		DestTable:           env.dataset + "." + table,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
		PageSize:            2,
		LoaderFileSize:      2,
		StagingBucket:       bucket,
	})

	rows := bqRowsWithoutLoadTS(ctx, t, env, table, "id")
	require.Len(t, rows, len(preStageMergeFixture))
	require.Equal(t, "alice", rows[0]["user_name"])
}
