package bigquery

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/bigquery"
	gcsstorage "cloud.google.com/go/storage"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/annotation"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const (
	defaultWriteParallelism            = 4
	exactRowCountWaitTimeout           = 30 * time.Second
	exactRowCountPollInterval          = 1 * time.Second
	queryJobMaxAttempts                = 4
	deleteInsertTransactionMaxAttempts = 8
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

	// Cache for dataset existence checks. Multi-table writes call
	// PrepareTable from one goroutine per table; each may reach
	// ensureDatasetExists concurrently for the same (project, dataset) key,
	// so reads and writes of knownDatasets must be serialized.
	knownDatasetsMu sync.Mutex
	knownDatasets   map[string]bool

	// Async table creation: overlaps staging table creation with source read.
	// State is tracked per table so concurrent multi-table writes don't race.
	pendingTableMu   sync.Mutex
	pendingTableErrs map[string]chan error
	gcsClientMu      sync.Mutex

	loadJobWriter func(ctx context.Context, dataset, table string, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error

	cdcStateMu          sync.Mutex
	cdcStateTable       string
	cdcStateConnectorID string
	activeCDCJobs       map[string]struct{}
	cdcJobReconcileMu   sync.Mutex
	cdcJobsReconciled   bool
	cdcJobCleanupMu     sync.Mutex
	lastCDCJobCleanup   time.Time
	cdcStatePruneMu     sync.Mutex
	nextCDCStatePrune   time.Time
	datasetCaseMu       sync.Mutex
	datasetCase         map[string]bigQueryDatasetCase
}

