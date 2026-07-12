//go:build integration

package postgres_cdc

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresArrayDelimiterMetadataFromContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.array_delimiters (id bigint PRIMARY KEY, boxes box[]);
		INSERT INTO public.array_delimiters VALUES (1, ARRAY[box(point(1, 1), point(0, 0)), box(point(2, 2), point(1, 1))]);
	`)
	require.NoError(t, err)

	tableSchema, err := getTableSchema(ctx, pool, "public.array_delimiters")
	require.NoError(t, err)
	var boxes schema.Column
	for _, column := range tableSchema.Columns {
		if column.Name == "boxes" {
			boxes = column
			break
		}
	}
	require.Equal(t, schema.TypeArray, boxes.DataType)
	require.Equal(t, ";", boxes.ArrayDelimiter)

	var literal string
	require.NoError(t, pool.QueryRow(ctx, `SELECT boxes::text FROM public.array_delimiters WHERE id = 1`).Scan(&literal))
	value, err := convertTextValue(literal, boxes)
	require.NoError(t, err)
	require.Equal(t, []interface{}{"(1,1),(0,0)", "(2,2),(1,1)"}, value)
}
