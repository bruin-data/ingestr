package d1

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

func writeTestArray(t *testing.T, typ arrow.DataType, input string) arrow.Array {
	t.Helper()
	values, _, err := array.FromJSON(memory.DefaultAllocator, typ, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func writeTestRecord(t *testing.T, values arrow.Array) arrow.RecordBatch {
	t.Helper()
	record := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "value", Type: values.DataType(), Nullable: true}}, nil), []arrow.Array{values}, int64(values.Len()))
	values.Release()
	return record
}

func writeTestRecords(record arrow.RecordBatch) <-chan source.RecordBatchResult {
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: record}
	close(records)
	return records
}

func TestWritePreservesTypedValues(t *testing.T) {
	for _, tt := range []struct {
		name    string
		typ     arrow.DataType
		sqlType string
		input   string
		want    []any
	}{
		{
			name: "signed integer limits", typ: arrow.PrimitiveTypes.Int64, sqlType: "INTEGER",
			input: `[9223372036854775807,-9223372036854775808,9007199254740993,null]`,
			want:  []any{int64(math.MaxInt64), int64(math.MinInt64), int64(9007199254740993), nil},
		},
		{
			name: "unsigned integers", typ: arrow.PrimitiveTypes.Uint64, sqlType: "INTEGER",
			input: `[9223372036854775807,0,null]`,
			want:  []any{int64(math.MaxInt64), int64(0), nil},
		},
		{
			name: "booleans", typ: arrow.FixedWidthTypes.Boolean, sqlType: "INTEGER",
			input: `[true,false,null]`, want: []any{int64(1), int64(0), nil},
		},
		{
			name: "floating point", typ: arrow.PrimitiveTypes.Float64, sqlType: "REAL",
			input: `[1.2345678901234567,-1.25,null]`, want: []any{1.2345678901234567, -1.25, nil},
		},
		{
			name: "decimal precision", typ: &arrow.Decimal128Type{Precision: 38, Scale: 4}, sqlType: "TEXT",
			input: `["123456789012345678901234567890.1234","0.0000",null]`,
			want:  []any{"123456789012345678901234567890.1234", "0.0000", nil},
		},
		{
			name: "text", typ: arrow.BinaryTypes.String, sqlType: "TEXT",
			input: `["quote'\" and newline\n","",null]`, want: []any{"quote'\" and newline\n", "", nil},
		},
		{
			name: "nested arrays", typ: arrow.ListOf(arrow.ListOf(arrow.PrimitiveTypes.Int64)), sqlType: "TEXT",
			input: `[[[7,null,-2],null,[]],[],null]`, want: []any{`[[7,null,-2],null,[]]`, `[]`, nil},
		},
		{
			name: "string arrays", typ: arrow.ListOf(arrow.BinaryTypes.String), sqlType: "TEXT",
			input: `[["quote'\"","line\nbreak",null,""],[],null]`, want: []any{`["quote'\"","line\nbreak",null,""]`, `[]`, nil},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, db := newTestDestination(t)
			if err := d.Exec(t.Context(), `CREATE TABLE values_table (value `+tt.sqlType+`)`); err != nil {
				t.Fatal(err)
			}
			batch := writeTestRecord(t, writeTestArray(t, tt.typ, tt.input))
			if err := d.Write(t.Context(), writeTestRecords(batch), destination.WriteOptions{Table: "values_table"}); err != nil {
				t.Fatal(err)
			}
			for i, want := range tt.want {
				var got any
				if err := db.QueryRowContext(t.Context(), `SELECT value FROM values_table WHERE rowid = ?`, i+1).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("row %d = %#v (%T), want %#v (%T)", i+1, got, got, want, want)
				}
			}
		})
	}
}

func TestWritePreservesBlobAndTimestamp(t *testing.T) {
	d, db := newTestDestination(t)
	if err := d.Exec(t.Context(), `CREATE TABLE typed (payload BLOB, ts TEXT)`); err != nil {
		t.Fatal(err)
	}
	pool := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer pool.AssertSize(t, 0)
	binaryBuilder := array.NewBinaryBuilder(pool, arrow.BinaryTypes.Binary)
	binaryBuilder.AppendValues([][]byte{{0, 127, 128, 255}, {}, nil}, []bool{true, true, false})
	values := binaryBuilder.NewArray()
	binaryBuilder.Release()
	tsType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "Asia/Kolkata"}
	timestampBuilder := array.NewTimestampBuilder(pool, tsType)
	instant := time.Date(2026, 10, 7, 6, 30, 12, 123456000, time.UTC)
	timestampBuilder.AppendValues([]arrow.Timestamp{arrow.Timestamp(instant.UnixMicro()), 0, 0}, []bool{true, true, false})
	timestamps := timestampBuilder.NewArray()
	timestampBuilder.Release()
	batch := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "payload", Type: values.DataType(), Nullable: true}, {Name: "ts", Type: tsType, Nullable: true}}, nil), []arrow.Array{values, timestamps}, 3)
	values.Release()
	timestamps.Release()
	if err := d.Write(t.Context(), writeTestRecords(batch), destination.WriteOptions{Table: "typed"}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		payload any
		ts      any
	}{
		{[]byte{0, 127, 128, 255}, "2026-10-07T06:30:12.123456Z"},
		{[]byte(nil), "1970-01-01T00:00:00.000000Z"},
		{nil, nil},
	} {
		var payload, ts any
		if err := db.QueryRowContext(t.Context(), `SELECT payload, ts FROM typed WHERE rowid = ?`, i+1).Scan(&payload, &ts); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(payload, want.payload) || ts != want.ts {
			t.Errorf("row %d = (%#v, %#v), want (%#v, %#v)", i+1, payload, ts, want.payload, want.ts)
		}
	}
}

