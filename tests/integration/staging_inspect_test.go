//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests verify the invariant: STAGING TABLE MIRRORS DEST SCHEMA.
//
// They run the pipeline with KeepStaging=true so the staging table survives
// past the merge, then inspect it directly via SQL.
//
// Shape of each test:
//   1. Set up a DuckDB file
//   2. Optionally pre-create the dest table with a different schema (drift)
//   3. Run pipeline with KeepStaging=true
//   4. Find the staging table (in _bruin_staging schema)
//   5. Assert: staging schema == dest schema (cols, types, order)
//   6. Assert: staging data is what we expect

func newDuckDBDest(t *testing.T) (uri, path, destTable string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, fmt.Sprintf("staging_inspect_%d.duckdb", time.Now().UnixNano()))
	uri = fmt.Sprintf("duckdb:///%s", path)
	destTable = "main.users"
	return
}

func openDuckDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err, "open duckdb")
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func preCreateDest(t *testing.T, ctx context.Context, destURI, table string, cols []schema.Column) {
	t.Helper()
	d := duckdb.NewDuckDBDestination()
	require.NoError(t, d.Connect(ctx, destURI), "connect duckdb")
	t.Cleanup(func() { _ = d.Close(ctx) })

	err := d.PrepareTable(ctx, destination.PrepareOptions{
		Table:     table,
		Schema:    &schema.TableSchema{Name: table, Columns: cols},
		DropFirst: true,
	})
	require.NoError(t, err, "pre-create dest")
	require.NoError(t, d.Close(ctx))
}

