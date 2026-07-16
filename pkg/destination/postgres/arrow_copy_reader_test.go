package postgres

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestArrowCopyReaderMatchesPGXBinaryEncoding(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	arrays := []arrow.Array{
		buildBoolArray(mem),
		buildInt16Array(mem),
		buildInt32Array(mem),
		buildInt64Array(mem),
		buildFloat32Array(mem),
		buildFloat64Array(mem),
		buildStringArray(mem),
		buildBinaryArray(mem),
		buildDecimal128Array(mem),
		buildDate32Array(mem),
		buildTime64Array(mem),
		buildTimestampArray(mem),
		buildJSONExtensionArray(mem),
	}
	for _, arr := range arrays {
		defer arr.Release()
	}

	fields := make([]arrow.Field, len(arrays))
	for i, arr := range arrays {
		fields[i] = arrow.Field{Name: "column_" + string(rune('a'+i)), Type: arr.DataType(), Nullable: true}
	}
	record := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, 2)
	defer record.Release()

	oids := []uint32{
		pgtype.BoolOID,
		pgtype.Int2OID,
		pgtype.Int4OID,
		pgtype.Int8OID,
		pgtype.Float4OID,
		pgtype.Float8OID,
		pgtype.TextOID,
		pgtype.ByteaOID,
		pgtype.NumericOID,
		pgtype.DateOID,
		pgtype.TimeOID,
		pgtype.TimestampOID,
		pgtype.JSONBOID,
	}
	typeMap := pgtype.NewMap()
	reader, ok := newArrowCopyReader(record, nil, typeMap, oids)
	if !ok {
		t.Fatal("newArrowCopyReader rejected built-in PostgreSQL types")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(data, postgresBinaryCopyHeader) {
		t.Fatal("binary COPY header is missing")
	}
	offset := len(postgresBinaryCopyHeader)
	getters := postgresValueGetters(record, nil)
	for row := 0; row < int(record.NumRows()); row++ {
		columnCount := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2
		if columnCount != len(getters) {
			t.Fatalf("row %d column count = %d, want %d", row, columnCount, len(getters))
		}

		for column, get := range getters {
			length := int32(binary.BigEndian.Uint32(data[offset:]))
			offset += 4
			value := get(row)
			if value == nil {
				if length != -1 {
					t.Fatalf("row %d column %d null length = %d", row, column, length)
				}
				continue
			}

			want, err := typeMap.Encode(oids[column], pgx.BinaryFormatCode, value, nil)
			if err != nil {
				t.Fatalf("encode expected row %d column %d: %v", row, column, err)
			}
			if length != int32(len(want)) {
				t.Fatalf("row %d column %d length = %d, want %d", row, column, length, len(want))
			}
			if !bytes.Equal(data[offset:offset+int(length)], want) {
				t.Fatalf("row %d column %d binary value differs from pgx", row, column)
			}
			offset += int(length)
		}
	}

	if trailer := int16(binary.BigEndian.Uint16(data[offset:])); trailer != -1 {
		t.Fatalf("binary COPY trailer = %d, want -1", trailer)
	}
	offset += 2
	if offset != len(data) {
		t.Fatalf("binary COPY stream has %d trailing bytes", len(data)-offset)
	}
}

func TestDirectArrowCopyColumnsMatchPGXBinaryEncoding(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	tests := []struct {
		name  string
		array arrow.Array
		oid   uint32
	}{
		{name: "numeric scale 4", array: buildDirectDecimalArray(t, mem, 4, []string{"0.0000", "0.0001", "-0.0001", "123456789012345678901234567890.1234", "-999999999999999999999999999999.9999"}), oid: pgtype.NumericOID},
		{name: "numeric scale 8", array: buildDirectDecimalArray(t, mem, 8, []string{"0.00000000", "0.00000001", "-0.00010000", "123456789012345678901234567890.12345678"}), oid: pgtype.NumericOID},
		{name: "date32", array: buildDirectDate32Array(mem), oid: pgtype.DateOID},
		{name: "date64", array: buildDirectDate64Array(mem), oid: pgtype.DateOID},
		{name: "time microseconds", array: buildDirectTime64Array(mem, arrow.Microsecond), oid: pgtype.TimeOID},
		{name: "time nanoseconds", array: buildDirectTime64Array(mem, arrow.Nanosecond), oid: pgtype.TimeOID},
		{name: "timestamp", array: buildDirectTimestampArray(mem), oid: pgtype.TimestampOID},
		{name: "timestamp with time zone", array: buildDirectTimestampArray(mem), oid: pgtype.TimestamptzOID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer test.array.Release()
			column, ok := directArrowCopyColumn(test.array, test.oid)
			if !ok {
				t.Fatal("directArrowCopyColumn rejected supported type")
			}

			get := postgresValueGetter(test.array)
			typeMap := pgtype.NewMap()
			for row := 0; row < test.array.Len(); row++ {
				got, isNull, err := column(row, nil)
				if err != nil {
					t.Fatalf("encode row %d: %v", row, err)
				}
				if test.array.IsNull(row) {
					if !isNull {
						t.Fatalf("row %d was not reported as null", row)
					}
					continue
				}
				if isNull {
					t.Fatalf("row %d was unexpectedly reported as null", row)
				}
				want, err := typeMap.Encode(test.oid, pgx.BinaryFormatCode, get(row), nil)
				if err != nil {
					t.Fatalf("encode expected row %d: %v", row, err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("row %d binary value differs from pgx: got %x, want %x", row, got, want)
				}
			}
		})
	}
}

