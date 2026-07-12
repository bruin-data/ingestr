//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	_ "github.com/apache/iceberg-go/catalog/rest"
	_ "github.com/apache/iceberg-go/catalog/sql"
	_ "github.com/apache/iceberg-go/io/gocloud"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	ingestconfig "github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	icebergdest "github.com/bruin-data/ingestr/pkg/destination/iceberg"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockermount "github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	icebergAzuriteAccountName = "devstoreaccount1"
	icebergAzuriteAccountKey  = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==" //gitleaks:allow -- public Azurite emulator key
)

type icebergCatalogTestEnv struct {
	destURI                string
	client                 *s3.Client
	bucket                 string
	objectPrefix           string
	tableLocationHasSuffix bool
	localWarehouse         string
}

func TestIcebergCatalogBackends(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	ctx := context.Background()

	tests := []struct {
		name  string
		setup func(t *testing.T, ctx context.Context) icebergCatalogTestEnv
	}{
		{
			name:  "sqlite sql catalog with minio",
			setup: setupIcebergSQLiteMinioCatalog,
		},
		{
			name:  "postgres sql catalog with minio",
			setup: setupIcebergPostgresMinioCatalog,
		},
		{
			name:  "rest catalog with local warehouse",
			setup: setupIcebergRESTCatalog,
		},
		{
			name:  "rest catalog with minio",
			setup: setupIcebergRESTMinioCatalog,
		},
		{
			name:  "nessie catalog with minio",
			setup: setupIcebergNessieMinioCatalog,
		},
		{
			name:  "hive metastore catalog with local warehouse",
			setup: setupIcebergHiveCatalog,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.setup(t, ctx)
			namespace := "it_" + uniqueSuffix()
			table := "events"
			tableName := namespace + "." + table

			exerciseIcebergDestination(t, ctx, env.destURI, tableName)
			if env.localWarehouse != "" {
				assert.Greater(t, countRegularFiles(t, env.localWarehouse), 0, "catalog writes must land in the configured warehouse")
				assert.NoDirExists(t, "file:", "file:/ URIs must not create a relative workspace directory")
			}

			if env.client != nil {
				tablePrefix := joinIcebergObjectPrefix(env.objectPrefix, namespace, table)
				if env.tableLocationHasSuffix {
					assert.Greater(t, countMinioObjects(t, ctx, env.client, env.bucket, strings.Trim(env.objectPrefix, "/")+"/"), 0)
				} else {
					assert.Greater(t, countMinioObjects(t, ctx, env.client, env.bucket, tablePrefix+"data/"), 0)
					assert.Greater(t, countMinioObjects(t, ctx, env.client, env.bucket, tablePrefix+"metadata/"), 0)
				}
			}
		})
	}
}

func TestIcebergManagedCatalogBackends(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping managed Iceberg catalog integration tests in short mode")
	}

	ctx := context.Background()
	tests := []struct {
		name   string
		envVar string
	}{
		{name: "AWS Glue", envVar: "GONG_TEST_ICEBERG_GLUE_URI"},
		{name: "Amazon S3 Tables", envVar: "GONG_TEST_ICEBERG_S3TABLES_URI"},
		{name: "Polaris", envVar: "GONG_TEST_ICEBERG_POLARIS_URI"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destURI := strings.TrimSpace(os.Getenv(tt.envVar))
			if destURI == "" {
				t.Skipf("Set %s to run the %s Iceberg destination conformance matrix", tt.envVar, tt.name)
			}
			namespace := "it_" + uniqueSuffix()
			tableName := namespace + ".events"
			t.Cleanup(func() { cleanupManagedIcebergCatalog(t, ctx, destURI, namespace, tableName) })
			exerciseIcebergDestination(t, ctx, destURI, tableName)
		})
	}
}

func TestIcebergHiveLocalWarehouseNormalizesFileURI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Hive Iceberg integration test in short mode")
	}
	ctx := context.Background()
	env := setupIcebergHiveCatalog(t, ctx)
	namespace := "it_" + uniqueSuffix()
	tableName := namespace + ".events"
	t.Cleanup(func() { cleanupManagedIcebergCatalog(t, ctx, env.destURI, namespace, tableName) })
	initial := writeIcebergJSONL(t, "hive-file-uri.jsonl", `{"id":1,"name":"absolute"}`)
	runIcebergPipeline(t, ctx, initial, env.destURI, tableName, ingestconfig.StrategyReplace)
	require.Greater(t, countRegularFiles(t, env.localWarehouse), 0)
	require.NoDirExists(t, "file:", "file:/ URIs must not create a relative workspace directory")
}

func cleanupManagedIcebergCatalog(t *testing.T, ctx context.Context, destURI, namespace, tableName string) {
	t.Helper()
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()

	var cleanupErr error
	dest := icebergdest.NewDestination()
	if err := dest.Connect(cleanupCtx, destURI); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("connect for managed Iceberg cleanup: %w", err))
	} else {
		if err := dest.DropTable(cleanupCtx, tableName); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("purge managed Iceberg test table %s: %w", tableName, err))
		}
		if err := dest.DropNamespace(cleanupCtx, namespace); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("drop managed Iceberg test namespace %s: %w", namespace, err))
		}
		if err := dest.Close(cleanupCtx); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close managed Iceberg cleanup destination: %w", err))
		}
	}
	assert.NoError(t, cleanupErr)
}

