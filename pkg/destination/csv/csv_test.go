package csv

import (
	"bytes"
	"encoding/csv"
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
		want  []string
	}{
		{"uint8", arrow.PrimitiveTypes.Uint8, `[7,255,null]`, []string{"7", "255", ""}},
		{"uint16", arrow.PrimitiveTypes.Uint16, `[7,65535,null]`, []string{"7", "65535", ""}},
		{"uint32", arrow.PrimitiveTypes.Uint32, `[7,4294967295,null]`, []string{"7", "4294967295", ""}},
		{"uint64", arrow.PrimitiveTypes.Uint64, `[7,18446744073709551615,null]`, []string{"7", "18446744073709551615", ""}},
		{"list", arrow.ListOf(arrow.PrimitiveTypes.Int64), `[[1,null,3],[],null]`, []string{`[1,null,3]`, `[]`, ""}},
		{"large_list", arrow.LargeListOf(arrow.PrimitiveTypes.Int64), `[[8,2],[],null]`, []string{`[8,2]`, `[]`, ""}},
		{"fixed_list", arrow.FixedSizeListOf(2, arrow.PrimitiveTypes.Int64), `[[8,2],[3,null],null]`, []string{`[8,2]`, `[3,null]`, ""}},
		{"struct", arrow.StructOf(arrow.Field{Name: "label", Type: arrow.BinaryTypes.String, Nullable: true}), `[{"label":"a,\"b"},{"label":null},null]`, []string{`{"label":"a,\"b"}`, `{"label":null}`, ""}},
		{"map", arrow.MapOf(arrow.PrimitiveTypes.Int64, arrow.BinaryTypes.String), `[[{"key":2,"value":"two"},{"key":2,"value":null}],[],null]`, []string{`[{"key":2,"value":"two"},{"key":2,"value":null}]`, `[]`, ""}},
		{"time_microseconds", &arrow.Time64Type{Unit: arrow.Microsecond}, `["01:00:00","23:12:34",null]`, []string{"01:00:00", "23:12:34", ""}},
		{"time_nanoseconds", &arrow.Time64Type{Unit: arrow.Nanosecond}, `["01:00:00","23:12:34",null]`, []string{"01:00:00", "23:12:34", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arr, _, err := array.FromJSON(memory.DefaultAllocator, tt.typ, strings.NewReader(tt.input))
			require.NoError(t, err)
			defer arr.Release()
			record := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "value", Type: tt.typ}, {Name: "copy", Type: tt.typ}}, nil), []arrow.Array{arr, arr}, int64(arr.Len()))
			defer record.Release()
			var buf bytes.Buffer
			d := &CSVDestination{writer: csv.NewWriter(&buf)}
			rows, err := d.writeRecordBatch(record)
			require.NoError(t, err)
			require.Equal(t, int64(len(tt.want)), rows)
			d.writer.Flush()
			require.NoError(t, d.writer.Error())
			got, err := csv.NewReader(&buf).ReadAll()
			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for i, row := range got {
				require.Equal(t, []string{tt.want[i], tt.want[i]}, row)
			}
		})
	}
}
