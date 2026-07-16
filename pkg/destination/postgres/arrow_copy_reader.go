package postgres

import (
	"encoding/binary"
	"io"
	"math"
	"math/bits"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var postgresBinaryCopyHeader = []byte("PGCOPY\n\xff\r\n\x00\x00\x00\x00\x00\x00\x00\x00\x00")

type arrowCopyReader struct {
	record  arrow.RecordBatch
	columns []arrowCopyColumn
	row     int
	buffer  []byte
	offset  int
	started bool
	done    bool
	err     error
}

type arrowCopyColumn func(int, []byte) ([]byte, bool, error)

func newArrowCopyReader(record arrow.RecordBatch, tableSchema *schema.TableSchema, typeMap *pgtype.Map, oids []uint32) (*arrowCopyReader, bool) {
	if len(oids) != int(record.NumCols()) {
		return nil, false
	}

	columns := make([]arrowCopyColumn, record.NumCols())
	var columnTypes map[string]schema.DataType
	for column := range columns {
		if direct, ok := directArrowCopyColumn(record.Column(column), oids[column]); ok {
			columns[column] = direct
			continue
		}
		if columnTypes == nil {
			columnTypes = postgresColumnTypesByName(tableSchema)
		}
		get := postgresValueGetterForType(record.Column(column), columnTypes[record.ColumnName(column)])

		var plan pgtype.EncodePlan
		for row := 0; row < int(record.NumRows()); row++ {
			value := get(row)
			if value == nil {
				continue
			}

			plan = typeMap.PlanEncode(oids[column], pgx.BinaryFormatCode, value)
			if plan == nil {
				return nil, false
			}
			if _, err := plan.Encode(value, nil); err != nil {
				return nil, false
			}
			break
		}
		columns[column] = func(row int, dst []byte) ([]byte, bool, error) {
			value := get(row)
			if value == nil {
				return dst, true, nil
			}
			encoded, err := plan.Encode(value, dst)
			return encoded, false, err
		}
	}

	return &arrowCopyReader{
		record:  record,
		columns: columns,
	}, true
}

func directArrowCopyColumn(column arrow.Array, oid uint32) (arrowCopyColumn, bool) {
	switch values := column.(type) {
	case *array.Boolean:
		if oid != pgtype.BoolOID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			if values.Value(row) {
				return append(dst, 1)
			}
			return append(dst, 0)
		}), true
	case *array.Int8:
		if oid != pgtype.Int2OID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return binary.BigEndian.AppendUint16(dst, uint16(int16(values.Value(row))))
		}), true
	case *array.Int16:
		if oid != pgtype.Int2OID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return binary.BigEndian.AppendUint16(dst, uint16(values.Value(row)))
		}), true
	case *array.Int32:
		if oid != pgtype.Int4OID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return binary.BigEndian.AppendUint32(dst, uint32(values.Value(row)))
		}), true
	case *array.Int64:
		if oid != pgtype.Int8OID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return binary.BigEndian.AppendUint64(dst, uint64(values.Value(row)))
		}), true
	case *array.Float32:
		if oid != pgtype.Float4OID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return binary.BigEndian.AppendUint32(dst, math.Float32bits(values.Value(row)))
		}), true
	case *array.Float64:
		if oid != pgtype.Float8OID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return binary.BigEndian.AppendUint64(dst, math.Float64bits(values.Value(row)))
		}), true
	case *array.Decimal128:
		decimalType := values.DataType().(*arrow.Decimal128Type)
		if oid != pgtype.NumericOID || decimalType.Scale < 0 {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return appendPostgresNumeric(dst, values.Value(row), decimalType.Scale)
		}), true
	case *array.Date32:
		if oid != pgtype.DateOID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return binary.BigEndian.AppendUint32(dst, uint32(int32(values.Value(row))-postgresDateEpochDays))
		}), true
	case *array.Date64:
		if oid != pgtype.DateOID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			days := int64(values.Value(row)) / millisecondsPerDay
			return binary.BigEndian.AppendUint32(dst, uint32(int32(days)-postgresDateEpochDays))
		}), true
	case *array.Time64:
		if oid != pgtype.TimeOID {
			return nil, false
		}
		timeType := values.DataType().(*arrow.Time64Type)
		if timeType.Unit != arrow.Microsecond && timeType.Unit != arrow.Nanosecond {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			value := int64(values.Value(row))
			if timeType.Unit == arrow.Nanosecond {
				value /= 1_000
			}
			return binary.BigEndian.AppendUint64(dst, uint64(value))
		}), true
	case *array.Timestamp:
		if oid != pgtype.TimestampOID && oid != pgtype.TimestamptzOID {
			return nil, false
		}
		timestampType := values.DataType().(*arrow.TimestampType)
		if timestampType.Unit != arrow.Microsecond {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			value := int64(values.Value(row)) - postgresTimestampEpochMicroseconds
			return binary.BigEndian.AppendUint64(dst, uint64(value))
		}), true
	case *array.String:
		if oid != pgtype.TextOID && oid != pgtype.VarcharOID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return append(dst, values.Value(row)...)
		}), true
	case *array.LargeString:
		if oid != pgtype.TextOID && oid != pgtype.VarcharOID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return append(dst, values.Value(row)...)
		}), true
	case *array.Binary:
		if oid != pgtype.ByteaOID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return append(dst, values.Value(row)...)
		}), true
	case *array.LargeBinary:
		if oid != pgtype.ByteaOID {
			return nil, false
		}
		return fixedArrowCopyColumn(values, func(row int, dst []byte) []byte {
			return append(dst, values.Value(row)...)
		}), true
	case array.ExtensionArray:
		if oid == pgtype.JSONBOID {
			storage, ok := values.Storage().(*array.String)
			if !ok {
				return nil, false
			}
			return fixedArrowCopyColumn(storage, func(row int, dst []byte) []byte {
				dst = append(dst, 1)
				return append(dst, storage.Value(row)...)
			}), true
		}
	}
	return nil, false
}

