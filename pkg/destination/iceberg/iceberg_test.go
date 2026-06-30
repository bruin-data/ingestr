package iceberg

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestParseIcebergConfig(t *testing.T) {
	cfg, err := parseIcebergConfig("iceberg+rest://?uri=http://localhost:8181&warehouse=s3://bucket/wh&catalog_name=prod&region=eu-west-1&table.write.format.default=parquet&create_namespace=false")
	require.NoError(t, err)
	require.Equal(t, "prod", cfg.CatalogName)
	require.Equal(t, "rest", cfg.Properties["type"])
	require.Equal(t, "http://localhost:8181", cfg.Properties["uri"])
	require.Equal(t, "s3://bucket/wh", cfg.Properties["warehouse"])
	require.Equal(t, "eu-west-1", cfg.Properties["glue.region"])
	require.Equal(t, "eu-west-1", cfg.Properties["s3.region"])
	require.Equal(t, "parquet", cfg.TableProperties["write.format.default"])
	require.False(t, cfg.CreateNamespace)
}

func TestParseIcebergConfigFriendlyURIs(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		want     map[string]string
		wantProp map[string]string
	}{
		{
			name: "sqlite catalog with minio storage",
			uri:  "iceberg+sqlite:///tmp/iceberg/catalog.db?storage=s3&bucket=ingestr-iceberg&endpoint=localhost:9000&use_ssl=false&access_key_id=minioadmin&secret_access_key=minioadmin&region=us-east-1&table_path={namespace}/{table}",
			want: map[string]string{
				"table_location": "s3://ingestr-iceberg/{namespace}/{table}",
			},
			wantProp: map[string]string{
				"type":                 "sql",
				"uri":                  "file:/tmp/iceberg/catalog.db",
				"sql.driver":           "sqlite",
				"sql.dialect":          "sqlite",
				"warehouse":            "s3://ingestr-iceberg/",
				"s3.endpoint":          "http://localhost:9000",
				"s3.access-key-id":     "minioadmin",
				"s3.secret-access-key": "minioadmin",
				"s3.region":            "us-east-1",
			},
		},
		{
			name: "hadoop local warehouse path",
			uri:  "iceberg+hadoop:///tmp/iceberg-warehouse",
			wantProp: map[string]string{
				"type":      "hadoop",
				"warehouse": "/tmp/iceberg-warehouse",
			},
		},
		{
			name: "rest catalog host",
			uri:  "iceberg+rest://catalog.internal:8181?storage=s3&bucket=warehouse&prefix=prod&region=us-east-1",
			wantProp: map[string]string{
				"type":      "rest",
				"uri":       "http://catalog.internal:8181",
				"warehouse": "s3://warehouse/prod/",
				"s3.region": "us-east-1",
			},
		},
		{
			name: "hive metastore host",
			uri:  "iceberg+hive://localhost:9083?storage=s3&bucket=warehouse&endpoint=localhost:9000&use_ssl=false",
			wantProp: map[string]string{
				"type":        "hive",
				"uri":         "thrift://localhost:9083",
				"warehouse":   "s3://warehouse/",
				"s3.endpoint": "http://localhost:9000",
			},
		},
		{
			name: "glue catalog with s3 prefix",
			uri:  "iceberg+glue://?region=eu-west-1&storage=s3&bucket=company-lake&prefix=warehouse",
			wantProp: map[string]string{
				"type":        "glue",
				"warehouse":   "s3://company-lake/warehouse/",
				"glue.region": "eu-west-1",
				"s3.region":   "eu-west-1",
			},
		},
		{
			name: "postgres sql catalog",
			uri:  "iceberg+postgres://iceberg_user:secret@metadata-db.internal:5432/iceberg_catalog?sslmode=require&connect_timeout=5&storage=s3&bucket=company-lake&region=eu-west-1",
			wantProp: map[string]string{
				"type":        "sql",
				"uri":         "postgres://iceberg_user:secret@metadata-db.internal:5432/iceberg_catalog?connect_timeout=5&sslmode=require",
				"sql.driver":  "pgx",
				"sql.dialect": "postgres",
				"warehouse":   "s3://company-lake/",
				"s3.region":   "eu-west-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseIcebergConfig(tt.uri)
			require.NoError(t, err)
			for key, want := range tt.want {
				switch key {
				case "table_location":
					require.Equal(t, want, cfg.TableLocation)
				default:
					require.Failf(t, "unexpected config assertion", "unknown key %s", key)
				}
			}
			for key, want := range tt.wantProp {
				require.Equal(t, want, cfg.Properties[key], key)
			}
		})
	}
}