func TestIcebergCloudObjectStores(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tests := []struct {
		name  string
		setup func(t *testing.T, ctx context.Context) string
	}{
		{name: "gcs json api", setup: setupIcebergGCSCatalog},
		{name: "azure data lake shared key", setup: setupIcebergAzureCatalog},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destURI := tt.setup(t, ctx)
			dest := icebergdest.NewDestination()
			require.NoError(t, dest.Connect(ctx, destURI))
			require.NoError(t, dest.CheckConnection(ctx))
			require.NoError(t, dest.Close(ctx))

			namespace := "it_" + uniqueSuffix()
			exerciseIcebergDestination(t, ctx, destURI, namespace+".events")
		})
	}
}

func TestIcebergRESTMinioDurableTokensAndCDCResume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	env := setupIcebergRESTMinioCatalog(t, ctx)
	dest := icebergdest.NewDestination()
	require.NoError(t, dest.Connect(ctx, env.destURI))
	t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })

	namespace := "it_" + uniqueSuffix()
	target := namespace + ".cdc_target"
	staging := namespace + ".cdc_staging"
	tableSchema := icebergIntegrationCDCSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: target, Schema: tableSchema, PrimaryKeys: []string{"id"},
	}))

	appendOpts := destination.WriteOptions{
		Table: target, Schema: tableSchema, PrimaryKeys: []string{"id"},
		Parallelism: 3, CommitToken: "rest-minio-append-1", CDCResumeLSN: "0/10",
	}
	require.NoError(t, dest.WriteParallel(ctx, icebergCDCRecords(t, tableSchema, 1, "first", "0/10"), appendOpts))
	require.NoError(t, dest.WriteParallel(ctx, icebergCDCRecords(t, tableSchema, 1, "first", "0/10"), appendOpts))
	require.EqualValues(t, 1, loadIcebergTableSummary(t, ctx, env.destURI, target).rows)

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: staging, Schema: tableSchema}))
	require.NoError(t, dest.WriteParallel(ctx, icebergCDCRecords(t, tableSchema, 2, "merged", "0/20"), destination.WriteOptions{
		Table: staging, Schema: tableSchema, Parallelism: 2,
	}))
	mergeOpts := destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      tableSchema.ColumnNames(),
		CommitToken:  "rest-minio-merge-1",
	}
	require.NoError(t, dest.MergeTable(ctx, mergeOpts))
	require.NoError(t, dest.MergeTable(ctx, mergeOpts))
	require.EqualValues(t, 2, loadIcebergTableSummary(t, ctx, env.destURI, target).rows)

	resume, err := dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Equal(t, "0/20", resume)
	require.NoError(t, dest.CommitWriteToken(ctx, target, "rest-minio-idle-1", "0/30"))
	resume, err = dest.GetMaxCDCLSN(ctx, target)
	require.NoError(t, err)
	require.Equal(t, "0/30", resume)
}

func icebergIntegrationCDCSchema() *schema.TableSchema {
	return &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: true},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: true},
		{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
}

func icebergCDCRecords(t *testing.T, tableSchema *schema.TableSchema, id int64, name, lsn string) <-chan source.RecordBatchResult {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, tableSchema.ToArrowSchema())
	builder.Field(0).(*array.Int64Builder).Append(id)
	builder.Field(1).(*array.StringBuilder).Append(name)
	builder.Field(2).(*array.StringBuilder).Append(lsn)
	builder.Field(3).(*array.BooleanBuilder).Append(false)
	builder.Field(4).(*array.TimestampBuilder).Append(arrow.Timestamp(time.Now().UTC().UnixMicro()))
	batch := builder.NewRecordBatch()
	builder.Release()

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: batch}
	close(records)
	return records
}

func setupIcebergGCSCatalog(t *testing.T, ctx context.Context) string {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "fsouza/fake-gcs-server:1.52.3",
		ExposedPorts: []string{"4443/tcp"},
		Cmd:          []string{"-scheme", "http", "-port", "4443", "-backend", "memory"},
		WaitingFor: wait.ForHTTP("/storage/v1/b?project=fake-project-id").
			WithPort("4443/tcp").
			WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "4443")
	require.NoError(t, err)
	endpoint := fmt.Sprintf("http://%s:%s/storage/v1", host, port.Port())
	bucket := "iceberg-" + uniqueSuffix()

	body, err := json.Marshal(map[string]string{"name": bucket})
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/b?project=fake-project-id", bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.Contains(t, []int{http.StatusOK, http.StatusConflict}, response.StatusCode)

	values := url.Values{}
	values.Set("storage", "gcs")
	values.Set("bucket", bucket)
	values.Set("prefix", "warehouse")
	values.Set("endpoint", endpoint+"/")
	values.Set("use_ssl", "false")
	values.Set("gcs_use_json_api", "true")
	values.Set("table.write.format.default", "parquet")
	return "iceberg+sqlite://" + filepath.Join(t.TempDir(), "catalog.db") + "?" + values.Encode()
}

