package spanner

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantDBPath string
		wantErr    bool
		errContain string
	}{
		{
			name:       "valid URI with all params",
			uri:        "spanner://?project_id=my-project&instance_id=my-instance&database=my-db",
			wantDBPath: "projects/my-project/instances/my-instance/databases/my-db",
		},
		{
			name:       "valid URI with credentials_path",
			uri:        "spanner://?project_id=proj&instance_id=inst&database=db&credentials_path=/path/to/creds.json",
			wantDBPath: "projects/proj/instances/inst/databases/db",
		},
		{
			name:       "valid URI with credentials_base64",
			uri:        "spanner://?project_id=proj&instance_id=inst&database=db&credentials_base64=" + base64.StdEncoding.EncodeToString([]byte(`{"type":"service_account"}`)),
			wantDBPath: "projects/proj/instances/inst/databases/db",
		},
		{
			name:       "missing project_id",
			uri:        "spanner://?instance_id=inst&database=db",
			wantErr:    true,
			errContain: "project_id, instance_id, and database are required",
		},
		{
			name:       "missing instance_id",
			uri:        "spanner://?project_id=proj&database=db",
			wantErr:    true,
			errContain: "project_id, instance_id, and database are required",
		},
		{
			name:       "missing database",
			uri:        "spanner://?project_id=proj&instance_id=inst",
			wantErr:    true,
			errContain: "project_id, instance_id, and database are required",
		},
		{
			name:       "empty URI",
			uri:        "spanner://",
			wantErr:    true,
			errContain: "project_id, instance_id, and database are required",
		},
		{
			name:       "invalid base64 credentials",
			uri:        "spanner://?project_id=proj&instance_id=inst&database=db&credentials_base64=not-valid-base64!!!",
			wantErr:    true,
			errContain: "failed to decode credentials_base64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath, opts, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDBPath, dbPath)
			if tt.uri == "spanner://?project_id=my-project&instance_id=my-instance&database=my-db" {
				assert.Empty(t, opts, "no credentials means no client options")
			}
		})
	}
}

func TestMapSpannerTypeString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantType  schema.DataType
		wantPrec  int
		wantScale int
		wantArray schema.DataType
	}{
		{"BOOL", "BOOL", schema.TypeBoolean, 0, 0, schema.TypeUnknown},
		{"INT64", "INT64", schema.TypeInt64, 0, 0, schema.TypeUnknown},
		{"FLOAT32", "FLOAT32", schema.TypeFloat32, 0, 0, schema.TypeUnknown},
		{"FLOAT64", "FLOAT64", schema.TypeFloat64, 0, 0, schema.TypeUnknown},
		{"NUMERIC", "NUMERIC", schema.TypeDecimal, 38, 9, schema.TypeUnknown},
		{"STRING", "STRING(MAX)", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"STRING with length", "STRING(255)", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"BYTES", "BYTES(MAX)", schema.TypeBinary, 0, 0, schema.TypeUnknown},
		{"DATE", "DATE", schema.TypeDate, 0, 0, schema.TypeUnknown},
		{"TIMESTAMP", "TIMESTAMP", schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown},
		{"JSON", "JSON", schema.TypeJSON, 0, 0, schema.TypeUnknown},
		{"ARRAY<STRING>", "ARRAY<STRING(MAX)>", schema.TypeArray, 0, 0, schema.TypeString},
		{"ARRAY<INT64>", "ARRAY<INT64>", schema.TypeArray, 0, 0, schema.TypeInt64},
		{"ARRAY<FLOAT64>", "ARRAY<FLOAT64>", schema.TypeArray, 0, 0, schema.TypeFloat64},
		{"ARRAY<BOOL>", "ARRAY<BOOL>", schema.TypeArray, 0, 0, schema.TypeBoolean},
		{"lowercase", "bool", schema.TypeBoolean, 0, 0, schema.TypeUnknown},
		{"mixed case", "String(MAX)", schema.TypeString, 0, 0, schema.TypeUnknown},
		{"unknown type", "PROTO", schema.TypeString, 0, 0, schema.TypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt, p, s, at := mapSpannerTypeString(tt.input)
			assert.Equal(t, tt.wantType, dt, "DataType")
			assert.Equal(t, tt.wantPrec, p, "Precision")
			assert.Equal(t, tt.wantScale, s, "Scale")
			assert.Equal(t, tt.wantArray, at, "ArrayType")
		})
	}
}

