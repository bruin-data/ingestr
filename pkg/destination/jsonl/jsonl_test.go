package jsonl

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestWriteRecordBatchCells(t *testing.T) {
	tests := []struct {
		name  string
		typ   arrow.DataType
		input string
		want  string
	}{
		{"uint8", arrow.PrimitiveTypes.Uint8, `[7,255,null]`, "{\"value\":7}\n{\"value\":255}\n{\"value\":null}\n"},
		{"uint16", arrow.PrimitiveTypes.Uint16, `[7,65535,null]`, "{\"value\":7}\n{\"value\":65535}\n{\"value\":null}\n"},
		{"uint32", arrow.PrimitiveTypes.Uint32, `[7,4294967295,null]`, "{\"value\":7}\n{\"value\":4294967295}\n{\"value\":null}\n"},
		{"uint64", arrow.PrimitiveTypes.Uint64, `[7,18446744073709551615,null]`, "{\"value\":7}\n{\"value\":18446744073709551615}\n{\"value\":null}\n"},
		{"list", arrow.ListOf(arrow.PrimitiveTypes.Int64), `[[1,null,3],[],null]`, "{\"value\":[1,null,3]}\n{\"value\":[]}\n{\"value\":null}\n"},
		{"large_list", arrow.LargeListOf(arrow.PrimitiveTypes.Int64), `[[8,2],[],null]`, "{\"value\":[8,2]}\n{\"value\":[]}\n{\"value\":null}\n"},
		{"fixed_list", arrow.FixedSizeListOf(2, arrow.PrimitiveTypes.Int64), `[[8,2],[3,null],null]`, "{\"value\":[8,2]}\n{\"value\":[3,null]}\n{\"value\":null}\n"},
		{"struct", arrow.StructOf(arrow.Field{Name: "label", Type: arrow.BinaryTypes.String, Nullable: true}), `[{"label":"a,\"b"},{"label":null},null]`, "{\"value\":{\"label\":\"a,\\\"b\"}}\n{\"value\":{\"label\":null}}\n{\"value\":null}\n"},
		{"map", arrow.MapOf(arrow.PrimitiveTypes.Int64, arrow.BinaryTypes.String), `[[{"key":2,"value":"two"},{"key":2,"value":null}],[],null]`, "{\"value\":[{\"key\":2,\"value\":\"two\"},{\"key\":2,\"value\":null}]}\n{\"value\":[]}\n{\"value\":null}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arr, _, err := array.FromJSON(memory.DefaultAllocator, tt.typ, strings.NewReader(tt.input))
			require.NoError(t, err)
			defer arr.Release()
			record := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "value", Type: tt.typ}}, nil), []arrow.Array{arr}, int64(arr.Len()))
			defer record.Release()
			path := filepath.Join(t.TempDir(), "out.jsonl")
			file, err := os.Create(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, file.Close()) })
			d := &JSONLDestination{file: file}
			rows, err := d.writeRecordBatch(record)
			require.NoError(t, err)
			require.Equal(t, int64(arr.Len()), rows)
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestTopLevelNonfiniteFloatsAreRejected(t *testing.T) {
	for _, typ := range []arrow.DataType{arrow.PrimitiveTypes.Float32, arrow.PrimitiveTypes.Float64} {
		t.Run(typ.Name(), func(t *testing.T) {
			builder := array.NewBuilder(memory.DefaultAllocator, typ)
			defer builder.Release()
			switch b := builder.(type) {
			case *array.Float32Builder:
				b.AppendValues([]float32{1.5, float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))}, nil)
			case *array.Float64Builder:
				b.AppendValues([]float64{1.5, math.NaN(), math.Inf(1), math.Inf(-1)}, nil)
			}
			arr := builder.NewArray()
			defer arr.Release()
			got, err := json.Marshal(extractValue(arr, 0))
			require.NoError(t, err)
			require.Equal(t, "1.5", string(got))
			for i := 1; i < arr.Len(); i++ {
				_, err := json.Marshal(extractValue(arr, i))
				var unsupported *json.UnsupportedValueError
				require.ErrorAs(t, err, &unsupported)
			}
		})
	}
}
