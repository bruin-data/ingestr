package databricks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/databricks/databricks-sdk-go"
	dbsql "github.com/databricks/databricks-sdk-go/service/sql"
)

const (
	defaultCatalog     = "main"
	defaultSchema      = "default"
	defaultBatchSize   = 100000
	statementTimeout   = "50s"
	maxRowsPerResponse = 100000
)

// A chunk download fails only when no bytes arrive for this long, so slow
// connections can still finish while hung ones fail fast.
var externalLinkStallTimeout = 30 * time.Second

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

		if resp.Result == nil || (resp.Result.DataArray == nil && len(resp.Result.ExternalLinks) == 0) {
			config.Debug("[DATABRICKS] Query returned no results")
			return
		}

		s.processResults(ctx, resp, arrowSchema, columns, opts.MaxBatchBytes, results)
	}()

	return results, nil
}

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

func fetchExternalLink(ctx context.Context, link dbsql.ExternalLink) ([][]string, error) {
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
		return nil, errors.New("failed to create external link request")
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
		return nil, wrap("failed to download external link", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("external link returned status %d", resp.StatusCode)
	}

	var data [][]string
	if err := json.NewDecoder(&stallReader{r: resp.Body, timer: timer}).Decode(&data); err != nil {
		return nil, wrap("failed to decode external link data", err)
	}
	return data, nil
}

func (s *DatabricksSource) processResults(ctx context.Context, resp *dbsql.StatementResponse, arrowSchema *arrow.Schema, columns []schema.Column, maxBatchBytes int64, results chan<- source.RecordBatchResult) {
	batchSize := defaultBatchSize
	alloc := memory.NewGoAllocator()

	flush := func(chunk [][]string) bool {
		batch, err := s.buildRecordBatch(alloc, arrowSchema, columns, chunk)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to build record batch: %w", err)}
			return false
		}
		results <- source.RecordBatchResult{Batch: batch}
		config.Debug("[DATABRICKS] Sent batch with %d rows", len(chunk))
		return true
	}

	processChunk := func(dataArray [][]string) {
		start := 0
		var accBytes int64
		for i, row := range dataArray {
			if maxBatchBytes > 0 {
				accBytes += arrowconv.CellsBytes(row)
			}
			rows := i - start + 1
			if rows >= batchSize || (maxBatchBytes > 0 && accBytes >= maxBatchBytes) {
				if !flush(dataArray[start : i+1]) {
					return
				}
				start = i + 1
				accBytes = 0
			}
		}
		if start < len(dataArray) {
			flush(dataArray[start:])
		}
	}

	processResultData := func(rd *dbsql.ResultData) error {
		if rd.DataArray != nil {
			config.Debug("[DATABRICKS] Processing inline chunk: data_array_rows=%d", len(rd.DataArray))
			processChunk(rd.DataArray)
		}
		for i, link := range rd.ExternalLinks {
			config.Debug("[DATABRICKS] Fetching external link %d/%d: row_count=%d, byte_count=%d", i+1, len(rd.ExternalLinks), link.RowCount, link.ByteCount)
			data, err := fetchExternalLink(ctx, link)
			if err != nil {
				return fmt.Errorf("failed to fetch external link: %w", err)
			}
			config.Debug("[DATABRICKS] External link fetched: rows=%d", len(data))
			processChunk(data)
		}
		return nil
	}

	if err := processResultData(resp.Result); err != nil {
		results <- source.RecordBatchResult{Err: err}
		return
	}

	statementID := resp.StatementId
	totalChunks := 0
	if resp.Manifest != nil {
		totalChunks = resp.Manifest.TotalChunkCount
	}

	if totalChunks > 1 {
		config.Debug("[DATABRICKS] Fetching remaining %d chunks by index (total=%d)", totalChunks-1, totalChunks)
		for chunkIndex := 1; chunkIndex < totalChunks; chunkIndex++ {
			select {
			case <-ctx.Done():
				results <- source.RecordBatchResult{Err: ctx.Err()}
				return
			default:
			}

			config.Debug("[DATABRICKS] Fetching chunk index=%d", chunkIndex)
			chunkResp, err := s.client.StatementExecution.GetStatementResultChunkN(ctx, dbsql.GetStatementResultChunkNRequest{
				StatementId: statementID,
				ChunkIndex:  chunkIndex,
			})
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get result chunk: %w", err)}
				return
			}
			config.Debug("[DATABRICKS] Chunk %d response: row_count=%d, row_offset=%d, external_links=%d, data_array_rows=%d, next_chunk_index=%d, has_next_chunk=%v",
				chunkIndex, chunkResp.RowCount, chunkResp.RowOffset, len(chunkResp.ExternalLinks), len(chunkResp.DataArray), chunkResp.NextChunkIndex, chunkResp.NextChunkInternalLink != "")

			if err := processResultData(chunkResp); err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
		}
		config.Debug("[DATABRICKS] Chunk pagination complete")
	} else if resp.Result.NextChunkInternalLink != "" {
		chunkIndex := resp.Result.NextChunkIndex
		config.Debug("[DATABRICKS] Entering link-based chunk pagination: starting_chunk_index=%d", chunkIndex)

		for {
			select {
			case <-ctx.Done():
				results <- source.RecordBatchResult{Err: ctx.Err()}
				return
			default:
			}

			config.Debug("[DATABRICKS] Fetching chunk index=%d", chunkIndex)
			chunkResp, err := s.client.StatementExecution.GetStatementResultChunkN(ctx, dbsql.GetStatementResultChunkNRequest{
				StatementId: statementID,
				ChunkIndex:  chunkIndex,
			})
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get result chunk: %w", err)}
				return
			}
			config.Debug("[DATABRICKS] Chunk %d response: row_count=%d, row_offset=%d, external_links=%d, data_array_rows=%d, next_chunk_index=%d, has_next_chunk=%v",
				chunkIndex, chunkResp.RowCount, chunkResp.RowOffset, len(chunkResp.ExternalLinks), len(chunkResp.DataArray), chunkResp.NextChunkIndex, chunkResp.NextChunkInternalLink != "")

			if err := processResultData(chunkResp); err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}

			if chunkResp.NextChunkInternalLink == "" {
				config.Debug("[DATABRICKS] No more chunks after index=%d", chunkIndex)
				break
			}
			chunkIndex = chunkResp.NextChunkIndex
		}
		config.Debug("[DATABRICKS] Link-based chunk pagination complete")
	} else {
		config.Debug("[DATABRICKS] No chunk pagination needed")
	}
}

