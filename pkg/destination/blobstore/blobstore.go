package blobstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/datalakeerror"
	datalakedirectory "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/directory"
	datalakefile "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/file"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bruin-data/ingestr/internal/adlsutil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/google/uuid"
	"google.golang.org/api/option"
)

type Provider string

const (
	ProviderS3            Provider = "s3"
	ProviderGCS           Provider = "gcs"
	ProviderAzure         Provider = "azure"
	ProviderAzureDatalake Provider = "adls"
)

type BlobstoreDestination struct {
	provider    Provider
	s3Client    *s3.Client
	gcsClient   *storage.Client
	azureClient *azblob.Client
	adlsClient  *azureDatalakeClient
	bucketName  string
	basePath    string
	layout      string
	arrowSchema *arrow.Schema
	schema      *schema.TableSchema
	tableName   string
	mu          sync.Mutex
}

func NewBlobstoreDestination() *BlobstoreDestination {
	return &BlobstoreDestination{}
}

func (d *BlobstoreDestination) Schemes() []string {
	return []string{"s3", "gs", "gcs", "az", "azure", "adls", "adlsgen2", "azdatalake", "abfs", "abfss"}
}

func (d *BlobstoreDestination) Connect(ctx context.Context, uri string) error {
	parsed, err := parseBlobstoreURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse blobstore URI: %w", err)
	}

	d.provider = parsed.provider
	d.layout = parsed.layout
	if d.layout == "" {
		d.layout = "{load_id}.{file_id}.{ext}"
	}

	switch d.provider {
	case ProviderS3:
		client, err := createS3Client(ctx, parsed)
		if err != nil {
			return fmt.Errorf("failed to create S3 client: %w", err)
		}
		d.s3Client = client
		config.Debug("[BLOBSTORE] Connected to S3 with layout %s", d.layout)

	case ProviderGCS:
		client, err := createGCSClient(ctx, parsed)
		if err != nil {
			return fmt.Errorf("failed to create GCS client: %w", err)
		}
		d.gcsClient = client
		config.Debug("[BLOBSTORE] Connected to GCS with layout %s", d.layout)

	case ProviderAzure:
		client, err := createAzureClient(ctx, parsed)
		if err != nil {
			return fmt.Errorf("failed to create Azure client: %w", err)
		}
		d.azureClient = client
		config.Debug("[BLOBSTORE] Connected to Azure Blob Storage with layout %s", d.layout)

	case ProviderAzureDatalake:
		client, err := createAzureDatalakeClient(parsed)
		if err != nil {
			return fmt.Errorf("failed to create Azure Data Lake Storage Gen2 client: %w", err)
		}
		d.adlsClient = client
		config.Debug("[BLOBSTORE] Connected to Azure Data Lake Storage Gen2 with layout %s", d.layout)
	}

	return nil
}

func createS3Client(ctx context.Context, parsed *parsedBlobstoreURI) (*s3.Client, error) {
	var opts []func(*awsconfig.LoadOptions) error

	if parsed.accessKeyID != "" && parsed.secretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(parsed.accessKeyID, parsed.secretAccessKey, ""),
		))
	}

	if parsed.region != "" {
		opts = append(opts, awsconfig.WithRegion(parsed.region))
	} else {
		opts = append(opts, awsconfig.WithRegion("us-east-1"))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}

	var s3Opts []func(*s3.Options)
	if parsed.endpointURL != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(parsed.endpointURL)
			o.UsePathStyle = true
		})
	}

	return s3.NewFromConfig(cfg, s3Opts...), nil
}

func createGCSClient(ctx context.Context, parsed *parsedBlobstoreURI) (*storage.Client, error) {
	var opts []option.ClientOption

	if parsed.credentialsFile != "" {
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, parsed.credentialsFile))
	}

	return storage.NewClient(ctx, opts...)
}

