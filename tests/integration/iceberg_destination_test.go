//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	_ "github.com/apache/iceberg-go/catalog/rest"
	_ "github.com/apache/iceberg-go/catalog/sql"
	_ "github.com/apache/iceberg-go/io/gocloud"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	ingestconfig "github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockermount "github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type icebergCatalogTestEnv struct {
	destURI string
	client  *s3.Client
	bucket  string
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.setup(t, ctx)
			namespace := "it_" + uniqueSuffix()
			table := "events"
			tableName := namespace + "." + table

			exerciseIcebergDestination(t, ctx, env.destURI, tableName)

			if env.client != nil {
				assert.Greater(t, countMinioObjects(t, ctx, env.client, env.bucket, namespace+"/"+table+"/data/"), 0)
				assert.Greater(t, countMinioObjects(t, ctx, env.client, env.bucket, namespace+"/"+table+"/metadata/"), 0)
			}
		})
	}
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

	_, err := client.CreateBucket(bucketCtx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	require.NoError(t, err)

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
		Entrypoint:   []string{"sh", "-c"},
		Cmd:          []string{"umask 000 && exec java -jar iceberg-rest-adapter.jar"},
		ExposedPorts: []string{"8181/tcp"},
		Env: map[string]string{
			"CATALOG_WAREHOUSE": warehouse,
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
	default:
		props["type"] = strings.TrimPrefix(parsed.Scheme, "iceberg+")
	}

	if bucket := query.Get("bucket"); bucket != "" {
		props["warehouse"] = "s3://" + bucket + "/"
	}
	if warehouse := firstIcebergTestQueryValue(query, "warehouse", "warehouse_path", "warehouse-path"); warehouse != "" {
		props["warehouse"] = warehouse
	}
	if endpoint := query.Get("endpoint"); endpoint != "" {
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
	for key, values := range parsed.Query() {
		if strings.HasPrefix(key, "table.") || key == "table_location" {
			continue
		}
		if key == "storage" || key == "bucket" || key == "endpoint" || key == "use_ssl" || key == "region" || key == "access_key_id" || key == "secret_access_key" || key == "table_path" || key == "warehouse_path" || key == "warehouse-path" || key == "uri" {
			continue
		}
		if len(values) > 0 {
			props[key] = values[0]
		}
	}
	return props, nil
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
