package integration

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	ingestconfig "github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func getMinioEnv(t *testing.T) minioEnv {
	t.Helper()
	if minioShared.container == nil {
		t.Skip("MinIO container not available")
	}
	return minioShared
}

func createMinioClient(t *testing.T, endpoint string) *s3.Client {
	t.Helper()

	cfg, err := config.LoadDefaultConfig(
		context.Background(),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(minioAccessKey, minioSecretKey, ""),
		),
		config.WithRegion("us-east-1"),
	)
	require.NoError(t, err)

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
}

func createBucket(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Logf("Bucket may already exist: %v", err)
	}
}

func uploadFile(t *testing.T, ctx context.Context, client *s3.Client, bucket, key string, data []byte) {
	t.Helper()
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	require.NoError(t, err)
}

func listObjects(t *testing.T, ctx context.Context, client *s3.Client, bucket, prefix string) []string {
	t.Helper()
	resp, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	require.NoError(t, err)

	var keys []string
	for _, obj := range resp.Contents {
		keys = append(keys, aws.ToString(obj.Key))
	}
	return keys
}

// =====================================================
// TEST: PostgreSQL to S3 (Destination Tests)
// =====================================================

func TestPostgresToS3_BasicWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-basic-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	sourceURI := sharedPostgresURI(t, "source")
	sourceSchema := uniqueSchemaName(t, "src")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, sourceURI, sourceSchema) })

	setupPostgresSourceData(t, ctx, sourceURI, sourceSchema, "users")

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         sourceSchema + ".users",
		DestURI:             minio.uri,
		DestTable:           bucket + "/output",
		IncrementalStrategy: "replace",
	}

	t.Logf("Running pipeline with source=%s dest=%s destTable=%s", cfg.SourceURI, cfg.DestURI, cfg.DestTable)
	p := pipeline.New(cfg)
	err := p.Run(ctx)
	if err != nil {
		t.Logf("Pipeline error: %v", err)
	}
	require.NoError(t, err, "Pipeline should run without errors")

	// List all keys in the bucket (not just output/)
	allKeys := listObjects(t, ctx, client, bucket, "")
	t.Logf("All keys in bucket %s: %v", bucket, allKeys)

	keys := listObjects(t, ctx, client, bucket, "output/")
	t.Logf("Keys with output/ prefix: %v", keys)
	require.NotEmpty(t, keys, "Should have written at least one parquet file")

	for _, key := range keys {
		assert.Contains(t, key, ".parquet", "Files should be parquet format")
	}

	t.Logf("Successfully wrote %d files to S3", len(keys))
}

func TestPostgresToS3_CustomLayout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-layout-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	sourceURI := sharedPostgresURI(t, "source")
	sourceSchema := uniqueSchemaName(t, "src")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, sourceURI, sourceSchema) })

	setupPostgresSourceData(t, ctx, sourceURI, sourceSchema, "users")

	destURI := fmt.Sprintf("%s&layout={table_name}.{ext}", minio.uri)

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         sourceSchema + ".users",
		DestURI:             destURI,
		DestTable:           bucket + "/data",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err := p.Run(ctx)
	require.NoError(t, err)

	keys := listObjects(t, ctx, client, bucket, "data/")
	require.NotEmpty(t, keys)
	t.Logf("Custom layout produced keys: %v", keys)

	found := false
	for _, key := range keys {
		// destTable is bucket/data, so tableName becomes "data"
		// layout is {table_name}.{ext} = data.parquet
		// basePath is "data", so final path is data/data.parquet
		if key == "data/data.parquet" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should have file named data.parquet based on custom layout")
}

// =====================================================
// TEST: S3 to SQLite (Source Tests - CSV)
// =====================================================