func TestDestinationConnectSQLiteCatalog(t *testing.T) {
	ctx := context.Background()
	dest := NewDestination()
	catalogDB := filepath.Join(t.TempDir(), "catalog.db")
	warehouse := t.TempDir()

	require.NoError(t, dest.Connect(ctx, "iceberg+sqlite://"+catalogDB+"?warehouse_path="+url.QueryEscape(warehouse)))
	require.NoError(t, dest.Close(ctx))
}

func TestIcebergSchemaFromTableSchemaNormalizesJSON(t *testing.T) {
	iceSchema, err := icebergSchemaFromTableSchema(&schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "payload", DataType: schema.TypeJSON, Nullable: true},
			{Name: "external_id", DataType: schema.TypeUUID, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	})
	require.NoError(t, err)
	require.Len(t, iceSchema.IdentifierFieldIDs, 1)

	payload, ok := iceSchema.FindFieldByName("payload")
	require.True(t, ok)
	require.Equal(t, "string", payload.Type.Type())

	externalID, ok := iceSchema.FindFieldByName("external_id")
	require.True(t, ok)
	require.Equal(t, "uuid", externalID.Type.Type())
}

func TestIcebergSchemaFromTableSchemaRejectsMissingPrimaryKey(t *testing.T) {
	_, err := icebergSchemaFromTableSchema(&schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys: []string{"missing_id"},
	})
	require.ErrorContains(t, err, `primary key "missing_id" is not present in schema`)
}

func TestIcebergSchemaFromTableSchemaRejectsInvalidPrimaryKeys(t *testing.T) {
	tests := []struct {
		name    string
		column  schema.Column
		wantErr string
	}{
		{
			name:    "nullable",
			column:  schema.Column{Name: "id", DataType: schema.TypeInt64, Nullable: true},
			wantErr: `primary key "id" must be non-nullable`,
		},
		{
			name:    "float",
			column:  schema.Column{Name: "id", DataType: schema.TypeFloat64, Nullable: false},
			wantErr: `primary key "id" cannot use floating-point type double`,
		},
		{
			name:    "array",
			column:  schema.Column{Name: "id", DataType: schema.TypeArray, ArrayType: schema.TypeString, Nullable: false},
			wantErr: `primary key "id" must be a primitive type, got list`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := icebergSchemaFromTableSchema(&schema.TableSchema{
				Columns:     []schema.Column{tt.column},
				PrimaryKeys: []string{"id"},
			})
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestTableSchemaFromIceberg(t *testing.T) {
	iceSchema, err := icebergSchemaFromTableSchema(&schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "amount", DataType: schema.TypeDecimal, Precision: 18, Scale: 2, Nullable: true},
			{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	})
	require.NoError(t, err)

	got, err := tableSchemaFromIceberg("analytics.orders", iceSchema)
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, got.PrimaryKeys)
	require.Equal(t, schema.TypeInt64, got.Columns[0].DataType)
	require.Equal(t, schema.TypeDecimal, got.Columns[1].DataType)
	require.Equal(t, 18, got.Columns[1].Precision)
	require.Equal(t, 2, got.Columns[1].Scale)
	require.Equal(t, schema.TypeArray, got.Columns[2].DataType)
	require.Equal(t, schema.TypeString, got.Columns[2].ArrayType)
}

func TestRecordBatchReaderNormalizesExtensionStorage(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	extBuilder := schema.NewJSONBuilder(mem)
	extBuilder.Append(`{"ok":true}`)
	jsonArr := extBuilder.NewArray()
	extBuilder.Release()

	inputSchema := arrow.NewSchema([]arrow.Field{{Name: "payload", Type: schema.JSONArrowType, Nullable: true}}, nil)
	batch := array.NewRecordBatch(inputSchema, []arrow.Array{jsonArr}, 1)
	jsonArr.Release()

	targetSchema := arrow.NewSchema([]arrow.Field{{Name: "payload", Type: arrow.BinaryTypes.String, Nullable: true}}, nil)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: batch}
	close(records)

	reader := newRecordBatchReader(context.Background(), records, targetSchema)
	defer reader.Release()

	require.True(t, reader.Next())
	got := reader.RecordBatch()
	require.Equal(t, targetSchema, got.Schema())
	require.Equal(t, arrow.STRING, got.Column(0).DataType().ID())
	require.False(t, reader.Next())
	require.NoError(t, reader.Err())
}

