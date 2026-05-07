package bigquery

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/bigquery"
	gcsstorage "cloud.google.com/go/storage"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/destination"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const (
	defaultWriteParallelism   = 4
	exactRowCountWaitTimeout  = 30 * time.Second
	exactRowCountPollInterval = 1 * time.Second
)

type bigQueryLoadMethod string

const (
	loadMethodLoadJob      bigQueryLoadMethod = "load_job"
	loadMethodStorageWrite bigQueryLoadMethod = "storage_write"
)

type storageArrowAppender interface {
	AppendArrowStreamFromSource(ctx context.Context, tablePath string, records <-chan source.RecordBatchResult, parallelism int) error
	AppendArrowPendingStreamsFromSource(ctx context.Context, tablePath string, records <-chan source.RecordBatchResult, parallelism int) error
	Close() error
}

// BigQueryDestination implements the Destination interface for BigQuery.
type BigQueryDestination struct {
	client             *bigquery.Client
	storageArrowClient storageArrowAppender
	gcsClient          *gcsstorage.Client
	projectID          string
	datasetID          string // Default dataset from URI
	location           string
	credPath           string
	credJSON           string
	loadMethod         bigQueryLoadMethod

	// Table metadata for current operation
	partitionBy string
	clusterBy   []string

	// Cache for dataset existence checks
	knownDatasets map[string]bool

	// Async table creation: overlaps staging table creation with source read.
	// State is tracked per table so concurrent multi-table writes don't race.
	pendingTableMu   sync.Mutex
	pendingTableErrs map[string]chan error
	gcsClientMu      sync.Mutex

	loadJobWriter func(ctx context.Context, dataset, table string, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error
}

// NewBigQueryDestination creates a new BigQuery destination.
func NewBigQueryDestination() *BigQueryDestination {
	return &BigQueryDestination{}
}

// Schemes returns the URI schemes supported by this destination.
func (d *BigQueryDestination) Schemes() []string {
	return []string{"bigquery"}
}

type connectConfig struct {
	projectID  string
	datasetID  string
	location   string
	credPath   string
	credJSON   string
	loadMethod bigQueryLoadMethod
}

func parseBigQueryURI(uri string) (*connectConfig, error) {
	// Parse URI: bigquery://project-id/dataset?credentials_path=/path/to/sa.json&location=us-central1
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid BigQuery URI: %w", err)
	}

	cfg := &connectConfig{}

	cfg.projectID = u.Host
	if cfg.projectID == "" {
		return nil, errors.New("BigQuery URI must include project_id as host (e.g., bigquery://my-project)")
	}

	// Extract dataset from path if provided
	if u.Path != "" && u.Path != "/" {
		// Remove leading slash
		dataset := u.Path
		if dataset[0] == '/' {
			dataset = dataset[1:]
		}
		cfg.datasetID = dataset
	}

	query := u.Query()

	// Handle credentials
	if credPath := query.Get("credentials_path"); credPath != "" {
		cfg.credPath = credPath
	} else if credBase64 := query.Get("credentials_base64"); credBase64 != "" {
		credContent, err := base64.StdEncoding.DecodeString(credBase64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 credentials: %w", err)
		}
		cfg.credJSON = string(credContent)
	}

	// Handle location parameter
	if location := query.Get("location"); location != "" {
		cfg.location = location
	}

	cfg.loadMethod = loadMethodLoadJob
	if loadMethod := query.Get("load_method"); loadMethod != "" {
		switch bigQueryLoadMethod(loadMethod) {
		case loadMethodLoadJob, loadMethodStorageWrite:
			cfg.loadMethod = bigQueryLoadMethod(loadMethod)
		default:
			return nil, fmt.Errorf("unsupported load_method %q", loadMethod)
		}
	}

	return cfg, nil
}

// Connect initializes the connection to BigQuery.
func (d *BigQueryDestination) Connect(ctx context.Context, uri string) error {
	cfg, err := parseBigQueryURI(uri)
	if err != nil {
		return err
	}

	d.projectID = cfg.projectID
	d.datasetID = cfg.datasetID
	d.location = cfg.location
	d.credPath = cfg.credPath
	d.credJSON = cfg.credJSON
	d.loadMethod = cfg.loadMethod

	// Create BigQuery client
	var clientOpts []option.ClientOption

	if d.credPath != "" {
		clientOpts = append(clientOpts, option.WithAuthCredentialsFile(option.ServiceAccount, d.credPath))
	} else if d.credJSON != "" {
		clientOpts = append(clientOpts, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(d.credJSON)))
	}

	// Create BigQuery client first; auxiliary clients depend on load method.
	var client *bigquery.Client
	client, clientErr := bigquery.NewClient(ctx, d.projectID, clientOpts...)
	if clientErr != nil {
		return fmt.Errorf("failed to create BigQuery client: %w", clientErr)
	}
	if d.location != "" {
		client.Location = d.location
	}

	var storageArrowClient storageArrowAppender
	if d.effectiveLoadMethod() == loadMethodStorageWrite {
		var storageErr error
		storageArrowClient, storageErr = NewStorageWriteArrowClient(ctx, d.projectID, clientOpts...)
		if storageErr != nil {
			_ = client.Close()
			return fmt.Errorf("failed to create Storage Write Arrow client: %w", storageErr)
		}
	}

	d.client = client
	d.storageArrowClient = storageArrowClient
	config.Debug("[DEST] Connected to BigQuery project: %s", d.projectID)
	if d.effectiveLoadMethod() == loadMethodStorageWrite {
		config.Debug("[DEST] Storage Write API (Arrow format) client initialized")
	} else {
		config.Debug("[DEST] BigQuery load jobs enabled")
	}

	return nil
}

// Close closes the BigQuery connection.
func (d *BigQueryDestination) Close(ctx context.Context) error {
	// Close storage Arrow client first
	if d.storageArrowClient != nil {
		if swc, ok := d.storageArrowClient.(*StorageWriteArrowClient); ok && swc == nil {
			d.storageArrowClient = nil
		} else {
			if err := d.storageArrowClient.Close(); err != nil {
				config.Debug("[DEST] Error closing Storage Write Arrow client: %v", err)
			}
		}
	}

	if d.gcsClient != nil {
		if err := d.gcsClient.Close(); err != nil {
			config.Debug("[DEST] Error closing GCS client: %v", err)
		}
	}

	// Close BigQuery client
	if d.client != nil {
		if err := d.client.Close(); err != nil {
			return fmt.Errorf("failed to close BigQuery client: %w", err)
		}
		config.Debug("[DEST] Closed BigQuery connection")
	}
	return nil
}

