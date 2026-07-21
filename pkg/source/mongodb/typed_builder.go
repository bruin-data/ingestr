package mongodb

import (
	"bytes"
	"encoding/hex"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/x/bsonx/bsoncore"
)

// typedColumnBuilder lazily picks an Arrow type from the first non-null value
// observed for a column. Subsequent values that don't fit the chosen type cause
// the column to be promoted to the unknown string-extension type (with values
// re-encoded as JSON), matching the previous behavior.
type typedColumnBuilder struct {
	mem         memory.Allocator
	rowCapacity int
	nullsAhead  int
	rowCount    int
	typ         arrow.DataType
	builder     array.Builder
	jsonBuffer  bytes.Buffer
}

func newTypedColumnBuilder(mem memory.Allocator) *typedColumnBuilder {
	return newTypedColumnBuilderWithCapacity(mem, 0)
}

func newTypedColumnBuilderWithCapacity(mem memory.Allocator, rowCapacity int) *typedColumnBuilder {
	return &typedColumnBuilder{mem: mem, rowCapacity: rowCapacity}
}

func (c *typedColumnBuilder) AppendNull() {
	if c.builder != nil {
		c.builder.AppendNull()
	} else {
		c.nullsAhead++
	}
	c.rowCount++
}

// AppendNulls adds n nulls at once; used when a column first appears mid-batch.
func (c *typedColumnBuilder) AppendNulls(n int) {
	if n <= 0 {
		return
	}
	if c.builder != nil {
		c.builder.AppendNulls(n)
	} else {
		c.nullsAhead += n
	}
	c.rowCount += n
}

func (c *typedColumnBuilder) Append(val any) {
	if val == nil {
		c.AppendNull()
		return
	}

	if c.builder == nil {
		c.materialize(inferTypeFromBSONValue(val))
	}

	if c.tryAppend(val) {
		c.rowCount++
		return
	}

	c.promoteToUnknown()
	arrowconv.AppendValue(c.builder, convertBSONValue(val))
	c.rowCount++
}

func (c *typedColumnBuilder) AppendRaw(val bson.RawValue) {
	if val.Type == bson.TypeNull {
		c.AppendNull()
		return
	}

	if c.builder == nil {
		c.materialize(inferTypeFromRawBSONValue(val))
	}

	if c.tryAppendRaw(val) {
		c.rowCount++
		return
	}

	c.promoteToUnknown()
	arrowconv.AppendValue(c.builder, convertRawBSONValue(val))
	c.rowCount++
}

func (c *typedColumnBuilder) materialize(typ arrow.DataType) {
	c.typ = typ
	c.builder = array.NewBuilder(c.mem, typ)
	if c.rowCapacity > 0 {
		c.builder.Reserve(c.rowCapacity)
	}
	if c.nullsAhead > 0 {
		c.builder.AppendNulls(c.nullsAhead)
		c.nullsAhead = 0
	}
}

func (c *typedColumnBuilder) tryAppend(val any) bool {
	switch b := c.builder.(type) {
	case *array.StringBuilder:
		switch v := val.(type) {
		case string:
			b.Append(v)
			return true
		case primitive.ObjectID:
			b.Append(v.Hex())
			return true
		case primitive.Decimal128:
			b.Append(v.String())
			return true
		case primitive.Regex:
			b.Append(v.Pattern)
			return true
		}
	case *array.Int64Builder:
		switch v := val.(type) {
		case int64:
			b.Append(v)
			return true
		case int32:
			b.Append(int64(v))
			return true
		case int:
			b.Append(int64(v))
			return true
		}
	case *array.Float64Builder:
		switch v := val.(type) {
		case float64:
			b.Append(v)
			return true
		case float32:
			b.Append(float64(v))
			return true
		case int64:
			b.Append(float64(v))
			return true
		case int32:
			b.Append(float64(v))
			return true
		case int:
			b.Append(float64(v))
			return true
		}
	case *array.BooleanBuilder:
		if v, ok := val.(bool); ok {
			b.Append(v)
			return true
		}
	case *array.TimestampBuilder:
		switch v := val.(type) {
		case primitive.DateTime:
			b.Append(arrow.Timestamp(v.Time().UnixMicro()))
			return true
		case primitive.Timestamp:
			b.Append(arrow.Timestamp(int64(v.T) * int64(time.Second/time.Microsecond)))
			return true
		case time.Time:
			b.Append(arrow.Timestamp(v.UnixMicro()))
			return true
		}
	case *array.BinaryBuilder:
		if v, ok := val.(primitive.Binary); ok {
			b.Append(v.Data)
			return true
		}
	case *array.ExtensionBuilder:
		arrowconv.AppendValue(b, convertBSONValue(val))
		return true
	}
	return false
}

