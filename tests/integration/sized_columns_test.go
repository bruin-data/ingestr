//go:build integration

// End-to-end tests for sized string columns (VARCHAR(n)) against a real
// Postgres (which enforces length), covering the three ways a length reaches
// the destination:
//   - an explicit --columns varchar(n) override,
//   - auto-capture from a sized source column (postgres -> postgres), and
//   - widening an existing column during schema evolution.
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readPostgresColumnLengths returns column name -> character_maximum_length,
// using 0 for unbounded (NULL) columns such as TEXT.
func readPostgresColumnLengths(t *testing.T, uri, schemaName, tableName string) map[string]int {
	t.Helper()
	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`
		SELECT column_name, COALESCE(character_maximum_length, 0)
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`, schemaName, tableName)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	out := map[string]int{}
	for rows.Next() {
		var name string
		var length int
		require.NoError(t, rows.Scan(&name, &length))
		out[strings.ToLower(name)] = length
	}
	require.NoError(t, rows.Err())
	return out
}

// A sized override produces a bounded VARCHAR(n) on the destination.
func TestSizedColumns_CSVToPostgres_AppliesLength(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	uri := sharedPostgresURI(t, "dest")
	ctx := context.Background()
	schemaName := uniqueSchemaName(t, "sized_apply")
	ensurePostgresSchema(t, ctx, uri, schemaName)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, uri, schemaName) })

	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "users.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvWithRows), 0o644))

	cfg := &config.IngestConfig{
		SourceURI:           "csv://" + csvPath,
		SourceTable:         "users",
		DestURI:             uri,
		DestTable:           schemaName + ".users",
		IncrementalStrategy: config.StrategyReplace,
		Columns:             "name:varchar(50),email:varchar(100)",
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	lengths := readPostgresColumnLengths(t, uri, schemaName, "users")
	assert.Equal(t, 50, lengths["name"], "name should be VARCHAR(50)")
	assert.Equal(t, 100, lengths["email"], "email should be VARCHAR(100)")
	assert.Equal(t, 0, lengths["id"], "unspecified column stays unbounded")
}

// A sized source column (VARCHAR(50)) is preserved across a plain copy with no
// --columns, while an unbounded TEXT column stays unbounded.
func TestSizedColumns_PostgresToPostgres_PreservesLength(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	srcURI := sharedPostgresURI(t, "source")
	dstURI := sharedPostgresURI(t, "dest")
	ctx := context.Background()
	srcSchema := uniqueSchemaName(t, "sized_src")
	dstSchema := uniqueSchemaName(t, "sized_dst")
	ensurePostgresSchema(t, ctx, srcURI, srcSchema)
	ensurePostgresSchema(t, ctx, dstURI, dstSchema)
	t.Cleanup(func() {
		dropPostgresSchema(t, ctx, srcURI, srcSchema)
		dropPostgresSchema(t, ctx, dstURI, dstSchema)
	})

	srcDB, err := sql.Open("pgx", srcURI)
	require.NoError(t, err)
	defer func() { _ = srcDB.Close() }()

	_, err = srcDB.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s (id bigint, name varchar(50), bio text)`, pqTable(srcSchema, "people"),
	))
	require.NoError(t, err)
	_, err = srcDB.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s VALUES (1,'Alice','hi'),(2,'Bob','yo')`, pqTable(srcSchema, "people"),
	))
	require.NoError(t, err)

	cfg := &config.IngestConfig{
		SourceURI:           srcURI,
		SourceTable:         srcSchema + ".people",
		DestURI:             dstURI,
		DestTable:           dstSchema + ".people",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	lengths := readPostgresColumnLengths(t, dstURI, dstSchema, "people")
	assert.Equal(t, 50, lengths["name"], "explicit varchar(50) preserved from source")
	assert.Equal(t, 0, lengths["bio"], "unbounded text stays unbounded")
}

// An existing bounded column is widened (never narrowed) during schema
// evolution, so a value longer than the original limit lands successfully.
func TestSizedColumns_WidensLengthOnEvolution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	uri := sharedPostgresURI(t, "dest")
	ctx := context.Background()
	schemaName := uniqueSchemaName(t, "sized_widen")
	ensurePostgresSchema(t, ctx, uri, schemaName)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, uri, schemaName) })

	tmpDir := t.TempDir()
	csv1 := filepath.Join(tmpDir, "u1.csv")
	require.NoError(t, os.WriteFile(csv1, []byte("id,name\n1,Alice\n"), 0o644))

	run1 := &config.IngestConfig{
		SourceURI:           "csv://" + csv1,
		SourceTable:         "u",
		DestURI:             uri,
		DestTable:           schemaName + ".widen",
		IncrementalStrategy: config.StrategyAppend,
		Columns:             "name:varchar(50)",
	}
	require.NoError(t, run1.Validate())
	require.NoError(t, pipeline.New(run1).Run(ctx))
	require.Equal(t, 50, readPostgresColumnLengths(t, uri, schemaName, "widen")["name"])

	longName := strings.Repeat("x", 70) // longer than 50, fits in 100
	csv2 := filepath.Join(tmpDir, "u2.csv")
	require.NoError(t, os.WriteFile(csv2, []byte("id,name\n2,"+longName+"\n"), 0o644))

	run2 := &config.IngestConfig{
		SourceURI:           "csv://" + csv2,
		SourceTable:         "u",
		DestURI:             uri,
		DestTable:           schemaName + ".widen",
		IncrementalStrategy: config.StrategyAppend,
		Columns:             "name:varchar(100)",
	}
	require.NoError(t, run2.Validate())
	require.NoError(t, pipeline.New(run2).Run(ctx))

	assert.Equal(t, 100, readPostgresColumnLengths(t, uri, schemaName, "widen")["name"], "column widened to 100")

	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var maxLen int
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT COALESCE(MAX(char_length(name)), 0) FROM %s", pqTable(schemaName, "widen"),
	)).Scan(&maxLen))
	assert.Equal(t, 70, maxLen, "the 70-char row must have landed after widening")
}
