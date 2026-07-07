package onelake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/datalakeerror"
	datalakefile "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/file"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/adlsutil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/google/uuid"
)

type writeMode int

const (
	modeTables writeMode = iota
	modeFiles
)

const (
	defaultLayout          = "{load_id}.{file_id}.{ext}"
	maxDeltaCommitAttempts = 5
)

var errDeltaCommitConflict = errors.New("delta commit version already exists")

// OneLakeDestination writes data to a Microsoft Fabric OneLake lakehouse. It can
// target the lakehouse "Tables" area (written as a Delta table so it is queryable
// in Fabric) or the "Files" area (raw Parquet files).
type OneLakeDestination struct {
	workspace string
	lakehouse string
	client    *adlsutil.DataLakeClient
	cred      azcore.TokenCredential
	sasToken  string
	layout    string

	mu          sync.Mutex
	schema      *schema.TableSchema
	arrowSchema *arrow.Schema
	mode        writeMode
	relPath     string
	dropFirst   bool
}

func NewOneLakeDestination() *OneLakeDestination {
	return &OneLakeDestination{}
}

func (d *OneLakeDestination) Schemes() []string {
	return []string{"onelake"}
}

func (d *OneLakeDestination) GetScheme() string {
	return "onelake"
}

type parsedOneLakeURI struct {
	workspace         string
	lakehouse         string
	sasToken          string
	clientCredentials adlsutil.ClientCredentials
	layout            string
}

func parseOneLakeURI(uri string) (*parsedOneLakeURI, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "onelake" {
		return nil, fmt.Errorf("unsupported scheme for OneLake: %s", u.Scheme)
	}

	workspace := u.Host
	lakehouse := strings.Trim(u.Path, "/")
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required: onelake://<workspace>/<lakehouse>")
	}
	if lakehouse == "" {
		return nil, fmt.Errorf("lakehouse is required: onelake://<workspace>/<lakehouse>")
	}
	if strings.Contains(lakehouse, "/") {
		return nil, fmt.Errorf("lakehouse must be a single path segment, got %q", lakehouse)
	}

	q := u.Query()
	return &parsedOneLakeURI{
		workspace:         workspace,
		lakehouse:         lakehouse,
		sasToken:          q.Get("sas_token"),
		clientCredentials: adlsutil.ParseClientCredentials(q),
		layout:            q.Get("layout"),
	}, nil
}

func (d *OneLakeDestination) Connect(ctx context.Context, uri string) error {
	parsed, err := parseOneLakeURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse OneLake URI: %w", err)
	}

	d.workspace = parsed.workspace
	d.lakehouse = parsed.lakehouse
	d.cred = nil
	d.sasToken = parsed.sasToken
	d.layout = parsed.layout
	if d.layout == "" {
		d.layout = defaultLayout
	}

	if parsed.sasToken != "" {
		d.client = adlsutil.NewDataLakeClientWithSAS(adlsutil.OneLakeAccountName, adlsutil.OneLakeDNSSuffix, parsed.sasToken)
	} else {
		cred, err := parsed.clientCredentials.NewTokenCredential()
		if err != nil {
			return err
		}
		d.cred = cred
		d.client = adlsutil.NewDataLakeClientWithToken(adlsutil.OneLakeAccountName, adlsutil.OneLakeDNSSuffix, cred)
	}

	config.Debug("[ONELAKE] Connected to workspace=%s lakehouse=%s", d.workspace, d.lakehouse)
	return nil
}

func (d *OneLakeDestination) Close(ctx context.Context) error {
	return nil
}

// itemPath returns the OneLake item segment, e.g. "mylakehouse.Lakehouse".
func (d *OneLakeDestination) itemPath() string {
	if strings.Contains(d.lakehouse, ".") {
		return d.lakehouse
	}
	return d.lakehouse + ".Lakehouse"
}

// tableDir returns the path of a Delta table directory within the filesystem.
func (d *OneLakeDestination) tableDir() string {
	return d.itemPath() + "/Tables/" + strings.Trim(d.relPath, "/")
}

// filesDir returns the path of the Files-area directory within the filesystem.
func (d *OneLakeDestination) filesDir() string {
	return d.itemPath() + "/Files/" + strings.Trim(d.relPath, "/")
}

