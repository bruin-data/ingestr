package destination_test

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/mssql"
	"github.com/bruin-data/ingestr/pkg/destination/mysql"
	"github.com/bruin-data/ingestr/pkg/destination/oracle"
	"github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/destination/redshift"
	"github.com/bruin-data/ingestr/pkg/destination/sqlite"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestDestinationWritersReleaseBatchAttachedToError(t *testing.T) {
	testCases := []struct {
		name  string
		dest  destination.Destination
		opts  destination.WriteOptions
		write func(context.Context, destination.Destination, <-chan source.RecordBatchResult, destination.WriteOptions) error
	}{
		{name: "postgres serial", dest: postgres.NewPostgresDestination(), write: writeSerial},
		{name: "postgres parallel", dest: postgres.NewPostgresDestination(), opts: destination.WriteOptions{Parallelism: 2}, write: writeParallel},
		{name: "mysql", dest: mysql.NewMySQLDestination(), write: writeParallel},
		{name: "mssql serial", dest: mssql.NewMSSQLDestination(), write: writeParallel},
		{name: "mssql parallel", dest: mssql.NewMSSQLDestination(), opts: destination.WriteOptions{StagingTable: true, Parallelism: 2}, write: writeParallel},
		{name: "sqlite", dest: sqlite.NewSQLiteDestination(), write: writeParallel},
		{name: "oracle", dest: oracle.NewOracleDestination(), write: writeParallel},
		{name: "redshift", dest: redshift.NewRedshiftDestination(), write: writeParallel},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			allocator := memory.NewCheckedAllocator(memory.NewGoAllocator())
			t.Cleanup(func() { allocator.AssertSize(t, 0) })
			batch := checkedRecordBatch(allocator)
			wantErr := errors.New("source failed")
			records := make(chan source.RecordBatchResult, 1)
			records <- source.RecordBatchResult{Batch: batch, Err: wantErr}
			close(records)

			err := testCase.write(t.Context(), testCase.dest, records, testCase.opts)
			require.ErrorIs(t, err, wantErr)
		})
	}
}

func checkedRecordBatch(allocator memory.Allocator) arrow.RecordBatch {
	builder := array.NewInt64Builder(allocator)
	builder.Append(1)
	values := builder.NewArray()
	builder.Release()
	record := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil),
		[]arrow.Array{values},
		1,
	)
	values.Release()
	return record
}

func writeSerial(ctx context.Context, dest destination.Destination, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return dest.Write(ctx, records, opts)
}

func writeParallel(ctx context.Context, dest destination.Destination, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return dest.WriteParallel(ctx, records, opts)
}
