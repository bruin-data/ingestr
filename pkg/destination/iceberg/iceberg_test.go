package iceberg

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
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
			uri:  "iceberg+postgres://iceberg_user@metadata-db.internal:5432/iceberg_catalog?password=secret&sslmode=require&connect_timeout=5&storage=s3&bucket=company-lake&region=eu-west-1",
			wantProp: map[string]string{
				"type":        "sql",
				"uri":         "postgres://iceberg_user@metadata-db.internal:5432/iceberg_catalog?connect_timeout=5&password=secret&sslmode=require",
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

func TestDestinationDropTablePurgesSQLCatalogFiles(t *testing.T) {
	ctx := context.Background()
	dest := NewDestination()
	root := t.TempDir()
	catalogDB := filepath.Join(root, "catalog.db")
	warehouse := filepath.Join(root, "warehouse")
	require.NoError(t, dest.Connect(ctx, "iceberg+sqlite://"+catalogDB+"?warehouse_path="+url.QueryEscape(warehouse)))
	t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })

	tableName := "lake.cleanup.managed_staging"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: tableName, Schema: tableSchema, ExpiresAfter: time.Hour,
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
		Table: tableName, Schema: tableSchema,
	}))

	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	location, ok := localFilesystemPath(tbl.Location())
	require.True(t, ok)
	require.NotEmpty(t, regularFiles(t, location))

	require.NoError(t, dest.DropTable(ctx, tableName))
	exists, err := dest.catalog.CheckTableExists(ctx, icebergCatalogIdentifier(t, tableName))
	require.NoError(t, err)
	require.False(t, exists, "purge must remove the catalog entry")
	require.Empty(t, regularFiles(t, location), "purge must remove the table's data and metadata objects")
}

func TestDestinationPurgesExpiredManagedTables(t *testing.T) {
	ctx := context.Background()
	dest := NewDestination()
	root := t.TempDir()
	catalogDB := filepath.Join(root, "catalog.db")
	warehouse := filepath.Join(root, "warehouse")
	require.NoError(t, dest.Connect(ctx, "iceberg+sqlite://"+catalogDB+"?warehouse_path="+url.QueryEscape(warehouse)))
	t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })

	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	expiredTable := "lake.cleanup.expired_staging"
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: expiredTable, Schema: tableSchema, ExpiresAfter: time.Hour,
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
		Table: expiredTable, Schema: tableSchema,
	}))

	tbl, err := dest.loadIcebergTable(ctx, expiredTable)
	require.NoError(t, err)
	expiresAtMillis, err := strconv.ParseInt(tbl.Properties().Get(managedExpiresAtProperty, ""), 10, 64)
	require.NoError(t, err)
	require.True(t, time.UnixMilli(expiresAtMillis).After(time.Now()))
	location, ok := localFilesystemPath(tbl.Location())
	require.True(t, ok)
	require.NotEmpty(t, regularFiles(t, location))

	txn := tbl.NewTransaction()
	require.NoError(t, txn.SetProperties(map[string]string{managedExpiresAtProperty: "0"}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: "lake.cleanup.current_staging", Schema: tableSchema, ExpiresAfter: time.Hour,
	}))
	exists, err := dest.catalog.CheckTableExists(ctx, icebergCatalogIdentifier(t, expiredTable))
	require.NoError(t, err)
	require.False(t, exists, "preparing managed staging should remove expired catalog entries in its namespace")
	require.Empty(t, regularFiles(t, location), "expiry cleanup must purge underlying objects")
}

func icebergCatalogIdentifier(t *testing.T, table string) []string {
	t.Helper()
	ident, err := parseIdentifier(table)
	require.NoError(t, err)
	return ident
}

func regularFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return fs.SkipDir
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(t, err)
	}
	return files
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

