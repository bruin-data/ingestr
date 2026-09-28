package athena

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestBuildInsertSQLIntegerAndDateLiterals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dtype  arrow.DataType
		values string
		want   string
	}{
		{"int8", arrow.PrimitiveTypes.Int8, `[-128, 127, null]`, "(-128), (127), (NULL)"},
		{"uint8", arrow.PrimitiveTypes.Uint8, `[0, 255, null]`, "(0), (255), (NULL)"},
		{"uint16", arrow.PrimitiveTypes.Uint16, `[0, 65535, null]`, "(0), (65535), (NULL)"},
		{"uint32", arrow.PrimitiveTypes.Uint32, `[0, 4294967295, null]`, "(0), (4294967295), (NULL)"},
		{"uint64", arrow.PrimitiveTypes.Uint64, `[0, 18446744073709551615, null]`, "(0), (18446744073709551615), (NULL)"},
		{"date64", arrow.PrimitiveTypes.Date64, `[-86400000, 86400000, null]`, "(DATE '1969-12-31'), (DATE '1970-01-02'), (NULL)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer alloc.AssertSize(t, 0)
			arr, _, err := array.FromJSON(alloc, tc.dtype, strings.NewReader(tc.values))
			require.NoError(t, err)
			defer arr.Release()
			rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "value", Type: tc.dtype, Nullable: true}}, nil), []arrow.Array{arr}, int64(arr.Len()))
			defer rec.Release()
			sql, err := buildInsertSQL("db", "tbl", []string{"value"}, rec, 0, arr.Len())
			require.NoError(t, err)
			require.Equal(t, `INSERT INTO "db"."tbl" (value) VALUES `+tc.want, sql)
		})
	}
}

func TestBuildCreateIcebergTableSQL(t *testing.T) {
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "payload", DataType: schema.TypeJSON, Nullable: true},
		{Name: "amount", DataType: schema.TypeDecimal, Precision: 12, Scale: 2, Nullable: true},
	}

	sql, err := buildCreateIcebergTableSQL("db", "tbl", cols, "s3://bucket/prefix/iceberg/db/tbl/")
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(sql), "table_type='iceberg'")
	require.Contains(t, strings.ToLower(sql), "is_external=false")
	require.Contains(t, strings.ToLower(sql), "format='parquet'")
	require.Contains(t, sql, `location='s3://bucket/prefix/iceberg/db/tbl/'`)
	require.Contains(t, strings.ToLower(sql), "as select")
	require.Contains(t, sql, `CAST(NULL AS bigint) AS id`)
	require.Contains(t, sql, `CAST(NULL AS varchar) AS payload`)
	require.Contains(t, sql, `CAST(NULL AS decimal(12,2)) AS amount`)
}
