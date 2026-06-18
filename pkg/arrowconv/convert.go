package arrowconv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/araddon/dateparse"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/shopspring/decimal"
)

// ItemsToArrowRecordWithSchema builds an Arrow RecordBatch from items and a given schema,
// excluding any columns specified in excludeColumns. Additional fields are included as unknown
// without per-item type inference.
func ItemsToArrowRecordWithSchema(items []map[string]interface{}, cols []schema.Column, excludeColumns []string) (arrow.RecordBatch, error) {
	excludeMap := make(map[string]bool)
	for _, col := range excludeColumns {
		excludeMap[strings.ToLower(col)] = true
	}

	fieldOrder := make([]string, 0, len(cols))
	fieldTypes := make(map[string]arrow.DataType)
	fieldNullable := make(map[string]bool)
	baseColumns := make(map[string]bool)

	for _, col := range cols {
		if excludeMap[strings.ToLower(col.Name)] {
			continue
		}
		fieldOrder = append(fieldOrder, col.Name)
		fieldTypes[col.Name] = schema.DataTypeToArrowType(col)
		fieldNullable[col.Name] = col.Nullable
		baseColumns[col.Name] = true
	}

	for _, item := range items {
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			if excludeMap[strings.ToLower(key)] {
				continue
			}
			if baseColumns[key] {
				continue
			}
			if _, ok := fieldTypes[key]; ok {
				continue
			}
			fieldOrder = append(fieldOrder, key)
			fieldTypes[key] = schema.UnknownArrowType
			fieldNullable[key] = true
		}
	}

	if len(fieldOrder) == 0 {
		emptySchema := arrow.NewSchema([]arrow.Field{}, nil)
		return array.NewRecordBatch(emptySchema, []arrow.Array{}, 0), nil
	}

	fields := make([]arrow.Field, len(fieldOrder))
	for i, name := range fieldOrder {
		fields[i] = arrow.Field{
			Name:     name,
			Type:     fieldTypes[name],
			Nullable: fieldNullable[name],
		}
	}

	arrowSchema := arrow.NewSchema(fields, nil)

	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(fieldOrder))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	for _, item := range items {
		for i, name := range fieldOrder {
			val, exists := item[name]
			if !exists || val == nil {
				builders[i].AppendNull()
				continue
			}
			AppendValue(builders[i], val)
		}
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
	}

	record := array.NewRecordBatch(arrowSchema, arrays, int64(len(items)))

	for _, arr := range arrays {
		arr.Release()
	}

	return record, nil
}

func IsJSONType(dt arrow.DataType) bool {
	if dt.ID() != arrow.EXTENSION {
		return false
	}
	ext, ok := dt.(arrow.ExtensionType)
	if !ok {
		return false
	}
	return ext.ExtensionName() == schema.JSONExtensionName
}

func IsNumeric(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64,
		arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64:
		return true
	default:
		return false
	}
}

func IsFloat(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64:
		return true
	default:
		return false
	}
}

// appendInt8/appendInt16/appendInt32 narrow an int64 into a fixed-width
// builder, appending a null instead of silently truncating out-of-range values.
func appendInt8(b *array.Int8Builder, i int64) {
	if i >= math.MinInt8 && i <= math.MaxInt8 {
		b.Append(int8(i))
	} else {
		b.AppendNull()
	}
}

func appendInt16(b *array.Int16Builder, i int64) {
	if i >= math.MinInt16 && i <= math.MaxInt16 {
		b.Append(int16(i))
	} else {
		b.AppendNull()
	}
}

func appendInt32(b *array.Int32Builder, i int64) {
	if i >= math.MinInt32 && i <= math.MaxInt32 {
		b.Append(int32(i))
	} else {
		b.AppendNull()
	}
}