func (s *DatabricksSource) buildRecordBatch(alloc memory.Allocator, arrowSchema *arrow.Schema, columns []schema.Column, rows [][]string) (arrow.RecordBatch, error) {
	builders := make([]array.Builder, len(columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(alloc, field.Type)
	}

	for _, row := range rows {
		for i, builder := range builders {
			if i >= len(row) {
				builder.AppendNull()
				continue
			}
			val := row[i]
			switch val {
			case "":
				builder.AppendEmptyValue()
			case "null", "NULL":
				builder.AppendNull()
			default:
				arrowconv.AppendValue(builder, val)
			}
		}
	}

	arrays := make([]arrow.Array, len(builders))
	for i, builder := range builders {
		arrays[i] = builder.NewArray()
		builder.Release()
	}

	return array.NewRecordBatch(arrowSchema, arrays, int64(len(rows))), nil
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

		if resp.Manifest != nil {
			config.Debug("[DATABRICKS] Manifest: total_row_count=%d, total_chunk_count=%d, total_byte_count=%d, truncated=%v",
				resp.Manifest.TotalRowCount, resp.Manifest.TotalChunkCount, resp.Manifest.TotalByteCount, resp.Manifest.Truncated)
		}
		if resp.Result != nil {
			config.Debug("[DATABRICKS] Initial result: row_count=%d, row_offset=%d, external_links=%d, data_array_rows=%d, next_chunk_index=%d, has_next_chunk=%v",
				resp.Result.RowCount, resp.Result.RowOffset, len(resp.Result.ExternalLinks), len(resp.Result.DataArray), resp.Result.NextChunkIndex, resp.Result.NextChunkInternalLink != "")
		}

		if resp.Result == nil || (resp.Result.DataArray == nil && len(resp.Result.ExternalLinks) == 0) {
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
		if len(columns) == 0 {
			rowWidth := 0
			if len(resp.Result.DataArray) > 0 {
				rowWidth = len(resp.Result.DataArray[0])
			} else if len(resp.Result.ExternalLinks) > 0 {
				data, err := fetchExternalLink(ctx, resp.Result.ExternalLinks[0])
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("custom query has no manifest and failed to fetch external link for schema inference: %w", err)}
					return
				}
				if len(data) > 0 {
					rowWidth = len(data[0])
				}
			}
			for i := 0; i < rowWidth; i++ {
				columns = append(columns, schema.Column{Name: fmt.Sprintf("col_%d", i), DataType: schema.TypeUnknown, Nullable: true})
			}
		}
		arrowSchema := buildArrowSchema(columns)

		s.processResults(ctx, resp, arrowSchema, columns, opts.MaxBatchBytes, results)
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
