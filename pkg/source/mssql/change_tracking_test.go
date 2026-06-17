package mssql

import (
	"errors"
	"math"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
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
		{raw: "00000000000000000000", want: 0, wantOK: true, message: "zero version"},
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

func TestSnapshotCTTableDoesNotRetryAfterEmittingRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	src := &MSSQLChangeTrackingSource{MSSQLSource: MSSQLSource{db: db}}
	tableSchema := &schema.TableSchema{
		Columns: append([]schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		}, ctMetadataColumns...),
	}

	mock.ExpectBegin()
	mock.ExpectQuery("CHANGE_TRACKING_CURRENT_VERSION").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(int64(7)))
	mock.ExpectQuery("FROM \\[dbo\\]\\.\\[items\\]").
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			destination.CDCLSNColumn,
			destination.CDCDeletedColumn,
			destination.CDCSyncedAtColumn,
		}).AddRow(int64(1), "00000000000000000007", false, time.Now()))
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))

	results := make(chan source.RecordBatchResult, 2)
	_, err = src.snapshotCTTable(t.Context(), "dbo.items", tableSchema, source.ReadOptions{PageSize: 100}, results)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not retrying")
	assert.Len(t, results, 1)
	res := <-results
	require.NoError(t, res.Err)
	require.NotNil(t, res.Batch)
	res.Batch.Release()
	require.NoError(t, mock.ExpectationsWereMet())
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

	got := buildCTSnapshotQuery("dbo.items", columns, 123, true)
	normalized := strings.Join(strings.Fields(got), " ")

	assert.Contains(t, normalized, "SELECT")
	assert.NotContains(t, normalized, "TOP")
	assert.Contains(t, normalized, "[id], [name]")
	assert.Contains(t, normalized, "CONVERT(varchar(20), 123)")
	assert.Contains(t, normalized, "FROM [dbo].[items] WITH (HOLDLOCK)")
}

func TestBuildCTHeartbeatQuery(t *testing.T) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
	}

	got := buildCTHeartbeatQuery("dbo.items", columns, []string{"id"}, 456)
	normalized := strings.Join(strings.Fields(got), " ")

	assert.Contains(t, normalized, "SELECT TOP 1")
	assert.Contains(t, normalized, "[id], [name]")
	assert.Contains(t, normalized, "CONVERT(varchar(20), 456)")
	assert.Contains(t, normalized, "FROM [dbo].[items]")
	assert.Contains(t, normalized, "ORDER BY [id]")
}

func TestChangeTrackingReadRejectsLimit(t *testing.T) {
	table := &changeTrackingTable{}

	records, err := table.Read(t.Context(), source.ReadOptions{Limit: 10})

	require.Error(t, err)
	assert.Nil(t, records)
	assert.Contains(t, err.Error(), "--sql-limit")
}

func TestChangeTrackingReadRejectsMetadataExcludes(t *testing.T) {
	table := &changeTrackingTable{}

	records, err := table.Read(t.Context(), source.ReadOptions{ExcludeColumns: []string{destination.CDCLSNColumn}})

	require.Error(t, err)
	assert.Nil(t, records)
	assert.Contains(t, err.Error(), "--sql-exclude-columns")
	assert.Contains(t, err.Error(), destination.CDCLSNColumn)
}

func TestChangeTrackingReadRejectsPrimaryKeyExcludes(t *testing.T) {
	table := &changeTrackingTable{primaryKeys: []string{"id"}}

	records, err := table.Read(t.Context(), source.ReadOptions{ExcludeColumns: []string{"ID"}})

	require.Error(t, err)
	assert.Nil(t, records)
	assert.Contains(t, err.Error(), "--sql-exclude-columns")
	assert.Contains(t, err.Error(), "ID")
}

func TestChangeTrackingGetTableRejectsNonMergeStrategy(t *testing.T) {
	src := &MSSQLChangeTrackingSource{}

	table, err := src.GetTable(t.Context(), source.TableRequest{
		Name:     "dbo.items",
		Strategy: config.StrategyAppend,
	})

	require.Error(t, err)
	assert.Nil(t, table)
	assert.Contains(t, err.Error(), `require "merge" incremental strategy`)
	assert.Contains(t, err.Error(), `"append"`)
}

