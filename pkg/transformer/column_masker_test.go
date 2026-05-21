package transformer

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustMasker(t *testing.T, configs ...string) *ColumnMasker {
	t.Helper()
	m, err := NewColumnMasker(configs)
	require.NoError(t, err)
	return m
}

func runOne(t *testing.T, fn func(*ColumnMasker, string, arrow.DataType, string, bool) (any, bool, error), value string, inputType arrow.DataType, param string, hasParam bool) any {
	t.Helper()
	m := &ColumnMasker{
		uuidCache: map[string]string{},
		seqCache:  map[string]int64{},
	}
	out, isNull, err := fn(m, value, inputType, param, hasParam)
	require.NoError(t, err)
	require.False(t, isNull, "value should not be null")
	return out
}

func TestParseMaskSpec(t *testing.T) {
	t.Run("Valid2Parts", func(t *testing.T) {
		s, err := parseMaskSpec("password:hash")
		require.NoError(t, err)
		assert.Equal(t, "password", s.column)
		assert.Equal(t, "hash", s.algorithm)
		assert.False(t, s.hasParam)
	})

	t.Run("Valid3Parts", func(t *testing.T) {
		s, err := parseMaskSpec("phone:partial:3")
		require.NoError(t, err)
		assert.Equal(t, "partial", s.algorithm)
		assert.Equal(t, "3", s.param)
		assert.True(t, s.hasParam)
	})

	t.Run("ParamCanContainColons", func(t *testing.T) {
		s, err := parseMaskSpec("api:hmac:sec:ret:key")
		require.NoError(t, err)
		assert.Equal(t, "sec:ret:key", s.param)
	})

	t.Run("InvalidNoAlgo", func(t *testing.T) {
		_, err := parseMaskSpec("password")
		require.Error(t, err)
	})

	t.Run("UnknownAlgoRejectedAtBuild", func(t *testing.T) {
		_, err := NewColumnMasker([]string{"x:notarealalgo"})
		require.Error(t, err)
	})

	t.Run("AlgorithmCaseInsensitive", func(t *testing.T) {
		m, err := NewColumnMasker([]string{"x:HASH"})
		require.NoError(t, err)
		assert.True(t, m.HasMasks())
	})

	t.Run("WhitespacePreservedInColumnName", func(t *testing.T) {
		s, err := parseMaskSpec(" email :hash")
		require.NoError(t, err)
		assert.Equal(t, " email ", s.column)
		assert.Equal(t, "hash", s.algorithm)
	})
}