func TestRecordBatchReaderNormalizesUUIDStrings(t *testing.T) {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	defer builder.Release()
	builder.Append("550e8400-e29b-41d4-a716-446655440000")

	inputArr := builder.NewArray()
	defer inputArr.Release()

	inputSchema := arrow.NewSchema([]arrow.Field{{Name: "external_id", Type: arrow.BinaryTypes.String, Nullable: true}}, nil)
	batch := array.NewRecordBatch(inputSchema, []arrow.Array{inputArr}, 1)

	targetSchema := arrow.NewSchema([]arrow.Field{{Name: "external_id", Type: extensions.NewUUIDType(), Nullable: true}}, nil)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: batch}
	close(records)

	reader := newRecordBatchReader(context.Background(), records, targetSchema)
	defer reader.Release()

	require.True(t, reader.Next())
	got := reader.RecordBatch()
	require.Equal(t, targetSchema, got.Schema())
	require.IsType(t, &extensions.UUIDArray{}, got.Column(0))
	require.False(t, reader.Next())
	require.NoError(t, reader.Err())
}

func TestDestinationWritesAppendAndReplaceWithHadoopCatalog(t *testing.T) {
	ctx := context.Background()
	tableName := "lake.analytics.events"
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		},
	}
	evolvedSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "age", DataType: schema.TypeInt64, Nullable: true},
		},
	}

	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, "iceberg+hadoop://?warehouse="+url.QueryEscape(t.TempDir())))
	defer func() {
		require.NoError(t, dest.Close(ctx))
	}()

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       tableName,
		Schema:      tableSchema,
		PrimaryKeys: []string{"id"},
		PartitionBy: "id",
	}))
	gotSchema, err := dest.GetTableSchema(ctx, tableName)
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, gotSchema.PrimaryKeys)
	require.Equal(t, []string{"id"}, icebergPartitionFieldNames(ctx, t, dest, tableName))

	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1, 2)), destination.WriteOptions{
		Table:  tableName,
		Schema: tableSchema,
	}))
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, tableName))

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:  tableName,
		Schema: tableSchema,
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 3)), destination.WriteOptions{
		Table:  tableName,
		Schema: tableSchema,
	}))
	require.EqualValues(t, 3, icebergRowCount(ctx, t, dest, tableName))

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:  tableName,
		Schema: evolvedSchema,
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64PairBatch(t, "id", []int64{4}, "age", []int64{31})), destination.WriteOptions{
		Table:  tableName,
		Schema: evolvedSchema,
	}))
	require.EqualValues(t, 4, icebergRowCount(ctx, t, dest, tableName))
	gotSchema, err = dest.GetTableSchema(ctx, tableName)
	require.NoError(t, err)
	require.Contains(t, gotSchema.ColumnNames(), "age")

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     tableName,
		Schema:    tableSchema,
		DropFirst: true,
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 9)), destination.WriteOptions{
		Table:  tableName,
		Schema: tableSchema,
	}))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, tableName))
	gotSchema, err = dest.GetTableSchema(ctx, tableName)
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, gotSchema.ColumnNames())
	require.Empty(t, gotSchema.PrimaryKeys)
	require.Empty(t, icebergPartitionFieldNames(ctx, t, dest, tableName))
}

func TestDestinationMergeUnsupported(t *testing.T) {
	dest := NewDestination()

	require.False(t, dest.SupportsMergeStrategy())
	require.ErrorContains(t, dest.MergeTable(context.Background(), destination.MergeOptions{}), "merge strategy is not supported")
}

func TestDestinationReplaceAddsNewRequiredColumns(t *testing.T) {
	ctx := context.Background()
	tableName := "lake.analytics.required_events"
	initialSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		},
	}
	replacementSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "event_name", DataType: schema.TypeString, Nullable: false},
		},
	}

	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, "iceberg+hadoop://?warehouse="+url.QueryEscape(t.TempDir())))
	defer func() {
		require.NoError(t, dest.Close(ctx))
	}()

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:  tableName,
		Schema: initialSchema,
	}))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     tableName,
		Schema:    replacementSchema,
		DropFirst: true,
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64StringBatch(t, "id", []int64{1}, "event_name", []string{"created"})), destination.WriteOptions{
		Table:  tableName,
		Schema: replacementSchema,
	}))

	gotSchema, err := dest.GetTableSchema(ctx, tableName)
	require.NoError(t, err)
	require.False(t, icebergColumn(t, gotSchema, "event_name").Nullable)
}