func setupIcebergAzureCatalog(t *testing.T, ctx context.Context) string {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "mcr.microsoft.com/azure-storage/azurite:3.35.0",
		ExposedPorts: []string{"10000/tcp"},
		Cmd:          []string{"azurite-blob", "--loose", "--blobHost", "0.0.0.0", "--blobPort", "10000", "--skipApiVersionCheck"},
		WaitingFor:   wait.ForListeningPort("10000/tcp").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "10000")
	require.NoError(t, err)
	endpoint := fmt.Sprintf("%s:%s", host, port.Port())
	containerName := "warehouse"

	credential, err := azblob.NewSharedKeyCredential(icebergAzuriteAccountName, icebergAzuriteAccountKey)
	require.NoError(t, err)
	client, err := azblob.NewClientWithSharedKeyCredential("http://"+endpoint+"/"+icebergAzuriteAccountName, credential, nil)
	require.NoError(t, err)
	_, err = client.CreateContainer(ctx, containerName, nil)
	require.NoError(t, err)

	values := url.Values{}
	values.Set("storage", "azure")
	values.Set("container", containerName)
	values.Set("account_name", icebergAzuriteAccountName)
	values.Set("account_key", icebergAzuriteAccountKey)
	values.Set("prefix", "warehouse")
	values.Set("endpoint", endpoint)
	values.Set("use_ssl", "false")
	values.Set("adls_scheme", "abfs")
	values.Set("table.write.format.default", "parquet")
	return "iceberg+sqlite://" + filepath.Join(t.TempDir(), "catalog.db") + "?" + values.Encode()
}

func setupIcebergSQLiteMinioCatalog(t *testing.T, ctx context.Context) icebergCatalogTestEnv {
	t.Helper()

	minio := getMinioEnv(t)
	setIcebergMinioEnv(t, minio.endpoint)

	client := createMinioClient(t, minio.endpoint)

	bucket := "iceberg-" + uniqueSuffix()
	createIcebergBucket(t, ctx, client, bucket)

	return icebergCatalogTestEnv{
		destURI: icebergSQLMinioDestinationURI(t, minio.endpoint, bucket),
		client:  client,
		bucket:  bucket,
	}
}

func setupIcebergPostgresMinioCatalog(t *testing.T, ctx context.Context) icebergCatalogTestEnv {
	t.Helper()

	minio := getMinioEnv(t)
	setIcebergMinioEnv(t, minio.endpoint)
	client := createMinioClient(t, minio.endpoint)

	bucket := "iceberg-" + uniqueSuffix()
	createIcebergBucket(t, ctx, client, bucket)

	container, pgURI, err := startPostgresContainerRaw(ctx, "iceberg-catalog")
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	return icebergCatalogTestEnv{
		destURI: icebergPostgresMinioDestinationURI(minio.endpoint, bucket, pgURI),
		client:  client,
		bucket:  bucket,
	}
}

func setupIcebergRESTCatalog(t *testing.T, ctx context.Context) icebergCatalogTestEnv {
	t.Helper()

	warehouse := dockerSharedTempDir(t, "rest")
	uri := startIcebergRESTContainer(t, ctx, warehouse)

	values := url.Values{}
	values.Set("warehouse_path", warehouse)
	values.Set("table.write.format.default", "parquet")

	return icebergCatalogTestEnv{
		destURI: uri + "?" + values.Encode(),
	}
}

func setupIcebergRESTMinioCatalog(t *testing.T, ctx context.Context) icebergCatalogTestEnv {
	t.Helper()

	nw, err := tcnetwork.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = nw.Remove(ctx) })

	minioContainer, minioEndpoint := startIcebergMinioContainer(t, ctx, nw.Name)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(minioContainer) })
	setIcebergMinioEnv(t, minioEndpoint)

	client := createMinioClient(t, minioEndpoint)
	bucket := "iceberg-" + uniqueSuffix()
	createIcebergBucket(t, ctx, client, bucket)

	restURI := startIcebergRESTContainerWithS3(t, ctx, nw.Name, bucket)

	values := icebergMinioValues(minioEndpoint, bucket)
	values.Set("prefix", "rest")
	return icebergCatalogTestEnv{
		destURI:      restURI + "?" + values.Encode(),
		client:       client,
		bucket:       bucket,
		objectPrefix: "rest",
	}
}

func setupIcebergNessieMinioCatalog(t *testing.T, ctx context.Context) icebergCatalogTestEnv {
	t.Helper()

	nw, err := tcnetwork.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = nw.Remove(ctx) })

	minioContainer, minioEndpoint := startIcebergMinioContainer(t, ctx, nw.Name)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(minioContainer) })
	setIcebergMinioEnv(t, minioEndpoint)

	client := createMinioClient(t, minioEndpoint)
	bucket := "iceberg-" + uniqueSuffix()
	createIcebergBucket(t, ctx, client, bucket)
	nessieURI := startIcebergNessieContainer(t, ctx, nw.Name, minioEndpoint, bucket)

	values := icebergMinioValues(minioEndpoint, bucket)
	values.Del("table_path")
	clearAmbientAWSCredentials(t)
	return icebergCatalogTestEnv{
		destURI:                nessieURI + "?" + values.Encode(),
		client:                 client,
		bucket:                 bucket,
		objectPrefix:           "nessie",
		tableLocationHasSuffix: true,
	}
}

