//go:build local

// This file is gated by the `local` build tag so CI does not run these tests.
// Run them locally with:
//
//   GONG_TEST_PG_URI="postgres://test:test@localhost:5432/testdb?sslmode=disable" \
//     go test -tags=local -v -count=1 -run TestColumnOverrides_CSVToPostgres ./tests/integration/
//
// CI runs `go test ./tests/integration/...` without `-tags=local`, so this file
// is excluded from compilation and the Postgres tests do not execute there.

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestColumnOverrides_CSVToPostgres_EmptySourceWithOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres destination container not available")
	}

	ctx := context.Background()
	schemaName := uniqueSchemaName(t, "co_empty")
	ensurePostgresSchema(t, ctx, pgDest.uri, schemaName)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, schemaName) })

	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "empty.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvHeadersOnly), 0o644))

	cfg := &config.IngestConfig{
		SourceURI:           "csv://" + csvPath,
		SourceTable:         "empty",
		DestURI:             pgDest.uri,
		DestTable:           schemaName + ".empty",
		IncrementalStrategy: config.StrategyReplace,
		Columns:             "id:bigint,name:string,email:string,age:smallint",
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	types := readPostgresColumnTypes(t, pgDest.uri, schemaName, "empty")
	assert.Equal(t, "bigint", types["id"])
	assert.Equal(t, "text", types["name"])
	assert.Equal(t, "text", types["email"])
	assert.Equal(t, "smallint", types["age"])
	assert.Equal(t, 4, len(types), "table should have exactly the 4 overridden columns")

	assert.Equal(t, 0, readPostgresRowCount(t, pgDest.uri, schemaName, "empty"))
}

func TestColumnOverrides_CSVToPostgres_AppliesTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres destination container not available")
	}

	ctx := context.Background()
	schemaName := uniqueSchemaName(t, "co_apply")
	ensurePostgresSchema(t, ctx, pgDest.uri, schemaName)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, schemaName) })

	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "users.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvWithRows), 0o644))

	cfg := &config.IngestConfig{
		SourceURI:           "csv://" + csvPath,
		SourceTable:         "users",
		DestURI:             pgDest.uri,
		DestTable:           schemaName + ".users",
		IncrementalStrategy: config.StrategyReplace,
		Columns:             "id:bigint,age:smallint",
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	types := readPostgresColumnTypes(t, pgDest.uri, schemaName, "users")
	assert.Equal(t, "bigint", types["id"], "id retyped to bigint")
	assert.Equal(t, "smallint", types["age"], "age retyped to smallint")
	assert.Equal(t, "text", types["name"])
	assert.Equal(t, "text", types["email"])
	assert.Equal(t, 3, readPostgresRowCount(t, pgDest.uri, schemaName, "users"))
}

func readPostgresColumnTypes(t *testing.T, uri, schemaName, tableName string) map[string]string {
	t.Helper()
	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`, schemaName, tableName)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var name, dt string
		require.NoError(t, rows.Scan(&name, &dt))
		out[strings.ToLower(name)] = strings.ToLower(strings.TrimSpace(dt))
	}
	require.NoError(t, rows.Err())
	return out
}

func readPostgresRowCount(t *testing.T, uri, schemaName, tableName string) int {
	t.Helper()
	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM %s`, pqTable(schemaName, tableName)),
	).Scan(&count))
	return count
}