func TestStringAlgorithms(t *testing.T) {
	t.Run("HashSHA256", func(t *testing.T) {
		got := runOne(t, algoSHA256, "hello", arrow.BinaryTypes.String, "", false)
		expected := sha256.Sum256([]byte("hello"))
		assert.Equal(t, hex.EncodeToString(expected[:]), got)
	})

	t.Run("MD5Length32", func(t *testing.T) {
		got := runOne(t, algoMD5, "hello", arrow.BinaryTypes.String, "", false).(string)
		assert.Len(t, got, 32)
	})

	t.Run("HMACDeterministicWithSameKey", func(t *testing.T) {
		a := runOne(t, algoHMAC, "abc", arrow.BinaryTypes.String, "secret", true).(string)
		b := runOne(t, algoHMAC, "abc", arrow.BinaryTypes.String, "secret", true).(string)
		assert.Equal(t, a, b)
		c := runOne(t, algoHMAC, "abc", arrow.BinaryTypes.String, "different", true).(string)
		assert.NotEqual(t, a, c)
	})

	t.Run("Email", func(t *testing.T) {
		assert.Equal(t, "j******e@example.com", runOne(t, algoEmail, "john.doe@example.com", arrow.BinaryTypes.String, "", false))
		assert.Equal(t, "**@test.com", runOne(t, algoEmail, "ab@test.com", arrow.BinaryTypes.String, "", false))
	})

	t.Run("Phone", func(t *testing.T) {
		assert.Equal(t, "555-***-****", runOne(t, algoPhone, "555-123-4567", arrow.BinaryTypes.String, "", false))
		assert.Equal(t, "*****", runOne(t, algoPhone, "12345", arrow.BinaryTypes.String, "", false))
	})

	t.Run("CreditCard", func(t *testing.T) {
		assert.Equal(t, "************1111", runOne(t, algoCreditCard, "4111-1111-1111-1111", arrow.BinaryTypes.String, "", false))
	})

	t.Run("SSN", func(t *testing.T) {
		assert.Equal(t, "***-**-6789", runOne(t, algoSSN, "123-45-6789", arrow.BinaryTypes.String, "", false))
	})

	t.Run("Redact", func(t *testing.T) {
		assert.Equal(t, "REDACTED", runOne(t, algoRedact, "anything", arrow.BinaryTypes.String, "", false))
	})

	t.Run("Stars", func(t *testing.T) {
		assert.Equal(t, "****", runOne(t, algoStars, "test", arrow.BinaryTypes.String, "", false))
	})

	t.Run("FixedDefault", func(t *testing.T) {
		assert.Equal(t, "MASKED", runOne(t, algoFixed, "x", arrow.BinaryTypes.String, "", false))
	})

	t.Run("FixedWithParam", func(t *testing.T) {
		assert.Equal(t, "CUSTOM", runOne(t, algoFixed, "x", arrow.BinaryTypes.String, "CUSTOM", true))
	})

	t.Run("PartialDefault", func(t *testing.T) {
		assert.Equal(t, "Jo****an", runOne(t, algoPartial, "Jonathan", arrow.BinaryTypes.String, "", false))
	})

	t.Run("FirstLetter", func(t *testing.T) {
		assert.Equal(t, "A****", runOne(t, algoFirstLetter, "Alice", arrow.BinaryTypes.String, "", false))
	})

	t.Run("MonthYear", func(t *testing.T) {
		assert.Equal(t, "2024-03", runOne(t, algoMonthYear, "2024-03-15", arrow.BinaryTypes.String, "", false))
	})

	t.Run("RangeOutputsString", func(t *testing.T) {
		assert.Equal(t, "40000-50000", runOne(t, algoRange, "45000", arrow.PrimitiveTypes.Int64, "10000", true))
	})
}

