package iceberg

import (
	"testing"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	"github.com/stretchr/testify/require"
)

func TestEncodeRowKeyUnambiguous(t *testing.T) {
	a, err := encodeRowKey([]any{"ab", "c"})
	require.NoError(t, err)
	b, err := encodeRowKey([]any{"a", "bc"})
	require.NoError(t, err)
	require.NotEqual(t, a, b, "composite string keys must not collide")

	c, err := encodeRowKey([]any{int64(12), "3"})
	require.NoError(t, err)
	d, err := encodeRowKey([]any{int64(1), "23"})
	require.NoError(t, err)
	require.NotEqual(t, c, d)

	_, err = encodeRowKey([]any{nil})
	require.ErrorContains(t, err, "NULL")
}

func TestValuesEqual(t *testing.T) {
	require.True(t, valuesEqual(nil, nil))
	require.False(t, valuesEqual(nil, int64(1)))
	require.False(t, valuesEqual(int64(1), nil))
	require.True(t, valuesEqual(int64(5), int64(5)))
	require.False(t, valuesEqual(int64(5), float64(5)))
	require.True(t, valuesEqual([]byte{1, 2}, []byte{1, 2}))
	require.False(t, valuesEqual([]byte{1, 2}, []byte{1, 3}))
	require.True(t, valuesEqual([]any{int64(1), "a"}, []any{int64(1), "a"}))
	require.False(t, valuesEqual([]any{int64(1)}, []any{int64(1), "a"}))
	require.True(t, valuesEqual(decimalVal("1.5"), decimalVal("1.5")))
}

func TestCompareOrdered(t *testing.T) {
	cmp, ok := compareOrdered(int64(1), int64(2))
	require.True(t, ok)
	require.Equal(t, -1, cmp)

	cmp, ok = compareOrdered(2.5, int64(2))
	require.True(t, ok)
	require.Equal(t, 1, cmp)

	cmp, ok = compareOrdered("a", "b")
	require.True(t, ok)
	require.Equal(t, -1, cmp)

	cmp, ok = compareOrdered(decimalVal("10"), decimalVal("9.5"))
	require.True(t, ok)
	require.Equal(t, 1, cmp, "decimals must compare numerically, not lexically")

	cmp, ok = compareOrdered(nil, int64(1))
	require.True(t, ok)
	require.Equal(t, -1, cmp)

	_, ok = compareOrdered("a", int64(1))
	require.False(t, ok)
}

func TestNormalizeDecimalString(t *testing.T) {
	require.Equal(t, "1.5", normalizeDecimalString("1.50"))
	require.Equal(t, "1", normalizeDecimalString("1.000"))
	require.Equal(t, "0", normalizeDecimalString("0.0"))
	require.Equal(t, "-2.25", normalizeDecimalString("-2.250"))
	require.Equal(t, "42", normalizeDecimalString("42"))
}

func TestNormalizeBoundValue(t *testing.T) {
	tsField := iceberggo.NestedField{Name: "ts", Type: iceberggo.TimestampTzType{}}
	dateField := iceberggo.NestedField{Name: "d", Type: iceberggo.DateType{}}
	timeField := iceberggo.NestedField{Name: "t", Type: iceberggo.TimeType{}}
	intField := iceberggo.NestedField{Name: "i", Type: iceberggo.Int64Type{}}

	at := time.Date(2026, 5, 1, 12, 30, 0, 123456000, time.UTC)
	got, err := normalizeBoundValue(tsField, at)
	require.NoError(t, err)
	require.Equal(t, at.UnixMicro(), got)

	got, err = normalizeBoundValue(dateField, "2026-05-01")
	require.NoError(t, err)
	require.Equal(t, at.Truncate(24*time.Hour).Unix()/86400, got)

	got, err = normalizeBoundValue(dateField, at)
	require.NoError(t, err)
	require.Equal(t, at.Truncate(24*time.Hour).Unix()/86400, got)

	got, err = normalizeBoundValue(intField, 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), got)

	got, err = normalizeBoundValue(intField, "42")
	require.NoError(t, err)
	require.Equal(t, int64(42), got)

	got, err = normalizeBoundValue(tsField, "2026-05-01T12:30:00Z")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC).UnixMicro(), got)

	got, err = normalizeBoundValue(timeField, at)
	require.NoError(t, err)
	require.Equal(t, int64(45_000_123_456), got)

	got, err = normalizeBoundValue(timeField, "12:30:00.123456")
	require.NoError(t, err)
	require.Equal(t, int64(45_000_123_456), got)

	_, err = normalizeBoundValue(timeField, "not-a-time")
	require.ErrorContains(t, err, `invalid time bound "not-a-time"`)

	// The strategy layer passes interval bounds as *time.Time; ensure the
	// pointer is dereferenced rather than rejected as an unsupported type.
	got, err = normalizeBoundValue(tsField, &at)
	require.NoError(t, err)
	require.Equal(t, at.UnixMicro(), got)

	var nilTime *time.Time
	_, err = normalizeBoundValue(tsField, nilTime)
	require.ErrorContains(t, err, "nil")

	_, err = normalizeBoundValue(intField, nil)
	require.ErrorContains(t, err, "nil")
}