func TestS3ToSQLite_CSVSource(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-csv-src-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	csvData := generateCSVData(100)
	uploadFile(t, ctx, client, bucket, "data/users.csv", csvData)

	tmpFile, err := os.CreateTemp("", "test_s3_csv_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/users.csv",
		DestURI:             destURI,
		DestTable:           "users",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should run without errors")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 100, count, "Should have 100 rows from CSV")
}

func TestS3ToSQLite_CSVGlobPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-csv-glob-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	uploadFile(t, ctx, client, bucket, "data/part1.csv", generateCSVData(50))
	uploadFile(t, ctx, client, bucket, "data/part2.csv", generateCSVData(50))
	uploadFile(t, ctx, client, bucket, "data/part3.csv", generateCSVData(50))

	tmpFile, err := os.CreateTemp("", "test_s3_glob_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/*.csv",
		DestURI:             destURI,
		DestTable:           "users",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should run without errors")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 150, count, "Should have 150 rows from 3 CSV files")
}

func TestS3ToSQLite_RecursiveGlobPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-recursive-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	uploadFile(t, ctx, client, bucket, "data/2024/01/events.csv", generateCSVData(25))
	uploadFile(t, ctx, client, bucket, "data/2024/02/events.csv", generateCSVData(25))
	uploadFile(t, ctx, client, bucket, "data/2024/03/events.csv", generateCSVData(25))
	uploadFile(t, ctx, client, bucket, "data/2023/12/events.csv", generateCSVData(25))

	tmpFile, err := os.CreateTemp("", "test_s3_recursive_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/**/*.csv",
		DestURI:             destURI,
		DestTable:           "events",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should run without errors")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 100, count, "Should have 100 rows from 4 CSV files across directories")
}

// =====================================================
// TEST: S3 to SQLite (Source Tests - JSONL)
// =====================================================

func TestS3ToSQLite_JSONLSource(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-jsonl-src-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	jsonlData := generateJSONLData(100)
	uploadFile(t, ctx, client, bucket, "data/users.jsonl", jsonlData)

	tmpFile, err := os.CreateTemp("", "test_s3_jsonl_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/users.jsonl",
		DestURI:             destURI,
		DestTable:           "users",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should run without errors")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 100, count, "Should have 100 rows from JSONL")
}

func TestS3ToSQLite_JSONLGlobPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-jsonl-glob-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	uploadFile(t, ctx, client, bucket, "logs/app1.jsonl", generateJSONLData(40))
	uploadFile(t, ctx, client, bucket, "logs/app2.jsonl", generateJSONLData(40))
	uploadFile(t, ctx, client, bucket, "logs/other.txt", []byte("not jsonl"))

	tmpFile, err := os.CreateTemp("", "test_s3_jsonl_glob_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/logs/*.jsonl",
		DestURI:             destURI,
		DestTable:           "logs",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 80, count, "Should have 80 rows from 2 JSONL files (not txt)")
}

// =====================================================
// TEST: S3 to SQLite (Source Tests - Parquet)
// =====================================================

func TestS3ToSQLite_ParquetSource(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-parquet-src-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	parquetData := generateParquetData(t, 100)
	uploadFile(t, ctx, client, bucket, "data/users.parquet", parquetData)

	tmpFile, err := os.CreateTemp("", "test_s3_parquet_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/users.parquet",
		DestURI:             destURI,
		DestTable:           "users",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should run without errors")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 100, count, "Should have 100 rows from Parquet")
}

// =====================================================
// TEST: Gzip Compressed Files
// =====================================================

func TestS3ToSQLite_GzipCSV(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-gzip-csv-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	csvData := generateCSVData(100)
	gzipData := compressGzip(t, csvData)
	uploadFile(t, ctx, client, bucket, "data/users.csv.gz", gzipData)

	tmpFile, err := os.CreateTemp("", "test_s3_gzip_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/users.csv.gz",
		DestURI:             destURI,
		DestTable:           "users",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should handle gzipped CSV")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 100, count, "Should have 100 rows from gzipped CSV")
}

func TestS3ToSQLite_GzipJSONL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-gzip-jsonl-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	jsonlData := generateJSONLData(100)
	gzipData := compressGzip(t, jsonlData)
	uploadFile(t, ctx, client, bucket, "data/events.jsonl.gz", gzipData)

	tmpFile, err := os.CreateTemp("", "test_s3_gzip_jsonl_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/events.jsonl.gz",
		DestURI:             destURI,
		DestTable:           "events",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should handle gzipped JSONL")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 100, count, "Should have 100 rows from gzipped JSONL")
}

func TestS3ToSQLite_GzipGlobPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-gzip-glob-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	uploadFile(t, ctx, client, bucket, "logs/day1.csv.gz", compressGzip(t, generateCSVData(30)))
	uploadFile(t, ctx, client, bucket, "logs/day2.csv.gz", compressGzip(t, generateCSVData(30)))
	uploadFile(t, ctx, client, bucket, "logs/day3.csv.gz", compressGzip(t, generateCSVData(40)))

	tmpFile, err := os.CreateTemp("", "test_s3_gzip_glob_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/logs/*.csv.gz",
		DestURI:             destURI,
		DestTable:           "logs",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 100, count, "Should have 100 rows from 3 gzipped CSV files")
}

// =====================================================
// TEST: File Type Hints
// =====================================================

func TestS3ToSQLite_FileTypeHint_JSONL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-hint-jsonl-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	jsonlData := generateJSONLData(50)
	uploadFile(t, ctx, client, bucket, "data/events.log", jsonlData)

	tmpFile, err := os.CreateTemp("", "test_s3_hint_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/events.log#jsonl",
		DestURI:             destURI,
		DestTable:           "events",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should use JSONL format hint")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 50, count, "Should have 50 rows using JSONL hint")
}

func TestS3ToSQLite_FileTypeHint_CSV(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-hint-csv-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	csvData := generateCSVData(50)
	uploadFile(t, ctx, client, bucket, "data/export.dat", csvData)

	tmpFile, err := os.CreateTemp("", "test_s3_hint_csv_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/export.dat#csv",
		DestURI:             destURI,
		DestTable:           "exports",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should use CSV format hint")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM exports").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 50, count, "Should have 50 rows using CSV hint")
}

// =====================================================
// TEST: Round-trip (PostgreSQL -> S3 -> SQLite)
// =====================================================

func TestRoundTrip_PostgresS3SQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-roundtrip-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	sourceURI := sharedPostgresURI(t, "source")
	sourceSchema := uniqueSchemaName(t, "src")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, sourceURI, sourceSchema) })

	setupPostgresSourceData(t, ctx, sourceURI, sourceSchema, "patients")

	cfg1 := &ingestconfig.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         sourceSchema + ".patients",
		DestURI:             minio.uri,
		DestTable:           bucket + "/patients",
		IncrementalStrategy: "replace",
	}

	p1 := pipeline.New(cfg1)
	err := p1.Run(ctx)
	require.NoError(t, err, "Stage 1: PostgreSQL -> S3 should succeed")

	keys := listObjects(t, ctx, client, bucket, "patients/")
	require.NotEmpty(t, keys, "Should have parquet files in S3")

	tmpFile, err := os.CreateTemp("", "test_roundtrip_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg2 := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/patients/*.parquet",
		DestURI:             destURI,
		DestTable:           "patients",
		IncrementalStrategy: "replace",
	}

	p2 := pipeline.New(cfg2)
	err = p2.Run(ctx)
	require.NoError(t, err, "Stage 2: S3 -> SQLite should succeed")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM patients").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 100, count, "Round-trip should preserve all 100 rows")

	t.Log("Round-trip test passed: PostgreSQL -> S3 -> SQLite maintained data integrity")
}