func (c *typedColumnBuilder) tryAppendRaw(val bson.RawValue) bool {
	if appendRawValue(c.builder, val, &c.jsonBuffer) {
		return true
	}
	if b, ok := c.builder.(*array.ExtensionBuilder); ok {
		arrowconv.AppendValue(b, convertRawBSONValue(val))
		return true
	}
	return false
}

func appendRawValue(builder array.Builder, val bson.RawValue, jsonBuffer *bytes.Buffer) bool {
	switch b := builder.(type) {
	case *array.StringBuilder:
		switch val.Type {
		case bson.TypeString:
			if !appendRawStringValue(b, val) {
				return false
			}
			return true
		case bson.TypeObjectID:
			if !appendRawObjectIDHex(b, val) {
				return false
			}
			return true
		case bson.TypeDecimal128:
			v, ok := val.Decimal128OK()
			if !ok {
				return false
			}
			b.Append(v.String())
			return true
		case bson.TypeRegex:
			pattern, ok := rawRegexPatternBytes(val)
			if !ok {
				return false
			}
			b.BinaryBuilder.Append(pattern)
			return true
		}
	case *array.Int64Builder:
		switch val.Type {
		case bson.TypeInt32:
			v, ok := val.Int32OK()
			if !ok {
				return false
			}
			b.Append(int64(v))
			return true
		case bson.TypeInt64:
			v, ok := val.Int64OK()
			if !ok {
				return false
			}
			b.Append(v)
			return true
		}
	case *array.Float64Builder:
		switch val.Type {
		case bson.TypeDouble:
			v, ok := val.DoubleOK()
			if !ok {
				return false
			}
			b.Append(v)
			return true
		case bson.TypeInt32:
			v, ok := val.Int32OK()
			if !ok {
				return false
			}
			b.Append(float64(v))
			return true
		case bson.TypeInt64:
			v, ok := val.Int64OK()
			if !ok {
				return false
			}
			b.Append(float64(v))
			return true
		}
	case *array.BooleanBuilder:
		if val.Type != bson.TypeBoolean {
			return false
		}
		v, ok := val.BooleanOK()
		if !ok {
			return false
		}
		b.Append(v)
		return true
	case *array.TimestampBuilder:
		switch val.Type {
		case bson.TypeDateTime:
			v, ok := val.DateTimeOK()
			if !ok {
				return false
			}
			b.Append(arrow.Timestamp(v * 1000))
			return true
		case bson.TypeTimestamp:
			seconds, _, ok := val.TimestampOK()
			if !ok {
				return false
			}
			b.Append(arrow.Timestamp(int64(seconds) * int64(time.Second/time.Microsecond)))
			return true
		}
	case *array.BinaryBuilder:
		if val.Type != bson.TypeBinary {
			return false
		}
		_, data, ok := val.BinaryOK()
		if !ok {
			return false
		}
		b.Append(data)
		return true
	case *array.ExtensionBuilder:
		if appendRawExtensionValue(b, val, jsonBuffer) {
			return true
		}
	}
	return false
}

func appendRawExtensionValue(builder *array.ExtensionBuilder, val bson.RawValue, jsonBuffer *bytes.Buffer) bool {
	extType, ok := builder.Type().(arrow.ExtensionType)
	if !ok || (extType.ExtensionName() != schema.UnknownExtensionName && extType.ExtensionName() != schema.JSONExtensionName) {
		return false
	}

	storage, ok := builder.StorageBuilder().(*array.StringBuilder)
	if !ok {
		return false
	}

	switch val.Type {
	case bson.TypeEmbeddedDocument, bson.TypeArray:
		jsonBuffer.Reset()
		if !appendRawBSONJSONValue(jsonBuffer, val) {
			return false
		}
		storage.BinaryBuilder.Append(jsonBuffer.Bytes())
		return true
	default:
		return false
	}
}

func appendRawStringValue(builder *array.StringBuilder, val bson.RawValue) bool {
	value, ok := rawStringBytes(val)
	if !ok {
		return false
	}
	builder.BinaryBuilder.Append(value)
	return true
}

func rawStringBytes(val bson.RawValue) ([]byte, bool) {
	length, rem, ok := bsoncore.ReadLength(val.Value)
	if !ok || length <= 0 || len(val.Value[4:]) < int(length) {
		return nil, false
	}
	return rem[:length-1], true
}

func rawRegexPatternBytes(val bson.RawValue) ([]byte, bool) {
	patternEnd := bytes.IndexByte(val.Value, 0)
	if patternEnd < 0 || bytes.IndexByte(val.Value[patternEnd+1:], 0) < 0 {
		return nil, false
	}
	return val.Value[:patternEnd], true
}