func TestNumericAlgorithms(t *testing.T) {
	t.Run("RoundReturnsInt64ForIntegerInput", func(t *testing.T) {
		v := runOne(t, algoRound, "52300", arrow.PrimitiveTypes.Int64, "5000", true)
		i, ok := v.(int64)
		require.True(t, ok, "round must return int64")
		assert.Equal(t, int64(50000), i)
	})

	t.Run("RoundReturnsInt64ForFloatInput", func(t *testing.T) {
		v := runOne(t, algoRound, "52300.5", arrow.PrimitiveTypes.Float64, "5000", true)
		i, ok := v.(int64)
		require.True(t, ok, "round must return int64 (Python ingestr parity)")
		assert.Equal(t, int64(50000), i)
	})

	t.Run("RoundReturnsNullOnParseFailure", func(t *testing.T) {
		m := &ColumnMasker{uuidCache: map[string]string{}, seqCache: map[string]int64{}}
		_, isNull, err := algoRound(m, "abc", arrow.BinaryTypes.String, "10", true)
		require.NoError(t, err)
		assert.True(t, isNull)
	})

	t.Run("YearOnlyReturnsInt64", func(t *testing.T) {
		v := runOne(t, algoYearOnly, "2024-03-15", arrow.BinaryTypes.String, "", false)
		i, ok := v.(int64)
		require.True(t, ok, "year_only must return int64")
		assert.Equal(t, int64(2024), i)
	})

	t.Run("YearOnlyReturnsNullOnParseFailure", func(t *testing.T) {
		m := &ColumnMasker{uuidCache: map[string]string{}, seqCache: map[string]int64{}}
		_, isNull, err := algoYearOnly(m, "not a date", arrow.BinaryTypes.String, "", false)
		require.NoError(t, err)
		assert.True(t, isNull)
	})

	t.Run("SequentialReturnsInt64", func(t *testing.T) {
		m := mustMasker(t, "id:sequential")
		fn := algorithms["sequential"].apply
		first, _, _ := fn(m, "x", arrow.BinaryTypes.String, "", false)
		second, _, _ := fn(m, "y", arrow.BinaryTypes.String, "", false)
		repeat, _, _ := fn(m, "x", arrow.BinaryTypes.String, "", false)
		assert.Equal(t, int64(1), first)
		assert.Equal(t, int64(2), second)
		assert.Equal(t, int64(1), repeat)
	})

	t.Run("NoisePreservesIntForIntegerInput", func(t *testing.T) {
		v := runOne(t, algoNoise, "100", arrow.PrimitiveTypes.Int64, "0.0", true)
		_, ok := v.(int64)
		assert.True(t, ok, "noise on integer input must return int64")
	})

	t.Run("NoisePreservesFloatForFloatInput", func(t *testing.T) {
		v := runOne(t, algoNoise, "100.5", arrow.PrimitiveTypes.Float64, "0.0", true)
		_, ok := v.(float64)
		assert.True(t, ok, "noise on float input must return float64")
	})

	t.Run("NoiseReturnsNullOnParseFailure", func(t *testing.T) {
		m := &ColumnMasker{uuidCache: map[string]string{}, seqCache: map[string]int64{}}
		_, isNull, err := algoNoise(m, "n/a", arrow.BinaryTypes.String, "0.1", true)
		require.NoError(t, err)
		assert.True(t, isNull)
	})

	t.Run("RangeReturnsNullOnParseFailure", func(t *testing.T) {
		m := &ColumnMasker{uuidCache: map[string]string{}, seqCache: map[string]int64{}}
		_, isNull, err := algoRange(m, "N/A", arrow.BinaryTypes.String, "100", true)
		require.NoError(t, err)
		assert.True(t, isNull)
	})

	t.Run("DateShiftReturnsNullOnParseFailure", func(t *testing.T) {
		m := &ColumnMasker{uuidCache: map[string]string{}, seqCache: map[string]int64{}}
		_, isNull, err := algoDateShift(m, "not a date", arrow.BinaryTypes.String, "30", true)
		require.NoError(t, err)
		assert.True(t, isNull)
	})

	t.Run("MonthYearReturnsNullOnParseFailure", func(t *testing.T) {
		m := &ColumnMasker{uuidCache: map[string]string{}, seqCache: map[string]int64{}}
		_, isNull, err := algoMonthYear(m, "not a date", arrow.BinaryTypes.String, "", false)
		require.NoError(t, err)
		assert.True(t, isNull)
	})

	t.Run("RandomIntReturnsNullOnParseFailure", func(t *testing.T) {
		m := &ColumnMasker{uuidCache: map[string]string{}, seqCache: map[string]int64{}}
		_, isNull, err := algoRandom(m, "not a number", arrow.PrimitiveTypes.Int64, "", false)
		require.NoError(t, err)
		assert.True(t, isNull)
	})

	t.Run("RandomFloatReturnsNullOnParseFailure", func(t *testing.T) {
		m := &ColumnMasker{uuidCache: map[string]string{}, seqCache: map[string]int64{}}
		_, isNull, err := algoRandom(m, "not a number", arrow.PrimitiveTypes.Float64, "", false)
		require.NoError(t, err)
		assert.True(t, isNull)
	})
}