// =====================================================
// TEST: Data Integrity Verification
// =====================================================

func TestS3_DataIntegrity_NumericValues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-integrity-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	jsonlData := []byte(`{"id":1,"value":123.456,"count":42}
{"id":2,"value":789.012,"count":99}
{"id":3,"value":0.001,"count":0}
{"id":4,"value":-999.999,"count":-1}
`)
	uploadFile(t, ctx, client, bucket, "data/numbers.jsonl", jsonlData)

	tmpFile, err := os.CreateTemp("", "test_integrity_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/numbers.jsonl",
		DestURI:             destURI,
		DestTable:           "numbers",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows, err := db.Query("SELECT id, value, count FROM numbers ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	expected := []struct {
		id    int
		value float64
		count int
	}{
		{1, 123.456, 42},
		{2, 789.012, 99},
		{3, 0.001, 0},
		{4, -999.999, -1},
	}

	idx := 0
	for rows.Next() {
		var id, count int
		var value float64
		err := rows.Scan(&id, &value, &count)
		require.NoError(t, err)

		assert.Equal(t, expected[idx].id, id)
		assert.InDelta(t, expected[idx].value, value, 0.001)
		assert.Equal(t, expected[idx].count, count)
		idx++
	}
	assert.Equal(t, 4, idx, "Should have 4 rows")
}

func TestS3_DataIntegrity_SpecialCharacters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-special-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	jsonlData := []byte(`{"id":1,"name":"John \"Doe\"","notes":"Line1\nLine2"}
{"id":2,"name":"Jane O'Connor","notes":"Tab\there"}
{"id":3,"name":"日本語テスト","notes":"Unicode: émojis 🎉"}
{"id":4,"name":"","notes":"Empty name"}
`)
	uploadFile(t, ctx, client, bucket, "data/special.jsonl", jsonlData)

	tmpFile, err := os.CreateTemp("", "test_special_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/special.jsonl",
		DestURI:             destURI,
		DestTable:           "special",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM special").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 4, count)

	var name string
	err = db.QueryRow("SELECT name FROM special WHERE id = 3").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "日本語テスト", name, "Should preserve Unicode characters")
}