func TestValidateRequiredColumnsReportsEveryViolation(t *testing.T) {
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendNull()

	first := builder.NewArray()
	defer first.Release()
	builder.AppendNull()
	second := builder.NewArray()
	defer second.Release()

	batch := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "tenant_id", Type: arrow.PrimitiveTypes.Int64},
	}, nil), []arrow.Array{first, second}, 1)
	defer batch.Release()

	err := validateRequiredColumns(batch, []requiredColumn{
		{name: "id", index: 0},
		{name: "tenant_id", index: 1},
	})
	require.ErrorContains(t, err, `required field "id" contains 1 NULL value(s)`)
	require.ErrorContains(t, err, `required field "tenant_id" contains 1 NULL value(s)`)
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
	_, err = dest.ApplySchemaEvolution(ctx, tableName, addColumnComparison(schema.Column{
		Name:     "age",
		DataType: schema.TypeInt64,
		Nullable: true,
	}))
	require.NoError(t, err)
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

func TestDestinationDirectReplaceDeduplicatesPrimaryKeys(t *testing.T) {
	withSpillRunRows(t, 2)

	ctx := context.Background()
	tableName := "lake.analytics.deduplicated_replace"
	tableSchema := mergeTestSchema()
	tableSchema.PrimaryKeys = []string{"id"}
	dest := newHadoopDestination(t)

	writeTableRows(t, dest, tableName, tableSchema, false, [][]any{
		{int64(99), "old-snapshot", 99.0, int64(99)},
	})

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       tableName,
		Schema:      tableSchema,
		DropFirst:   true,
		PrimaryKeys: []string{"id"},
	}))

	first, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "v1-old", 10.0, int64(1)},
		{int64(2), "v2", 20.0, int64(2)},
		{int64(1), "v1-latest", 11.0, int64(3)},
	})
	require.NoError(t, err)
	second, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(3), "v3-latest", 31.0, int64(4)},
		{int64(3), "v3-old", 30.0, int64(5)},
	})
	require.NoError(t, err)

	require.NoError(t, dest.WriteParallel(ctx, recordBatches(append(first, second...)...), destination.WriteOptions{
		Table:                  tableName,
		Schema:                 tableSchema,
		PrimaryKeys:            []string{"id"},
		DeduplicatePrimaryKeys: true,
		IncrementalKey:         "score",
	}))

	rows := readTableRows(t, dest, tableName)
	byID := singleRowByKey(t, rows, "id")
	require.Len(t, byID, 3)
	require.Equal(t, "v1-latest", rows.Value(byID[int64(1)], "name"))
	require.Equal(t, "v3-latest", rows.Value(byID[int64(3)], "name"))
	require.NotContains(t, byID, int64(99), "replace must remove the previous snapshot")
}

func TestDestinationStrategySupport(t *testing.T) {
	dest := NewDestination()

	require.True(t, dest.SupportsReplaceStrategy())
	require.True(t, dest.SupportsAppendStrategy())
	require.True(t, dest.SupportsMergeStrategy())
	require.True(t, dest.SupportsDeleteInsertStrategy())
	require.True(t, dest.SupportsSCD2Strategy())
	require.True(t, dest.SupportsCDCMerge())
	require.True(t, dest.SupportsCDCUnchangedCols())
	require.False(t, dest.SupportsAtomicSwap())
	require.True(t, dest.SupportsDirectReplaceDeduplication())

	require.ErrorContains(t, dest.SwapTable(context.Background(), destination.SwapOptions{}), "does not support atomic table swap")
	require.ErrorContains(t, dest.MergeTable(context.Background(), destination.MergeOptions{PrimaryKeys: []string{"id"}}), "not connected")
	require.ErrorContains(t, dest.DeleteInsertTable(context.Background(), destination.DeleteInsertOptions{IncrementalKey: "id"}), "not connected")
	require.ErrorContains(t, dest.SCD2Table(context.Background(), destination.SCD2Options{PrimaryKeys: []string{"id"}}), "not connected")
	require.ErrorContains(t, dest.TruncateTable(context.Background(), "ns.t"), "not connected")
}

