package mssql

import (
	"net/url"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeChangeTrackingURI(t *testing.T) {
	normalized, err := normalizeChangeTrackingURI("sqlserver+ct://sa:pass@example:1433/app?encrypt=disable")
	require.NoError(t, err)

	u, err := url.Parse(normalized)
	require.NoError(t, err)
	assert.Equal(t, "sqlserver", u.Scheme)
	assert.Equal(t, "disable", u.Query().Get("encrypt"))
}

func TestParseStoredCTVersion(t *testing.T) {
	tests := []struct {
		raw     string
		want    int64
		wantOK  bool
		message string
	}{
		{raw: "", wantOK: false, message: "empty"},
		{raw: "00000000000000000123", want: 123, wantOK: true, message: "padded"},
		{raw: "00000000000000000123:ignored", want: 123, wantOK: true, message: "padded with suffix"},
		{raw: "not-a-version", wantOK: false, message: "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got, ok := parseStoredCTVersion(tt.raw)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAddCTColumns(t *testing.T) {
	original := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}

	got := addCTColumns(original)

	require.Len(t, got.Columns, 5)
	assert.Equal(t, destination.CDCLSNColumn, got.Columns[2].Name)
	assert.Equal(t, destination.CDCDeletedColumn, got.Columns[3].Name)
	assert.Equal(t, destination.CDCSyncedAtColumn, got.Columns[4].Name)
	assert.Len(t, original.Columns, 2, "addCTColumns must not mutate the input schema")
}

func TestBuildCTChangesQuery(t *testing.T) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
		{Name: "value", DataType: schema.TypeInt32},
	}

	got := buildCTChangesQuery("dbo.items", columns, []string{"id"})
	normalized := strings.Join(strings.Fields(got), " ")

	assert.Contains(t, normalized, "FROM CHANGETABLE(CHANGES [dbo].[items], @p1) AS CT")
	assert.Contains(t, normalized, "LEFT JOIN [dbo].[items] AS T ON T.[id] = CT.[id]")
	assert.Contains(t, normalized, "CT.[id] AS [id]")
	assert.Contains(t, normalized, "T.[name] AS [name]")
	assert.Contains(t, normalized, "CASE WHEN CT.SYS_CHANGE_OPERATION = 'D' THEN 1 ELSE 0 END")
	assert.Contains(t, normalized, "WHERE CT.SYS_CHANGE_VERSION <= @p2")
}

func TestBuildCTSnapshotQuery(t *testing.T) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
	}

	got := buildCTSnapshotQuery("dbo.items", columns, sourceReadOptionsForTest(10), 123, true)
	normalized := strings.Join(strings.Fields(got), " ")

	assert.Contains(t, normalized, "SELECT TOP 10")
	assert.Contains(t, normalized, "[id], [name]")
	assert.Contains(t, normalized, "CONVERT(varchar(20), 123)")
	assert.Contains(t, normalized, "FROM [dbo].[items] WITH (HOLDLOCK)")
}

func sourceReadOptionsForTest(limit int) source.ReadOptions {
	return source.ReadOptions{Limit: limit}
}
