//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // Register ADBC driver
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteInsertStrategy_JSONLToDuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	testdataDir, err := filepath.Abs("testdata")
	require.NoError(t, err)

	initialFile := filepath.Join(testdataDir, "conformance_deleteinsert_initial.jsonl")
	intervalFile := filepath.Join(testdataDir, "conformance_deleteinsert_interval.jsonl")

	_, err = os.Stat(initialFile)
	require.NoError(t, err, "Initial JSONL file should exist")
	_, err = os.Stat(intervalFile)
	require.NoError(t, err, "Interval JSONL file should exist")

	tmpDir := t.TempDir()
	duckDBPath := filepath.Join(tmpDir, fmt.Sprintf("deleteinsert_test_%d.duckdb", time.Now().UnixNano()))

	destURI := fmt.Sprintf("duckdb:///%s", duckDBPath)

	t.Log("=== First Load: Initial data ===")
	cfg1 := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("jsonl://%s", initialFile),
		SourceTable:         "data",
		DestURI:             destURI,
		DestTable:           "main.events",
		IncrementalStrategy: config.StrategyDeleteInsert,
		IncrementalKey:      "id",
	}

	p1 := pipeline.New(cfg1)
	err = p1.Run(ctx)
	require.NoError(t, err, "First pipeline run should succeed")

	validateDuckDBDeleteInsertResults(t, duckDBPath, "After initial load", 10, map[int64]string{
		1:  "v1-1",
		2:  "v1-2",
		3:  "v1-3",
		4:  "v1-4",
		5:  "v1-5",
		6:  "v1-6",
		8:  "v1-8",
		9:  "v1-9",
		10: "v1-10",
		11: "v1-11",
	})

	t.Log("=== Second Load: Interval update (should delete IDs 3-7 and insert new records) ===")
	cfg2 := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("jsonl://%s", intervalFile),
		SourceTable:         "data",
		DestURI:             destURI,
		DestTable:           "main.events",
		IncrementalStrategy: config.StrategyDeleteInsert,
		IncrementalKey:      "id",
	}

	p2 := pipeline.New(cfg2)
	err = p2.Run(ctx)
	require.NoError(t, err, "Second pipeline run should succeed")

	validateDuckDBDeleteInsertResults(t, duckDBPath, "After interval update", 11, map[int64]string{
		1:  "v1-1",
		2:  "v1-2",
		3:  "v2-3",
		4:  "v2-4",
		5:  "v2-5",
		6:  "v2-6",
		7:  "v2-7",
		8:  "v1-8",
		9:  "v1-9",
		10: "v1-10",
		11: "v1-11",
	})

	t.Log("=== Delete+Insert test completed successfully ===")
}

func TestDeleteInsertStrategy_WithExplicitInterval(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	dir := t.TempDir()
	initial := filepath.Join(dir, "initial.jsonl")
	replacement := filepath.Join(dir, "replacement.jsonl")
	require.NoError(t, os.WriteFile(initial, []byte(`{"id":1,"dt":"2024-01-01","name":"outside-before"}
{"id":2,"dt":"2024-01-02","name":"start-boundary"}
{"id":3,"dt":"2024-01-03","name":"old"}
{"id":4,"dt":"2024-01-04","name":"end-boundary"}
{"id":5,"dt":"2024-01-05","name":"outside-after"}
`), 0o600))
	require.NoError(t, os.WriteFile(replacement, []byte(`{"id":3,"dt":"2024-01-03","name":"new"}
`), 0o600))
	dbPath := filepath.Join(dir, "explicit.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           "jsonl://" + initial,
		SourceTable:         "data",
		DestURI:             "duckdb:///" + dbPath,
		DestTable:           "main.events",
		IncrementalStrategy: config.StrategyDeleteInsert,
		IncrementalKey:      "dt",
		Columns:             "dt:date",
	}
	require.NoError(t, pipeline.New(cfg).Run(t.Context()))
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)
	cfg.SourceURI = "jsonl://" + replacement
	cfg.IntervalStart, cfg.IntervalEnd = &start, &end
	require.NoError(t, pipeline.New(cfg).Run(t.Context()))
	validateDuckDBDeleteInsertResults(t, dbPath, "Explicit bounds wider than staged data", 3, map[int64]string{
		1: "outside-before", 3: "new", 5: "outside-after",
	})
}