// parseTarget splits a dest-table into a write mode and relative path. A "Files/"
// prefix selects Files mode and takes the remainder verbatim (periods intact, so
// file extensions survive). Everything else is a Delta table, where "." and "/"
// are interchangeable separators and a leading "Tables" segment is optional — so
// "schema.name", "Tables.schema.name" and "Tables/schema/name" all map to
// "schema/name". Periods are unambiguous here because Fabric table and schema
// names cannot contain them.
func parseTarget(table string) (writeMode, string) {
	t := strings.Trim(table, "/")
	if strings.HasPrefix(strings.ToLower(t), "files/") {
		return modeFiles, t[len("files/"):]
	}

	parts := strings.FieldsFunc(t, func(r rune) bool { return r == '.' || r == '/' })
	if len(parts) > 0 && strings.EqualFold(parts[0], "tables") {
		parts = parts[1:]
	}
	return modeTables, strings.Join(parts, "/")
}

func (d *OneLakeDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.schema = opts.Schema
	if opts.Schema != nil {
		d.arrowSchema = opts.Schema.ToArrowSchema()
	}
	d.dropFirst = opts.DropFirst
	d.mode, d.relPath = parseTarget(opts.Table)

	if d.relPath == "" {
		return fmt.Errorf("dest-table is required for OneLake")
	}

	if d.mode == modeTables && opts.PartitionBy != "" {
		config.Debug("[ONELAKE] partition_by is not supported for Delta tables yet; ignoring %q", opts.PartitionBy)
	}

	if d.dropFirst {
		var dir string
		if d.mode == modeTables {
			dir = d.tableDir()
		} else {
			dir = d.filesDir()
		}
		if err := d.client.DeleteDir(ctx, d.workspace, dir); err != nil {
			return fmt.Errorf("failed to clear target %s: %w", dir, err)
		}
		config.Debug("[ONELAKE] Cleared target directory %s", dir)
	}

	return nil
}

func (d *OneLakeDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *OneLakeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var buffer bytes.Buffer
	var writer *pqarrow.FileWriter

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
			arrowProps := pqarrow.NewArrowWriterProperties(pqarrow.WithStoreSchema())

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
			return fmt.Errorf("failed to write batch: %w", err)
		}

		totalRows += recordToWrite.NumRows()
		if shouldRelease {
			recordToWrite.Release()
		}
		record.Release()
	}

	if writer == nil || totalRows == 0 {
		config.Debug("[ONELAKE] No rows to write")
		return nil
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close parquet writer: %w", err)
	}

	if d.mode == modeFiles {
		if err := d.writeFilesMode(ctx, buffer.Bytes()); err != nil {
			return err
		}
	} else {
		if err := d.writeTablesMode(ctx, buffer.Bytes()); err != nil {
			return err
		}
	}

	config.Debug("[ONELAKE] Wrote %d rows in %v", totalRows, time.Since(startTime))
	return nil
}

func (d *OneLakeDestination) writeFilesMode(ctx context.Context, data []byte) error {
	fileName := d.renderLayout(uuid.New().String()[:8], 0)
	fullPath := d.filesDir() + "/" + fileName
	if err := d.uploadBuffer(ctx, fullPath, data); err != nil {
		return fmt.Errorf("failed to upload %s: %w", fullPath, err)
	}
	config.Debug("[ONELAKE] Wrote %d bytes to %s", len(data), fullPath)
	return nil
}

