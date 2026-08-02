package onelake

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
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

func TestDeltaMetadataEvolvesWhenSourceAddsColumn(t *testing.T) {
	t.Parallel()

	metadata, err := newDeltaMetadata([]schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
	}, "table-id", 1700000000000)
	require.NoError(t, err)
	metadata["description"] = json.RawMessage(`"raw landing table"`)

	deltaSchema, err := deltaSchemaFromMetadata(metadata)
	require.NoError(t, err)
	destinationSchema, err := tableSchemaFromDelta("Tables/users", deltaSchema)
	require.NoError(t, err)
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
		{Name: "email", DataType: schema.TypeString, Nullable: false},
	}}
	normalizer := (&OneLakeDestination{}).NormalizeSchemaEvolutionColumn
	comparison, err := schemaevolution.Compare(sourceSchema, destinationSchema, &schemaevolution.CompareOptions{
		NormalizeColumn: normalizer,
	})
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, schemaevolution.ChangeAddColumn, comparison.Changes[0].Type)

	updated, changed, err := evolveDeltaMetadata(metadata, comparison)
	require.NoError(t, err)
	require.True(t, changed)
	assert.JSONEq(t, `"raw landing table"`, string(updated["description"]))

	evolvedSchema, err := deltaSchemaFromMetadata(updated)
	require.NoError(t, err)
	require.Len(t, evolvedSchema.Fields, 3)
	assert.Equal(t, "email", evolvedSchema.Fields[2].Name)
	assert.Equal(t, "string", evolvedSchema.Fields[2].Type)
	assert.True(t, evolvedSchema.Fields[2].Nullable, "new columns must be nullable for pre-evolution files")

	originalSchema, err := deltaSchemaFromMetadata(metadata)
	require.NoError(t, err)
	require.Len(t, originalSchema.Fields, 2, "evolution must not mutate the snapshot used to build the plan")
}

func TestBuildSchemaEvolutionCommitContainsMetadataOnly(t *testing.T) {
	t.Parallel()

	metadata, err := newDeltaMetadata([]schema.Column{{Name: "id", DataType: schema.TypeInt64}}, "table-id", 1)
	require.NoError(t, err)
	commit, err := buildSchemaEvolutionCommit(metadata, 1700000000000)
	require.NoError(t, err)

	lines := parseCommitLines(t, commit)
	require.Len(t, lines, 2)
	_, hasMetadata := lines[0]["metaData"]
	assert.True(t, hasMetadata)
	_, hasAdd := lines[0]["add"]
	assert.False(t, hasAdd)

	var info struct {
		Operation string `json:"operation"`
	}
	require.NoError(t, json.Unmarshal(lines[1]["commitInfo"], &info))
	assert.Equal(t, "ALTER TABLE", info.Operation)
}

func TestReplayDeltaCommitsKeepsLatestSchemaAndActiveFiles(t *testing.T) {
	t.Parallel()

	initial, err := buildInitialCommit(
		[]schema.Column{{Name: "id", DataType: schema.TypeInt64}},
		[]deltaAddFile{{Path: "part-a.parquet", Size: 10}},
		"table-id",
		1,
	)
	require.NoError(t, err)
	metadata, err := newDeltaMetadata([]schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "email", DataType: schema.TypeString, Nullable: true},
	}, "table-id", 1)
	require.NoError(t, err)
	evolution, err := buildSchemaEvolutionCommit(metadata, 2)
	require.NoError(t, err)
	rewrite, err := buildRewriteCommit(
		[]string{"part-a.parquet"},
		[]deltaAddFile{{Path: "part-b.parquet", Size: 20}},
		"MERGE",
		3,
	)
	require.NoError(t, err)

	snapshot := &deltaSnapshot{exists: true}
	active := make(map[string]bool)
	var order []string
	for _, commit := range [][]byte{initial, evolution, rewrite} {
		require.NoError(t, replayDeltaCommit(snapshot, active, &order, commit))
	}

	var activeFiles []string
	for _, path := range order {
		if _, ok := active[path]; ok {
			activeFiles = append(activeFiles, path)
		}
	}
	assert.Equal(t, []string{"part-b.parquet"}, activeFiles)
	deltaSchema, err := deltaSchemaFromMetadata(snapshot.metadata)
	require.NoError(t, err)
	require.Len(t, deltaSchema.Fields, 2)
	assert.Equal(t, "email", deltaSchema.Fields[1].Name)
	require.NotNil(t, snapshot.protocol)
	assert.Equal(t, 1, snapshot.protocol.MinReaderVersion)
	assert.Equal(t, 2, snapshot.protocol.MinWriterVersion)
}