func TestDeleteInsertStrategy_RejectsNullKeysWithoutChangingTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "rows.jsonl")
	dbPath := filepath.Join(dir, "nulls.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           "jsonl://" + input,
		SourceTable:         "data",
		DestURI:             "duckdb:///" + dbPath,
		DestTable:           "main.events",
		IncrementalStrategy: config.StrategyDeleteInsert,
		IncrementalKey:      "batch_id",
	}
	require.NoError(t, os.WriteFile(input, []byte("{\"id\":1,\"batch_id\":3,\"name\":\"original\"}\n"), 0o600))
	require.NoError(t, pipeline.New(cfg).Run(t.Context()))
	require.NoError(t, os.WriteFile(input, []byte(`{"id":2,"batch_id":3,"name":"replacement"}
{"id":3,"batch_id":null,"name":"unbounded"}
`), 0o600))
	for range 2 {
		require.ErrorContains(t, pipeline.New(cfg).Run(t.Context()), "NULL")
		validateDuckDBDeleteInsertResults(t, dbPath, "Rejected NULL-key batch", 1, map[int64]string{1: "original"})
	}
}

func TestDeleteInsertStrategy_AdditionalBoundsReachDuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	for _, tc := range []struct {
		name, sqlType, values string
		dt                    arrow.DataType
		keys                  [5]string
	}{
		{
			name: "uint64", sqlType: "UBIGINT", dt: arrow.PrimitiveTypes.Uint64,
			values: `[18446744073709551614,9223372036854775808]`,
			keys:   [5]string{"9223372036854775807", "9223372036854775808", "9223372036854775809", "18446744073709551614", "18446744073709551615"},
		},
		{
			name: "time64", sqlType: "TIME", dt: arrow.FixedWidthTypes.Time64us,
			values: `["13:04:05.000007","02:03:04.000005"]`,
			keys:   [5]string{"'02:03:04.000004'", "'02:03:04.000005'", "'05:00:00.000000'", "'13:04:05.000007'", "'13:04:05.000008'"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			dbPath := filepath.Join(t.TempDir(), "bounds.duckdb")
			dest := duckdb.NewDuckDBDestination()
			require.NoError(t, dest.Connect(ctx, "duckdb:///"+dbPath))
			t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf("CREATE TABLE main.events (id BIGINT, k %s, name VARCHAR); CREATE TABLE staged AS SELECT * FROM main.events", tc.sqlType)))
			for i, key := range tc.keys {
				require.NoError(t, dest.Exec(ctx, fmt.Sprintf("INSERT INTO main.events VALUES (%d, %s, 'old-%d')", i+1, key, i+1)))
			}
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf("INSERT INTO staged VALUES (2, %s, 'new-2'), (4, %s, 'new-4')", tc.keys[1], tc.keys[3])))

			pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
			t.Cleanup(func() { pool.AssertSize(t, 0) })
			arr, _, err := array.FromJSON(pool, tc.dt, strings.NewReader(tc.values))
			require.NoError(t, err)
			batch := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "k", Type: tc.dt}}, nil), []arrow.Array{arr}, int64(arr.Len()))
			arr.Release()
			in := make(chan source.RecordBatchResult, 1)
			in <- source.RecordBatchResult{Batch: batch}
			close(in)
			tracker := strategy.NewIntervalTracker("k")
			for result := range tracker.Wrap(in) {
				result.Batch.Release()
			}
			for range 2 {
				require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
					StagingTable: "staged", TargetTable: "main.events", IncrementalKey: "k",
					IntervalStart: tracker.Min, IntervalEnd: tracker.Max, Columns: []string{"id", "k", "name"},
				}))
				validateDuckDBDeleteInsertResults(t, dbPath, "Typed bounds preserve adjacent values", 4, map[int64]string{
					1: "old-1", 2: "new-2", 4: "new-4", 5: "old-5",
				})
			}
		})
	}
}

