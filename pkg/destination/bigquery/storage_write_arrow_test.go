package bigquery

import (
	"context"
	"errors"
	"io"
	"runtime"
	"testing"
	"time"

	storagepb "cloud.google.com/go/bigquery/storage/apiv1/storagepb"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type failingAppendRowsClient struct {
	grpc.ClientStream
	err error
}

func (c *failingAppendRowsClient) Send(*storagepb.AppendRowsRequest) error { return c.err }
func (c *failingAppendRowsClient) Recv() (*storagepb.AppendRowsResponse, error) {
	return nil, io.EOF
}
func (c *failingAppendRowsClient) CloseSend() error { return nil }

func TestAppendArrowStreamFromSourceReleasesWorkerQueueOnFailure(t *testing.T) {
	allocator := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { allocator.AssertSize(t, 0) })
	wantErr := errors.New("worker failed with queued batches")
	client := &StorageWriteArrowClient{
		appendWorker: func(ctx context.Context, _ string, records <-chan arrow.RecordBatch, _ int) error {
			for len(records) < cap(records) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					runtime.Gosched()
				}
			}
			return wantErr
		},
	}

	records := make(chan source.RecordBatchResult, 8)
	for i := 0; i < cap(records); i++ {
		records <- source.RecordBatchResult{Batch: makeCheckedRecordBatch(allocator, int64(i))}
	}
	close(records)

	err := client.AppendArrowStreamFromSource(t.Context(), "projects/p/datasets/d/tables/t/streams/_default", records, 1)
	if !errors.Is(err, wantErr) {
		t.Fatalf("AppendArrowStreamFromSource() error = %v, want %v", err, wantErr)
	}
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
}

func TestAppendArrowStreamWorkerReleasesDequeuedBatchWhenStreamOpenFails(t *testing.T) {
	allocator := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { allocator.AssertSize(t, 0) })
	wantErr := errors.New("open stream failed")
	client := &StorageWriteArrowClient{
		streamOpener: func(context.Context, int) (*streamState, error) {
			return nil, wantErr
		},
	}
	records := make(chan arrow.RecordBatch, 1)
	records <- makeCheckedRecordBatch(allocator, 1)
	close(records)

	err := client.appendArrowStreamWorker(t.Context(), "projects/p/datasets/d/tables/t/streams/_default", records, 0)
	if !errors.Is(err, wantErr) {
		t.Fatalf("appendArrowStreamWorker() error = %v, want %v", err, wantErr)
	}
}

func TestConsumeSplitBatchesReleasesUnsentSlicesAfterFailure(t *testing.T) {
	allocator := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { allocator.AssertSize(t, 0) })
	record := makeCheckedRecordBatch(allocator, 1, 2, 3, 4, 5, 6, 7, 8)
	batches := splitRecordBatch(record, 1)
	if len(batches) < 2 {
		t.Fatalf("splitRecordBatch() returned %d batch, want multiple", len(batches))
	}
	wantErr := errors.New("first append failed")
	calls := 0
	err := consumeSplitBatches(batches, func(splitBatch) error {
		calls++
		return wantErr
	})
	record.Release()
	if !errors.Is(err, wantErr) {
		t.Fatalf("consumeSplitBatches() error = %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("consumer calls = %d, want 1", calls)
	}
}

func TestAppendArrowStreamWorkerReleasesAllSplitSlicesOnSendFailure(t *testing.T) {
	allocator := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { allocator.AssertSize(t, 0) })
	record := makeLargeCheckedRecordBatch(allocator, 1_300_000)
	data, err := serializeArrowRecordBatch(record)
	if err != nil {
		record.Release()
		t.Fatal(err)
	}
	if len(data) <= maxAppendRequestBytes {
		record.Release()
		t.Fatalf("serialized record size = %d, want more than %d", len(data), maxAppendRequestBytes)
	}
	recvDone := make(chan struct{})
	close(recvDone)
	wantErr := status.Error(codes.InvalidArgument, "append rejected")
	client := &StorageWriteArrowClient{
		streamOpener: func(context.Context, int) (*streamState, error) {
			return &streamState{
				stream:   &failingAppendRowsClient{err: wantErr},
				recvErr:  make(chan error, 1),
				recvDone: recvDone,
			}, nil
		},
	}
	records := make(chan arrow.RecordBatch, 1)
	records <- record
	close(records)

	err = client.appendArrowStreamWorker(t.Context(), "projects/p/datasets/d/tables/t/streams/_default", records, 0)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("appendArrowStreamWorker() error = %v, want InvalidArgument", err)
	}
}

