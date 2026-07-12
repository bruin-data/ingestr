//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestLateCDCTargetAtomicCreateAndConditionalTruncate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "postgres:16-alpine",
			Env: map[string]string{
				"POSTGRES_USER": "testuser", "POSTGRES_PASSWORD": "testpass", "POSTGRES_DB": "testdb",
			},
			ExposedPorts: []string{"5432/tcp"},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	defer func() { _ = container.Terminate(context.Background()) }()
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	dest := NewPostgresDestination()
	require.NoError(t, dest.Connect(ctx, fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())))
	defer func() { _ = dest.Close(context.Background()) }()
	require.NoError(t, dest.Exec(ctx, `
		CREATE SCHEMA _bruin_staging;
		CREATE TABLE _bruin_staging.cdc_targets (
			destination_table text PRIMARY KEY,
			connector_id text NOT NULL,
			claimed_at timestamptz NOT NULL
		);
	`))
	claim := destination.CDCTargetClaim{
		DestinationTable: "public.events",
		ConnectorID:      "connector-a",
		SourceTable:      "public.events",
	}
	opts := destination.PrepareOptions{Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}}
	incarnation, err := dest.ClaimAndPrepareEmptyCDCTarget(ctx, "_bruin_staging.cdc_targets", claim, opts)
	require.NoError(t, err)
	require.NotEmpty(t, incarnation)
	require.NoError(t, dest.Exec(ctx, `INSERT INTO public.events VALUES (1)`))
	require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, "public.events", incarnation))

	var count int
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM public.events`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, dest.Exec(ctx, `DROP TABLE public.events; CREATE TABLE public.events (id bigint); INSERT INTO public.events VALUES (7)`))
	err = dest.TruncateCDCTableIfIncarnation(ctx, "public.events", incarnation)
	require.ErrorContains(t, err, "physical incarnation changed")
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM public.events`).Scan(&count))
	require.Equal(t, 1, count)

	require.NoError(t, dest.Exec(ctx, `CREATE TABLE public.write_fence (id bigint)`))
	writeIncarnation, exists, err := dest.CDCTargetIncarnation(ctx, "public.write_fence")
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, dest.WriteParallel(ctx, postgresInt64RecordBatches(2, 3), destination.WriteOptions{
		Table:                  "public.write_fence",
		Schema:                 opts.Schema,
		Parallelism:            1,
		CDCExpectedIncarnation: writeIncarnation,
	}))
	var values []int64
	rows, err := dest.pool.Query(ctx, `SELECT id FROM public.write_fence ORDER BY id`)
	require.NoError(t, err)
	for rows.Next() {
		var value int64
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, []int64{2, 3}, values)
	require.NoError(t, dest.Exec(ctx, `DROP TABLE public.write_fence; CREATE TABLE public.write_fence (id bigint); INSERT INTO public.write_fence VALUES (7)`))
	err = dest.WriteParallel(ctx, postgresInt64RecordBatches(9), destination.WriteOptions{
		Table:                  "public.write_fence",
		Schema:                 opts.Schema,
		Parallelism:            1,
		CDCExpectedIncarnation: writeIncarnation,
	})
	require.ErrorContains(t, err, "physical incarnation changed before write")
	values = nil
	rows, err = dest.pool.Query(ctx, `SELECT id FROM public.write_fence ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var value int64
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int64{7}, values)

	require.NoError(t, dest.Exec(ctx, `CREATE TABLE public.evolve_fence (id bigint)`))
	evolutionIncarnation, exists, err := dest.CDCTargetIncarnation(ctx, "public.evolve_fence")
	require.NoError(t, err)
	require.True(t, exists)
	_, err = dest.ApplySchemaEvolutionIfIncarnation(
		ctx,
		"public.evolve_fence",
		postgresAddColumnComparison("fresh_column"),
		evolutionIncarnation,
	)
	require.NoError(t, err)
	require.NoError(t, dest.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'evolve_fence' AND column_name = 'fresh_column'
	`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, dest.Exec(ctx, `DROP TABLE public.evolve_fence; CREATE TABLE public.evolve_fence (id bigint)`))
	_, err = dest.ApplySchemaEvolutionIfIncarnation(
		ctx,
		"public.evolve_fence",
		postgresAddColumnComparison("replacement_column"),
		evolutionIncarnation,
	)
	require.ErrorContains(t, err, "physical incarnation changed before schema evolution")
	require.NoError(t, dest.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'evolve_fence' AND column_name = 'replacement_column'
	`).Scan(&count))
	require.Zero(t, count)

	require.NoError(t, dest.Exec(ctx, `CREATE TABLE public.external_events (id bigint)`))
	claim.DestinationTable = "public.external_events"
	claim.SourceTable = "public.external_events"
	_, err = dest.ClaimAndPrepareEmptyCDCTarget(ctx, "_bruin_staging.cdc_targets", claim, opts)
	require.Error(t, err)
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT COUNT(*) FROM _bruin_staging.cdc_targets WHERE destination_table = $1`, destination.CDCTargetKey("public", "external_events")).Scan(&count))
	require.Zero(t, count)
}

func postgresInt64RecordBatches(values ...int64) <-chan source.RecordBatchResult {
	records := make(chan source.RecordBatchResult, len(values))
	for _, value := range values {
		builder := array.NewInt64Builder(memory.DefaultAllocator)
		builder.Append(value)
		column := builder.NewArray()
		builder.Release()
		record := array.NewRecordBatch(
			arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil),
			[]arrow.Array{column},
			1,
		)
		column.Release()
		records <- source.RecordBatchResult{Batch: record}
	}
	close(records)
	return records
}

func postgresAddColumnComparison(name string) *schemaevolution.SchemaComparison {
	return &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type:       schemaevolution.ChangeAddColumn,
			ColumnName: name,
			NewColumn:  schema.Column{Name: name, DataType: schema.TypeString, Nullable: true},
		}},
	}
}