func TestResolveChangeTrackingStrategyDistinguishesDefaultReplace(t *testing.T) {
	strategy, err := resolveChangeTrackingStrategy(config.StrategyReplace, false, false)
	require.NoError(t, err)
	assert.Equal(t, config.StrategyMerge, strategy)

	strategy, err = resolveChangeTrackingStrategy(config.StrategyReplace, true, true)
	require.NoError(t, err)
	assert.Equal(t, config.StrategyMerge, strategy)

	strategy, err = resolveChangeTrackingStrategy(config.StrategyMerge, true, false)
	require.NoError(t, err)
	assert.Equal(t, config.StrategyMerge, strategy)

	strategy, err = resolveChangeTrackingStrategy(config.StrategyAppend, true, true)
	require.NoError(t, err)
	assert.Equal(t, config.StrategyMerge, strategy)
}

func TestChangeTrackingGetTableRejectsExplicitReplaceWithoutFullRefresh(t *testing.T) {
	src := &MSSQLChangeTrackingSource{}

	table, err := src.GetTable(t.Context(), source.TableRequest{
		Name:        "dbo.items",
		Strategy:    config.StrategyReplace,
		StrategySet: true,
	})

	require.Error(t, err)
	assert.Nil(t, table)
	assert.Contains(t, err.Error(), `require "merge" incremental strategy`)
	assert.Contains(t, err.Error(), "full-refresh")
	assert.Contains(t, err.Error(), `"replace"`)
}

func TestShouldEmitCTHeartbeatOnlyWhenNoChangeRows(t *testing.T) {
	assert.True(t, shouldEmitCTHeartbeat(true, 0))
	assert.False(t, shouldEmitCTHeartbeat(true, 1))
	assert.False(t, shouldEmitCTHeartbeat(false, 0))
}

func TestSyntheticCTHeartbeatRecord(t *testing.T) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
	}

	record, err := syntheticCTHeartbeatRecord(columns, []string{"id"}, 42)
	require.NoError(t, err)
	defer record.Release()

	require.EqualValues(t, 1, record.NumRows())
	require.EqualValues(t, math.MinInt64, record.Column(0).(*array.Int64).Value(0))
	assert.True(t, record.Column(1).IsNull(0))
	assert.Equal(t, "00000000000000000042", record.Column(2).(*array.String).Value(0))
	assert.True(t, record.Column(3).(*array.Boolean).Value(0))
	assert.False(t, record.Column(4).IsNull(0))
}

func TestObjectIDNamePreservesSupportedTableRefs(t *testing.T) {
	tests := []struct {
		name  string
		table string
		want  string
	}{
		{
			name:  "unqualified table defaults dbo",
			table: "items",
			want:  "[dbo].[items]",
		},
		{
			name:  "schema qualified table",
			table: "sales.items",
			want:  "[sales].[items]",
		},
		{
			name:  "database qualified table",
			table: "RemoteDB.dbo.items",
			want:  "[RemoteDB].[dbo].[items]",
		},
		{
			name:  "bracketed identifiers with dots",
			table: "[RemoteDB].[erp.schema].[order.items]",
			want:  "[RemoteDB].[erp.schema].[order.items]",
		},
		{
			name:  "escaped bracket in identifier",
			table: "[Remote]]DB].[dbo].[item]]table]",
			want:  "[Remote]]DB].[dbo].[item]]table]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, objectIDName(tt.table))
		})
	}
}

func TestCTVersionExpiredErrorMentionsFullRefresh(t *testing.T) {
	err := (&ctVersionExpiredError{table: "dbo.items", version: 10, minVersion: 20}).Error()

	assert.Contains(t, err, "version 10")
	assert.Contains(t, err, "minimum valid version is 20")
	assert.Contains(t, err, "--full-refresh")
}