func (d *OneLakeDestination) writeTablesMode(ctx context.Context, data []byte) error {
	tableDir := d.tableDir()
	dataFile := fmt.Sprintf("part-00000-%s.c000.snappy.parquet", uuid.New().String())

	if err := d.uploadBuffer(ctx, tableDir+"/"+dataFile, data); err != nil {
		return fmt.Errorf("failed to upload data file: %w", err)
	}

	cols, err := d.columns()
	if err != nil {
		return err
	}

	logDir := tableDir + "/_delta_log"
	adds := []deltaAddFile{{Path: dataFile, Size: int64(len(data))}}
	nowMillis := time.Now().UnixMilli()

	for attempt := 0; attempt < maxDeltaCommitAttempts; attempt++ {
		version, err := d.nextDeltaVersion(ctx, logDir)
		if err != nil {
			return err
		}

		var commit []byte
		if version == 0 {
			commit, err = buildInitialCommit(cols, adds, uuid.New().String(), nowMillis)
		} else {
			commit, err = buildAppendCommit(adds, nowMillis)
		}
		if err != nil {
			return err
		}

		if err := d.uploadDeltaCommit(ctx, logDir, version, commit); err != nil {
			if errors.Is(err, errDeltaCommitConflict) {
				config.Debug("[ONELAKE] Delta commit version %d already exists; retrying append commit", version)
				continue
			}
			return err
		}
		config.Debug("[ONELAKE] Committed delta version %d to %s", version, tableDir)
		return nil
	}

	return fmt.Errorf("failed to commit delta table after %d attempts: %w", maxDeltaCommitAttempts, errDeltaCommitConflict)
}

func (d *OneLakeDestination) uploadDeltaCommit(ctx context.Context, logDir string, version int64, commit []byte) error {
	commitPath := logDir + "/" + commitFileName(version)
	if err := d.ensureDirectories(ctx, logDir); err != nil {
		return fmt.Errorf("failed to prepare delta log directory: %w", err)
	}

	tempPath := deltaCommitTempPath(logDir)
	if err := d.uploadBuffer(ctx, tempPath, commit); err != nil {
		return fmt.Errorf("failed to upload temporary delta commit %s: %w", tempPath, err)
	}

	tempFileClient, err := d.newOneLakeFileClient(tempPath)
	if err != nil {
		return err
	}

	if _, err := tempFileClient.Rename(ctx, commitPath, deltaCommitRenameOptions()); err != nil {
		if _, deleteErr := tempFileClient.Delete(ctx, nil); deleteErr != nil {
			config.Debug("[ONELAKE] Failed to delete temporary delta commit %s: %v", tempPath, deleteErr)
		}
		if isDeltaCommitConflict(err) {
			return errDeltaCommitConflict
		}
		return fmt.Errorf("failed to publish delta commit %s: %w", commitPath, err)
	}
	return nil
}

func (d *OneLakeDestination) newOneLakeFileClient(path string) (*datalakefile.Client, error) {
	pathURL, err := adlsutil.PathURLWithSuffix(adlsutil.OneLakeAccountName, adlsutil.OneLakeDNSSuffix, d.workspace, path)
	if err != nil {
		return nil, err
	}
	if d.sasToken != "" {
		return datalakefile.NewClientWithNoCredential(adlsutil.AppendSASToken(pathURL, d.sasToken), nil)
	}
	if d.cred == nil {
		return nil, fmt.Errorf("OneLake destination is not connected")
	}
	return datalakefile.NewClient(pathURL, d.cred, nil)
}

func (d *OneLakeDestination) uploadBuffer(ctx context.Context, path string, data []byte) error {
	return d.client.UploadBufferSkippingPrefix(ctx, d.workspace, path, data, adlsutil.OneLakeManagedPrefixSegments)
}

func (d *OneLakeDestination) ensureDirectories(ctx context.Context, path string) error {
	return d.client.EnsureDirectoriesSkippingPrefix(ctx, d.workspace, path, adlsutil.OneLakeManagedPrefixSegments)
}

func deltaCommitRenameOptions() *datalakefile.RenameOptions {
	etagAny := azcore.ETagAny
	return &datalakefile.RenameOptions{
		AccessConditions: &datalakefile.AccessConditions{
			ModifiedAccessConditions: &datalakefile.ModifiedAccessConditions{
				IfNoneMatch: &etagAny,
			},
		},
	}
}

func deltaCommitTempPath(logDir string) string {
	tableDir := strings.TrimSuffix(logDir, "/_delta_log")
	return tableDir + "/_bruin_delta_tmp/" + uuid.New().String() + ".tmp"
}

func isDeltaCommitConflict(err error) bool {
	return datalakeerror.HasCode(
		err,
		datalakeerror.ConditionNotMet,
		datalakeerror.PathAlreadyExists,
		datalakeerror.ResourceAlreadyExists,
	)
}

