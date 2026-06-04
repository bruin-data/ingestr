package blobstore

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/filesystem"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/bruin-data/ingestr/internal/adlsutil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	csvsource "github.com/bruin-data/ingestr/pkg/source/csv"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

type Provider string

const (
	ProviderS3            Provider = "s3"
	ProviderGCS           Provider = "gcs"
	ProviderAzure         Provider = "azure"
	ProviderAzureDatalake Provider = "adls"
	ProviderSFTP          Provider = "sftp"

	defaultParallelism               = 5
	defaultBlobstoreFilePathColumn   = "_ingestr_source_file_path"
	defaultBlobstoreModifiedAtColumn = "_ingestr_source_file_modified_at"
)

type FileFormat string

const (
	FormatCSV     FileFormat = "csv"
	FormatJSONL   FileFormat = "jsonl"
	FormatParquet FileFormat = "parquet"
	FormatUnknown FileFormat = "unknown"
)

type s3FileDiscovery string

const (
	s3FileDiscoveryList            s3FileDiscovery = "list"
	s3FileDiscoveryAthenaInventory s3FileDiscovery = "athena_inventory"
)

type BlobstoreSource struct {
	provider     Provider
	s3Client     *s3.Client
	athenaClient athenaAPI
	gcsClient    *storage.Client
	adlsClient   *azureDatalakeSourceClient
	sftpClient   *sftp.Client
	sshClient    *ssh.Client
	parsedURI    *parsedBlobstoreURI
}

type blobstoreFile struct {
	key          string
	lastModified *time.Time
}

type blobstoreFileMetadata struct {
	incrementalKey string
	lastModified   *time.Time
	filepathColumn string
	filepath       string
}

type athenaAPI interface {
	StartQueryExecution(context.Context, *athena.StartQueryExecutionInput, ...func(*athena.Options)) (*athena.StartQueryExecutionOutput, error)
	GetQueryExecution(context.Context, *athena.GetQueryExecutionInput, ...func(*athena.Options)) (*athena.GetQueryExecutionOutput, error)
	GetQueryResults(context.Context, *athena.GetQueryResultsInput, ...func(*athena.Options)) (*athena.GetQueryResultsOutput, error)
}

func NewBlobstoreSource() *BlobstoreSource {
	return &BlobstoreSource{}
}

func (s *BlobstoreSource) Schemes() []string {
	return []string{"s3", "gs", "gcs", "az", "azure", "adls", "adlsgen2", "azdatalake", "abfs", "abfss", "sftp"}
}

func (s *BlobstoreSource) Connect(ctx context.Context, uri string) error {
	parsed, err := parseBlobstoreURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse blobstore URI: %w", err)
	}

	s.parsedURI = parsed
	s.provider = parsed.provider

	switch s.provider {
	case ProviderS3:
		client, err := createS3Client(ctx, parsed)
		if err != nil {
			return fmt.Errorf("failed to create S3 client: %w", err)
		}
		s.s3Client = client
		if parsed.s3FileDiscovery == s3FileDiscoveryAthenaInventory {
			athenaClient, err := createS3DiscoveryAthenaClient(ctx, parsed)
			if err != nil {
				return fmt.Errorf("failed to create Athena client for S3 file discovery: %w", err)
			}
			s.athenaClient = athenaClient
		}
		config.Debug("[BLOBSTORE-SRC] Connected to S3")

	case ProviderGCS:
		client, err := createGCSClient(ctx, parsed)
		if err != nil {
			return fmt.Errorf("failed to create GCS client: %w", err)
		}
		s.gcsClient = client
		config.Debug("[BLOBSTORE-SRC] Connected to GCS")

	case ProviderSFTP:
		sshConn, sftpConn, err := createSFTPClient(parsed)
		if err != nil {
			return fmt.Errorf("failed to create SFTP client: %w", err)
		}
		s.sshClient = sshConn
		s.sftpClient = sftpConn
		config.Debug("[BLOBSTORE-SRC] Connected to SFTP %s:%s", parsed.sftpHost, parsed.sftpPort)

	case ProviderAzureDatalake:
		client, err := createAzureDatalakeSourceClient(parsed)
		if err != nil {
			return fmt.Errorf("failed to create Azure Data Lake Storage Gen2 client: %w", err)
		}
		s.adlsClient = client
		config.Debug("[BLOBSTORE-SRC] Connected to Azure Data Lake Storage Gen2")

	case ProviderAzure:
		return fmt.Errorf("azure Blob Storage source is not yet implemented")
	}

	return nil
}

func createS3Client(ctx context.Context, parsed *parsedBlobstoreURI) (*s3.Client, error) {
	cfg, err := loadAWSConfig(ctx, parsed, parsed.region)
	if err != nil {
		return nil, err
	}

	s3Opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.DisableLogOutputChecksumValidationSkipped = true
		},
	}

	if parsed.endpointURL != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(parsed.endpointURL)
			o.UsePathStyle = true
		})
	}

	return s3.NewFromConfig(cfg, s3Opts...), nil
}

func createS3DiscoveryAthenaClient(ctx context.Context, parsed *parsedBlobstoreURI) (athenaAPI, error) {
	cfg, err := loadAWSConfig(ctx, parsed, parsed.athenaRegion)
	if err != nil {
		return nil, err
	}
	return athena.NewFromConfig(cfg), nil
}

func loadAWSConfig(ctx context.Context, parsed *parsedBlobstoreURI, region string) (aws.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error

	if parsed.accessKeyID != "" && parsed.secretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(parsed.accessKeyID, parsed.secretAccessKey, ""),
		))
	}

	if region == "" {
		region = parsed.region
	}
	if region == "" {
		region = "us-east-1"
	}
	opts = append(opts, awsconfig.WithRegion(region))

	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

