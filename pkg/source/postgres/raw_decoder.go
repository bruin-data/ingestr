package postgres

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	postgresDateEpochDays              = 10_957
	postgresTimestampEpochMicroseconds = 946_684_800_000_000
	postgresNumericNegative            = 0x4000
)

type postgresRawAppender func([]byte) error

func newPostgresRawAppender(builder array.Builder, field pgconn.FieldDescription, column schema.Column, typeMap *pgtype.Map) postgresRawAppender {
	fallback := func(src []byte) error {
		value, err := decodePostgresValue(typeMap, field, src)
		if err != nil {
			return err
		}
		arrowconv.AppendValue(builder, convertValue(value, column))
		return nil
	}

	switch b := builder.(type) {
	case *array.BooleanBuilder:
		if field.DataTypeOID == pgtype.BoolOID {
			return func(src []byte) error {
				if field.Format == pgx.BinaryFormatCode && len(src) == 1 {
					b.Append(src[0] != 0)
					return nil
				}
				if field.Format == pgx.TextFormatCode && len(src) == 1 && (src[0] == 't' || src[0] == 'f') {
					b.Append(src[0] == 't')
					return nil
				}
				return fallback(src)
			}
		}
	case *array.Int16Builder:
		if field.DataTypeOID == pgtype.Int2OID {
			return func(src []byte) error {
				if field.Format == pgx.BinaryFormatCode && len(src) == 2 {
					b.Append(int16(binary.BigEndian.Uint16(src)))
					return nil
				}
				return appendPostgresTextInt(src, field.Format, 16, func(value int64) { b.Append(int16(value)) }, fallback)
			}
		}
	case *array.Int32Builder:
		if field.DataTypeOID == pgtype.Int4OID {
			return func(src []byte) error {
				if field.Format == pgx.BinaryFormatCode && len(src) == 4 {
					b.Append(int32(binary.BigEndian.Uint32(src)))
					return nil
				}
				return appendPostgresTextInt(src, field.Format, 32, func(value int64) { b.Append(int32(value)) }, fallback)
			}
		}
	case *array.Int64Builder:
		if field.DataTypeOID == pgtype.Int8OID {
			return func(src []byte) error {
				if field.Format == pgx.BinaryFormatCode && len(src) == 8 {
					b.Append(int64(binary.BigEndian.Uint64(src)))
					return nil
				}
				return appendPostgresTextInt(src, field.Format, 64, b.Append, fallback)
			}
		}
	case *array.Float32Builder:
		if field.DataTypeOID == pgtype.Float4OID {
			return func(src []byte) error {
				if field.Format == pgx.BinaryFormatCode && len(src) == 4 {
					b.Append(math.Float32frombits(binary.BigEndian.Uint32(src)))
					return nil
				}
				if field.Format == pgx.TextFormatCode {
					value, err := strconv.ParseFloat(string(src), 32)
					if err == nil {
						b.Append(float32(value))
						return nil
					}
				}
				return fallback(src)
			}
		}
	case *array.Float64Builder:
		if field.DataTypeOID == pgtype.Float8OID {
			return func(src []byte) error {
				if field.Format == pgx.BinaryFormatCode && len(src) == 8 {
					b.Append(math.Float64frombits(binary.BigEndian.Uint64(src)))
					return nil
				}
				if field.Format == pgx.TextFormatCode {
					value, err := strconv.ParseFloat(string(src), 64)
					if err == nil {
						b.Append(value)
						return nil
					}
				}
				return fallback(src)
			}
		}
	case *array.StringBuilder:
		if isPostgresTextOID(field.DataTypeOID) && (field.Format == pgx.TextFormatCode || field.Format == pgx.BinaryFormatCode) {
			return func(src []byte) error {
				b.BinaryBuilder.Append(src)
				return nil
			}
		}
	case *array.BinaryBuilder:
		if field.DataTypeOID == pgtype.ByteaOID && field.Format == pgx.BinaryFormatCode {
			return func(src []byte) error {
				b.Append(src)
				return nil
			}
		}
	case *array.Date32Builder:
		if field.DataTypeOID == pgtype.DateOID {
			return func(src []byte) error {
				if field.Format == pgx.BinaryFormatCode && len(src) == 4 {
					days := int32(binary.BigEndian.Uint32(src))
					if days != math.MinInt32 && days <= math.MaxInt32-postgresDateEpochDays {
						b.Append(arrow.Date32(days + postgresDateEpochDays))
						return nil
					}
				}
				return fallback(src)
			}
		}
	case *array.Time64Builder:
		if field.DataTypeOID == pgtype.TimeOID {
			return func(src []byte) error {
				if field.Format == pgx.BinaryFormatCode && len(src) == 8 {
					b.Append(arrow.Time64(int64(binary.BigEndian.Uint64(src))))
					return nil
				}
				return fallback(src)
			}
		}
	case *array.TimestampBuilder:
		if field.DataTypeOID == pgtype.TimestampOID || field.DataTypeOID == pgtype.TimestamptzOID {
			return func(src []byte) error {
				if field.Format == pgx.BinaryFormatCode && len(src) == 8 {
					microseconds := int64(binary.BigEndian.Uint64(src))
					if microseconds <= math.MaxInt64-postgresTimestampEpochMicroseconds && microseconds != math.MinInt64 {
						b.Append(arrow.Timestamp(microseconds + postgresTimestampEpochMicroseconds))
						return nil
					}
				}
				return fallback(src)
			}
		}
	case *array.Decimal128Builder:
		if field.DataTypeOID == pgtype.NumericOID && column.Precision > 0 && column.Precision <= 18 {
			return func(src []byte) error {
				var value decimal128.Num
				var ok bool
				switch field.Format {
				case pgx.BinaryFormatCode:
					value, ok = parsePostgresBinaryDecimal128(src, int32(column.Precision), int32(column.Scale))
				case pgx.TextFormatCode:
					value, ok = arrowconv.ParseDecimal128BytesFast(src, int32(column.Precision), int32(column.Scale))
				}
				if ok {
					b.Append(value)
					return nil
				}
				return fallback(src)
			}
		}
	case *array.ExtensionBuilder:
		if arrowconv.IsJSONType(b.Type()) && (field.DataTypeOID == pgtype.JSONOID || field.DataTypeOID == pgtype.JSONBOID) {
			storage, ok := b.StorageBuilder().(*array.StringBuilder)
			if ok {
				return func(src []byte) error {
					if field.DataTypeOID == pgtype.JSONBOID && field.Format == pgx.BinaryFormatCode {
						if len(src) == 0 || src[0] != 1 {
							return fallback(src)
						}
						src = src[1:]
					}
					storage.BinaryBuilder.Append(src)
					return nil
				}
			}
		}
	}

	return fallback
}

