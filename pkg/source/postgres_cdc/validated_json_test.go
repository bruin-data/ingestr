package postgres_cdc

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestMarkValidatedJSONOnlyFlagsPostgresJSONColumns(t *testing.T) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "doc", DataType: schema.TypeJSON},
		{Name: "raw", DataType: schema.TypeJSON},
		{Name: "tags", DataType: schema.TypeJSON},
	}
	fields := []pgconn.FieldDescription{
		{DataTypeOID: pgtype.Int8OID}, {DataTypeOID: pgtype.JSONBOID}, {DataTypeOID: pgtype.JSONOID}, {DataTypeOID: pgtype.TextArrayOID},
	}
	marked := markValidatedJSON(buildArrowSchema(columns), columns, fields)
	for i, want := range []bool{false, true, true, false} {
		value, ok := marked.Field(i).Metadata.GetValue(schema.JSONValidatedMetadataKey)
		require.Equal(t, want, ok && value == "true", columns[i].Name)
	}
	plain := buildArrowSchema(columns[:1])
	require.Same(t, plain, markValidatedJSON(plain, columns[:1], fields[:1]))
}