// ensureDatasetExists creates the dataset if it doesn't exist.
func (d *BigQueryDestination) ensureDatasetExists(ctx context.Context, datasetID string) error {
	if d.knownDatasets[datasetID] {
		return nil
	}

	ds := d.client.Dataset(datasetID)

	// Check if dataset exists
	_, err := ds.Metadata(ctx)
	if err == nil {
		if d.knownDatasets == nil {
			d.knownDatasets = make(map[string]bool)
		}
		d.knownDatasets[datasetID] = true
		return nil
	}

	if !isNotFoundError(err) {
		return fmt.Errorf("failed to check dataset existence: %w", err)
	}

	// Dataset doesn't exist, create it
	config.Debug("[DEST] Creating dataset: %s", datasetID)

	metadata := &bigquery.DatasetMetadata{}
	if d.location != "" {
		metadata.Location = d.location
	} else {
		metadata.Location = "US" // Default location
	}

	if err := ds.Create(ctx, metadata); err != nil {
		// Check if it was created by another process in the meantime
		if isAlreadyExistsError(err) {
			return nil
		}
		return fmt.Errorf("failed to create dataset: %w", err)
	}

	config.Debug("[DEST] Dataset created: %s", datasetID)
	if d.knownDatasets == nil {
		d.knownDatasets = make(map[string]bool)
	}
	d.knownDatasets[datasetID] = true
	return nil
}

func (d *BigQueryDestination) resolveTable(table string) (string, string, string, error) {
	dataset, tableName, err := ParseTableName(table)
	if err != nil {
		return "", "", "", err
	}

	if dataset == "" && d.datasetID != "" {
		dataset = d.datasetID
	}

	if dataset == "" {
		return "", "", "", errors.New("dataset must be specified in table name (dataset.table) or URI path")
	}

	return dataset, tableName, dataset + "." + tableName, nil
}

func (d *BigQueryDestination) setPendingTableErr(tableKey string, errCh chan error) {
	d.pendingTableMu.Lock()
	defer d.pendingTableMu.Unlock()

	if d.pendingTableErrs == nil {
		d.pendingTableErrs = make(map[string]chan error)
	}

	d.pendingTableErrs[tableKey] = errCh
}

func (d *BigQueryDestination) takePendingTableErr(tableKey string) chan error {
	d.pendingTableMu.Lock()
	defer d.pendingTableMu.Unlock()

	if d.pendingTableErrs == nil {
		return nil
	}

	errCh := d.pendingTableErrs[tableKey]
	if errCh == nil {
		return nil
	}

	delete(d.pendingTableErrs, tableKey)
	if len(d.pendingTableErrs) == 0 {
		d.pendingTableErrs = nil
	}

	return errCh
}