func createGCSClient(ctx context.Context, parsed *parsedBlobstoreURI) (*storage.Client, error) {
	var opts []option.ClientOption

	if parsed.credentialsFile != "" {
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, parsed.credentialsFile))
	}

	return storage.NewClient(ctx, opts...)
}

type azureDatalakeSourceClient struct {
	accountName         string
	newFilesystemClient func(string) (*filesystem.Client, error)
}

func createAzureDatalakeSourceClient(parsed *parsedBlobstoreURI) (*azureDatalakeSourceClient, error) {
	if parsed.accountName == "" {
		return nil, fmt.Errorf("account_name is required for Azure Data Lake Storage Gen2")
	}

	client := &azureDatalakeSourceClient{
		accountName: parsed.accountName,
	}

	if parsed.accountKey != "" {
		cred, err := azdatalake.NewSharedKeyCredential(parsed.accountName, parsed.accountKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create shared key credential: %w", err)
		}
		client.newFilesystemClient = func(fileSystemURL string) (*filesystem.Client, error) {
			return filesystem.NewClientWithSharedKeyCredential(fileSystemURL, cred, nil)
		}
		return client, nil
	}

	if parsed.sasToken != "" {
		sasToken := strings.TrimPrefix(parsed.sasToken, "?")
		client.newFilesystemClient = func(fileSystemURL string) (*filesystem.Client, error) {
			return filesystem.NewClientWithNoCredential(adlsutil.AppendSASToken(fileSystemURL, sasToken), nil)
		}
		return client, nil
	}

	cred, err := parsed.clientCredentials.NewTokenCredential()
	if err != nil {
		return nil, err
	}
	client.newFilesystemClient = func(fileSystemURL string) (*filesystem.Client, error) {
		return filesystem.NewClient(fileSystemURL, cred, nil)
	}
	return client, nil
}

func createSFTPClient(parsed *parsedBlobstoreURI) (*ssh.Client, *sftp.Client, error) {
	if parsed.sftpPassword == "" && parsed.sftpKeyFile == "" {
		return nil, nil, fmt.Errorf("SFTP connection requires either a password or a key_file")
	}

	addr := fmt.Sprintf("%s:%s", parsed.sftpHost, parsed.sftpPort)

	sshConfig := &ssh.ClientConfig{
		User:            parsed.sftpUsername,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // SFTP sources connect to user-specified hosts
	}

	if parsed.sftpPassword != "" {
		sshConfig.Auth = []ssh.AuthMethod{
			ssh.Password(parsed.sftpPassword),
		}
	}

	if parsed.sftpKeyFile != "" {
		key, err := os.ReadFile(parsed.sftpKeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read SSH key file: %w", err)
		}
		var signer ssh.Signer
		if parsed.sftpKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(parsed.sftpKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse SSH key: %w", err)
		}
		sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeys(signer))
	}

	sshConn, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	sftpConn, err := sftp.NewClient(sshConn)
	if err != nil {
		_ = sshConn.Close()
		return nil, nil, fmt.Errorf("failed to create SFTP session: %w", err)
	}

	return sshConn, sftpConn, nil
}

func (s *BlobstoreSource) Close(ctx context.Context) error {
	if s.sftpClient != nil {
		_ = s.sftpClient.Close()
	}
	if s.sshClient != nil {
		_ = s.sshClient.Close()
	}
	if s.gcsClient != nil {
		return s.gcsClient.Close()
	}
	return nil
}

func (s *BlobstoreSource) HandlesIncrementality() bool {
	// Blobstore still supports user-defined row-level incremental keys from file
	// data. Object modified filtering is a reserved-key read mode, not a global
	// source-managed incrementality contract.
	return false
}

func (s *BlobstoreSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("blobstore source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *BlobstoreSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()

	var bucket, pattern string
	var formatHint FileFormat
	var tableEncoding string

	if s.provider == ProviderSFTP {
		bucket, pattern, formatHint, tableEncoding = parseSFTPTablePattern(table)
		config.Debug("[BLOBSTORE-SRC] Reading from SFTP pattern=%s, formatHint=%s, encoding=%q", pattern, formatHint, tableEncoding)
	} else {
		bucket, pattern, formatHint, tableEncoding = parseTablePattern(table)
		config.Debug("[BLOBSTORE-SRC] Reading from bucket=%s, pattern=%s, formatHint=%s, encoding=%q", bucket, pattern, formatHint, tableEncoding)
	}

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}

	config.Debug("[BLOBSTORE-SRC] Starting with parallelism %d", parallelism)

	results := make(chan source.RecordBatchResult, parallelism*2)
	fileChan := make(chan blobstoreFile, parallelism*2)

	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fileKey := range fileChan {
				select {
				case <-ctx.Done():
					return
				default:
				}
				s.processFile(ctx, bucket, fileKey, formatHint, tableEncoding, batchSize, opts, results)
			}
		}()
	}

	// List files and send to channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(fileChan)
		count, err := s.listMatchingFiles(ctx, bucket, pattern, opts, fileChan)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to list files: %w", err)}
		} else if count == 0 {
			if handlesBlobstoreModifiedIncrementality(s.provider) && usesBlobstoreModifiedIncrementality(opts) && hasModifiedInterval(opts) {
				config.Debug("[BLOBSTORE-SRC] No files found matching pattern=%s/%s and modified interval", bucket, pattern)
			} else {
				results <- source.RecordBatchResult{Err: fmt.Errorf("no files found matching pattern: %s/%s", bucket, pattern)}
			}
		} else {
			config.Debug("[BLOBSTORE-SRC] Found %d matching files", count)
		}
	}()

	// Close results channel when all goroutines done
	go func() {
		wg.Wait()
		close(results)
		config.Debug("[BLOBSTORE-SRC] Completed in %v", time.Since(startTotal))
	}()

	return results, nil
}