func createAzureClient(ctx context.Context, parsed *parsedBlobstoreURI) (*azblob.Client, error) {
	if parsed.accountName == "" {
		return nil, fmt.Errorf("account_name is required for Azure Blob Storage")
	}

	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", parsed.accountName)

	if parsed.accountKey != "" {
		cred, err := azblob.NewSharedKeyCredential(parsed.accountName, parsed.accountKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create shared key credential: %w", err)
		}
		return azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	}

	if parsed.sasToken != "" {
		return azblob.NewClientWithNoCredential(serviceURL+"?"+parsed.sasToken, nil)
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create default Azure credential: %w", err)
	}
	return azblob.NewClient(serviceURL, cred, nil)
}

type azureDatalakeClient struct {
	accountName        string
	newFileClient      func(string) (*datalakefile.Client, error)
	newDirectoryClient func(string) (*datalakedirectory.Client, error)
}

func createAzureDatalakeClient(parsed *parsedBlobstoreURI) (*azureDatalakeClient, error) {
	if parsed.accountName == "" {
		return nil, fmt.Errorf("account_name is required for Azure Data Lake Storage Gen2")
	}

	client := &azureDatalakeClient{
		accountName: parsed.accountName,
	}

	if parsed.accountKey != "" {
		cred, err := azdatalake.NewSharedKeyCredential(parsed.accountName, parsed.accountKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create shared key credential: %w", err)
		}
		client.newFileClient = func(pathURL string) (*datalakefile.Client, error) {
			return datalakefile.NewClientWithSharedKeyCredential(pathURL, cred, nil)
		}
		client.newDirectoryClient = func(pathURL string) (*datalakedirectory.Client, error) {
			return datalakedirectory.NewClientWithSharedKeyCredential(pathURL, cred, nil)
		}
		return client, nil
	}

	if parsed.sasToken != "" {
		sasToken := strings.TrimPrefix(parsed.sasToken, "?")
		client.newFileClient = func(pathURL string) (*datalakefile.Client, error) {
			return datalakefile.NewClientWithNoCredential(adlsutil.AppendSASToken(pathURL, sasToken), nil)
		}
		client.newDirectoryClient = func(pathURL string) (*datalakedirectory.Client, error) {
			return datalakedirectory.NewClientWithNoCredential(adlsutil.AppendSASToken(pathURL, sasToken), nil)
		}
		return client, nil
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create default Azure credential: %w", err)
	}
	client.newFileClient = func(pathURL string) (*datalakefile.Client, error) {
		return datalakefile.NewClient(pathURL, cred, nil)
	}
	client.newDirectoryClient = func(pathURL string) (*datalakedirectory.Client, error) {
		return datalakedirectory.NewClient(pathURL, cred, nil)
	}
	return client, nil
}

func (d *BlobstoreDestination) Close(ctx context.Context) error {
	if d.gcsClient != nil {
		return d.gcsClient.Close()
	}
	return nil
}

func (d *BlobstoreDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.schema = opts.Schema

	bucketName, basePath := parseBucketAndPath(opts.Table)
	d.bucketName = bucketName
	d.basePath = basePath
	d.tableName = opts.Table

	if opts.Schema != nil {
		d.arrowSchema = opts.Schema.ToArrowSchema()
	}

	config.Debug("[BLOBSTORE] Prepared table: bucket=%s, basePath=%s", d.bucketName, d.basePath)
	return nil
}