// PrepareTable creates or recreates a table with the given schema.
func (d *BigQueryDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	dataset, table, tableKey, err := d.resolveTable(opts.Table)
	if err != nil {
		return err
	}

	tableRef := d.client.Dataset(dataset).Table(table)

	// Store partition and cluster information for use in SwapTable
	d.partitionBy = opts.PartitionBy
	d.clusterBy = opts.ClusterBy

	if opts.DropFirst {
		// Run the entire DropFirst flow async to overlap with source reading.
		// Optimistic TRUNCATE: fire TRUNCATE directly without checking Metadata first.
		// If table doesn't exist, TRUNCATE fails and we fall back to CREATE.
		tableSchema := opts.Schema
		if opts.CDCMode {
			tableSchema = makeNonPKColumnsNullable(opts.Schema, opts.PrimaryKeys)
		}
		tableSchema = d.normalizeSchemaForLoadMethod(tableSchema)
		metadata := BuildTableMetadata(tableSchema, opts.PrimaryKeys, d.location, opts.PartitionBy, opts.ClusterBy, opts.ExpiresAfter)
		errCh := make(chan error, 1)
		d.setPendingTableErr(tableKey, errCh)
		go func() {
			truncateSQL := fmt.Sprintf("TRUNCATE TABLE `%s`.`%s`.`%s`", d.projectID, dataset, table)
			config.Debug("[DEST] Truncating table: %s", opts.Table)
			query := d.client.Query(truncateSQL)
			if d.location != "" {
				query.Location = d.location
			}
			job, err := query.Run(ctx)
			if err != nil {
				// TRUNCATE failed to submit — table likely doesn't exist
				errCh <- d.createTableFresh(ctx, tableRef, dataset, metadata)
				return
			}
			for {
				status, err := job.Status(ctx)
				if err != nil {
					errCh <- fmt.Errorf("truncate status check failed: %w", err)
					return
				}
				if status.Done() {
					if status.Err() != nil {
						if isNotFoundError(status.Err()) {
							errCh <- d.createTableFresh(ctx, tableRef, dataset, metadata)
							return
						}
						errCh <- fmt.Errorf("truncate error: %w", status.Err())
						return
					}
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			// TRUNCATE succeeded — table exists. Check schema evolution if needed.
			if opts.Schema != nil {
				existingMeta, err := tableRef.Metadata(ctx)
				if err == nil {
					tableSchema := opts.Schema
					if opts.CDCMode {
						tableSchema = makeNonPKColumnsNullable(opts.Schema, opts.PrimaryKeys)
					}
					tableSchema = d.normalizeSchemaForLoadMethod(tableSchema)
					if err := d.addMissingColumns(ctx, tableRef, existingMeta, tableSchema); err != nil {
						errCh <- fmt.Errorf("failed to update schema: %w", err)
						return
					}
					latestMeta, metaErr := tableRef.Metadata(ctx)
					if metaErr == nil {
						if err := d.ensureLoadJobColumnRelaxation(ctx, tableRef, latestMeta, tableSchema); err != nil {
							errCh <- fmt.Errorf("failed to relax schema for load jobs: %w", err)
							return
						}
						existingMeta = latestMeta
					}
					if err := d.ensureTableExpiration(ctx, tableRef, existingMeta, opts.ExpiresAfter); err != nil {
						errCh <- fmt.Errorf("failed to update table expiration: %w", err)
						return
					}
				}
			}
			errCh <- nil
		}()
		return nil
	}

	// Non-DropFirst: check if table exists, add missing columns or create
	existingMeta, err := tableRef.Metadata(ctx)
	if err == nil {
		if opts.Schema != nil {
			tableSchema := opts.Schema
			if opts.CDCMode {
				tableSchema = makeNonPKColumnsNullable(opts.Schema, opts.PrimaryKeys)
			}
			tableSchema = d.normalizeSchemaForLoadMethod(tableSchema)
			if err := d.addMissingColumns(ctx, tableRef, existingMeta, tableSchema); err != nil {
				return fmt.Errorf("failed to add missing columns: %w", err)
			}
			latestMeta, metaErr := tableRef.Metadata(ctx)
			if metaErr == nil {
				if err := d.ensureLoadJobColumnRelaxation(ctx, tableRef, latestMeta, tableSchema); err != nil {
					return fmt.Errorf("failed to relax schema for load jobs: %w", err)
				}
				existingMeta = latestMeta
			}
		}
		if err := d.ensureTableExpiration(ctx, tableRef, existingMeta, opts.ExpiresAfter); err != nil {
			return fmt.Errorf("failed to update table expiration: %w", err)
		}
		return nil
	}
	if !isNotFoundError(err) {
		return fmt.Errorf("failed to check table existence: %w", err)
	}

	// Table doesn't exist — ensure dataset exists and create
	if err := d.ensureDatasetExists(ctx, dataset); err != nil {
		return fmt.Errorf("failed to ensure dataset exists: %w", err)
	}

	tableSchema := opts.Schema
	if opts.CDCMode {
		tableSchema = makeNonPKColumnsNullable(opts.Schema, opts.PrimaryKeys)
	}
	tableSchema = d.normalizeSchemaForLoadMethod(tableSchema)
	metadata := BuildTableMetadata(tableSchema, opts.PrimaryKeys, d.location, opts.PartitionBy, opts.ClusterBy, opts.ExpiresAfter)

	config.Debug("[DEST] Creating table: %s", opts.Table)
	if err := tableRef.Create(ctx, metadata); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}

func (d *BigQueryDestination) createTableFresh(ctx context.Context, tableRef *bigquery.Table, dataset string, metadata *bigquery.TableMetadata) error {
	if err := d.ensureDatasetExists(ctx, dataset); err != nil {
		return fmt.Errorf("failed to ensure dataset exists: %w", err)
	}
	if err := tableRef.Create(ctx, metadata); err != nil {
		if isAlreadyExistsError(err) {
			if delErr := tableRef.Delete(ctx); delErr != nil && !isNotFoundError(delErr) {
				return fmt.Errorf("failed to drop table: %w", delErr)
			}
			if err := tableRef.Create(ctx, metadata); err != nil {
				return fmt.Errorf("failed to create table after drop: %w", err)
			}
		} else {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}
	return nil
}

func (d *BigQueryDestination) normalizeSchemaForLoadMethod(tableSchema *schema.TableSchema) *schema.TableSchema {
	if tableSchema == nil || d.effectiveLoadMethod() != loadMethodLoadJob {
		return tableSchema
	}

	cp := *tableSchema
	cp.Columns = make([]schema.Column, len(tableSchema.Columns))
	for i, col := range tableSchema.Columns {
		cp.Columns[i] = col
		cp.Columns[i].Nullable = true
	}

	return &cp
}

func (d *BigQueryDestination) ensureTableExpiration(
	ctx context.Context,
	tableRef *bigquery.Table,
	existingMeta *bigquery.TableMetadata,
	expiresAfter time.Duration,
) error {
	if expiresAfter <= 0 || existingMeta == nil {
		return nil
	}

	desiredExpiration := time.Now().UTC().Add(expiresAfter)
	update := bigquery.TableMetadataToUpdate{
		ExpirationTime: desiredExpiration,
	}

	if _, err := tableRef.Update(ctx, update, ""); err != nil {
		return fmt.Errorf("failed to set table expiration: %w", err)
	}

	return nil
}

func (d *BigQueryDestination) ensureLoadJobColumnRelaxation(
	ctx context.Context,
	tableRef *bigquery.Table,
	existingMeta *bigquery.TableMetadata,
	sourceSchema *schema.TableSchema,
) error {
	if d.effectiveLoadMethod() != loadMethodLoadJob || existingMeta == nil || sourceSchema == nil {
		return nil
	}

	sourceCols := make(map[string]struct{}, len(sourceSchema.Columns))
	for _, col := range sourceSchema.Columns {
		sourceCols[col.Name] = struct{}{}
	}

	keyCols := make(map[string]bool)
	if existingMeta.TableConstraints != nil && existingMeta.TableConstraints.PrimaryKey != nil {
		for _, col := range existingMeta.TableConstraints.PrimaryKey.Columns {
			keyCols[col] = true
		}
	}
	if existingMeta.Clustering != nil {
		for _, col := range existingMeta.Clustering.Fields {
			keyCols[col] = true
		}
	}

	var (
		updatedSchema bigquery.Schema
		needsUpdate   bool
	)
	for _, field := range existingMeta.Schema {
		fieldCopy := *field
		if _, ok := sourceCols[field.Name]; ok && fieldCopy.Required && !keyCols[field.Name] {
			fieldCopy.Required = false
			needsUpdate = true
		}
		updatedSchema = append(updatedSchema, &fieldCopy)
	}

	if !needsUpdate {
		return nil
	}

	config.Debug("[DEST] Relaxing REQUIRED columns for load-job compatibility on %s", tableRef.TableID)
	_, err := tableRef.Update(ctx, bigquery.TableMetadataToUpdate{
		Schema: updatedSchema,
	}, existingMeta.ETag)
	if err != nil {
		return fmt.Errorf("failed to relax table schema: %w", err)
	}

	return nil
}

func (d *BigQueryDestination) addMissingColumns(ctx context.Context, tableRef *bigquery.Table, existingMeta *bigquery.TableMetadata, sourceSchema *schema.TableSchema) error {
	existingCols := make(map[string]bool, len(existingMeta.Schema))
	for _, field := range existingMeta.Schema {
		existingCols[field.Name] = true
	}

	var newFields []*bigquery.FieldSchema
	for _, col := range sourceSchema.Columns {
		if existingCols[col.Name] {
			continue
		}
		field := &bigquery.FieldSchema{
			Name: col.Name,
			Type: MapDataTypeToBigQuery(col),
		}
		applyBigQueryDecimalPrecisionScale(field, col)
		if col.DataType == schema.TypeArray && col.ArrayType != schema.TypeUnknown {
			elemField := schema.Column{
				DataType:  col.ArrayType,
				Precision: col.Precision,
				Scale:     col.Scale,
			}
			field.Type = MapDataTypeToBigQuery(elemField)
			field.Repeated = true
		}
		newFields = append(newFields, field)
	}

	if len(newFields) == 0 {
		return nil
	}

	updatedSchema := append(existingMeta.Schema, newFields...)
	config.Debug("[DEST] Adding %d missing column(s) to %s", len(newFields), tableRef.TableID)

	_, err := tableRef.Update(ctx, bigquery.TableMetadataToUpdate{
		Schema: updatedSchema,
	}, existingMeta.ETag)
	if err != nil {
		return fmt.Errorf("failed to update table schema: %w", err)
	}

	return nil
}

// Write writes records to BigQuery (single-threaded).
func (d *BigQueryDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	// Delegate to WriteParallel with parallelism=1
	opts.Parallelism = 1
	return d.WriteParallel(ctx, records, opts)
}

// WriteParallel writes records to BigQuery using Storage Write API with Arrow format.
func (d *BigQueryDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	dataset, table, tableKey, err := d.resolveTable(opts.Table)
	if err != nil {
		return err
	}

	// Wait for async table creation to complete (overlapped with source read)
	if pendingErr := d.takePendingTableErr(tableKey); pendingErr != nil {
		if err := <-pendingErr; err != nil {
			return fmt.Errorf("failed to prepare table: %w", err)
		}
	}

	if d.effectiveLoadMethod() == loadMethodLoadJob {
		if d.loadJobWriter != nil {
			return d.loadJobWriter(ctx, dataset, table, records, opts)
		}
		config.Debug("[DEST] Using BigQuery load job for %s.%s", dataset, table)
		return d.writeWithLoadJob(ctx, dataset, table, records, opts)
	}

	tablePath := fmt.Sprintf("projects/%s/datasets/%s/tables/%s", d.projectID, dataset, table)

	if opts.AtomicCommit {
		parallelism := d.resolvePendingWriteParallelism(opts.Parallelism)
		config.Debug("[DEST] Using Storage Write API (Arrow format, pending streams) for %s", tablePath)
		return d.storageArrowClient.AppendArrowPendingStreamsFromSource(ctx, tablePath, records, parallelism)
	}

	streamPath := tablePath + "/streams/_default"
	config.Debug("[DEST] Using Storage Write API (Arrow format, default stream) for %s", streamPath)

	parallelism := d.resolveWriteParallelism(opts.Parallelism)

	return d.storageArrowClient.AppendArrowStreamFromSource(ctx, streamPath, records, parallelism)
}

const (
	maxDefaultStreamParallelism = 4
	maxPendingStreamParallelism = 32
)

func (d *BigQueryDestination) resolveWriteParallelism(requested int) int {
	if requested <= 0 {
		return defaultWriteParallelism
	}
	if requested > maxDefaultStreamParallelism {
		config.Debug(
			"[DEST] Capping Storage Write parallelism from %d to %d for default stream stability",
			requested,
			maxDefaultStreamParallelism,
		)
		return maxDefaultStreamParallelism
	}
	return requested
}

func (d *BigQueryDestination) resolvePendingWriteParallelism(requested int) int {
	if requested <= 0 {
		return defaultWriteParallelism
	}
	if requested > maxPendingStreamParallelism {
		config.Debug(
			"[DEST] Capping pending Storage Write parallelism from %d to %d",
			requested,
			maxPendingStreamParallelism,
		)
		return maxPendingStreamParallelism
	}
	return requested
}

func (d *BigQueryDestination) effectiveLoadMethod() bigQueryLoadMethod {
	if d.loadMethod == "" {
		return loadMethodLoadJob
	}
	return d.loadMethod
}

func (d *BigQueryDestination) SupportsAtomicCommitWrites() bool {
	switch d.effectiveLoadMethod() {
	case loadMethodLoadJob, loadMethodStorageWrite:
		return true
	default:
		return false
	}
}

func (d *BigQueryDestination) WaitForExactRowCount(ctx context.Context, table string, expectedRows int64) error {
	if expectedRows < 0 {
		return fmt.Errorf("expected row count must be non-negative, got %d", expectedRows)
	}

	dataset, tableName, err := ParseTableName(table)
	if err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}

	waitCtx := ctx
	cancel := func() {}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > exactRowCountWaitTimeout {
		waitCtx, cancel = context.WithTimeout(ctx, exactRowCountWaitTimeout)
	}
	defer cancel()

	ticker := time.NewTicker(exactRowCountPollInterval)
	defer ticker.Stop()

	var lastCount int64 = -1
	for {
		actualRows, err := d.queryTableRowCount(waitCtx, dataset, tableName)
		if err == nil {
			lastCount = actualRows
			if actualRows == expectedRows {
				return nil
			}
			config.Debug(
				"[DEST] Waiting for exact row count on %s: rows=%d expected=%d",
				table,
				actualRows,
				expectedRows,
			)
		} else {
			config.Debug("[DEST] Waiting for exact row count on %s: count query failed: %v", table, err)
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf(
				"timed out waiting for exact row count on %s: got %d rows, want %d: %w",
				table,
				lastCount,
				expectedRows,
				waitCtx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func (d *BigQueryDestination) queryTableRowCount(ctx context.Context, dataset, table string) (int64, error) {
	sql := fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`.`%s`", d.projectID, dataset, table)
	q := d.client.Query(sql)
	it, err := q.Read(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to run row count query: %w", err)
	}

	var values []bigquery.Value
	if err := it.Next(&values); err != nil {
		return 0, fmt.Errorf("failed to read row count query result: %w", err)
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("unexpected row count query result: %v", values)
	}

	switch v := values[0].(type) {
	case int64:
		return v, nil
	case uint64:
		return int64(v), nil
	default:
		return 0, fmt.Errorf("unexpected row count type %T", v)
	}
}

// SwapTable swaps a staging table with the target table.
func (d *BigQueryDestination) SwapTable(ctx context.Context, stagingTable string, targetTable string) error {
	stagingDataset, stagingTableName, err := ParseTableName(stagingTable)
	if err != nil {
		return fmt.Errorf("invalid staging table name: %w", err)
	}

	targetDataset, targetTableName, err := ParseTableName(targetTable)
	if err != nil {
		return fmt.Errorf("invalid target table name: %w", err)
	}

	stagingRef := d.client.Dataset(stagingDataset).Table(stagingTableName)

	config.Debug("[DEST] Swapping tables: %s → %s", stagingTable, targetTable)

	if d.effectiveLoadMethod() == loadMethodLoadJob {
		if err := d.swapTableWithCopyJob(ctx, stagingDataset, stagingTableName, targetDataset, targetTableName); err != nil {
			return err
		}
		config.Debug("[DEST] Copy completed, deleting staging table")
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := stagingRef.Delete(cleanupCtx); err != nil {
			config.Debug("[DEST] Failed to delete staging table: %v", err)
		}
		return nil
	}

	if d.partitionBy != "" || len(d.clusterBy) > 0 {
		// For partitioned/clustered tables, must use SQL to apply partitioning
		sql := fmt.Sprintf("CREATE OR REPLACE TABLE `%s`.`%s`.`%s`\n", d.projectID, targetDataset, targetTableName)

		if d.partitionBy != "" {
			sql += fmt.Sprintf("PARTITION BY DATE(`%s`)\n", d.partitionBy)
		}

		if len(d.clusterBy) > 0 {
			clusterCols := make([]string, len(d.clusterBy))
			for i, col := range d.clusterBy {
				clusterCols[i] = "`" + col + "`"
			}
			sql += fmt.Sprintf("CLUSTER BY %s\n", strings.Join(clusterCols, ", "))
		}

		sql += fmt.Sprintf("AS SELECT * FROM `%s`.`%s`.`%s`", d.projectID, stagingDataset, stagingTableName)

		config.Debug("[DEST] Executing SQL copy (partitioned): %s", sql)

		query := d.client.Query(sql)
		if d.location != "" {
			query.Location = d.location
		}

		job, err := query.Run(ctx)
		if err != nil {
			return fmt.Errorf("failed to start SQL copy job: %w", err)
		}

		for {
			status, err := job.Status(ctx)
			if err != nil {
				return fmt.Errorf("SQL copy job status check failed: %w", err)
			}
			if status.Done() {
				if status.Err() != nil {
					return fmt.Errorf("SQL copy job error: %w", status.Err())
				}
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	} else {
		// Use SQL CREATE OR REPLACE TABLE AS SELECT * — Copy Jobs don't read
		// from the streaming buffer, so they'd copy 0 rows after Storage Write API writes.
		sql := fmt.Sprintf("CREATE OR REPLACE TABLE `%s`.`%s`.`%s` AS SELECT * FROM `%s`.`%s`.`%s`",
			d.projectID, targetDataset, targetTableName,
			d.projectID, stagingDataset, stagingTableName)

		config.Debug("[DEST] Executing SQL swap: %s", sql)

		query := d.client.Query(sql)
		if d.location != "" {
			query.Location = d.location
		}

		job, err := query.Run(ctx)
		if err != nil {
			return fmt.Errorf("failed to start SQL swap job: %w", err)
		}

		for {
			status, err := job.Status(ctx)
			if err != nil {
				return fmt.Errorf("SQL swap job status check failed: %w", err)
			}
			if status.Done() {
				if status.Err() != nil {
					return fmt.Errorf("SQL swap job error: %w", status.Err())
				}
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	config.Debug("[DEST] Copy completed, deleting staging table")
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := stagingRef.Delete(cleanupCtx); err != nil {
		config.Debug("[DEST] Failed to delete staging table: %v", err)
	}

	return nil
}

// Exec executes a SQL query.
func (d *BigQueryDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	query := d.client.Query(sql)
	if d.location != "" {
		query.Location = d.location
	}

	job, err := query.Run(ctx)
	if err != nil {
		config.LogFailedQuery(sql, err)
		return fmt.Errorf("failed to run query: %w", err)
	}

	status, err := job.Wait(ctx)
	if err != nil {
		config.LogFailedQuery(sql, err)
		if isBigQueryAlterTypeRewriteCandidate(sql, err) {
			if rewriteErr := d.execAlterColumnTypeWithRewrite(ctx, sql); rewriteErr == nil {
				return nil
			} else {
				return fmt.Errorf("query job failed: %w (rewrite fallback failed: %v)", err, rewriteErr)
			}
		}
		return fmt.Errorf("query job failed: %w", err)
	}
	if err := status.Err(); err != nil {
		config.LogFailedQuery(sql, err)
		if isBigQueryAlterTypeRewriteCandidate(sql, err) {
			if rewriteErr := d.execAlterColumnTypeWithRewrite(ctx, sql); rewriteErr == nil {
				return nil
			} else {
				return fmt.Errorf("query job error: %w (rewrite fallback failed: %v)", err, rewriteErr)
			}
		}
		return fmt.Errorf("query job error: %w", err)
	}

	return nil
}

func isBigQueryAlterTypeRewriteCandidate(sql string, err error) bool {
	if err == nil {
		return false
	}
	if _, _, _, ok := parseAlterColumnTypeSQL(sql); !ok {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "alter table alter column set data type requires") ||
		strings.Contains(msg, "assignable to the new type")
}

func parseAlterColumnTypeSQL(sql string) (table string, column string, newType string, ok bool) {
	const (
		prefix      = "ALTER TABLE "
		alterColumn = " ALTER COLUMN "
		setDataType = " SET DATA TYPE "
	)

	trimmed := strings.TrimSpace(sql)
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, prefix) {
		return "", "", "", false
	}

	rest := trimmed[len(prefix):]
	restUpper := upper[len(prefix):]
	alterIdx := strings.Index(restUpper, alterColumn)
	if alterIdx < 0 {
		return "", "", "", false
	}

	table = strings.TrimSpace(rest[:alterIdx])
	afterAlter := rest[alterIdx+len(alterColumn):]
	afterAlterUpper := restUpper[alterIdx+len(alterColumn):]
	typeIdx := strings.Index(afterAlterUpper, setDataType)
	if typeIdx < 0 {
		return "", "", "", false
	}

	column = strings.Trim(afterAlter[:typeIdx], "` ")
	newType = strings.TrimSpace(afterAlter[typeIdx+len(setDataType):])
	if table == "" || column == "" || newType == "" {
		return "", "", "", false
	}

	return strings.ReplaceAll(table, "`", ""), column, newType, true
}

func (d *BigQueryDestination) execAlterColumnTypeWithRewrite(ctx context.Context, originalSQL string) error {
	tableName, columnName, newType, ok := parseAlterColumnTypeSQL(originalSQL)
	if !ok {
		return fmt.Errorf("not an ALTER COLUMN TYPE statement: %s", originalSQL)
	}

	dataset, table, err := ParseTableName(tableName)
	if err != nil {
		return err
	}

	tableRef := d.client.Dataset(dataset).Table(table)
	meta, err := tableRef.Metadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch table metadata for rewrite: %w", err)
	}

	rewrittenSQL, err := d.buildAlterColumnTypeRewriteSQL(dataset, table, columnName, newType, meta)
	if err != nil {
		return err
	}

	config.Debug("[DEST] Rewriting unsupported ALTER COLUMN TYPE with CREATE OR REPLACE TABLE for %s.%s", dataset, table)
	query := d.client.Query(rewrittenSQL)
	if d.location != "" {
		query.Location = d.location
	}

	job, err := query.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to start rewrite query: %w", err)
	}

	status, err := job.Wait(ctx)
	if err != nil {
		return fmt.Errorf("rewrite query failed: %w", err)
	}
	if err := status.Err(); err != nil {
		return fmt.Errorf("rewrite query error: %w", err)
	}

	return nil
}

func (d *BigQueryDestination) buildAlterColumnTypeRewriteSQL(
	dataset string,
	table string,
	columnName string,
	newType string,
	meta *bigquery.TableMetadata,
) (string, error) {
	if meta == nil {
		return "", errors.New("table metadata is required")
	}
	if meta.RangePartitioning != nil {
		return "", errors.New("range-partitioned tables are not supported for type rewrite")
	}
	if meta.TimePartitioning != nil && meta.TimePartitioning.Field == "" {
		return "", errors.New("ingestion-time partitioned tables are not supported for type rewrite")
	}

	selectExprs := make([]string, 0, len(meta.Schema))
	foundColumn := false
	for _, field := range meta.Schema {
		if field.Name == columnName {
			selectExprs = append(selectExprs, fmt.Sprintf("CAST(`%s` AS %s) AS `%s`", field.Name, newType, field.Name))
			foundColumn = true
			continue
		}
		selectExprs = append(selectExprs, fmt.Sprintf("`%s`", field.Name))
	}
	if !foundColumn {
		return "", fmt.Errorf("column %q not found in table metadata", columnName)
	}

	var sqlBuilder strings.Builder
	fmt.Fprintf(&sqlBuilder, "CREATE OR REPLACE TABLE `%s`.`%s`.`%s`\n", d.projectID, dataset, table)
	if meta.TimePartitioning != nil && meta.TimePartitioning.Field != "" {
		fmt.Fprintf(&sqlBuilder, "PARTITION BY DATE(`%s`)\n", meta.TimePartitioning.Field)
	}
	if meta.Clustering != nil && len(meta.Clustering.Fields) > 0 {
		clusterCols := make([]string, len(meta.Clustering.Fields))
		for i, field := range meta.Clustering.Fields {
			clusterCols[i] = fmt.Sprintf("`%s`", field)
		}
		fmt.Fprintf(&sqlBuilder, "CLUSTER BY %s\n", strings.Join(clusterCols, ", "))
	}
	fmt.Fprintf(
		&sqlBuilder,
		"AS SELECT %s FROM `%s`.`%s`.`%s`",
		strings.Join(selectExprs, ", "),
		d.projectID,
		dataset,
		table,
	)

	return sqlBuilder.String(), nil
}

// MergeTable performs an atomic merge operation using BigQuery's MERGE statement.
// This merges data from stagingTable into targetTable based on primary keys.
func (d *BigQueryDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("merge requires at least one primary key")
	}

	stagingDataset, stagingTableName, err := ParseTableName(opts.StagingTable)
	if err != nil {
		return fmt.Errorf("invalid staging table name: %w", err)
	}

	targetDataset, targetTableName, err := ParseTableName(opts.TargetTable)
	if err != nil {
		return fmt.Errorf("invalid target table name: %w", err)
	}

	// Fetch target and staging table schemas to detect type mismatches
	castMap := d.buildCastMap(ctx, targetDataset, targetTableName, stagingDataset, stagingTableName)

	// Build MERGE statement
	mergeSQL := d.buildMergeSQL(targetDataset, targetTableName, stagingDataset, stagingTableName, opts.PrimaryKeys, opts.Columns, castMap)

	config.Debug("[MERGE] Executing MERGE statement")
	config.Debug("[MERGE] SQL: %s", mergeSQL)

	query := d.client.Query(mergeSQL)
	if d.location != "" {
		query.Location = d.location
	}

	job, err := query.Run(ctx)
	if err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to start merge job: %w", err)
	}
	config.Debug("[MERGE] Merge job started: %s", job.ID())

	status, err := job.Wait(ctx)
	if err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("merge job failed: %w", err)
	}
	if err := status.Err(); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("merge job error: %w", err)
	}

	config.Debug("[MERGE] Merge completed successfully")

	return nil
}

