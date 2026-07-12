package onelake

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOneLakeURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		uri       string
		wantWS    string
		wantLH    string
		wantSAS   string
		wantSP    bool
		wantErr   bool
		wantLayou string
	}{
		{
			name:   "service principal",
			uri:    "onelake://myworkspace/mylakehouse?tenant_id=t&client_id=c&client_secret=s",
			wantWS: "myworkspace",
			wantLH: "mylakehouse",
			wantSP: true,
		},
		{
			name:    "sas token",
			uri:     "onelake://ws/lh?sas_token=sv=2021",
			wantWS:  "ws",
			wantLH:  "lh",
			wantSAS: "sv=2021",
		},
		{
			name:   "default credential",
			uri:    "onelake://ws/lh",
			wantWS: "ws",
			wantLH: "lh",
		},
		{
			name:      "custom layout",
			uri:       "onelake://ws/lh?layout={table_name}.parquet",
			wantWS:    "ws",
			wantLH:    "lh",
			wantLayou: "{table_name}.parquet",
		},
		{name: "missing lakehouse", uri: "onelake://ws", wantErr: true},
		{name: "nested lakehouse", uri: "onelake://ws/a/b", wantErr: true},
		{name: "wrong scheme", uri: "s3://ws/lh", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parsed, err := parseOneLakeURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantWS, parsed.workspace)
			assert.Equal(t, tt.wantLH, parsed.lakehouse)
			assert.Equal(t, tt.wantSAS, parsed.sasToken)
			assert.Equal(t, tt.wantSP, parsed.clientCredentials.IsSet())
			assert.Equal(t, tt.wantLayou, parsed.layout)
		})
	}
}

func TestParseTarget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		table    string
		wantMode writeMode
		wantPath string
	}{
		{"Tables/users", modeTables, "users"},
		{"tables/schema/users", modeTables, "schema/users"},
		{"Files/exports/users", modeFiles, "exports/users"},
		{"FILES/raw", modeFiles, "raw"},
		{"Files/data.parquet", modeFiles, "data.parquet"},
		{"users", modeTables, "users"},
		{"/Tables/users/", modeTables, "users"},
		{"schema.name", modeTables, "schema/name"},
		{"Tables.schema.name", modeTables, "schema/name"},
		{"Tables.users", modeTables, "users"},
	}
	for _, c := range cases {
		mode, path, err := parseTarget(c.table)
		require.NoError(t, err, c.table)
		assert.Equal(t, c.wantMode, mode, c.table)
		assert.Equal(t, c.wantPath, path, c.table)
	}

	for _, bad := range []string{"", "Tables", "tables", "schema..users", ".users", "Files/"} {
		_, _, err := parseTarget(bad)
		assert.Error(t, err, bad)
	}
}

func TestItemAndDirPaths(t *testing.T) {
	t.Parallel()

	d := &OneLakeDestination{lakehouse: "mylakehouse", relPath: "users"}
	assert.Equal(t, "mylakehouse.Lakehouse", d.itemPath())
	assert.Equal(t, "mylakehouse.Lakehouse/Tables/users", d.tableDir())
	assert.Equal(t, "mylakehouse.Lakehouse/Files/users", d.filesDir())

	// Already-typed item segment is preserved.
	d2 := &OneLakeDestination{lakehouse: "wh.Warehouse", relPath: "t"}
	assert.Equal(t, "wh.Warehouse", d2.itemPath())
}

func TestConnectBuildsClient(t *testing.T) {
	t.Parallel()

	d := NewOneLakeDestination()
	require.NoError(t, d.Connect(t.Context(), "onelake://ws/lh?sas_token=sig"))
	assert.Equal(t, "ws", d.workspace)
	assert.Equal(t, "lh", d.lakehouse)
	require.NotNil(t, d.client)

	d2 := NewOneLakeDestination()
	require.NoError(t, d2.Connect(t.Context(), "onelake://ws/lh?tenant_id=t&client_id=c&client_secret=s"))
	require.NotNil(t, d2.client)

	d3 := NewOneLakeDestination()
	require.Error(t, d3.Connect(t.Context(), "onelake://ws/lh?tenant_id=t&client_id=c"))
}

func TestDeltaTypeFor(t *testing.T) {
	t.Parallel()
	cases := map[schema.DataType]any{
		schema.TypeBoolean:     "boolean",
		schema.TypeInt16:       "short",
		schema.TypeInt32:       "integer",
		schema.TypeInt64:       "long",
		schema.TypeFloat32:     "float",
		schema.TypeFloat64:     "double",
		schema.TypeString:      "string",
		schema.TypeUUID:        "string",
		schema.TypeJSON:        "string",
		schema.TypeBinary:      "binary",
		schema.TypeDate:        "date",
		schema.TypeTime:        "long",
		schema.TypeTimestamp:   "timestamp",
		schema.TypeTimestampTZ: "timestamp",
	}
	for dt, want := range cases {
		assert.Equal(t, want, deltaTypeFor(schema.Column{DataType: dt}), dt)
	}

	assert.Equal(t, "decimal(10,2)", deltaTypeFor(schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2}))
	assert.Equal(t, "decimal(38,0)", deltaTypeFor(schema.Column{DataType: schema.TypeDecimal}))

	arr := deltaTypeFor(schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeString})
	m, ok := arr.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "array", m["type"])
	assert.Equal(t, "string", m["elementType"])
	assert.Equal(t, true, m["containsNull"])
}

