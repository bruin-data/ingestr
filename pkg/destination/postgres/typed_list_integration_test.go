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
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestWriteTypedListPreservesValues(t *testing.T) {
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
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "items", DataType: schema.TypeArray, ArrayType: schema.TypeInt64, Nullable: true},
		{Name: "uuids", DataType: schema.TypeArray, ArrayType: schema.TypeUUID, Nullable: true},
	}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: "public.typed_lists", Schema: tableSchema}))

	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })
	b := array.NewListBuilder(mem, arrow.PrimitiveTypes.Int64)
	defer b.Release()
	child := b.ValueBuilder().(*array.Int64Builder)
	b.Append(true)
	child.Append(7)
	child.AppendNull()
	child.Append(-2)
	b.Append(true)
	b.AppendNull()
	values := b.NewArray()
	defer values.Release()
	uuidBuilder := array.NewListBuilder(mem, arrow.BinaryTypes.String)
	defer uuidBuilder.Release()
	uuidBuilder.Append(true)
	uuidChild := uuidBuilder.ValueBuilder().(*array.StringBuilder)
	uuidChild.AppendNull()
	uuidChild.Append("01234567-89ab-cdef-0123-456789abcdef")
	uuidBuilder.Append(true)
	uuidBuilder.AppendNull()
	uuids := uuidBuilder.NewArray()
	defer uuids.Release()
	record := array.NewRecordBatch(tableSchema.ToArrowSchema(), []arrow.Array{values, uuids}, 3)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: record}
	close(records)
	require.NoError(t, dest.Write(ctx, records, destination.WriteOptions{Table: "public.typed_lists", Schema: tableSchema}))

	var populated, empty, nulls int
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE items = ARRAY[7,NULL,-2]::bigint[]),
		count(*) FILTER (WHERE items = ARRAY[]::bigint[]),
		count(*) FILTER (WHERE items IS NULL)
		FROM public.typed_lists`).Scan(&populated, &empty, &nulls))
	require.Equal(t, 1, populated)
	require.Equal(t, 1, empty)
	require.Equal(t, 1, nulls)
	require.NoError(t, dest.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE uuids = ARRAY[NULL,'01234567-89ab-cdef-0123-456789abcdef']::uuid[]),
		count(*) FILTER (WHERE uuids = ARRAY[]::uuid[]),
		count(*) FILTER (WHERE uuids IS NULL)
		FROM public.typed_lists`).Scan(&populated, &empty, &nulls))
	require.Equal(t, 1, populated)
	require.Equal(t, 1, empty)
	require.Equal(t, 1, nulls)
}
