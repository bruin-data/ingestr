package postgres

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgtype"
)

type releaseCountingRecord struct {
	arrow.RecordBatch
	releases atomic.Int32
}

func (r *releaseCountingRecord) Release() {
	r.releases.Add(1)
	r.RecordBatch.Release()
}

func TestPostgresRecordCopyStreamMatchesSingleRecordEncoding(t *testing.T) {
	first := int32Record(t, 1, 2)
	second := int32Record(t, 3, 4)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: second}
	close(records)

	stream, err := newPostgresRecordCopyStream(t.Context(), records, first, int32TableSchema(), pgtype.NewMap(), []uint32{pgtype.Int4OID})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()

	combined := int32Record(t, 1, 2, 3, 4)
	defer combined.Release()
	reader, ok := newArrowCopyReader(combined, int32TableSchema(), pgtype.NewMap(), []uint32{pgtype.Int4OID})
	if !ok {
		t.Fatal("combined record did not support direct PostgreSQL COPY")
	}
	expected, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(actual, expected) {
		t.Fatalf("streamed COPY bytes differ from single-record encoding\nactual:   %x\nexpected: %x", actual, expected)
	}
	if stream.Batches() != 2 {
		t.Fatalf("Batches() = %d, want 2", stream.Batches())
	}
}

func TestPostgresRecordCopyStreamReleasesConsumedRecords(t *testing.T) {
	first := &releaseCountingRecord{RecordBatch: int32Record(t, 1)}
	second := &releaseCountingRecord{RecordBatch: int32Record(t, 2)}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: second}
	close(records)

	stream, err := newPostgresRecordCopyStream(t.Context(), records, first, int32TableSchema(), pgtype.NewMap(), []uint32{pgtype.Int4OID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, stream); err != nil {
		t.Fatal(err)
	}
	stream.Close()

	if got := first.releases.Load(); got != 1 {
		t.Fatalf("first record releases = %d, want 1", got)
	}
	if got := second.releases.Load(); got != 1 {
		t.Fatalf("second record releases = %d, want 1", got)
	}
}

func TestPostgresRecordCopyStreamDefersSourceErrorUntilCopyBoundary(t *testing.T) {
	first := int32Record(t, 1)
	sourceErr := errors.New("source failed")
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Err: sourceErr}
	close(records)

	stream, err := newPostgresRecordCopyStream(t.Context(), records, first, int32TableSchema(), pgtype.NewMap(), []uint32{pgtype.Int4OID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, stream); err != nil {
		t.Fatal(err)
	}
	stream.Close()

	pending := stream.Pending()
	if pending == nil || !errors.Is(pending.Err, sourceErr) {
		t.Fatalf("Pending() = %#v, want source error", pending)
	}
}

func int32Record(t *testing.T, values ...int32) arrow.RecordBatch {
	t.Helper()
	builder := array.NewInt32Builder(memory.DefaultAllocator)
	builder.AppendValues(values, nil)
	column := builder.NewArray()
	builder.Release()
	record := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.PrimitiveTypes.Int32}}, nil),
		[]arrow.Array{column},
		int64(len(values)),
	)
	column.Release()
	return record
}

func int32TableSchema() *schema.TableSchema {
	return &schema.TableSchema{Columns: []schema.Column{{Name: "value", DataType: schema.TypeInt32}}}
}
