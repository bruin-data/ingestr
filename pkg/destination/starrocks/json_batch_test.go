package starrocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestBoundedJSONStringsMatchStandardEncoder(t *testing.T) {
	values := []string{"", "plain text", "emoji 😀 and 中文", "\u2028\u2029", "\xff\xfe\xed\xa0\x80", "\ufffd"}
	for i := range 256 {
		values = append(values, string([]byte{byte(i)}))
	}
	for _, value := range values {
		want, err := json.Marshal(value)
		require.NoError(t, err)
		for _, limit := range []int{len(want) - 1, len(want), len(want) + 1} {
			b := boundedJSONBuffer{limit: limit}
			b.writeQuoted(value)
			require.LessOrEqual(t, cap(b.data), limit)
			if limit < len(want) {
				require.ErrorIs(t, b.err, errJSONBodyLimit)
			} else {
				require.NoError(t, b.err)
				require.Equal(t, string(want), string(b.data))
			}
		}
	}
}

func stringJSONBatch(mem memory.Allocator, values []string) arrow.RecordBatch {
	builder := array.NewStringBuilder(mem)
	defer builder.Release()
	builder.AppendValues(values, nil)
	arr := builder.NewStringArray()
	defer arr.Release()
	return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.BinaryTypes.String}}, nil), []arrow.Array{arr}, int64(len(values)))
}

func TestJSONBatchEncoderSplitsAtExactByteLimit(t *testing.T) {
	values := []string{"first", "\"\n\x00<>&\u2028\xff", strings.Repeat("wide", 500), "last"}
	rec := stringJSONBatch(memory.DefaultAllocator, values)
	defer rec.Release()
	for _, limit := range []int{2014, 2027, 4096, 8192} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			encoder := newJSONBatchEncoder(rec, limit)
			var got []string
			for {
				body, rows, err := encoder.next(t.Context())
				require.NoError(t, err)
				if rows == 0 {
					break
				}
				require.LessOrEqual(t, len(body), limit)
				require.LessOrEqual(t, cap(encoder.body.data), limit)
				require.LessOrEqual(t, cap(encoder.row.data), limit-2)
				var decoded []struct{ Value string }
				require.NoError(t, json.Unmarshal(body, &decoded))
				require.Len(t, decoded, int(rows))
				for _, row := range decoded {
					got = append(got, row.Value)
				}
			}
			require.Equal(t, []string{"first", "\"\n\x00<>&\u2028\ufffd", values[2], "last"}, got)
		})
	}
}

func TestJSONBatchEncoderRejectsOversizedRowWithoutGrowingPastLimit(t *testing.T) {
	for _, value := range []string{strings.Repeat("x", 1024*1024), strings.Repeat("\x00", 256)} {
		rec := stringJSONBatch(memory.DefaultAllocator, []string{value})
		encoder := newJSONBatchEncoder(rec, 1024)
		_, _, err := encoder.next(t.Context())
		require.ErrorContains(t, err, "single row (row 1) exceeds the 1024-byte Stream Load limit")
		require.LessOrEqual(t, cap(encoder.body.data), 1024)
		require.LessOrEqual(t, cap(encoder.row.data), 1022)
		rec.Release()
	}
}

func TestJSONBatchEncoderDuplicateFieldsKeepLastValue(t *testing.T) {
	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.PrimitiveTypes.Int64},
		{Name: "payload", Type: arrow.BinaryTypes.Binary},
		{Name: "value", Type: arrow.PrimitiveTypes.Int64},
		{Name: "payload", Type: arrow.BinaryTypes.String},
	}, nil))
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).Append(1)
	builder.Field(1).(*array.BinaryBuilder).Append([]byte{0xff})
	builder.Field(2).(*array.Int64Builder).Append(2)
	builder.Field(3).(*array.StringBuilder).Append("last")
	rec := builder.NewRecordBatch()
	defer rec.Release()
	encoder := newJSONBatchEncoder(rec, 128)
	body, n, err := encoder.next(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.Equal(t, `[{"payload":"last","value":2}]`, string(body))
	require.Equal(t, []string{"payload"}, encoder.binaryColumns())
}

func TestJSONBatchEncoderBoundsEncodedColumnNames(t *testing.T) {
	for name, names := range map[string][]string{
		"wide":       {strings.Repeat("x", 1<<20)},
		"escaped":    {strings.Repeat("\x00", 10)},
		"cumulative": {"first", "second", "third", "fourth", "fifth", "sixth"},
	} {
		t.Run(name, func(t *testing.T) {
			builder := array.NewInt64Builder(memory.DefaultAllocator)
			defer builder.Release()
			builder.Append(1)
			arr := builder.NewInt64Array()
			defer arr.Release()
			fields := make([]arrow.Field, len(names))
			columns := make([]arrow.Array, len(names))
			for i, name := range names {
				fields[i] = arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int64}
				columns[i] = arr
			}
			rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), columns, 1)
			defer rec.Release()
			encoder := newJSONBatchEncoder(rec, 32)
			require.ErrorIs(t, encoder.initErr, errJSONBodyLimit)
			body, n, err := encoder.next(t.Context())
			require.ErrorContains(t, err, "column names cannot fit in a single row")
			require.Nil(t, body)
			require.Zero(t, n)
			require.Empty(t, encoder.keys)
		})
	}
}

func TestJSONBatchEncoderEmptyRowsAndBatches(t *testing.T) {
	for _, rows := range []int64{0, 1} {
		rec := array.NewRecordBatch(arrow.NewSchema(nil, nil), nil, rows)
		for _, limit := range []int{0, 1, 2, 3, 4} {
			encoder := newJSONBatchEncoder(rec, limit)
			body, n, err := encoder.next(t.Context())
			if rows == 0 {
				require.NoError(t, err)
				require.Nil(t, body)
				require.Zero(t, n)
			} else if limit < 4 {
				require.ErrorContains(t, err, "single row")
			} else {
				require.NoError(t, err)
				require.Equal(t, "[{}]", string(body))
				require.EqualValues(t, 1, n)
			}
		}
		rec.Release()
	}
}