func TestDestinationApplySchemaEvolutionPromotesColumnType(t *testing.T) {
	ctx := context.Background()
	tableName := "lake.analytics.promoted_events"
	initialSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32, Nullable: false},
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
	_, err := dest.ApplySchemaEvolution(ctx, tableName, &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type:       schemaevolution.ChangeWidenType,
			ColumnName: "id",
			OldColumn:  &initialSchema.Columns[0],
			NewColumn:  schema.Column{Name: "id", DataType: schema.TypeInt64, Nullable: true},
		}},
	})
	require.NoError(t, err)

	gotSchema, err := dest.GetTableSchema(ctx, tableName)
	require.NoError(t, err)
	require.Equal(t, schema.TypeInt64, icebergColumn(t, gotSchema, "id").DataType)
	require.True(t, icebergColumn(t, gotSchema, "id").Nullable)
}

func TestDestinationApplySchemaEvolutionPromotesArrayElementType(t *testing.T) {
	ctx := context.Background()
	tableName := "lake.analytics.promoted_array_events"
	initial := schema.Column{Name: "values", DataType: schema.TypeArray, ArrayType: schema.TypeInt32, Nullable: true}

	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, "iceberg+hadoop://?warehouse="+url.QueryEscape(t.TempDir())))
	defer func() { require.NoError(t, dest.Close(ctx)) }()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: tableName, Schema: &schema.TableSchema{Columns: []schema.Column{initial}},
	}))

	_, err := dest.ApplySchemaEvolution(ctx, tableName, &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type: schemaevolution.ChangeWidenType, ColumnName: "values", OldColumn: &initial,
			NewColumn: schema.Column{Name: "values", DataType: schema.TypeArray, ArrayType: schema.TypeInt64, Nullable: true},
		}},
	})
	require.NoError(t, err)

	got, err := dest.GetTableSchema(ctx, tableName)
	require.NoError(t, err)
	require.Equal(t, schema.TypeInt64, icebergColumn(t, got, "values").ArrayType)
}

func TestDestinationApplySchemaEvolutionRelaxesRequiredColumns(t *testing.T) {
	ctx := context.Background()
	tableName := "lake.analytics.relaxed_events"
	relaxed := schema.Column{Name: "relaxed", DataType: schema.TypeString, Nullable: false}
	removed := schema.Column{Name: "removed", DataType: schema.TypeString, Nullable: false}

	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, "iceberg+hadoop://?warehouse="+url.QueryEscape(t.TempDir())))
	defer func() { require.NoError(t, dest.Close(ctx)) }()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: tableName, Schema: &schema.TableSchema{Columns: []schema.Column{relaxed, removed}},
	}))

	_, err := dest.ApplySchemaEvolution(ctx, tableName, &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeRelaxNullability, ColumnName: "relaxed", OldColumn: &relaxed},
			{Type: schemaevolution.ChangeRemoveColumn, ColumnName: "removed", OldColumn: &removed},
		},
	})
	require.NoError(t, err)

	got, err := dest.GetTableSchema(ctx, tableName)
	require.NoError(t, err)
	require.True(t, icebergColumn(t, got, "relaxed").Nullable)
	require.True(t, icebergColumn(t, got, "removed").Nullable)
}

func TestDestinationAppendReturnsSourceErrorAfterBatch(t *testing.T) {
	ctx := context.Background()
	tableName := "lake.analytics.failed_append_events"
	tableSchema := &schema.TableSchema{
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
		Table:  tableName,
		Schema: tableSchema,
	}))

	writeErr := errors.New("reader failed")
	err := dest.WriteParallel(ctx, recordBatchesThenError(writeErr, int64Batch(t, 1)), destination.WriteOptions{
		Table:  tableName,
		Schema: tableSchema,
	})
	require.ErrorIs(t, err, writeErr)
}