// DeleteInsertTable performs a DELETE + INSERT operation for BigQuery.
func (d *BigQueryDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	stagingDataset, stagingTableName, err := ParseTableName(opts.StagingTable)
	if err != nil {
		return fmt.Errorf("invalid staging table name: %w", err)
	}

	targetDataset, targetTableName, err := ParseTableName(opts.TargetTable)
	if err != nil {
		return fmt.Errorf("invalid target table name: %w", err)
	}

	startVal := formatBigQueryValue(opts.IntervalStart, opts.IncrementalKeyType)
	endVal := formatBigQueryValue(opts.IntervalEnd, opts.IncrementalKeyType)

	deleteSQL := fmt.Sprintf(
		"DELETE FROM `%s`.`%s`.`%s` WHERE `%s` >= %s AND `%s` <= %s",
		d.projectID, targetDataset, targetTableName,
		opts.IncrementalKey, startVal,
		opts.IncrementalKey, endVal,
	)

	config.Debug("[DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if err := d.Exec(ctx, deleteSQL); err != nil {
		return fmt.Errorf("failed to delete records: %w", err)
	}

	quotedCols := make([]string, len(opts.Columns))
	for i, col := range opts.Columns {
		quotedCols[i] = fmt.Sprintf("`%s`", col)
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO `%s`.`%s`.`%s` (%s) SELECT %s FROM `%s`.`%s`.`%s`",
		d.projectID, targetDataset, targetTableName,
		strings.Join(quotedCols, ", "),
		strings.Join(quotedCols, ", "),
		d.projectID, stagingDataset, stagingTableName,
	)

	config.Debug("[DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if err := d.Exec(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert records: %w", err)
	}

	config.Debug("[DELETE+INSERT] Delete+Insert completed successfully")
	return nil
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *BigQueryDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	stagingDataset, stagingTableName, err := ParseTableName(opts.StagingTable)
	if err != nil {
		return fmt.Errorf("invalid staging table name: %w", err)
	}

	targetDataset, targetTableName, err := ParseTableName(opts.TargetTable)
	if err != nil {
		return fmt.Errorf("invalid target table name: %w", err)
	}

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, opts.PrimaryKeys)
	changeConditions := buildChangeConditionsBigQuery(nonPKColumns, "t", "s")
	onConditions := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		onConditions[i] = fmt.Sprintf("t.`%s` = s.`%s`", pk, pk)
	}
	onClause := strings.Join(onConditions, " AND ")

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	updateSQL := fmt.Sprintf(
		`
		MERGE INTO %s.%s.%s AS t
		USING %s.%s.%s AS s
		ON %s AND t._scd_is_current = true AND (%s)
		WHEN MATCHED THEN UPDATE SET
			t._scd_valid_to = s._scd_valid_from,
			t._scd_is_current = false`,
		quoteIdentifier(d.projectID), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
		quoteIdentifier(d.projectID), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTableName),
		onClause,
		changeConditions,
	)
	config.Debug("[BIGQUERY SCD2] Step 1 - Close changed records: %s", updateSQL)

	if err := d.Exec(ctx, updateSQL); err != nil {
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		pkColumnsQuoted := make([]string, len(opts.PrimaryKeys))
		for i, pk := range opts.PrimaryKeys {
			pkColumnsQuoted[i] = fmt.Sprintf("`%s`", pk)
		}
		// Format timestamp as BigQuery TIMESTAMP literal
		tsLiteral := fmt.Sprintf("TIMESTAMP '%s'", opts.Timestamp.Format("2006-01-02 15:04:05.999999"))
		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s.%s.%s AS t SET
				t._scd_valid_to = %s,
				t._scd_is_current = false
			WHERE t._scd_is_current = true
			  AND NOT EXISTS (
				SELECT 1 FROM %s.%s.%s AS s
				WHERE %s
			  )`,
			quoteIdentifier(d.projectID), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
			tsLiteral,
			quoteIdentifier(d.projectID), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTableName),
			onClause,
		)
		config.Debug("[BIGQUERY SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if err := d.Exec(ctx, softDeleteSQL); err != nil {
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	allColumns := append(opts.Columns, "_scd_valid_from", "_scd_valid_to", "_scd_is_current")
	quotedColumns := make([]string, len(allColumns))
	for i, col := range allColumns {
		quotedColumns[i] = fmt.Sprintf("`%s`", col)
	}

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s.%s.%s (%s)
		SELECT %s FROM %s.%s.%s AS s
		WHERE NOT EXISTS (
			SELECT 1 FROM %s.%s.%s AS t
			WHERE %s
			  AND t._scd_is_current = true
		)`,
		quoteIdentifier(d.projectID), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quoteIdentifier(d.projectID), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTableName),
		quoteIdentifier(d.projectID), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
		onClause,
	)
	config.Debug("[BIGQUERY SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if err := d.Exec(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	config.Debug("[BIGQUERY SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func quoteIdentifier(s string) string {
	return fmt.Sprintf("`%s`", s)
}

func filterColumns(columns []string, exclude []string) []string {
	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToLower(col)] = true
	}

	var result []string
	for _, col := range columns {
		if !excludeMap[strings.ToLower(col)] {
			result = append(result, col)
		}
	}
	return result
}