func clearAmbientAWSCredentials(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_SECURITY_TOKEN",
		"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_S3_ENDPOINT", "AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_S3",
		"AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_CREDENTIALS_FULL_URI",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN", "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE",
	} {
		t.Setenv(key, "")
	}
	missingConfigDir := t.TempDir()
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(missingConfigDir, "missing-credentials"))
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(missingConfigDir, "missing-config"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

func setupIcebergHiveCatalog(t *testing.T, ctx context.Context) icebergCatalogTestEnv {
	t.Helper()

	warehouse := dockerSharedTempDir(t, "hive")
	hiveURI := startIcebergHiveMetastoreContainer(t, ctx, warehouse)
	values := url.Values{}
	values.Set("warehouse_path", warehouse)
	values.Set("table.write.format.default", "parquet")
	return icebergCatalogTestEnv{destURI: hiveURI + "?" + values.Encode(), localWarehouse: warehouse}
}

func countRegularFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	require.NoError(t, err)
	return count
}

func exerciseIcebergDestination(t *testing.T, ctx context.Context, destURI, tableName string) {
	t.Helper()

	initial := writeIcebergJSONL(
		t, "initial.jsonl",
		`{"id":1,"name":"alpha","active":true,"score":10.5}`,
		`{"id":2,"name":"bravo","active":false,"score":20.25}`,
	)
	runIcebergPipeline(t, ctx, initial, destURI, tableName, ingestconfig.StrategyReplace)

	summary := loadIcebergTableSummary(t, ctx, destURI, tableName)
	assert.EqualValues(t, 2, summary.rows)
	assert.ElementsMatch(t, []string{"id"}, summary.primaryKeys)
	assert.True(t, summary.fields["id"])
	assert.True(t, summary.fields["name"])
	assert.True(t, summary.fields["active"])
	assert.True(t, summary.fields["score"])

	appendRows := writeIcebergJSONL(
		t, "append.jsonl",
		`{"id":3,"name":"charlie","active":true,"score":30.5,"age":31}`,
		`{"id":4,"name":"delta","active":false,"score":40.75,"age":42}`,
	)
	runIcebergPipeline(t, ctx, appendRows, destURI, tableName, ingestconfig.StrategyAppend)

	summary = loadIcebergTableSummary(t, ctx, destURI, tableName)
	assert.EqualValues(t, 4, summary.rows)
	assert.True(t, summary.fields["age"], "append should evolve the Iceberg table schema")

	replacement := writeIcebergJSONL(
		t, "replacement.jsonl",
		`{"id":9,"name":"replace","active":true,"score":99.9,"age":55}`,
	)
	runIcebergPipeline(t, ctx, replacement, destURI, tableName, ingestconfig.StrategyReplace)

	summary = loadIcebergTableSummary(t, ctx, destURI, tableName)
	assert.EqualValues(t, 1, summary.rows)
	assert.True(t, summary.fields["age"])

	mergeRows := writeIcebergJSONL(
		t, "merge.jsonl",
		`{"id":9,"name":"replace-merged","active":false,"score":9.5,"age":56}`,
		`{"id":10,"name":"merged-new","active":true,"score":10.5,"age":20}`,
	)
	runIcebergPipeline(t, ctx, mergeRows, destURI, tableName, ingestconfig.StrategyMerge)

	rows := readIcebergRows(t, ctx, destURI, tableName)
	assert.Len(t, rows, 2)
	assert.Equal(t, "replace-merged", icebergNameByID(t, rows, 9), "merge should update the existing row in place")
	assert.Equal(t, "merged-new", icebergNameByID(t, rows, 10), "merge should insert net-new rows")
}

func icebergSQLMinioDestinationURI(t *testing.T, minioEndpoint, bucket string) string {
	t.Helper()

	catalogDB := filepath.Join(t.TempDir(), "iceberg-catalog.db")
	values := url.Values{}
	values.Set("storage", "s3")
	values.Set("bucket", bucket)
	values.Set("endpoint", strings.TrimPrefix(minioEndpoint, "http://"))
	values.Set("use_ssl", "false")
	values.Set("region", "us-east-1")
	values.Set("access_key_id", minioAccessKey)
	values.Set("secret_access_key", minioSecretKey)
	values.Set("table_path", "{namespace}/{table}")
	values.Set("table.write.format.default", "parquet")
	return "iceberg+sqlite://" + catalogDB + "?" + values.Encode()
}

func icebergPostgresMinioDestinationURI(minioEndpoint, bucket, pgURI string) string {
	values := icebergMinioValues(minioEndpoint, bucket)
	values.Set("uri", strings.Replace(pgURI, "postgresql://", "postgres://", 1))
	return "iceberg+postgres://catalog?" + values.Encode()
}

func icebergMinioValues(minioEndpoint, bucket string) url.Values {
	values := url.Values{}
	values.Set("storage", "s3")
	values.Set("bucket", bucket)
	values.Set("endpoint", strings.TrimPrefix(minioEndpoint, "http://"))
	values.Set("use_ssl", "false")
	values.Set("region", "us-east-1")
	values.Set("access_key_id", minioAccessKey)
	values.Set("secret_access_key", minioSecretKey)
	values.Set("table_path", "{namespace}/{table}")
	values.Set("table.write.format.default", "parquet")
	return values
}

func setIcebergMinioEnv(t *testing.T, endpoint string) {
	t.Helper()

	t.Setenv("AWS_S3_ENDPOINT", endpoint)
	t.Setenv("AWS_ACCESS_KEY_ID", minioAccessKey)
	t.Setenv("AWS_SECRET_ACCESS_KEY", minioSecretKey)
	t.Setenv("AWS_REGION", "us-east-1")
}