// =====================================================
// TEST: Error Handling
// =====================================================

func TestS3_Error_NonExistentBucket(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)

	tmpFile, err := os.CreateTemp("", "test_error_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         "nonexistent-bucket-xyz/data/*.csv",
		DestURI:             destURI,
		DestTable:           "data",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	assert.Error(t, err, "Should fail for non-existent bucket")
}

func TestS3_Error_NoMatchingFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-nomatch-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	uploadFile(t, ctx, client, bucket, "data/file.txt", []byte("not csv"))

	tmpFile, err := os.CreateTemp("", "test_nomatch_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/*.csv",
		DestURI:             destURI,
		DestTable:           "data",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	assert.Error(t, err, "Should fail when no files match pattern")
}

// =====================================================
// TEST: Large File Handling
// =====================================================

func TestS3_LargeFile_CSV(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	minio := getMinioEnv(t)
	client := createMinioClient(t, minio.endpoint)

	bucket := fmt.Sprintf("test-large-%d", time.Now().UnixNano())
	createBucket(t, ctx, client, bucket)

	csvData := generateCSVData(10000)
	uploadFile(t, ctx, client, bucket, "data/large.csv", csvData)

	tmpFile, err := os.CreateTemp("", "test_large_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &ingestconfig.IngestConfig{
		SourceURI:           minio.uri,
		SourceTable:         bucket + "/data/large.csv",
		DestURI:             destURI,
		DestTable:           "large_data",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Should handle large CSV file")

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM large_data").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 10000, count, "Should have all 10000 rows")
}

// =====================================================
// Helper Functions
// =====================================================

func generateCSVData(rows int) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	_ = w.Write([]string{"id", "name", "email", "age"})

	for i := 1; i <= rows; i++ {
		_ = w.Write([]string{
			fmt.Sprintf("%d", i),
			fmt.Sprintf("User %d", i),
			fmt.Sprintf("user%d@example.com", i),
			fmt.Sprintf("%d", 20+(i%50)),
		})
	}
	w.Flush()
	return buf.Bytes()
}

func generateJSONLData(rows int) []byte {
	var buf bytes.Buffer
	for i := 1; i <= rows; i++ {
		record := map[string]interface{}{
			"id":    i,
			"name":  fmt.Sprintf("User %d", i),
			"email": fmt.Sprintf("user%d@example.com", i),
			"age":   20 + (i % 50),
		}
		data, _ := json.Marshal(record)
		buf.Write(data)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func generateParquetData(t *testing.T, rows int) []byte {
	t.Helper()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "email", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "age", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)

	mem := memory.NewGoAllocator()

	idBuilder := array.NewInt64Builder(mem)
	nameBuilder := array.NewStringBuilder(mem)
	emailBuilder := array.NewStringBuilder(mem)
	ageBuilder := array.NewInt64Builder(mem)

	for i := 1; i <= rows; i++ {
		idBuilder.Append(int64(i))
		nameBuilder.Append(fmt.Sprintf("User %d", i))
		emailBuilder.Append(fmt.Sprintf("user%d@example.com", i))
		ageBuilder.Append(int64(20 + (i % 50)))
	}

	idArr := idBuilder.NewArray()
	nameArr := nameBuilder.NewArray()
	emailArr := emailBuilder.NewArray()
	ageArr := ageBuilder.NewArray()

	record := array.NewRecordBatch(schema, []arrow.Array{idArr, nameArr, emailArr, ageArr}, int64(rows))
	defer record.Release()
	defer idArr.Release()
	defer nameArr.Release()
	defer emailArr.Release()
	defer ageArr.Release()

	var buf bytes.Buffer
	writerProps := parquet.NewWriterProperties(
		parquet.WithCompression(compress.Codecs.Snappy),
	)
	arrowProps := pqarrow.NewArrowWriterProperties(
		pqarrow.WithStoreSchema(),
	)

	writer, err := pqarrow.NewFileWriter(schema, &buf, writerProps, arrowProps)
	require.NoError(t, err)

	err = writer.WriteBuffered(record)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	return buf.Bytes()
}

func compressGzip(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	_, err := gzWriter.Write(data)
	require.NoError(t, err)
	err = gzWriter.Close()
	require.NoError(t, err)
	return buf.Bytes()
}

// startMinioContainerForMain is called from TestMain
func startMinioContainerForMain(ctx context.Context) (testcontainers.Container, string, string, error) {
	return startMinioContainerRaw(ctx)
}