// findStagingTable returns the fully-qualified staging table name.
func findStagingTable(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_schema = '_bruin_staging'`)
	require.NoError(t, err, "list staging tables")
	defer func() { _ = rows.Close() }()

	var found []string
	for rows.Next() {
		var s, n []byte
		require.NoError(t, rows.Scan(&s, &n))
		found = append(found, fmt.Sprintf("%s.%s", string(s), string(n)))
	}
	require.NoError(t, rows.Err())
	require.Len(t, found, 1, "expected exactly one staging table, got: %v", found)
	return found[0]
}

type colInfo struct {
	name     string
	dataType string
	nullable bool
}

func tableColumns(t *testing.T, db *sql.DB, qualifiedTable string) []colInfo {
	t.Helper()
	parts := strings.SplitN(qualifiedTable, ".", 2)
	require.Len(t, parts, 2, "expected schema.table, got %q", qualifiedTable)
	tableSchema, name := parts[0], parts[1]

	// String-interpolate instead of `?` placeholders — ADBC's prepared-statement
	// path mis-counts parameters on some DuckDB queries.
	q := fmt.Sprintf(`
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = '%s' AND table_name = '%s'
		ORDER BY ordinal_position`, tableSchema, name)
	rows, err := db.Query(q)
	require.NoError(t, err, "query columns")
	defer func() { _ = rows.Close() }()

	var out []colInfo
	for rows.Next() {
		var nameRaw, typeRaw, nullableRaw []byte
		require.NoError(t, rows.Scan(&nameRaw, &typeRaw, &nullableRaw))
		out = append(out, colInfo{
			name:     string(nameRaw),
			dataType: strings.ToUpper(string(typeRaw)),
			nullable: strings.EqualFold(string(nullableRaw), "YES"),
		})
	}
	require.NoError(t, rows.Err())
	return out
}

func colNames(cols []colInfo) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.name
	}
	return out
}

func nullCol(name string, dt schema.DataType) schema.Column {
	return schema.Column{Name: name, DataType: dt, Nullable: true}
}

// =====================================================================
// Case 1: Basic merge — no drift. Staging should mirror dest exactly.
// =====================================================================
func TestStaging_BasicMergeMirrorsDest(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	ctx := context.Background()
	destURI, destPath, destTable := newDuckDBDest(t)
	sourceURI := jsonlURI(t, "testdata/conformance_merge_initial.jsonl")

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
		KeepStaging:         true,
	}

	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)), "pipeline should succeed")

	db := openDuckDB(t, destPath)
	staging := findStagingTable(t, db)

	stagingCols := tableColumns(t, db, staging)
	destCols := tableColumns(t, db, destTable)

	assert.Equal(t, colNames(destCols), colNames(stagingCols),
		"staging columns must match dest exactly in order")
	for i := range destCols {
		assert.Equal(t, destCols[i].dataType, stagingCols[i].dataType,
			"col %q: type mismatch", destCols[i].name)
	}

	var rowCount int
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", staging)).Scan(&rowCount))
	assert.Equal(t, mergeInitialRows, rowCount, "staging should contain all source rows")
}

// =====================================================================
// Case 2: Soft-removed column. Dest has an extra column that source
// doesn't. Staging MUST include that column (null-filled) so the MERGE
// can write NULLs into dest for new rows.
// =====================================================================
func TestStaging_SoftRemovedColumnNullFilled(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	ctx := context.Background()
	destURI, destPath, destTable := newDuckDBDest(t)

	preCreateDest(t, ctx, destURI, destTable, []schema.Column{
		nullCol("id", schema.TypeInt64),
		nullCol("name", schema.TypeString),
		nullCol("active", schema.TypeBoolean),
		nullCol("score", schema.TypeFloat64),
		nullCol("deprecated_note", schema.TypeString), // dest-only
	})

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_merge_initial.jsonl"),
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
		KeepStaging:         true,
	}
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)), "pipeline should succeed")

	db := openDuckDB(t, destPath)
	staging := findStagingTable(t, db)

	stagingCols := tableColumns(t, db, staging)
	destCols := tableColumns(t, db, destTable)

	assert.Equal(t, colNames(destCols), colNames(stagingCols),
		"staging must include the dest-only soft-removed column")

	var nullCount, totalCount int
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE deprecated_note IS NULL", staging,
	)).Scan(&nullCount))
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s", staging,
	)).Scan(&totalCount))
	assert.Equal(t, totalCount, nullCount,
		"every row in staging must have NULL for the soft-removed column")
}

// =====================================================================
// Case 3: source DOUBLE, dest VARCHAR (pre-existing). Evolution doesn't
// fire (VARCHAR absorbs DOUBLE). Staging MUST be created with VARCHAR.
// =====================================================================
func TestStaging_WidenedDestTypeUsedInStaging(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	ctx := context.Background()
	destURI, destPath, destTable := newDuckDBDest(t)

	preCreateDest(t, ctx, destURI, destTable, []schema.Column{
		nullCol("id", schema.TypeInt64),
		nullCol("name", schema.TypeString),
		nullCol("active", schema.TypeBoolean),
		nullCol("score", schema.TypeString), // source has DOUBLE; dest already wider
	})

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_merge_initial.jsonl"),
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
		KeepStaging:         true,
	}
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)), "pipeline should succeed")

	db := openDuckDB(t, destPath)
	staging := findStagingTable(t, db)

	stagingByName := map[string]colInfo{}
	for _, c := range tableColumns(t, db, staging) {
		stagingByName[c.name] = c
	}
	destByName := map[string]colInfo{}
	for _, c := range tableColumns(t, db, destTable) {
		destByName[c.name] = c
	}

	assert.Equal(t, destByName["score"].dataType, stagingByName["score"].dataType,
		"staging.score type must mirror dest, not source")
	assert.Equal(t, "VARCHAR", stagingByName["score"].dataType,
		"staging.score should be VARCHAR (dest's pre-existing type)")
}

// =====================================================================
// Case 4: SCD2. Staging must contain _scd_valid_from, _scd_valid_to,
// _scd_is_current at the END of the column list and every row must
// have those filled.
// =====================================================================
func TestStaging_SCD2HasMetadataColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	ctx := context.Background()
	destURI, destPath, destTable := newDuckDBDest(t)

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_scd2_initial.jsonl"),
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategySCD2,
		PrimaryKeys:         []string{"id"},
		KeepStaging:         true,
	}
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)), "SCD2 pipeline should succeed")

	db := openDuckDB(t, destPath)
	staging := findStagingTable(t, db)

	stagingCols := tableColumns(t, db, staging)
	destCols := tableColumns(t, db, destTable)

	assert.Equal(t, colNames(destCols), colNames(stagingCols),
		"staging schema must match dest exactly (cols + order)")

	require.GreaterOrEqual(t, len(stagingCols), 3, "staging should have at least 3 cols")
	last3 := colNames(stagingCols[len(stagingCols)-3:])
	assert.Equal(t,
		[]string{"_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		last3,
		"SCD2 metadata cols must be appended at the end of staging in canonical order")

	var nullFrom, notCurrent, total int
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s", staging,
	)).Scan(&total))
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE _scd_valid_from IS NULL", staging,
	)).Scan(&nullFrom))
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE _scd_is_current = false", staging,
	)).Scan(&notCurrent))

	assert.Greater(t, total, 0, "staging should have rows")
	assert.Equal(t, 0, nullFrom, "every staging row must have _scd_valid_from set")
	assert.Equal(t, 0, notCurrent, "every staging row must be marked current=true")
}

// =====================================================================
// Case 5: Source narrower — BOOLEAN source, VARCHAR dest. No ALTER.
// Staging must use dest's VARCHAR.
// =====================================================================
func TestStaging_SourceNarrower_BooleanVsVarchar(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	ctx := context.Background()
	destURI, destPath, destTable := newDuckDBDest(t)

	preCreateDest(t, ctx, destURI, destTable, []schema.Column{
		nullCol("id", schema.TypeInt64),
		nullCol("name", schema.TypeString),
		nullCol("active", schema.TypeString), // source BOOLEAN; dest wider
		nullCol("score", schema.TypeFloat64),
	})

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_merge_initial.jsonl"),
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
		KeepStaging:         true,
	}
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)), "pipeline should succeed")

	db := openDuckDB(t, destPath)
	staging := findStagingTable(t, db)

	stagingByName := map[string]colInfo{}
	for _, c := range tableColumns(t, db, staging) {
		stagingByName[c.name] = c
	}
	destByName := map[string]colInfo{}
	for _, c := range tableColumns(t, db, destTable) {
		destByName[c.name] = c
	}

	assert.Equal(t, "VARCHAR", destByName["active"].dataType, "dest stays VARCHAR (no ALTER)")
	assert.Equal(t, "VARCHAR", stagingByName["active"].dataType,
		"staging.active must be VARCHAR (dest's wider type)")
}

// =====================================================================
// Case 6: Mixed drift. Source wider (id), source narrower (active),
// no-drift (name, score), and a soft-removed dest-only col.
// Exercises every code path of buildBufferReaderTarget at once.
// =====================================================================
func TestStaging_MixedDrift_AddWidenSoftRemove(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	ctx := context.Background()
	destURI, destPath, destTable := newDuckDBDest(t)

	preCreateDest(t, ctx, destURI, destTable, []schema.Column{
		nullCol("id", schema.TypeInt32),          // source BIGINT (wider) → ALTER fires
		nullCol("name", schema.TypeString),       // matches
		nullCol("active", schema.TypeString),     // source BOOLEAN (narrower) → no ALTER
		nullCol("score", schema.TypeFloat64),     // matches
		nullCol("deprecated", schema.TypeString), // soft-removed
	})

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_merge_initial.jsonl"),
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
		KeepStaging:         true,
	}
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)), "pipeline should succeed")

	db := openDuckDB(t, destPath)
	staging := findStagingTable(t, db)

	stagingCols := tableColumns(t, db, staging)
	destCols := tableColumns(t, db, destTable)

	assert.Equal(t, colNames(destCols), colNames(stagingCols),
		"staging and dest must have identical column lists in identical order")
	for i := range destCols {
		assert.Equal(t, destCols[i].dataType, stagingCols[i].dataType,
			"col %q: staging=%s dest=%s", destCols[i].name,
			stagingCols[i].dataType, destCols[i].dataType)
	}

	destByName := map[string]colInfo{}
	for _, c := range destCols {
		destByName[c.name] = c
	}
	assert.Equal(t, "BIGINT", destByName["id"].dataType, "id widened INTEGER → BIGINT")
	assert.Equal(t, "VARCHAR", destByName["active"].dataType, "active stays VARCHAR")

	var nullDep, total int
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s", staging,
	)).Scan(&total))
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE deprecated IS NULL", staging,
	)).Scan(&nullDep))
	assert.Equal(t, total, nullDep, "deprecated column must be NULL for every staging row")
	assert.Equal(t, mergeInitialRows, total, "staging should contain all source rows")
}

// =====================================================================
// Case 7: Two consecutive runs, schema drifts between them. After the
// SECOND run, staging from the second pipeline must STILL mirror dest.
// =====================================================================
func TestStaging_SecondRun_StillMirrorsDest(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	ctx := context.Background()
	destURI, destPath, destTable := newDuckDBDest(t)

	preCreateDest(t, ctx, destURI, destTable, []schema.Column{
		nullCol("id", schema.TypeInt32), // source BIGINT will widen
		nullCol("name", schema.TypeString),
		nullCol("active", schema.TypeBoolean),
		nullCol("score", schema.TypeFloat64),
	})

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_merge_initial.jsonl"),
		SourceTable:         "users",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
		KeepStaging:         false, // first run cleans up so the second is unambiguous
	}
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)), "first run")

	cfg.SourceURI = jsonlURI(t, "testdata/conformance_merge_update.jsonl")
	cfg.SourceTable = "users"
	cfg.KeepStaging = true
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)), "second run")

	db := openDuckDB(t, destPath)
	staging := findStagingTable(t, db)
	stagingCols := tableColumns(t, db, staging)
	destCols := tableColumns(t, db, destTable)

	assert.Equal(t, colNames(destCols), colNames(stagingCols),
		"second-run staging must still mirror the (now-evolved) dest")
	for i := range destCols {
		assert.Equal(t, destCols[i].dataType, stagingCols[i].dataType,
			"col %q: staging=%s dest=%s", destCols[i].name,
			stagingCols[i].dataType, destCols[i].dataType)
	}
}
