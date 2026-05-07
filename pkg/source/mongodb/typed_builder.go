package mongodb

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// typedColumnBuilder lazily picks an Arrow type from the first non-null value
// observed for a column. Subsequent values that don't fit the chosen type cause
// the column to be promoted to the unknown string-extension type (with values
// re-encoded as JSON), matching the previous behavior.
type typedColumnBuilder struct {
	mem        memory.Allocator
	nullsAhead int
	rowCount   int
	typ        arrow.DataType
	builder    array.Builder
}

func newTypedColumnBuilder(mem memory.Allocator) *typedColumnBuilder {
	return &typedColumnBuilder{mem: mem}
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

func (c *typedColumnBuilder) materialize(typ arrow.DataType) {
	c.typ = typ
	c.builder = array.NewBuilder(c.mem, typ)
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

func (c *typedColumnBuilder) promoteToUnknown() {
	if isUnknownType(c.typ) {
		return
	}

	newBuilder := array.NewBuilder(c.mem, schema.UnknownArrowType)
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
	case primitive.DateTime, time.Time:
		return arrow.FixedWidthTypes.Timestamp_us
	case primitive.ObjectID:
		return arrow.BinaryTypes.String
	case primitive.Decimal128:
		return arrow.BinaryTypes.String
	case primitive.Binary:
		return arrow.BinaryTypes.Binary
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