type bigQueryDatasetCase struct {
	caseInsensitive bool
	provisional     bool
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
func (d *BigQueryDestination) ensureDatasetExists(ctx context.Context, project, datasetID string) error {
	datasetKey := project + "." + datasetID

	d.knownDatasetsMu.Lock()
	if d.knownDatasets[datasetKey] {
		d.knownDatasetsMu.Unlock()
		return nil
	}
	d.knownDatasetsMu.Unlock()

	ds := d.client.DatasetInProject(project, datasetID)

	// Check if dataset exists
	metadata, err := ds.Metadata(ctx)
	if err == nil {
		d.markDatasetKnown(datasetKey)
		d.cacheDatasetCase(datasetKey, metadata.IsCaseInsensitive, false)
		return nil
	}

	if !isNotFoundError(err) {
		return fmt.Errorf("failed to check dataset existence: %w", err)
	}

	// Dataset doesn't exist, create it
	config.Debug("[DEST] Creating dataset: %s", datasetKey)

	metadata = &bigquery.DatasetMetadata{}
	if d.location != "" {
		metadata.Location = d.location
	} else {
		metadata.Location = "US" // Default location
	}

	if err := ds.Create(ctx, metadata); err != nil {
		// Check if it was created by another process in the meantime
		if isAlreadyExistsError(err) {
			d.markDatasetKnown(datasetKey)
			d.invalidateDatasetCase(datasetKey)
			return nil
		}
		return fmt.Errorf("failed to create dataset: %w", err)
	}

	config.Debug("[DEST] Dataset created: %s", datasetKey)
	d.markDatasetKnown(datasetKey)
	d.cacheDatasetCase(datasetKey, false, false)
	return nil
}

func (d *BigQueryDestination) cacheDatasetCase(key string, caseInsensitive, provisional bool) {
	d.datasetCaseMu.Lock()
	defer d.datasetCaseMu.Unlock()
	if d.datasetCase == nil {
		d.datasetCase = make(map[string]bigQueryDatasetCase)
	}
	d.datasetCase[key] = bigQueryDatasetCase{caseInsensitive: caseInsensitive, provisional: provisional}
}

func (d *BigQueryDestination) invalidateDatasetCase(key string) {
	d.datasetCaseMu.Lock()
	defer d.datasetCaseMu.Unlock()
	delete(d.datasetCase, key)
}

func (d *BigQueryDestination) markDatasetKnown(datasetKey string) {
	d.knownDatasetsMu.Lock()
	defer d.knownDatasetsMu.Unlock()
	if d.knownDatasets == nil {
		d.knownDatasets = make(map[string]bool)
	}
	d.knownDatasets[datasetKey] = true
}

// parseTable resolves a possibly project-qualified destination table into its
// project, dataset and table. The project defaults to the connection project
// when omitted; a different project performs a cross-project write (the query
// is still billed to the connection project).
func (d *BigQueryDestination) parseTable(table string) (project, dataset, tableName string, err error) {
	project, dataset, tableName, err = ParseTableName(table)
	if err != nil {
		return "", "", "", err
	}
	if project == "" {
		project = d.projectID
	}
	return project, dataset, tableName, nil
}

func (d *BigQueryDestination) resolveTable(table string) (project, dataset, tableName, tableKey string, err error) {
	project, dataset, tableName, err = d.parseTable(table)
	if err != nil {
		return "", "", "", "", err
	}

	if dataset == "" && d.datasetID != "" {
		dataset = d.datasetID
	}

	if dataset == "" {
		return "", "", "", "", errors.New("dataset must be specified in table name (dataset.table) or URI path")
	}

	return project, dataset, tableName, project + "." + dataset + "." + tableName, nil
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

func jobRef(job *bigquery.Job) string {
	if job == nil {
		return ""
	}
	if loc := job.Location(); loc != "" {
		return fmt.Sprintf("%s (location %s)", job.ID(), loc)
	}
	return job.ID()
}

var (
	bigQueryJobReconcileDelay  = time.Second
	bigQueryAmbiguousJobWindow = 2 * time.Minute
	bigQueryJobAPIAttempts     = 4
	bigQueryJobAPICallTimeout  = 15 * time.Second
	bigQueryCDCStateMinAge     = 45 * time.Minute
	bigQueryCDCStateRetryDelay = 10 * time.Minute
)

func waitForBigQueryJob(ctx context.Context, job *bigquery.Job) (*bigquery.JobStatus, error) {
	failedAttempts := 0
	for {
		callCtx, cancel := context.WithTimeout(ctx, bigQueryJobAPICallTimeout)
		status, err := job.Wait(callCtx)
		callTimedOut := callCtx.Err() != nil && ctx.Err() == nil
		cancel()
		if err == nil {
			return status, nil
		}
		if ctx.Err() != nil {
			return reconcileCanceledBigQueryJob(ctx, job)
		}
		if callTimedOut {
			statusCtx, statusCancel := context.WithTimeout(ctx, bigQueryJobAPICallTimeout)
			polledStatus, statusErr := job.Status(statusCtx)
			statusTimedOut := statusCtx.Err() != nil && ctx.Err() == nil
			statusCancel()
			if statusErr == nil {
				failedAttempts = 0
				if polledStatus.Done() {
					return polledStatus, nil
				}
				if err := sleepWithContextForLoadJob(ctx, bigQueryJobReconcileDelay); err != nil {
					return reconcileCanceledBigQueryJob(ctx, job)
				}
				continue
			}
			err = statusErr
			callTimedOut = statusTimedOut
		}
		failedAttempts++
		if !callTimedOut && !isRetryableBigQueryAPIError(err) {
			return status, fmt.Errorf("cannot poll BigQuery job %s: %w", jobRef(job), err)
		}
		if failedAttempts == bigQueryJobAPIAttempts {
			return status, fmt.Errorf("cannot poll BigQuery job %s after %d attempts: %w", jobRef(job), failedAttempts, err)
		}
		if err := sleepWithContextForLoadJob(ctx, bigQueryJobReconcileDelay); err != nil {
			return reconcileCanceledBigQueryJob(ctx, job)
		}
	}
}

func reconcileCanceledBigQueryJob(ctx context.Context, job *bigquery.Job) (*bigquery.JobStatus, error) {
	var cancelErr error
	callCtx, cancel := context.WithTimeout(context.Background(), bigQueryJobAPICallTimeout)
	if err := job.Cancel(callCtx); err != nil {
		cancelErr = err
	}
	cancel()
	deadline := time.Now().Add(bigQueryAmbiguousJobWindow)
	failedAttempts := 0
	for {
		callCtx, cancel := context.WithTimeout(context.Background(), bigQueryJobAPICallTimeout)
		terminalStatus, err := job.Status(callCtx)
		cancel()
		if err == nil {
			failedAttempts = 0
			if terminalStatus.Done() {
				return terminalStatus, context.Cause(ctx)
			}
		} else if !isRetryableBigQueryAPIError(err) {
			return terminalStatus, errors.Join(context.Cause(ctx), cancelErr, fmt.Errorf("cannot reconcile BigQuery job %s: %w", jobRef(job), err))
		} else {
			failedAttempts++
		}
		if failedAttempts == bigQueryJobAPIAttempts || time.Now().After(deadline) {
			terminalErr := err
			if terminalErr == nil {
				terminalErr = errors.New("job did not become terminal")
			}
			return terminalStatus, errors.Join(context.Cause(ctx), cancelErr, fmt.Errorf("cannot reconcile BigQuery job %s within bounded polling window: %w", jobRef(job), terminalErr))
		}
		time.Sleep(bigQueryJobReconcileDelay)
	}
}

func (d *BigQueryDestination) reconcileAmbiguousBigQueryJob(ctx context.Context, jobID string) (*bigquery.Job, error) {
	deadline := time.Now().Add(bigQueryAmbiguousJobWindow)
	for {
		callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		job, err := d.client.JobFromProject(callCtx, d.projectID, jobID, d.location)
		cancel()
		if err == nil {
			status, terminalErr := reconcileCanceledBigQueryJob(ctx, job)
			if status != nil && status.Done() {
				_ = d.resolveCDCJob(context.Background(), jobID)
			}
			return job, terminalErr
		}
		if isNotFoundError(err) && time.Now().After(deadline) {
			_ = d.resolveCDCJob(context.Background(), jobID)
			return nil, context.Cause(ctx)
		}
		if !isNotFoundError(err) && !isRetryableBigQueryAPIError(err) {
			return nil, errors.Join(context.Cause(ctx), fmt.Errorf("cannot reconcile ambiguous BigQuery job %s: %w", jobID, err))
		}
		if time.Now().After(deadline) {
			return nil, errors.Join(context.Cause(ctx), fmt.Errorf("cannot reconcile ambiguous BigQuery job %s before deadline: %w", jobID, err))
		}
		time.Sleep(bigQueryJobReconcileDelay)
	}
}

func isRetryableBigQueryAPIError(err error) bool {
	if isRetryableLoadJobError(err) {
		return true
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr != nil {
		switch apiErr.Code {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}

func (d *BigQueryDestination) waitForBigQueryJob(ctx context.Context, job *bigquery.Job) (*bigquery.JobStatus, error) {
	status, err := waitForBigQueryJob(ctx, job)
	if status != nil && status.Done() {
		if resolveErr := d.resolveCDCJob(context.Background(), job.ID()); resolveErr != nil {
			err = errors.Join(err, resolveErr)
		}
	}
	return status, err
}

// isDatePartitionColumn reports whether the named partition column is a DATE
// column in the given internal schema. A DATE column must be referenced bare in
// a PARTITION BY clause; TIMESTAMP/DATETIME columns must be wrapped in DATE().
func isDatePartitionColumn(s *schema.TableSchema, column string) bool {
	if s == nil || column == "" {
		return false
	}
	for _, col := range s.Columns {
		if strings.EqualFold(col.Name, column) {
			return col.DataType == schema.TypeDate
		}
	}
	return false
}

// partitionFieldIsDate is the equivalent of isDatePartitionColumn for a live
// BigQuery schema, used where only table metadata (not the internal schema) is
// available.
func partitionFieldIsDate(s bigquery.Schema, column string) bool {
	for _, field := range s {
		if strings.EqualFold(field.Name, column) {
			return field.Type == bigquery.DateFieldType
		}
	}
	return false
}

// partitionByClause builds the PARTITION BY clause for a partition column.
// BigQuery requires a DATE-valued expression: an already-DATE column is used
// bare, while TIMESTAMP/DATETIME columns are wrapped in DATE().
func partitionByClause(column string, isDateColumn bool) string {
	if isDateColumn {
		return fmt.Sprintf("PARTITION BY %s\n", quoteIdentifier(column))
	}
	return fmt.Sprintf("PARTITION BY DATE(%s)\n", quoteIdentifier(column))
}

// partitionOrClusterMismatch reports whether the table's partition/cluster spec
// differs from the configured one. An empty configured spec means "leave as-is".
func (d *BigQueryDestination) partitionOrClusterMismatch(meta *bigquery.TableMetadata, clusterBy []string) bool {
	if d.partitionBy != "" {
		if meta.RangePartitioning != nil {
			return true
		}
		if meta.TimePartitioning == nil || !strings.EqualFold(meta.TimePartitioning.Field, d.partitionBy) {
			return true
		}
		// Compare the partition type against what we would create; ingestr builds
		// only Field, so the desired type is BigQuery's default.
		if effectivePartitionType(meta.TimePartitioning) != effectivePartitionType(&bigquery.TimePartitioning{Field: d.partitionBy}) {
			return true
		}
	}

	if len(clusterBy) > 0 {
		var existingCluster []string
		if meta.Clustering != nil {
			existingCluster = meta.Clustering.Fields
		}
		if len(existingCluster) != len(clusterBy) {
			return true
		}
		for i := range existingCluster {
			if !strings.EqualFold(existingCluster[i], clusterBy[i]) {
				return true
			}
		}
	}
	return false
}

// recreateSpecGuard refuses a recreate that would silently drop a live spec half
// the user didn't configure (recreate keeps only the configured partition/cluster).
func (d *BigQueryDestination) recreateSpecGuard(meta *bigquery.TableMetadata, table string, clusterBy []string) error {
	if d.partitionBy == "" && (meta.TimePartitioning != nil || meta.RangePartitioning != nil) {
		return fmt.Errorf("changing the clustering of %s requires recreating it, which would drop its existing partitioning; pass partition_by to keep (or change) it", table)
	}
	if len(clusterBy) == 0 && meta.Clustering != nil && len(meta.Clustering.Fields) > 0 {
		return fmt.Errorf("changing the partitioning of %s requires recreating it, which would drop its existing clustering (%s); pass cluster_by to keep (or change) it", table, strings.Join(meta.Clustering.Fields, ", "))
	}
	return nil
}

// effectiveClusterBy resolves the clustering ingestr would apply: the configured
// cluster_by, or the default primary-key clustering when none is configured.
func (d *BigQueryDestination) effectiveClusterBy(opts destination.SwapOptions) []string {
	if len(d.clusterBy) > 0 {
		return d.clusterBy
	}
	if opts.Schema == nil {
		return nil
	}
	return defaultClusteringFromPrimaryKeys(BuildBigQuerySchema(opts.Schema), opts.Schema.PrimaryKeys)
}

// effectivePartitionType returns the time-partitioning type, resolving the unset
// value to BigQuery's default (DAY) so a stored type compares equal to an unset one.
func effectivePartitionType(tp *bigquery.TimePartitioning) bigquery.TimePartitioningType {
	if tp == nil || tp.Type == "" {
		return bigquery.DayPartitioningType
	}
	return tp.Type
}

type mergePartitionPruning struct {
	Column string
	IsDate bool
}

func buildMergePartitionPruning(meta *bigquery.TableMetadata, primaryKeys []string) *mergePartitionPruning {
	if meta == nil || meta.TimePartitioning == nil || meta.TimePartitioning.Field == "" {
		return nil
	}
	partitionColumn := meta.TimePartitioning.Field
	if !containsIdentifier(primaryKeys, partitionColumn) {
		return nil
	}
	fieldType, ok := partitionFieldType(meta.Schema, partitionColumn)
	if !ok {
		return nil
	}
	switch fieldType {
	case bigquery.DateFieldType:
		return &mergePartitionPruning{Column: partitionColumn, IsDate: true}
	case bigquery.TimestampFieldType, bigquery.DateTimeFieldType:
		return &mergePartitionPruning{Column: partitionColumn}
	default:
		return nil
	}
}

func containsIdentifier(values []string, value string) bool {
	for _, item := range values {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

func hasCastForColumn(castMap map[string]string, column string) bool {
	for castColumn := range castMap {
		if strings.EqualFold(castColumn, column) {
			return true
		}
	}
	return false
}

func partitionFieldType(s bigquery.Schema, column string) (bigquery.FieldType, bool) {
	for _, field := range s {
		if strings.EqualFold(field.Name, column) {
			return field.Type, true
		}
	}
	return "", false
}

func mergePartitionExpr(alias, column string, isDate bool) string {
	ref := quoteIdentifier(column)
	if alias != "" {
		ref = alias + "." + ref
	}
	if isDate {
		return ref
	}
	return fmt.Sprintf("DATE(%s)", ref)
}

func mergePartitionPruningDeclarations(stagingRef string, pruning *mergePartitionPruning) string {
	if pruning == nil {
		return ""
	}
	partitionExpr := mergePartitionExpr("", pruning.Column, pruning.IsDate)
	partitionCol := quoteIdentifier(pruning.Column)
	return fmt.Sprintf(
		"DECLARE _ingestr_merge_partition_min DATE DEFAULT (SELECT MIN(%s) FROM %s);\n"+
			"DECLARE _ingestr_merge_partition_max DATE DEFAULT (SELECT MAX(%s) FROM %s);\n"+
			"DECLARE _ingestr_merge_partition_has_null BOOL DEFAULT (SELECT COALESCE(LOGICAL_OR(%s IS NULL), FALSE) FROM %s);\n\n",
		partitionExpr, stagingRef,
		partitionExpr, stagingRef,
		partitionCol, stagingRef,
	)
}

// nonNullablePKColumns returns the lower-cased primary key columns guaranteed
// non-NULL on at least one side of the merge join — REQUIRED in the target
// table, or NOT NULL in the ingestion schema (so staging holds no NULL keys).
// For these a bare equality join is equivalent to the null-safe one.
func nonNullablePKColumns(targetMeta *bigquery.TableMetadata, tableSchema *schema.TableSchema, primaryKeys []string) map[string]bool {
	nonNullable := make(map[string]bool)
	if targetMeta != nil {
		for _, f := range targetMeta.Schema {
			if f.Required {
				nonNullable[strings.ToLower(f.Name)] = true
			}
		}
	}
	if tableSchema != nil {
		for _, col := range tableSchema.Columns {
			if !col.Nullable {
				nonNullable[strings.ToLower(col.Name)] = true
			}
		}
	}

	result := make(map[string]bool, len(primaryKeys))
	for _, pk := range primaryKeys {
		if nonNullable[strings.ToLower(pk)] {
			result[strings.ToLower(pk)] = true
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func mergePartitionTargetPredicate(pruning *mergePartitionPruning) string {
	if pruning == nil {
		return ""
	}
	targetExpr := mergePartitionExpr("t", pruning.Column, pruning.IsDate)
	targetCol := fmt.Sprintf("t.%s", quoteIdentifier(pruning.Column))
	return fmt.Sprintf(
		"(%s BETWEEN _ingestr_merge_partition_min AND _ingestr_merge_partition_max OR (_ingestr_merge_partition_has_null AND %s IS NULL))",
		targetExpr,
		targetCol,
	)
}

// PrepareTable creates or recreates a table with the given schema.
func (d *BigQueryDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	ctx = annotation.WithStep(ctx, annotation.StepDDL)
	project, dataset, table, tableKey, err := d.resolveTable(opts.Table)
	if err != nil {
		return err
	}

	tableRef := d.client.DatasetInProject(project, dataset).Table(table)

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
			truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(table))
			config.Debug("[DEST] Truncating table: %s", opts.Table)
			job, err := d.runQueryJobWithRetry(ctx, truncateSQL, "truncate")
			if err != nil {
				if job != nil && job.LastStatus() != nil && isNotFoundError(job.LastStatus().Err()) {
					errCh <- d.createTableFresh(ctx, tableRef, project, dataset, metadata)
					return
				}
				errCh <- fmt.Errorf("truncate error (job %s): %w", jobRef(job), err)
				return
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
		return d.reconcileExistingTable(ctx, tableRef, existingMeta, opts)
	}
	if !isNotFoundError(err) {
		return fmt.Errorf("failed to check table existence: %w", err)
	}

	// Table doesn't exist — ensure dataset exists and create
	if err := d.ensureDatasetExists(ctx, project, dataset); err != nil {
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
		if isAlreadyExistsError(err) {
			winnerMeta, waitErr := waitForBigQueryTableMetadata(ctx, tableRef)
			if waitErr != nil {
				return fmt.Errorf("concurrently created table did not become visible: %w", waitErr)
			}
			return d.reconcileExistingTable(ctx, tableRef, winnerMeta, opts)
		}
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}

func (d *BigQueryDestination) reconcileExistingTable(ctx context.Context, tableRef *bigquery.Table, existingMeta *bigquery.TableMetadata, opts destination.PrepareOptions) error {
	tableSchema := opts.Schema
	if tableSchema != nil {
		if opts.CDCMode {
			tableSchema = makeNonPKColumnsNullable(opts.Schema, opts.PrimaryKeys)
		}
		tableSchema = d.normalizeSchemaForLoadMethod(tableSchema)
		if err := validateBigQuerySchemaCompatibility(existingMeta, tableSchema); err != nil {
			return err
		}
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

func waitForBigQueryTableMetadata(ctx context.Context, tableRef *bigquery.Table) (*bigquery.TableMetadata, error) {
	const attempts = 7
	delay := 10 * time.Millisecond
	for attempt := 0; attempt < attempts; attempt++ {
		metadata, err := tableRef.Metadata(ctx)
		if err == nil {
			return metadata, nil
		}
		if !isNotFoundError(err) {
			return nil, err
		}
		if attempt == attempts-1 {
			return nil, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, 100*time.Millisecond)
	}
	return nil, errors.New("table metadata visibility retry exhausted")
}

func validateBigQuerySchemaCompatibility(existingMeta *bigquery.TableMetadata, desired *schema.TableSchema) error {
	if existingMeta == nil || desired == nil {
		return nil
	}
	existing := make(map[string]*bigquery.FieldSchema, len(existingMeta.Schema))
	for _, field := range existingMeta.Schema {
		existing[field.Name] = field
	}
	for _, desiredField := range BuildBigQuerySchema(desired) {
		field, ok := existing[desiredField.Name]
		if !ok {
			continue
		}
		if field.Type != desiredField.Type || field.Repeated != desiredField.Repeated {
			return fmt.Errorf("bigquery table has incompatible column %q: got %s repeated=%t, want %s repeated=%t", desiredField.Name, field.Type, field.Repeated, desiredField.Type, desiredField.Repeated)
		}
		if err := validateBigQueryParameterizedField(field, desiredField); err != nil {
			return fmt.Errorf("bigquery table has incompatible column %q: %w", desiredField.Name, err)
		}
	}
	return nil
}

func validateBigQueryParameterizedField(existing, desired *bigquery.FieldSchema) error {
	switch desired.Type {
	case bigquery.StringFieldType, bigquery.BytesFieldType:
		switch {
		case desired.MaxLength == 0 && existing.MaxLength > 0:
			return fmt.Errorf("existing max length %d is bounded, want unbounded", existing.MaxLength)
		case desired.MaxLength > 0 && existing.MaxLength > 0 && existing.MaxLength < desired.MaxLength:
			return fmt.Errorf("existing max length %d is narrower than required %d", existing.MaxLength, desired.MaxLength)
		}
	case bigquery.NumericFieldType, bigquery.BigNumericFieldType:
		existingPrecision, existingScale := normalizeBigQueryDecimalPrecisionScale(existing.Type, existing.Precision, existing.Scale)
		desiredPrecision, desiredScale := normalizeBigQueryDecimalPrecisionScale(desired.Type, desired.Precision, desired.Scale)
		if existingScale < desiredScale {
			return fmt.Errorf("existing scale %d is narrower than required %d", existingScale, desiredScale)
		}
		existingIntegerDigits := existingPrecision - existingScale
		desiredIntegerDigits := desiredPrecision - desiredScale
		if existingIntegerDigits < desiredIntegerDigits {
			return fmt.Errorf("existing integer-digit capacity %d is narrower than required %d", existingIntegerDigits, desiredIntegerDigits)
		}
	}
	return nil
}

func (d *BigQueryDestination) createTableFresh(ctx context.Context, tableRef *bigquery.Table, project, dataset string, metadata *bigquery.TableMetadata) error {
	if err := d.ensureDatasetExists(ctx, project, dataset); err != nil {
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
		if (col.DataType == schema.TypeString || col.DataType == schema.TypeBinary) && col.MaxLength > 0 {
			field.MaxLength = int64(col.MaxLength)
		}
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
	project, dataset, table, tableKey, err := d.resolveTable(opts.Table)
	if err != nil {
		return err
	}

	// Wait for async table creation to complete (overlapped with source read)
	if pendingErr := d.takePendingTableErr(tableKey); pendingErr != nil {
		if err := <-pendingErr; err != nil {
			return fmt.Errorf("failed to prepare table: %w", err)
		}
	}

	if opts.PreStaged != nil {
		ps, ok := opts.PreStaged.(*preStagedLoadSet)
		if !ok || d.effectiveLoadMethod() != loadMethodLoadJob {
			return fmt.Errorf("pre-staged data is not compatible with this BigQuery configuration")
		}
		return d.writePreStaged(ctx, project, dataset, table, records, ps, opts)
	}

	if d.effectiveLoadMethod() == loadMethodLoadJob {
		if d.loadJobWriter != nil {
			return d.loadJobWriter(ctx, dataset, table, records, opts)
		}
		config.Debug("[DEST] Using BigQuery load job for %s.%s", dataset, table)
		return d.writeWithLoadJob(ctx, project, dataset, table, records, opts)
	}

	tablePath := fmt.Sprintf("projects/%s/datasets/%s/tables/%s", project, dataset, table)

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

	project, dataset, tableName, err := d.parseTable(table)
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
		actualRows, err := d.queryTableRowCount(waitCtx, project, dataset, tableName)
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

func (d *BigQueryDestination) queryTableRowCount(ctx context.Context, project, dataset, table string) (int64, error) {
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(table))
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
func (d *BigQueryDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	ctx = annotation.WithStep(ctx, annotation.StepSwap)
	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable
	stagingProject, stagingDataset, stagingTableName, err := d.parseTable(stagingTable)
	if err != nil {
		return fmt.Errorf("invalid staging table name: %w", err)
	}

	targetProject, targetDataset, targetTableName, err := d.parseTable(targetTable)
	if err != nil {
		return fmt.Errorf("invalid target table name: %w", err)
	}

	// Replace only PrepareTables the staging side, so the target dataset may
	// not exist yet. BigQuery's CREATE OR REPLACE TABLE and Copy jobs do NOT
	// auto-create the dataset — they fail with "Not found: Dataset ...".
	if err := d.ensureDatasetExists(ctx, targetProject, targetDataset); err != nil {
		return fmt.Errorf("failed to ensure target dataset exists: %w", err)
	}

	// BigQuery can't ALTER partitioning/clustering, so a target whose spec differs
	// must be recreated; detect once here (safe: replace overwrites the target anyway).
	targetRef := d.client.DatasetInProject(targetProject, targetDataset).Table(targetTableName)
	// Resolve the clustering ingestr would apply — including the default PK
	// clustering — so the mismatch check, guard, and CTAS all agree with the table.
	clusterBy := d.effectiveClusterBy(opts)
	mismatch := false
	var targetExpiration time.Time
	if d.partitionBy != "" || len(clusterBy) > 0 {
		if meta, err := targetRef.Metadata(ctx); err == nil {
			mismatch = d.partitionOrClusterMismatch(meta, clusterBy)
			targetExpiration = meta.ExpirationTime
			if mismatch {
				if err := d.recreateSpecGuard(meta, targetTable, clusterBy); err != nil {
					return err
				}
			}
		} else if !isNotFoundError(err) {
			return fmt.Errorf("failed to check target table metadata: %w", err)
		}
	}

	stagingRef := d.client.DatasetInProject(stagingProject, stagingDataset).Table(stagingTableName)

	config.Debug("[DEST] Swapping tables: %s → %s", stagingTable, targetTable)

	// Copy jobs can't dedup.
	if d.effectiveLoadMethod() == loadMethodLoadJob && len(opts.PrimaryKeys) == 0 {
		doCopy := func() error {
			return d.swapTableWithCopyJob(ctx, stagingProject, stagingDataset, stagingTableName, targetProject, targetDataset, targetTableName)
		}
		var swapErr error
		if mismatch {
			// Partition/cluster changed: rename the target aside, copy into a fresh
			// target with the new spec, then drop the old (restoring it on failure).
			swapErr = d.renameAsideSwap(ctx, targetProject, targetDataset, targetTableName, targetExpiration, doCopy)
		} else {
			swapErr = doCopy()
		}
		if swapErr != nil {
			return swapErr
		}
		config.Debug("[DEST] Copy completed, deleting staging table")
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := stagingRef.Delete(cleanupCtx); err != nil {
			config.Debug("[DEST] Failed to delete staging table: %v", err)
		}
		return nil
	}

	// CTAS path. CREATE OR REPLACE can't change an existing table's spec, so on a
	// mismatch rename the target aside, CTAS into a fresh target, drop the old.
	ctas := func() error {
		return d.runCTASSwap(ctx, opts, clusterBy, stagingProject, stagingDataset, stagingTableName, targetProject, targetDataset, targetTableName)
	}
	var swapErr error
	if mismatch {
		swapErr = d.renameAsideSwap(ctx, targetProject, targetDataset, targetTableName, targetExpiration, ctas)
	} else {
		swapErr = ctas()
	}
	if swapErr != nil {
		return swapErr
	}

	config.Debug("[DEST] Copy completed, deleting staging table")
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := stagingRef.Delete(cleanupCtx); err != nil {
		config.Debug("[DEST] Failed to delete staging table: %v", err)
	}

	return nil
}

// repartitionAsideSuffix returns a unique suffix for moving a table aside.
func repartitionAsideSuffix() string {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// renameAsideSwap repartitions the target: rename it aside, run swap (recreating a
// fresh target), then drop the old — or rename it back (with its expiration) on failure.
func (d *BigQueryDestination) renameAsideSwap(ctx context.Context, project, dataset, target string, originalExpiration time.Time, swap func() error) error {
	oldName, err := d.renameTargetAside(ctx, project, dataset, target)
	if err != nil {
		return fmt.Errorf("failed to rename target aside for repartitioning: %w", err)
	}

	if swapErr := swap(); swapErr != nil {
		// The swap may have failed because ctx was canceled (Ctrl-C, timeout);
		// the restore must still run or the target stays aside and self-deletes.
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		// A canceled wait can report failure for a swap that committed server-side;
		// if the new target exists, keep it (don't clobber) and let the aside expire.
		if d.tableExists(restoreCtx, project, dataset, target) {
			return fmt.Errorf("repartition swap reported an error but the new target exists and was kept; aside table %q expires in ~24h: %w", oldName, swapErr)
		}
		if restoreErr := d.restoreTargetFromAside(restoreCtx, project, dataset, oldName, target, originalExpiration); restoreErr != nil {
			return fmt.Errorf("repartition swap failed (%w); restoring aside table %q also failed: %w", swapErr, oldName, restoreErr)
		}
		return swapErr
	}

	// Success: drop the aside table. Best-effort — the expiration set in
	// renameTargetAside cleans it up if this drop never lands.
	oldFQN := fmt.Sprintf("%s.%s.%s", project, dataset, oldName)
	if err := d.DropTable(ctx, oldFQN); err != nil {
		config.Debug("[DEST] failed to drop aside table %s (will expire): %v", oldFQN, err)
	}
	return nil
}

// renameTargetAside renames target → a unique aside name and sets a 24h
// expiration on it so it self-cleans if a later drop never runs.
func (d *BigQueryDestination) renameTargetAside(ctx context.Context, project, dataset, table string) (string, error) {
	// BigQuery can't RENAME a table that has a primary-key constraint; drop it
	// first (it's informational/not-enforced; the recreated target's spec comes
	// from the swap, as in any replace).
	dropPKSQL := fmt.Sprintf("ALTER TABLE %s.%s.%s DROP PRIMARY KEY IF EXISTS",
		quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(table))
	if _, err := d.runQueryJobWithRetry(ctx, dropPKSQL, "drop target primary key"); err != nil {
		return "", fmt.Errorf("failed to drop primary key before renaming %q aside: %w", table, err)
	}
	for range 3 {
		oldName := table + "__ingestr_repartition_" + repartitionAsideSuffix()
		renameSQL := fmt.Sprintf("ALTER TABLE %s.%s.%s RENAME TO %s",
			quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(table), quoteIdentifier(oldName))
		if _, err := d.runQueryJobWithRetry(ctx, renameSQL, "rename target aside"); err != nil {
			// RENAME isn't idempotent: a retried job can fail even though an earlier
			// attempt committed, so probe (detached from ctx) whether it landed.
			probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			landed := d.tableRenameLanded(probeCtx, project, dataset, table, oldName)
			cancel()
			if !landed {
				if isAlreadyExistsError(err) {
					continue // extremely unlikely with a random suffix; try a fresh one
				}
				return "", fmt.Errorf("failed to rename %q aside (if it is missing, it may live under %q — rename it back manually): %w", table, oldName, err)
			}
		}
		// Self-clean safety net if the final drop never runs (e.g. a crash).
		expireSQL := fmt.Sprintf("ALTER TABLE %s.%s.%s SET OPTIONS(expiration_timestamp = TIMESTAMP_ADD(CURRENT_TIMESTAMP(), INTERVAL 24 HOUR))",
			quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(oldName))
		if _, err := d.runQueryJobWithRetry(ctx, expireSQL, "set aside expiration"); err != nil {
			config.Debug("[DEST] failed to set expiration on aside table %s: %v", oldName, err)
		}
		return oldName, nil
	}
	return "", fmt.Errorf("could not rename %q aside after 3 attempts", table)
}

// restoreTargetFromAside restores the aside table's original expiration (fixed
// first, since it survives the rename) and renames it back to target.
func (d *BigQueryDestination) restoreTargetFromAside(ctx context.Context, project, dataset, oldName, table string, originalExpiration time.Time) error {
	expiration := "NULL"
	if !originalExpiration.IsZero() {
		expiration = fmt.Sprintf("TIMESTAMP_MICROS(%d)", originalExpiration.UnixMicro())
	}
	clearSQL := fmt.Sprintf("ALTER TABLE %s.%s.%s SET OPTIONS(expiration_timestamp = %s)",
		quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(oldName), expiration)
	if _, err := d.runQueryJobWithRetry(ctx, clearSQL, "restore aside expiration"); err != nil {
		return fmt.Errorf("failed to restore expiration on aside table %q — it still expires in ~24h; clear its expiration and rename it back to %q manually: %w", oldName, table, err)
	}
	restoreSQL := fmt.Sprintf("ALTER TABLE %s.%s.%s RENAME TO %s",
		quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(oldName), quoteIdentifier(table))
	if _, err := d.runQueryJobWithRetry(ctx, restoreSQL, "restore target from aside"); err != nil {
		if d.tableRenameLanded(ctx, project, dataset, oldName, table) {
			return nil
		}
		return fmt.Errorf("failed to rename aside table %q back to %q — rename it back manually: %w", oldName, table, err)
	}
	return nil
}

// tableRenameLanded reports whether a rename whose job errored actually committed
// server-side (RENAME isn't idempotent, so a retried job can spuriously fail).
func (d *BigQueryDestination) tableRenameLanded(ctx context.Context, project, dataset, from, to string) bool {
	if !d.tableExists(ctx, project, dataset, to) {
		return false
	}
	_, err := d.client.DatasetInProject(project, dataset).Table(from).Metadata(ctx)
	return isNotFoundError(err)
}

func (d *BigQueryDestination) tableExists(ctx context.Context, project, dataset, table string) bool {
	_, err := d.client.DatasetInProject(project, dataset).Table(table).Metadata(ctx)
	return err == nil
}

// runCTASSwap creates/replaces the target from staging via CREATE OR REPLACE … AS
// SELECT, applying partition/clustering. The CTAS swap step (direct or rename-aside).
func (d *BigQueryDestination) runCTASSwap(ctx context.Context, opts destination.SwapOptions, clusterBy []string, stagingProject, stagingDataset, stagingTableName, targetProject, targetDataset, targetTableName string) error {
	stagingFQN := fmt.Sprintf("%s.%s.%s", quoteIdentifier(stagingProject), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTableName))
	selectClause := buildBigQueryDedupSelect(stagingFQN, opts.PrimaryKeys, opts.IncrementalKey)

	if d.partitionBy != "" || len(clusterBy) > 0 {
		// For partitioned/clustered tables, must use SQL to apply partitioning.
		sql := fmt.Sprintf("CREATE OR REPLACE TABLE %s.%s.%s\n", quoteIdentifier(targetProject), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName))
		if d.partitionBy != "" {
			sql += partitionByClause(d.partitionBy, isDatePartitionColumn(opts.Schema, d.partitionBy))
		}
		if len(clusterBy) > 0 {
			clusterCols := make([]string, len(clusterBy))
			for i, col := range clusterBy {
				clusterCols[i] = quoteIdentifier(col)
			}
			sql += fmt.Sprintf("CLUSTER BY %s\n", strings.Join(clusterCols, ", "))
		}
		sql += "AS " + selectClause
		config.Debug("[DEST] Executing SQL copy (partitioned): %s", sql)
		job, err := d.runQueryJobWithRetry(ctx, sql, "SQL copy")
		if err != nil {
			if job == nil {
				return fmt.Errorf("failed to start SQL copy job: %w", err)
			}
			return fmt.Errorf("SQL copy job error (job %s): %w", jobRef(job), err)
		}
	} else {
		// Use SQL CREATE OR REPLACE TABLE AS SELECT * — Copy Jobs don't read
		// from the streaming buffer, so they'd copy 0 rows after Storage Write API writes.
		sql := fmt.Sprintf("CREATE OR REPLACE TABLE %s.%s.%s AS %s",
			quoteIdentifier(targetProject), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName), selectClause)
		config.Debug("[DEST] Executing SQL swap: %s", sql)
		job, err := d.runQueryJobWithRetry(ctx, sql, "SQL swap")
		if err != nil {
			if job == nil {
				return fmt.Errorf("failed to start SQL swap job: %w", err)
			}
			return fmt.Errorf("SQL swap job error (job %s): %w", jobRef(job), err)
		}
	}
	return nil
}

// Exec executes a SQL query.
func (d *BigQueryDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	job, err := d.runQueryJobWithRetry(ctx, sql, "query")
	if err != nil {
		config.LogFailedQuery(sql, err)
		if isBigQueryAlterTypeRewriteCandidate(sql, err) {
			if rewriteErr := d.execAlterColumnTypeWithRewrite(ctx, sql); rewriteErr == nil {
				return nil
			} else {
				return fmt.Errorf("query job failed (job %s): %w (rewrite fallback failed: %v)", jobRef(job), err, rewriteErr)
			}
		}
		if job == nil {
			return fmt.Errorf("failed to run query: %w", err)
		}
		return fmt.Errorf("query job failed (job %s): %w", jobRef(job), err)
	}
	return nil
}

// BigQuery jobs are atomic — failed jobs do not commit partial work — so
// retrying on the reasons checked by isRetryableLoadJobError is safe.
func (d *BigQueryDestination) runQueryJobWithRetry(ctx context.Context, sql, opLabel string) (*bigquery.Job, error) {
	return d.runQueryJobWithRetryAttempts(ctx, sql, opLabel, queryJobMaxAttempts)
}

func (d *BigQueryDestination) runTransactionScriptWithRetry(ctx context.Context, sql, opLabel string) (*bigquery.Job, error) {
	return d.runQueryJobWithRetryAttempts(ctx, sql, opLabel, deleteInsertTransactionMaxAttempts)
}

func (d *BigQueryDestination) runQueryJobWithRetryAttempts(ctx context.Context, sql, opLabel string, maxAttempts int) (*bigquery.Job, error) {
	if maxAttempts <= 0 {
		maxAttempts = queryJobMaxAttempts
	}

	var (
		lastJob *bigquery.Job
		lastErr error
	)
	baseJobID := newBigQueryQueryJobID()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		annotatedSQL := annotation.Prepend(ctx, sql)
		jobID := loadJobAttemptID(baseJobID, attempt)
		job, err := d.startQueryJobWithRetry(ctx, annotatedSQL, nil, opLabel, jobID)
		if err != nil {
			return nil, err
		}
		lastJob = job

		status, err := d.waitForBigQueryJob(ctx, job)
		if err != nil {
			return job, err
		}
		if err := status.Err(); err != nil {
			if attempt < maxAttempts && isRetryableLoadJobError(err) {
				lastErr = err
				config.Debug("[%s] Retrying after job error: %v", opLabel, err)
				if sleepErr := sleepWithContextForLoadJob(ctx, retryDelayForQueryJob(attempt, err)); sleepErr != nil {
					return job, sleepErr
				}
				continue
			}
			return job, err
		}

		return job, nil
	}

	// Unreachable in practice: the final attempt's branches all return
	// unconditionally. Preserved as a defensive return that surfaces the last
	// retried error if the loop is ever restructured.
	if lastErr != nil {
		return lastJob, lastErr
	}
	return lastJob, fmt.Errorf("%s job exhausted retries", opLabel)
}

func (d *BigQueryDestination) startQueryJobWithRetry(ctx context.Context, sql string, parameters []bigquery.QueryParameter, opLabel, jobID string) (*bigquery.Job, error) {
	if err := d.beginCDCJob(ctx, jobID); err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		query := d.client.Query(sql)
		query.JobID = jobID
		query.ProjectID = d.projectID
		query.Parameters = parameters
		if d.location != "" {
			query.Location = d.location
		}
		job, err := query.Run(ctx)
		if err == nil {
			return job, nil
		}
		if ctx.Err() != nil {
			return d.reconcileAmbiguousBigQueryJob(ctx, jobID)
		}
		retry := isRetryableLoadJobError(err)
		if isBigQueryDuplicateJobError(err) {
			recovered, recoverErr := d.recoverDuplicateQueryJob(ctx, jobID, sql)
			if recoverErr == nil {
				config.Debug("[%s] Recovered duplicate job insert as existing job %s", opLabel, jobRef(recovered))
				return recovered, nil
			}
			err = fmt.Errorf("failed to recover duplicate %s job %s: %w", opLabel, jobID, recoverErr)
			retry = true
		}
		if !retry {
			_ = d.resolveCDCJob(context.Background(), jobID)
			return nil, err
		}
		config.Debug("[%s] Retrying ambiguous start with stable job ID %s: %v", opLabel, jobID, err)
		if sleepErr := sleepWithContextForLoadJob(ctx, retryDelayForQueryJob(min(attempt, queryJobMaxAttempts), err)); sleepErr != nil {
			return d.reconcileAmbiguousBigQueryJob(ctx, jobID)
		}
	}
}

func (d *BigQueryDestination) recoverDuplicateQueryJob(ctx context.Context, jobID, sql string) (*bigquery.Job, error) {
	job, err := d.client.JobFromProject(ctx, d.projectID, jobID, d.location)
	if err != nil {
		return nil, err
	}
	if err := validateRecoveredQueryJob(job, sql); err != nil {
		return nil, err
	}
	return job, nil
}

func validateRecoveredQueryJob(job *bigquery.Job, sql string) error {
	cfg, err := job.Config()
	if err != nil {
		return err
	}
	queryCfg, ok := cfg.(*bigquery.QueryConfig)
	if !ok {
		return fmt.Errorf("existing job is %T, not a query job", cfg)
	}
	if !queryJobSQLMatches(queryCfg.Q, sql) {
		return fmt.Errorf(
			"existing job SQL does not match retried query (existing=%q expected=%q)",
			queryJobSQLSnippet(queryCfg.Q),
			queryJobSQLSnippet(sql),
		)
	}
	return nil
}

func queryJobSQLMatches(existing, expected string) bool {
	if existing == expected {
		return true
	}
	return stripLeadingQueryAnnotation(existing) == stripLeadingQueryAnnotation(expected)
}

func stripLeadingQueryAnnotation(sql string) string {
	trimmed := strings.TrimLeft(sql, " \t\r\n")
	if !strings.HasPrefix(trimmed, "-- @bruin.config: ") {
		return sql
	}
	_, rest, ok := strings.Cut(trimmed, "\n")
	if !ok {
		return sql
	}
	return rest
}

func queryJobSQLSnippet(sql string) string {
	const limit = 256
	runes := []rune(strings.TrimSpace(sql))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func newBigQueryQueryJobID() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return fmt.Sprintf("ingestr_%d", time.Now().UnixNano())
	}
	return "ingestr_" + hex.EncodeToString(b[:])
}

func isBigQueryDuplicateJobError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr != nil {
		if apiErr.Code != http.StatusConflict {
			return false
		}
		msg := strings.ToLower(apiErr.Message)
		if strings.Contains(msg, "already exists: job") {
			return true
		}
		for _, item := range apiErr.Errors {
			if strings.EqualFold(item.Reason, "duplicate") && strings.Contains(strings.ToLower(item.Message), "job") {
				return true
			}
		}
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "error 409") &&
		strings.Contains(msg, "already exists: job") &&
		strings.Contains(msg, "duplicate")
}

func isBigQueryAlterTypeRewriteCandidate(sql string, err error) bool {
	if err == nil {
		return false
	}
	if _, _, ok := parseAlterColumnTypesSQL(sql); !ok {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "alter table alter column set data type requires") ||
		strings.Contains(msg, "assignable to the new type")
}

type alterTypeChange struct {
	column  string
	newType string
}

// parseAlterColumnTypeSQL parses a single-clause ALTER COLUMN SET DATA TYPE.
func parseAlterColumnTypeSQL(sql string) (table string, column string, newType string, ok bool) {
	table, changes, ok := parseAlterColumnTypesSQL(sql)
	if !ok || len(changes) != 1 {
		return "", "", "", false
	}
	return table, changes[0].column, changes[0].newType, true
}

var (
	alterColumnTypesRe = regexp.MustCompile(`(?is)^ALTER TABLE\s+(.+?)\s+ALTER COLUMN\s+(.+)$`)
	alterClauseSepRe   = regexp.MustCompile(`(?i),\s+ALTER COLUMN\s+`)
	alterClauseRe      = regexp.MustCompile("(?is)^`?([^`\\s]+)`?\\s+SET DATA TYPE\\s+(.+)$")
)

// parseAlterColumnTypesSQL parses an ALTER TABLE statement carrying one or more
// comma-separated "ALTER COLUMN <col> SET DATA TYPE <type>" clauses, so the
// batched form produced by Dialect.BatchAlterColumnTypesSQL can be rewritten as
// a single CREATE OR REPLACE TABLE.
func parseAlterColumnTypesSQL(sql string) (table string, changes []alterTypeChange, ok bool) {
	m := alterColumnTypesRe.FindStringSubmatch(strings.TrimSpace(sql))
	if m == nil {
		return "", nil, false
	}
	for _, clause := range alterClauseSepRe.Split(m[2], -1) {
		c := alterClauseRe.FindStringSubmatch(strings.TrimSpace(clause))
		if c == nil {
			return "", nil, false
		}
		changes = append(changes, alterTypeChange{column: c[1], newType: strings.TrimSpace(c[2])})
	}
	return strings.ReplaceAll(strings.TrimSpace(m[1]), "`", ""), changes, true
}

func (d *BigQueryDestination) execAlterColumnTypeWithRewrite(ctx context.Context, originalSQL string) error {
	tableName, changes, ok := parseAlterColumnTypesSQL(originalSQL)
	if !ok {
		return fmt.Errorf("not an ALTER COLUMN TYPE statement: %s", originalSQL)
	}

	project, dataset, table, err := d.parseTable(tableName)
	if err != nil {
		return err
	}

	tableRef := d.client.DatasetInProject(project, dataset).Table(table)
	meta, err := tableRef.Metadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch table metadata for rewrite: %w", err)
	}

	typeChanges := make(map[string]string, len(changes))
	for _, c := range changes {
		typeChanges[c.column] = c.newType
	}

	rewrittenSQL, err := d.buildBatchAlterColumnTypeRewriteSQL(project, dataset, table, typeChanges, meta)
	if err != nil {
		return err
	}

	config.Debug("[DEST] Rewriting unsupported ALTER COLUMN TYPE with CREATE OR REPLACE TABLE for %s.%s", dataset, table)
	config.Debug("[DEST] Column type change SQL: %s\n", rewrittenSQL)
	job, err := d.runQueryJobWithRetry(ctx, rewrittenSQL, "alter type rewrite")
	if err != nil {
		return fmt.Errorf("rewrite query error (job %s): %w", jobRef(job), err)
	}

	return nil
}

func (d *BigQueryDestination) buildAlterColumnTypeRewriteSQL(
	project string,
	dataset string,
	table string,
	columnName string,
	newType string,
	meta *bigquery.TableMetadata,
) (string, error) {
	return d.buildBatchAlterColumnTypeRewriteSQL(project, dataset, table, map[string]string{columnName: newType}, meta)
}

// buildBatchAlterColumnTypeRewriteSQL rewrites the table once, casting every
// column in typeChanges to its new type in a single CREATE OR REPLACE TABLE.
func (d *BigQueryDestination) buildBatchAlterColumnTypeRewriteSQL(
	project string,
	dataset string,
	table string,
	typeChanges map[string]string,
	meta *bigquery.TableMetadata,
) (string, error) {
	if meta == nil {
		return "", errors.New("table metadata is required")
	}
	if len(typeChanges) == 0 {
		return "", errors.New("no column type changes provided")
	}
	if meta.RangePartitioning != nil {
		return "", errors.New("range-partitioned tables are not supported for type rewrite")
	}
	if meta.TimePartitioning != nil && meta.TimePartitioning.Field == "" {
		return "", errors.New("ingestion-time partitioned tables are not supported for type rewrite")
	}

	selectExprs := make([]string, 0, len(meta.Schema))
	found := make(map[string]bool, len(typeChanges))
	for _, field := range meta.Schema {
		if newType, ok := typeChanges[field.Name]; ok {
			selectExprs = append(selectExprs, fmt.Sprintf("CAST(%s AS %s) AS %s", quoteIdentifier(field.Name), newType, quoteIdentifier(field.Name)))
			found[field.Name] = true
			continue
		}
		selectExprs = append(selectExprs, quoteIdentifier(field.Name))
	}
	for col := range typeChanges {
		if !found[col] {
			return "", fmt.Errorf("column %q not found in table metadata", col)
		}
	}

	var sqlBuilder strings.Builder
	fmt.Fprintf(&sqlBuilder, "CREATE OR REPLACE TABLE %s.%s.%s\n", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(table))
	if meta.TimePartitioning != nil && meta.TimePartitioning.Field != "" {
		sqlBuilder.WriteString(partitionByClause(meta.TimePartitioning.Field, partitionFieldIsDate(meta.Schema, meta.TimePartitioning.Field)))
	}
	if meta.Clustering != nil && len(meta.Clustering.Fields) > 0 {
		clusterCols := make([]string, len(meta.Clustering.Fields))
		for i, field := range meta.Clustering.Fields {
			clusterCols[i] = quoteIdentifier(field)
		}
		fmt.Fprintf(&sqlBuilder, "CLUSTER BY %s\n", strings.Join(clusterCols, ", "))
	}
	fmt.Fprintf(
		&sqlBuilder,
		"AS SELECT %s FROM %s.%s.%s",
		strings.Join(selectExprs, ", "),
		quoteIdentifier(project),
		quoteIdentifier(dataset),
		quoteIdentifier(table),
	)

	return sqlBuilder.String(), nil
}

// MergeTable performs an atomic merge operation using BigQuery's MERGE statement.
// This merges data from stagingTable into targetTable based on primary keys.
func (d *BigQueryDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	ctx = annotation.WithStep(ctx, annotation.StepMerge)
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("merge requires at least one primary key")
	}

	// Staging is always co-located with the target (same project/dataset family),
	// so a single resolved project qualifies both sides.
	project, stagingDataset, stagingTableName, err := d.parseTable(opts.StagingTable)
	if err != nil {
		return fmt.Errorf("invalid staging table name: %w", err)
	}

	_, targetDataset, targetTableName, err := d.parseTable(opts.TargetTable)
	if err != nil {
		return fmt.Errorf("invalid target table name: %w", err)
	}

	// The merge target may have just been created asynchronously by PrepareTable
	// (e.g. the deduplicated replace table). Wait for that creation to finish so
	// the MERGE doesn't 404 on a not-yet-visible table.
	if _, _, _, tableKey, resolveErr := d.resolveTable(opts.TargetTable); resolveErr == nil {
		if pendingErr := d.takePendingTableErr(tableKey); pendingErr != nil {
			if err := <-pendingErr; err != nil {
				return fmt.Errorf("failed to prepare merge target table: %w", err)
			}
		}
	}

	targetMeta, err := d.client.DatasetInProject(project, targetDataset).Table(targetTableName).Metadata(ctx)
	if err != nil {
		config.Debug("[MERGE] Could not fetch target metadata for partition pruning: %v", err)
	}
	// Fetch target and staging table schemas to detect type mismatches
	castMap := d.buildCastMap(ctx, project, targetDataset, targetTableName, stagingDataset, stagingTableName)

	nonNullablePKs := nonNullablePKColumns(targetMeta, opts.Schema, opts.PrimaryKeys)

	pruning := buildMergePartitionPruning(targetMeta, opts.PrimaryKeys)
	pruningSkipReason := ""
	if pruning != nil && hasCastForColumn(castMap, pruning.Column) {
		pruningSkipReason = fmt.Sprintf("partition field %s requires source casting", pruning.Column)
		pruning = nil
	} else if pruning == nil && targetMeta != nil && targetMeta.TimePartitioning != nil && targetMeta.TimePartitioning.Field != "" {
		partitionField := targetMeta.TimePartitioning.Field
		if !containsIdentifier(opts.PrimaryKeys, partitionField) {
			pruningSkipReason = fmt.Sprintf("partition field %s is not part of the merge primary key", partitionField)
		} else {
			pruningSkipReason = fmt.Sprintf("partition field %s has an unsupported type or was not found in schema", partitionField)
		}
	}
	if pruning != nil {
		config.Debug("[MERGE] Applying target partition pruning on %s", pruning.Column)
	} else if pruningSkipReason != "" {
		config.Debug("[MERGE] Skipping target partition pruning: %s", pruningSkipReason)
	}

	// Build MERGE statement
	mergeSQL := d.buildMergeSQLWithPredicate(project, targetDataset, targetTableName, stagingDataset, stagingTableName, opts.PrimaryKeys, opts.Columns, castMap, opts.IncrementalKey, nonNullablePKs, pruning, opts.IncrementalPredicate)

	config.Debug("[MERGE] Executing MERGE statement")
	config.Debug("[MERGE] SQL: %s", mergeSQL)

	job, err := d.runQueryJobWithRetry(ctx, mergeSQL, "MERGE")
	if err != nil {
		config.LogFailedQuery(mergeSQL, err)
		if job == nil {
			return fmt.Errorf("failed to start merge job: %w", err)
		}
		return fmt.Errorf("merge job failed (job %s): %w", jobRef(job), err)
	}

	config.Debug("[MERGE] Merge completed successfully (job %s)", jobRef(job))
	return nil
}

// DeleteInsertTable performs a DELETE + INSERT operation for BigQuery.
func (d *BigQueryDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	ctx = annotation.WithStep(ctx, annotation.StepDeleteInsert)
	project, stagingDataset, stagingTableName, err := d.parseTable(opts.StagingTable)
	if err != nil {
		return fmt.Errorf("invalid staging table name: %w", err)
	}

	_, targetDataset, targetTableName, err := d.parseTable(opts.TargetTable)
	if err != nil {
		return fmt.Errorf("invalid target table name: %w", err)
	}

	deleteSQL, insertSQL := d.buildDeleteInsertStatements(project, stagingDataset, stagingTableName, targetDataset, targetTableName, opts)
	transactionSQL := buildBigQueryTransactionScript(deleteSQL, insertSQL)

	config.Debug("[DELETE+INSERT] Executing transaction")
	config.Debug("[DELETE+INSERT] DELETE: %s", deleteSQL)
	config.Debug("[DELETE+INSERT] INSERT: %s", insertSQL)

	job, err := d.runTransactionScriptWithRetry(ctx, transactionSQL, "DELETE+INSERT")
	if err != nil {
		config.LogFailedQuery(transactionSQL, err)
		if job == nil {
			return fmt.Errorf("failed to start delete+insert transaction job: %w", err)
		}
		return fmt.Errorf("delete+insert transaction job failed (job %s): %w", jobRef(job), err)
	}

	config.Debug("[DELETE+INSERT] Delete+Insert completed successfully (job %s)", jobRef(job))
	return nil
}

func (d *BigQueryDestination) buildDeleteInsertStatements(project, stagingDataset, stagingTableName, targetDataset, targetTableName string, opts destination.DeleteInsertOptions) (string, string) {
	startVal := formatBigQueryValue(opts.IntervalStart, opts.IncrementalKeyType)
	endVal := formatBigQueryValue(opts.IntervalEnd, opts.IncrementalKeyType)

	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s.%s.%s WHERE %s >= %s AND %s <= %s",
		quoteIdentifier(project), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
		quoteIdentifier(opts.IncrementalKey), startVal,
		quoteIdentifier(opts.IncrementalKey), endVal,
	)

	quotedCols := make([]string, len(opts.Columns))
	for i, col := range opts.Columns {
		quotedCols[i] = quoteIdentifier(col)
	}

	selectClause := fmt.Sprintf(
		"SELECT %s FROM %s.%s.%s",
		strings.Join(quotedCols, ", "),
		quoteIdentifier(project), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTableName),
	)
	if len(opts.PrimaryKeys) > 0 {
		pkCols := make([]string, len(opts.PrimaryKeys))
		for i, pk := range opts.PrimaryKeys {
			pkCols[i] = quoteIdentifier(pk)
		}
		selectClause += fmt.Sprintf(" QUALIFY ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC) = 1", strings.Join(pkCols, ", "), quoteIdentifier(opts.IncrementalKey))
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s.%s.%s (%s) %s",
		quoteIdentifier(project), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
		strings.Join(quotedCols, ", "),
		selectClause,
	)

	return deleteSQL, insertSQL
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *BigQueryDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	ctx = annotation.WithStep(ctx, annotation.StepSCD2)
	startOp := time.Now()

	project, stagingDataset, stagingTableName, err := d.parseTable(opts.StagingTable)
	if err != nil {
		return fmt.Errorf("invalid staging table name: %w", err)
	}

	_, targetDataset, targetTableName, err := d.parseTable(opts.TargetTable)
	if err != nil {
		return fmt.Errorf("invalid target table name: %w", err)
	}

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditionsBigQuery(nonPKColumns, "t", "s")
	onConditions := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		onConditions[i] = fmt.Sprintf("(t.%s = s.%s OR (t.%s IS NULL AND s.%s IS NULL))", quoteIdentifier(pk), quoteIdentifier(pk), quoteIdentifier(pk), quoteIdentifier(pk))
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
		quoteIdentifier(project), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
		quoteIdentifier(project), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTableName),
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
			pkColumnsQuoted[i] = quoteIdentifier(pk)
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
			quoteIdentifier(project), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
			tsLiteral,
			quoteIdentifier(project), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTableName),
			onClause,
		)
		config.Debug("[BIGQUERY SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if err := d.Exec(ctx, softDeleteSQL); err != nil {
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := make([]string, len(allColumns))
	for i, col := range allColumns {
		quotedColumns[i] = quoteIdentifier(col)
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
		quoteIdentifier(project), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quoteIdentifier(project), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTableName),
		quoteIdentifier(project), quoteIdentifier(targetDataset), quoteIdentifier(targetTableName),
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
	return fmt.Sprintf("`%s`", strings.ReplaceAll(s, "`", "``"))
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
func (d *BigQueryDestination) buildCastMap(ctx context.Context, project, targetDataset, targetTable, stagingDataset, stagingTable string) map[string]string {
	targetMeta, err := d.client.DatasetInProject(project, targetDataset).Table(targetTable).Metadata(ctx)
	if err != nil {
		return nil
	}
	stagingMeta, err := d.client.DatasetInProject(project, stagingDataset).Table(stagingTable).Metadata(ctx)
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
			return fmt.Sprintf("CAST(s.%s AS %s)", quoteIdentifier(col), targetType)
		}
	}
	return fmt.Sprintf("s.%s", quoteIdentifier(col))
}

func buildBigQueryDedupSelect(qualifiedTable string, primaryKeys []string, orderByCol string) string {
	if len(primaryKeys) == 0 {
		return fmt.Sprintf("SELECT * FROM %s", qualifiedTable)
	}
	pkCols := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		pkCols[i] = quoteIdentifier(pk)
	}
	orderClause := ""
	if orderByCol != "" {
		orderClause = fmt.Sprintf(" ORDER BY %s DESC", quoteIdentifier(orderByCol))
	}
	return fmt.Sprintf(
		"SELECT * FROM %s QUALIFY ROW_NUMBER() OVER (PARTITION BY %s%s) = 1",
		qualifiedTable, strings.Join(pkCols, ", "), orderClause,
	)
}

