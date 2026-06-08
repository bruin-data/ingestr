//go:build integration

// CSV → DuckDB end-to-end tests for the --columns flag. DuckDB is in-process
// (no container needed), but these tests still live in package integration
// so they share fixtures with the Postgres variant in
// column_overrides_postgres_test.go (which is gated by //go:build local).
package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const csvWithRows = `id,name,email,age
1,User 1,user1@example.com,25
2,User 2,user2@example.com,30
3,User 3,user3@example.com,35
`

const csvHeadersOnly = "id,name,email,age\n"

const csvSparseRows = `id,name,email,age
1,User 1,,25
2,User 2,,30
3,User 3,,35
`

const csvMaskRows = `id,name,email,ssn,age,notes
1,Alice Smith,alice@example.com,123-45-6789,34,keep
2,Bob Jones,bob@example.com,987-65-4321,37,keep2
`

func TestColumnOverrides_CSVToDuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "users.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvWithRows), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "users",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.users",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	types := readDuckDBColumnTypes(t, duckDBPath, "main.users")
	assert.Equal(t, "VARCHAR", types["id"])
	assert.Equal(t, "VARCHAR", types["name"])
	assert.Equal(t, "VARCHAR", types["email"])
	assert.Equal(t, "VARCHAR", types["age"])
	assert.Equal(t, 4, len(types))
	assert.Equal(t, 3, readDuckDBRowCount(t, duckDBPath, "main.users"))
}

func TestColumnOverrides_CSVToDuckDB_AppliesTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "users.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvWithRows), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "users",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.users",
		IncrementalStrategy: config.StrategyReplace,
		Columns:             "id:bigint,age:smallint",
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	types := readDuckDBColumnTypes(t, duckDBPath, "main.users")
	assert.Equal(t, "BIGINT", types["id"], "id should be overridden to BIGINT")
	assert.Equal(t, "SMALLINT", types["age"], "age should be overridden to SMALLINT")
	assert.Equal(t, "VARCHAR", types["name"], "name should default to VARCHAR")
	assert.Equal(t, "VARCHAR", types["email"], "email should default to VARCHAR")
	assert.Equal(t, 3, readDuckDBRowCount(t, duckDBPath, "main.users"))
}

func TestColumnMasking_CSVToDuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "users.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvMaskRows), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "users",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.users",
		IncrementalStrategy: config.StrategyReplace,
		Mask: []string{
			"email:hash",
			"name:partial:1",
			"ssn:redact",
			"age:round:10",
		},
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	assert.Equal(t, 2, readDuckDBRowCount(t, duckDBPath, "main.users"))

	types := readDuckDBColumnTypes(t, duckDBPath, "main.users")
	assert.Equal(t, "VARCHAR", types["name"])
	assert.Equal(t, "VARCHAR", types["email"])
	assert.Equal(t, "VARCHAR", types["ssn"])
	assert.Equal(t, "BIGINT", types["age"])

	db := openDuckDBForTest(t, duckDBPath)
	defer func() { _ = db.Close() }()

	rows, err := db.Query("SELECT name, email, ssn, age, notes FROM main.users ORDER BY CAST(id AS INTEGER)")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	type maskedRow struct {
		name  string
		email string
		ssn   string
		age   int64
		notes string
	}

	var got []maskedRow
	for rows.Next() {
		var r maskedRow
		var name, email, ssn, notes []byte
		require.NoError(t, rows.Scan(&name, &email, &ssn, &r.age, &notes))
		r.name = string(name)
		r.email = string(email)
		r.ssn = string(ssn)
		r.notes = string(notes)
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 2)

	assert.Equal(t, "A*********h", got[0].name)
	assert.Equal(t, sha256Hex("alice@example.com"), got[0].email)
	assert.Equal(t, "REDACTED", got[0].ssn)
	assert.Equal(t, int64(30), got[0].age)
	assert.Equal(t, "keep", got[0].notes)

	assert.Equal(t, "B*******s", got[1].name)
	assert.Equal(t, sha256Hex("bob@example.com"), got[1].email)
	assert.Equal(t, "REDACTED", got[1].ssn)
	assert.Equal(t, int64(40), got[1].age)
	assert.Equal(t, "keep2", got[1].notes)
}

func TestColumnOverrides_EmptyCSV_NoOverrides_TableNotCreated(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "empty.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvHeadersOnly), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "empty",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.empty",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)), "pipeline should succeed (skip) even with no rows")

	assert.False(t, duckDBTableExists(t, duckDBPath, "main", "empty"),
		"destination table should NOT be created when source has no rows and no overrides")
}

func TestColumnOverrides_EmptyCSV_WithOverrides_TableCreatedFromOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "empty.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvHeadersOnly), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "empty",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.empty",
		IncrementalStrategy: config.StrategyReplace,
		Columns:             "id:bigint,name:string,email:string,age:smallint",
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	require.True(t, duckDBTableExists(t, duckDBPath, "main", "empty"),
		"destination table should be created from --columns when source has no rows")

	types := readDuckDBColumnTypes(t, duckDBPath, "main.empty")
	assert.Equal(t, "BIGINT", types["id"])
	assert.Equal(t, "VARCHAR", types["name"])
	assert.Equal(t, "VARCHAR", types["email"])
	assert.Equal(t, "SMALLINT", types["age"])
	assert.Equal(t, 4, len(types), "table should have exactly the 4 overridden columns")

	assert.Equal(t, 0, readDuckDBRowCount(t, duckDBPath, "main.empty"),
		"empty source should produce zero rows in destination")
}