func TestOutputTypePerAlgorithm(t *testing.T) {
	cases := []struct {
		algo        string
		input       arrow.DataType
		expected    arrow.Type
		description string
	}{
		{"hash", arrow.PrimitiveTypes.Int64, arrow.STRING, "hash always string"},
		{"redact", arrow.PrimitiveTypes.Float64, arrow.STRING, "redact always string"},
		{"round", arrow.PrimitiveTypes.Int64, arrow.INT64, "round always int64"},
		{"round", arrow.PrimitiveTypes.Float64, arrow.INT64, "round always int64 even for float"},
		{"year_only", arrow.BinaryTypes.String, arrow.INT64, "year_only always int"},
		{"sequential", arrow.BinaryTypes.String, arrow.INT64, "sequential always int"},
		{"noise", arrow.PrimitiveTypes.Int64, arrow.INT64, "noise preserves int"},
		{"noise", arrow.PrimitiveTypes.Float64, arrow.FLOAT64, "noise preserves float"},
		{"random", arrow.PrimitiveTypes.Int32, arrow.INT64, "random preserves numeric (int->int64)"},
		{"random", arrow.BinaryTypes.String, arrow.STRING, "random on string stays string"},
		{"date_shift", &arrow.Date32Type{}, arrow.DATE32, "date_shift preserves Date32"},
		{"date_shift", arrow.BinaryTypes.String, arrow.STRING, "date_shift on string stays string"},
		{"month_year", &arrow.Date32Type{}, arrow.STRING, "month_year always string"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			alg, ok := algorithms[tc.algo]
			require.True(t, ok)
			out := alg.outputType(tc.input)
			assert.Equal(t, tc.expected, out.ID())
		})
	}
}

func TestColumnMaskerTransformPreservesNumericType(t *testing.T) {
	pool := memory.NewGoAllocator()
	m := mustMasker(t, "salary:round:5000", "year:year_only")

	fields := []arrow.Field{
		{Name: "salary", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		{Name: "year", Type: arrow.BinaryTypes.String, Nullable: true},
	}
	inputSchema := arrow.NewSchema(fields, nil)

	salB := array.NewFloat64Builder(pool)
	defer salB.Release()
	salB.AppendValues([]float64{52300, 98000}, nil)

	yrB := array.NewStringBuilder(pool)
	defer yrB.Release()
	yrB.AppendValues([]string{"2024-03-15", "1990-01-01"}, nil)

	cols := []arrow.Array{salB.NewArray(), yrB.NewArray()}
	batch := array.NewRecordBatch(inputSchema, cols, 2)
	for _, c := range cols {
		c.Release()
	}
	defer batch.Release()

	out, err := m.Transform(batch)
	require.NoError(t, err)
	defer out.Release()

	// round always returns int64 (Python ingestr parity), even for float input
	assert.Equal(t, arrow.INT64, out.Schema().Field(0).Type.ID())
	salaries := out.Column(0).(*array.Int64)
	assert.Equal(t, int64(50000), salaries.Value(0))

	// year should be int64
	assert.Equal(t, arrow.INT64, out.Schema().Field(1).Type.ID())
	years := out.Column(1).(*array.Int64)
	assert.Equal(t, int64(2024), years.Value(0))
	assert.Equal(t, int64(1990), years.Value(1))
}

func TestColumnMaskerNullPreservation(t *testing.T) {
	pool := memory.NewGoAllocator()
	m := mustMasker(t, "x:redact")

	fields := []arrow.Field{{Name: "x", Type: arrow.BinaryTypes.String, Nullable: true}}
	b := array.NewStringBuilder(pool)
	defer b.Release()
	b.Append("hello")
	b.AppendNull()
	col := b.NewArray()
	batch := array.NewRecordBatch(arrow.NewSchema(fields, nil), []arrow.Array{col}, 2)
	col.Release()
	defer batch.Release()

	out, err := m.Transform(batch)
	require.NoError(t, err)
	defer out.Release()

	res := out.Column(0).(*array.String)
	assert.Equal(t, "REDACTED", res.Value(0))
	assert.True(t, res.IsNull(1))
}

func TestHMACRequiresKey(t *testing.T) {
	t.Run("RejectedWithoutKey", func(t *testing.T) {
		_, err := NewColumnMasker([]string{"name:hmac"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hmac")
	})

	t.Run("RejectedWithEmptyKey", func(t *testing.T) {
		_, err := NewColumnMasker([]string{"name:hmac:"})
		require.Error(t, err)
	})

	t.Run("AcceptedWithKey", func(t *testing.T) {
		m, err := NewColumnMasker([]string{"name:hmac:secret123"})
		require.NoError(t, err)
		assert.True(t, m.HasMasks())
	})
}

func TestValidateColumns(t *testing.T) {
	src := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id"}, {Name: "email"}, {Name: "password"},
		},
	}

	t.Run("AllColumnsPresent", func(t *testing.T) {
		m := mustMasker(t, "email:hash", "password:redact")
		require.NoError(t, m.ValidateColumns(src))
	})

	t.Run("TypoedColumnRejected", func(t *testing.T) {
		m := mustMasker(t, "emial:hash")
		err := m.ValidateColumns(src)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "emial")
	})

	t.Run("CaseMismatchRejected", func(t *testing.T) {
		m := mustMasker(t, "Email:hash")
		err := m.ValidateColumns(src)
		require.Error(t, err)
	})

	t.Run("NoMasksConfigured", func(t *testing.T) {
		m, err := NewColumnMasker(nil)
		require.NoError(t, err)
		require.NoError(t, m.ValidateColumns(src))
	})

	t.Run("NilSchema", func(t *testing.T) {
		m := mustMasker(t, "email:hash")
		require.NoError(t, m.ValidateColumns(nil))
	})
}