func createIcebergBucket(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()

	bucketCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Idempotent: an existing bucket (e.g. a same-nanosecond uniqueSuffix collision)
	// is fine; HeadBucket below confirms it exists.
	_, err := client.CreateBucket(bucketCtx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	var owned *s3types.BucketAlreadyOwnedByYou
	var exists *s3types.BucketAlreadyExists
	if err != nil && !errors.As(err, &owned) && !errors.As(err, &exists) {
		require.NoError(t, err)
	}

	_, err = client.HeadBucket(bucketCtx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	require.NoError(t, err)
}

func writeIcebergJSONL(t *testing.T, name string, rows ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o600))
	return "jsonl://" + path
}

func runIcebergPipeline(t *testing.T, ctx context.Context, sourceURI, destURI, table string, strategy ingestconfig.IncrementalStrategy) {
	t.Helper()

	prepareIcebergRESTLocalDataDir(t, destURI, table)

	cfg := ingestconfig.DefaultConfig()
	cfg.SourceURI = sourceURI
	cfg.SourceTable = filepath.Base(strings.TrimPrefix(sourceURI, "jsonl://"))
	cfg.DestURI = destURI
	cfg.DestTable = table
	cfg.IncrementalStrategy = strategy
	cfg.IncrementalStrategyExplicit = true
	cfg.PrimaryKeys = []string{"id"}
	cfg.Progress = ingestconfig.ProgressLog
	cfg.ExtractParallelism = 2

	require.NoError(t, pipeline.New(cfg).Run(ctx))
}

func prepareIcebergRESTLocalDataDir(t *testing.T, destURI, table string) {
	t.Helper()

	parsed, err := url.Parse(destURI)
	require.NoError(t, err)
	if parsed.Scheme != "iceberg+rest" {
		return
	}

	warehouse := firstIcebergTestQueryValue(parsed.Query(), "warehouse_path", "warehouse-path", "warehouse")
	if warehouse == "" || strings.Contains(warehouse, "://") {
		return
	}

	parts := strings.Split(table, ".")
	require.Len(t, parts, 2)

	tableDir := filepath.Join(warehouse, parts[0], parts[1])
	for _, dir := range []string{tableDir, filepath.Join(tableDir, "data"), filepath.Join(tableDir, "metadata")} {
		require.NoError(t, os.MkdirAll(dir, 0o777))
		require.NoError(t, os.Chmod(dir, 0o777))
	}
}

func dockerSharedTempDir(t *testing.T, prefix string) string {
	t.Helper()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	base := filepath.Join(cwd, ".testtmp")
	require.NoError(t, os.MkdirAll(base, 0o755))
	t.Cleanup(func() { _ = os.Remove(base) })

	dir, err := os.MkdirTemp(base, "iceberg-"+prefix+"-")
	require.NoError(t, err)
	require.NoError(t, os.Chmod(dir, 0o777))
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	abs, err := filepath.Abs(dir)
	require.NoError(t, err)
	return abs
}

func startIcebergRESTContainer(t *testing.T, ctx context.Context, warehouse string) string {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "apache/iceberg-rest-fixture:1.9.2",
		ExposedPorts: []string{"8181/tcp"},
		Env: map[string]string{
			"CATALOG_WAREHOUSE": warehouse,
			"CATALOG_URI":       "jdbc:sqlite:/tmp/iceberg-rest.db",
		},
		HostConfigModifier: func(hostConfig *dockercontainer.HostConfig) {
			hostConfig.Mounts = append(hostConfig.Mounts, dockermount.Mount{
				Type:   dockermount.TypeBind,
				Source: warehouse,
				Target: warehouse,
			})
		},
		WaitingFor: wait.ForHTTP("/v1/config").
			WithPort("8181/tcp").
			WithStartupTimeout(90 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "8181")
	require.NoError(t, err)

	return fmt.Sprintf("iceberg+rest://%s:%s", host, port.Port())
}

func startIcebergRESTContainerWithS3(t *testing.T, ctx context.Context, networkName, bucket string) string {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "apache/iceberg-rest-fixture:1.9.2",
		ExposedPorts: []string{"8181/tcp"},
		Env: map[string]string{
			"AWS_ACCESS_KEY_ID":                 minioAccessKey,
			"AWS_SECRET_ACCESS_KEY":             minioSecretKey,
			"AWS_REGION":                        "us-east-1",
			"CATALOG_WAREHOUSE":                 "s3://" + bucket + "/rest/",
			"CATALOG_IO__IMPL":                  "org.apache.iceberg.aws.s3.S3FileIO",
			"CATALOG_S3_ENDPOINT":               "http://minio:9000",
			"CATALOG_S3_PATH__STYLE__ACCESS":    "true",
			"CATALOG_S3_ACCESS__KEY__ID":        minioAccessKey,
			"CATALOG_S3_SECRET__ACCESS__KEY":    minioSecretKey,
			"CATALOG_CLIENT_REGION":             "us-east-1",
			"CATALOG_URI":                       "jdbc:sqlite:/tmp/iceberg-rest.db",
			"CATALOG_S3_PRELOAD_CLIENT_ENABLED": "false",
		},
		Networks: []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"iceberg-rest"},
		},
		WaitingFor: wait.ForHTTP("/v1/config").
			WithPort("8181/tcp").
			WithStartupTimeout(90 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "8181")
	require.NoError(t, err)

	return fmt.Sprintf("iceberg+rest://%s:%s", host, port.Port())
}