func TestColumnOverrides_InvalidType_ReturnsError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "users.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvWithRows), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "users",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.users",
		IncrementalStrategy: config.StrategyReplace,
		Columns:             "id:bogustype",
	}
	require.NoError(t, cfg.Validate())

	err := runPipeline(t, ctx, pipeline.New(cfg))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type 'bogustype'")

	assert.False(t, duckDBTableExists(t, duckDBPath, "main", "users"),
		"destination table should NOT be created when override parsing fails")
}

func TestColumnOverrides_ExtraColumn_AddedWithNulls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "users.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvWithRows), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "users",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.users",
		IncrementalStrategy: config.StrategyReplace,
		Columns:             "id:bigint,does_not_exist:string",
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	types := readDuckDBColumnTypes(t, duckDBPath, "main.users")
	assert.Equal(t, "BIGINT", types["id"], "id override should apply")
	assert.Equal(t, "VARCHAR", types["does_not_exist"],
		"override for a column not in the schema-less source should be added with NULL values")
	assert.Equal(t, 5, len(types), "4 source columns + 1 added override column")

	db := openDuckDBForTest(t, duckDBPath)
	defer func() { _ = db.Close() }()
	var nullCount int
	require.NoError(t,
		db.QueryRow("SELECT COUNT(*) FROM main.users WHERE does_not_exist IS NULL").Scan(&nullCount))
	assert.Equal(t, 3, nullCount, "added column should be NULL for every row")
}

func TestColumnOverrides_DroppedColumn_ReappearsViaOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	// CSV where every row has an empty email value — inference would normally
	// drop the column entirely.
	csvPath := filepath.Join(tmpDir, "sparse.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvSparseRows), 0o644))

	t.Run("without override the column is dropped", func(t *testing.T) {
		duckDBPath := filepath.Join(tmpDir, "without_override.duckdb")
		cfg := &config.IngestConfig{
			SourceURI:           fmt.Sprintf("csv://%s", csvPath),
			SourceTable:         "sparse",
			DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
			DestTable:           "main.sparse",
			IncrementalStrategy: config.StrategyReplace,
		}
		require.NoError(t, cfg.Validate())
		require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

		types := readDuckDBColumnTypes(t, duckDBPath, "main.sparse")
		_, hasEmail := types["email"]
		assert.False(t, hasEmail, "all-empty email column should be dropped during inference")
	})

	t.Run("override re-adds the column with NULL values", func(t *testing.T) {
		duckDBPath := filepath.Join(tmpDir, "with_override.duckdb")
		cfg := &config.IngestConfig{
			SourceURI:           fmt.Sprintf("csv://%s", csvPath),
			SourceTable:         "sparse",
			DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
			DestTable:           "main.sparse",
			IncrementalStrategy: config.StrategyReplace,
			Columns:             "email:string,age:int",
		}
		require.NoError(t, cfg.Validate())
		require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

		types := readDuckDBColumnTypes(t, duckDBPath, "main.sparse")
		assert.Equal(t, "VARCHAR", types["email"], "email override should re-add the column")
		assert.Equal(t, "INTEGER", types["age"], "age override should retype the column")
		assert.Equal(t, 3, readDuckDBRowCount(t, duckDBPath, "main.sparse"))

		db := openDuckDBForTest(t, duckDBPath)
		defer func() { _ = db.Close() }()
		var nullEmails int
		require.NoError(t,
			db.QueryRow("SELECT COUNT(*) FROM main.sparse WHERE email IS NULL").Scan(&nullEmails))
		assert.Equal(t, 3, nullEmails, "every row's email value should be NULL")
	})
}

func TestColumnOverrides_EmptyCSV_AutoAddsPKColumn(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "empty.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvHeadersOnly), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "empty",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.empty",
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"user_uid"},
		Columns:             "id:bigint,name:string",
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	require.True(t, duckDBTableExists(t, duckDBPath, "main", "empty"))

	types := readDuckDBColumnTypes(t, duckDBPath, "main.empty")
	assert.Equal(t, "BIGINT", types["id"])
	assert.Equal(t, "VARCHAR", types["name"])
	assert.Equal(t, "VARCHAR", types["user_uid"],
		"PK column missing from --columns should be auto-added as VARCHAR so merge-style destinations accept the CREATE TABLE")
	assert.Equal(t, 3, len(types), "expected id + name + user_uid (the resolved PK)")
}

// --- helpers ---------------------------------------------------------------

func openDuckDBForTest(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	return db
}

func readDuckDBColumnTypes(t *testing.T, dbPath, qualifiedTable string) map[string]string {
	t.Helper()
	db := openDuckDBForTest(t, dbPath)
	defer func() { _ = db.Close() }()

	tableName := qualifiedTable
	if idx := strings.LastIndex(qualifiedTable, "."); idx >= 0 {
		tableName = qualifiedTable[idx+1:]
	}

	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info('%s')", tableName))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var cid int
		var nameRaw, ctypeRaw []byte
		var notnull bool
		var dflt any
		var pk bool
		require.NoError(t, rows.Scan(&cid, &nameRaw, &ctypeRaw, &notnull, &dflt, &pk))
		out[strings.ToLower(string(nameRaw))] = strings.ToUpper(strings.TrimSpace(string(ctypeRaw)))
	}
	require.NoError(t, rows.Err())
	return out
}

func readDuckDBRowCount(t *testing.T, dbPath, qualifiedTable string) int {
	t.Helper()
	db := openDuckDBForTest(t, dbPath)
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", qualifiedTable)).Scan(&count))
	return count
}

func duckDBTableExists(t *testing.T, dbPath, schemaName, tableName string) bool {
	t.Helper()
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return false
	}
	db := openDuckDBForTest(t, dbPath)
	defer func() { _ = db.Close() }()

	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND lower(table_name) = lower(?)",
		schemaName, tableName,
	).Scan(&count)
	require.NoError(t, err)
	return count > 0
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