func TestColumnNameMatchIsCaseSensitive(t *testing.T) {
	pool := memory.NewGoAllocator()
	m := mustMasker(t, "Email:redact")

	fields := []arrow.Field{
		{Name: "email", Type: arrow.BinaryTypes.String, Nullable: true},
	}
	b := array.NewStringBuilder(pool)
	defer b.Release()
	b.Append("alice@example.com")
	col := b.NewArray()
	batch := array.NewRecordBatch(arrow.NewSchema(fields, nil), []arrow.Array{col}, 1)
	col.Release()
	defer batch.Release()

	out, err := m.Transform(batch)
	require.NoError(t, err)
	defer out.Release()

	got := out.Column(0).(*array.String).Value(0)
	assert.Equal(t, "alice@example.com", got)
}

func TestEmptyMaskListPassesThrough(t *testing.T) {
	m, err := NewColumnMasker(nil)
	require.NoError(t, err)
	assert.False(t, m.HasMasks())
}

func TestStarsPreservesLength(t *testing.T) {
	got := runOne(t, algoStars, "abcdef", arrow.BinaryTypes.String, "", false)
	assert.Equal(t, strings.Repeat("*", 6), got)
}

func TestMultiByteUTF8Inputs(t *testing.T) {
	// Inputs containing accented letters, CJK, and emoji must not panic
	// and must mask by rune count, not byte count.

	t.Run("PartialOnAccented", func(t *testing.T) {
		// "Jönäthän" — 8 runes, 11 bytes. partial:2 → "Jö****än"
		got := runOne(t, algoPartial, "Jönäthän", arrow.BinaryTypes.String, "2", true)
		assert.Equal(t, "Jö****än", got)
	})

	t.Run("PartialOnCJK", func(t *testing.T) {
		// 6 runes, 18 bytes. partial:1 → first rune + 4 stars + last rune
		got := runOne(t, algoPartial, "東京都新宿区", arrow.BinaryTypes.String, "1", true)
		assert.Equal(t, "東****区", got)
	})

	t.Run("EmailWithAccents", func(t *testing.T) {
		// "café@x.com" — local "café" has 4 runes, 5 bytes. Output: c**é@x.com
		got := runOne(t, algoEmail, "café@x.com", arrow.BinaryTypes.String, "", false)
		assert.Equal(t, "c**é@x.com", got)
	})

	t.Run("FirstLetterCJK", func(t *testing.T) {
		got := runOne(t, algoFirstLetter, "東京", arrow.BinaryTypes.String, "", false)
		assert.Equal(t, "東*", got)
	})

	t.Run("StarsByRuneCount", func(t *testing.T) {
		// "café" — 4 runes, 5 bytes. Should be 4 stars, not 5.
		got := runOne(t, algoStars, "café", arrow.BinaryTypes.String, "", false)
		assert.Equal(t, "****", got)
	})

	t.Run("StarsOnEmoji", func(t *testing.T) {
		// "😊😊" — 2 runes, 8 bytes. Should be 2 stars, not 8.
		got := runOne(t, algoStars, "😊😊", arrow.BinaryTypes.String, "", false)
		assert.Equal(t, "**", got)
	})
}