func TestWriteSplitsLargeArrowBatches(t *testing.T) {
	d, db := newTestDestination(t)
	if err := d.Exec(t.Context(), `CREATE TABLE values_table (value INTEGER)`); err != nil {
		t.Fatal(err)
	}
	pool := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer pool.AssertSize(t, 0)
	builder := array.NewInt64Builder(pool)
	const rows = 5101
	for i := range rows {
		builder.Append(int64(i))
	}
	values := builder.NewArray()
	builder.Release()
	batch := writeTestRecord(t, values)
	if err := d.WriteParallel(t.Context(), writeTestRecords(batch), destination.WriteOptions{Table: "values_table", Parallelism: 8}); err != nil {
		t.Fatal(err)
	}
	var count, sum int64
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*), SUM(value) FROM values_table`).Scan(&count, &sum); err != nil {
		t.Fatal(err)
	}
	if count != rows || sum != (rows*(rows-1))/2 {
		t.Fatalf("wrote count=%d sum=%d, want %d and %d", count, sum, rows, (rows*(rows-1))/2)
	}
}

func TestWriteRejectsUnrepresentableValues(t *testing.T) {
	for _, tt := range []struct {
		name  string
		array func() arrow.Array
	}{
		{"unsigned overflow", func() arrow.Array { return writeTestArray(t, arrow.PrimitiveTypes.Uint64, `[18446744073709551615]`) }},
		{"NaN", func() arrow.Array {
			builder := array.NewFloat64Builder(memory.DefaultAllocator)
			defer builder.Release()
			builder.Append(math.NaN())
			return builder.NewArray()
		}},
		{"infinity", func() arrow.Array {
			builder := array.NewFloat64Builder(memory.DefaultAllocator)
			defer builder.Release()
			builder.Append(math.Inf(1))
			return builder.NewArray()
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, db := newTestDestination(t)
			if err := d.Exec(t.Context(), `CREATE TABLE values_table (value REAL)`); err != nil {
				t.Fatal(err)
			}
			batch := writeTestRecord(t, tt.array())
			if err := d.Write(t.Context(), writeTestRecords(batch), destination.WriteOptions{Table: "values_table"}); err == nil {
				t.Fatal("write accepted an unrepresentable value")
			}
			var count int
			if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM values_table`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("wrote %d rows for an invalid value", count)
			}
		})
	}
}

type releaseCountingBatch struct {
	arrow.RecordBatch
	releases int
}

func (b *releaseCountingBatch) Release() {
	b.releases++
	b.RecordBatch.Release()
}

func TestWriteReleasesBatchOnEveryConsumedPath(t *testing.T) {
	sourceErr := errors.New("source failed")
	for _, tt := range []struct {
		name      string
		prepare   bool
		sourceErr error
	}{
		{"success", true, nil},
		{"database error", false, nil},
		{"source error", true, sourceErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := newTestDestination(t)
			if tt.prepare {
				if err := d.Exec(t.Context(), `CREATE TABLE values_table (value INTEGER)`); err != nil {
					t.Fatal(err)
				}
			}
			batch := &releaseCountingBatch{RecordBatch: writeTestRecord(t, writeTestArray(t, arrow.PrimitiveTypes.Int64, `[1]`))}
			records := make(chan source.RecordBatchResult, 1)
			records <- source.RecordBatchResult{Batch: batch, Err: tt.sourceErr}
			close(records)
			err := d.WriteParallel(t.Context(), records, destination.WriteOptions{Table: "values_table"})
			if tt.name == "success" && err != nil {
				t.Fatal(err)
			}
			if tt.name != "success" && err == nil {
				t.Fatal("expected write error")
			}
			if tt.sourceErr != nil && !errors.Is(err, sourceErr) {
				t.Fatalf("source error not preserved: %v", err)
			}
			if batch.releases != 1 {
				t.Fatalf("batch released %d times, want 1", batch.releases)
			}
		})
	}
}

func TestWriteCancelsWhileWaitingForSource(t *testing.T) {
	d, _ := newTestDestination(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := d.Write(ctx, make(chan source.RecordBatchResult), destination.WriteOptions{Table: "unused"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("write error = %v, want context cancellation", err)
	}
}