// buildMergeSQL constructs a BigQuery MERGE statement
func (d *BigQueryDestination) buildMergeSQL(project, targetDataset, targetTable, stagingDataset, stagingTable string, primaryKeys, allColumns []string, castMap map[string]string, incrementalKey string) string {
	return d.buildMergeSQLWithPartitionPruning(project, targetDataset, targetTable, stagingDataset, stagingTable, primaryKeys, allColumns, castMap, incrementalKey, nil, nil)
}

func (d *BigQueryDestination) buildMergeSQLWithPartitionPruning(project, targetDataset, targetTable, stagingDataset, stagingTable string, primaryKeys, allColumns []string, castMap map[string]string, incrementalKey string, nonNullablePKs map[string]bool, pruning *mergePartitionPruning) string {
	return d.buildMergeSQLWithPredicate(project, targetDataset, targetTable, stagingDataset, stagingTable, primaryKeys, allColumns, castMap, incrementalKey, nonNullablePKs, pruning, "")
}

func (d *BigQueryDestination) buildMergeSQLWithPredicate(project, targetDataset, targetTable, stagingDataset, stagingTable string, primaryKeys, allColumns []string, castMap map[string]string, incrementalKey string, nonNullablePKs map[string]bool, pruning *mergePartitionPruning, incrementalPredicate string) string {
	destColumns := destination.DestinationColumns(allColumns)
	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		sourceCol := castSourceCol(pk, castMap)
		// The null-safe OR disables clustered block pruning, so use bare
		// equality when either join side is provably never NULL.
		if nonNullablePKs[strings.ToLower(pk)] {
			onConditions[i] = fmt.Sprintf("t.%s = %s", quoteIdentifier(pk), sourceCol)
		} else {
			onConditions[i] = fmt.Sprintf("(t.%s = %s OR (t.%s IS NULL AND %s IS NULL))", quoteIdentifier(pk), sourceCol, quoteIdentifier(pk), sourceCol)
		}
	}
	if partitionPredicate := mergePartitionTargetPredicate(pruning); partitionPredicate != "" {
		onConditions = append(onConditions, partitionPredicate)
	}
	if incrementalPredicate = strings.TrimSpace(incrementalPredicate); incrementalPredicate != "" {
		onConditions = append(onConditions, "("+incrementalPredicate+")")
	}
	onClause := strings.Join(onConditions, " AND ")

	// Build UPDATE SET clause (all non-PK columns)
	pkMap := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	// Check if this is CDC mode (has _cdc_deleted column)
	hasCDCDeleted := destination.HasCDCDeletedColumn(allColumns)
	// _cdc_unchanged_cols is only emitted by sources that can mark columns as
	// unchanged (e.g. Postgres TOAST); other CDC sources materialize full rows
	// and their staging tables have no such column to reference.
	hasUnchangedCols := slices.Contains(allColumns, destination.CDCUnchangedColsColumn)

	unchangedRef := fmt.Sprintf("s.%s", quoteIdentifier(destination.CDCUnchangedColsColumn))
	var updateSets []string
	for _, col := range destColumns {
		if !pkMap[strings.ToLower(col)] {
			src := castSourceCol(col, castMap)
			if hasCDCDeleted && hasUnchangedCols && !destination.IsCDCMetaColumn(col) {
				updateSets = append(updateSets, cdcMergeAssign(
					col, fmt.Sprintf("t.%s", quoteIdentifier(col)), src, unchangedRef,
				))
			} else {
				updateSets = append(updateSets, fmt.Sprintf("t.%s = %s", quoteIdentifier(col), src))
			}
		}
	}

	// Build INSERT columns and values
	quotedCols := make([]string, len(destColumns))
	sourceCols := make([]string, len(destColumns))
	for i, col := range destColumns {
		quotedCols[i] = quoteIdentifier(col)
		sourceCols[i] = castSourceCol(col, castMap)
	}

	var sql strings.Builder
	stagingRef := fmt.Sprintf("%s.%s.%s", quoteIdentifier(project), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTable))
	sql.WriteString(mergePartitionPruningDeclarations(stagingRef, pruning))
	fmt.Fprintf(&sql, "MERGE %s.%s.%s AS t\n", quoteIdentifier(project), quoteIdentifier(targetDataset), quoteIdentifier(targetTable))

	if hasCDCDeleted && len(primaryKeys) > 0 {
		// CDC mode: compose the merge source from two per-PK dedups of staging:
		// data columns come from the latest non-deleted change (so a trailing
		// delete doesn't discard the last update's values), while the CDC
		// columns and deleted flag come from the latest change overall. This
		// also materializes rows that were inserted and deleted within one sync
		// window as soft-deleted rows, storing the delete's LSN for resume.
		pkPartition := make([]string, len(primaryKeys))
		laActJoin := make([]string, len(primaryKeys))
		for i, pk := range primaryKeys {
			quoted := quoteIdentifier(pk)
			pkPartition[i] = quoted
			laActJoin[i] = fmt.Sprintf("(la.%s = act.%s OR (la.%s IS NULL AND act.%s IS NULL))", quoted, quoted, quoted, quoted)
		}

		selectCols := make([]string, 0, len(allColumns)+1)
		for _, col := range allColumns {
			alias := "act"
			if pkMap[strings.ToLower(col)] || destination.IsCDCColumn(col) {
				alias = "la"
			}
			selectCols = append(selectCols, fmt.Sprintf("%s.%s", alias, quoteIdentifier(col)))
		}
		selectCols = append(selectCols, "act.`_cdc_lsn` IS NOT NULL AS `__ingestr_has_active`")

		fmt.Fprintf(
			&sql,
			"USING (SELECT %s FROM (SELECT * FROM %s QUALIFY ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) = 1) AS la LEFT JOIN (SELECT * FROM %s WHERE `_cdc_deleted` = false QUALIFY ROW_NUMBER() OVER (PARTITION BY %s ORDER BY `_cdc_lsn` DESC) = 1) AS act ON %s) AS s\n",
			strings.Join(selectCols, ", "),
			stagingRef,
			strings.Join(pkPartition, ", "),
			destination.CDCLatestOverallOrderBy(quoteIdentifier),
			stagingRef,
			strings.Join(pkPartition, ", "),
			strings.Join(laActJoin, " AND "),
		)
	} else {
		pkPartition := make([]string, len(primaryKeys))
		for i, pk := range primaryKeys {
			pkPartition[i] = quoteIdentifier(pk)
		}

		// When an incremental key is set the latest row per PK wins; otherwise
		// the winner is arbitrary.
		dedupOrderBy := ""
		if incrementalKey != "" {
			dedupOrderBy = fmt.Sprintf(" ORDER BY %s DESC", quoteIdentifier(incrementalKey))
		}
		fmt.Fprintf(
			&sql,
			"USING (SELECT * FROM %s.%s.%s QUALIFY ROW_NUMBER() OVER (PARTITION BY %s%s) = 1) AS s\n",
			quoteIdentifier(project), quoteIdentifier(stagingDataset), quoteIdentifier(stagingTable), strings.Join(pkPartition, ", "), dedupOrderBy,
		)
	}

	fmt.Fprintf(&sql, "ON %s\n", onClause)

	if hasCDCDeleted {
		newerLSN := "(t.`_cdc_lsn` IS NULL OR s.`_cdc_lsn` > t.`_cdc_lsn`)"
		// Full update whenever the window has a non-deleted change carrying row
		// data; for deleted PKs this applies the last active values together
		// with the delete marking. Clause order matters: BigQuery executes the
		// first matching WHEN clause.
		if len(updateSets) > 0 {
			fmt.Fprintf(&sql, "WHEN MATCHED AND %s AND (s.`_cdc_deleted` = false OR s.`__ingestr_has_active`) THEN\n", newerLSN)
			fmt.Fprintf(&sql, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}

		// Delete-only window for an existing row: update CDC columns and keep
		// the row data as-is (the delete change carries no usable row image for
		// all sources).
		fmt.Fprintf(&sql, "WHEN MATCHED AND %s AND s.`_cdc_deleted` = true THEN\n", newerLSN)
		sql.WriteString("  UPDATE SET t.`_cdc_deleted` = true, t.`_cdc_lsn` = s.`_cdc_lsn`, t.`_cdc_synced_at` = s.`_cdc_synced_at`\n")

		// Insert new rows, including ones already deleted within the window
		// (materialized as soft-deleted). A delete-only window for an unknown
		// row has no data to materialize and is skipped.
		sql.WriteString("WHEN NOT MATCHED AND (s.`_cdc_deleted` = false OR s.`__ingestr_has_active`) THEN\n")
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

func buildBigQueryTransactionScript(statements ...string) string {
	var sql strings.Builder
	sql.WriteString("BEGIN TRANSACTION;\n")
	for _, statement := range statements {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		sql.WriteString(statement)
		if !strings.HasSuffix(statement, ";") {
			sql.WriteString(";")
		}
		sql.WriteString("\n")
	}
	sql.WriteString("COMMIT TRANSACTION;")
	return sql.String()
}

// BeginTransaction begins a BigQuery multi-statement transaction.
func (d *BigQueryDestination) BeginTransaction(_ context.Context) (destination.Transaction, error) {
	return &bigQueryTransaction{destination: d}, nil
}

type bigQueryTransaction struct {
	destination *BigQueryDestination
	statements  []string
	closed      bool
}

func (t *bigQueryTransaction) Exec(_ context.Context, sql string, args ...interface{}) error {
	if t.closed {
		return errors.New("transaction is already closed")
	}
	if len(args) > 0 {
		return errors.New("BigQuery transaction does not support positional query args")
	}
	t.statements = append(t.statements, sql)
	return nil
}

func (t *bigQueryTransaction) Commit(ctx context.Context) error {
	if t.closed {
		return errors.New("transaction is already closed")
	}
	t.closed = true
	if len(t.statements) == 0 {
		return nil
	}

	transactionSQL := buildBigQueryTransactionScript(t.statements...)
	job, err := t.destination.runTransactionScriptWithRetry(ctx, transactionSQL, "transaction")
	if err != nil {
		config.LogFailedQuery(transactionSQL, err)
		if job == nil {
			return fmt.Errorf("failed to start transaction job: %w", err)
		}
		return fmt.Errorf("transaction job failed (job %s): %w", jobRef(job), err)
	}
	return nil
}

func (t *bigQueryTransaction) Rollback(_ context.Context) error {
	t.closed = true
	t.statements = nil
	return nil
}

// DropTable drops a table if it exists.
func (d *BigQueryDestination) DropTable(ctx context.Context, table string) error {
	project, dataset, tableName, err := d.parseTable(table)
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

	tableRef := d.client.DatasetInProject(project, dataset).Table(tableName)
	if err := tableRef.Delete(ctx); err != nil && !isNotFoundError(err) {
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[DEST] Dropped table: %s", table)
	return nil
}

// TruncateTable empties a table while preserving its definition and dependents.
func (d *BigQueryDestination) TruncateTable(ctx context.Context, table string) error {
	ctx = annotation.WithStep(ctx, annotation.StepTruncate)
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}

	if dataset == "" && d.datasetID != "" {
		dataset = d.datasetID
	}
	if dataset == "" {
		return errors.New("dataset must be specified in table name (dataset.table) or URI path")
	}

	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(tableName))
	job, err := d.runQueryJobWithRetry(ctx, truncateSQL, "truncate")
	if err != nil {
		return fmt.Errorf("failed to truncate table %s (job %s): %w", table, jobRef(job), err)
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

// SupportsIncrementalPredicate returns true as BigQuery appends
// MergeOptions.IncrementalPredicate to the MERGE join condition.
func (d *BigQueryDestination) SupportsIncrementalPredicate() bool { return true }

// SupportsDeleteInsertStrategy returns true as BigQuery supports the delete+insert strategy.
func (d *BigQueryDestination) SupportsDeleteInsertStrategy() bool { return true }

// SupportsSCD2Strategy returns true as BigQuery supports the SCD2 strategy.
func (d *BigQueryDestination) SupportsSCD2Strategy() bool { return true }

// SupportsAtomicSwap returns true as BigQuery supports atomic table swaps.
func (d *BigQueryDestination) SupportsAtomicSwap() bool { return true }

func (d *BigQueryDestination) GetScheme() string { return "bigquery" }

func (d *BigQueryDestination) ManagedCDCStateCatalog() string { return d.projectID }

func (d *BigQueryDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return nil, err
	}

	if dataset == "" && d.datasetID != "" {
		dataset = d.datasetID
	}

	if dataset == "" {
		return nil, errors.New("dataset must be specified in table name (dataset.table) or URI path")
	}

	tableRef := d.client.DatasetInProject(project, dataset).Table(tableName)

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
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
		return true
	}
	// Check for various "not found" error messages
	errStr := err.Error()
	return contains(errStr, "not found") || contains(errStr, "Not found") || contains(errStr, "NOT_FOUND")
}

func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		if apiErr.Code != http.StatusConflict {
			return false
		}
		for _, item := range apiErr.Errors {
			if strings.EqualFold(item.Reason, "duplicate") || strings.EqualFold(item.Reason, "alreadyExists") {
				return true
			}
		}
		return contains(apiErr.Message, "Already Exists") || contains(apiErr.Message, "already exists") || contains(apiErr.Message, "ALREADY_EXISTS")
	}
	errStr := err.Error()
	return contains(errStr, "Already Exists") || contains(errStr, "already exists") || contains(errStr, "ALREADY_EXISTS")
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

