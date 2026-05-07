package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/pipeline"
	_ "github.com/bruin-data/gong/pkg/source/adbc" // Register ADBC driver
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
	// Skip this test - explicit interval with non-timestamp types requires
	// different handling in the config (IntervalStart/End are *time.Time)
	t.Skip("Explicit interval with integer IDs not supported yet - requires config changes")
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