func appendPostgresTextInt(src []byte, format int16, bitSize int, appendValue func(int64), fallback postgresRawAppender) error {
	if format == pgx.TextFormatCode {
		value, err := strconv.ParseInt(string(src), 10, bitSize)
		if err == nil {
			appendValue(value)
			return nil
		}
	}
	return fallback(src)
}

func decodePostgresValue(typeMap *pgtype.Map, field pgconn.FieldDescription, src []byte) (any, error) {
	if dataType, ok := typeMap.TypeForOID(field.DataTypeOID); ok {
		return dataType.Codec.DecodeValue(typeMap, field.DataTypeOID, field.Format, src)
	}
	if field.Format == pgx.TextFormatCode {
		return string(src), nil
	}
	value := make([]byte, len(src))
	copy(value, src)
	return value, nil
}

func isPostgresTextOID(oid uint32) bool {
	switch oid {
	case pgtype.TextOID, pgtype.VarcharOID, pgtype.BPCharOID, pgtype.NameOID:
		return true
	default:
		return false
	}
}

func parsePostgresBinaryDecimal128(src []byte, precision, scale int32) (decimal128.Num, bool) {
	if len(src) < 8 || precision <= 0 || precision > 18 {
		return decimal128.Num{}, false
	}
	ndigits := int(binary.BigEndian.Uint16(src))
	weight := int(int16(binary.BigEndian.Uint16(src[2:])))
	sign := binary.BigEndian.Uint16(src[4:])
	if (sign != 0 && sign != postgresNumericNegative) || len(src) != 8+ndigits*2 {
		return decimal128.Num{}, false
	}

	var unscaled uint64
	for i := range ndigits {
		digit := uint64(binary.BigEndian.Uint16(src[8+i*2:]))
		if digit >= 10_000 || unscaled > (math.MaxUint64-digit)/10_000 {
			return decimal128.Num{}, false
		}
		unscaled = unscaled*10_000 + digit
	}

	exponent := int(scale) + 4*(weight-ndigits+1)
	if exponent > 0 {
		multiplier, ok := uint64PowerOfTen(exponent)
		if !ok || unscaled > math.MaxUint64/multiplier {
			return decimal128.Num{}, false
		}
		unscaled *= multiplier
	} else if exponent < 0 {
		divisor, ok := uint64PowerOfTen(-exponent)
		if !ok || unscaled%divisor != 0 {
			return decimal128.Num{}, false
		}
		unscaled /= divisor
	}

	if unscaled > math.MaxInt64 {
		return decimal128.Num{}, false
	}
	value := int64(unscaled)
	if sign == postgresNumericNegative {
		value = -value
	}
	result := decimal128.FromI64(value)
	if !result.FitsInPrecision(precision) {
		return decimal128.Num{}, false
	}
	return result, true
}

func uint64PowerOfTen(exponent int) (uint64, bool) {
	if exponent < 0 || exponent > 18 {
		return 0, false
	}
	result := uint64(1)
	for range exponent {
		result *= 10
	}
	return result, true
}

func appendPostgresRawRow(appenders []postgresRawAppender, builders []array.Builder, values [][]byte) error {
	if len(values) != len(appenders) {
		return fmt.Errorf("postgres row has %d values, expected %d", len(values), len(appenders))
	}
	for i, value := range values {
		if value == nil {
			builders[i].AppendNull()
			continue
		}
		if err := appenders[i](value); err != nil {
			return fmt.Errorf("failed to decode PostgreSQL column %d: %w", i, err)
		}
	}
	return nil
}