const (
	postgresDateEpochDays              = 10_957
	postgresTimestampEpochMicroseconds = 946_684_800_000_000
	millisecondsPerDay                 = 86_400_000
	postgresNumericNegative            = 0x4000
)

func appendPostgresNumeric(dst []byte, value decimal128.Num, scale int32) []byte {
	negative := value.Sign() < 0
	if negative {
		value = value.Negate()
	}

	var digits [11]uint16
	hi := uint64(value.HighBits())
	lo := value.LowBits()
	digitCount := 0
	for hi != 0 || lo != 0 {
		quotientHigh, remainder := bits.Div64(0, hi, 10_000)
		quotientLow, remainder := bits.Div64(remainder, lo, 10_000)
		digits[digitCount] = uint16(remainder)
		digitCount++
		hi = quotientHigh
		lo = quotientLow
	}

	if remainder := scale % 4; remainder != 0 {
		multiplier := uint32(10)
		for i := remainder + 1; i < 4; i++ {
			multiplier *= 10
		}
		var carry uint32
		for i := 0; i < digitCount; i++ {
			product := uint32(digits[i])*multiplier + carry
			digits[i] = uint16(product % 10_000)
			carry = product / 10_000
		}
		if carry != 0 {
			digits[digitCount] = uint16(carry)
			digitCount++
		}
	}

	fractionalDigits := int((scale + 3) / 4)
	for digitCount < fractionalDigits {
		digitCount++
	}

	weight := int16(-1)
	if wholeDigits := digitCount - fractionalDigits; wholeDigits > 0 {
		weight = int16(wholeDigits - 1)
	}
	sign := uint16(0)
	if negative {
		sign = postgresNumericNegative
	}

	dst = binary.BigEndian.AppendUint16(dst, uint16(digitCount))
	dst = binary.BigEndian.AppendUint16(dst, uint16(weight))
	dst = binary.BigEndian.AppendUint16(dst, sign)
	dst = binary.BigEndian.AppendUint16(dst, uint16(scale))
	for i := digitCount - 1; i >= 0; i-- {
		dst = binary.BigEndian.AppendUint16(dst, digits[i])
	}
	return dst
}

func fixedArrowCopyColumn(values arrow.Array, encode func(int, []byte) []byte) arrowCopyColumn {
	if values.NullN() == 0 {
		return func(row int, dst []byte) ([]byte, bool, error) {
			return encode(row, dst), false, nil
		}
	}
	return func(row int, dst []byte) ([]byte, bool, error) {
		if values.IsNull(row) {
			return dst, true, nil
		}
		return encode(row, dst), false, nil
	}
}

func (r *arrowCopyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.err != nil {
		return 0, r.err
	}

	if r.offset == len(r.buffer) {
		if r.done {
			return 0, io.EOF
		}
		r.fill(p)
		if r.err != nil {
			return 0, r.err
		}
	}

	n := copy(p, r.buffer[r.offset:])
	r.offset += n
	return n, nil
}

func (r *arrowCopyReader) fill(target []byte) {
	fillLimit := 64 * 1024
	if len(target) >= 32*1024 {
		r.buffer = target[:0:len(target)]
		fillLimit = len(target) - 1024
	} else {
		r.buffer = r.buffer[:0]
	}
	r.offset = 0
	if !r.started {
		r.buffer = append(r.buffer, postgresBinaryCopyHeader...)
		r.started = true
	}

	for r.row < int(r.record.NumRows()) && len(r.buffer) < fillLimit {
		r.appendRow(r.row)
		if r.err != nil {
			return
		}
		r.row++
	}

	if r.row == int(r.record.NumRows()) {
		r.buffer = binary.BigEndian.AppendUint16(r.buffer, uint16(0xffff))
		r.done = true
	}
}

func (r *arrowCopyReader) appendRow(row int) {
	r.buffer = binary.BigEndian.AppendUint16(r.buffer, uint16(len(r.columns)))
	for _, column := range r.columns {
		lengthOffset := len(r.buffer)
		r.buffer = binary.BigEndian.AppendUint32(r.buffer, uint32(0xffffffff))

		encoded, isNull, err := column(row, r.buffer)
		if err != nil {
			r.err = err
			return
		}
		if isNull || encoded == nil {
			continue
		}
		r.buffer = encoded
		binary.BigEndian.PutUint32(r.buffer[lengthOffset:], uint32(len(r.buffer)-lengthOffset-4))
	}
}