// DestTableName maps a multi-table source name like "dbo.orders" to a valid
// BigQuery "dataset.table" name: BigQuery table IDs cannot contain dots, so
// the source's schema qualifier is folded into the table name. The dataset is
// the configured dest_schema, falling back to the dataset from the URI.
func (d *BigQueryDestination) DestTableName(destSchema, sourceTable string) string {
	dataset := destSchema
	if dataset == "" {
		dataset = d.datasetID
	}
	table := strings.ReplaceAll(sourceTable, ".", "_")
	if dataset == "" {
		return table
	}
	return dataset + "." + table
}

func (d *BigQueryDestination) SupportsCDCUnchangedCols() bool { return true }

func (d *BigQueryDestination) SupportsCDCMerge() bool {
	return true
}

func (d *BigQueryDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return "", err
	}

	if dataset == "" && d.datasetID != "" {
		dataset = d.datasetID
	}

	if dataset == "" {
		return "", errors.New("dataset must be specified in table name (dataset.table) or URI path")
	}

	ctx = annotation.WithStep(ctx, annotation.StepCDCResume)
	sql := fmt.Sprintf("SELECT MAX(`_cdc_lsn`) FROM %s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(tableName))
	query := d.client.Query(annotation.Prepend(ctx, sql))
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

type bigQueryCDCStateRow struct {
	EventID          string
	Version          string
	ConnectorID      string
	SourceTable      string
	DestinationTable string
	StateKind        string
	StateGeneration  int64
	StateStatus      string
	CDCLSN           string
	RecordedAt       time.Time
}

func (r *bigQueryCDCStateRow) Save() (map[string]bigquery.Value, string, error) {
	return map[string]bigquery.Value{
		"event_id":          r.EventID,
		"state_version":     r.Version,
		"connector_id":      r.ConnectorID,
		"source_table":      r.SourceTable,
		"destination_table": r.DestinationTable,
		"state_kind":        r.StateKind,
		"state_generation":  r.StateGeneration,
		"state_status":      r.StateStatus,
		"_cdc_lsn":          r.CDCLSN,
		"recorded_at":       r.RecordedAt,
	}, r.EventID, nil
}

func (d *BigQueryDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	project, dataset, tableName, err := d.parseTable(opts.Table)
	if err != nil {
		return err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	inserter := d.client.DatasetInProject(project, dataset).Table(tableName).Inserter()
	for result := range records {
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			return result.Err
		}
		if result.Batch == nil {
			continue
		}
		record := result.Batch
		rows := make([]*bigQueryCDCStateRow, 0, record.NumRows())
		for i := 0; i < int(record.NumRows()); i++ {
			row := &bigQueryCDCStateRow{
				EventID:          record.Column(0).(*array.String).Value(i),
				Version:          record.Column(1).(*array.String).Value(i),
				ConnectorID:      record.Column(2).(*array.String).Value(i),
				SourceTable:      record.Column(3).(*array.String).Value(i),
				DestinationTable: record.Column(4).(*array.String).Value(i),
				StateKind:        record.Column(5).(*array.String).Value(i),
				StateGeneration:  record.Column(6).(*array.Int64).Value(i),
				StateStatus:      record.Column(7).(*array.String).Value(i),
				CDCLSN:           record.Column(8).(*array.String).Value(i),
				RecordedAt:       record.Column(9).(*array.Timestamp).Value(i).ToTime(record.Column(9).DataType().(*arrow.TimestampType).Unit),
			}
			rows = append(rows, row)
			d.cdcStateMu.Lock()
			d.cdcStateTable = opts.Table
			d.cdcStateConnectorID = row.ConnectorID
			d.cdcStateMu.Unlock()
		}
		err := inserter.Put(ctx, rows)
		record.Release()
		if err != nil {
			return fmt.Errorf("failed to stream BigQuery CDC state: %w", err)
		}
	}
	return nil
}

func (d *BigQueryDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	project, dataset, tableName, err := d.parseTable(claimTable)
	if err != nil {
		return err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	targetProject, targetDataset, targetTable, err := d.parseTable(claim.DestinationTable)
	if err != nil {
		return err
	}
	if targetDataset == "" {
		targetDataset = d.datasetID
	}
	canonicalTarget, err := d.canonicalCDCTarget(ctx, targetProject, targetDataset, targetTable)
	if err != nil {
		return err
	}
	quotedClaimTable := fmt.Sprintf("%s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(tableName))
	sql := fmt.Sprintf(`BEGIN TRANSACTION;
INSERT INTO %s (destination_table, connector_id, claimed_at)
SELECT @destination_table, @connector_id, CURRENT_TIMESTAMP()
FROM (SELECT 1 AS singleton)
WHERE NOT EXISTS (SELECT 1 FROM %s WHERE destination_table = @destination_table);
ASSERT (SELECT connector_id FROM %s WHERE destination_table = @destination_table) = @connector_id AS 'CDC destination target is already claimed by another connector';
COMMIT TRANSACTION;`, quotedClaimTable, quotedClaimTable, quotedClaimTable)
	parameters := []bigquery.QueryParameter{
		{Name: "destination_table", Value: canonicalTarget},
		{Name: "connector_id", Value: ownerID},
	}
	annotatedSQL := annotation.Prepend(ctx, sql)
	for attempt := 1; attempt <= deleteInsertTransactionMaxAttempts; attempt++ {
		jobID := loadJobAttemptID(newBigQueryQueryJobID(), attempt)
		job, err := d.startQueryJobWithRetry(ctx, annotatedSQL, parameters, "CDC target claim", jobID)
		if err != nil {
			return err
		}
		status, err := d.waitForBigQueryJob(ctx, job)
		if err != nil {
			return err
		}
		if err := status.Err(); err != nil {
			if attempt < deleteInsertTransactionMaxAttempts && isRetryableLoadJobError(err) {
				if err := sleepWithContextForLoadJob(ctx, retryDelayForQueryJob(attempt, err)); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("CDC destination target %q is already claimed by another connector: %w", canonicalTarget, err)
		}
		return nil
	}
	return fmt.Errorf("CDC destination target %q claim exhausted retries", canonicalTarget)
}

func (d *BigQueryDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return "", false, err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	metadata, err := d.client.DatasetInProject(project, dataset).Table(tableName).Metadata(ctx)
	if err != nil {
		if isNotFoundError(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read BigQuery CDC target incarnation for %s: %w", table, err)
	}
	if metadata.CreationTime.IsZero() {
		return "", false, fmt.Errorf("BigQuery table %s returned an empty creation time", table)
	}
	return bigQueryTableIncarnation(project, dataset, tableName, metadata.CreationTime), true, nil
}

func bigQueryTableIncarnation(project, dataset, table string, creationTime time.Time) string {
	return destination.CDCTargetKey(project, dataset, table, strconv.FormatInt(creationTime.UnixNano(), 10))
}

func (d *BigQueryDestination) canonicalCDCTarget(ctx context.Context, project, dataset, table string) (string, error) {
	key := project + "." + dataset
	d.datasetCaseMu.Lock()
	datasetCase, cached := d.datasetCase[key]
	d.datasetCaseMu.Unlock()
	if !cached {
		metadata, err := d.client.DatasetInProject(project, dataset).Metadata(ctx)
		provisional := false
		if err != nil {
			if !isNotFoundError(err) {
				return "", fmt.Errorf("failed to read BigQuery dataset metadata for CDC target %s: %w", key, err)
			}
			// A first load may resolve its connector identity before PrepareTable
			// creates the dataset. New datasets use BigQuery's case-sensitive default.
			metadata = &bigquery.DatasetMetadata{}
			provisional = true
		}
		datasetCase = bigQueryDatasetCase{caseInsensitive: metadata.IsCaseInsensitive, provisional: provisional}
		d.cacheDatasetCase(key, datasetCase.caseInsensitive, datasetCase.provisional)
	}
	if datasetCase.caseInsensitive {
		dataset = strings.ToLower(dataset)
		table = strings.ToLower(table)
	}
	return destination.CDCTargetKey(project, dataset, table), nil
}

func (d *BigQueryDestination) CanonicalCDCTarget(ctx context.Context, table string) (string, error) {
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return "", err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	return d.canonicalCDCTarget(ctx, project, dataset, tableName)
}

func (d *BigQueryDestination) cdcJobFence() (string, string, map[string]struct{}) {
	d.cdcStateMu.Lock()
	defer d.cdcStateMu.Unlock()
	active := make(map[string]struct{}, len(d.activeCDCJobs))
	for jobID := range d.activeCDCJobs {
		active[jobID] = struct{}{}
	}
	return d.cdcStateTable, d.cdcStateConnectorID, active
}

func (d *BigQueryDestination) beginCDCJob(ctx context.Context, jobID string) error {
	table, connectorID, _ := d.cdcJobFence()
	if table == "" || connectorID == "" {
		return nil
	}
	d.cdcStateMu.Lock()
	if d.activeCDCJobs == nil {
		d.activeCDCJobs = make(map[string]struct{})
	}
	d.activeCDCJobs[jobID] = struct{}{}
	d.cdcStateMu.Unlock()
	rollbackActive := true
	defer func() {
		if rollbackActive {
			d.cdcStateMu.Lock()
			delete(d.activeCDCJobs, jobID)
			d.cdcStateMu.Unlock()
		}
	}()
	if err := d.ensureCDCJobsReconciled(ctx, table, connectorID); err != nil {
		return err
	}
	d.maybeCleanupCDCJobMarkers(ctx, table, connectorID)
	if err := d.writeCDCJobMarker(ctx, table, connectorID, jobID, "pending"); err != nil {
		return err
	}
	rollbackActive = false
	return nil
}

func (d *BigQueryDestination) resolveCDCJob(ctx context.Context, jobID string) error {
	table, connectorID, _ := d.cdcJobFence()
	if table == "" || connectorID == "" {
		return nil
	}
	if err := d.writeCDCJobMarker(ctx, table, connectorID, jobID, "resolved"); err != nil {
		return err
	}
	d.cdcStateMu.Lock()
	delete(d.activeCDCJobs, jobID)
	d.cdcStateMu.Unlock()
	return nil
}

func (d *BigQueryDestination) writeCDCJobMarker(ctx context.Context, table, connectorID, jobID, status string) error {
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	row := &bigQueryCDCStateRow{
		EventID: "job-" + jobID + "-" + status, Version: "v2", ConnectorID: connectorID,
		StateKind: "job", StateStatus: status, CDCLSN: jobID, RecordedAt: time.Now().UTC(),
	}
	if err := d.client.DatasetInProject(project, dataset).Table(tableName).Inserter().Put(ctx, row); err != nil {
		return fmt.Errorf("failed to persist BigQuery job fence %s: %w", jobID, err)
	}
	return nil
}

func (d *BigQueryDestination) ensureCDCJobsReconciled(ctx context.Context, table, connectorID string) error {
	d.cdcJobReconcileMu.Lock()
	defer d.cdcJobReconcileMu.Unlock()
	if d.cdcJobsReconciled {
		return nil
	}
	if err := d.reconcilePendingCDCJobs(ctx, table, connectorID); err != nil {
		return err
	}
	d.cdcJobsReconciled = true
	return nil
}

func (d *BigQueryDestination) reconcilePendingCDCJobs(ctx context.Context, table, connectorID string) error {
	entries, err := d.loadCDCJobMarkers(ctx, table, connectorID)
	if err != nil {
		return fmt.Errorf("failed to load BigQuery job fences: %w", err)
	}
	_, _, active := d.cdcJobFence()
	pending := reduceCDCJobMarkers(entries)
	for jobID, unresolved := range pending {
		if !unresolved {
			continue
		}
		if _, ok := active[jobID]; ok {
			continue
		}
		if _, err := d.reconcileAmbiguousBigQueryJob(ctx, jobID); err != nil {
			return fmt.Errorf("unresolved predecessor BigQuery job %s blocks CDC takeover: %w", jobID, err)
		}
	}
	return nil
}

func (d *BigQueryDestination) maybeCleanupCDCJobMarkers(ctx context.Context, table, connectorID string) {
	d.cdcJobCleanupMu.Lock()
	defer d.cdcJobCleanupMu.Unlock()
	if !d.lastCDCJobCleanup.IsZero() && time.Since(d.lastCDCJobCleanup) < bigQueryCDCStateRetryDelay {
		return
	}
	d.lastCDCJobCleanup = time.Now()
	entries, err := d.loadCDCJobMarkers(ctx, table, connectorID)
	if err != nil {
		config.Debug("[BIGQUERY] Failed to load resolved CDC job markers for cleanup: %v", err)
		return
	}
	jobIDs := resolvedCDCJobIDs(entries, time.Now().Add(-bigQueryCDCStateMinAge))
	if err := d.deleteCDCJobMarkersUntracked(ctx, table, connectorID, jobIDs); err != nil {
		config.Debug("[BIGQUERY] Deferred cleanup of %d aged CDC job markers: %v", len(jobIDs), err)
	}
}

func resolvedCDCJobIDs(entries []destination.CDCStateEntry, cutoff time.Time) []string {
	jobIDs := make(map[string]struct{})
	for _, entry := range entries {
		if entry.StateKind == "job" && entry.Status == "resolved" && !entry.RecordedAt.After(cutoff) {
			jobIDs[entry.Position] = struct{}{}
		}
	}
	result := make([]string, 0, len(jobIDs))
	for jobID := range jobIDs {
		result = append(result, jobID)
	}
	slices.Sort(result)
	return result
}

func reduceCDCJobMarkers(entries []destination.CDCStateEntry) map[string]bool {
	pending := make(map[string]bool)
	for _, entry := range entries {
		if entry.StateKind != "job" {
			continue
		}
		if entry.Status == "resolved" {
			pending[entry.Position] = false
		} else if _, exists := pending[entry.Position]; !exists {
			pending[entry.Position] = true
		}
	}
	return pending
}

func (d *BigQueryDestination) loadCDCJobMarkers(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return nil, err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	quotedTable := fmt.Sprintf("%s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(tableName))
	query := d.client.Query(fmt.Sprintf("SELECT `state_status`, `_cdc_lsn`, `recorded_at` FROM %s WHERE `connector_id` = @connector_id AND `state_kind` = 'job'", quotedTable))
	query.Parameters = []bigquery.QueryParameter{{Name: "connector_id", Value: connectorID}}
	query.Location = d.location
	it, err := query.Read(ctx)
	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []destination.CDCStateEntry
	for {
		var row []bigquery.Value
		if err := it.Next(&row); err != nil {
			if err == iterator.Done {
				return entries, nil
			}
			return nil, err
		}
		if len(row) != 3 {
			return nil, fmt.Errorf("unexpected BigQuery CDC job marker row width %d", len(row))
		}
		status, statusOK := row[0].(string)
		jobID, jobOK := row[1].(string)
		recordedAt, recordedAtOK := row[2].(time.Time)
		if !statusOK || !jobOK || !recordedAtOK {
			return nil, fmt.Errorf("unexpected BigQuery CDC job marker row types")
		}
		entries = append(entries, destination.CDCStateEntry{StateKind: "job", Status: status, Position: jobID, RecordedAt: recordedAt})
	}
}

func (d *BigQueryDestination) deleteCDCJobMarkersUntracked(ctx context.Context, table, connectorID string, jobIDs []string) error {
	if len(jobIDs) == 0 {
		return nil
	}
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	eventIDs := make([]string, 0, len(jobIDs)*2)
	for _, jobID := range jobIDs {
		eventIDs = append(eventIDs, "job-"+jobID+"-pending", "job-"+jobID+"-resolved")
	}
	quotedTable := fmt.Sprintf("%s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(tableName))
	query := d.client.Query(fmt.Sprintf("DELETE FROM %s WHERE `connector_id` = @connector_id AND `event_id` IN UNNEST(@event_ids)", quotedTable))
	query.Parameters = []bigquery.QueryParameter{{Name: "connector_id", Value: connectorID}, {Name: "event_ids", Value: eventIDs}}
	query.Location = d.location
	job, err := query.Run(ctx)
	if err != nil {
		return err
	}
	status, err := waitForBigQueryJob(ctx, job)
	if err != nil {
		return err
	}
	return status.Err()
}

func (d *BigQueryDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return nil, err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	if dataset == "" {
		return nil, errors.New("dataset must be specified in state table name or URI path")
	}

	ctx = annotation.WithStep(ctx, annotation.StepCDCResume)
	sql := fmt.Sprintf("SELECT `event_id`, `source_table`, `destination_table`, `state_kind`, `state_generation`, `state_status`, `_cdc_lsn` FROM %s.%s.%s WHERE `connector_id` = @connector_id",
		quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(tableName))
	query := d.client.Query(annotation.Prepend(ctx, sql))
	query.Parameters = []bigquery.QueryParameter{{Name: "connector_id", Value: connectorID}}
	if d.location != "" {
		query.Location = d.location
	}
	it, err := query.Read(ctx)
	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []destination.CDCStateEntry
	for {
		var row []bigquery.Value
		if err := it.Next(&row); err != nil {
			if err == iterator.Done {
				break
			}
			return nil, err
		}
		if len(row) != 7 {
			return nil, fmt.Errorf("unexpected BigQuery CDC state row width %d", len(row))
		}
		eventID, eventOK := row[0].(string)
		sourceTable, sourceOK := row[1].(string)
		destinationTable, destinationOK := row[2].(string)
		kind, kindOK := row[3].(string)
		generation, generationOK := row[4].(int64)
		status, statusOK := row[5].(string)
		position, positionOK := row[6].(string)
		if !eventOK || !sourceOK || !destinationOK || !kindOK || !generationOK || !statusOK || !positionOK {
			return nil, fmt.Errorf("unexpected BigQuery CDC state row types")
		}
		entries = append(entries, destination.CDCStateEntry{
			EventID:          eventID,
			SourceTable:      sourceTable,
			DestinationTable: destinationTable,
			StateKind:        kind,
			Generation:       generation,
			Status:           status,
			Position:         position,
		})
	}
	return entries, nil
}

// EnsureCDCStatePositionColumn widens a `_cdc_lsn STRING(64)` column left by
// older releases to unbounded STRING. PrepareTable's schema reconciliation
// rejects a bounded column against the now-unbounded state schema, so this
// runs before retrying the preparation.
func (d *BigQueryDestination) EnsureCDCStatePositionColumn(ctx context.Context, table string) error {
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	if dataset == "" {
		return errors.New("dataset must be specified in state table name or URI path")
	}
	meta, err := d.client.DatasetInProject(project, dataset).Table(tableName).Metadata(ctx)
	if err != nil {
		if isNotFoundError(err) {
			return nil
		}
		return err
	}
	bounded := false
	for _, field := range meta.Schema {
		if field.Name == destination.CDCLSNColumn {
			bounded = field.MaxLength > 0
			break
		}
	}
	if !bounded {
		return nil
	}
	quotedTable := fmt.Sprintf("%s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(tableName))
	sql := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN `_cdc_lsn` SET DATA TYPE STRING", quotedTable)
	query := d.client.Query(sql)
	if d.location != "" {
		query.Location = d.location
	}
	job, err := query.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to widen BigQuery CDC state position column: %w", err)
	}
	status, err := waitForBigQueryJob(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to widen BigQuery CDC state position column: %w", err)
	}
	if err := status.Err(); err != nil {
		return fmt.Errorf("failed to widen BigQuery CDC state position column: %w", err)
	}
	return nil
}

func (d *BigQueryDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	if dataset == "" {
		return destination.CDCStateFence{}, errors.New("dataset must be specified in state table name or URI path")
	}

	ctx = annotation.WithStep(ctx, annotation.StepCDCResume)
	quotedTable := fmt.Sprintf("%s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(tableName))
	sql := buildCDCStateFenceQuery(quotedTable)
	query := d.client.Query(annotation.Prepend(ctx, sql))
	query.Parameters = []bigquery.QueryParameter{{Name: "connector_id", Value: connectorID}}
	if d.location != "" {
		query.Location = d.location
	}
	it, err := query.Read(ctx)
	if err != nil {
		if isNotFoundError(err) {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, err
	}

	var fence destination.CDCStateFence
	for {
		var row []bigquery.Value
		if err := it.Next(&row); err != nil {
			if err == iterator.Done {
				break
			}
			return destination.CDCStateFence{}, err
		}
		if len(row) != 2 {
			return destination.CDCStateFence{}, fmt.Errorf("unexpected BigQuery CDC fence row width %d", len(row))
		}
		eventID, eventOK := row[0].(string)
		generation, generationOK := row[1].(int64)
		if !eventOK || !generationOK {
			return destination.CDCStateFence{}, fmt.Errorf("unexpected BigQuery CDC fence row types")
		}
		fence.Generation = generation
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	return fence, nil
}

func buildCDCStateFenceQuery(quotedTable string) string {
	return fmt.Sprintf("SELECT DISTINCT `event_id`, `state_generation` FROM %s WHERE `connector_id` = @connector_id AND `state_kind` = 'run' AND `state_generation` = (SELECT MAX(`state_generation`) FROM %s WHERE `connector_id` = @connector_id AND `state_kind` = 'run') ORDER BY `event_id`", quotedTable, quotedTable)
}

func (d *BigQueryDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	project, dataset, tableName, err := d.parseTable(table)
	if err != nil {
		return err
	}
	if dataset == "" {
		dataset = d.datasetID
	}
	if dataset == "" {
		return errors.New("dataset must be specified in state table name or URI path")
	}
	d.cdcStatePruneMu.Lock()
	defer d.cdcStatePruneMu.Unlock()
	if time.Now().Before(d.nextCDCStatePrune) {
		return fmt.Errorf("BigQuery CDC state pruning is deferred until %s", d.nextCDCStatePrune.UTC().Format(time.RFC3339))
	}

	ctx = annotation.WithStep(ctx, annotation.StepCDCResume)
	quotedTable := fmt.Sprintf("%s.%s.%s", quoteIdentifier(project), quoteIdentifier(dataset), quoteIdentifier(tableName))
	parameters := []bigquery.QueryParameter{
		{Name: "connector_id", Value: connectorID},
		{Name: "event_ids", Value: eventIDs},
	}
	ageQuery := d.client.Query(fmt.Sprintf("SELECT COUNTIF(`recorded_at` > TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 45 MINUTE)) FROM %s WHERE `connector_id` = @connector_id AND `event_id` IN UNNEST(@event_ids)", quotedTable))
	ageQuery.Parameters = parameters
	ageQuery.Location = d.location
	it, err := ageQuery.Read(ctx)
	if err != nil {
		d.nextCDCStatePrune = time.Now().Add(bigQueryCDCStateRetryDelay)
		return err
	}
	var row []bigquery.Value
	if err := it.Next(&row); err != nil {
		d.nextCDCStatePrune = time.Now().Add(bigQueryCDCStateRetryDelay)
		return err
	}
	if len(row) != 1 {
		return fmt.Errorf("unexpected BigQuery CDC state age row width %d", len(row))
	}
	young, ok := row[0].(int64)
	if !ok {
		return fmt.Errorf("unexpected BigQuery CDC state age type %T", row[0])
	}
	if young > 0 {
		d.nextCDCStatePrune = time.Now().Add(bigQueryCDCStateRetryDelay)
		return fmt.Errorf("BigQuery CDC state pruning deferred for %d rows still in the streaming-buffer safety window", young)
	}
	deleteQuery := d.client.Query(annotation.Prepend(ctx, fmt.Sprintf("DELETE FROM %s WHERE `connector_id` = @connector_id AND `event_id` IN UNNEST(@event_ids)", quotedTable)))
	deleteQuery.Parameters = parameters
	deleteQuery.Location = d.location
	job, err := deleteQuery.Run(ctx)
	if err != nil {
		d.nextCDCStatePrune = time.Now().Add(bigQueryCDCStateRetryDelay)
		return err
	}
	status, err := waitForBigQueryJob(ctx, job)
	if err != nil {
		d.nextCDCStatePrune = time.Now().Add(bigQueryCDCStateRetryDelay)
		return err
	}
	if err := status.Err(); err != nil {
		d.nextCDCStatePrune = time.Now().Add(bigQueryCDCStateRetryDelay)
		return err
	}
	d.nextCDCStatePrune = time.Time{}
	return nil
}

func (d *BigQueryDestination) CDCStatePruneBatchSize() int { return 10_000 }

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func cdcMergeAssign(col, targetExpr, sourceExpr, unchangedColsExpr string) string {
	// The source emits _cdc_unchanged_cols with source-side column names; the
	// merge column may carry different casing (e.g. when the schema is read
	// back from an existing destination table), so compare case-insensitively.
	colLit := strings.ReplaceAll(strings.ToLower(col), "'", "''")
	return fmt.Sprintf(
		"t.`%s` = IF('%s' IN UNNEST(IFNULL(JSON_EXTRACT_STRING_ARRAY(LOWER(%s)), [])), %s, %s)",
		col, colLit, unchangedColsExpr, targetExpr, sourceExpr,
	)
}
