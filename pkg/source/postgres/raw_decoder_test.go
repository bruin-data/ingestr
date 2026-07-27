package postgres

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type rawTestRows struct {
	fields       []pgconn.FieldDescription
	rows         [][][]byte
	index        int
	valuesCalled bool
}

func (r *rawTestRows) Close() {}

func (r *rawTestRows) Err() error { return nil }

func (r *rawTestRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

func (r *rawTestRows) FieldDescriptions() []pgconn.FieldDescription { return r.fields }

func (r *rawTestRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}

func (r *rawTestRows) Scan(...any) error { return errors.New("not implemented") }

func (r *rawTestRows) Values() ([]any, error) {
	r.valuesCalled = true
	return nil, errors.New("Values must not be called by the raw decoder")
}

func (r *rawTestRows) RawValues() [][]byte { return r.rows[r.index-1] }

func (r *rawTestRows) Conn() *pgx.Conn { return nil }

func TestRowsToArrowRecordBatchUsesRawPostgresValues(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 12, 34, 56, 123456000, time.UTC)
	postgresMicros := createdAt.UnixMicro() - postgresTimestampEpochMicroseconds
	payload := []byte(`{"key": "value", "num": 123}`)
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "name", DataType: schema.TypeString},
		{Name: "amount", DataType: schema.TypeDecimal, Precision: 18, Scale: 4},
		{Name: "created_at", DataType: schema.TypeTimestamp},
		{Name: "payload", DataType: schema.TypeJSON},
		{Name: "nullable", DataType: schema.TypeInt64, Nullable: true},
	}
	rows := &rawTestRows{
		fields: []pgconn.FieldDescription{
			{Name: "id", DataTypeOID: pgtype.Int4OID, Format: pgx.BinaryFormatCode},
			{Name: "name", DataTypeOID: pgtype.TextOID, Format: pgx.TextFormatCode},
			{Name: "amount", DataTypeOID: pgtype.NumericOID, Format: pgx.BinaryFormatCode},
			{Name: "created_at", DataTypeOID: pgtype.TimestampOID, Format: pgx.BinaryFormatCode},
			{Name: "payload", DataTypeOID: pgtype.JSONBOID, Format: pgx.TextFormatCode},
			{Name: "nullable", DataTypeOID: pgtype.Int8OID, Format: pgx.BinaryFormatCode},
		},
		rows: [][][]byte{
			{
				binaryInt32(42),
				[]byte("alice"),
				binaryNumeric(0, 0, 4, 123, 4500),
				binaryInt64(postgresMicros),
				payload,
				binaryInt64(99),
			},
			{nil, nil, nil, nil, nil, nil},
		},
	}

	record, count, err := rowsToArrowRecordBatch(rows, buildArrowSchema(columns), columns, 25_000)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Release()
	if rows.valuesCalled {
		t.Fatal("rows.Values was called")
	}
	if count != 2 {
		t.Fatalf("row count = %d, want 2", count)
	}
	if got := record.Column(0).(*array.Int32).Value(0); got != 42 {
		t.Fatalf("id = %d, want 42", got)
	}
	if got := record.Column(1).(*array.String).Value(0); got != "alice" {
		t.Fatalf("name = %q, want alice", got)
	}
	if got := record.Column(2).(*array.Decimal128).Value(0).ToString(4); got != "123.4500" {
		t.Fatalf("amount = %s, want 123.4500", got)
	}
	if got := record.Column(3).(*array.Timestamp).Value(0).ToTime(arrow.Microsecond); !got.Equal(createdAt) {
		t.Fatalf("created_at = %s, want %s", got, createdAt)
	}
	jsonColumn := record.Column(4).(array.ExtensionArray).Storage().(*array.String)
	if got := jsonColumn.Value(0); got != string(payload) {
		t.Fatalf("payload = %q, want exact raw value %q", got, payload)
	}
	for column := 0; column < int(record.NumCols()); column++ {
		if !record.Column(column).IsNull(1) {
			t.Fatalf("column %d row 1 should be null", column)
		}
	}
}