func TestJSONBatchEncoderCancellation(t *testing.T) {
	rec := stringJSONBatch(memory.DefaultAllocator, []string{"first", "second"})
	defer rec.Release()
	encoder := newJSONBatchEncoder(rec, 20)
	ctx, cancel := context.WithCancel(t.Context())
	_, n, err := encoder.next(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	cancel()
	_, _, err = encoder.next(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 1, encoder.nextRow)
}

func TestJSONBatchEncoderRawJSONExtension(t *testing.T) {
	values := []string{` { "nested": [ 1, { "a": "escaped\" \" <>&" } ] } `, "[\"\u2028\u2029\", \"\xff\"]", `"not json`, "null"}
	builder := schema.NewJSONBuilder(memory.DefaultAllocator)
	defer builder.Release()
	for _, value := range values {
		builder.Append(value)
	}
	arr := builder.NewArray()
	defer arr.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "json", Type: schema.JSONArrowType}}, nil), []arrow.Array{arr}, int64(len(values)))
	defer rec.Release()
	want := make([]map[string]interface{}, len(values))
	for i := range values {
		want[i] = map[string]interface{}{"json": extractValue(arr, i)}
	}
	expected, err := json.Marshal(want)
	require.NoError(t, err)
	encoder := newJSONBatchEncoder(rec, 4096)
	body, n, err := encoder.next(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, len(values), n)
	require.Equal(t, string(expected), string(body))
}

func TestLoadRecordSplitsBinaryRowsAndStopsAfterFailure(t *testing.T) {
	previous := maxStreamLoadJSONBytes
	maxStreamLoadJSONBytes = 32
	t.Cleanup(func() { maxStreamLoadJSONBytes = previous })
	builder := array.NewBinaryBuilder(memory.DefaultAllocator, arrow.BinaryTypes.Binary)
	defer builder.Release()
	builder.AppendValues([][]byte{{0xff, 0x00, 0xfe}, {0x01, 0x02, 0x03}, {0x04, 0x05, 0x06}}, nil)
	arr := builder.NewBinaryArray()
	defer arr.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "binary", Type: arrow.BinaryTypes.Binary}}, nil), []arrow.Array{arr}, 3)
	defer rec.Release()
	for _, failAt := range []int{0, 2} {
		t.Run(strconv.Itoa(failAt), func(t *testing.T) {
			loadErr := errors.New("load failed")
			var loaded []string
			labels := map[string]bool{}
			d := &StarRocksDestination{loader: &streamLoader{endpoint: "http://starrocks", client: &http.Client{Transport: parallelLoadTransport(func(r *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.EqualValues(t, len(body), r.ContentLength)
				require.LessOrEqual(t, len(body), maxStreamLoadJSONBytes)
				require.Equal(t, "`binary`, `binary`=to_binary(`binary`, 'hex')", r.Header.Get("columns"))
				label := r.Header.Get("label")
				require.NotEmpty(t, label)
				require.False(t, labels[label])
				labels[label] = true
				loaded = append(loaded, string(body))
				if failAt == len(loaded) {
					return nil, loadErr
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Status":"Success","NumberLoadedRows":1,"NumberTotalRows":1}`))}, nil
			})}}}
			n, err := d.loadRecord(t.Context(), "db", "table", rec, "")
			if failAt > 0 {
				require.ErrorIs(t, err, loadErr)
				require.EqualValues(t, failAt-1, n)
				require.Len(t, loaded, failAt)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 3, n)
				require.Equal(t, []string{`[{"binary":"ff00fe"}]`, `[{"binary":"010203"}]`, `[{"binary":"040506"}]`}, loaded)
			}
		})
	}
}

func BenchmarkJSONBatchEncoderWideRows(b *testing.B) {
	for _, rowBytes := range []int{240, 50_000} {
		b.Run(fmt.Sprint(rowBytes), func(b *testing.B) {
			values := make([]string, 500)
			for i := range values {
				values[i] = strings.Repeat("x", rowBytes)
			}
			rec := stringJSONBatch(memory.DefaultAllocator, values)
			defer rec.Release()
			b.SetBytes(int64(len(values) * rowBytes))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				encoder := newJSONBatchEncoder(rec, 4<<20)
				for {
					_, n, err := encoder.next(context.Background())
					if err != nil {
						b.Fatal(err)
					}
					if n == 0 {
						break
					}
				}
			}
		})
	}
}

func FuzzBoundedJSONQuoted(f *testing.F) {
	for _, value := range []string{"", "abc", "\x00\xff\n\t", "<>&\"\\", "\u2028\u2029😀"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		want, err := json.Marshal(value)
		require.NoError(t, err)
		b := boundedJSONBuffer{limit: math.MaxInt}
		b.writeQuoted(value)
		require.NoError(t, b.err)
		require.Equal(t, string(want), string(b.data))
	})
}

func FuzzBoundedRawJSON(f *testing.F) {
	for _, value := range []string{` { "a": [ 1, " <>& ", true, null ] } `, "\"\u2028\u2029\"", `"\""`, "\"\xff\"", `"\\"`, `"a \" b"`} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !json.Valid([]byte(value)) {
			return
		}
		want, err := json.Marshal(json.RawMessage(value))
		require.NoError(t, err)
		b := boundedJSONBuffer{limit: len(want)}
		b.writeRawJSON(value)
		require.NoError(t, b.err)
		require.Equal(t, string(want), string(b.data))
	})
}