func TestDeleteInsertStrategy_DeletesRecordsNotInNewData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	tmpDir := t.TempDir()

	initialFile := filepath.Join(tmpDir, "initial.jsonl")
	secondFile := filepath.Join(tmpDir, "second.jsonl")

	// Use integer 'batch_id' as incremental key for clearer semantics
	// Initial data: batch_id 1 has IDs 1-2, batch_id 2 has IDs 3-4, batch_id 3 has ID 5
	err := os.WriteFile(initialFile, []byte(`{"id":1,"batch_id":1,"value":"a"}
{"id":2,"batch_id":1,"value":"b"}
{"id":3,"batch_id":2,"value":"c"}
{"id":4,"batch_id":2,"value":"d"}
{"id":5,"batch_id":3,"value":"e"}
`), 0o644)
	require.NoError(t, err)

	// Second load: only update batch_id=2, but with only one record (ID 3)
	// This should DELETE ID 4 (was in batch_id=2 but not in new data)
	err = os.WriteFile(secondFile, []byte(`{"id":3,"batch_id":2,"value":"c-updated"}
`), 0o644)
	require.NoError(t, err)

	duckDBPath := filepath.Join(tmpDir, "test.duckdb")
	destURI := fmt.Sprintf("duckdb:///%s", duckDBPath)

	t.Log("=== First Load: 5 records across 3 batches ===")
	cfg1 := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("jsonl://%s", initialFile),
		SourceTable:         "data",
		DestURI:             destURI,
		DestTable:           "main.events",
		IncrementalStrategy: config.StrategyDeleteInsert,
		IncrementalKey:      "batch_id",
	}

	err = pipeline.New(cfg1).Run(ctx)
	require.NoError(t, err)

	// Open fresh connection after first pipeline
	db1, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	require.NoError(t, err)

	var count int
	err = db1.QueryRow("SELECT COUNT(*) FROM main.events").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 5, count, "Should have 5 records after initial load")
	_ = db1.Close()

	t.Log("=== Second Load: Only 1 record for batch_id=2 ===")
	t.Log("Expected: Delete records where batch_id=2, insert new record")
	t.Log("Result: ID 4 should be DELETED (was in interval but not in new data)")

	cfg2 := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("jsonl://%s", secondFile),
		SourceTable:         "data",
		DestURI:             destURI,
		DestTable:           "main.events",
		IncrementalStrategy: config.StrategyDeleteInsert,
		IncrementalKey:      "batch_id",
	}

	err = pipeline.New(cfg2).Run(ctx)
	require.NoError(t, err)

	// Open fresh connection after second pipeline to see latest changes
	db2, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()

	err = db2.QueryRow("SELECT COUNT(*) FROM main.events").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 4, count, "Should have 4 records: IDs 1,2 (batch=1), ID 3 updated (batch=2), ID 5 (batch=3)")

	var deletedExists bool
	err = db2.QueryRow("SELECT EXISTS(SELECT 1 FROM main.events WHERE id = 4)").Scan(&deletedExists)
	require.NoError(t, err)
	assert.False(t, deletedExists, "ID 4 should be deleted (was in interval but not in new data)")

	var value string
	var valueRaw []byte
	err = db2.QueryRow("SELECT value FROM main.events WHERE id = 3").Scan(&valueRaw)
	require.NoError(t, err)
	value = string(valueRaw)
	assert.Equal(t, "c-updated", value, "ID 3 should have updated value")

	t.Log("=== Delete+Insert correctly deletes records not in new data ===")
}

func validateDuckDBDeleteInsertResults(t *testing.T, dbPath, phase string, expectedCount int, expectedNames map[int64]string) {
	t.Helper()

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", dbPath))
	require.NoError(t, err, "Failed to open DuckDB for validation")
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM main.events").Scan(&count)
	require.NoError(t, err, "Failed to count rows")
	assert.Equal(t, expectedCount, count, "%s: row count mismatch", phase)

	rows, err := db.Query("SELECT id, name FROM main.events ORDER BY id")
	require.NoError(t, err, "Failed to query rows")
	defer func() { _ = rows.Close() }()

	actualNames := make(map[int64]string)
	for rows.Next() {
		var id int64
		var nameRaw []byte
		err := rows.Scan(&id, &nameRaw)
		require.NoError(t, err)
		copied := append([]byte(nil), nameRaw...)
		actualNames[id] = string(copied)
	}
	require.NoError(t, rows.Err())

	for id, expectedName := range expectedNames {
		actualName, exists := actualNames[id]
		assert.True(t, exists, "%s: ID %d should exist", phase, id)
		assert.Equal(t, expectedName, actualName, "%s: ID %d name mismatch", phase, id)
	}

	for id := range actualNames {
		_, expected := expectedNames[id]
		assert.True(t, expected, "%s: unexpected ID %d found", phase, id)
	}

	t.Logf("%s: validated %d rows", phase, count)
}