func (s *BlobstoreSource) processFile(ctx context.Context, bucket string, file blobstoreFile, formatHint FileFormat, tableEncoding string, batchSize int, opts source.ReadOptions, results chan<- source.RecordBatchResult) {
	startFile := time.Now()
	fileKey := file.key
	format := detectFileFormat(fileKey, formatHint)
	if format == FormatUnknown {
		config.Debug("[BLOBSTORE-SRC] Skipping file with unknown format: %s", fileKey)
		return
	}

	config.Debug("[BLOBSTORE-SRC] Processing file: %s (format: %s)", fileKey, format)

	data, err := retry(3, time.Second, func() ([]byte, error) {
		return s.downloadFile(ctx, bucket, fileKey)
	})
	if err != nil {
		results <- source.RecordBatchResult{Err: fmt.Errorf("failed to download %s after retries: %w", fileKey, err)}
		return
	}

	reader := bytes.NewReader(data)
	var dataReader io.Reader = reader

	if isGzipped(fileKey) {
		gzReader, err := gzip.NewReader(reader)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to decompress gzipped file %s: %w", fileKey, err)}
			return
		}
		decompressed, err := io.ReadAll(gzReader)
		_ = gzReader.Close()
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read gzipped file %s: %w", fileKey, err)}
			return
		}
		dataReader = bytes.NewReader(decompressed)
		data = decompressed
	}

	var totalRows int64
	var batchNum int
	metadata := s.fileMetadata(opts, bucket, file.key, file.lastModified)

	switch format {
	case FormatParquet:
		err = s.readParquetFile(ctx, data, results, &totalRows, &batchNum, opts, metadata)
	case FormatJSONL:
		err = s.readJSONLFile(ctx, dataReader, results, &totalRows, &batchNum, batchSize, opts, metadata)
	case FormatCSV:
		err = s.readCSVFile(ctx, dataReader, tableEncoding, results, &totalRows, &batchNum, batchSize, opts, metadata)
	}

	if err != nil {
		results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read %s: %w", fileKey, err)}
	}

	config.Debug("[BLOBSTORE-SRC] File %s: %d rows in %d batches, read time: %v", fileKey, totalRows, batchNum, time.Since(startFile))
}

func (s *BlobstoreSource) listMatchingFiles(ctx context.Context, bucket, pattern string, opts source.ReadOptions, fileChan chan<- blobstoreFile) (int, error) {
	count := 0
	prefix := extractPrefix(pattern)

	switch s.provider {
	case ProviderS3:
		if s.parsedURI != nil && s.parsedURI.s3FileDiscovery == s3FileDiscoveryAthenaInventory {
			return s.listMatchingS3InventoryFiles(ctx, bucket, pattern, prefix, opts, fileChan)
		}
		s3Count, err := s.listMatchingS3Objects(ctx, bucket, pattern, prefix, opts, fileChan)
		if err != nil {
			return count, err
		}
		count += s3Count

	case ProviderGCS:
		it := s.gcsClient.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: prefix})
		for {
			attrs, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return count, fmt.Errorf("failed to list GCS objects: %w", err)
			}

			updated := attrs.Updated
			if matchesGlobPattern(attrs.Name, pattern) && objectMatchesIncrementalOptions(&updated, opts) {
				select {
				case fileChan <- blobstoreFile{key: attrs.Name, lastModified: copyTimePtr(&updated)}:
					count++
				case <-ctx.Done():
					return count, ctx.Err()
				}
			}
		}

	case ProviderAzureDatalake:
		if s.adlsClient == nil {
			return count, fmt.Errorf("azure Data Lake Storage Gen2 client is not initialized")
		}

		fsClient, err := s.adlsClient.newFilesystemClient(buildAzureDatalakeFilesystemURL(s.adlsClient.accountName, bucket))
		if err != nil {
			return count, fmt.Errorf("failed to create ADLS filesystem client: %w", err)
		}

		opts := &filesystem.ListPathsOptions{}
		// The Azure SDK maps Prefix to the ADLS "directory" query parameter,
		// so exact file paths must list their parent directory.
		listDirectory := azureDatalakeListDirectory(pattern)
		if listDirectory != "" {
			opts.Prefix = &listDirectory
		}
		pager := fsClient.NewListPathsPager(true, opts)
		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				return count, fmt.Errorf("failed to list ADLS paths: %w", err)
			}

			for _, item := range resp.Paths {
				if item == nil || item.Name == nil {
					continue
				}
				if item.IsDirectory != nil && *item.IsDirectory {
					continue
				}

				key := *item.Name
				if matchesGlobPattern(key, pattern) {
					select {
					case fileChan <- blobstoreFile{key: key}:
						count++
					case <-ctx.Done():
						return count, ctx.Err()
					}
				}
			}
		}

	case ProviderSFTP:
		baseDir := "/"
		if prefix != "" {
			baseDir = "/" + prefix
		}
		walker := s.sftpClient.Walk(baseDir)
		for walker.Step() {
			select {
			case <-ctx.Done():
				return count, ctx.Err()
			default:
			}
			if err := walker.Err(); err != nil {
				return count, fmt.Errorf("error walking SFTP path %s: %w", walker.Path(), err)
			}
			if walker.Stat().IsDir() {
				continue
			}
			relPath := strings.TrimPrefix(walker.Path(), "/")
			if matchesGlobPattern(relPath, pattern) {
				select {
				case fileChan <- blobstoreFile{key: walker.Path()}:
					count++
				case <-ctx.Done():
					return count, ctx.Err()
				}
			}
		}
	}

	return count, nil
}