func TestReplayDeltaCommitTracksDeletionVectors(t *testing.T) {
	t.Parallel()

	commit := []byte(`{"protocol":{"minReaderVersion":3,"minWriterVersion":7,"readerFeatures":["deletionVectors"],"writerFeatures":["deletionVectors"]}}
{"add":{"path":"part-plain.parquet","size":1,"dataChange":true}}
{"add":{"path":"part-null-dv.parquet","size":1,"dataChange":true,"deletionVector":null}}
{"add":{"path":"part-dv.parquet","size":1,"dataChange":true,"deletionVector":{"storageType":"u","pathOrInlineDv":"x","offset":1,"sizeInBytes":36,"cardinality":2}}}
`)

	snapshot := &deltaSnapshot{exists: true}
	active := make(map[string]bool)
	var order []string
	require.NoError(t, replayDeltaCommit(snapshot, active, &order, commit))

	assert.False(t, active["part-plain.parquet"])
	assert.False(t, active["part-null-dv.parquet"])
	assert.True(t, active["part-dv.parquet"])
	require.NotNil(t, snapshot.protocol)
	assert.Equal(t, []string{"deletionVectors"}, snapshot.protocol.ReaderFeatures)

	// A later add of the same file without a deletion vector replaces the entry.
	require.NoError(t, replayDeltaCommit(snapshot, active, &order,
		[]byte(`{"add":{"path":"part-dv.parquet","size":1,"dataChange":true}}`)))
	assert.False(t, active["part-dv.parquet"])
}

func TestCheckSupportedDeltaProtocol(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		protocol *deltaProtocol
		wantErr  string
	}{
		{"missing", nil, "carries no protocol action"},
		{"legacy", &deltaProtocol{MinReaderVersion: 1, MinWriterVersion: 2}, ""},
		{
			"table features within allowlist",
			&deltaProtocol{
				MinReaderVersion: 3, MinWriterVersion: 7,
				ReaderFeatures: []string{"deletionVectors", "timestampNtz"},
				WriterFeatures: []string{"deletionVectors", "appendOnly", "invariants"},
			},
			"",
		},
		{
			"unknown reader feature",
			&deltaProtocol{MinReaderVersion: 3, MinWriterVersion: 7, ReaderFeatures: []string{"typeWidening"}},
			`reader feature "typeWidening"`,
		},
		{
			"unknown writer feature",
			&deltaProtocol{MinReaderVersion: 3, MinWriterVersion: 7, WriterFeatures: []string{"icebergCompatV2"}},
			`writer feature "icebergCompatV2"`,
		},
		{
			"future versions",
			&deltaProtocol{MinReaderVersion: 4, MinWriterVersion: 8},
			"reader version 4 and writer version 8",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkSupportedDeltaProtocol(tt.protocol, "merge")
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestEvolveDeltaMetadataRelaxesColumnsAndRejectsTypeChanges(t *testing.T) {
	t.Parallel()

	metadata, err := newDeltaMetadata([]schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}, "table-id", 1)
	require.NoError(t, err)
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeRemoveColumn, ColumnName: "id",
	}}}

	updated, changed, err := evolveDeltaMetadata(metadata, comparison)
	require.NoError(t, err)
	require.True(t, changed)
	deltaSchema, err := deltaSchemaFromMetadata(updated)
	require.NoError(t, err)
	assert.True(t, deltaSchema.Fields[0].Nullable)

	oldColumn := schema.Column{Name: "id", DataType: schema.TypeInt64}
	_, changed, err = evolveDeltaMetadata(metadata, &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type: schemaevolution.ChangeWidenType, ColumnName: "id", OldColumn: &oldColumn,
			NewColumn: schema.Column{Name: "id", DataType: schema.TypeString},
		}},
	})
	require.ErrorContains(t, err, "requires changing its Delta type from long to string")
	assert.False(t, changed)
}

func TestEvolveDeltaMetadataRejectsUnmappableExistingColumn(t *testing.T) {
	t.Parallel()

	// tableSchemaFromDelta omits the Fabric-authored struct column, so a source
	// column with the same name arrives as an add; it must fail loudly instead
	// of redeclaring the struct as a string.
	metadata, err := newDeltaMetadata(nil, "table-id", 1)
	require.NoError(t, err)
	metadata["schemaString"], err = json.Marshal(`{"type":"struct","fields":[` +
		`{"name":"profile","type":{"type":"struct","fields":[]},"nullable":true,"metadata":{}}]}`)
	require.NoError(t, err)

	_, _, err = evolveDeltaMetadata(metadata, &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type: schemaevolution.ChangeAddColumn, ColumnName: "profile",
			NewColumn: schema.Column{Name: "profile", DataType: schema.TypeString},
		}},
	})
	require.ErrorContains(t, err, `column "profile"`)
	require.ErrorContains(t, err, "unsupported delta type")

	// An unknown column must not be mistaken for a string just because that is
	// deltaTypeFor's fallback.
	_, _, err = evolveDeltaMetadata(metadata, &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type: schemaevolution.ChangeWidenType, ColumnName: "profile",
			OldColumn: &schema.Column{Name: "profile", DataType: schema.TypeUnknown, Nullable: true},
			NewColumn: schema.Column{Name: "profile", DataType: schema.TypeString},
		}},
	})
	require.ErrorContains(t, err, `requires changing its Delta type from {"fields":[],"type":"struct"} to string`)
}