func buildChangeConditionsBigQuery(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "false"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		// BigQuery supports IS DISTINCT FROM (via NOT (a = b) with NULL handling)
		conditions[i] = fmt.Sprintf(
			`IFNULL(%s.%s <> %s.%s, %s.%s IS NOT NULL OR %s.%s IS NOT NULL)`,
			targetAlias, quoteIdentifier(col), sourceAlias, quoteIdentifier(col),
			targetAlias, quoteIdentifier(col), sourceAlias, quoteIdentifier(col),
		)
	}
	return strings.Join(conditions, " OR ")
}

func formatBigQueryValue(v interface{}, keyType schema.DataType) string {
	switch val := v.(type) {
	case time.Time:
		if keyType == schema.TypeDate {
			return fmt.Sprintf("DATE '%s'", val.Format("2006-01-02"))
		}
		return fmt.Sprintf("TIMESTAMP '%s'", val.Format("2006-01-02 15:04:05.000000"))
	case *time.Time:
		if val == nil {
			return "NULL"
		}
		if keyType == schema.TypeDate {
			return fmt.Sprintf("DATE '%s'", val.Format("2006-01-02"))
		}
		return fmt.Sprintf("TIMESTAMP '%s'", val.Format("2006-01-02 15:04:05.000000"))
	case string:
		return fmt.Sprintf("'%s'", val)
	case int, int32, int64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("'%v'", val)
	}
}