func TestDestinationRejectsNullIdentifiers(t *testing.T) {
	tests := []struct {
		name          string
		replace       bool
		arrowNullable bool
	}{
		{name: "append", arrowNullable: true},
		{name: "replace with non-nullable Arrow field", replace: true, arrowNullable: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tableName := "lake.analytics.null_identifier_events"
			tableSchema := &schema.TableSchema{
				Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}},
			}

			dest := NewDestination()
			require.NoError(t, dest.Connect(ctx, "iceberg+hadoop://?warehouse="+url.QueryEscape(t.TempDir())))
			defer func() { require.NoError(t, dest.Close(ctx)) }()

			require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
				Table: tableName, Schema: tableSchema, PrimaryKeys: []string{"id"},
			}))
			require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 7)), destination.WriteOptions{
				Table: tableName, Schema: tableSchema,
			}))

			prepareOpts := destination.PrepareOptions{
				Table: tableName, Schema: tableSchema, DropFirst: tt.replace,
			}
			if tt.replace {
				prepareOpts.PrimaryKeys = []string{"id"}
			}
			require.NoError(t, dest.PrepareTable(ctx, prepareOpts))

			err := dest.WriteParallel(ctx, recordBatches(int64BatchWithValidity(
				t, []int64{8, 0}, []bool{true, false}, tt.arrowNullable,
			)), destination.WriteOptions{Table: tableName, Schema: tableSchema})
			require.ErrorContains(t, err, `required field "id" contains 1 NULL value(s)`)
			require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, tableName))
		})
	}
}

func TestDestinationMakesNonIdentifierColumnsOptional(t *testing.T) {
	ctx := context.Background()
	tableName := "lake.analytics.optional_events"
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "event_name", DataType: schema.TypeString, Nullable: false},
		},
	}

	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, "iceberg+hadoop://?warehouse="+url.QueryEscape(t.TempDir())))
	defer func() { require.NoError(t, dest.Close(ctx)) }()

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       tableName,
		Schema:      tableSchema,
		PrimaryKeys: []string{"id"},
	}))

	gotSchema, err := dest.GetTableSchema(ctx, tableName)
	require.NoError(t, err)
	require.False(t, icebergColumn(t, gotSchema, "id").Nullable)
	require.True(t, icebergColumn(t, gotSchema, "event_name").Nullable)
	require.False(t, tableSchema.Columns[1].Nullable, "PrepareTable must not mutate the caller schema")
}

func TestDestinationReplaceAddsNewOptionalColumns(t *testing.T) {
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
	require.True(t, icebergColumn(t, gotSchema, "event_name").Nullable)
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
	err := dest.WriteParallel(ctx, recordBatchesThenError(writeErr, int64Batch(t, 9)), destination.WriteOptions{
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

func int64BatchWithValidity(t *testing.T, values []int64, valid []bool, nullable bool) arrow.RecordBatch {
	t.Helper()
	require.Len(t, valid, len(values))

	builder := array.NewInt64Builder(memory.DefaultAllocator)
	defer builder.Release()
	builder.AppendValues(values, valid)

	arr := builder.NewArray()
	defer arr.Release()

	arrowSchema := arrow.NewSchema([]arrow.Field{{
		Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: nullable,
	}}, nil)
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

func recordBatchesThenError(err error, batches ...arrow.RecordBatch) <-chan source.RecordBatchResult {
	records := make(chan source.RecordBatchResult, len(batches)+1)
	for _, batch := range batches {
		records <- source.RecordBatchResult{Batch: batch}
	}
	records <- source.RecordBatchResult{Err: err}
	close(records)
	return records
}

func addColumnComparison(col schema.Column) *schemaevolution.SchemaComparison {
	return &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type:       schemaevolution.ChangeAddColumn,
			ColumnName: col.Name,
			NewColumn:  col,
		}},
	}
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