func TestDestinationReplaceWriteFailureDoesNotMutateExistingMetadata(t *testing.T) {
	ctx := context.Background()
	tableName := "lake.analytics.failed_replace_events"
	initialSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "age", DataType: schema.TypeInt64, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}
	replacementSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		},
	}

	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, "iceberg+hadoop://?warehouse="+url.QueryEscape(t.TempDir())))
	defer func() {
		require.NoError(t, dest.Close(ctx))
	}()

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       tableName,
		Schema:      initialSchema,
		PrimaryKeys: []string{"id"},
		PartitionBy: "id",
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64PairBatch(t, "id", []int64{1}, "age", []int64{31})), destination.WriteOptions{
		Table:  tableName,
		Schema: initialSchema,
	}))

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     tableName,
		Schema:    replacementSchema,
		DropFirst: true,
	}))

	writeErr := errors.New("reader failed")
	err := dest.WriteParallel(ctx, errorRecordBatches(writeErr), destination.WriteOptions{
		Table:  tableName,
		Schema: replacementSchema,
	})
	require.ErrorIs(t, err, writeErr)

	gotSchema, err := dest.GetTableSchema(ctx, tableName)
	require.NoError(t, err)
	require.Equal(t, []string{"id", "age"}, gotSchema.ColumnNames())
	require.Equal(t, []string{"id"}, gotSchema.PrimaryKeys)
	require.Equal(t, []string{"id"}, icebergPartitionFieldNames(ctx, t, dest, tableName))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, tableName))
}

func int64Batch(t *testing.T, values ...int64) arrow.RecordBatch {
	t.Helper()

	builder := array.NewInt64Builder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues(values, nil)

	arr := builder.NewArray()
	defer arr.Release()

	arrowSchema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	return array.NewRecordBatch(arrowSchema, []arrow.Array{arr}, int64(len(values)))
}

func int64PairBatch(t *testing.T, firstName string, firstValues []int64, secondName string, secondValues []int64) arrow.RecordBatch {
	t.Helper()
	require.Len(t, secondValues, len(firstValues))

	firstBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer firstBuilder.Release()
	firstBuilder.AppendValues(firstValues, nil)
	firstArr := firstBuilder.NewArray()
	defer firstArr.Release()

	secondBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer secondBuilder.Release()
	secondBuilder.AppendValues(secondValues, nil)
	secondArr := secondBuilder.NewArray()
	defer secondArr.Release()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: firstName, Type: arrow.PrimitiveTypes.Int64},
		{Name: secondName, Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	return array.NewRecordBatch(arrowSchema, []arrow.Array{firstArr, secondArr}, int64(len(firstValues)))
}

func int64StringBatch(t *testing.T, firstName string, firstValues []int64, secondName string, secondValues []string) arrow.RecordBatch {
	t.Helper()
	require.Len(t, secondValues, len(firstValues))

	firstBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer firstBuilder.Release()
	firstBuilder.AppendValues(firstValues, nil)
	firstArr := firstBuilder.NewArray()
	defer firstArr.Release()

	secondBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	defer secondBuilder.Release()
	secondBuilder.AppendValues(secondValues, nil)
	secondArr := secondBuilder.NewArray()
	defer secondArr.Release()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: firstName, Type: arrow.PrimitiveTypes.Int64},
		{Name: secondName, Type: arrow.BinaryTypes.String, Nullable: false},
	}, nil)
	return array.NewRecordBatch(arrowSchema, []arrow.Array{firstArr, secondArr}, int64(len(firstValues)))
}

func icebergColumn(t *testing.T, tableSchema *schema.TableSchema, name string) schema.Column {
	t.Helper()
	for _, col := range tableSchema.Columns {
		if col.Name == name {
			return col
		}
	}
	require.Failf(t, "missing column", "column %q not found", name)
	return schema.Column{}
}

func recordBatches(batches ...arrow.RecordBatch) <-chan source.RecordBatchResult {
	records := make(chan source.RecordBatchResult, len(batches))
	for _, batch := range batches {
		records <- source.RecordBatchResult{Batch: batch}
	}
	close(records)
	return records
}

func errorRecordBatches(err error) <-chan source.RecordBatchResult {
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Err: err}
	close(records)
	return records
}

func icebergRowCount(ctx context.Context, t *testing.T, dest *Destination, tableName string) int64 {
	t.Helper()

	ident, err := parseIdentifier(tableName)
	require.NoError(t, err)

	tbl, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)

	tasks, err := tbl.Scan().PlanFiles(ctx)
	require.NoError(t, err)

	var rows int64
	for _, task := range tasks {
		rows += task.File.Count()
	}
	return rows
}

func icebergPartitionFieldNames(ctx context.Context, t *testing.T, dest *Destination, tableName string) []string {
	t.Helper()

	ident, err := parseIdentifier(tableName)
	require.NoError(t, err)

	tbl, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)

	spec := tbl.Metadata().PartitionSpec()
	names := make([]string, 0)
	for _, field := range spec.Fields() {
		names = append(names, field.Name)
	}
	return names
}
