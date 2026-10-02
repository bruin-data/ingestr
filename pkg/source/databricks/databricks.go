package databricks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/databricks/databricks-sdk-go"
	dbsql "github.com/databricks/databricks-sdk-go/service/sql"
)

const (
	defaultCatalog       = "main"
	defaultSchema        = "default"
	defaultBatchSize     = 100000
	statementTimeout     = "50s"
	externalLinkAttempts = 3
)

var externalLinkRetryDelay = 2 * time.Second

// A chunk download fails only when no bytes arrive for this long, so slow
// connections can still finish while hung ones fail fast.
var externalLinkStallTimeout = 30 * time.Second

type stallReader struct {
	r     io.Reader
	timer *time.Timer
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(externalLinkStallTimeout)
	}
	return n, err
}

type DatabricksSource struct {
	client     *databricks.WorkspaceClient
	host       string
	token      string
	httpPath   string
	catalog    string
	schemaName string
}

func NewDatabricksSource() *DatabricksSource {
	return &DatabricksSource{}
}

func (s *DatabricksSource) Schemes() []string {
	return []string{"databricks"}
}

func (s *DatabricksSource) Connect(ctx context.Context, uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid Databricks URI: %w", err)
	}

	s.host = u.Hostname()
	if s.host == "" {
		return errors.New("databricks URI must include host")
	}

	if u.User != nil {
		if u.User.Username() == "token" {
			s.token, _ = u.User.Password()
		} else {
			s.token = u.User.Username()
		}
	}

	if s.token == "" {
		return errors.New("databricks URI must include access token (databricks://token:<token>@host)")
	}

	query := u.Query()
	s.httpPath = query.Get("http_path")
	if s.httpPath == "" {
		return errors.New("databricks URI must include http_path query parameter for SQL warehouse")
	}

	s.catalog = query.Get("catalog")
	if s.catalog == "" {
		s.catalog = defaultCatalog
	}

	s.schemaName = query.Get("schema")
	if s.schemaName == "" {
		s.schemaName = defaultSchema
	}

	client, err := databricks.NewWorkspaceClient(&databricks.Config{
		Host:  "https://" + s.host,
		Token: s.token,
	})
	if err != nil {
		return fmt.Errorf("failed to create Databricks client: %w", err)
	}

	s.client = client
	config.Debug("[DATABRICKS] Connected to %s, catalog=%s, schema=%s", s.host, s.catalog, s.schemaName)
	return nil
}

func (s *DatabricksSource) Close(ctx context.Context) error {
	config.Debug("[DATABRICKS] Closed connection")
	return nil
}

func (s *DatabricksSource) HandlesIncrementality() bool {
	return false
}

func (s *DatabricksSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	tableName := req.Name
	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    pks,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, tableSchema, opts)
		},
	}, nil
}

func (s *DatabricksSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := s.parseTableName(table)
	fullTable := fmt.Sprintf("%s.%s.%s", quoteIdentifier(s.catalog), quoteIdentifier(schemaName), quoteIdentifier(tableName))

	query := fmt.Sprintf("DESCRIBE TABLE %s", fullTable)

	config.Debug("[DATABRICKS] Fetching schema: %s", query)

	warehouseID := s.extractWarehouseID()
	resp, err := s.client.StatementExecution.ExecuteAndWait(ctx, dbsql.ExecuteStatementRequest{
		WarehouseId: warehouseID,
		Statement:   query,
		WaitTimeout: statementTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	if resp.Status != nil && resp.Status.State == dbsql.StatementStateFailed {
		errMsg := ""
		if resp.Status.Error != nil {
			errMsg = resp.Status.Error.Message
		}
		return nil, fmt.Errorf("schema query failed: %s", errMsg)
	}

	var columns []schema.Column
	if resp.Result != nil && resp.Result.DataArray != nil {
		for _, row := range resp.Result.DataArray {
			if len(row) < 2 {
				continue
			}

			colName := strings.TrimSpace(row[0])
			dataType := strings.TrimSpace(row[1])

			if colName == "" || strings.HasPrefix(colName, "#") {
				continue
			}

			dt, precision, scale, arrayType := MapDatabricksToDataType(dataType)

			col := schema.Column{
				Name:      colName,
				DataType:  dt,
				Nullable:  true,
				Precision: precision,
				Scale:     scale,
				ArrayType: arrayType,
			}
			if dt == schema.TypeString {
				col.MaxLength = source.ParseSizedStringLength(dataType)
			}
			columns = append(columns, col)
		}
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s.%s.%s not found or has no columns", s.catalog, schemaName, tableName)
	}

	return &schema.TableSchema{
		Name:    tableName,
		Schema:  schemaName,
		Columns: columns,
	}, nil
}

func (s *DatabricksSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	schemaName, tableName := s.parseTableName(table)
	fullTable := fmt.Sprintf("%s.%s.%s", quoteIdentifier(s.catalog), quoteIdentifier(schemaName), quoteIdentifier(tableName))

	columns := tableSchema.Columns
	if len(opts.ExcludeColumns) > 0 {
		columns = filterColumns(columns, opts.ExcludeColumns)
	}

	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = quoteIdentifier(col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), fullTable)

	if opts.IncrementalKey != "" {
		whereClause := buildIncrementalWhere(opts.IncrementalKey, opts.IntervalStart, opts.IntervalEnd)
		if whereClause != "" {
			query += " WHERE " + whereClause
		}
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	config.Debug("[DATABRICKS] Executing query: %s", query)

	results := make(chan source.RecordBatchResult)
	arrowSchema := buildArrowSchema(columns)

	go func() {
		defer close(results)

		warehouseID := s.extractWarehouseID()
		resp, err := s.client.StatementExecution.ExecuteAndWait(ctx, dbsql.ExecuteStatementRequest{
			WarehouseId: warehouseID,
			Statement:   query,
			WaitTimeout: statementTimeout,
			Disposition: dbsql.DispositionExternalLinks,
			Format:      dbsql.FormatArrowStream,
		})
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("query execution failed: %w", err)}
			return
		}

		if resp.Status != nil && resp.Status.State == dbsql.StatementStateFailed {
			errMsg := ""
			if resp.Status.Error != nil {
				errMsg = resp.Status.Error.Message
			}
			results <- source.RecordBatchResult{Err: fmt.Errorf("query failed: %s", errMsg)}
			return
		}

		if resp.Result == nil || len(resp.Result.ExternalLinks) == 0 {
			config.Debug("[DATABRICKS] Query returned no results")
			return
		}

		s.processResults(ctx, resp, arrowSchema, opts.MaxBatchBytes, results)
	}()

	return results, nil
}

