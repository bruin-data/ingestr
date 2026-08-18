//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPostgresCDC_TableSubset covers the whole contract for a comma-separated
// --source-table against PostgreSQL: only the named tables reach the
// destination, both spellings of a public-schema table work, a table created
// later is ignored because the selection pins the capture set, and the
// publication is deliberately left covering everything.
func TestPostgresCDC_TableSubset(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `CREATE TABLE public.users (id INT PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.users (id, name) VALUES (1, 'alice'), (2, 'bob')`)
	require.NoError(t, err)

	_, err = srcPool.Exec(ctx, `CREATE SCHEMA sales`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `CREATE TABLE sales.orders (id INT PRIMARY KEY, amount INT)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO sales.orders (id, amount) VALUES (1, 100)`)
	require.NoError(t, err)

	// Eligible, but not selected: it must never reach the destination.
	_, err = srcPool.Exec(ctx, `CREATE TABLE public.products (id INT PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.products (id, name) VALUES (1, 'widget')`)
	require.NoError(t, err)

	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	destSchema := uniqueSchemaName(t, "cdc_subset")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })

	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] +
			"&mode=batch&dest_schema=" + destSchema,
		// "public.users" is spelled with its schema even though the connector
		// reports it bare; "sales.orders" must stay qualified.
		SourceTables: []string{"public.users", "sales.orders"},
		DestURI:      pgDest.uri,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })

	var count int
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.users`, destSchema)).Scan(&count))
	assert.Equal(t, 2, count, "users snapshot")
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q."sales_orders"`, destSchema)).Scan(&count))
	assert.Equal(t, 1, count, "sales.orders snapshot")

	assertRelationAbsent(t, ctx, pg, destSchema, "products")

	// The managed publication still covers everything: narrowing it would make
	// concurrent pipelines against this database fight over its table set.
	var published int
	require.NoError(t, srcPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pg_publication_tables WHERE pubname = 'ingestr_publication'`).Scan(&published))
	assert.Equal(t, 3, published, "the managed publication must not be narrowed by a table subset")

	// A table created after the first run is not picked up: the selection pins
	// the capture set.
	_, err = srcPool.Exec(ctx, `CREATE TABLE public.invoices (id INT PRIMARY KEY, total INT)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `INSERT INTO public.invoices (id, total) VALUES (1, 5)`)
	require.NoError(t, err)

	// Changes to the selected tables still propagate on the next run.
	_, err = srcPool.Exec(ctx, `INSERT INTO public.users (id, name) VALUES (3, 'carol')`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `UPDATE sales.orders SET amount = 250 WHERE id = 1`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q.users`, destSchema)).Scan(&count))
	assert.Equal(t, 3, count, "users after CDC insert")
	var amount int
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT amount FROM %q."sales_orders" WHERE id = 1`, destSchema)).Scan(&amount))
	assert.Equal(t, 250, amount, "sales.orders after CDC update")

	assertRelationAbsent(t, ctx, pg, destSchema, "products")
	assertRelationAbsent(t, ctx, pg, destSchema, "invoices")
}

// TestPostgresCDC_TableSubsetRejectsUnknownTable proves the hard-error
// contract, including the specific reason for a table that exists but cannot be
// replicated -- that exclusion happens in SQL, so it would otherwise be
// reported as simply not found.
func TestPostgresCDC_TableSubsetRejectsUnknownTable(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `CREATE TABLE public.users (id INT PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `CREATE TABLE public.logs_nopk (id INT, msg TEXT)`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	destSchema := uniqueSchemaName(t, "cdc_subset_err")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })

	base := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&mode=batch&dest_schema=" + destSchema

	typo := &config.IngestConfig{
		SourceURI:    base,
		SourceTables: []string{"users", "userz"},
		DestURI:      pgDest.uri,
	}
	err = pipeline.New(typo).Run(ctx)
	require.Error(t, err, "a typo must fail the run rather than quietly ingesting less")
	assert.Contains(t, err.Error(), "userz")

	keyless := &config.IngestConfig{
		SourceURI:    base,
		SourceTables: []string{"users", "logs_nopk"},
		DestURI:      pgDest.uri,
	}
	err = pipeline.New(keyless).Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replica identity",
		"a table that exists but cannot be replicated must say why, not report as missing")
}