func parseBucketAndPath(table string) (bucket, path string) {
	parts := strings.SplitN(table, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func (d *BlobstoreDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *BlobstoreDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int
	loadID := uuid.New().String()[:8]
	fileID := 0

	config.Debug("[BLOBSTORE] Starting write to bucket=%s, basePath=%s with layout=%s", d.bucketName, d.basePath, d.layout)

	var buffer bytes.Buffer
	var writer *pqarrow.FileWriter
	var rowsInCurrentFile int64

	flushToBlob := func() error {
		if writer == nil || rowsInCurrentFile == 0 {
			return nil
		}

		if err := writer.Close(); err != nil {
			return fmt.Errorf("failed to close parquet writer: %w", err)
		}
		writer = nil

		path := d.renderLayout(loadID, fileID)
		if err := d.writeBlob(ctx, path, buffer.Bytes()); err != nil {
			return fmt.Errorf("failed to write to blob %s: %w", path, err)
		}

		config.Debug("[BLOBSTORE] Wrote %d rows to %s (%d bytes)", rowsInCurrentFile, path, buffer.Len())
		fileID++
		rowsInCurrentFile = 0
		buffer.Reset()
		return nil
	}

	for result := range records {
		if result.Err != nil {
			return result.Err
		}

		record := result.Batch
		if record == nil || record.NumRows() == 0 {
			if record != nil {
				record.Release()
			}
			continue
		}

		batchNum++
		startBatch := time.Now()

		if writer == nil {
			d.mu.Lock()
			if d.arrowSchema == nil {
				d.arrowSchema = stripSchemaMetadata(record.Schema())
			}
			d.mu.Unlock()

			writerProps := parquet.NewWriterProperties(
				parquet.WithCompression(compress.Codecs.Snappy),
				parquet.WithDictionaryDefault(true),
				parquet.WithDataPageSize(1024*1024),
			)
			arrowProps := pqarrow.NewArrowWriterProperties(
				pqarrow.WithStoreSchema(),
			)

			var err error
			writer, err = pqarrow.NewFileWriter(d.arrowSchema, &buffer, writerProps, arrowProps)
			if err != nil {
				record.Release()
				return fmt.Errorf("failed to create parquet writer: %w", err)
			}
		}

		recordToWrite := record
		shouldRelease := false
		if !record.Schema().Equal(d.arrowSchema) && schemaEqualIgnoringMetadata(record.Schema(), d.arrowSchema) {
			normalized, err := normalizeRecordToSchema(record, d.arrowSchema)
			if err != nil {
				record.Release()
				return fmt.Errorf("failed to normalize record schema: %w", err)
			}
			recordToWrite = normalized
			shouldRelease = true
		}

		if err := writer.WriteBuffered(recordToWrite); err != nil {
			if shouldRelease {
				recordToWrite.Release()
			}
			record.Release()
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		rows := recordToWrite.NumRows()
		totalRows += rows
		rowsInCurrentFile += rows
		config.Debug("[BLOBSTORE] Batch %d: %d rows in %v (total: %d)", batchNum, rows, time.Since(startBatch), totalRows)

		if shouldRelease {
			recordToWrite.Release()
		}
		record.Release()
	}

	if err := flushToBlob(); err != nil {
		return err
	}

	config.Debug("[BLOBSTORE] Total: %d rows written to %d files in %v", totalRows, fileID, time.Since(startTime))
	return nil
}

func (d *BlobstoreDestination) writeBlob(ctx context.Context, path string, data []byte) error {
	fullPath := path
	if d.basePath != "" {
		fullPath = strings.TrimSuffix(d.basePath, "/") + "/" + path
	}

	switch d.provider {
	case ProviderS3:
		_, err := d.s3Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(d.bucketName),
			Key:         aws.String(fullPath),
			Body:        bytes.NewReader(data),
			ContentType: aws.String("application/octet-stream"),
		})
		return err

	case ProviderGCS:
		obj := d.gcsClient.Bucket(d.bucketName).Object(fullPath)
		w := obj.NewWriter(ctx)
		w.ContentType = "application/octet-stream"
		if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
			_ = w.Close()
			return err
		}
		return w.Close()

	case ProviderAzure:
		_, err := d.azureClient.UploadBuffer(ctx, d.bucketName, fullPath, data, nil)
		return err

	case ProviderAzureDatalake:
		if d.adlsClient == nil {
			return fmt.Errorf("azure Data Lake Storage Gen2 client is not initialized")
		}
		return d.adlsClient.uploadBuffer(ctx, d.bucketName, fullPath, data)
	}

	return fmt.Errorf("unsupported provider: %s", d.provider)
}

func (c *azureDatalakeClient) uploadBuffer(ctx context.Context, fileSystem, path string, data []byte) error {
	if err := c.ensureDirectories(ctx, fileSystem, parentDirectory(path)); err != nil {
		return err
	}

	pathURL, err := buildAzureDatalakePathURL(c.accountName, fileSystem, path)
	if err != nil {
		return err
	}

	fileClient, err := c.newFileClient(pathURL)
	if err != nil {
		return fmt.Errorf("failed to create file client: %w", err)
	}

	if err := recreateAzureDatalakeFile(ctx, fileClient, path); err != nil {
		return err
	}

	if err := fileClient.UploadBuffer(ctx, data, nil); err != nil {
		return fmt.Errorf("failed to upload file %s: %w", path, err)
	}
	return nil
}

