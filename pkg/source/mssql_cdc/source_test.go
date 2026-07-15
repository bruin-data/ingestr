package mssql_cdc

import (
	"net/url"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURIConfig(t *testing.T) {
	cfg, normalized, err := parseURIConfig("mssql+cdc://sa:pass@example:1433/app?encrypt=disable&mode=stream&dest_schema=raw&capture_instance=dbo_users&poll_interval=250ms")
	require.NoError(t, err)

	assert.Equal(t, "raw", cfg.DestSchema)
	assert.Equal(t, "dbo_users", cfg.CaptureInstance)
	assert.Equal(t, 250*time.Millisecond, cfg.PollInterval)

	u, err := url.Parse(normalized)
	require.NoError(t, err)
	assert.Equal(t, "mssql", u.Scheme)
	assert.Equal(t, "disable", u.Query().Get("encrypt"))
	assert.Empty(t, u.Query().Get("mode"))
	assert.Empty(t, u.Query().Get("dest_schema"))
	assert.Empty(t, u.Query().Get("capture_instance"))
	assert.Empty(t, u.Query().Get("poll_interval"))
}

func TestStoredLSNHelpers(t *testing.T) {
	assert.Equal(t, "0000002F0000010D0002", startLSNFromStored("0000002f0000010d0002:0000002f0000010d0003:04"))
	assert.Equal(t, "0000002F0000010D0002", startLSNFromStored("0x0000002f0000010d0002"))
	assert.Empty(t, startLSNFromStored("00000000/00000123"))

	assert.Equal(
		t,
		"0000002F0000010D0002:0000002F0000010D0003:04",
		formatStoredLSN("0000002f0000010d0002", "0000002f0000010d0003", 4),
	)

	assert.Less(t, compareLSNHex("00000000000000000001", "00000000000000000002"), 0)
	assert.Greater(t, compareLSNHex("00000000000000000002", "00000000000000000001"), 0)
	assert.True(t, isZeroLSN("00000000000000000000"))
}

func TestAddCDCColumns(t *testing.T) {
	original := &schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}

	got := addCDCColumns(original)

	require.Len(t, got.Columns, 5)
	assert.Equal(t, destination.CDCLSNColumn, got.Columns[2].Name)
	assert.Equal(t, destination.CDCDeletedColumn, got.Columns[3].Name)
	assert.Equal(t, destination.CDCSyncedAtColumn, got.Columns[4].Name)
	assert.Len(t, original.Columns, 2, "addCDCColumns must not mutate the input schema")
}

func TestSourceColumnsWithoutCDC(t *testing.T) {
	tableSchema := addCDCColumns(&schema.TableSchema{
		Name: "users",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	})

	got := sourceColumnsWithoutCDC(tableSchema)

	require.Len(t, got, 2)
	assert.Equal(t, "id", got[0].Name)
	assert.Equal(t, "name", got[1].Name)
}

func TestBuildSnapshotQueryUsesNullForDroppedCapturedColumns(t *testing.T) {
	meta := tableMetadata{
		SourceSchema:   "dbo",
		SourceName:     "users",
		CurrentColumns: map[string]bool{"id": true},
	}
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "old_name", DataType: schema.TypeString},
	}

	got := buildSnapshotQuery(meta, columns, true)

	assert.Contains(t, got, "SELECT [id], NULL AS [old_name]")
	assert.Contains(t, got, "FROM [dbo].[users] WITH (HOLDLOCK)")
}