func (s *BlobstoreSource) listMatchingS3Objects(ctx context.Context, bucket, pattern, prefix string, opts source.ReadOptions, fileChan chan<- blobstoreFile) (int, error) {
	count := 0
	paginator := s3.NewListObjectsV2Paginator(s.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return count, fmt.Errorf("failed to list S3 objects: %w", err)
		}

		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if matchesGlobPattern(key, pattern) && objectMatchesIncrementalOptions(obj.LastModified, opts) {
				select {
				case fileChan <- blobstoreFile{key: key, lastModified: copyTimePtr(obj.LastModified)}:
					count++
				case <-ctx.Done():
					return count, ctx.Err()
				}
			}
		}
	}

	return count, nil
}

func (s *BlobstoreSource) listMatchingS3InventoryFiles(ctx context.Context, bucket, pattern, prefix string, opts source.ReadOptions, fileChan chan<- blobstoreFile) (int, error) {
	if s.athenaClient == nil {
		return 0, fmt.Errorf("Athena client is not initialized for S3 file discovery")
	}
	if s.parsedURI == nil {
		return 0, fmt.Errorf("S3 source URI is not initialized")
	}

	query, database, err := buildS3InventoryQuery(s.parsedURI, bucket, prefix, opts)
	if err != nil {
		return 0, err
	}
	config.Debug("[BLOBSTORE-SRC] Discovering S3 files with Athena inventory query: %s", query)

	execID, err := s.startAthenaQuery(ctx, query, database)
	if err != nil {
		return 0, err
	}
	if err := s.waitForAthenaQuery(ctx, execID); err != nil {
		return 0, err
	}

	return s.streamS3InventoryQueryResults(ctx, execID, pattern, opts, fileChan)
}

func (s *BlobstoreSource) startAthenaQuery(ctx context.Context, query, database string) (string, error) {
	input := &athena.StartQueryExecutionInput{
		QueryString: aws.String(query),
		ResultConfiguration: &athenatypes.ResultConfiguration{
			OutputLocation: aws.String(s.parsedURI.athenaResultsLocation),
		},
	}
	if database != "" {
		input.QueryExecutionContext = &athenatypes.QueryExecutionContext{Database: aws.String(database)}
	}
	if s.parsedURI.athenaWorkgroup != "" {
		input.WorkGroup = aws.String(s.parsedURI.athenaWorkgroup)
	}

	resp, err := s.athenaClient.StartQueryExecution(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to start Athena inventory query: %w", err)
	}
	if resp.QueryExecutionId == nil || *resp.QueryExecutionId == "" {
		return "", fmt.Errorf("failed to start Athena inventory query: empty execution id")
	}
	return *resp.QueryExecutionId, nil
}

func (s *BlobstoreSource) waitForAthenaQuery(ctx context.Context, executionID string) error {
	delay := 400 * time.Millisecond
	for {
		resp, err := s.athenaClient.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{QueryExecutionId: aws.String(executionID)})
		if err != nil {
			return fmt.Errorf("failed to get Athena inventory query status: %w", err)
		}
		if resp.QueryExecution == nil || resp.QueryExecution.Status == nil || resp.QueryExecution.Status.State == "" {
			return fmt.Errorf("failed to get Athena inventory query status: missing state")
		}

		switch resp.QueryExecution.Status.State {
		case athenatypes.QueryExecutionStateSucceeded:
			return nil
		case athenatypes.QueryExecutionStateFailed, athenatypes.QueryExecutionStateCancelled:
			reason := "unknown reason"
			if resp.QueryExecution.Status.StateChangeReason != nil && *resp.QueryExecution.Status.StateChangeReason != "" {
				reason = *resp.QueryExecution.Status.StateChangeReason
			}
			return fmt.Errorf("Athena inventory query %s %s: %s", executionID, strings.ToLower(string(resp.QueryExecution.Status.State)), reason)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			if delay < 5*time.Second {
				delay *= 2
				if delay > 5*time.Second {
					delay = 5 * time.Second
				}
			}
		}
	}
}

func (s *BlobstoreSource) streamS3InventoryQueryResults(ctx context.Context, executionID, pattern string, opts source.ReadOptions, fileChan chan<- blobstoreFile) (int, error) {
	count := 0
	skipHeader := true
	var nextToken *string

	for {
		out, err := s.athenaClient.GetQueryResults(ctx, &athena.GetQueryResultsInput{
			QueryExecutionId: aws.String(executionID),
			NextToken:        nextToken,
			MaxResults:       aws.Int32(1000),
		})
		if err != nil {
			return count, fmt.Errorf("failed to get Athena inventory query results: %w", err)
		}

		if out.ResultSet != nil {
			for i, row := range out.ResultSet.Rows {
				if skipHeader && i == 0 {
					continue
				}
				key := athenaRowValue(row, 0)
				if key == "" {
					continue
				}
				modified, err := parseAthenaInventoryTime(athenaRowValue(row, 1))
				if err != nil {
					return count, fmt.Errorf("failed to parse Athena inventory modified timestamp for key %q: %w", key, err)
				}
				if matchesGlobPattern(key, pattern) && objectMatchesIncrementalOptions(modified, opts) {
					select {
					case fileChan <- blobstoreFile{key: key, lastModified: modified}:
						count++
					case <-ctx.Done():
						return count, ctx.Err()
					}
				}
			}
		}

		skipHeader = false
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		nextToken = out.NextToken
	}

	return count, nil
}

func (s *BlobstoreSource) fileMetadata(opts source.ReadOptions, bucket, key string, lastModified *time.Time) blobstoreFileMetadata {
	if !handlesBlobstoreModifiedIncrementality(s.provider) || !usesBlobstoreModifiedIncrementality(opts) {
		return blobstoreFileMetadata{}
	}

	metadata := blobstoreFileMetadata{}
	if lastModified != nil && !lastModified.IsZero() && !isExcludedColumn(defaultBlobstoreModifiedAtColumn, opts.ExcludeColumns) {
		t := lastModified.UTC()
		metadata.incrementalKey = defaultBlobstoreModifiedAtColumn
		metadata.lastModified = &t
	}

	if key != "" && !isExcludedColumn(defaultBlobstoreFilePathColumn, opts.ExcludeColumns) {
		metadata.filepathColumn = defaultBlobstoreFilePathColumn
		metadata.filepath = s.filepath(bucket, key)
	}

	return metadata
}