func startIcebergNessieContainer(t *testing.T, ctx context.Context, networkName, externalMinioEndpoint, bucket string) string {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "ghcr.io/projectnessie/nessie:0.107.2",
		ExposedPorts: []string{"19120/tcp", "9000/tcp"},
		Env: map[string]string{
			"nessie.version.store.type":                                   "IN_MEMORY",
			"nessie.server.authentication.enabled":                        "false",
			"nessie.catalog.default-warehouse":                            "warehouse",
			"nessie.catalog.warehouses.warehouse.location":                "s3://" + bucket + "/nessie/",
			"nessie.catalog.service.s3.default-options.region":            "us-east-1",
			"nessie.catalog.service.s3.default-options.path-style-access": "true",
			"nessie.catalog.service.s3.default-options.endpoint":          "http://minio:9000/",
			"nessie.catalog.service.s3.default-options.external-endpoint": externalMinioEndpoint + "/",
			"nessie.catalog.service.s3.default-options.access-key":        "urn:nessie-secret:quarkus:nessie.catalog.secrets.access-key",
			"nessie.catalog.secrets.access-key.name":                      minioAccessKey,
			"nessie.catalog.secrets.access-key.secret":                    minioSecretKey,
		},
		Networks: []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"nessie"},
		},
		WaitingFor: wait.ForHTTP("/q/health/ready").
			WithPort("9000/tcp").
			WithStartupTimeout(120 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "19120")
	require.NoError(t, err)
	return fmt.Sprintf("iceberg+nessie://%s:%s", host, port.Port())
}

func startIcebergHiveMetastoreContainer(t *testing.T, ctx context.Context, warehouse string) string {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "apache/hive:4.0.0",
		User:         "root",
		ExposedPorts: []string{"9083/tcp"},
		Env: map[string]string{
			"SERVICE_NAME": "metastore",
			"DB_DRIVER":    "derby",
		},
		HostConfigModifier: func(hostConfig *dockercontainer.HostConfig) {
			hostConfig.Mounts = append(hostConfig.Mounts, dockermount.Mount{
				Type: dockermount.TypeBind, Source: warehouse, Target: warehouse,
			})
		},
		WaitingFor: wait.ForListeningPort("9083/tcp").
			WithStartupTimeout(180 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "9083")
	require.NoError(t, err)
	hiveURI := fmt.Sprintf("iceberg+hive://%s:%s", host, port.Port())
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		probe := icebergdest.NewDestination()
		if !assert.NoError(collect, probe.Connect(ctx, hiveURI+"?warehouse_path="+url.QueryEscape(warehouse))) {
			return
		}
		defer func() { _ = probe.Close(ctx) }()
		_, probeErr := probe.GetTableSchema(ctx, "default.__ingestr_readiness_probe")
		assert.NoError(collect, probeErr)
	}, 90*time.Second, 500*time.Millisecond, "Hive metastore did not become query-ready")
	return hiveURI
}

func startIcebergMinioContainer(t *testing.T, ctx context.Context, networkName string) (testcontainers.Container, string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "minio/minio:latest",
		ExposedPorts: []string{"9000/tcp"},
		Env: map[string]string{
			"MINIO_ROOT_USER":     minioAccessKey,
			"MINIO_ROOT_PASSWORD": minioSecretKey,
		},
		Cmd:      []string{"server", "/data"},
		Networks: []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"minio"},
		},
		WaitingFor: wait.ForHTTP("/minio/health/ready").
			WithPort("9000").
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "9000")
	require.NoError(t, err)

	return container, fmt.Sprintf("http://%s:%s", host, port.Port())
}

type icebergTableSummary struct {
	rows        int64
	fields      map[string]bool
	primaryKeys []string
}

func loadIcebergTableSummary(t *testing.T, ctx context.Context, destURI, tableName string) icebergTableSummary {
	t.Helper()

	cfg, err := parseIcebergTestURI(destURI)
	require.NoError(t, err)

	cat, err := icebergcatalog.Load(ctx, icebergTestCatalogName(destURI), cfg)
	require.NoError(t, err)

	tbl, err := cat.LoadTable(ctx, icebergcatalog.ToIdentifier(strings.Split(tableName, ".")...))
	require.NoError(t, err)

	tasks, err := tbl.Scan().PlanFiles(ctx)
	require.NoError(t, err)

	var rows int64
	for _, task := range tasks {
		rows += task.File.Count()
	}

	fields := make(map[string]bool)
	for _, field := range tbl.Schema().Fields() {
		fields[field.Name] = true
	}

	idNames := make([]string, 0, len(tbl.Schema().IdentifierFieldIDs))
	for _, fieldID := range tbl.Schema().IdentifierFieldIDs {
		field, ok := tbl.Schema().FindFieldByID(fieldID)
		require.True(t, ok, "identifier field id %d should exist", fieldID)
		idNames = append(idNames, field.Name)
	}

	return icebergTableSummary{
		rows:        rows,
		fields:      fields,
		primaryKeys: idNames,
	}
}

