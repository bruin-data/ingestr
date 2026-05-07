package athena

import (
	"strings"
	"testing"

	"github.com/bruin-data/gong/pkg/schema"
	"github.com/stretchr/testify/require"
)

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
