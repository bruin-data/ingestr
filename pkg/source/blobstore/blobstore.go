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
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/filesystem"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bmatcuk/doublestar/v4"
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

	azureDatalakeDNSSuffix = ".dfs.core.windows.net"
	defaultParallelism     = 5
)

type FileFormat string

const (
	FormatCSV     FileFormat = "csv"
	FormatJSONL   FileFormat = "jsonl"
	FormatParquet FileFormat = "parquet"
	FormatUnknown FileFormat = "unknown"
)

type BlobstoreSource struct {
	provider   Provider
	s3Client   *s3.Client
	gcsClient  *storage.Client
	adlsClient *azureDatalakeSourceClient
	sftpClient *sftp.Client
	sshClient  *ssh.Client
	parsedURI  *parsedBlobstoreURI
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
			return filesystem.NewClientWithNoCredential(appendSASToken(fileSystemURL, sasToken), nil)
		}
		return client, nil
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create default Azure credential: %w", err)
	}
	client.newFilesystemClient = func(fileSystemURL string) (*filesystem.Client, error) {
		return filesystem.NewClient(fileSystemURL, cred, nil)
	}
	return client, nil
}

func appendSASToken(rawURL, sasToken string) string {
	if sasToken == "" {
		return rawURL
	}
	if strings.Contains(rawURL, "?") {
		return rawURL + "&" + sasToken
	}
	return rawURL + "?" + sasToken
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
	fileChan := make(chan string, parallelism*2)

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
		count, err := s.listMatchingFiles(ctx, bucket, pattern, fileChan)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to list files: %w", err)}
		} else if count == 0 {
			results <- source.RecordBatchResult{Err: fmt.Errorf("no files found matching pattern: %s/%s", bucket, pattern)}
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

func (s *BlobstoreSource) processFile(ctx context.Context, bucket, fileKey string, formatHint FileFormat, tableEncoding string, batchSize int, opts source.ReadOptions, results chan<- source.RecordBatchResult) {
	startFile := time.Now()
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

	switch format {
	case FormatParquet:
		err = s.readParquetFile(ctx, data, results, &totalRows, &batchNum, opts)
	case FormatJSONL:
		err = s.readJSONLFile(ctx, dataReader, results, &totalRows, &batchNum, batchSize, opts)
	case FormatCSV:
		err = s.readCSVFile(ctx, dataReader, tableEncoding, results, &totalRows, &batchNum, batchSize, opts)
	}

	if err != nil {
		results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read %s: %w", fileKey, err)}
	}

	config.Debug("[BLOBSTORE-SRC] File %s: %d rows in %d batches, read time: %v", fileKey, totalRows, batchNum, time.Since(startFile))
}

func (s *BlobstoreSource) listMatchingFiles(ctx context.Context, bucket, pattern string, fileChan chan<- string) (int, error) {
	count := 0
	prefix := extractPrefix(pattern)

	switch s.provider {
	case ProviderS3:
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
				if matchesGlobPattern(key, pattern) {
					select {
					case fileChan <- key:
						count++
					case <-ctx.Done():
						return count, ctx.Err()
					}
				}
			}
		}

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

			if matchesGlobPattern(attrs.Name, pattern) {
				select {
				case fileChan <- attrs.Name:
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
		if prefix != "" {
			opts.Prefix = &prefix
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
					case fileChan <- key:
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
				case fileChan <- walker.Path():
					count++
				case <-ctx.Done():
					return count, ctx.Err()
				}
			}
		}
	}

	return count, nil
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

func (s *BlobstoreSource) readParquetFile(ctx context.Context, data []byte, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, opts source.ReadOptions) error {
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

		results <- source.RecordBatchResult{Batch: rec}

		if opts.Limit > 0 && *totalRows >= int64(opts.Limit) {
			break
		}
	}

	return tr.Err()
}

func (s *BlobstoreSource) readJSONLFile(ctx context.Context, reader io.Reader, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions) error {
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

			results <- source.RecordBatchResult{Batch: record}
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

		results <- source.RecordBatchResult{Batch: record}
	}

	return nil
}

func (s *BlobstoreSource) readCSVFile(ctx context.Context, reader io.Reader, tableEncoding string, results chan<- source.RecordBatchResult, totalRows *int64, batchNum *int, batchSize int, opts source.ReadOptions) error {
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

			results <- source.RecordBatchResult{Batch: rec}
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

		results <- source.RecordBatchResult{Batch: rec}
	}

	return nil
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
	provider          Provider
	accessKeyID       string
	secretAccessKey   string
	region            string
	endpointURL       string
	credentialsFile   string
	accountName       string
	accountKey        string
	sasToken          string
	sftpHost          string
	sftpPort          string
	sftpUsername      string
	sftpPassword      string
	sftpKeyFile       string
	sftpKeyPassphrase string
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
		parsed.accountName = parseAzureDatalakeAccountName(u)
		parsed.accountKey = u.Query().Get("account_key")
		parsed.sasToken = u.Query().Get("sas_token")
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

func parseAzureDatalakeAccountName(u *url.URL) string {
	if accountName := u.Query().Get("account_name"); accountName != "" {
		return accountName
	}

	host := u.Hostname()
	if strings.HasSuffix(host, azureDatalakeDNSSuffix) {
		return strings.TrimSuffix(host, azureDatalakeDNSSuffix)
	}
	if host != "" && !strings.Contains(host, ".") {
		return host
	}

	return ""
}

func buildAzureDatalakeFilesystemURL(accountName, fileSystem string) string {
	u := &url.URL{
		Scheme: "https",
		Host:   accountName + azureDatalakeDNSSuffix,
		Path:   "/" + strings.Trim(fileSystem, "/"),
	}
	return u.String()
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
