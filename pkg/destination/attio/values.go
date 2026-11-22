package attio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// cellValue converts a source cell to the native JSON value Attio takes,
// returning ok=false for nulls and non-finite floats.
func cellValue(arr arrow.Array, idx int) (interface{}, bool) {
	if arr.IsNull(idx) {
		return nil, false
	}

	if ext, ok := arr.DataType().(arrow.ExtensionType); ok && ext.ExtensionName() == schema.JSONExtensionName {
		val := arrowutil.Value(arr, idx)
		str, isStr := val.(string)
		if !isStr {
			return val, val != nil
		}
		var decoded interface{}
		if err := json.Unmarshal([]byte(str), &decoded); err != nil {
			return strings.Clone(str), true
		}
		return decoded, decoded != nil
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx), true
	case *array.Int8:
		return int64(a.Value(idx)), true
	case *array.Int16:
		return int64(a.Value(idx)), true
	case *array.Int32:
		return int64(a.Value(idx)), true
	case *array.Int64:
		return a.Value(idx), true
	case *array.Uint8:
		return int64(a.Value(idx)), true
	case *array.Uint16:
		return int64(a.Value(idx)), true
	case *array.Uint32:
		return int64(a.Value(idx)), true
	case *array.Uint64:
		return json.Number(strconv.FormatUint(a.Value(idx), 10)), true
	case *array.Float32:
		return floatValue(float64(a.Value(idx)), 32)
	case *array.Float64:
		return floatValue(a.Value(idx), 64)
	case *array.Decimal128:
		scale := int32(0)
		if dt, ok := a.DataType().(*arrow.Decimal128Type); ok {
			scale = dt.Scale
		}
		return json.Number(a.Value(idx).ToString(scale)), true
	case *array.Decimal256:
		scale := int32(0)
		if dt, ok := a.DataType().(*arrow.Decimal256Type); ok {
			scale = dt.Scale
		}
		return json.Number(a.Value(idx).ToString(scale)), true
	case *array.String:
		return strings.Clone(a.Value(idx)), true
	case *array.LargeString:
		return strings.Clone(a.Value(idx)), true
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02"), true
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02"), true
	case *array.Timestamp:
		unit := arrow.Microsecond
		if tt, ok := a.DataType().(*arrow.TimestampType); ok {
			unit = tt.Unit
		}
		return a.Value(idx).ToTime(unit).UTC().Format(time.RFC3339Nano), true
	case *array.Struct:
		st := a.DataType().(*arrow.StructType)
		out := make(map[string]interface{}, st.NumFields())
		for i, f := range st.Fields() {
			v, ok := cellValue(a.Field(i), idx)
			if !ok {
				v = nil
			}
			out[f.Name] = v
		}
		return out, true
	case array.ListLike:
		start, end := a.ValueOffsets(idx)
		values := a.ListValues()
		list := make([]interface{}, 0, int(end-start))
		for i := int(start); i < int(end); i++ {
			if v, ok := cellValue(values, i); ok {
				list = append(list, v)
			}
		}
		return list, true
	default:
		v := arrowutil.Value(arr, idx)
		if v == nil {
			return nil, false
		}
		if s, ok := v.(string); ok {
			return strings.Clone(s), true
		}
		return fmt.Sprintf("%v", v), true
	}
}

// floatValue treats NaN and ±Inf as null: JSON can't encode them.
func floatValue(f float64, bitSize int) (interface{}, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, false
	}
	return json.Number(strconv.FormatFloat(f, 'f', -1, bitSize)), true
}

func stringValue(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}

func canonicalNumber(s string) (string, bool) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
	if !ok {
		return "", false
	}
	if r.IsInt() {
		return r.Num().String(), true
	}
	digits, ok := decimalDigits(r.Denom())
	if !ok {
		return "", false
	}
	return strings.TrimRight(r.FloatString(digits), "0"), true
}

// decimalDigits is how many fraction digits write 1/denom exactly; false when
// the fraction doesn't terminate (denom has a prime factor other than 2 or 5).
func decimalDigits(denom *big.Int) (int, bool) {
	d := new(big.Int).Set(denom)
	count := func(p int64) int {
		n, q, r := 0, new(big.Int), new(big.Int)
		for {
			q.QuoRem(d, big.NewInt(p), r)
			if r.Sign() != 0 {
				return n
			}
			d.Set(q)
			n++
		}
	}
	twos, fives := count(2), count(5)
	return max(twos, fives), d.IsInt64() && d.Int64() == 1
}

// matchKey folds a match value for correlation; Attio compares emails and
// domains case-insensitively.
func matchKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// storedValues extracts the comparable values of one attribute from a record
// returned by the API, for the mirror sweep.
func storedValues(raw json.RawMessage) []string {
	var values []map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&values); err != nil {
		return nil
	}
	var out []string
	for _, v := range values {
		for _, field := range []string{"value", "email_address", "domain", "original_phone_number", "currency_value", "target_record_id", "full_name"} {
			if x, ok := v[field]; ok && x != nil {
				out = append(out, stringValue(x))
				break
			}
		}
		for _, field := range []string{"option", "status"} {
			if m, ok := v[field].(map[string]interface{}); ok {
				out = append(out, stringValue(m["title"]))
			}
		}
	}
	return out
}
