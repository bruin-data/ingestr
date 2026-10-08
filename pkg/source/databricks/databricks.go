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
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/databuffer"
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

const externalLinkAttempts = 3

var externalLinkRetryDelay = 2 * time.Second

var errLinkForbidden = errors.New("link expired or forbidden")

// A chunk download fails only when no bytes arrive for this long, so slow
// connections can still finish while hung ones fail fast.
var (
	externalLinkStallTimeout = 30 * time.Second
	externalLinkMaxDuration  = 15 * time.Minute
)

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

		s.processResults(ctx, resp, arrowSchema, opts.MaxBatchBytes, opts.Parallelism, results)
	}()

	return results, nil
}

func fetchExternalLink(parent context.Context, link dbsql.ExternalLink) ([]byte, bool, error) {
	ctx, cancel := context.WithCancel(parent)
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
		if resp.StatusCode == http.StatusForbidden {
			return nil, false, fmt.Errorf("external link returned status %d: %w", resp.StatusCode, errLinkForbidden)
		}
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError
		return nil, retryable, fmt.Errorf("external link returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(&stallReader{r: resp.Body, timer: timer})
	if err != nil {
		return nil, true, wrap("failed to read external link", err)
	}
	return data, false, nil
}

func fetchExternalLinkWithRetry(ctx context.Context, link dbsql.ExternalLink) ([]byte, error) {
	// The overall limit covers all attempts, so retries can't extend it.
	dlCtx, cancel := context.WithTimeout(ctx, externalLinkMaxDuration)
	defer cancel()
	tooSlow := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("failed to download external link: did not finish within %s", externalLinkMaxDuration)
	}

	var lastErr error
	for attempt := 0; attempt < externalLinkAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * externalLinkRetryDelay):
			case <-dlCtx.Done():
				return nil, tooSlow()
			}
		}
		data, retryable, err := fetchExternalLink(dlCtx, link)
		if err == nil {
			return data, nil
		}
		if dlCtx.Err() != nil {
			return nil, tooSlow()
		}
		if !retryable {
			return nil, err
		}
		config.Debug("[DATABRICKS] Chunk %d download attempt %d failed: %v", link.ChunkIndex, attempt+1, err)
		lastErr = err
	}
	return nil, lastErr
}

func conformRecord(alloc memory.Allocator, rec arrow.RecordBatch, target *arrow.Schema, intervals []string) (arrow.RecordBatch, error) {
	rec = formatIntervals(alloc, rec, intervals)
	defer rec.Release()
	if target == nil {
		rec.Retain()
		return rec, nil
	}
	if int(rec.NumCols()) != target.NumFields() {
		return nil, fmt.Errorf("result has %d columns, expected %d", rec.NumCols(), target.NumFields())
	}

	// Cast column by column so columns match by position, not by name:
	// custom queries can return duplicate column names.
	cols := make([]arrow.Array, target.NumFields())
	defer func() {
		for _, c := range cols {
			if c != nil {
				c.Release()
			}
		}
	}()
	for i, field := range target.Fields() {
		col := rec.Column(i)
		srcField := arrow.Field{Name: field.Name, Type: col.DataType(), Nullable: true}
		single := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{srcField}, nil), []arrow.Array{col}, rec.NumRows())
		casted, err := databuffer.CastRecordToSchema(single, arrow.NewSchema([]arrow.Field{field}, nil), true)
		single.Release()
		if err != nil {
			return nil, err
		}
		cols[i] = casted.Column(0)
		cols[i].Retain()
		casted.Release()
	}
	return array.NewRecordBatch(target, cols, rec.NumRows()), nil
}

// downloadChunk fetches a chunk's data, asking Databricks for a fresh link once
// if the current one has expired.
func (s *DatabricksSource) downloadChunk(ctx context.Context, statementID string, link dbsql.ExternalLink) ([]byte, error) {
	data, err := fetchExternalLinkWithRetry(ctx, link)
	if !errors.Is(err, errLinkForbidden) || s.client == nil || statementID == "" {
		return data, err
	}
	config.Debug("[DATABRICKS] Chunk %d link rejected, requesting a fresh one", link.ChunkIndex)
	fresh, ferr := s.client.StatementExecution.GetStatementResultChunkN(ctx, dbsql.GetStatementResultChunkNRequest{
		StatementId: statementID,
		ChunkIndex:  link.ChunkIndex,
	})
	if ferr != nil {
		return nil, fmt.Errorf("failed to refresh external link: %w", ferr)
	}
	for _, l := range fresh.ExternalLinks {
		if l.ChunkIndex == link.ChunkIndex {
			return fetchExternalLinkWithRetry(ctx, l)
		}
	}
	return nil, err
}

