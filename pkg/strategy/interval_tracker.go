package strategy

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
)

type IntervalTracker struct {
	IncrementalKey string
	Min            interface{}
	Max            interface{}

	mu       sync.Mutex
	colIndex int
	foundCol bool
	err      error
}

func NewIntervalTracker(incrementalKey string) *IntervalTracker {
	return &IntervalTracker{
		IncrementalKey: incrementalKey,
		colIndex:       -1,
	}
}

func (t *IntervalTracker) Wrap(records <-chan source.RecordBatchResult) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult)
	go func() {
		defer close(out)
		for result := range records {
			if result.Batch != nil && result.Err == nil {
				t.updateBounds(result.Batch)
			}
			out <- result
		}
	}()
	return out
}

func (t *IntervalTracker) updateBounds(batch arrow.RecordBatch) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if batch.NumRows() == 0 || t.err != nil {
		return
	}

	if !t.foundCol || t.colIndex < 0 || t.colIndex >= int(batch.NumCols()) ||
		batch.ColumnName(t.colIndex) != t.IncrementalKey {
		t.colIndex = -1
		t.foundCol = false
		for i := 0; i < int(batch.NumCols()); i++ {
			if batch.ColumnName(i) == t.IncrementalKey {
				t.colIndex = i
				t.foundCol = true
				break
			}
		}
	}

	if !t.foundCol || t.colIndex < 0 {
		t.err = fmt.Errorf("incremental key %q is missing from a nonempty batch", t.IncrementalKey)
		return
	}

	col := batch.Column(t.colIndex)
	for i := 0; i < col.Len(); i++ {
		if col.IsNull(i) {
			t.err = fmt.Errorf("incremental key %q contains NULL; filter or replace NULL keys before using delete+insert", t.IncrementalKey)
			continue
		}

		val := extractValue(col, i)
		if val == nil {
			t.err = fmt.Errorf("incremental key %q has unsupported Arrow type %s", t.IncrementalKey, col.DataType())
			return
		}
		if t.Min != nil && reflect.TypeOf(val) != reflect.TypeOf(t.Min) {
			t.err = fmt.Errorf("incremental key %q changed bound type from %T to %T; use a consistent key type for delete+insert", t.IncrementalKey, t.Min, val)
			return
		}

		if t.Min == nil || compareValues(val, t.Min) < 0 {
			t.Min = val
		}
		if t.Max == nil || compareValues(val, t.Max) > 0 {
			t.Max = val
		}
	}
}

func extractValue(col arrow.Array, idx int) interface{} {
	switch arr := col.(type) {
	case *array.Int64:
		return arr.Value(idx)
	case *array.Int32:
		return int64(arr.Value(idx))
	case *array.Int16:
		return int64(arr.Value(idx))
	case *array.Int8:
		return int64(arr.Value(idx))
	case *array.Uint64:
		return arr.Value(idx)
	case *array.Uint32:
		return int64(arr.Value(idx))
	case *array.Uint16:
		return int64(arr.Value(idx))
	case *array.Uint8:
		return int64(arr.Value(idx))
	case *array.Float64:
		return arr.Value(idx)
	case *array.Float32:
		return float64(arr.Value(idx))
	case *array.Decimal128:
		dt, ok := arr.DataType().(*arrow.Decimal128Type)
		if !ok {
			return nil
		}
		return arr.Value(idx).ToFloat64(dt.Scale)
	case *array.Decimal256:
		dt, ok := arr.DataType().(*arrow.Decimal256Type)
		if !ok {
			return nil
		}
		return arr.Value(idx).ToFloat64(dt.Scale)
	case *array.String:
		return arr.Value(idx)
	case *array.LargeString:
		return arr.Value(idx)
	case *array.Timestamp:
		return arr.Value(idx).ToTime(arrow.Microsecond)
	case *array.Date32:
		return arr.Value(idx).ToTime()
	case *array.Date64:
		return arr.Value(idx).ToTime()
	case *array.Time64:
		return arr.Value(idx).FormattedString(arr.DataType().(*arrow.Time64Type).Unit)
	default:
		return nil
	}
}

func compareValues(a, b interface{}) int {
	switch va := a.(type) {
	case uint64:
		vb := b.(uint64)
		if va < vb {
			return -1
		} else if va > vb {
			return 1
		}
		return 0
	case int64:
		vb := b.(int64)
		if va < vb {
			return -1
		} else if va > vb {
			return 1
		}
		return 0
	case float64:
		vb := b.(float64)
		if va < vb {
			return -1
		} else if va > vb {
			return 1
		}
		return 0
	case string:
		vb := b.(string)
		if va < vb {
			return -1
		} else if va > vb {
			return 1
		}
		return 0
	case time.Time:
		vb := b.(time.Time)
		if va.Before(vb) {
			return -1
		} else if va.After(vb) {
			return 1
		}
		return 0
	default:
		return 0
	}
}