func TestTableSchemaFromDeltaSkipsUnmappableColumns(t *testing.T) {
	t.Parallel()

	deltaSchema := &deltaStruct{Type: "struct", Fields: []deltaField{
		{Name: "id", Type: "long"},
		{Name: "profile", Type: map[string]any{"type": "struct", "fields": []any{}}, Nullable: true},
	}}

	tableSchema, err := tableSchemaFromDelta("Tables/users", deltaSchema)
	require.NoError(t, err, "a Delta type ingestr cannot map must not fail the whole load")
	require.Len(t, tableSchema.Columns, 1,
		"unmappable columns must stay out of the write schema so appended files omit them")
	assert.Equal(t, schema.TypeInt64, tableSchema.Columns[0].DataType)
}

// TestArrowFieldToColumnRoundTripsDeltaTypes pins the two directions of the
// type map against each other. The first copy-on-write commit derives the Delta
// metadata from the Arrow schema of the file it just wrote, and every later run
// aligns that file to the declared metadata, so a column that does not round
// trip breaks the table on its second run.
func TestArrowFieldToColumnRoundTripsDeltaTypes(t *testing.T) {
	t.Parallel()

	columns := []schema.Column{
		{Name: "bool", DataType: schema.TypeBoolean},
		{Name: "i8", DataType: schema.TypeInt8},
		{Name: "i16", DataType: schema.TypeInt16},
		{Name: "i32", DataType: schema.TypeInt32},
		{Name: "i64", DataType: schema.TypeInt64},
		{Name: "f32", DataType: schema.TypeFloat32},
		{Name: "f64", DataType: schema.TypeFloat64},
		{Name: "dec", DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
		{Name: "dec_default", DataType: schema.TypeDecimal},
		{Name: "str", DataType: schema.TypeString, MaxLength: 64},
		{Name: "json", DataType: schema.TypeJSON},
		{Name: "uuid", DataType: schema.TypeUUID},
		{Name: "bin", DataType: schema.TypeBinary},
		{Name: "date", DataType: schema.TypeDate},
		{Name: "time", DataType: schema.TypeTime},
		{Name: "ts", DataType: schema.TypeTimestamp},
		{Name: "tstz", DataType: schema.TypeTimestampTZ},
		{Name: "arr_str", DataType: schema.TypeArray, ArrayType: schema.TypeString},
		{Name: "arr_i64", DataType: schema.TypeArray, ArrayType: schema.TypeInt64},
		{Name: "arr_dec", DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 10, Scale: 2},
		{Name: "arr_ts", DataType: schema.TypeArray, ArrayType: schema.TypeTimestampTZ},
	}

	for _, column := range columns {
		t.Run(column.Name, func(t *testing.T) {
			roundTripped := arrowFieldToColumn(arrow.Field{
				Name: column.Name, Type: schema.DataTypeToArrowType(column),
			})
			assert.Equal(t, deltaTypeFor(column), deltaTypeFor(roundTripped))
		})
	}
}

func TestDeltaTypeForArrayKeepsElementPrecision(t *testing.T) {
	t.Parallel()

	deltaType := deltaTypeFor(schema.Column{
		DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 10, Scale: 2,
	})
	encoded, err := json.Marshal(deltaType)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"array","elementType":"decimal(10,2)","containsNull":true}`, string(encoded))
}

func TestEvolveDeltaMetadataAllowsMissingConfiguration(t *testing.T) {
	t.Parallel()

	metadata, err := newDeltaMetadata([]schema.Column{{Name: "id", DataType: schema.TypeInt64}}, "table-id", 1)
	require.NoError(t, err)
	delete(metadata, "configuration")
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type:       schemaevolution.ChangeAddColumn,
		ColumnName: "email",
		NewColumn:  schema.Column{Name: "email", DataType: schema.TypeString},
	}}}

	updated, changed, err := evolveDeltaMetadata(metadata, comparison)
	require.NoError(t, err)
	require.True(t, changed)
	evolved, err := deltaSchemaFromMetadata(updated)
	require.NoError(t, err)
	require.Len(t, evolved.Fields, 2)
}

// TestEvolveDeltaMetadataKeepsFieldMetadataObject covers tables written by
// other engines, which may omit the per-field metadata object the Delta
// protocol requires. Committing it back as null makes the table unreadable.
func TestEvolveDeltaMetadataKeepsFieldMetadataObject(t *testing.T) {
	t.Parallel()

	metadata, err := newDeltaMetadata([]schema.Column{{Name: "id", DataType: schema.TypeInt64}}, "table-id", 1)
	require.NoError(t, err)
	schemaString, err := json.Marshal(`{"type":"struct","fields":[{"name":"id","type":"long","nullable":true}]}`)
	require.NoError(t, err)
	metadata["schemaString"] = schemaString
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type:       schemaevolution.ChangeAddColumn,
		ColumnName: "email",
		NewColumn:  schema.Column{Name: "email", DataType: schema.TypeString},
	}}}

	updated, changed, err := evolveDeltaMetadata(metadata, comparison)
	require.NoError(t, err)
	require.True(t, changed)

	var encoded string
	require.NoError(t, json.Unmarshal(updated["schemaString"], &encoded))
	assert.NotContains(t, encoded, `"metadata":null`)
	assert.JSONEq(t, `{"type":"struct","fields":[`+
		`{"name":"id","type":"long","nullable":true,"metadata":{}},`+
		`{"name":"email","type":"string","nullable":true,"metadata":{}}]}`, encoded)
}

func TestNormalizeSchemaEvolutionColumn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   schema.Column
		want schema.Column
	}{
		// Delta has no JSON, UUID, TIME or naive timestamp type, so these are
		// the columns GetTableSchema reports back differently from what the
		// source declared. Without the normalization the second run of a
		// pipeline sees a type change and fails.
		{"json", schema.Column{DataType: schema.TypeJSON}, schema.Column{DataType: schema.TypeString}},
		{"uuid", schema.Column{DataType: schema.TypeUUID}, schema.Column{DataType: schema.TypeString}},
		{"time", schema.Column{DataType: schema.TypeTime}, schema.Column{DataType: schema.TypeInt64}},
		{"timestamp", schema.Column{DataType: schema.TypeTimestamp}, schema.Column{DataType: schema.TypeTimestampTZ}},
		{"timestamptz", schema.Column{DataType: schema.TypeTimestampTZ}, schema.Column{DataType: schema.TypeTimestampTZ}},
		{
			"max length is not carried by delta",
			schema.Column{DataType: schema.TypeString, MaxLength: 64},
			schema.Column{DataType: schema.TypeString},
		},
		{
			"decimal keeps precision and scale",
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
			schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
		},
		{
			"array element",
			schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeJSON},
			schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeString},
		},
		{
			"array of time",
			schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeTime},
			schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt64},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, normalizeSchemaEvolutionColumn(tt.in))
		})
	}
}

// TestNormalizedSchemaSurvivesTheNextRun ties the normalizer to what it exists
// for: the schema GetTableSchema reports must compare equal to the source
// schema that produced the table, or every run after the first fails.
func TestNormalizedSchemaSurvivesTheNextRun(t *testing.T) {
	t.Parallel()

	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "ID", DataType: schema.TypeInt64},
		{Name: "PAYLOAD", DataType: schema.TypeJSON},
		{Name: "EXTERNAL_ID", DataType: schema.TypeUUID},
		{Name: "DURATION", DataType: schema.TypeTime},
		{Name: "SEEN_AT", DataType: schema.TypeTimestamp},
		{Name: "NAME", DataType: schema.TypeString, MaxLength: 64},
	}}
	metadata, err := newDeltaMetadata(sourceSchema.Columns, "table-id", 1)
	require.NoError(t, err)
	deltaSchema, err := deltaSchemaFromMetadata(metadata)
	require.NoError(t, err)
	reported, err := tableSchemaFromDelta("analytics.events", deltaSchema)
	require.NoError(t, err)

	dest := &OneLakeDestination{}
	comparison, err := schemaevolution.Compare(sourceSchema, reported, &schemaevolution.CompareOptions{
		NormalizeColumn: dest.NormalizeSchemaEvolutionColumn,
	})
	require.NoError(t, err)
	assert.False(t, comparison.HasChanges, "the lossy Delta types must not read back as schema changes: %+v", comparison.Changes)
}

func TestEvolveDeltaMetadataRejectsColumnMapping(t *testing.T) {
	t.Parallel()

	metadata, err := newDeltaMetadata([]schema.Column{{Name: "id", DataType: schema.TypeInt64}}, "table-id", 1)
	require.NoError(t, err)
	metadata["configuration"] = json.RawMessage(`{"delta.columnMapping.mode":"name"}`)
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type:       schemaevolution.ChangeAddColumn,
		ColumnName: "email",
		NewColumn:  schema.Column{Name: "email", DataType: schema.TypeString},
	}}}

	_, changed, err := evolveDeltaMetadata(metadata, comparison)
	require.ErrorContains(t, err, "column mapping mode")
	assert.False(t, changed)
}
