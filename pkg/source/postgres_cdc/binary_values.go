package postgres_cdc

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgreSQL type OIDs for the binary decode path.
const (
	oidBool        = 16
	oidBytea       = 17
	oidChar        = 18
	oidName        = 19
	oidInt8        = 20
	oidInt2        = 21
	oidInt4        = 23
	oidText        = 25
	oidOID         = 26
	oidJSON        = 114
	oidFloat4      = 700
	oidFloat8      = 701
	oidBpchar      = 1042
	oidVarchar     = 1043
	oidDate        = 1082
	oidTime        = 1083
	oidTimestamp   = 1114
	oidTimestamptz = 1184
	oidNumeric     = 1700
	oidUUID        = 2950
)

// pgEpoch is PostgreSQL's binary timestamp epoch (2000-01-01 UTC); binary
// timestamps are microseconds relative to it, dates are days relative to it.
var pgEpoch = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

const (
	pgTimestampInfinity    = math.MaxInt64
	pgTimestampNegInfinity = math.MinInt64
)

// convertBinaryValue decodes a pgoutput binary-format column value (sent when
// the replication stream runs with the `binary 'true'` option) into the same
// Go representation the text path (convertTextValue) produces, so downstream
// Arrow materialization is identical in both modes.
//
// Only the standard OIDs below are supported; any other type surfaces an
// error telling the user to drop the opt-in binary option rather than
// risking silent misdecoding of an unknown binary send format.
func convertBinaryValue(data []byte, col schema.Column, oid uint32, m *pgtype.Map) (interface{}, error) {
	switch col.DataType {
	case schema.TypeBoolean:
		if len(data) != 1 {
			return nil, fmt.Errorf("binary bool: want 1 byte, got %d", len(data))
		}
		return data[0] != 0, nil

	case schema.TypeInt16:
		if len(data) != 2 {
			return nil, fmt.Errorf("binary int2: want 2 bytes, got %d", len(data))
		}
		return int16(binary.BigEndian.Uint16(data)), nil

	case schema.TypeInt32:
		if len(data) != 4 {
			return nil, fmt.Errorf("binary int4: want 4 bytes, got %d", len(data))
		}
		return int32(binary.BigEndian.Uint32(data)), nil

	case schema.TypeInt64:
		if len(data) != 8 {
			return nil, fmt.Errorf("binary int8: want 8 bytes, got %d", len(data))
		}
		return int64(binary.BigEndian.Uint64(data)), nil

	case schema.TypeFloat32:
		if len(data) != 4 {
			return nil, fmt.Errorf("binary float4: want 4 bytes, got %d", len(data))
		}
		return math.Float32frombits(binary.BigEndian.Uint32(data)), nil

	case schema.TypeFloat64:
		if len(data) != 8 {
			return nil, fmt.Errorf("binary float8: want 8 bytes, got %d", len(data))
		}
		return math.Float64frombits(binary.BigEndian.Uint64(data)), nil

	case schema.TypeTimestamp, schema.TypeTimestampTZ:
		if len(data) != 8 {
			return nil, fmt.Errorf("binary timestamp: want 8 bytes, got %d", len(data))
		}
		micros := int64(binary.BigEndian.Uint64(data))
		if micros == pgTimestampInfinity || micros == pgTimestampNegInfinity {
			return nil, nil
		}
		return pgEpoch.Add(time.Duration(micros) * time.Microsecond), nil

	case schema.TypeDate:
		if len(data) != 4 {
			return nil, fmt.Errorf("binary date: want 4 bytes, got %d", len(data))
		}
		days := int32(binary.BigEndian.Uint32(data))
		if days == math.MaxInt32 || days == math.MinInt32 {
			return nil, nil
		}
		return pgEpoch.AddDate(0, 0, int(days)), nil

	case schema.TypeTime:
		if len(data) != 8 {
			return nil, fmt.Errorf("binary time: want 8 bytes, got %d", len(data))
		}
		micros := int64(binary.BigEndian.Uint64(data))
		// Match the text path, which parses "15:04:05" onto the zero date.
		return time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(micros) * time.Microsecond), nil

	case schema.TypeDecimal:
		var n pgtype.Numeric
		if err := m.Scan(oidNumeric, pgtype.BinaryFormatCode, data, &n); err != nil {
			return nil, fmt.Errorf("binary numeric: %w", err)
		}
		if !n.Valid {
			return nil, nil
		}
		v, err := n.Value()
		if err != nil {
			return nil, fmt.Errorf("binary numeric: %w", err)
		}
		// Matches the text path, which keeps decimals as strings.
		return v, nil

	case schema.TypeJSON:
		if oid == oidJSONB {
			if len(data) < 1 || data[0] != 1 {
				return nil, fmt.Errorf("binary jsonb: unknown version byte")
			}
			return string(data[1:]), nil
		}
		return string(data), nil

	case schema.TypeUUID:
		if len(data) != 16 {
			return nil, fmt.Errorf("binary uuid: want 16 bytes, got %d", len(data))
		}
		return formatUUID(data), nil

	case schema.TypeBinary:
		return append([]byte(nil), data...), nil

	case schema.TypeArray:
		return convertBinaryArray(data, col, m)

	case schema.TypeString:
		switch oid {
		case oidText, oidVarchar, oidBpchar, oidName, oidChar:
			return string(data), nil
		case oidUUID:
			return convertBinaryValue(data, schema.Column{DataType: schema.TypeUUID}, oid, m)
		case oidOID:
			if len(data) != 4 {
				return nil, fmt.Errorf("binary oid: want 4 bytes, got %d", len(data))
			}
			return fmt.Sprintf("%d", binary.BigEndian.Uint32(data)), nil
		}
	}

	return nil, fmt.Errorf(
		"column %q (type OID %d) has no supported binary decoding; remove the binary=true option from the source URI",
		col.Name, oid,
	)
}