func recreateAzureDatalakeFile(ctx context.Context, fileClient *datalakefile.Client, path string) error {
	if _, err := fileClient.Create(ctx, nil); err == nil {
		return nil
	} else if !isAzureDatalakeAlreadyExists(err) {
		return fmt.Errorf("failed to create file %s: %w", path, err)
	}

	if _, err := fileClient.Delete(ctx, nil); err != nil {
		return fmt.Errorf("failed to delete existing file %s before upload: %w", path, err)
	}
	if _, err := fileClient.Create(ctx, nil); err != nil {
		return fmt.Errorf("failed to recreate file %s: %w", path, err)
	}
	return nil
}

func (c *azureDatalakeClient) ensureDirectories(ctx context.Context, fileSystem, dirPath string) error {
	dirPath = strings.Trim(dirPath, "/")
	if dirPath == "" {
		return nil
	}

	var current string
	for _, part := range strings.Split(dirPath, "/") {
		if part == "" {
			continue
		}
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}

		pathURL, err := buildAzureDatalakePathURL(c.accountName, fileSystem, current)
		if err != nil {
			return err
		}

		dirClient, err := c.newDirectoryClient(pathURL)
		if err != nil {
			return fmt.Errorf("failed to create directory client for %s: %w", current, err)
		}

		if _, err := dirClient.Create(ctx, nil); err != nil && !isAzureDatalakeAlreadyExists(err) {
			return fmt.Errorf("failed to create directory %s: %w", current, err)
		}
	}

	return nil
}

func buildAzureDatalakePathURL(accountName, fileSystem, path string) (string, error) {
	return adlsutil.PathURL(accountName, fileSystem, path)
}

func parentDirectory(path string) string {
	path = strings.Trim(path, "/")
	if idx := strings.LastIndex(path, "/"); idx != -1 {
		return path[:idx]
	}
	return ""
}

func isAzureDatalakeAlreadyExists(err error) bool {
	return datalakeerror.HasCode(
		err,
		datalakeerror.PathAlreadyExists,
		datalakeerror.ResourceAlreadyExists,
	)
}

func (d *BlobstoreDestination) renderLayout(loadID string, fileID int) string {
	tableName := d.tableName
	if idx := strings.LastIndex(tableName, "/"); idx != -1 {
		tableName = tableName[idx+1:]
	}
	if idx := strings.LastIndex(tableName, "."); idx != -1 {
		tableName = tableName[idx+1:]
	}
	if tableName == "" {
		tableName = "data"
	}

	result := d.layout
	result = strings.ReplaceAll(result, "{table_name}", tableName)
	result = strings.ReplaceAll(result, "{load_id}", loadID)
	result = strings.ReplaceAll(result, "{file_id}", fmt.Sprintf("%d", fileID))
	result = strings.ReplaceAll(result, "{ext}", "parquet")

	return result
}

func (d *BlobstoreDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	config.Debug("[BLOBSTORE] SwapTable called (no-op for blobstore)")
	return nil
}

