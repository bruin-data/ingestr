//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresToPostgresTrimWhitespaceBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceURI := sharedPostgresURI(t, "source")
	destURI := sharedPostgresURI(t, "dest")
	sourceSchema := uniqueSchemaName(t, "src_trim")
	destSchema := uniqueSchemaName(t, "dst_trim")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() {
		dropPostgresSchema(t, ctx, sourceURI, sourceSchema)
		dropPostgresSchema(t, ctx, destURI, destSchema)
	})

	sourceDB, err := sql.Open("pgx", sourceURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sourceDB.Close() })

	sourceTable := pqTable(sourceSchema, "trim_source")
	_, err = sourceDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id INTEGER PRIMARY KEY,
			name TEXT,
			code VARCHAR(20),
			note TEXT,
			amount INTEGER
		)
	`, sourceTable))
	require.NoError(t, err)

	_, err = sourceDB.ExecContext(
		ctx, fmt.Sprintf(`
		INSERT INTO %s (id, name, code, note, amount) VALUES
			($1, $2, $3, $4, $5),
			($6, $7, $8, $9, $10)
	`, sourceTable),
		1, "  Alice  ", "  A-1  ", " keep  inner  space ", 10,
		2, "\tBob\n", " B-2 ", nil, 20,
	)
	require.NoError(t, err)

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         sourceSchema + ".trim_source",
		DestURI:             destURI,
		DestTable:           destSchema + ".trim_dest",
		IncrementalStrategy: "replace",
		TrimWhitespace:      true,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	destDB, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = destDB.Close() })

	rows, err := destDB.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, name, code, note, amount FROM %s ORDER BY id`,
		pqTable(destSchema, "trim_dest"),
	))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	type trimRow struct {
		id     int
		name   string
		code   string
		note   sql.NullString
		amount int
	}

	var got []trimRow
	for rows.Next() {
		var row trimRow
		require.NoError(t, rows.Scan(&row.id, &row.name, &row.code, &row.note, &row.amount))
		got = append(got, row)
	}
	require.NoError(t, rows.Err())

	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].id)
	assert.Equal(t, "Alice", got[0].name)
	assert.Equal(t, "A-1", got[0].code)
	assert.True(t, got[0].note.Valid)
	assert.Equal(t, "keep  inner  space", got[0].note.String)
	assert.Equal(t, 10, got[0].amount)

	assert.Equal(t, 2, got[1].id)
	assert.Equal(t, "Bob", got[1].name)
	assert.Equal(t, "B-2", got[1].code)
	assert.False(t, got[1].note.Valid)
	assert.Equal(t, 20, got[1].amount)
}
