package adbc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/annotation"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

// ADBCSource is a generic ADBC-based source implementation.
// Database-specific behavior is delegated to the Dialect interface.
type ADBCSource struct {
	dialect        Dialect
	db             *sql.DB
	uri            string
	currentDataset string // Track current dataset for DatasetConnector dialects
}

// NewADBCSource creates a new ADBC source with the given dialect.
func NewADBCSource(dialect Dialect) *ADBCSource {
	return &ADBCSource{dialect: dialect}
}

// Schemes returns the URI schemes this source handles.
func (s *ADBCSource) Schemes() []string {
	return s.dialect.Schemes()
}

// Connect establishes a connection to the database.
func (s *ADBCSource) Connect(ctx context.Context, uri string) error {
	// Set URI early so dialects can start native connections in parallel
	// This allows StorageReader implementations to pre-connect while ADBC connects
	if uriAware, ok := s.dialect.(URIAware); ok {
		uriAware.SetURI(uri)
	}

	// 1. Ensure ADBC driver is available
	if err := s.dialect.EnsureDriver(ctx); err != nil {
		return fmt.Errorf("failed to ensure ADBC driver for %s: %w", s.dialect.Name(), err)
	}

	// 2. Build connection string using dialect
	connStr, err := s.dialect.BuildConnectionString(uri)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 3. Open connection via ADBC sqldriver
	db, err := sql.Open(ADBCDriverName, connStr)
	if err != nil {
		return fmt.Errorf("failed to open %s connection: %w", s.dialect.Name(), err)
	}

	// 4. Verify connection
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping %s: %w", s.dialect.Name(), err)
	}

	s.db = db
	s.uri = uri

	// If dialect implements ConnectionAware, provide it with the db connection
	if connAware, ok := s.dialect.(ConnectionAware); ok {
		connAware.SetConnection(db)
	}

	// Note: SetURI is called at the start of Connect() to allow parallel initialization

	config.Debug("[%s] Connected successfully", s.dialect.Name())
	return nil
}

// Close closes the database connection.
func (s *ADBCSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *ADBCSource) HandlesIncrementality() bool {
	return false
}

func (s *ADBCSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		// For custom queries, set a placeholder dataset_id so the ADBC driver can connect.
		// The user's SQL must use fully-qualified table names (dataset.table).
		if dsConnector, ok := s.dialect.(DatasetConnector); ok {
			if err := s.reconnectWithDataset(ctx, dsConnector, "dummy"); err != nil {
				return nil, fmt.Errorf("failed to set dataset for custom query: %w", err)
			}
		}
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	// Use user-provided PKs if available, otherwise use auto-detected
	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}

	// Use user's strategy or default to replace
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

// reconnectWithDataset reconnects to the database with the dataset in the connection string.
// This is needed for databases like BigQuery that require dataset_id at connection level.
func (s *ADBCSource) reconnectWithDataset(ctx context.Context, dsConnector DatasetConnector, dataset string) error {
	// Skip if we're already connected to this dataset
	if s.currentDataset == dataset {
		return nil
	}

	config.Debug("[%s] Reconnecting with dataset: %s", s.dialect.Name(), dataset)

	// Close existing connection
	if s.db != nil {
		_ = s.db.Close()
	}

	// Build new connection string with dataset
	connStr, err := dsConnector.BuildConnectionStringWithDataset(s.uri, dataset)
	if err != nil {
		return fmt.Errorf("failed to build connection string with dataset: %w", err)
	}

	// Open new connection
	db, err := sql.Open(ADBCDriverName, connStr)
	if err != nil {
		return fmt.Errorf("failed to open connection with dataset: %w", err)
	}

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping with dataset: %w", err)
	}

	s.db = db
	s.currentDataset = dataset
	config.Debug("[%s] Reconnected with dataset: %s", s.dialect.Name(), dataset)
	return nil
}

