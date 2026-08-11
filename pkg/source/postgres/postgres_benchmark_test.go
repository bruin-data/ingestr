package postgres

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const benchmarkPostgresBatchSize = 25_000

var benchmarkArrowRows int64

type syntheticBenchmarkRows struct {
	fields  []pgconn.FieldDescription
	values  [][]byte
	typeMap *pgtype.Map
	rows    int
	index   int
}

func (r *syntheticBenchmarkRows) Close() {}

func (r *syntheticBenchmarkRows) Err() error { return nil }

func (r *syntheticBenchmarkRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

func (r *syntheticBenchmarkRows) FieldDescriptions() []pgconn.FieldDescription { return r.fields }

func (r *syntheticBenchmarkRows) Next() bool {
	if r.index == r.rows {
		return false
	}
	r.index++
	return true
}

func (r *syntheticBenchmarkRows) Scan(...any) error { return errors.New("not implemented") }

func (r *syntheticBenchmarkRows) Values() ([]any, error) {
	values := make([]any, 0, len(r.values))
	for i, raw := range r.values {
		field := r.fields[i]
		dataType, ok := r.typeMap.TypeForOID(field.DataTypeOID)
		if !ok {
			return nil, fmt.Errorf("OID %d is not registered", field.DataTypeOID)
		}
		value, err := dataType.Codec.DecodeValue(r.typeMap, field.DataTypeOID, field.Format, raw)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (r *syntheticBenchmarkRows) RawValues() [][]byte { return r.values }

func (r *syntheticBenchmarkRows) Conn() *pgx.Conn { return nil }

func BenchmarkPostgresJSONBToArrow(b *testing.B) {
	payload := []byte(`{"key": "val_99", "num": 9999999}`)
	typeMap := pgtype.NewMap()
	codec := typeMap.TypeForOID

	b.Run("legacy_decode_reencode", func(b *testing.B) {
		jsonbType, ok := codec(pgtype.JSONBOID)
		if !ok {
			b.Fatal("JSONB codec not registered")
		}

		b.ReportAllocs()
		b.SetBytes(int64(len(payload) * benchmarkPostgresBatchSize))
		for b.Loop() {
			builder := array.NewBuilder(memory.DefaultAllocator, schema.JSONArrowType)
			for range benchmarkPostgresBatchSize {
				value, err := jsonbType.Codec.DecodeValue(typeMap, pgtype.JSONBOID, pgx.TextFormatCode, payload)
				if err != nil {
					b.Fatal(err)
				}
				arrowconv.AppendValue(builder, value)
			}
			result := builder.NewArray()
			benchmarkArrowRows = int64(result.Len())
			result.Release()
			builder.Release()
		}
	})

	b.Run("optimized_raw_append", func(b *testing.B) {
		field := pgconn.FieldDescription{DataTypeOID: pgtype.JSONBOID, Format: pgx.TextFormatCode}
		column := schema.Column{DataType: schema.TypeJSON}

		b.ReportAllocs()
		b.SetBytes(int64(len(payload) * benchmarkPostgresBatchSize))
		for b.Loop() {
			builder := array.NewBuilder(memory.DefaultAllocator, schema.JSONArrowType)
			builder.Reserve(benchmarkPostgresBatchSize)
			appendRaw := newPostgresRawAppender(builder, field, column, typeMap)
			for range benchmarkPostgresBatchSize {
				if err := appendRaw(payload); err != nil {
					b.Fatal(err)
				}
			}
			result := builder.NewArray()
			benchmarkArrowRows = int64(result.Len())
			result.Release()
			builder.Release()
		}
	})
}

func BenchmarkPostgresRowBatchToArrow(b *testing.B) {
	columns, fields, values := benchmarkPostgresRow()
	arrowSchema := buildArrowSchema(columns)
	rowBytes := 0
	for _, value := range values {
		rowBytes += len(value)
	}

	b.Run("legacy_values_generic_append", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(rowBytes * benchmarkPostgresBatchSize))
		for b.Loop() {
			rows := &syntheticBenchmarkRows{
				fields: fields, values: values, typeMap: pgtype.NewMap(), rows: benchmarkPostgresBatchSize,
			}
			record, count, err := rowsToArrowRecordBatchLegacy(rows, arrowSchema, columns, benchmarkPostgresBatchSize)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkArrowRows = count
			record.Release()
		}
	})

	b.Run("optimized_raw_typed_append", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(rowBytes * benchmarkPostgresBatchSize))
		for b.Loop() {
			rows := &syntheticBenchmarkRows{
				fields: fields, values: values, typeMap: pgtype.NewMap(), rows: benchmarkPostgresBatchSize,
			}
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, benchmarkPostgresBatchSize)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkArrowRows = count
			record.Release()
		}
	})
}

