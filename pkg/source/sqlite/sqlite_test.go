package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSQLitePath(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "absolute path",
			uri:  "sqlite:///Users/test/data.db",
			want: "/Users/test/data.db",
		},
		{
			name: "relative path",
			uri:  "sqlite://data.db",
			want: "data.db",
		},
		{
			name: "relative with dot",
			uri:  "sqlite://./data.db",
			want: "./data.db",
		},
		{
			name: "single slash filename treated as relative",
			uri:  "sqlite:///data.db",
			want: "./data.db",
		},
		{
			name:      "empty path",
			uri:       "sqlite://",
			wantErr:   true,
			errSubstr: "empty file path",
		},
		{
			name:      "wrong scheme",
			uri:       "postgres://localhost/db",
			wantErr:   true,
			errSubstr: "invalid sqlite URI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSQLitePath(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMapSQLiteToDataType(t *testing.T) {
	tests := []struct {
		name      string
		sqlType   string
		wantType  schema.DataType
		wantPrec  int
		wantScale int
	}{
		{"BOOLEAN", "BOOLEAN", schema.TypeBoolean, 0, 0},
		{"BOOL", "BOOL", schema.TypeBoolean, 0, 0},
		{"TINYINT", "TINYINT", schema.TypeInt64, 0, 0},
		{"SMALLINT", "SMALLINT", schema.TypeInt64, 0, 0},
		{"INT2", "INT2", schema.TypeInt64, 0, 0},
		{"MEDIUMINT", "MEDIUMINT", schema.TypeInt64, 0, 0},
		{"INT", "INT", schema.TypeInt64, 0, 0},
		{"INTEGER", "INTEGER", schema.TypeInt64, 0, 0},
		{"INT4", "INT4", schema.TypeInt64, 0, 0},
		{"BIGINT", "BIGINT", schema.TypeInt64, 0, 0},
		{"INT8", "INT8", schema.TypeInt64, 0, 0},
		{"REAL", "REAL", schema.TypeFloat64, 0, 0},
		{"DOUBLE", "DOUBLE", schema.TypeFloat64, 0, 0},
		{"DOUBLE PRECISION", "DOUBLE PRECISION", schema.TypeFloat64, 0, 0},
		{"FLOAT", "FLOAT", schema.TypeFloat64, 0, 0},
		{"DECIMAL bare", "DECIMAL", schema.TypeDecimal, 0, 0},
		{"DECIMAL(10,2)", "DECIMAL(10,2)", schema.TypeDecimal, 10, 2},
		{"NUMERIC bare", "NUMERIC", schema.TypeDecimal, 0, 0},
		{"NUMERIC(20,5)", "NUMERIC(20,5)", schema.TypeDecimal, 20, 5},
		{"TEXT", "TEXT", schema.TypeString, 0, 0},
		{"VARCHAR(50)", "VARCHAR(50)", schema.TypeString, 0, 0},
		{"CHARACTER", "CHARACTER", schema.TypeString, 0, 0},
		{"CHAR(10)", "CHAR(10)", schema.TypeString, 0, 0},
		{"CLOB", "CLOB", schema.TypeString, 0, 0},
		{"NVARCHAR(100)", "NVARCHAR(100)", schema.TypeString, 0, 0},
		{"NCHAR(10)", "NCHAR(10)", schema.TypeString, 0, 0},
		{"BLOB", "BLOB", schema.TypeBinary, 0, 0},
		{"DATE", "DATE", schema.TypeDate, 0, 0},
		{"TIME", "TIME", schema.TypeTime, 0, 0},
		{"DATETIME", "DATETIME", schema.TypeTimestampTZ, 0, 0},
		{"TIMESTAMP", "TIMESTAMP", schema.TypeTimestamp, 0, 0},
		{"JSON", "JSON", schema.TypeJSON, 0, 0},
		{"UUID", "UUID", schema.TypeUUID, 0, 0},
		{"empty type defaults to string", "", schema.TypeString, 0, 0},
		{"unknown type defaults to string", "GEOMETRY", schema.TypeString, 0, 0},
		{"case insensitive", "integer", schema.TypeInt64, 0, 0},
		{"whitespace trimmed", "  TEXT  ", schema.TypeString, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt, prec, scale := MapSQLiteToDataType(tt.sqlType)
			assert.Equal(t, tt.wantType, dt, "data type mismatch")
			assert.Equal(t, tt.wantPrec, prec, "precision mismatch")
			assert.Equal(t, tt.wantScale, scale, "scale mismatch")
		})
	}
}

func TestBuildSelectQueryAddsExtractPartitionPredicate(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	query := buildSelectQuery("orders", []schema.Column{
		{Name: "id"},
		{Name: "created_at"},
	}, source.ReadOptions{
		IncrementalKey:        "updated_at",
		IntervalStart:         &intervalStart,
		IntervalEnd:           &intervalEnd,
		ExtractPartitionBy:    "created_at",
		ExtractPartitionStart: &windowStart,
		ExtractPartitionEnd:   &windowEnd,
	})

	want := "SELECT \"id\", \"created_at\" FROM \"orders\" WHERE \"updated_at\" >= '2026-01-01 00:00:00' AND \"updated_at\" <= '2026-01-31 00:00:00' AND \"created_at\" >= '2026-01-08 00:00:00' AND \"created_at\" < '2026-01-15 00:00:00'"
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestCustomQueryExtractPartitioning(t *testing.T) {
	ctx := context.Background()
	src := NewSQLiteSource()
	uri := "sqlite:///" + filepath.Join(t.TempDir(), "source.db")
	require.NoError(t, src.Connect(ctx, uri))
	t.Cleanup(func() { require.NoError(t, src.Close(ctx)) })

	_, err := src.db.ExecContext(ctx, `
		CREATE TABLE events (id INTEGER, created_at TIMESTAMP);
		INSERT INTO events VALUES
			(1, '2026-01-01 00:00:00'),
			(2, '2026-01-01 12:00:00'),
			(3, '2026-01-02 00:00:00'),
			(4, '2026-01-03 00:00:00');
	`)
	require.NoError(t, err)

	table, err := src.GetTable(ctx, source.TableRequest{
		Name:           "query:SELECT id, created_at AS partition_ts FROM events;",
		IncrementalKey: "partition_ts",
	})
	require.NoError(t, err)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	opts := source.ReadOptions{
		IncrementalKey:           "partition_ts",
		IntervalStart:            &start,
		IntervalEnd:              &end,
		ExtractPartitionBy:       "partition_ts",
		ExtractPartitionInterval: 24 * time.Hour,
		Parallelism:              2,
	}
	provider, ok := table.(source.ReadSchemaProvider)
	require.True(t, ok)
	opts.Schema, err = provider.GetReadSchema(ctx, opts)
	require.NoError(t, err)
	_, err = source.ValidateExtractPartitionColumn(opts.Schema, opts.ExtractPartitionBy)
	require.NoError(t, err)

	records, err := table.Read(ctx, opts)
	require.NoError(t, err)
	rowCount := int64(0)
	for result := range records {
		require.NoError(t, result.Err)
		rowCount += result.Batch.NumRows()
		result.Batch.Release()
	}
	assert.Equal(t, int64(4), rowCount)
}

func TestExtractTableName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"users", "users"},
		{"main.users", "users"},
		{"schema.table", "table"},
		{"a.b.c", "c"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, extractTableName(tt.input))
		})
	}
}

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"users", `"users"`},
		{`already "quoted"`, `"already ""quoted"""`},
		{`"fully_quoted"`, `"fully_quoted"`},
		{"has space", `"has space"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, quoteIdentifier(tt.input))
		})
	}
}