func (s *ADBCSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	// Check if dialect requires special table name validation (e.g., BigQuery)
	if datasetDialect, ok := s.dialect.(DatasetAwareDialect); ok {
		if err := datasetDialect.ValidateTableName(table); err != nil {
			return nil, err
		}
	}

	// Parse table name to extract dataset/schema (catalog-aware when supported)
	_, schemaName, _ := s.parseTableName(table)

	// Check if dialect needs dataset in the connection (e.g., BigQuery)
	// This must happen BEFORE using SchemaProvider or SQL queries
	if dsConnector, ok := s.dialect.(DatasetConnector); ok && schemaName != "" {
		if err := s.reconnectWithDataset(ctx, dsConnector, schemaName); err != nil {
			return nil, fmt.Errorf("failed to reconnect with dataset: %w", err)
		}
	}

	// Check if dialect implements SchemaProvider interface (e.g., BigQuery with native API)
	// This is much faster than SQL-based schema queries
	if schemaProvider, ok := s.dialect.(SchemaProvider); ok {
		config.Debug("[%s] Using native schema provider", s.dialect.Name())
		return schemaProvider.GetSchema(ctx, table)
	}

	// Fallback to SQL-based schema fetching
	config.Debug("[%s] Using SQL-based schema fetching", s.dialect.Name())

	// Re-parse to get catalog/schema/table for the SQL query
	catalog, schemaName, tableName := s.parseTableName(table)

	// Determine the schema query and its parameters. BigQuery-like dialects
	// embed the dataset in the query path (only tableName is a parameter);
	// catalog-aware dialects with a catalog present qualify by catalog.
	var schemaQuery string
	var schemaArgs []any
	switch {
	case isDatasetAware(s.dialect):
		schemaQuery = s.dialect.(DatasetAwareDialect).SchemaQueryForDataset(schemaName)
		schemaArgs = []any{tableName}
	case catalog != "" && isCatalogSQL(s.dialect):
		schemaQuery = s.dialect.(CatalogSQLDialect).SchemaQueryForCatalog()
		schemaArgs = []any{catalog, schemaName, tableName}
	default:
		schemaQuery = s.dialect.SchemaQuery()
		schemaArgs = []any{schemaName, tableName}
	}

	rows, err := s.db.QueryContext(ctx, schemaQuery, schemaArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, isNullable string
		if err := rows.Scan(&columnName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Use dialect's type mapper
		dt, precision, scale, arrayType := s.dialect.MapDataType(dataType)

		columns = append(columns, schema.Column{
			Name:      CopyString(columnName), // ADBC buffer safety
			DataType:  dt,
			Nullable:  isNullable == "YES",
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	// Get primary keys using dialect's SQL template
	primaryKeys := s.fetchPrimaryKeys(ctx, catalog, schemaName, tableName)
	config.Debug("[%s] Detected primary keys: %v", s.dialect.Name(), primaryKeys)

	// Mark primary key columns
	for i := range columns {
		for _, pk := range primaryKeys {
			if columns[i].Name == pk {
				columns[i].IsPrimaryKey = true
				break
			}
		}
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

// parseTableName resolves the table identifier into (catalog, schema, table).
// Catalog is "" for dialects that are not catalog-aware.
func (s *ADBCSource) parseTableName(table string) (catalog, schemaName, tableName string) {
	if ca, ok := s.dialect.(CatalogAwareDialect); ok {
		return ca.ParseTableNameWithCatalog(table)
	}
	schemaName, tableName = s.dialect.ParseTableName(table)
	return "", schemaName, tableName
}

func isDatasetAware(d Dialect) bool {
	_, ok := d.(DatasetAwareDialect)
	return ok
}

func isCatalogSQL(d Dialect) bool {
	_, ok := d.(CatalogSQLDialect)
	return ok
}

// fetchPrimaryKeys executes the PK query and parses results via dialect.
func (s *ADBCSource) fetchPrimaryKeys(ctx context.Context, catalog, schemaName, tableName string) []string {
	var pkSQL string
	var args []any
	switch {
	case isDatasetAware(s.dialect):
		pkSQL = s.dialect.(DatasetAwareDialect).PrimaryKeyQueryForDataset(schemaName)
		args = []any{tableName}
	case catalog != "" && isCatalogSQL(s.dialect):
		pkSQL = s.dialect.(CatalogSQLDialect).PrimaryKeyQueryForCatalog()
		args = []any{catalog, tableName}
	default:
		pkSQL = s.dialect.PrimaryKeyQuery()
		args = []any{tableName}
	}
	if pkSQL == "" {
		return nil
	}

	rows, err := s.db.QueryContext(ctx, pkSQL, args...)
	if err != nil {
		config.Debug("[%s] Failed to query primary keys: %v", s.dialect.Name(), err)
		return nil
	}
	defer func() { _ = rows.Close() }()

	var primaryKeys []string
	for rows.Next() {
		var rawValue interface{}
		if err := rows.Scan(&rawValue); err == nil && rawValue != nil {
			pks := s.dialect.ParsePrimaryKeyResult(CopyValue(rawValue))
			primaryKeys = append(primaryKeys, pks...)
		}
	}

	return primaryKeys
}

func (s *ADBCSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	// Tag the extract query for cost attribution. Snowflake strips leading SQL
	// comments, so it gets a session QUERY_TAG (applied below); every other
	// engine keeps the "-- @bruin.config" comment.
	annotated := annotation.WithStep(ctx, annotation.StepExtract)
	var tagSQL string
	if strings.EqualFold(s.dialect.Name(), "SNOWFLAKE") {
		if tag, ok := annotation.QueryTag(annotated); ok {
			tagSQL = "ALTER SESSION SET QUERY_TAG = '" + strings.ReplaceAll(tag, "'", "''") + "'"
		}
	} else {
		query = annotation.Prepend(annotated, query)
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		// Run on the pool by default. For Snowflake, pin one connection so the
		// QUERY_TAG and the query share a session (both *sql.DB and *sql.Conn
		// satisfy QueryContext).
		var runner interface {
			QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
		} = s.db
		if tagSQL != "" {
			conn, err := s.db.Conn(ctx)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get connection: %w", err)}
				return
			}
			defer func() { _ = conn.Close() }()
			if _, err := conn.ExecContext(ctx, tagSQL); err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to set query tag: %w", err)}
				return
			}
			runner = conn
		}

		config.Debug("[%s] Executing custom query: %s", s.dialect.Name(), query)
		rows, err := runner.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to execute custom query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()

		colTypes, err := rows.ColumnTypes()
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get column types: %w", err)}
			return
		}

		columns := make([]schema.Column, len(colTypes))
		for i, ct := range colTypes {
			dt, precision, scale, arrayType := s.dialect.MapDataType(ct.DatabaseTypeName())
			nullable, _ := ct.Nullable()
			columns[i] = schema.Column{
				Name:      ct.Name(),
				DataType:  dt,
				Nullable:  nullable,
				Precision: precision,
				Scale:     scale,
				ArrayType: arrayType,
			}
		}
		arrowSchema := BuildArrowSchema(columns)

		for {
			record, count, err := RowsToArrowBatch(rows, arrowSchema, batchSize)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			if count == 0 {
				break
			}
			results <- source.RecordBatchResult{Batch: record}
		}
	}()

	return results, nil
}

func (s *ADBCSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[%s] Starting read from %s", s.dialect.Name(), table)

	// Check if dialect supports native storage API reading
	if storageReader, ok := s.dialect.(StorageReader); ok {
		config.Debug("[%s] Using Storage API for data read", s.dialect.Name())

		// Ensure schema is in opts for storage reader
		optsWithSchema := opts
		optsWithSchema.Schema = tableSchema

		results, err := storageReader.ReadWithStorageAPI(ctx, table, optsWithSchema)
		if err != nil {
			// Fall back to SQL-based reading if Storage API fails
			config.Debug("[%s] Storage API failed (%v), falling back to SQL-based reading", s.dialect.Name(), err)
		} else {
			return results, nil
		}
	}

	// Existing SQL-based read path (unchanged)
	columns := FilterColumns(tableSchema.Columns, opts.ExcludeColumns)
	arrowSchema := BuildArrowSchema(columns)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		query := BuildSelectQuery(table, columns, opts, s.dialect.QuoteIdentifier)
		// Annotate the source read so a warehouse source (e.g. BigQuery) attributes
		// the extract query's cost. Prepend keeps a leading comment (BigQuery,
		// DuckDB); Snowflake strips it, so a Snowflake source would need a QUERY_TAG
		// instead — not handled here.
		query = annotation.Prepend(annotation.WithStep(ctx, annotation.StepExtract), query)
		config.Debug("[%s] Executing query: %s", s.dialect.Name(), query)

		startQuery := time.Now()
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()
		config.Debug("[%s] Query started in %v", s.dialect.Name(), time.Since(startQuery))

		batchNum := 0
		totalRows := int64(0)

		for {
			startBatch := time.Now()
			record, count, err := RowsToArrowBatch(rows, arrowSchema, batchSize)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}

			if count == 0 {
				break
			}

			batchNum++
			totalRows += count
			config.Debug("[%s] Batch %d: %d rows read in %v (total: %d)", s.dialect.Name(), batchNum, count, time.Since(startBatch), totalRows)

			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[%s] Total: %d rows in %d batches, read time: %v", s.dialect.Name(), totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}