func TestAppendArrowStreamFromSourceReleasesBatchAttachedToError(t *testing.T) {
	allocator := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { allocator.AssertSize(t, 0) })
	client := &StorageWriteArrowClient{
		appendWorker: func(_ context.Context, _ string, records <-chan arrow.RecordBatch, _ int) error {
			for batch := range records {
				batch.Release()
			}
			return nil
		},
	}
	wantErr := errors.New("source failed")
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: makeCheckedRecordBatch(allocator, 1), Err: wantErr}
	close(records)

	err := client.AppendArrowStreamFromSource(t.Context(), "projects/p/datasets/d/tables/t/streams/_default", records, 1)
	if !errors.Is(err, wantErr) {
		t.Fatalf("AppendArrowStreamFromSource() error = %v, want %v", err, wantErr)
	}
}

func TestAppendArrowStream_ReturnsWorkerErrorWithoutHanging(t *testing.T) {
	client := &StorageWriteArrowClient{
		appendWorker: failingAppendWorker(errors.New("worker failed")),
	}

	records := make(chan arrow.RecordBatch, 16)
	for i := 0; i < cap(records); i++ {
		records <- makeTestRecordBatch(t, int64(i))
	}
	close(records)

	done := make(chan error, 1)
	go func() {
		done <- client.AppendArrowStream(context.Background(), "projects/p/datasets/d/tables/t/streams/_default", records, 2)
	}()

	select {
	case err := <-done:
		if err == nil || err.Error() != "worker failed" {
			t.Fatalf("AppendArrowStream error = %v, want worker failed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AppendArrowStream deadlocked after worker failure")
	}

	for batch := range records {
		batch.Release()
	}
}

func TestAppendArrowStreamFromSource_ReturnsWorkerErrorWithoutHanging(t *testing.T) {
	client := &StorageWriteArrowClient{
		appendWorker: failingAppendWorker(errors.New("worker failed")),
	}

	records := make(chan source.RecordBatchResult, 16)
	for i := 0; i < cap(records); i++ {
		records <- source.RecordBatchResult{Batch: makeTestRecordBatch(t, int64(i))}
	}
	close(records)

	done := make(chan error, 1)
	go func() {
		done <- client.AppendArrowStreamFromSource(context.Background(), "projects/p/datasets/d/tables/t/streams/_default", records, 2)
	}()

	select {
	case err := <-done:
		if err == nil || err.Error() != "worker failed" {
			t.Fatalf("AppendArrowStreamFromSource error = %v, want worker failed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AppendArrowStreamFromSource deadlocked after worker failure")
	}

	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
}

func TestIsRetryableSendError_EOF(t *testing.T) {
	if !isRetryableSendError(io.EOF) {
		t.Fatal("io.EOF should be retryable")
	}
	if !isRetryableSendError(io.ErrUnexpectedEOF) {
		t.Fatal("io.ErrUnexpectedEOF should be retryable")
	}
}

func failingAppendWorker(err error) func(context.Context, string, <-chan arrow.RecordBatch, int) error {
	return func(ctx context.Context, _ string, records <-chan arrow.RecordBatch, workerID int) error {
		if workerID == 0 {
			batch, ok := <-records
			if !ok {
				return nil
			}
			if batch != nil {
				batch.Release()
			}
			return err
		}

		for {
			select {
			case batch, ok := <-records:
				if !ok {
					return nil
				}
				if batch != nil {
					batch.Release()
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func makeTestRecordBatch(t *testing.T, values ...int64) arrow.RecordBatch {
	t.Helper()

	pool := memory.NewGoAllocator()
	builder := array.NewInt64Builder(pool)
	defer builder.Release()

	builder.AppendValues(values, nil)
	arr := builder.NewArray()
	defer arr.Release()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	return array.NewRecordBatch(schema, []arrow.Array{arr}, int64(len(values)))
}

func makeCheckedRecordBatch(allocator memory.Allocator, values ...int64) arrow.RecordBatch {
	builder := array.NewInt64Builder(allocator)
	builder.AppendValues(values, nil)
	arr := builder.NewArray()
	builder.Release()
	record := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil),
		[]arrow.Array{arr},
		int64(len(values)),
	)
	arr.Release()
	return record
}

func makeLargeCheckedRecordBatch(allocator memory.Allocator, rows int) arrow.RecordBatch {
	builder := array.NewInt64Builder(allocator)
	builder.AppendValues(make([]int64, rows), nil)
	arr := builder.NewArray()
	builder.Release()
	record := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil),
		[]arrow.Array{arr},
		int64(rows),
	)
	arr.Release()
	return record
}