func handlesBlobstoreModifiedIncrementality(provider Provider) bool {
	return provider == ProviderS3 || provider == ProviderGCS
}

func usesBlobstoreModifiedIncrementality(opts source.ReadOptions) bool {
	return strings.EqualFold(opts.IncrementalKey, defaultBlobstoreModifiedAtColumn)
}

func hasModifiedInterval(opts source.ReadOptions) bool {
	return opts.IntervalStart != nil || opts.IntervalEnd != nil
}

func objectMatchesIncrementalOptions(lastModified *time.Time, opts source.ReadOptions) bool {
	if !usesBlobstoreModifiedIncrementality(opts) {
		return true
	}
	return objectModifiedInInterval(lastModified, opts.IntervalStart, opts.IntervalEnd)
}

func objectModifiedInInterval(lastModified, intervalStart, intervalEnd *time.Time) bool {
	if intervalStart == nil && intervalEnd == nil {
		return true
	}
	if lastModified == nil || lastModified.IsZero() {
		return false
	}

	modified := lastModified.UTC()
	if intervalStart != nil {
		start := intervalStart.UTC()
		if modified.Before(start) {
			return false
		}
	}
	if intervalEnd != nil {
		end := intervalEnd.UTC()
		if modified.After(end) {
			return false
		}
	}
	return true
}

func copyTimePtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	copied := t.UTC()
	return &copied
}

func (s *BlobstoreSource) filepath(bucket, key string) string {
	if bucket == "" {
		return key
	}

	scheme := string(s.provider)
	if s.provider == ProviderGCS {
		scheme = "gs"
	}
	return scheme + "://" + strings.TrimRight(bucket, "/") + "/" + strings.TrimLeft(key, "/")
}

func isExcludedColumn(name string, excludeColumns []string) bool {
	for _, excluded := range excludeColumns {
		if strings.EqualFold(name, excluded) {
			return true
		}
	}
	return false
}

func buildS3InventoryQuery(parsed *parsedBlobstoreURI, bucket, prefix string, opts source.ReadOptions) (query, database string, err error) {
	database, table, err := parseAthenaTableRef(parsed.athenaInventoryTable)
	if err != nil {
		return "", "", err
	}

	keyColumn := quoteAthenaIdent(parsed.athenaInventoryKeyColumn)
	modifiedColumn := quoteAthenaIdent(parsed.athenaInventoryModifiedColumn)
	query = fmt.Sprintf(
		"SELECT %s, %s FROM %s",
		keyColumn,
		modifiedColumn,
		quoteAthenaTableRef(database, table),
	)

	conditions := []string{
		fmt.Sprintf("%s = '%s'", quoteAthenaIdent(parsed.athenaInventoryBucketColumn), escapeAthenaString(bucket)),
	}
	if prefix != "" {
		conditions = append(conditions, fmt.Sprintf(
			"substr(%s, 1, %d) = '%s'",
			keyColumn,
			len([]rune(prefix)),
			escapeAthenaString(prefix),
		))
	}
	if usesBlobstoreModifiedIncrementality(opts) {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf(
				"%s >= timestamp '%s'",
				modifiedColumn,
				formatAthenaTimestamp(*opts.IntervalStart),
			))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf(
				"%s <= timestamp '%s'",
				modifiedColumn,
				formatAthenaTimestamp(*opts.IntervalEnd),
			))
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query, database, nil
}

func athenaRowValue(row athenatypes.Row, index int) string {
	if index < 0 || index >= len(row.Data) || row.Data[index].VarCharValue == nil {
		return ""
	}
	return *row.Data[index].VarCharValue
}

func parseAthenaInventoryTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	formats := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	var lastErr error
	for _, format := range formats {
		t, err := time.ParseInLocation(format, value, time.UTC)
		if err == nil {
			utc := t.UTC()
			return &utc, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func parseAthenaTableRef(tableRef string) (database, table string, err error) {
	parts := strings.Split(tableRef, ".")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("athena_inventory_table must be qualified as <database>.<table>")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func quoteAthenaTableRef(database, table string) string {
	return quoteAthenaIdent(database) + "." + quoteAthenaIdent(table)
}

func quoteAthenaIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func escapeAthenaString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func formatAthenaTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

func (s *BlobstoreSource) downloadFile(ctx context.Context, bucket, key string) ([]byte, error) {
	switch s.provider {
	case ProviderS3:
		resp, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		return io.ReadAll(resp.Body)

	case ProviderGCS:
		reader, err := s.gcsClient.Bucket(bucket).Object(key).NewReader(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = reader.Close() }()
		return io.ReadAll(reader)

	case ProviderAzureDatalake:
		if s.adlsClient == nil {
			return nil, fmt.Errorf("azure Data Lake Storage Gen2 client is not initialized")
		}

		fsClient, err := s.adlsClient.newFilesystemClient(buildAzureDatalakeFilesystemURL(s.adlsClient.accountName, bucket))
		if err != nil {
			return nil, fmt.Errorf("failed to create ADLS filesystem client: %w", err)
		}

		fileClient := fsClient.NewFileClient(key)
		resp, err := fileClient.DownloadStream(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		return io.ReadAll(resp.Body)

	case ProviderSFTP:
		f, err := s.sftpClient.Open(key)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return io.ReadAll(f)
	}

	return nil, fmt.Errorf("unsupported provider: %s", s.provider)
}

func (s *BlobstoreSource) readParquetFile(ctx context.Context, data []byte, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, opts source.ReadOptions, metadata blobstoreFileMetadata) error {
	reader := bytes.NewReader(data)
	pr, err := file.NewParquetReader(reader)
	if err != nil {
		return fmt.Errorf("failed to open parquet reader: %w", err)
	}

	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		return fmt.Errorf("failed to create parquet arrow reader: %w", err)
	}

	tbl, err := fr.ReadTable(ctx)
	if err != nil {
		return fmt.Errorf("failed to read parquet table: %w", err)
	}
	defer tbl.Release()

	chunkSize := int64(opts.PageSize)
	if chunkSize <= 0 {
		chunkSize = 10000
	}

	tr := array.NewTableReader(tbl, chunkSize)
	defer tr.Release()

	for tr.Next() {
		rec := tr.RecordBatch()
		rec.Retain()

		*batchNum++
		rows := rec.NumRows()
		*totalRows += rows
		config.Debug("[BLOBSTORE-SRC] Parquet batch %d: %d rows (total: %d)", *batchNum, rows, *totalRows)

		if err := sendRecordBatchWithMetadata(results, rec, metadata); err != nil {
			return err
		}

		if opts.Limit > 0 && *totalRows >= int64(opts.Limit) {
			break
		}
	}

	return tr.Err()
}

func (s *BlobstoreSource) readJSONLFile(ctx context.Context, reader io.Reader, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions, metadata blobstoreFileMetadata) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	items := make([]map[string]interface{}, 0, batchSize)
	lineNum := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineNum++

		if line == "" {
			continue
		}

		var item map[string]interface{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return fmt.Errorf("failed to parse JSON at line %d: %w", lineNum, err)
		}

		items = append(items, item)

		if len(items) >= batchSize {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert to Arrow: %w", err)
			}

			*batchNum++
			*totalRows += int64(len(items))
			config.Debug("[BLOBSTORE-SRC] JSONL batch %d: %d items (total: %d)", *batchNum, len(items), *totalRows)

			if err := sendRecordBatchWithMetadata(results, record, metadata); err != nil {
				return err
			}
			items = make([]map[string]interface{}, 0, batchSize)

			if opts.Limit > 0 && *totalRows >= int64(opts.Limit) {
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading JSONL file: %w", err)
	}

	if len(items) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to Arrow: %w", err)
		}

		*batchNum++
		*totalRows += int64(len(items))
		config.Debug("[BLOBSTORE-SRC] JSONL batch %d: %d items (total: %d)", *batchNum, len(items), *totalRows)

		if err := sendRecordBatchWithMetadata(results, record, metadata); err != nil {
			return err
		}
	}

	return nil
}

func (s *BlobstoreSource) readCSVFile(ctx context.Context, reader io.Reader, tableEncoding string, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions, metadata blobstoreFileMetadata) error {
	decoded, err := csvsource.Decode(reader, tableEncoding)
	if err != nil {
		return fmt.Errorf("failed to set up CSV decoder: %w", err)
	}
	csvReader := csv.NewReader(decoded)
	csvReader.FieldsPerRecord = -1

	headers, err := csvReader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV headers: %w", err)
	}

	rows := make([]map[string]interface{}, 0, batchSize)
	lineNum := 1

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read CSV row %d: %w", lineNum+1, err)
		}
		lineNum++

		row := make(map[string]interface{})
		for i, h := range headers {
			if i < len(record) {
				row[h] = parseCSVValue(record[i])
			}
		}
		rows = append(rows, row)

		if len(rows) >= batchSize {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert CSV to Arrow: %w", err)
			}

			*batchNum++
			*totalRows += int64(len(rows))
			config.Debug("[BLOBSTORE-SRC] CSV batch %d: %d rows (total: %d)", *batchNum, len(rows), *totalRows)

			if err := sendRecordBatchWithMetadata(results, rec, metadata); err != nil {
				return err
			}
			rows = make([]map[string]interface{}, 0, batchSize)

			if opts.Limit > 0 && *totalRows >= int64(opts.Limit) {
				break
			}
		}
	}

	if len(rows) > 0 {
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert CSV to Arrow: %w", err)
		}

		*batchNum++
		*totalRows += int64(len(rows))
		config.Debug("[BLOBSTORE-SRC] CSV batch %d: %d rows (total: %d)", *batchNum, len(rows), *totalRows)

		if err := sendRecordBatchWithMetadata(results, rec, metadata); err != nil {
			return err
		}
	}

	return nil
}

func sendRecordBatchWithMetadata(results chan<- source.RecordBatchResult, record arrow.RecordBatch, metadata blobstoreFileMetadata) error {
	out, added, err := addBlobstoreMetadataColumns(record, metadata)
	if err != nil {
		record.Release()
		return err
	}
	if added {
		record.Release()
	}
	results <- source.RecordBatchResult{Batch: out}
	return nil
}