func downloadExternalLink(ctx context.Context, link dbsql.ExternalLink) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < externalLinkAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * externalLinkRetryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		data, retryable, err := downloadExternalLinkOnce(ctx, link)
		if err == nil {
			return data, nil
		}
		if !retryable || ctx.Err() != nil {
			return nil, err
		}
		config.Debug("[DATABRICKS] Chunk %d download attempt %d failed: %v", link.ChunkIndex, attempt+1, err)
		lastErr = err
	}
	return nil, lastErr
}

func downloadExternalLinkOnce(ctx context.Context, link dbsql.ExternalLink) ([]byte, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	timer := time.AfterFunc(externalLinkStallTimeout, func() {
		stalled.Store(true)
		cancel()
	})
	defer timer.Stop()

	wrap := func(msg string, err error) error {
		if stalled.Load() {
			return fmt.Errorf("%s: no data received for %s", msg, externalLinkStallTimeout)
		}
		return fmt.Errorf("%s: %w", msg, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.ExternalLink, nil)
	if err != nil {
		return nil, false, errors.New("failed to create external link request")
	}
	for k, v := range link.HttpHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// The presigned URL carries a temporary credential, so drop it from the error.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, true, wrap("failed to download external link", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError
		return nil, retryable, fmt.Errorf("external link returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(&stallReader{r: resp.Body, timer: timer})
	if err != nil {
		return nil, true, wrap("failed to read external link", err)
	}
	return data, false, nil
}

func (s *DatabricksSource) streamExternalLink(ctx context.Context, alloc memory.Allocator, link dbsql.ExternalLink, arrowSchema *arrow.Schema, maxBatchBytes int64, results chan<- source.RecordBatchResult) error {
	data, err := downloadExternalLink(ctx, link)
	if err != nil {
		return err
	}

	rdr, err := ipc.NewReader(bytes.NewReader(data), ipc.WithAllocator(alloc))
	if err != nil {
		return fmt.Errorf("failed to read arrow stream: %w", err)
	}
	defer rdr.Release()

	for rdr.Next() {
		rec, err := conformRecord(ctx, alloc, rdr.RecordBatch(), arrowSchema)
		if err != nil {
			return fmt.Errorf("failed to convert record batch: %w", err)
		}
		batches := sliceRecord(rec, defaultBatchSize, maxBatchBytes)
		rec.Release()
		for i, batch := range batches {
			select {
			case results <- source.RecordBatchResult{Batch: batch}:
			case <-ctx.Done():
				for _, b := range batches[i:] {
					b.Release()
				}
				return ctx.Err()
			}
		}
	}
	if err := rdr.Err(); err != nil {
		return fmt.Errorf("failed to read arrow stream: %w", err)
	}
	return nil
}

func (s *DatabricksSource) processResults(ctx context.Context, resp *dbsql.StatementResponse, arrowSchema *arrow.Schema, maxBatchBytes int64, results chan<- source.RecordBatchResult) {
	alloc := memory.NewGoAllocator()

	nextChunk := 0
	hasMore := false
	processChunk := func(rd *dbsql.ResultData) error {
		hasMore = rd.NextChunkInternalLink != ""
		for _, link := range rd.ExternalLinks {
			if link.ChunkIndex < nextChunk {
				continue
			}
			config.Debug("[DATABRICKS] Fetching chunk %d: row_count=%d, byte_count=%d", link.ChunkIndex, link.RowCount, link.ByteCount)
			if err := s.streamExternalLink(ctx, alloc, link, arrowSchema, maxBatchBytes, results); err != nil {
				return fmt.Errorf("chunk %d: %w", link.ChunkIndex, err)
			}
			nextChunk = link.ChunkIndex + 1
			hasMore = link.NextChunkInternalLink != ""
		}
		return nil
	}

	if err := processChunk(resp.Result); err != nil {
		results <- source.RecordBatchResult{Err: err}
		return
	}

	totalChunks := 0
	if resp.Manifest != nil {
		totalChunks = resp.Manifest.TotalChunkCount
	}

	for nextChunk < totalChunks || hasMore {
		if ctx.Err() != nil {
			results <- source.RecordBatchResult{Err: ctx.Err()}
			return
		}

		chunkIndex := nextChunk
		chunkResp, err := s.client.StatementExecution.GetStatementResultChunkN(ctx, dbsql.GetStatementResultChunkNRequest{
			StatementId: resp.StatementId,
			ChunkIndex:  chunkIndex,
		})
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get result chunk %d: %w", chunkIndex, err)}
			return
		}
		if err := processChunk(chunkResp); err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		if nextChunk == chunkIndex {
			results <- source.RecordBatchResult{Err: fmt.Errorf("result chunk %d returned no external links", chunkIndex)}
			return
		}
	}
}

func (s *DatabricksSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[DATABRICKS] Executing custom query: %s", query)

		warehouseID := s.extractWarehouseID()
		resp, err := s.client.StatementExecution.ExecuteAndWait(ctx, dbsql.ExecuteStatementRequest{
			WarehouseId: warehouseID,
			Statement:   query,
			WaitTimeout: statementTimeout,
			Disposition: dbsql.DispositionExternalLinks,
			Format:      dbsql.FormatArrowStream,
		})
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("custom query execution failed: %w", err)}
			return
		}

		if resp.Status != nil && resp.Status.State == dbsql.StatementStateFailed {
			errMsg := ""
			if resp.Status.Error != nil {
				errMsg = resp.Status.Error.Message
			}
			results <- source.RecordBatchResult{Err: fmt.Errorf("custom query failed: %s", errMsg)}
			return
		}

		if resp.Result == nil || len(resp.Result.ExternalLinks) == 0 {
			config.Debug("[DATABRICKS] Custom query returned no results")
			return
		}

		var columns []schema.Column
		if resp.Manifest != nil && resp.Manifest.Schema != nil {
			for _, col := range resp.Manifest.Schema.Columns {
				dt, precision, scale, arrayType := MapDatabricksToDataType(col.TypeText)
				columns = append(columns, schema.Column{
					Name:      col.Name,
					DataType:  dt,
					Nullable:  true,
					Precision: precision,
					Scale:     scale,
					ArrayType: arrayType,
				})
			}
		}
		var arrowSchema *arrow.Schema
		if len(columns) > 0 {
			arrowSchema = buildArrowSchema(columns)
		}

		s.processResults(ctx, resp, arrowSchema, opts.MaxBatchBytes, results)
	}()

	return results, nil
}