// buildCastMap compares target and staging table schemas and returns a map of
// column name → target BigQuery type name for columns that need casting.
func (d *BigQueryDestination) buildCastMap(ctx context.Context, targetDataset, targetTable, stagingDataset, stagingTable string) map[string]string {
	targetMeta, err := d.client.Dataset(targetDataset).Table(targetTable).Metadata(ctx)
	if err != nil {
		return nil
	}
	stagingMeta, err := d.client.Dataset(stagingDataset).Table(stagingTable).Metadata(ctx)
	if err != nil {
		return nil
	}

	targetTypes := make(map[string]bigquery.FieldType)
	for _, f := range targetMeta.Schema {
		targetTypes[f.Name] = f.Type
	}

	stagingTypes := make(map[string]bigquery.FieldType)
	for _, f := range stagingMeta.Schema {
		stagingTypes[f.Name] = f.Type
	}

	dialect := &Dialect{}
	castMap := make(map[string]string)
	for col, targetType := range targetTypes {
		if stagingType, ok := stagingTypes[col]; ok && stagingType != targetType {
			targetSchemaType := mapBigQueryTypeToSchema(&bigquery.FieldSchema{Type: targetType})
			castMap[col] = dialect.TypeName(schema.Column{DataType: targetSchemaType})
		}
	}

	if len(castMap) == 0 {
		return nil
	}
	return castMap
}

