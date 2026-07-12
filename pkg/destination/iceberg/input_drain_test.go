package iceberg

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestRecordBatchInputReleasesCurrentErrorAndTrailingBatches(t *testing.T) {
	mem := checkedDrainAllocator(t)
	sc := drainTestArrowSchema("id")
	records := make(chan source.RecordBatchResult, 3)
	records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, sc, 1)}
	sourceErr := errors.New("source failed")
	records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, sc, 2), Err: sourceErr}
	records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, sc, 3)}
	close(records)

	input := newRecordBatchInputWithDrainTimeout(records, time.Second)
	reader := input.RecordReader(context.Background(), sc)
	require.True(t, reader.Next())
	require.False(t, reader.Next())
	require.ErrorIs(t, reader.Err(), sourceErr)
	input.Close()
	waitForDrain(t, input.done)
	require.Zero(t, mem.CurrentAlloc())
}

func TestRecordBatchInputCloseUnblocksBlockedAndDelayedProducers(t *testing.T) {
	for _, delay := range []time.Duration{0, 25 * time.Millisecond} {
		t.Run(delay.String(), func(t *testing.T) {
			mem := checkedDrainAllocator(t)
			sc := drainTestArrowSchema("id")
			records := make(chan source.RecordBatchResult)
			producerDone := make(chan struct{})
			go func() {
				defer close(producerDone)
				if delay > 0 {
					time.Sleep(delay)
				}
				records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, sc, 1)}
				close(records)
			}()

			input := newRecordBatchInputWithDrainTimeout(records, time.Second)
			input.Close()
			waitForDrain(t, producerDone)
			waitForDrain(t, input.done)
			require.Zero(t, mem.CurrentAlloc())
		})
	}
}

func TestRecordBatchInputDrainIsBounded(t *testing.T) {
	records := make(chan source.RecordBatchResult)
	input := newRecordBatchInputWithDrainTimeout(records, 20*time.Millisecond)
	started := time.Now()
	input.Close()
	waitForDrain(t, input.done)
	require.Less(t, time.Since(started), time.Second)

	nilInput := newRecordBatchInputWithDrainTimeout(nil, time.Hour)
	nilInput.Close()
	select {
	case <-nilInput.done:
	default:
		require.Fail(t, "nil input did not close synchronously")
	}
}

func TestIcebergWriteEntrypointPreflightFailuresDrainBlockedProducers(t *testing.T) {
	calls := map[string]func(*Destination, <-chan source.RecordBatchResult) error{
		"append_replace": func(dest *Destination, records <-chan source.RecordBatchResult) error {
			return dest.WriteParallel(context.Background(), records, destination.WriteOptions{})
		},
		"direct_merge": func(dest *Destination, records <-chan source.RecordBatchResult) error {
			return dest.MergeRecords(context.Background(), records, destination.WriteOptions{}, destination.MergeOptions{})
		},
		"truncate_insert": func(dest *Destination, records <-chan source.RecordBatchResult) error {
			return dest.TruncateInsertRecords(context.Background(), records, destination.WriteOptions{})
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			mem := checkedDrainAllocator(t)
			sc := drainTestArrowSchema("id")
			records := make(chan source.RecordBatchResult)
			producerDone := make(chan struct{})
			go func() {
				defer close(producerDone)
				records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, sc, 1)}
				close(records)
			}()

			require.Error(t, call(NewDestination(), records))
			waitForDrain(t, producerDone)
			requireDrainAllocatorZero(t, mem)
		})
	}
}

func TestWriteParallelTransformFailureDrainsDelayedProducer(t *testing.T) {
	dest := newHadoopDestination(t)
	tableSchema := &schema.TableSchema{
		Columns:     []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}},
		PrimaryKeys: []string{"id"},
	}
	table := "lake.correctness.transform_drain"
	require.NoError(t, dest.PrepareTable(context.Background(), destination.PrepareOptions{
		Table: table, Schema: tableSchema, PrimaryKeys: []string{"id"}, DropFirst: true,
	}))

	mem := checkedDrainAllocator(t)
	records := make(chan source.RecordBatchResult)
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		time.Sleep(20 * time.Millisecond)
		records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, icebergArrowSchema(tableSchema), 1)}
		close(records)
	}()
	err := dest.WriteParallel(context.Background(), records, destination.WriteOptions{
		Table: table, Schema: tableSchema, DeduplicatePrimaryKeys: true, IncrementalKey: "missing",
	})
	require.ErrorContains(t, err, `incremental key column "missing" not found`)
	waitForDrain(t, producerDone)
	requireDrainAllocatorZero(t, mem)
}