func TestDirectArrowCopyColumnFallsBackForNonAlignedDecimalScale(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	values := buildDirectDecimalArray(t, mem, 2, []string{"123.45"})
	defer values.Release()
	if _, ok := directArrowCopyColumn(values, pgtype.NumericOID); ok {
		t.Fatal("directArrowCopyColumn accepted a decimal scale requiring normalization")
	}
}

func TestArrowCopyReaderHandlesRowsLargerThanReadBuffer(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	builder := array.NewStringBuilder(mem)
	builder.Append(string(bytes.Repeat([]byte("x"), 100_000)))
	values := builder.NewStringArray()
	builder.Release()
	defer values.Release()
	record := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.BinaryTypes.String}}, nil),
		[]arrow.Array{values},
		1,
	)
	defer record.Release()

	smallBufferReader, ok := newArrowCopyReader(record, nil, pgtype.NewMap(), []uint32{pgtype.TextOID})
	if !ok {
		t.Fatal("newArrowCopyReader rejected string record")
	}
	want := readAllWithBuffer(t, smallBufferReader, 4*1024)

	directBufferReader, ok := newArrowCopyReader(record, nil, pgtype.NewMap(), []uint32{pgtype.TextOID})
	if !ok {
		t.Fatal("newArrowCopyReader rejected string record")
	}
	got := readAllWithBuffer(t, directBufferReader, 65_531)
	if !bytes.Equal(got, want) {
		t.Fatal("direct-buffer COPY stream differs for an oversized row")
	}
}

func readAllWithBuffer(t *testing.T, reader io.Reader, size int) []byte {
	t.Helper()
	buffer := make([]byte, size)
	var result []byte
	for {
		n, err := reader.Read(buffer)
		result = append(result, buffer[:n]...)
		if err == io.EOF {
			return result
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func buildDirectDecimalArray(t *testing.T, mem memory.Allocator, scale int32, values []string) arrow.Array {
	t.Helper()
	dt := &arrow.Decimal128Type{Precision: 38, Scale: scale}
	builder := array.NewDecimal128Builder(mem, dt)
	defer builder.Release()
	for _, value := range values {
		number, err := decimal128.FromString(value, dt.Precision, dt.Scale)
		if err != nil {
			t.Fatalf("parse decimal %q: %v", value, err)
		}
		builder.Append(number)
	}
	builder.AppendNull()
	return builder.NewArray()
}

func buildDirectDate32Array(mem memory.Allocator) arrow.Array {
	builder := array.NewDate32Builder(mem)
	defer builder.Release()
	builder.AppendValues([]arrow.Date32{-20_000, -1, 0, 10_957, 20_000}, []bool{true, true, true, true, true})
	builder.AppendNull()
	return builder.NewArray()
}

func buildDirectDate64Array(mem memory.Allocator) arrow.Array {
	builder := array.NewDate64Builder(mem)
	defer builder.Release()
	builder.AppendValues([]arrow.Date64{-millisecondsPerDay - 1, -1, 0, millisecondsPerDay, 20_000 * millisecondsPerDay}, []bool{true, true, true, true, true})
	builder.AppendNull()
	return builder.NewArray()
}

func buildDirectTime64Array(mem memory.Allocator, unit arrow.TimeUnit) arrow.Array {
	builder := array.NewTime64Builder(mem, &arrow.Time64Type{Unit: unit})
	defer builder.Release()
	multiplier := int64(1)
	if unit == arrow.Nanosecond {
		multiplier = 1_000
	}
	builder.AppendValues([]arrow.Time64{0, arrow.Time64(1*multiplier + multiplier/2), arrow.Time64(43_210_987_654 * multiplier)}, []bool{true, true, true})
	builder.AppendNull()
	return builder.NewArray()
}

func buildDirectTimestampArray(mem memory.Allocator) arrow.Array {
	dt := arrow.FixedWidthTypes.Timestamp_us.(*arrow.TimestampType)
	builder := array.NewTimestampBuilder(mem, dt)
	defer builder.Release()
	for _, value := range []time.Time{
		time.Date(1900, 1, 1, 0, 0, 0, 1_000, time.UTC),
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2100, 12, 31, 23, 59, 59, 999_999_000, time.UTC),
	} {
		timestamp, err := arrow.TimestampFromTime(value, arrow.Microsecond)
		if err != nil {
			panic(err)
		}
		builder.Append(timestamp)
	}
	builder.AppendNull()
	return builder.NewArray()
}
