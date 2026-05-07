package bigquery

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
)

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