func TestMergeRecordsReaderFailureReleasesErrorAndTrailingBatches(t *testing.T) {
	dest := newHadoopDestination(t)
	tableSchema := &schema.TableSchema{
		Columns:     []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}},
		PrimaryKeys: []string{"id"},
	}
	table := "lake.correctness.merge_reader_drain"
	require.NoError(t, dest.PrepareTable(context.Background(), destination.PrepareOptions{
		Table: table, Schema: tableSchema, PrimaryKeys: []string{"id"},
	}))

	mem := checkedDrainAllocator(t)
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, drainTestArrowSchema("wrong_id"), 1)}
	records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, icebergArrowSchema(tableSchema), 2)}
	close(records)
	err := dest.MergeRecords(context.Background(), records, destination.WriteOptions{
		Table: table, Schema: tableSchema,
	}, destination.MergeOptions{
		TargetTable: table, PrimaryKeys: []string{"id"}, Columns: []string{"id"}, Schema: tableSchema,
	})
	require.ErrorContains(t, err, "name mismatch")
	require.Zero(t, mem.CurrentAlloc())
}

func TestAlreadyCommittedWriteDrainReportsSourceErrorAndReleasesTrailingBatch(t *testing.T) {
	dest := newHadoopDestination(t)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}},
	}
	table := "lake.correctness.retry_drain"
	require.NoError(t, dest.PrepareTable(context.Background(), destination.PrepareOptions{
		Table: table, Schema: tableSchema,
	}))
	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: "committed-token"}
	require.NoError(t, dest.WriteParallel(
		context.Background(),
		recordBatches(int64Batch(t, 1)),
		opts,
	))

	mem := checkedDrainAllocator(t)
	sourceErr := errors.New("retry source failed")
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{
		Batch: checkedDrainBatch(mem, icebergArrowSchema(tableSchema), 2),
		Err:   sourceErr,
	}
	records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, icebergArrowSchema(tableSchema), 3)}
	close(records)
	err := dest.WriteParallel(context.Background(), records, opts)
	require.ErrorIs(t, err, sourceErr)
	require.Empty(t, records)
	require.Zero(t, mem.CurrentAlloc())
}

func TestWriteParallelCancellationDuringFileWriteDrainsProducer(t *testing.T) {
	dest := newHadoopDestination(t)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}},
	}
	table := "lake.correctness.file_write_cancel_drain"
	require.NoError(t, dest.PrepareTable(context.Background(), destination.PrepareOptions{
		Table: table, Schema: tableSchema,
	}))
	dest.catalog = loadIgnoringContextCatalog{Catalog: dest.catalog}

	mem := checkedDrainAllocator(t)
	records := make(chan source.RecordBatchResult)
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		records <- source.RecordBatchResult{Batch: checkedDrainBatch(mem, icebergArrowSchema(tableSchema), 1)}
		close(records)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := dest.WriteParallel(ctx, records, destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "canceled-write",
	})
	require.Error(t, err)
	waitForDrain(t, producerDone)
	require.Zero(t, mem.CurrentAlloc())
}

type loadIgnoringContextCatalog struct {
	icebergcatalog.Catalog
}

func (c loadIgnoringContextCatalog) LoadTable(
	_ context.Context,
	ident icebergtable.Identifier,
) (*icebergtable.Table, error) {
	return c.Catalog.LoadTable(context.Background(), ident)
}

func checkedDrainAllocator(t *testing.T) *memory.CheckedAllocator {
	t.Helper()
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })
	return mem
}

func drainTestArrowSchema(name string) *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{{Name: name, Type: arrow.PrimitiveTypes.Int64, Nullable: false}}, nil)
}

func checkedDrainBatch(mem memory.Allocator, sc *arrow.Schema, value int64) arrow.RecordBatch {
	builder := array.NewInt64Builder(mem)
	builder.Append(value)
	values := builder.NewArray()
	builder.Release()
	batch := array.NewRecordBatch(sc, []arrow.Array{values}, 1)
	values.Release()
	return batch
}

func waitForDrain(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.Fail(t, "timed out waiting for record-batch drain")
	}
}

func requireDrainAllocatorZero(t *testing.T, mem *memory.CheckedAllocator) {
	t.Helper()
	require.Eventually(t, func() bool {
		return mem.CurrentAlloc() == 0
	}, 2*time.Second, time.Millisecond)
}