func parseIcebergTestURI(rawURI string) (iceberggo.Properties, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return nil, err
	}

	query := parsed.Query()
	props := iceberggo.Properties{}
	switch parsed.Scheme {
	case "iceberg+sqlite":
		props["type"] = "sql"
		props["uri"] = "file:" + parsed.Path
		props["sql.dialect"] = "sqlite"
		props["sql.driver"] = "sqlite"
	case "iceberg+postgres":
		props["type"] = "sql"
		props["uri"] = strings.Replace(query.Get("uri"), "postgresql://", "postgres://", 1)
		props["sql.dialect"] = "postgres"
		props["sql.driver"] = "pgx"
	case "iceberg+rest":
		props["type"] = "rest"
		props["uri"] = "http://" + parsed.Host
	case "iceberg+nessie":
		props["type"] = "rest"
		path := strings.TrimRight(parsed.EscapedPath(), "/")
		if path == "" {
			path = "/iceberg"
		}
		branch := strings.Trim(firstIcebergTestQueryValue(query, "nessie_branch", "nessie-branch", "branch", "ref"), "/")
		warehouse := firstIcebergTestQueryValue(query, "nessie_warehouse", "nessie-warehouse")
		if branch != "" || warehouse != "" {
			path += "/" + url.PathEscape(branch)
			if warehouse != "" {
				path += "%7C" + url.PathEscape(warehouse)
			}
		}
		props["uri"] = "http://" + parsed.Host + path
	case "iceberg+polaris":
		props["type"] = "rest"
		path := strings.TrimRight(parsed.EscapedPath(), "/")
		if path == "" {
			path = "/api/catalog"
		}
		protocol := "https"
		if firstIcebergTestQueryValue(query, "polaris_use_ssl", "polaris-use-ssl", "catalog_use_ssl", "catalog-use-ssl") == "false" {
			protocol = "http"
		}
		props["uri"] = protocol + "://" + parsed.Host + path
		props["warehouse"] = query.Get("warehouse")
	case "iceberg+s3tables":
		props["type"] = "rest"
		region := firstIcebergTestQueryValue(query, "region", "region_name", "rest.signing-region")
		endpoint := "https://s3tables." + region + ".amazonaws.com/iceberg"
		if parsed.Host != "" {
			path := strings.TrimRight(parsed.EscapedPath(), "/")
			if path == "" {
				path = "/iceberg"
			}
			protocol := "https"
			if firstIcebergTestQueryValue(query, "s3tables_use_ssl", "s3tables-use-ssl", "catalog_use_ssl", "catalog-use-ssl") == "false" {
				protocol = "http"
			}
			endpoint = protocol + "://" + parsed.Host + path
		}
		props["uri"] = endpoint
		props["warehouse"] = query.Get("warehouse")
		props["rest.sigv4-enabled"] = "true"
		props["rest.signing-name"] = "s3tables"
		props["rest.signing-region"] = region
	case "iceberg+hive":
		props["type"] = "hive"
		props["uri"] = "thrift://" + parsed.Host
	default:
		props["type"] = strings.TrimPrefix(parsed.Scheme, "iceberg+")
	}

	storage := strings.ToLower(query.Get("storage"))
	bucket := query.Get("bucket")
	prefix := strings.Trim(query.Get("prefix"), "/")
	switch storage {
	case "gcs":
		if bucket != "" {
			props["warehouse"] = joinIcebergCloudWarehouse("gs://"+bucket, prefix)
		}
		if endpoint := query.Get("endpoint"); endpoint != "" {
			if !strings.Contains(endpoint, "://") {
				endpoint = "http://" + endpoint
			}
			props["gcs.endpoint"] = endpoint
		}
		if query.Has("gcs_use_json_api") {
			props["gcs.usejsonapi"] = query.Get("gcs_use_json_api")
		}
	case "azure", "adls":
		containerName := firstIcebergTestQueryValue(query, "container", "bucket")
		accountName := query.Get("account_name")
		scheme := query.Get("adls_scheme")
		if scheme == "" {
			scheme = "abfss"
		}
		if containerName != "" && accountName != "" {
			props["warehouse"] = joinIcebergCloudWarehouse(fmt.Sprintf("%s://%s@%s.dfs.core.windows.net", scheme, containerName, accountName), prefix)
		}
		if accountName != "" {
			props["adls.auth.shared-key.account.name"] = accountName
		}
		if accountKey := query.Get("account_key"); accountKey != "" {
			props["adls.auth.shared-key.account.key"] = accountKey
		}
		if endpoint := query.Get("endpoint"); endpoint != "" {
			props["adls.endpoint"] = strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
		}
		protocol := "https"
		if query.Get("use_ssl") == "false" {
			protocol = "http"
		}
		props["adls.protocol"] = protocol
	default:
		if bucket != "" && parsed.Scheme != "iceberg+nessie" && parsed.Scheme != "iceberg+polaris" && parsed.Scheme != "iceberg+s3tables" {
			props["warehouse"] = joinIcebergCloudWarehouse("s3://"+bucket, prefix)
		}
	}
	if warehouse := firstIcebergTestQueryValue(query, "warehouse", "warehouse_path", "warehouse-path"); warehouse != "" {
		props["warehouse"] = warehouse
	}
	if endpoint := query.Get("endpoint"); endpoint != "" && storage != "gcs" && storage != "azure" && storage != "adls" {
		if !strings.Contains(endpoint, "://") {
			endpoint = "http://" + endpoint
		}
		props["s3.endpoint"] = endpoint
	}
	if region := query.Get("region"); region != "" {
		props["s3.region"] = region
	}
	if accessKey := query.Get("access_key_id"); accessKey != "" {
		props["s3.access-key-id"] = accessKey
	}
	if secretKey := query.Get("secret_access_key"); secretKey != "" {
		props["s3.secret-access-key"] = secretKey
	}
	if token := firstIcebergTestQueryValue(query, "oauth_token", "oauth-token"); token != "" {
		props["token"] = token
	}
	clientID := firstIcebergTestQueryValue(query, "oauth_client_id", "oauth-client-id")
	clientSecret := firstIcebergTestQueryValue(query, "oauth_client_secret", "oauth-client-secret")
	if clientID != "" && clientSecret != "" {
		props["credential"] = clientID + ":" + clientSecret
	}
	if realm := firstIcebergTestQueryValue(query, "polaris_realm", "polaris-realm"); realm != "" {
		props["header.Polaris-Realm"] = realm
	}
	if region := firstIcebergTestQueryValue(query, "region", "region_name"); region != "" {
		props["glue.region"] = region
	}
	if accessKey := query.Get("access_key_id"); accessKey != "" {
		props["glue.access-key-id"] = accessKey
	}
	if secretKey := query.Get("secret_access_key"); secretKey != "" {
		props["glue.secret-access-key"] = secretKey
	}
	if sessionToken := query.Get("session_token"); sessionToken != "" {
		props["glue.session-token"] = sessionToken
		props["s3.session-token"] = sessionToken
	}
	for key, values := range parsed.Query() {
		if strings.HasPrefix(key, "table.") || key == "table_location" {
			continue
		}
		if key == "storage" || key == "bucket" || key == "container" || key == "prefix" || key == "endpoint" || key == "use_ssl" || key == "region" || key == "access_key_id" || key == "secret_access_key" || key == "table_path" || key == "warehouse_path" || key == "warehouse-path" || key == "uri" || key == "account_name" || key == "account_key" || key == "adls_scheme" || key == "gcs_use_json_api" {
			continue
		}
		if len(values) > 0 {
			props[key] = values[0]
		}
	}
	return props, nil
}