// oidJSONB is separate from the const block above for readability at its use
// site; jsonb's binary format prefixes a version byte to the JSON text.
const oidJSONB = 3802

// formatUUID renders 16 raw UUID bytes in canonical form.
func formatUUID(data []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[0:4], data[4:6], data[6:8], data[8:10], data[10:16])
}

// convertBinaryArray decodes PostgreSQL's binary array format:
// int32 ndim, int32 hasnull, uint32 element OID, then per dimension
// (int32 size, int32 lower bound), then per element int32 byte length
// (-1 for NULL) followed by the element's binary value.
func convertBinaryArray(data []byte, col schema.Column, m *pgtype.Map) (interface{}, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("binary array: header truncated")
	}
	ndim := int32(binary.BigEndian.Uint32(data[0:4]))
	elemOID := binary.BigEndian.Uint32(data[8:12])
	data = data[12:]

	if ndim == 0 {
		return []interface{}{}, nil
	}
	if ndim != 1 {
		return nil, fmt.Errorf("binary array: %d-dimensional arrays are not supported; remove the binary=true option from the source URI", ndim)
	}

	if len(data) < 8 {
		return nil, fmt.Errorf("binary array: dimension header truncated")
	}
	count := int(int32(binary.BigEndian.Uint32(data[0:4])))
	data = data[8:]

	elemCol := schema.Column{Name: col.Name, DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale}
	out := make([]interface{}, 0, count)
	for i := 0; i < count; i++ {
		if len(data) < 4 {
			return nil, fmt.Errorf("binary array: element %d length truncated", i)
		}
		length := int32(binary.BigEndian.Uint32(data[0:4]))
		data = data[4:]
		if length < 0 {
			out = append(out, nil)
			continue
		}
		if len(data) < int(length) {
			return nil, fmt.Errorf("binary array: element %d data truncated", i)
		}
		v, err := convertBinaryValue(data[:length], elemCol, elemOID, m)
		if err != nil {
			return nil, fmt.Errorf("binary array element %d: %w", i, err)
		}
		out = append(out, v)
		data = data[length:]
	}
	return out, nil
}
