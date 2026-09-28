//go:build integration

package integration

import (
	"context"
	"net/url"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	postgresdest "github.com/bruin-data/ingestr/pkg/destination/postgres"
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