func joinIcebergCloudWarehouse(base, prefix string) string {
	base = strings.TrimRight(base, "/")
	if prefix != "" {
		base += "/" + strings.Trim(prefix, "/")
	}
	return base + "/"
}

func icebergTestCatalogName(rawURI string) string {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return "ingestr"
	}
	if name := parsed.Query().Get("catalog_name"); name != "" {
		return name
	}
	return "ingestr"
}

func firstIcebergTestQueryValue(query url.Values, keys ...string) string {
	for _, key := range keys {
		if value := query.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func countMinioObjects(t *testing.T, ctx context.Context, client *s3.Client, bucket, prefix string) int {
	t.Helper()

	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return len(listObjects(t, listCtx, client, bucket, prefix))
}

func joinIcebergObjectPrefix(parts ...string) string {
	joined := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			joined = append(joined, part)
		}
	}
	if len(joined) == 0 {
		return ""
	}
	return strings.Join(joined, "/") + "/"
}

// TestIcebergCommitTableUsesURICredentials guards apache/iceberg-go#1167: the
// commit-path FileIO must use the catalog's s3.* creds so URI-only S3 writes work.
// If a future iceberg-go bump regresses this, the write fails with
// "Invalid region: region was not a valid DNS name" and this test catches it.
func TestIcebergCommitTableUsesURICredentials(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	clearAWSEnvForURIOnlyTest(t)

	ctx := context.Background()

	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)
	bucket := "iceberg-" + uniqueSuffix()
	createIcebergBucket(t, ctx, client, bucket)

	destURI := icebergSQLMinioDestinationURI(t, minio.endpoint, bucket)
	namespace := "it_" + uniqueSuffix()
	tableName := namespace + ".events"

	rows := writeIcebergJSONL(
		t, "commit_table_creds.jsonl",
		`{"id":1,"name":"alpha"}`,
		`{"id":2,"name":"bravo"}`,
	)
	// replace exercises create + the overwrite-commit metadata write (CommitTable).
	// If CommitTable doesn't get the catalog's s3.* props, this fails with
	// "Invalid region" because the commit's PutObject has no credentials.
	runIcebergPipeline(t, ctx, rows, destURI, tableName, ingestconfig.StrategyReplace)

	// The commit produced a valid, queryable snapshot from the URI creds alone.
	summary := loadIcebergTableSummary(t, ctx, destURI, tableName)
	assert.EqualValues(t, 2, summary.rows)

	// ...and the committed metadata + data landed in S3.
	tablePrefix := joinIcebergObjectPrefix("", namespace, "events")
	assert.Greater(t, countMinioObjects(t, ctx, client, bucket, tablePrefix+"data/"), 0)
	assert.Greater(t, countMinioObjects(t, ctx, client, bucket, tablePrefix+"metadata/"), 0)
}

// clearAWSEnvForURIOnlyTest removes ambient AWS credential/region/endpoint inputs
// and points the SDK config files at nonexistent paths, so the destination URI is
// the only possible source of S3 credentials. Originals are restored on cleanup.
func clearAWSEnvForURIOnlyTest(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN", "AWS_S3_ENDPOINT", "AWS_PROFILE",
	} {
		orig, had := os.LookupEnv(key)
		require.NoError(t, os.Unsetenv(key))
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, orig)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
	// Block ambient ~/.aws config and disable the IMDS probe so a regression fails
	// fast with the credential error instead of hanging on 169.254.169.254.
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "no-credentials"))
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "no-config"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}
