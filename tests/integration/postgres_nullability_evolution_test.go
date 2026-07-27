//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestPostgresSchemaEvolutionRelaxesNullability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := sharedPostgresURI(t, "source")
	destURI := sharedPostgresURI(t, "dest")
	sourceSchema := uniqueSchemaName(t, "src_nullability")
	destSchema := uniqueSchemaName(t, "dst_nullability")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() {
		dropPostgresSchema(t, ctx, sourceURI, sourceSchema)
		dropPostgresSchema(t, ctx, destURI, destSchema)
	})

	sourceDB, err := sql.Open("pgx", sourceURI)
	require.NoError(t, err)
	defer func() { require.NoError(t, sourceDB.Close()) }()
	_, err = sourceDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s.events (id BIGINT PRIMARY KEY, payload TEXT NULL);
		INSERT INTO %s.events VALUES (1, 'ready')`, sourceSchema, sourceSchema))
	require.NoError(t, err)

	destDB, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	defer func() { require.NoError(t, destDB.Close()) }()
	_, err = destDB.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s.events (id BIGINT PRIMARY KEY, payload TEXT NOT NULL)`, destSchema,
	))
	require.NoError(t, err)

	err = pipeline.New(&config.IngestConfig{
		SourceURI: sourceURI, SourceTable: sourceSchema + ".events",
		DestURI: destURI, DestTable: destSchema + ".events",
		IncrementalStrategy: config.StrategyAppend,
		PrimaryKeys:         []string{"id"}, NoLoadTimestamp: true,
	}).Run(ctx)
	require.NoError(t, err)

	var nullable string
	err = destDB.QueryRowContext(ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'events' AND column_name = 'payload'`, destSchema).Scan(&nullable)
	require.NoError(t, err)
	require.Equal(t, "YES", nullable)
}
