//go:build integration

package integration

import (
	"context"
	"net/url"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	postgresdest "github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestPostgresDestinationPrepareQuotedTableNames(t *testing.T) {
	if pgDest.uri == "" {
		t.Skip("shared postgres destination container not available")
	}

	ctx := t.Context()
	schemaName := uniqueSchemaName(t, "quoted_prepare")
	ensurePostgresSchema(t, ctx, pgDest.uri, schemaName)
	t.Cleanup(func() { dropPostgresSchema(t, context.Background(), pgDest.uri, schemaName) })

	uri, err := url.Parse(pgDest.uri)
	require.NoError(t, err)
	query := uri.Query()
	query.Set("search_path", schemaName)
	uri.RawQuery = query.Encode()
	conn, err := pgx.Connect(ctx, uri.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	for _, fixedSchema := range []bool{false, true} {
		name := "postgres search path"
		if fixedSchema {
			name = "fixed schema for Redshift"
		}
		t.Run(name, func(t *testing.T) {
			dest := postgresdest.NewPostgresDestination()
			if fixedSchema {
				dest.DefaultSchema = schemaName
			}
			require.NoError(t, dest.Connect(ctx, uri.String()))
			t.Cleanup(func() { _ = dest.Close(context.Background()) })

			for _, table := range []string{`"Order.Events"`, pgx.Identifier{schemaName, `Order"Events`}.Sanitize()} {
				t.Run(table, func(t *testing.T) {
					opts := destination.PrepareOptions{
						Table: table,
						Schema: &schema.TableSchema{Columns: []schema.Column{
							{Name: "id", DataType: schema.TypeInt64},
						}},
					}
					require.NoError(t, dest.PrepareTable(ctx, opts))
					_, err := conn.Exec(ctx, "INSERT INTO "+table+" VALUES (1)")
					require.NoError(t, err)
					opts.DropFirst = true
					require.NoError(t, dest.PrepareTable(ctx, opts))
					var count int
					require.NoError(t, conn.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count))
					require.Zero(t, count, "DropFirst must recreate the same quoted table")
				})
			}
		})
	}
}

func TestPostgresDestinationSkipsGeneratedColumns(t *testing.T) {
	if pgDest.uri == "" {
		t.Skip("shared postgres destination container not available")
	}

	ctx := t.Context()
	schemaName := uniqueSchemaName(t, "generated_cols")
	ensurePostgresSchema(t, ctx, pgDest.uri, schemaName)
	t.Cleanup(func() { dropPostgresSchema(t, context.Background(), pgDest.uri, schemaName) })

	conn, err := pgx.Connect(ctx, pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	sources := map[string]string{
		"plain":     "(id int PRIMARY KEY, a int, b int)",
		"generated": "(id int PRIMARY KEY, a int, b int, total int GENERATED ALWAYS AS (a + b) STORED)",
	}
	for name, columns := range sources {
		src := pgx.Identifier{schemaName, name}.Sanitize()
		_, err = conn.Exec(ctx, "CREATE TABLE "+src+" "+columns+"; INSERT INTO "+src+" (id, a, b) VALUES (1, 2, 3), (2, 5, 6)")
		require.NoError(t, err)
	}

	for name := range sources {
		for _, strategy := range []config.IncrementalStrategy{config.StrategyAppend, config.StrategyMerge} {
			t.Run(name+"/"+string(strategy), func(t *testing.T) {
				dstName := "dst_" + name + "_" + string(strategy)
				dst := pgx.Identifier{schemaName, dstName}.Sanitize()
				_, err := conn.Exec(ctx, "CREATE TABLE "+dst+" "+sources["generated"])
				require.NoError(t, err)

				cfg := &config.IngestConfig{
					SourceURI:           pgDest.uri,
					SourceTable:         schemaName + "." + name,
					DestURI:             pgDest.uri,
					DestTable:           schemaName + "." + dstName,
					IncrementalStrategy: strategy,
					PrimaryKeys:         []string{"id"},
				}
				require.NoError(t, cfg.Validate())
				require.NoError(t, pipeline.New(cfg).Run(ctx))

				rows, err := conn.Query(ctx, "SELECT total FROM "+dst+" ORDER BY id")
				require.NoError(t, err)
				totals, err := pgx.CollectRows(rows, pgx.RowTo[int])
				require.NoError(t, err)
				require.Equal(t, []int{5, 11}, totals)
			})
		}
	}
}