func (d *BlobstoreDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (d *BlobstoreDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return &blobstoreTransaction{}, nil
}

type blobstoreTransaction struct{}

func (t *blobstoreTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (t *blobstoreTransaction) Commit(ctx context.Context) error {
	return nil
}

func (t *blobstoreTransaction) Rollback(ctx context.Context) error {
	return nil
}

func (d *BlobstoreDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return fmt.Errorf("merge strategy is not supported for blobstore destination")
}

func (d *BlobstoreDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return fmt.Errorf("delete+insert strategy is not supported for blobstore destination")
}

func (d *BlobstoreDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return fmt.Errorf("scd2 strategy is not supported for blobstore destination")
}

func (d *BlobstoreDestination) DropTable(ctx context.Context, table string) error {
	return nil
}

func (d *BlobstoreDestination) SupportsReplaceStrategy() bool      { return true }
func (d *BlobstoreDestination) SupportsAppendStrategy() bool       { return true }
func (d *BlobstoreDestination) SupportsMergeStrategy() bool        { return false }
func (d *BlobstoreDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *BlobstoreDestination) SupportsSCD2Strategy() bool         { return false }
func (d *BlobstoreDestination) SupportsAtomicSwap() bool           { return false }

func (d *BlobstoreDestination) GetScheme() string {
	if d.provider == "" {
		return string(ProviderS3)
	}
	return string(d.provider)
}

func (d *BlobstoreDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

type parsedBlobstoreURI struct {
	provider        Provider
	accessKeyID     string
	secretAccessKey string
	region          string
	endpointURL     string
	credentialsFile string
	accountName     string
	accountKey      string
	sasToken        string
	layout          string
}

func parseBlobstoreURI(uri string) (*parsedBlobstoreURI, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	parsed := &parsedBlobstoreURI{}

	switch u.Scheme {
	case "s3":
		parsed.provider = ProviderS3
		parsed.accessKeyID = u.Query().Get("access_key_id")
		parsed.secretAccessKey = u.Query().Get("secret_access_key")
		parsed.region = u.Query().Get("region")
		parsed.endpointURL = u.Query().Get("endpoint_url")
	case "gs", "gcs":
		parsed.provider = ProviderGCS
		parsed.credentialsFile = u.Query().Get("credentials_file")
	case "az", "azure":
		parsed.provider = ProviderAzure
		parsed.accountName = u.Query().Get("account_name")
		parsed.accountKey = u.Query().Get("account_key")
		parsed.sasToken = u.Query().Get("sas_token")
	case "adls", "adlsgen2", "azdatalake", "abfs", "abfss":
		parsed.provider = ProviderAzureDatalake
		parsed.accountName = adlsutil.ParseAccountName(u)
		parsed.accountKey = u.Query().Get("account_key")
		parsed.sasToken = u.Query().Get("sas_token")
	default:
		return nil, fmt.Errorf("unsupported blobstore scheme: %s", u.Scheme)
	}

	parsed.layout = u.Query().Get("layout")

	return parsed, nil
}

func schemaEqualIgnoringMetadata(a, b *arrow.Schema) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.NumFields() != b.NumFields() {
		return false
	}

	af := make([]arrow.Field, a.NumFields())
	bf := make([]arrow.Field, b.NumFields())
	for i := 0; i < a.NumFields(); i++ {
		f := a.Field(i)
		f.Metadata = arrow.Metadata{}
		af[i] = f
	}
	for i := 0; i < b.NumFields(); i++ {
		f := b.Field(i)
		f.Metadata = arrow.Metadata{}
		bf[i] = f
	}

	na := arrow.NewSchema(af, nil)
	nb := arrow.NewSchema(bf, nil)
	return na.Equal(nb)
}

func stripSchemaMetadata(s *arrow.Schema) *arrow.Schema {
	if s == nil {
		return nil
	}
	fields := make([]arrow.Field, s.NumFields())
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		f.Metadata = arrow.Metadata{}
		fields[i] = f
	}
	return arrow.NewSchema(fields, nil)
}

func normalizeRecordToSchema(rec arrow.RecordBatch, target *arrow.Schema) (arrow.RecordBatch, error) {
	if rec == nil {
		return nil, nil
	}
	if target == nil {
		return nil, fmt.Errorf("target schema is nil")
	}
	if rec.NumCols() != int64(target.NumFields()) {
		return nil, fmt.Errorf("column count mismatch: record=%d schema=%d", rec.NumCols(), target.NumFields())
	}

	cols := make([]arrow.Array, rec.NumCols())
	for i := 0; i < int(rec.NumCols()); i++ {
		col := rec.Column(i)
		col.Retain()
		cols[i] = col
	}

	out := array.NewRecordBatch(target, cols, rec.NumRows())
	for _, c := range cols {
		c.Release()
	}
	return out, nil
}
