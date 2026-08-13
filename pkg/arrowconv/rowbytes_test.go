package arrowconv

import (
	"encoding/json"
	"testing"
)

func TestRowBytesMatchesMarshalMany(t *testing.T) {
	vals := []interface{}{
		// floats (as JSON decoding produces)
		float64(0), float64(3.14), float64(125000.75), float64(1000000),
		float64(12345), float64(1e6), float64(1e21), float64(1e-7), float64(0.0001),
		float64(-1000000), float64(123456789.123), float64(1.5e300),
		// ints (Go literals)
		0, 7, -1234567, 1000000, 9223372036854775807,
		// strings, bool, nil
		"hello", "", true, false, nil,
		// nested
		[]interface{}{1, 2.5, "x", true},
		map[string]interface{}{"a": 1000000.0, "b": []interface{}{1e6, "y"}},
	}
	mism := 0
	for _, v := range vals {
		item := map[string]interface{}{"k": v}
		raw, _ := json.Marshal(item)
		got := RowBytes(item)
		if got != int64(len(raw)) {
			mism++
			t.Errorf("MISMATCH %-28v RowBytes=%d json=%d  json=%q", v, got, len(raw), string(raw))
		}
	}
	t.Logf("total mismatches: %d / %d", mism, len(vals))
}