// castSourceCol returns the source column reference, adding a CAST if the column
// has a type mismatch between staging and target tables.
func castSourceCol(col string, castMap map[string]string) string {
	if castMap != nil {
		if targetType, ok := castMap[col]; ok {
			return fmt.Sprintf("CAST(s.`%s` AS %s)", col, targetType)
		}
	}
	return fmt.Sprintf("s.`%s`", col)
}

// buildMergeSQL constructs a BigQuery MERGE statement
func (d *BigQueryDestination) buildMergeSQL(targetDataset, targetTable, stagingDataset, stagingTable string, primaryKeys, allColumns []string, castMap map[string]string) string {
	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf("t.`%s` = %s", pk, castSourceCol(pk, castMap))
	}
	onClause := strings.Join(onConditions, " AND ")

	// Build UPDATE SET clause (all non-PK columns)
	pkMap := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	var updateSets []string
	for _, col := range allColumns {
		if !pkMap[strings.ToLower(col)] {
			updateSets = append(updateSets, fmt.Sprintf("t.`%s` = %s", col, castSourceCol(col, castMap)))
		}
	}

	// Build INSERT columns and values
	quotedCols := make([]string, len(allColumns))
	sourceCols := make([]string, len(allColumns))
	for i, col := range allColumns {
		quotedCols[i] = fmt.Sprintf("`%s`", col)
		sourceCols[i] = castSourceCol(col, castMap)
	}

	// Check if this is CDC mode (has _cdc_deleted column)
	hasCDCDeleted := slices.Contains(allColumns, "_cdc_deleted")

	var sql strings.Builder
	fmt.Fprintf(&sql, "MERGE `%s`.`%s`.`%s` AS t\n", d.projectID, targetDataset, targetTable)

	if hasCDCDeleted && len(primaryKeys) > 0 {
		// CDC mode: deduplicate staging table by PKs, keeping the latest change per row.
		// This handles cases where the same row appears in both the snapshot and WAL stream.
		pkPartition := make([]string, len(primaryKeys))
		for i, pk := range primaryKeys {
			pkPartition[i] = fmt.Sprintf("`%s`", pk)
		}
		fmt.Fprintf(
			&sql,
			"USING (SELECT * FROM `%s`.`%s`.`%s` QUALIFY ROW_NUMBER() OVER (PARTITION BY %s ORDER BY `_cdc_lsn` DESC, `_cdc_deleted` DESC) = 1) AS s\n",
			d.projectID, stagingDataset, stagingTable, strings.Join(pkPartition, ", "),
		)
	} else {
		pkPartition := make([]string, len(primaryKeys))
		for i, pk := range primaryKeys {
			pkPartition[i] = fmt.Sprintf("`%s`", pk)
		}

		fmt.Fprintf(
			&sql,
			"USING (SELECT * FROM `%s`.`%s`.`%s` QUALIFY ROW_NUMBER() OVER (PARTITION BY %s) = 1) AS s\n",
			d.projectID, stagingDataset, stagingTable, strings.Join(pkPartition, ", "),
		)
	}

	fmt.Fprintf(&sql, "ON %s\n", onClause)

	if hasCDCDeleted {
		// CDC mode: handle deleted rows specially (only update CDC columns to preserve original data)

		// WHEN MATCHED AND NOT deleted: full update
		if len(updateSets) > 0 {
			sql.WriteString("WHEN MATCHED AND s.`_cdc_deleted` = false THEN\n")
			fmt.Fprintf(&sql, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}

		// WHEN MATCHED AND deleted: only update CDC columns (preserve original data)
		sql.WriteString("WHEN MATCHED AND s.`_cdc_deleted` = true THEN\n")
		sql.WriteString("  UPDATE SET t.`_cdc_deleted` = true, t.`_cdc_lsn` = s.`_cdc_lsn`, t.`_cdc_synced_at` = s.`_cdc_synced_at`\n")

		// WHEN NOT MATCHED AND NOT deleted: insert
		sql.WriteString("WHEN NOT MATCHED AND s.`_cdc_deleted` = false THEN\n")
		fmt.Fprintf(&sql, "  INSERT (%s)\n", strings.Join(quotedCols, ", "))
		fmt.Fprintf(&sql, "  VALUES (%s)", strings.Join(sourceCols, ", "))
	} else {
		// Non-CDC mode: standard merge
		if len(updateSets) > 0 {
			sql.WriteString("WHEN MATCHED THEN\n")
			fmt.Fprintf(&sql, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}

		sql.WriteString("WHEN NOT MATCHED THEN\n")
		fmt.Fprintf(&sql, "  INSERT (%s)\n", strings.Join(quotedCols, ", "))
		fmt.Fprintf(&sql, "  VALUES (%s)", strings.Join(sourceCols, ", "))
	}

	return sql.String()
}

// BeginTransaction begins a transaction.
func (d *BigQueryDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	// BigQuery doesn't support traditional transactions
	// Use MergeTable method instead for merge operations
	return nil, errors.New("transactions not supported for BigQuery - use MergeTable for merge operations")
}

// DropTable drops a table if it exists.
func (d *BigQueryDestination) DropTable(ctx context.Context, table string) error {
	dataset, tableName, err := ParseTableName(table)
	if err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}

	// Use dataset from table name if not set in URI
	if dataset == "" && d.datasetID != "" {
		dataset = d.datasetID
	}

	if dataset == "" {
		return errors.New("dataset must be specified in table name (dataset.table) or URI path")
	}

	tableRef := d.client.Dataset(dataset).Table(tableName)
	if err := tableRef.Delete(ctx); err != nil && !isNotFoundError(err) {
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[DEST] Dropped table: %s", table)
	return nil
}

// TruncateTable empties a table while preserving its definition and dependents.
func (d *BigQueryDestination) TruncateTable(ctx context.Context, table string) error {
	dataset, tableName, err := ParseTableName(table)
	if err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}

	if dataset == "" && d.datasetID != "" {
		dataset = d.datasetID
	}
	if dataset == "" {
		return errors.New("dataset must be specified in table name (dataset.table) or URI path")
	}

	truncateSQL := fmt.Sprintf("TRUNCATE TABLE `%s`.`%s`.`%s`", d.projectID, dataset, tableName)
	query := d.client.Query(truncateSQL)
	if d.location != "" {
		query.Location = d.location
	}
	job, err := query.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to submit truncate for %s: %w", table, err)
	}
	status, err := job.Wait(ctx)
	if err != nil {
		return fmt.Errorf("truncate wait failed for %s: %w", table, err)
	}
	if err := status.Err(); err != nil {
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[DEST] Truncated table: %s", table)
	return nil
}