func (s *DatabricksSource) streamExternalLink(ctx context.Context, alloc memory.Allocator, data []byte, arrowSchema *arrow.Schema, intervals []string, maxBatchBytes int64, results chan<- source.RecordBatchResult) error {
	rdr, err := ipc.NewReader(bytes.NewReader(data), ipc.WithAllocator(alloc))
	if err != nil {
		return fmt.Errorf("failed to read arrow stream: %w", err)
	}
	defer rdr.Release()

	for rdr.Next() {
		rec, err := conformRecord(alloc, rdr.RecordBatch(), arrowSchema, intervals)
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

type pendingChunk struct {
	done chan struct{}
	data []byte
	err  error
}

// processResults downloads up to parallelism chunks at once while emitting them
// in chunk order. Every queued chunk holds a slot until it has been emitted,
// which bounds memory to that many chunks.
func (s *DatabricksSource) processResults(ctx context.Context, resp *dbsql.StatementResponse, arrowSchema *arrow.Schema, maxBatchBytes int64, parallelism int, results chan<- source.RecordBatchResult) {
	if parallelism <= 0 {
		parallelism = config.DefaultExtractParallelism
	}
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	alloc := memory.NewGoAllocator()
	intervals := intervalQualifiers(resp.Manifest)
	processed := -1
	statementID := resp.StatementId

	slots := make(chan struct{}, parallelism)
	queue := make(chan *pendingChunk, parallelism)
	emitted := make(chan struct{})
	failed := false
	// The first failure, from a download or a link request, cancels the other
	// downloads right away instead of waiting for earlier, slower chunks.
	var firstErr atomic.Pointer[error]
	go func() {
		defer close(emitted)
		for p := range queue {
			if !failed {
				<-p.done
				err := p.err
				if err == nil {
					err = s.streamExternalLink(ctx, alloc, p.data, arrowSchema, intervals, maxBatchBytes, results)
				}
				if first := firstErr.Load(); err != nil && first != nil {
					err = *first
				}
				if err != nil {
					results <- source.RecordBatchResult{Err: err}
					failed = true
					cancel()
				}
			}
			<-slots
		}
	}()
	defer func() {
		close(queue)
		<-emitted
		if failed {
			return
		}
		if first := firstErr.Load(); first != nil {
			results <- source.RecordBatchResult{Err: *first}
		} else if parent.Err() != nil {
			results <- source.RecordBatchResult{Err: parent.Err()}
		}
	}()

	// A slot is taken before asking Databricks for a chunk's link, so links are
	// fetched only when they can be downloaded right away.
	held := false
	acquire := func() bool {
		if held {
			return true
		}
		select {
		case slots <- struct{}{}:
			held = true
			return true
		case <-ctx.Done():
			return false
		}
	}
	fail := func(err error) {
		if firstErr.CompareAndSwap(nil, &err) {
			cancel()
		}
	}

	processResultData := func(rd *dbsql.ResultData) bool {
		for i, link := range rd.ExternalLinks {
			if link.ChunkIndex <= processed {
				continue
			}
			if !acquire() {
				return false
			}
			held = false
			config.Debug("[DATABRICKS] Fetching external link %d/%d: row_count=%d, byte_count=%d", i+1, len(rd.ExternalLinks), link.RowCount, link.ByteCount)
			p := &pendingChunk{done: make(chan struct{})}
			go func(link dbsql.ExternalLink) {
				defer close(p.done)
				p.data, p.err = s.downloadChunk(ctx, statementID, link)
				if p.err != nil {
					p.err = fmt.Errorf("failed to fetch external link: %w", p.err)
					if firstErr.CompareAndSwap(nil, &p.err) {
						cancel()
					}
				}
			}(link)
			queue <- p
			processed = link.ChunkIndex
		}
		return true
	}

	if !processResultData(resp.Result) {
		return
	}

	totalChunks := 0
	if resp.Manifest != nil {
		totalChunks = resp.Manifest.TotalChunkCount
	}

	if totalChunks > 1 {
		config.Debug("[DATABRICKS] Fetching remaining %d chunks by index (total=%d)", totalChunks-1, totalChunks)
		for chunkIndex := 1; chunkIndex < totalChunks; chunkIndex++ {
			if chunkIndex <= processed {
				continue
			}
			if !acquire() {
				return
			}

			config.Debug("[DATABRICKS] Fetching chunk index=%d", chunkIndex)
			chunkResp, err := s.client.StatementExecution.GetStatementResultChunkN(ctx, dbsql.GetStatementResultChunkNRequest{
				StatementId: statementID,
				ChunkIndex:  chunkIndex,
			})
			if err != nil {
				fail(fmt.Errorf("failed to get result chunk: %w", err))
				return
			}
			config.Debug("[DATABRICKS] Chunk %d response: row_count=%d, row_offset=%d, external_links=%d, next_chunk_index=%d, has_next_chunk=%v",
				chunkIndex, chunkResp.RowCount, chunkResp.RowOffset, len(chunkResp.ExternalLinks), chunkResp.NextChunkIndex, chunkResp.NextChunkInternalLink != "")

			if !processResultData(chunkResp) {
				return
			}
		}
		config.Debug("[DATABRICKS] Chunk pagination complete")
	} else if next, ok := nextChunk(resp.Result); ok {
		chunkIndex := next
		config.Debug("[DATABRICKS] Entering link-based chunk pagination: starting_chunk_index=%d", chunkIndex)

		for {
			if !acquire() {
				return
			}

			config.Debug("[DATABRICKS] Fetching chunk index=%d", chunkIndex)
			chunkResp, err := s.client.StatementExecution.GetStatementResultChunkN(ctx, dbsql.GetStatementResultChunkNRequest{
				StatementId: statementID,
				ChunkIndex:  chunkIndex,
			})
			if err != nil {
				fail(fmt.Errorf("failed to get result chunk: %w", err))
				return
			}
			config.Debug("[DATABRICKS] Chunk %d response: row_count=%d, row_offset=%d, external_links=%d, next_chunk_index=%d, has_next_chunk=%v",
				chunkIndex, chunkResp.RowCount, chunkResp.RowOffset, len(chunkResp.ExternalLinks), chunkResp.NextChunkIndex, chunkResp.NextChunkInternalLink != "")

			if !processResultData(chunkResp) {
				return
			}

			next, ok := nextChunk(chunkResp)
			if !ok {
				config.Debug("[DATABRICKS] No more chunks after index=%d", chunkIndex)
				break
			}
			chunkIndex = next
		}
		config.Debug("[DATABRICKS] Link-based chunk pagination complete")
	} else {
		config.Debug("[DATABRICKS] No chunk pagination needed")
	}
}

// nextChunk reports the next chunk to fetch. With external links the
// continuation is carried on the last link rather than on the result itself.
func nextChunk(rd *dbsql.ResultData) (int, bool) {
	if n := len(rd.ExternalLinks); n > 0 && rd.ExternalLinks[n-1].NextChunkInternalLink != "" {
		return rd.ExternalLinks[n-1].NextChunkIndex, true
	}
	return rd.NextChunkIndex, rd.NextChunkInternalLink != ""
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

		if resp.Manifest != nil {
			config.Debug("[DATABRICKS] Manifest: total_row_count=%d, total_chunk_count=%d, total_byte_count=%d, truncated=%v",
				resp.Manifest.TotalRowCount, resp.Manifest.TotalChunkCount, resp.Manifest.TotalByteCount, resp.Manifest.Truncated)
		}
		if resp.Result != nil {
			config.Debug("[DATABRICKS] Initial result: row_count=%d, row_offset=%d, external_links=%d, next_chunk_index=%d, has_next_chunk=%v",
				resp.Result.RowCount, resp.Result.RowOffset, len(resp.Result.ExternalLinks), resp.Result.NextChunkIndex, resp.Result.NextChunkInternalLink != "")
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

		s.processResults(ctx, resp, arrowSchema, opts.MaxBatchBytes, opts.Parallelism, results)
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

func sliceRecord(rec arrow.RecordBatch, batchSize int, maxBatchBytes int64) []arrow.RecordBatch {
	rows := int(rec.NumRows())
	if rows <= batchSize && (maxBatchBytes <= 0 || recordBytes(rec) <= maxBatchBytes) {
		rec.Retain()
		return []arrow.RecordBatch{rec}
	}

	var sizes []int64
	if maxBatchBytes > 0 {
		sizes = rowBytes(rec)
	}
	var out []arrow.RecordBatch
	start := 0
	var accBytes int64
	for i := 0; i < rows; i++ {
		if sizes != nil {
			accBytes += sizes[i]
		}
		if i-start+1 >= batchSize || (sizes != nil && accBytes >= maxBatchBytes) {
			out = append(out, rec.NewSlice(int64(start), int64(i+1)))
			start = i + 1
			accBytes = 0
		}
	}
	if start < rows {
		out = append(out, rec.NewSlice(int64(start), int64(rows)))
	}
	return out
}

// rowBytes estimates each row's size: exact for strings and binary, the type
// width for fixed-width columns, and the column average for nested ones.
func rowBytes(rec arrow.RecordBatch) []int64 {
	sizes := make([]int64, rec.NumRows())
	for _, col := range rec.Columns() {
		addColumnBytes(col, sizes)
	}
	return sizes
}

func addColumnBytes(col arrow.Array, sizes []int64) {
	switch a := col.(type) {
	case array.ExtensionArray:
		addColumnBytes(a.Storage(), sizes)
		return
	case interface{ ValueLen(int) int }:
		for i := range sizes {
			sizes[i] += int64(a.ValueLen(i))
		}
		return
	}
	var width int64
	if fw, ok := col.DataType().(arrow.FixedWidthDataType); ok {
		width = int64(max(fw.BitWidth()/8, 1))
	} else if len(sizes) > 0 {
		width = dataBytes(col.Data()) / int64(len(sizes))
	}
	for i := range sizes {
		sizes[i] += width
	}
}

func recordBytes(rec arrow.RecordBatch) int64 {
	var n int64
	for _, col := range rec.Columns() {
		n += dataBytes(col.Data())
	}
	return n
}

func dataBytes(d arrow.ArrayData) int64 {
	var n int64
	for _, b := range d.Buffers() {
		if b != nil {
			n += int64(b.Len())
		}
	}
	for _, c := range d.Children() {
		n += dataBytes(c)
	}
	return n
}