func benchmarkPostgresRow() ([]schema.Column, []pgconn.FieldDescription, [][]byte) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "small_str", DataType: schema.TypeString, MaxLength: 20},
		{Name: "medium_str", DataType: schema.TypeString, MaxLength: 100},
		{Name: "large_str", DataType: schema.TypeString, MaxLength: 500},
		{Name: "tiny_int", DataType: schema.TypeInt16},
		{Name: "regular_int", DataType: schema.TypeInt32},
		{Name: "big_int", DataType: schema.TypeInt64},
		{Name: "float_val", DataType: schema.TypeFloat64},
		{Name: "decimal_val", DataType: schema.TypeDecimal, Precision: 18, Scale: 4},
		{Name: "bool_val", DataType: schema.TypeBoolean},
		{Name: "date_val", DataType: schema.TypeDate},
		{Name: "ts_val", DataType: schema.TypeTimestamp},
		{Name: "ts_tz_val", DataType: schema.TypeTimestampTZ},
		{Name: "json_val", DataType: schema.TypeJSON},
		{Name: "extra_text", DataType: schema.TypeString},
	}
	fields := []pgconn.FieldDescription{
		{Name: "id", DataTypeOID: pgtype.Int4OID, Format: pgx.BinaryFormatCode},
		{Name: "small_str", DataTypeOID: pgtype.VarcharOID, Format: pgx.TextFormatCode},
		{Name: "medium_str", DataTypeOID: pgtype.VarcharOID, Format: pgx.TextFormatCode},
		{Name: "large_str", DataTypeOID: pgtype.VarcharOID, Format: pgx.TextFormatCode},
		{Name: "tiny_int", DataTypeOID: pgtype.Int2OID, Format: pgx.BinaryFormatCode},
		{Name: "regular_int", DataTypeOID: pgtype.Int4OID, Format: pgx.BinaryFormatCode},
		{Name: "big_int", DataTypeOID: pgtype.Int8OID, Format: pgx.BinaryFormatCode},
		{Name: "float_val", DataTypeOID: pgtype.Float8OID, Format: pgx.BinaryFormatCode},
		{Name: "decimal_val", DataTypeOID: pgtype.NumericOID, Format: pgx.BinaryFormatCode},
		{Name: "bool_val", DataTypeOID: pgtype.BoolOID, Format: pgx.BinaryFormatCode},
		{Name: "date_val", DataTypeOID: pgtype.DateOID, Format: pgx.BinaryFormatCode},
		{Name: "ts_val", DataTypeOID: pgtype.TimestampOID, Format: pgx.BinaryFormatCode},
		{Name: "ts_tz_val", DataTypeOID: pgtype.TimestamptzOID, Format: pgx.BinaryFormatCode},
		{Name: "json_val", DataTypeOID: pgtype.JSONBOID, Format: pgx.TextFormatCode},
		{Name: "extra_text", DataTypeOID: pgtype.TextOID, Format: pgx.TextFormatCode},
	}
	values := [][]byte{
		binaryUint32(9_999_999),
		[]byte("name_9999"),
		[]byte("user_9999999@example-499.com"),
		[]byte(strings.Repeat("A", 150)),
		binaryUint16(123),
		binaryUint32(9_999_999),
		binaryUint64(9_999_999_000_000),
		binaryUint64(math.Float64bits(1_429_570.285714)),
		binaryNumeric(0, 0, 4, 999, 9900),
		{1},
		binaryUint32(postgresDateEpochDays),
		binaryUint64(9_999_999_000_000),
		binaryUint64(9_999_999_000_000),
		[]byte(`{"key":"val_99","num":9999999}`),
		[]byte(strings.Repeat("x", 122)),
	}
	return columns, fields, values
}

func rowsToArrowRecordBatchLegacy(rows pgx.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int) (arrow.RecordBatch, int64, error) {
	builders := make([]array.Builder, len(columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(memory.NewGoAllocator(), field.Type)
	}

	var rowCount int64
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, 0, err
		}
		for i, value := range values {
			arrowconv.AppendValue(builders[i], convertValue(value, columns[i]))
		}
		rowCount++
		if rowCount >= int64(batchSize) {
			break
		}
	}

	arrays := make([]arrow.Array, len(builders))
	for i, builder := range builders {
		arrays[i] = builder.NewArray()
	}
	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)
	for _, column := range arrays {
		column.Release()
	}
	return record, rowCount, nil
}

func binaryUint16(value uint16) []byte {
	result := make([]byte, 2)
	result[0] = byte(value >> 8)
	result[1] = byte(value)
	return result
}

func binaryUint32(value uint32) []byte {
	result := make([]byte, 4)
	result[0] = byte(value >> 24)
	result[1] = byte(value >> 16)
	result[2] = byte(value >> 8)
	result[3] = byte(value)
	return result
}

func binaryUint64(value uint64) []byte {
	result := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		result[i] = byte(value)
		value >>= 8
	}
	return result
}

var _ pgx.Rows = (*syntheticBenchmarkRows)(nil)

func TestPostgresRawBatchMatchesLegacyForBenchmarkTypes(t *testing.T) {
	columns, fields, values := benchmarkPostgresRow()
	arrowSchema := buildArrowSchema(columns)
	legacyRows := &syntheticBenchmarkRows{
		fields: fields, values: values, typeMap: pgtype.NewMap(), rows: 1,
	}
	rawRows := &syntheticBenchmarkRows{
		fields: fields, values: values, typeMap: pgtype.NewMap(), rows: 1,
	}
	legacy, _, err := rowsToArrowRecordBatchLegacy(legacyRows, arrowSchema, columns, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Release()
	optimized, _, err := rowsToArrowRecordBatch(rawRows, arrowSchema, columns, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer optimized.Release()
	if !array.RecordEqual(legacy, optimized) {
		t.Fatal("optimized PostgreSQL decoder produced a different Arrow record")
	}
}