func addBlobstoreMetadataColumns(record arrow.RecordBatch, metadata blobstoreFileMetadata) (arrow.RecordBatch, bool, error) {
	if record == nil {
		return record, false, nil
	}

	addLastModified := metadata.incrementalKey != "" && metadata.lastModified != nil
	addFilepath := metadata.filepathColumn != "" && metadata.filepath != ""
	if !addLastModified && !addFilepath {
		return record, false, nil
	}
	if addLastModified && addFilepath && strings.EqualFold(metadata.incrementalKey, metadata.filepathColumn) {
		return nil, false, fmt.Errorf("blobstore metadata columns conflict on %q; choose a different --incremental-key", metadata.incrementalKey)
	}

	for _, name := range []string{metadata.incrementalKey, metadata.filepathColumn} {
		if name == "" {
			continue
		}
		if recordHasColumn(record, name) {
			return nil, false, fmt.Errorf("blobstore metadata column %q already exists in file data; choose a different --incremental-key or exclude the column", name)
		}
	}

	addedCols := 0
	if addLastModified {
		addedCols++
	}
	if addFilepath {
		addedCols++
	}

	fields := make([]arrow.Field, int(record.NumCols())+addedCols)
	columns := make([]arrow.Array, int(record.NumCols())+addedCols)
	for i := 0; i < int(record.NumCols()); i++ {
		fields[i] = record.Schema().Field(i)
		columns[i] = record.Column(i)
		columns[i].Retain()
	}

	nextCol := int(record.NumCols())
	if addLastModified {
		timestampType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
		fields[nextCol] = arrow.Field{
			Name:     metadata.incrementalKey,
			Type:     timestampType,
			Nullable: false,
		}

		builder := array.NewTimestampBuilder(memory.DefaultAllocator, timestampType)
		for i := int64(0); i < record.NumRows(); i++ {
			builder.Append(arrow.Timestamp(metadata.lastModified.UTC().UnixMicro()))
		}
		columns[nextCol] = builder.NewArray()
		builder.Release()
		nextCol++
	}

	if addFilepath {
		fields[nextCol] = arrow.Field{
			Name:     metadata.filepathColumn,
			Type:     arrow.BinaryTypes.String,
			Nullable: false,
		}

		builder := array.NewStringBuilder(memory.DefaultAllocator)
		for i := int64(0); i < record.NumRows(); i++ {
			builder.Append(metadata.filepath)
		}
		columns[nextCol] = builder.NewArray()
		builder.Release()
	}

	newRecord := array.NewRecordBatch(arrow.NewSchema(fields, nil), columns, record.NumRows())
	for _, col := range columns {
		col.Release()
	}
	return newRecord, true, nil
}

func recordHasColumn(record arrow.RecordBatch, name string) bool {
	for _, field := range record.Schema().Fields() {
		if strings.EqualFold(field.Name, name) {
			return true
		}
	}
	return false
}

func parseCSVValue(s string) interface{} {
	s = strings.TrimSpace(s)

	if s == "" {
		return nil
	}

	// Boolean
	lower := strings.ToLower(s)
	if lower == "true" {
		return true
	}
	if lower == "false" {
		return false
	}

	// Integer
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	// Float
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	return s
}

type parsedBlobstoreURI struct {
	provider                      Provider
	accessKeyID                   string
	secretAccessKey               string
	region                        string
	endpointURL                   string
	s3FileDiscovery               s3FileDiscovery
	athenaInventoryTable          string
	athenaInventoryBucketColumn   string
	athenaInventoryKeyColumn      string
	athenaInventoryModifiedColumn string
	athenaResultsLocation         string
	athenaWorkgroup               string
	athenaRegion                  string
	credentialsFile               string
	accountName                   string
	accountKey                    string
	sasToken                      string
	clientCredentials             adlsutil.ClientCredentials
	sftpHost                      string
	sftpPort                      string
	sftpUsername                  string
	sftpPassword                  string
	sftpKeyFile                   string
	sftpKeyPassphrase             string
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
		if err := parseS3BlobstoreURIOptions(u, parsed); err != nil {
			return nil, err
		}
	case "gs", "gcs":
		parsed.provider = ProviderGCS
		parsed.credentialsFile = u.Query().Get("credentials_file")
	case "az", "azure":
		parsed.provider = ProviderAzure
		parsed.accountName = u.Query().Get("account_name")
		parsed.accountKey = u.Query().Get("account_key")
		parsed.sasToken = u.Query().Get("sas_token")
		parsed.clientCredentials = adlsutil.ParseClientCredentials(u.Query())
	case "adls", "adlsgen2", "azdatalake", "abfs", "abfss":
		parsed.provider = ProviderAzureDatalake
		parsed.accountName = adlsutil.ParseAccountName(u)
		parsed.accountKey = u.Query().Get("account_key")
		parsed.sasToken = u.Query().Get("sas_token")
		parsed.clientCredentials = adlsutil.ParseClientCredentials(u.Query())
	case "sftp":
		parsed.provider = ProviderSFTP
		parsed.sftpHost = u.Hostname()
		parsed.sftpPort = u.Port()
		if parsed.sftpPort == "" {
			parsed.sftpPort = "22"
		}
		parsed.sftpUsername = u.User.Username()
		parsed.sftpPassword, _ = u.User.Password()
		parsed.sftpKeyFile = u.Query().Get("key_file")
		parsed.sftpKeyPassphrase = u.Query().Get("key_passphrase")
	default:
		return nil, fmt.Errorf("unsupported blobstore scheme: %s", u.Scheme)
	}

	return parsed, nil
}