func AppendValue(builder array.Builder, val interface{}) {
	if val == nil {
		builder.AppendNull()
		return
	}

	switch b := builder.(type) {
	case *array.BooleanBuilder:
		switch v := val.(type) {
		case bool:
			b.Append(v)
		case float64:
			b.Append(v != 0)
		case int64:
			b.Append(v != 0)
		case int:
			b.Append(v != 0)
		case uint8:
			b.Append(v != 0)
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "1", "t", "true", "yes", "y", "on":
				b.Append(true)
			case "0", "f", "false", "no", "n", "off", "":
				b.Append(false)
			default:
				b.AppendNull()
			}
		case json.Number:
			if i, err := v.Int64(); err == nil {
				b.Append(i != 0)
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.Int8Builder:
		switch v := val.(type) {
		case int8:
			b.Append(v)
		case int16:
			appendInt8(b, int64(v))
		case int32:
			appendInt8(b, int64(v))
		case int64:
			appendInt8(b, v)
		case int:
			appendInt8(b, int64(v))
		case float64:
			if v >= math.MinInt8 && v <= math.MaxInt8 {
				b.Append(int8(v))
			} else {
				b.AppendNull()
			}
		case uint8:
			appendInt8(b, int64(v))
		case string:
			if i, err := strconv.ParseInt(v, 10, 8); err == nil {
				b.Append(int8(i))
			} else {
				b.AppendNull()
			}
		case json.Number:
			if i, err := v.Int64(); err == nil {
				appendInt8(b, i)
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.Int16Builder:
		switch v := val.(type) {
		case int16:
			b.Append(v)
		case int32:
			appendInt16(b, int64(v))
		case int64:
			appendInt16(b, v)
		case int:
			appendInt16(b, int64(v))
		case float64:
			if v >= math.MinInt16 && v <= math.MaxInt16 {
				b.Append(int16(v))
			} else {
				b.AppendNull()
			}
		case uint8:
			b.Append(int16(v))
		case uint16:
			appendInt16(b, int64(v))
		case int8:
			b.Append(int16(v))
		case string:
			if i, err := strconv.ParseInt(v, 10, 16); err == nil {
				b.Append(int16(i))
			} else {
				b.AppendNull()
			}
		case json.Number:
			if i, err := v.Int64(); err == nil {
				appendInt16(b, i)
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.Int32Builder:
		switch v := val.(type) {
		case int32:
			b.Append(v)
		case int:
			appendInt32(b, int64(v))
		case int64:
			appendInt32(b, v)
		case int8:
			b.Append(int32(v))
		case int16:
			b.Append(int32(v))
		case uint8:
			b.Append(int32(v))
		case uint16:
			b.Append(int32(v))
		case uint32:
			appendInt32(b, int64(v))
		case float64:
			if v >= math.MinInt32 && v <= math.MaxInt32 {
				b.Append(int32(v))
			} else {
				b.AppendNull()
			}
		case string:
			if i, err := strconv.ParseInt(v, 10, 32); err == nil {
				b.Append(int32(i))
			} else {
				b.AppendNull()
			}
		case json.Number:
			if i, err := v.Int64(); err == nil {
				appendInt32(b, i)
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.Int64Builder:
		switch v := val.(type) {
		case float64:
			b.Append(int64(v))
		case int64:
			b.Append(v)
		case int:
			b.Append(int64(v))
		case int32:
			b.Append(int64(v))
		case int16:
			b.Append(int64(v))
		case int8:
			b.Append(int64(v))
		case uint8:
			b.Append(int64(v))
		case uint16:
			b.Append(int64(v))
		case uint32:
			b.Append(int64(v))
		case uint64:
			if v <= math.MaxInt64 {
				b.Append(int64(v))
			} else {
				b.AppendNull()
			}
		case string:
			if i, err := strconv.ParseInt(v, 10, 64); err == nil {
				b.Append(i)
			} else if f, err := strconv.ParseFloat(v, 64); err == nil {
				b.Append(int64(f))
			} else {
				b.AppendNull()
			}
		case json.Number:
			if i, err := v.Int64(); err == nil {
				b.Append(i)
			} else {
				b.AppendNull()
			}
		case []interface{}:
			if len(v) == 1 {
				AppendValue(b, v[0])
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.Float32Builder:
		switch v := val.(type) {
		case float32:
			b.Append(v)
		case float64:
			b.Append(float32(v))
		case int:
			b.Append(float32(v))
		case int32:
			b.Append(float32(v))
		case int64:
			b.Append(float32(v))
		case string:
			if f, err := strconv.ParseFloat(v, 32); err == nil {
				b.Append(float32(f))
			} else {
				b.AppendNull()
			}
		case json.Number:
			if f, err := v.Float64(); err == nil {
				b.Append(float32(f))
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.Float64Builder:
		switch v := val.(type) {
		case float64:
			b.Append(v)
		case int64:
			b.Append(float64(v))
		case int:
			b.Append(float64(v))
		case int32:
			b.Append(float64(v))
		case float32:
			b.Append(float64(v))
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				b.Append(f)
			} else {
				b.AppendNull()
			}
		case json.Number:
			if f, err := v.Float64(); err == nil {
				b.Append(f)
			} else {
				b.AppendNull()
			}
		case []interface{}:
			if len(v) == 1 {
				AppendValue(b, v[0])
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.StringBuilder:
		switch v := val.(type) {
		case string:
			b.Append(v)
		case []byte:
			b.Append(string(v))
		case map[string]interface{}:
			jsonBytes, err := marshalJSON(v)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(string(jsonBytes))
			}
		case []interface{}:
			jsonBytes, err := marshalJSON(v)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(string(jsonBytes))
			}
		case float64:
			b.Append(strconv.FormatFloat(v, 'f', -1, 64))
		case bool:
			if v {
				b.Append("True")
			} else {
				b.Append("False")
			}
		default:
			b.Append(fmt.Sprintf("%v", v))
		}

	case *array.BinaryBuilder:
		switch v := val.(type) {
		case []byte:
			b.Append(v)
		case string:
			b.Append([]byte(v))
		default:
			b.AppendNull()
		}

	case *array.ExtensionBuilder:
		extType, ok := b.Type().(arrow.ExtensionType)
		if !ok {
			b.AppendNull()
			return
		}
		sb, ok := b.StorageBuilder().(*array.StringBuilder)
		if !ok {
			b.AppendNull()
			return
		}
		switch extType.ExtensionName() {
		case schema.JSONExtensionName:
			AppendJSONStringValue(sb, val)
		case schema.UnknownExtensionName:
			AppendUnknownValue(sb, val)
		default:
			b.AppendNull()
		}

	case *array.Date32Builder:
		switch v := val.(type) {
		case time.Time:
			b.Append(arrow.Date32FromTime(v))
		case *time.Time:
			if v != nil {
				b.Append(arrow.Date32FromTime(*v))
			} else {
				b.AppendNull()
			}
		case string:
			if t, err := time.Parse("2006-01-02", v); err == nil {
				b.Append(arrow.Date32FromTime(t))
			} else if t, err := dateparse.ParseAny(v); err == nil {
				b.Append(arrow.Date32FromTime(t))
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.TimestampBuilder:
		switch v := val.(type) {
		case time.Time:
			b.Append(arrow.Timestamp(v.UnixMicro()))
		case *time.Time:
			if v != nil {
				b.Append(arrow.Timestamp(v.UnixMicro()))
			} else {
				b.AppendNull()
			}
		case float64:
			b.Append(arrow.Timestamp(UnixToMicroseconds(int64(v))))
		case int64:
			b.Append(arrow.Timestamp(UnixToMicroseconds(v)))
		case int:
			b.Append(arrow.Timestamp(UnixToMicroseconds(int64(v))))
		case json.Number:
			if i, err := v.Int64(); err == nil {
				b.Append(arrow.Timestamp(UnixToMicroseconds(i)))
			} else if f, err := v.Float64(); err == nil {
				b.Append(arrow.Timestamp(UnixToMicroseconds(int64(f))))
			} else {
				b.AppendNull()
			}
		case string:
			if t, err := dateparse.ParseAny(v); err == nil {
				b.Append(arrow.Timestamp(t.UnixMicro()))
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.Time64Builder:
		switch v := val.(type) {
		case time.Time:
			micros := int64(v.Hour())*3600000000 + int64(v.Minute())*60000000 + int64(v.Second())*1000000 + int64(v.Nanosecond())/1000
			b.Append(arrow.Time64(micros))
		case *time.Time:
			if v != nil {
				micros := int64(v.Hour())*3600000000 + int64(v.Minute())*60000000 + int64(v.Second())*1000000 + int64(v.Nanosecond())/1000
				b.Append(arrow.Time64(micros))
			} else {
				b.AppendNull()
			}
		case string:
			if t, err := time.Parse("15:04:05", v); err == nil {
				micros := int64(t.Hour())*3600000000 + int64(t.Minute())*60000000 + int64(t.Second())*1000000
				b.Append(arrow.Time64(micros))
			} else if t, err := time.Parse("15:04:05.999999", v); err == nil {
				micros := int64(t.Hour())*3600000000 + int64(t.Minute())*60000000 + int64(t.Second())*1000000 + int64(t.Nanosecond())/1000
				b.Append(arrow.Time64(micros))
			} else if t, err := dateparse.ParseAny(v); err == nil {
				micros := int64(t.Hour())*3600000000 + int64(t.Minute())*60000000 + int64(t.Second())*1000000 + int64(t.Nanosecond())/1000
				b.Append(arrow.Time64(micros))
			} else {
				b.AppendNull()
			}
		default:
			b.AppendNull()
		}

	case *array.Decimal128Builder:
		dt, ok := builder.Type().(*arrow.Decimal128Type)
		if !ok {
			b.AppendNull()
			return
		}
		switch v := val.(type) {
		case decimal128.Num:
			b.Append(v)
		case decimal.Decimal:
			num, err := decimal128.FromString(v.String(), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case string:
			v = strings.TrimSpace(v)
			if v == "" {
				b.AppendNull()
			} else {
				num, err := decimal128.FromString(v, dt.Precision, dt.Scale)
				if err != nil {
					bf := new(big.Float)
					if _, ok := bf.SetString(v); ok {
						scale := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dt.Scale)), nil))
						bf.Mul(bf, scale)
						bi, _ := bf.Int(nil)
						b.Append(decimal128.FromBigInt(bi))
					} else {
						b.AppendNull()
					}
				} else {
					b.Append(num)
				}
			}
		case float64:
			str := strconv.FormatFloat(v, 'f', -1, 64)
			num, err := decimal128.FromString(str, dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case int64:
			num, err := decimal128.FromString(strconv.FormatInt(v, 10), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case int:
			num, err := decimal128.FromString(strconv.FormatInt(int64(v), 10), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case int8:
			num, err := decimal128.FromString(strconv.FormatInt(int64(v), 10), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case int16:
			num, err := decimal128.FromString(strconv.FormatInt(int64(v), 10), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case int32:
			num, err := decimal128.FromString(strconv.FormatInt(int64(v), 10), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case uint8:
			num, err := decimal128.FromString(strconv.FormatUint(uint64(v), 10), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case uint16:
			num, err := decimal128.FromString(strconv.FormatUint(uint64(v), 10), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case uint32:
			num, err := decimal128.FromString(strconv.FormatUint(uint64(v), 10), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case uint64:
			num, err := decimal128.FromString(strconv.FormatUint(v, 10), dt.Precision, dt.Scale)
			if err != nil {
				b.AppendNull()
			} else {
				b.Append(num)
			}
		case []byte:
			s := strings.TrimSpace(string(v))
			if s == "" {
				b.AppendNull()
			} else {
				num, err := decimal128.FromString(s, dt.Precision, dt.Scale)
				if err != nil {
					bf := new(big.Float)
					if _, ok := bf.SetString(s); ok {
						scale := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dt.Scale)), nil))
						bf.Mul(bf, scale)
						bi, _ := bf.Int(nil)
						b.Append(decimal128.FromBigInt(bi))
					} else {
						b.AppendNull()
					}
				} else {
					b.Append(num)
				}
			}
		case *big.Int:
			b.Append(decimal128.FromBigInt(v))
		case json.Number:
			AppendValue(b, string(v))
		default:
			b.AppendNull()
		}

	case *array.ListBuilder:
		appendListValue(b, val)

	default:
		builder.AppendNull()
	}
}

func appendListValue(b *array.ListBuilder, val interface{}) {
	if s, ok := val.(string); ok {
		values, ok := parseJSONStringArray(s)
		if !ok {
			b.AppendNull()
			return
		}
		val = values
	}

	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice {
		b.AppendNull()
		return
	}

	b.Append(true)
	vb := b.ValueBuilder()
	for i := 0; i < rv.Len(); i++ {
		elem, ok := listElementValue(rv.Index(i))
		if !ok {
			vb.AppendNull()
			continue
		}
		AppendValue(vb, elem)
	}
}

func parseJSONStringArray(s string) ([]interface{}, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	if s[0] != '[' {
		return nil, false
	}

	var values []interface{}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&values); err != nil {
		return nil, false
	}
	if dec.More() {
		return nil, false
	}
	return values, true
}

func listElementValue(v reflect.Value) (interface{}, bool) {
	if !v.IsValid() {
		return nil, false
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, false
		}
		if v.Type() == reflect.TypeOf((*big.Int)(nil)) {
			return v.Interface(), true
		}
		v = v.Elem()
	}
	return v.Interface(), true
}

func AppendJSONStringValue(b *array.StringBuilder, val interface{}) {
	switch v := val.(type) {
	case string:
		b.Append(v)
	case map[string]interface{}:
		jsonBytes, err := marshalJSON(v)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(string(jsonBytes))
		}
	case []interface{}:
		jsonBytes, err := marshalJSON(v)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(string(jsonBytes))
		}
	case nil:
		b.Append("null")
	default:
		jsonBytes, err := marshalJSON(v)
		if err != nil {
			b.Append(fmt.Sprintf("%v", v))
		} else {
			b.Append(string(jsonBytes))
		}
	}
}

func AppendUnknownValue(b *array.StringBuilder, val interface{}) {
	if val == nil {
		b.AppendNull()
		return
	}

	jsonBytes, err := marshalJSON(val)
	if err != nil {
		b.Append(fmt.Sprintf("%v", val))
		return
	}
	b.Append(string(jsonBytes))
}

// UnixToMicroseconds converts a Unix timestamp to microseconds,
// detecting the unit from the value magnitude.
func UnixToMicroseconds(v int64) int64 {
	switch {
	case v > 1e18: // nanoseconds
		return v / 1000
	case v > 1e15: // microseconds
		return v
	case v > 1e12: // milliseconds
		return v * 1000
	default: // seconds
		return v * 1_000_000
	}
}

func marshalJSON(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
}
