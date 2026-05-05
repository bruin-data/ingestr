package arrowutil

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Value converts an Arrow array element into a Go value suitable for database drivers.
func Value(arr interface {
	IsNull(int) bool
	Len() int
}, idx int,
) interface{} {
	if arr.IsNull(idx) {
		return nil
	}

	switch a := arr.(type) {
	case interface{ Value(int) bool }:
		return a.Value(idx)
	case interface{ Value(int) int16 }:
		return a.Value(idx)
	case interface{ Value(int) int32 }:
		return a.Value(idx)
	case interface{ Value(int) int64 }:
		return a.Value(idx)
	case interface{ Value(int) float32 }:
		return a.Value(idx)
	case interface{ Value(int) float64 }:
		return a.Value(idx)
	case interface{ Value(int) string }:
		return a.Value(idx)
	case interface{ Value(int) []byte }:
		return a.Value(idx)
	case *array.Decimal128:
		v := a.Value(idx)
		dt := a.DataType().(*arrow.Decimal128Type)
		return v.ToFloat64(dt.Scale)
	case *array.Date32:
		v := a.Value(idx)
		return v.ToTime()
	case *array.Date64:
		v := a.Value(idx)
		return v.ToTime()
	case *array.Time64:
		v := a.Value(idx)
		micros := int64(v)
		h := micros / 3600000000
		micros %= 3600000000
		m := micros / 60000000
		micros %= 60000000
		s := micros / 1000000
		micros %= 1000000
		return time.Date(0, 1, 1, int(h), int(m), int(s), int(micros)*1000, time.UTC)
	case *array.Timestamp:
		v := a.Value(idx)
		return v.ToTime(arrow.Microsecond)
	case array.ExtensionArray:
		// Handle extension types (like JSON) by extracting the underlying storage value.
		storage := a.Storage()
		return Value(storage, idx)
	default:
		return nil
	}
}
