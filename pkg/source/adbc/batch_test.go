package adbc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeNestedValue(t *testing.T) {
	t.Parallel()

	one, x, v := int32(1), "x", 1.5
	arr := []int32{1, 2}
	type inner struct {
		V *float64 `arrow:"v"`
	}
	type row struct {
		A      *int32    `arrow:"a"`
		BName  *string   `arrow:"b name"`
		Nested *inner    `arrow:"nested"`
		Arr    *[]int32  `arrow:"arr"`
		Null   *string   `arrow:"null"`
		TS     time.Time `arrow:"ts"`
	}
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, map[string]interface{}{
		"a": int32(1), "b name": "x", "nested": map[string]interface{}{"v": 1.5},
		"arr": []interface{}{int32(1), int32(2)}, "null": nil, "ts": ts,
	}, NormalizeNestedValue(row{A: &one, BName: &x, Nested: &inner{V: &v}, Arr: &arr, TS: ts}))

	assert.Equal(t, map[string]interface{}{"7": "seven"}, NormalizeNestedValue(map[int32]string{7: "seven"}))
	assert.Equal(t,
		[]interface{}{map[string]interface{}{"v": 1.5}, map[string]interface{}{"v": nil}},
		NormalizeNestedValue([]inner{{V: &v}, {}}))

	for _, passthrough := range []interface{}{nil, "s", []byte("b"), []int32{1}, ts, int64(3)} {
		assert.Equal(t, passthrough, NormalizeNestedValue(passthrough))
	}
}