func TestRowsToArrowRecordBatchHandlesBinaryPostgresValues(t *testing.T) {
	uuid := []byte{0x0f, 0x36, 0x41, 0x30, 0xda, 0x0b, 0x49, 0x09, 0xb8, 0x24, 0x54, 0x13, 0xd7, 0x95, 0xaa, 0x93}
	payload := []byte(`{"binary":true}`)
	columns := []schema.Column{
		{Name: "time_value", DataType: schema.TypeTime},
		{Name: "binary_value", DataType: schema.TypeBinary},
		{Name: "uuid_value", DataType: schema.TypeUUID},
		{Name: "json_value", DataType: schema.TypeJSON},
	}
	rows := &rawTestRows{
		fields: []pgconn.FieldDescription{
			{Name: "time_value", DataTypeOID: pgtype.TimeOID, Format: pgx.BinaryFormatCode},
			{Name: "binary_value", DataTypeOID: pgtype.ByteaOID, Format: pgx.BinaryFormatCode},
			{Name: "uuid_value", DataTypeOID: pgtype.UUIDOID, Format: pgx.BinaryFormatCode},
			{Name: "json_value", DataTypeOID: pgtype.JSONBOID, Format: pgx.BinaryFormatCode},
		},
		rows: [][][]byte{{
			binaryInt64(45_296_123_456),
			{0x00, 0x01, 0xfe, 0xff},
			uuid,
			append([]byte{1}, payload...),
		}},
	}

	record, count, err := rowsToArrowRecordBatch(rows, buildArrowSchema(columns), columns, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Release()
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}
	if got := record.Column(0).(*array.Time64).Value(0); got != 45_296_123_456 {
		t.Fatalf("time_value = %d, want 45296123456", got)
	}
	if got := record.Column(1).(*array.Binary).Value(0); string(got) != string([]byte{0x00, 0x01, 0xfe, 0xff}) {
		t.Fatalf("binary_value = %v", got)
	}
	if got := record.Column(2).(*array.String).Value(0); got != "0f364130-da0b-4909-b824-5413d795aa93" {
		t.Fatalf("uuid_value = %q", got)
	}
	jsonColumn := record.Column(3).(array.ExtensionArray).Storage().(*array.String)
	if got := jsonColumn.Value(0); got != string(payload) {
		t.Fatalf("json_value = %q, want %q", got, payload)
	}
}

func TestParsePostgresBinaryDecimal128(t *testing.T) {
	tests := []struct {
		name      string
		value     []byte
		precision int32
		scale     int32
		want      string
	}{
		{name: "scale four", value: binaryNumeric(0, 0, 4, 123, 4500), precision: 18, scale: 4, want: "123.4500"},
		{name: "remove base ten thousand padding", value: binaryNumeric(0, 0, 2, 123, 4500), precision: 18, scale: 2, want: "123.45"},
		{name: "negative", value: binaryNumeric(0, postgresNumericNegative, 2, 12, 3400), precision: 18, scale: 2, want: "-12.34"},
		{name: "zero", value: binaryNumeric(0, 0, 4), precision: 18, scale: 4, want: "0.0000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePostgresBinaryDecimal128(tt.value, tt.precision, tt.scale)
			if !ok {
				t.Fatal("parse failed")
			}
			if value := got.ToString(tt.scale); value != tt.want {
				t.Fatalf("value = %s, want %s", value, tt.want)
			}
		})
	}
}

func binaryInt32(value int32) []byte {
	result := make([]byte, 4)
	binary.BigEndian.PutUint32(result, uint32(value))
	return result
}

func binaryInt64(value int64) []byte {
	result := make([]byte, 8)
	binary.BigEndian.PutUint64(result, uint64(value))
	return result
}

func binaryNumeric(weight int16, sign uint16, scale int16, digits ...uint16) []byte {
	result := make([]byte, 8+len(digits)*2)
	binary.BigEndian.PutUint16(result, uint16(len(digits)))
	binary.BigEndian.PutUint16(result[2:], uint16(weight))
	binary.BigEndian.PutUint16(result[4:], sign)
	binary.BigEndian.PutUint16(result[6:], uint16(scale))
	for i, digit := range digits {
		binary.BigEndian.PutUint16(result[8+i*2:], digit)
	}
	return result
}

var _ pgx.Rows = (*rawTestRows)(nil)