func TestTimestampForUnit(t *testing.T) {
	// Verifies algoDateShift's helper scales correctly per arrow.TimeUnit,
	// so a TIMESTAMP[ns] column doesn't get a value off by 1000× because the
	// helper assumed microseconds.
	parsed, err := time.Parse(time.RFC3339, "2024-03-15T10:30:00Z")
	require.NoError(t, err)

	cases := []struct {
		name     string
		unit     arrow.TimeUnit
		expected arrow.Timestamp
	}{
		{"second", arrow.Second, arrow.Timestamp(parsed.Unix())},
		{"millisecond", arrow.Millisecond, arrow.Timestamp(parsed.UnixMilli())},
		{"microsecond", arrow.Microsecond, arrow.Timestamp(parsed.UnixMicro())},
		{"nanosecond", arrow.Nanosecond, arrow.Timestamp(parsed.UnixNano())},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, timestampForUnit(parsed, tc.unit))
		})
	}
}

func TestUUIDIsConsistentWithinSession(t *testing.T) {
	m := mustMasker(t, "id:uuid")
	fn := algorithms["uuid"].apply
	a, _, _ := fn(m, "CUST001", arrow.BinaryTypes.String, "", false)
	b, _, _ := fn(m, "CUST001", arrow.BinaryTypes.String, "", false)
	c, _, _ := fn(m, "CUST002", arrow.BinaryTypes.String, "", false)
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
}

func TestPhoneWithCountryPrefix(t *testing.T) {
	got := runOne(t, algoPhone, "+1-555-123-4567", arrow.BinaryTypes.String, "", false).(string)
	assert.Contains(t, got, "***")
}

func TestCreditCardEdgeCases(t *testing.T) {
	t.Run("NoDashes", func(t *testing.T) {
		assert.Equal(t, "************1111", runOne(t, algoCreditCard, "4111111111111111", arrow.BinaryTypes.String, "", false))
	})
	t.Run("ShortInputAllStars", func(t *testing.T) {
		assert.Equal(t, "****", runOne(t, algoCreditCard, "1234", arrow.BinaryTypes.String, "", false))
	})
}

func TestSSNEdgeCases(t *testing.T) {
	t.Run("NoDashes", func(t *testing.T) {
		assert.Equal(t, "***-**-6789", runOne(t, algoSSN, "123456789", arrow.BinaryTypes.String, "", false))
	})
	t.Run("ShortInputAllStars", func(t *testing.T) {
		assert.Equal(t, "*****", runOne(t, algoSSN, "12345", arrow.BinaryTypes.String, "", false))
	})
}

func TestPartialMaskEdgeCases(t *testing.T) {
	t.Run("KeepOne", func(t *testing.T) {
		assert.Equal(t, "t**t", runOne(t, algoPartial, "test", arrow.BinaryTypes.String, "1", true))
	})
	t.Run("KeepEqualsHalfReturnsAllStars", func(t *testing.T) {
		assert.Equal(t, "**", runOne(t, algoPartial, "ab", arrow.BinaryTypes.String, "2", true))
	})
}

func TestFirstLetterSingleCharacter(t *testing.T) {
	assert.Equal(t, "B", runOne(t, algoFirstLetter, "B", arrow.BinaryTypes.String, "", false))
}

func TestRoundAdditionalInputs(t *testing.T) {
	t.Run("ThirtyFourTo10", func(t *testing.T) {
		assert.Equal(t, int64(30), runOne(t, algoRound, "34", arrow.PrimitiveTypes.Int64, "10", true))
	})
	t.Run("FloatRoundsHalfUp", func(t *testing.T) {
		assert.Equal(t, int64(40), runOne(t, algoRound, "37.5", arrow.PrimitiveTypes.Float64, "10", true))
	})
}

func TestRangeAdditionalInputs(t *testing.T) {
	assert.Equal(t, "200-300", runOne(t, algoRange, "234", arrow.PrimitiveTypes.Int64, "100", true))
}