func TestFilterColumns(t *testing.T) {
	columns := []schema.Column{
		{Name: "id"},
		{Name: "name"},
		{Name: "email"},
		{Name: "created_at"},
	}

	tests := []struct {
		name    string
		exclude []string
		want    []string
	}{
		{
			name:    "no exclusions",
			exclude: nil,
			want:    []string{"id", "name", "email", "created_at"},
		},
		{
			name:    "exclude one column",
			exclude: []string{"email"},
			want:    []string{"id", "name", "created_at"},
		},
		{
			name:    "exclude multiple columns",
			exclude: []string{"email", "created_at"},
			want:    []string{"id", "name"},
		},
		{
			name:    "exclude with different case",
			exclude: []string{"EMAIL", "Name"},
			want:    []string{"id", "created_at"},
		},
		{
			name:    "exclude non-existent column",
			exclude: []string{"nonexistent"},
			want:    []string{"id", "name", "email", "created_at"},
		},
		{
			name:    "empty exclude list",
			exclude: []string{},
			want:    []string{"id", "name", "email", "created_at"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterColumns(columns, tt.exclude)
			got := make([]string, len(result))
			for i, c := range result {
				got[i] = c.Name
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildSelectQuery(t *testing.T) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
		{Name: "created_at", DataType: schema.TypeTimestampTZ},
	}

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	tests := []struct {
		name  string
		table string
		opts  source.ReadOptions
		want  string
	}{
		{
			name:  "basic select",
			table: "users",
			opts:  source.ReadOptions{},
			want:  "SELECT `id`, `name`, `created_at` FROM `users`",
		},
		{
			name:  "with limit",
			table: "users",
			opts:  source.ReadOptions{Limit: 10},
			want:  "SELECT `id`, `name`, `created_at` FROM `users` LIMIT 10",
		},
		{
			name:  "with incremental key and both intervals",
			table: "events",
			opts: source.ReadOptions{
				IncrementalKey: "created_at",
				IntervalStart:  &start,
				IntervalEnd:    &end,
			},
			want: "SELECT `id`, `name`, `created_at` FROM `events` WHERE `created_at` >= TIMESTAMP('2024-01-01T00:00:00Z') AND `created_at` <= TIMESTAMP('2024-12-31T23:59:59Z')",
		},
		{
			name:  "with incremental key start only",
			table: "events",
			opts: source.ReadOptions{
				IncrementalKey: "created_at",
				IntervalStart:  &start,
			},
			want: "SELECT `id`, `name`, `created_at` FROM `events` WHERE `created_at` >= TIMESTAMP('2024-01-01T00:00:00Z')",
		},
		{
			name:  "with incremental key end only",
			table: "events",
			opts: source.ReadOptions{
				IncrementalKey: "created_at",
				IntervalEnd:    &end,
			},
			want: "SELECT `id`, `name`, `created_at` FROM `events` WHERE `created_at` <= TIMESTAMP('2024-12-31T23:59:59Z')",
		},
		{
			name:  "incremental key with limit",
			table: "events",
			opts: source.ReadOptions{
				IncrementalKey: "created_at",
				IntervalStart:  &start,
				Limit:          100,
			},
			want: "SELECT `id`, `name`, `created_at` FROM `events` WHERE `created_at` >= TIMESTAMP('2024-01-01T00:00:00Z') LIMIT 100",
		},
		{
			name:  "no incremental key ignores intervals",
			table: "events",
			opts: source.ReadOptions{
				IntervalStart: &start,
				IntervalEnd:   &end,
			},
			want: "SELECT `id`, `name`, `created_at` FROM `events`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSelectQuery(tt.table, columns, tt.opts)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildArrowSchema(t *testing.T) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
		{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
		{Name: "active", DataType: schema.TypeBoolean, Nullable: false},
	}

	arrowSchema := buildArrowSchema(columns)

	require.Equal(t, 4, arrowSchema.NumFields())
	assert.Equal(t, "id", arrowSchema.Field(0).Name)
	assert.Equal(t, "name", arrowSchema.Field(1).Name)
	assert.Equal(t, "score", arrowSchema.Field(2).Name)
	assert.Equal(t, "active", arrowSchema.Field(3).Name)
	assert.False(t, arrowSchema.Field(0).Nullable)
	assert.True(t, arrowSchema.Field(1).Nullable)
}

func TestSchemes(t *testing.T) {
	s := NewSpannerSource()
	assert.Equal(t, []string{"spanner"}, s.Schemes())
}

func TestHandlesIncrementality(t *testing.T) {
	s := NewSpannerSource()
	assert.False(t, s.HandlesIncrementality())
}

func TestNewSpannerSource(t *testing.T) {
	s := NewSpannerSource()
	require.NotNil(t, s)
	assert.Nil(t, s.client)
	assert.Empty(t, s.dbPath)
}