func appendRawObjectIDHex(builder *array.StringBuilder, val bson.RawValue) bool {
	if len(val.Value) < 12 {
		return false
	}

	var buf [24]byte
	hex.Encode(buf[:], val.Value[:12])
	builder.BinaryBuilder.Append(buf[:])
	return true
}

func (c *typedColumnBuilder) promoteToUnknown() {
	if isUnknownType(c.typ) {
		return
	}

	newBuilder := array.NewBuilder(c.mem, schema.UnknownArrowType)
	if c.rowCapacity > 0 {
		newBuilder.Reserve(c.rowCapacity)
	}
	if c.builder != nil {
		existingArr := c.builder.NewArray()
		replayArrayAsUnknown(newBuilder, existingArr)
		existingArr.Release()
		c.builder.Release()
	}
	c.builder = newBuilder
	c.typ = schema.UnknownArrowType
}

func (c *typedColumnBuilder) Build(targetRows int) (arrow.Array, arrow.Field) {
	if c.builder == nil {
		// Column was only ever null. Emit as unknown so the schema inferrer
		// can drop it (matches existing behavior).
		b := array.NewBuilder(c.mem, schema.UnknownArrowType)
		b.AppendNulls(targetRows)
		arr := b.NewArray()
		b.Release()
		return arr, arrow.Field{Type: schema.UnknownArrowType, Nullable: true}
	}

	if missing := targetRows - c.rowCount; missing > 0 {
		c.builder.AppendNulls(missing)
	}
	arr := c.builder.NewArray()
	c.builder.Release()
	c.builder = nil
	return arr, arrow.Field{Type: c.typ, Nullable: true}
}

func (c *typedColumnBuilder) Release() {
	if c.builder != nil {
		c.builder.Release()
		c.builder = nil
	}
}

func inferTypeFromBSONValue(val any) arrow.DataType {
	switch val.(type) {
	case string:
		return arrow.BinaryTypes.String
	case int, int32, int64:
		return arrow.PrimitiveTypes.Int64
	case float32, float64:
		return arrow.PrimitiveTypes.Float64
	case bool:
		return arrow.FixedWidthTypes.Boolean
	case primitive.DateTime, primitive.Timestamp, time.Time:
		return arrow.FixedWidthTypes.Timestamp_us
	case primitive.ObjectID:
		return arrow.BinaryTypes.String
	case primitive.Decimal128:
		return arrow.BinaryTypes.String
	case primitive.Binary:
		return arrow.BinaryTypes.Binary
	case primitive.Regex:
		return arrow.BinaryTypes.String
	case bson.M, bson.D, primitive.A:
		return schema.JSONArrowType
	}
	return schema.UnknownArrowType
}

func inferTypeFromRawBSONValue(val bson.RawValue) arrow.DataType {
	switch val.Type {
	case bson.TypeString, bson.TypeObjectID, bson.TypeDecimal128, bson.TypeRegex:
		return arrow.BinaryTypes.String
	case bson.TypeInt32, bson.TypeInt64:
		return arrow.PrimitiveTypes.Int64
	case bson.TypeDouble:
		return arrow.PrimitiveTypes.Float64
	case bson.TypeBoolean:
		return arrow.FixedWidthTypes.Boolean
	case bson.TypeDateTime, bson.TypeTimestamp:
		return arrow.FixedWidthTypes.Timestamp_us
	case bson.TypeBinary:
		return arrow.BinaryTypes.Binary
	case bson.TypeEmbeddedDocument, bson.TypeArray:
		return schema.JSONArrowType
	}
	return schema.UnknownArrowType
}

func replayArrayAsUnknown(dst array.Builder, arr arrow.Array) {
	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			dst.AppendNull()
			continue
		}
		switch a := arr.(type) {
		case *array.String:
			arrowconv.AppendValue(dst, a.Value(i))
		case *array.Int64:
			arrowconv.AppendValue(dst, a.Value(i))
		case *array.Float64:
			arrowconv.AppendValue(dst, a.Value(i))
		case *array.Boolean:
			arrowconv.AppendValue(dst, a.Value(i))
		case *array.Timestamp:
			arrowconv.AppendValue(dst, time.UnixMicro(int64(a.Value(i))).UTC())
		case *array.Binary:
			arrowconv.AppendValue(dst, a.Value(i))
		default:
			dst.AppendNull()
		}
	}
}

func isUnknownType(t arrow.DataType) bool {
	if t == nil {
		return false
	}
	if ext, ok := t.(arrow.ExtensionType); ok {
		return ext.ExtensionName() == schema.UnknownExtensionName
	}
	return false
}