func (d *OneLakeDestination) nextDeltaVersion(ctx context.Context, logDir string) (int64, error) {
	version := int64(0)
	if !d.dropFirst {
		versions, err := d.client.ListLogVersions(ctx, d.workspace, logDir)
		if err != nil {
			return 0, fmt.Errorf("failed to inspect delta log: %w", err)
		}
		if len(versions) > 0 {
			version = versions[len(versions)-1] + 1
		}
	}
	return version, nil
}

// columns returns the schema columns, deriving them from the Arrow schema when an
// explicit table schema was not provided (e.g. schema-less sources).
func (d *OneLakeDestination) columns() ([]schema.Column, error) {
	if d.schema != nil && len(d.schema.Columns) > 0 {
		return d.schema.Columns, nil
	}
	if d.arrowSchema == nil {
		return nil, fmt.Errorf("no schema available to build delta table")
	}
	cols := make([]schema.Column, d.arrowSchema.NumFields())
	for i := 0; i < d.arrowSchema.NumFields(); i++ {
		f := d.arrowSchema.Field(i)
		cols[i] = arrowFieldToColumn(f)
	}
	return cols, nil
}

func (d *OneLakeDestination) renderLayout(loadID string, fileID int) string {
	tableName := strings.Trim(d.relPath, "/")
	if idx := strings.LastIndex(tableName, "/"); idx != -1 {
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

func (d *OneLakeDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

// Strategy support: OneLake is a direct-write, file-based destination.
func (d *OneLakeDestination) SupportsReplaceStrategy() bool      { return true }
func (d *OneLakeDestination) SupportsAppendStrategy() bool       { return true }
func (d *OneLakeDestination) SupportsMergeStrategy() bool        { return true }
func (d *OneLakeDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *OneLakeDestination) SupportsSCD2Strategy() bool         { return true }
func (d *OneLakeDestination) SupportsAtomicSwap() bool           { return false }

func (d *OneLakeDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	return nil
}

// tableDirForTables resolves a dest-table string to a Delta table directory,
// rejecting Files-mode targets (the copy-on-write strategies only apply to
// Delta tables).
func (d *OneLakeDestination) tableDirForTables(table, op string) (string, error) {
	mode, rel := parseTarget(table)
	if mode != modeTables {
		return "", fmt.Errorf("%s strategy requires a Tables/ destination, got %q", op, table)
	}
	if rel == "" {
		return "", fmt.Errorf("invalid table for %s: %q", op, table)
	}
	return d.itemPath() + "/Tables/" + strings.Trim(rel, "/"), nil
}

// readTable resolves a dest-table string to its Delta directory, returns the log
// snapshot and (if it exists) its data as Arrow batches owned by the caller.
func (d *OneLakeDestination) readTable(ctx context.Context, table, op string) (string, *deltaSnapshot, []arrow.RecordBatch, error) {
	dir, err := d.tableDirForTables(table, op)
	if err != nil {
		return "", nil, nil, err
	}
	snap, err := readDeltaSnapshot(ctx, d.client, d.workspace, dir)
	if err != nil {
		return "", nil, nil, err
	}
	if !snap.exists {
		return dir, snap, nil, nil
	}
	batches, err := readDeltaData(ctx, d.client, d.workspace, dir, snap.activeFiles)
	if err != nil {
		return "", nil, nil, err
	}
	return dir, snap, batches, nil
}

func (d *OneLakeDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return d.rewriteTableWithRetry(ctx, opts.TargetTable, opts.StagingTable, "merge", "MERGE", func(target, staging []arrow.RecordBatch) ([]arrow.RecordBatch, error) {
		return mergeBatches(ctx, target, staging, opts.PrimaryKeys)
	})
}

func (d *OneLakeDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return d.rewriteTableWithRetry(ctx, opts.TargetTable, opts.StagingTable, "delete+insert", "DELETE+INSERT", func(target, staging []arrow.RecordBatch) ([]arrow.RecordBatch, error) {
		return deleteInsertBatches(ctx, target, staging, opts)
	})
}

func (d *OneLakeDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return d.rewriteTableWithRetry(ctx, opts.TargetTable, opts.StagingTable, "scd2", "SCD2", func(target, staging []arrow.RecordBatch) ([]arrow.RecordBatch, error) {
		return scd2Batches(ctx, target, staging, opts)
	})
}

func (d *OneLakeDestination) rewriteTableWithRetry(ctx context.Context, targetTable, stagingTable, readOp, commitOp string, transform func(target, staging []arrow.RecordBatch) ([]arrow.RecordBatch, error)) error {
	_, _, sBatches, err := d.readTable(ctx, stagingTable, readOp)
	if err != nil {
		return err
	}
	defer releaseBatches(sBatches)

	var lastConflict error
	for attempt := 0; attempt < maxDeltaCommitAttempts; attempt++ {
		tDir, tSnap, tBatches, err := d.readTable(ctx, targetTable, readOp)
		if err != nil {
			return err
		}

		result, err := transform(tBatches, sBatches)
		releaseBatches(tBatches)
		if err != nil {
			return err
		}

		// Delta data files are immutable, so each attempt writes a fresh file
		// rather than overwriting the previous attempt's object. Files left by
		// conflicting attempts are unreferenced and reclaimed by VACUUM.
		dataFile := fmt.Sprintf("part-00000-%s.c000.snappy.parquet", uuid.New().String())
		rewrite, err := d.uploadRewriteData(ctx, tDir, dataFile, result)
		releaseBatches(result)
		if err != nil {
			return err
		}

		err = d.commitRewrite(ctx, tDir, tSnap, rewrite, commitOp)
		if err == nil {
			return nil
		}
		if errors.Is(err, errDeltaCommitConflict) {
			lastConflict = err
			config.Debug("[ONELAKE] Delta commit conflict during %s; retrying from latest snapshot", readOp)
			continue
		}
		return err
	}

	return fmt.Errorf("failed to commit %s after %d attempts: %w", readOp, maxDeltaCommitAttempts, lastConflict)
}

type rewriteData struct {
	adds []deltaAddFile
	cols []schema.Column
}

func (d *OneLakeDestination) uploadRewriteData(ctx context.Context, tableDir, dataFile string, batches []arrow.RecordBatch) (*rewriteData, error) {
	if len(batches) == 0 {
		return &rewriteData{}, nil
	}

	data, arrowSchema, err := writeBatchesToParquet(batches)
	if err != nil {
		return nil, err
	}
	if err := d.uploadBuffer(ctx, tableDir+"/"+dataFile, data); err != nil {
		return nil, fmt.Errorf("failed to upload data file: %w", err)
	}

	return &rewriteData{
		adds: []deltaAddFile{{Path: dataFile, Size: int64(len(data))}},
		cols: arrowSchemaToColumns(arrowSchema),
	}, nil
}

// commitRewrite commits a new Delta version that removes the prior data files
// and adds the prepared rewrite data file (copy-on-write). If the table did not
// exist and the rewrite has no rows, it writes nothing.
func (d *OneLakeDestination) commitRewrite(ctx context.Context, tableDir string, snap *deltaSnapshot, rewrite *rewriteData, operation string) error {
	if rewrite == nil {
		rewrite = &rewriteData{}
	}
	if len(rewrite.adds) == 0 && (snap == nil || !snap.exists) {
		config.Debug("[ONELAKE] %s produced no rows and table does not exist; nothing to do", operation)
		return nil
	}

	logDir := tableDir + "/_delta_log"
	nowMillis := time.Now().UnixMilli()
	var removePaths []string
	if snap != nil && snap.exists {
		removePaths = snap.activeFiles
	}

	version := int64(0)
	if snap != nil && snap.exists {
		version = snap.version + 1
	}

	var commit []byte
	var err error
	if version == 0 {
		commit, err = buildInitialCommit(rewrite.cols, rewrite.adds, uuid.New().String(), nowMillis)
	} else {
		commit, err = buildRewriteCommit(removePaths, rewrite.adds, operation, nowMillis)
	}
	if err != nil {
		return err
	}

	if err := d.uploadDeltaCommit(ctx, logDir, version, commit); err != nil {
		return err
	}
	config.Debug("[ONELAKE] %s committed delta version %d to %s", operation, version, tableDir)
	return nil
}

func arrowSchemaToColumns(s *arrow.Schema) []schema.Column {
	cols := make([]schema.Column, s.NumFields())
	for i := 0; i < s.NumFields(); i++ {
		cols[i] = arrowFieldToColumn(s.Field(i))
	}
	return cols
}

// writeBatchesToParquet encodes a set of record batches into a single Parquet
// file, normalizing each batch to the first batch's schema.
func writeBatchesToParquet(batches []arrow.RecordBatch) ([]byte, *arrow.Schema, error) {
	target := stripSchemaMetadata(batches[0].Schema())

	var buffer bytes.Buffer
	writerProps := parquet.NewWriterProperties(
		parquet.WithCompression(compress.Codecs.Snappy),
		parquet.WithDictionaryDefault(true),
		parquet.WithDataPageSize(1024*1024),
	)
	arrowProps := pqarrow.NewArrowWriterProperties(pqarrow.WithStoreSchema())
	writer, err := pqarrow.NewFileWriter(target, &buffer, writerProps, arrowProps)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create parquet writer: %w", err)
	}

	for _, b := range batches {
		rec := b
		release := false
		if !b.Schema().Equal(target) {
			if !schemaEqualIgnoringMetadata(b.Schema(), target) {
				_ = writer.Close()
				return nil, nil, fmt.Errorf("incompatible batch schema during rewrite")
			}
			normalized, err := normalizeRecordToSchema(b, target)
			if err != nil {
				_ = writer.Close()
				return nil, nil, err
			}
			rec = normalized
			release = true
		}
		if err := writer.WriteBuffered(rec); err != nil {
			if release {
				rec.Release()
			}
			_ = writer.Close()
			return nil, nil, fmt.Errorf("failed to write batch: %w", err)
		}
		if release {
			rec.Release()
		}
	}

	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close parquet writer: %w", err)
	}
	return buffer.Bytes(), target, nil
}

func (d *OneLakeDestination) DropTable(ctx context.Context, table string) error {
	mode, relPath := parseTarget(table)
	area := "Tables"
	if mode == modeFiles {
		area = "Files"
	}
	dir := d.itemPath() + "/" + area + "/" + strings.Trim(relPath, "/")
	return d.client.DeleteDir(ctx, d.workspace, dir)
}

func (d *OneLakeDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (d *OneLakeDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return &onelakeTransaction{}, nil
}

type onelakeTransaction struct{}

func (t *onelakeTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}
func (t *onelakeTransaction) Commit(ctx context.Context) error   { return nil }
func (t *onelakeTransaction) Rollback(ctx context.Context) error { return nil }

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
	return arrow.NewSchema(af, nil).Equal(arrow.NewSchema(bf, nil))
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

func arrowFieldToColumn(f arrow.Field) schema.Column {
	col := schema.Column{Name: f.Name, Nullable: f.Nullable}
	switch dt := f.Type.(type) {
	case *arrow.BooleanType:
		col.DataType = schema.TypeBoolean
	case *arrow.Int16Type:
		col.DataType = schema.TypeInt16
	case *arrow.Int32Type:
		col.DataType = schema.TypeInt32
	case *arrow.Int64Type:
		col.DataType = schema.TypeInt64
	case *arrow.Float32Type:
		col.DataType = schema.TypeFloat32
	case *arrow.Float64Type:
		col.DataType = schema.TypeFloat64
	case *arrow.Decimal128Type:
		col.DataType = schema.TypeDecimal
		col.Precision = int(dt.Precision)
		col.Scale = int(dt.Scale)
	case *arrow.BinaryType, *arrow.LargeBinaryType:
		col.DataType = schema.TypeBinary
	case *arrow.Date32Type, *arrow.Date64Type:
		col.DataType = schema.TypeDate
	case *arrow.Time32Type, *arrow.Time64Type:
		col.DataType = schema.TypeTime
	case *arrow.TimestampType:
		if dt.TimeZone != "" {
			col.DataType = schema.TypeTimestampTZ
		} else {
			col.DataType = schema.TypeTimestamp
		}
	case *arrow.ListType:
		col.DataType = schema.TypeArray
		col.ArrayType = arrowFieldToColumn(arrow.Field{Type: dt.Elem()}).DataType
	default:
		col.DataType = schema.TypeString
	}
	return col
}