func TestBuildSchemaString(t *testing.T) {
	t.Parallel()
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
	}
	s, err := buildSchemaString(cols)
	require.NoError(t, err)

	var parsed struct {
		Type   string `json:"type"`
		Fields []struct {
			Name     string `json:"name"`
			Type     any    `json:"type"`
			Nullable bool   `json:"nullable"`
			Metadata any    `json:"metadata"`
		} `json:"fields"`
	}
	require.NoError(t, json.Unmarshal([]byte(s), &parsed))
	assert.Equal(t, "struct", parsed.Type)
	require.Len(t, parsed.Fields, 2)
	assert.Equal(t, "id", parsed.Fields[0].Name)
	assert.Equal(t, "long", parsed.Fields[0].Type)
	assert.False(t, parsed.Fields[0].Nullable)
	assert.Equal(t, "name", parsed.Fields[1].Name)
	assert.Equal(t, "string", parsed.Fields[1].Type)
	assert.True(t, parsed.Fields[1].Nullable)
}

func parseCommitLines(t *testing.T, data []byte) []map[string]json.RawMessage {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]map[string]json.RawMessage, 0, len(lines))
	for _, l := range lines {
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(l), &m), "line: %s", l)
		require.Len(t, m, 1, "each action line has exactly one top-level key")
		out = append(out, m)
	}
	return out
}

func TestBuildInitialCommit(t *testing.T) {
	t.Parallel()
	cols := []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}}
	adds := []deltaAddFile{
		{Path: "part-00000-a.c000.snappy.parquet", Size: 123},
		{Path: "part-00001-b.c000.snappy.parquet", Size: 456},
	}

	data, err := buildInitialCommit(cols, adds, "table-uuid", 1700000000000)
	require.NoError(t, err)

	lines := parseCommitLines(t, data)
	// protocol, metaData, 2 adds, commitInfo
	require.Len(t, lines, 5)

	_, hasProtocol := lines[0]["protocol"]
	assert.True(t, hasProtocol)

	meta, hasMeta := lines[1]["metaData"]
	require.True(t, hasMeta)
	var metaObj struct {
		ID               string   `json:"id"`
		SchemaString     string   `json:"schemaString"`
		PartitionColumns []string `json:"partitionColumns"`
		CreatedTime      int64    `json:"createdTime"`
	}
	require.NoError(t, json.Unmarshal(meta, &metaObj))
	assert.Equal(t, "table-uuid", metaObj.ID)
	assert.Equal(t, int64(1700000000000), metaObj.CreatedTime)
	assert.NotNil(t, metaObj.PartitionColumns)
	assert.Empty(t, metaObj.PartitionColumns)
	assert.Contains(t, metaObj.SchemaString, "\"long\"")

	add0, hasAdd := lines[2]["add"]
	require.True(t, hasAdd)
	var addObj struct {
		Path       string `json:"path"`
		Size       int64  `json:"size"`
		DataChange bool   `json:"dataChange"`
	}
	require.NoError(t, json.Unmarshal(add0, &addObj))
	assert.Equal(t, "part-00000-a.c000.snappy.parquet", addObj.Path)
	assert.Equal(t, int64(123), addObj.Size)
	assert.True(t, addObj.DataChange)

	_, hasCommit := lines[4]["commitInfo"]
	assert.True(t, hasCommit)
}

func TestBuildAppendCommit(t *testing.T) {
	t.Parallel()
	adds := []deltaAddFile{{Path: "part-00000-x.parquet", Size: 10}}
	data, err := buildAppendCommit(adds, 1700000000000)
	require.NoError(t, err)

	lines := parseCommitLines(t, data)
	require.Len(t, lines, 2) // one add + commitInfo
	_, hasAdd := lines[0]["add"]
	assert.True(t, hasAdd)
	_, hasCommit := lines[1]["commitInfo"]
	assert.True(t, hasCommit)
	// No protocol/metaData on append commits.
	_, hasProtocol := lines[0]["protocol"]
	assert.False(t, hasProtocol)
}

func TestDeltaCommitRenameOptionsUsesIfNoneMatchAny(t *testing.T) {
	t.Parallel()

	opts := deltaCommitRenameOptions()
	require.NotNil(t, opts.AccessConditions)
	require.NotNil(t, opts.AccessConditions.ModifiedAccessConditions)
	require.NotNil(t, opts.AccessConditions.ModifiedAccessConditions.IfNoneMatch)
	assert.Equal(t, azcore.ETagAny, *opts.AccessConditions.ModifiedAccessConditions.IfNoneMatch)
}

func TestDeltaCommitTempPathStaysOutsideDeltaLog(t *testing.T) {
	t.Parallel()

	got := deltaCommitTempPath("lakehouse.Lakehouse/Tables/orders/_delta_log")
	assert.Contains(t, got, "lakehouse.Lakehouse/Tables/orders/_bruin_delta_tmp/")
	assert.NotContains(t, got, "/_delta_log/")
	assert.True(t, strings.HasSuffix(got, ".tmp"))
}

func TestCommitFileName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "00000000000000000000.json", commitFileName(0))
	assert.Equal(t, "00000000000000000001.json", commitFileName(1))
	assert.Equal(t, "00000000000000000042.json", commitFileName(42))
}

func TestRenderLayout(t *testing.T) {
	t.Parallel()
	d := &OneLakeDestination{relPath: "exports/users", layout: defaultLayout}
	got := d.renderLayout("abcd1234", 0)
	assert.Equal(t, "abcd1234.0.parquet", got)

	d2 := &OneLakeDestination{relPath: "exports/users", layout: "{table_name}.{ext}"}
	assert.Equal(t, "users.parquet", d2.renderLayout("x", 0))
}