func (s *DatabricksSource) parseTableName(table string) (schemaName, tableName string) {
	tn, err := tablename.Databricks.Parse(table, tablename.Defaults{Schema: s.schemaName})
	if err != nil {
		return s.schemaName, table
	}
	return tn.Schema, tn.Table
}

func (s *DatabricksSource) extractWarehouseID() string {
	parts := strings.Split(s.httpPath, "/")
	for i, part := range parts {
		if part == "warehouses" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return s.httpPath
}

func filterColumns(columns []schema.Column, exclude []string) []schema.Column {
	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToLower(col)] = true
	}

	var filtered []schema.Column
	for _, col := range columns {
		if !excludeMap[strings.ToLower(col.Name)] {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func buildArrowSchema(columns []schema.Column) *arrow.Schema {
	fields := make([]arrow.Field, len(columns))
	for i, col := range columns {
		fields[i] = arrow.Field{
			Name:     col.Name,
			Type:     schema.DataTypeToArrowType(col),
			Nullable: col.Nullable,
		}
	}
	return arrow.NewSchema(fields, nil)
}

func quoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

func buildIncrementalWhere(key string, start, end interface{}) string {
	var conditions []string

	if start != nil {
		conditions = append(conditions, fmt.Sprintf("%s >= %s", quoteIdentifier(key), formatValue(start)))
	}
	if end != nil {
		conditions = append(conditions, fmt.Sprintf("%s <= %s", quoteIdentifier(key), formatValue(end)))
	}

	return strings.Join(conditions, " AND ")
}

func formatValue(v interface{}) string {
	switch val := v.(type) {
	case time.Time:
		return fmt.Sprintf("TIMESTAMP '%s'", val.Format("2006-01-02 15:04:05.000000"))
	case *time.Time:
		if val == nil {
			return "NULL"
		}
		return fmt.Sprintf("TIMESTAMP '%s'", val.Format("2006-01-02 15:04:05.000000"))
	case string:
		return fmt.Sprintf("'%s'", strings.ReplaceAll(val, "'", "''"))
	case int, int32, int64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("'%v'", val)
	}
}