// SupportsReplaceStrategy returns true as BigQuery supports the replace strategy.
func (d *BigQueryDestination) SupportsReplaceStrategy() bool { return true }

// SupportsAppendStrategy returns true as BigQuery supports the append strategy.
func (d *BigQueryDestination) SupportsAppendStrategy() bool { return true }

// SupportsMergeStrategy returns true as BigQuery supports the merge strategy via native MERGE.
func (d *BigQueryDestination) SupportsMergeStrategy() bool { return true }

// SupportsDeleteInsertStrategy returns true as BigQuery supports the delete+insert strategy.
func (d *BigQueryDestination) SupportsDeleteInsertStrategy() bool { return true }

// SupportsSCD2Strategy returns true as BigQuery supports the SCD2 strategy.
func (d *BigQueryDestination) SupportsSCD2Strategy() bool { return true }

// SupportsAtomicSwap returns true as BigQuery supports atomic table swaps.
func (d *BigQueryDestination) SupportsAtomicSwap() bool { return true }

func (d *BigQueryDestination) GetScheme() string { return "bigquery" }

func (d *BigQueryDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	dataset, tableName, err := ParseTableName(table)
	if err != nil {
		return nil, err
	}

	if dataset == "" && d.datasetID != "" {
		dataset = d.datasetID
	}

	if dataset == "" {
		return nil, errors.New("dataset must be specified in table name (dataset.table) or URI path")
	}

	tableRef := d.client.Dataset(dataset).Table(tableName)

	metadata, err := tableRef.Metadata(ctx)
	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get table metadata: %w", err)
	}

	var columns []schema.Column
	for _, field := range metadata.Schema {
		col := schema.Column{
			Name:     field.Name,
			DataType: mapBigQueryTypeToSchema(field),
			Nullable: !field.Required,
		}

		if field.Type == bigquery.NumericFieldType || field.Type == bigquery.BigNumericFieldType {
			col.Precision, col.Scale = normalizeBigQueryDecimalPrecisionScale(field.Type, field.Precision, field.Scale)
		}

		columns = append(columns, col)
	}

	return &schema.TableSchema{
		Name:    tableName,
		Schema:  dataset,
		Columns: columns,
	}, nil
}

func mapBigQueryTypeToSchema(field *bigquery.FieldSchema) schema.DataType {
	if field.Repeated {
		return schema.TypeArray
	}

	switch field.Type {
	case bigquery.BooleanFieldType:
		return schema.TypeBoolean
	case bigquery.IntegerFieldType:
		return schema.TypeInt64
	case bigquery.FloatFieldType:
		return schema.TypeFloat64
	case bigquery.NumericFieldType, bigquery.BigNumericFieldType:
		return schema.TypeDecimal
	case bigquery.StringFieldType:
		return schema.TypeString
	case bigquery.BytesFieldType:
		return schema.TypeBinary
	case bigquery.DateFieldType:
		return schema.TypeDate
	case bigquery.TimeFieldType:
		return schema.TypeTime
	case bigquery.DateTimeFieldType:
		return schema.TypeTimestamp
	case bigquery.TimestampFieldType:
		return schema.TypeTimestampTZ
	case bigquery.JSONFieldType:
		return schema.TypeJSON
	default:
		return schema.TypeString
	}
}

// isNotFoundError checks if an error is a "not found" error.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check for various "not found" error messages
	errStr := err.Error()
	return contains(errStr, "not found") || contains(errStr, "Not found") || contains(errStr, "NOT_FOUND")
}

func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "Already Exists") || contains(errStr, "already exists") || contains(errStr, "ALREADY_EXISTS") || contains(errStr, "409")
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func (d *BigQueryDestination) SupportsCDCMerge() bool {
	return true
}

func (d *BigQueryDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	dataset, tableName, err := ParseTableName(table)
	if err != nil {
		return "", err
	}

	if dataset == "" && d.datasetID != "" {
		dataset = d.datasetID
	}

	if dataset == "" {
		return "", errors.New("dataset must be specified in table name (dataset.table) or URI path")
	}

	query := d.client.Query(fmt.Sprintf("SELECT MAX(`_cdc_lsn`) FROM `%s`.`%s`.`%s`", d.projectID, dataset, tableName))
	if d.location != "" {
		query.Location = d.location
	}

	it, err := query.Read(ctx)
	if err != nil {
		if isNotFoundError(err) {
			return "", nil
		}
		return "", err
	}

	var row []bigquery.Value
	if err := it.Next(&row); err != nil {
		if err == iterator.Done {
			return "", nil
		}
		return "", err
	}

	if len(row) == 0 || row[0] == nil {
		return "", nil
	}

	maxLSN, ok := row[0].(string)
	if !ok || maxLSN == "" {
		return "", nil
	}

	return maxLSN, nil
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