func parseS3BlobstoreURIOptions(u *url.URL, parsed *parsedBlobstoreURI) error {
	q := u.Query()
	parsed.accessKeyID = q.Get("access_key_id")
	parsed.secretAccessKey = q.Get("secret_access_key")
	parsed.region = q.Get("region")
	parsed.endpointURL = q.Get("endpoint_url")

	discovery := strings.TrimSpace(q.Get("file_discovery"))
	switch discovery {
	case "", string(s3FileDiscoveryList), "s3_list", "list_objects":
		parsed.s3FileDiscovery = s3FileDiscoveryList
	case string(s3FileDiscoveryAthenaInventory):
		parsed.s3FileDiscovery = s3FileDiscoveryAthenaInventory
	default:
		return fmt.Errorf("unsupported S3 file_discovery %q; supported values are %q and %q", discovery, s3FileDiscoveryList, s3FileDiscoveryAthenaInventory)
	}

	parsed.athenaInventoryTable = strings.TrimSpace(q.Get("athena_inventory_table"))
	parsed.athenaInventoryBucketColumn = strings.TrimSpace(q.Get("athena_inventory_bucket_column"))
	if parsed.athenaInventoryBucketColumn == "" {
		parsed.athenaInventoryBucketColumn = "bucket"
	}
	parsed.athenaInventoryKeyColumn = strings.TrimSpace(q.Get("athena_inventory_key_column"))
	if parsed.athenaInventoryKeyColumn == "" {
		parsed.athenaInventoryKeyColumn = "key"
	}
	parsed.athenaInventoryModifiedColumn = strings.TrimSpace(q.Get("athena_inventory_modified_column"))
	if parsed.athenaInventoryModifiedColumn == "" {
		parsed.athenaInventoryModifiedColumn = "last_modified_date"
	}
	parsed.athenaWorkgroup = strings.TrimSpace(q.Get("athena_workgroup"))
	parsed.athenaRegion = strings.TrimSpace(q.Get("athena_region"))
	parsed.athenaResultsLocation = strings.TrimSpace(q.Get("athena_results_location"))

	if parsed.s3FileDiscovery != s3FileDiscoveryAthenaInventory {
		return nil
	}
	if parsed.athenaInventoryTable == "" {
		return fmt.Errorf("athena_inventory_table is required when file_discovery=%s", s3FileDiscoveryAthenaInventory)
	}
	resultsLocation, err := normalizeS3Location(parsed.athenaResultsLocation, "athena_results_location")
	if err != nil {
		return err
	}
	parsed.athenaResultsLocation = resultsLocation
	if _, _, err := parseAthenaTableRef(parsed.athenaInventoryTable); err != nil {
		return err
	}
	return nil
}

func normalizeS3Location(value, field string) (string, error) {
	s := strings.TrimSpace(value)
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	s = strings.TrimPrefix(s, "s3://")
	s = strings.TrimLeft(s, "/")
	if s == "" {
		return "", fmt.Errorf("invalid %s %q", field, value)
	}
	out := "s3://" + s
	if !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out, nil
}

func buildAzureDatalakeFilesystemURL(accountName, fileSystem string) string {
	return adlsutil.FilesystemURL(accountName, fileSystem)
}

func parseTableHints(s string) (FileFormat, string) {
	formatHint := FormatUnknown
	encoding := ""
	for _, hint := range strings.Split(s, ",") {
		hint = strings.TrimSpace(hint)
		if hint == "" {
			continue
		}
		if eq := strings.Index(hint, "="); eq > 0 {
			key := strings.ToLower(hint[:eq])
			val := hint[eq+1:]
			if key == "encoding" {
				encoding = val
			}
			continue
		}
		switch strings.ToLower(hint) {
		case "csv":
			formatHint = FormatCSV
		case "jsonl", "ndjson":
			formatHint = FormatJSONL
		case "parquet":
			formatHint = FormatParquet
		}
	}
	return formatHint, encoding
}

func parseSFTPTablePattern(table string) (bucket, pattern string, formatHint FileFormat, encoding string) {
	formatHint = FormatUnknown

	if idx := strings.Index(table, "#"); idx != -1 {
		formatHint, encoding = parseTableHints(table[idx+1:])
		table = table[:idx]
	}

	if !strings.HasPrefix(table, "/") {
		table = "/" + table
	}

	pattern = strings.TrimPrefix(table, "/")
	return "", pattern, formatHint, encoding
}

func parseTablePattern(table string) (bucket, pattern string, formatHint FileFormat, encoding string) {
	formatHint = FormatUnknown

	if idx := strings.Index(table, "#"); idx != -1 {
		formatHint, encoding = parseTableHints(table[idx+1:])
		table = table[:idx]
	}

	parts := strings.SplitN(table, "/", 2)
	if len(parts) == 1 {
		return parts[0], "*", formatHint, encoding
	}
	return parts[0], parts[1], formatHint, encoding
}

func extractPrefix(pattern string) string {
	idx := strings.IndexAny(pattern, "*?[")
	if idx == -1 {
		return pattern
	}
	lastSlash := strings.LastIndex(pattern[:idx], "/")
	if lastSlash == -1 {
		return ""
	}
	return pattern[:lastSlash+1]
}

func azureDatalakeListDirectory(pattern string) string {
	prefix := extractPrefix(pattern)
	if prefix == "" {
		return ""
	}

	if !strings.ContainsAny(pattern, "*?[") {
		pattern = strings.Trim(pattern, "/")
		if idx := strings.LastIndex(pattern, "/"); idx != -1 {
			return pattern[:idx]
		}
		return ""
	}

	return strings.Trim(prefix, "/")
}

func matchesGlobPattern(key, pattern string) bool {
	matched, _ := doublestar.Match(pattern, key)
	return matched
}

func detectFileFormat(key string, hint FileFormat) FileFormat {
	if hint != FormatUnknown {
		return hint
	}

	lower := strings.ToLower(key)

	lower = strings.TrimSuffix(lower, ".gz")

	switch {
	case strings.HasSuffix(lower, ".csv"):
		return FormatCSV
	case strings.HasSuffix(lower, ".jsonl") || strings.HasSuffix(lower, ".ndjson"):
		return FormatJSONL
	case strings.HasSuffix(lower, ".parquet"):
		return FormatParquet
	default:
		return FormatUnknown
	}
}

func isGzipped(key string) bool {
	return strings.HasSuffix(strings.ToLower(key), ".gz")
}

func retry[T any](attempts int, delay time.Duration, fn func() (T, error)) (T, error) {
	var result T
	var err error
	for i := 0; i < attempts; i++ {
		result, err = fn()
		if err == nil {
			return result, nil
		}
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}
	return result, err
}

var _ source.Source = (*BlobstoreSource)(nil)
