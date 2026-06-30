package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	_ "github.com/go-sql-driver/mysql"
)

// defaultCatalog is StarRocks' internal catalog; external lakehouse catalogs
// (Iceberg/Hudi/Hive) are named as the leading part of a catalog.database.table.
const defaultCatalog = "default_catalog"

// maxDecimal128Precision is the largest precision Arrow's Decimal128 can hold.
const maxDecimal128Precision = 38

// StarRocksSource reads from StarRocks over the MySQL wire protocol. Table names
// may be catalog.database.table; the URI path sets defaults the name overrides.
type StarRocksSource struct {
	db       *sql.DB
	catalog  string
	database string
}

func NewStarRocksSource() *StarRocksSource {
	return &StarRocksSource{}
}

func (s *StarRocksSource) Schemes() []string {
	return []string{"starrocks"}
}

func (s *StarRocksSource) Connect(ctx context.Context, uri string) error {
	dsn, catalog, database, err := parseStarRocksURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse StarRocks URI: %w", err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open StarRocks connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping StarRocks: %w", err)
	}

	s.db = db
	s.catalog = catalog
	s.database = database
	config.Debug("[STARROCKS] Connected (catalog: %s, database: %s)", catalog, database)
	return nil
}

func (s *StarRocksSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *StarRocksSource) HandlesIncrementality() bool {
	return false
}

func (s *StarRocksSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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

// getSchema reads column metadata from a zero-row SELECT, which (unlike
// information_schema) works for external lakehouse tables too.
func (s *StarRocksSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	catalog, database, tableName := s.parseTableName(table)
	if database == "" {
		return nil, fmt.Errorf("no database resolved for table %q: provide a default in the URI path "+
			"(starrocks://.../<database> or starrocks://.../<catalog>/<database>) "+
			"or qualify the table name (database.table or catalog.database.table)", table)
	}
	fqn := buildFQN(catalog, database, tableName)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT 0", fqn))
	if err != nil {
		return nil, fmt.Errorf("failed to query schema for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := columnsFromTypes(rows)
	if err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	return &schema.TableSchema{
		Name:    tableName,
		Schema:  database,
		Columns: columns,
	}, nil
}

func columnsFromTypes(rows *sql.Rows) ([]schema.Column, error) {
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	columns := make([]schema.Column, len(colTypes))
	for i, ct := range colTypes {
		dt, precision, scale, arrayType := MapStarRocksToDataType(ct.DatabaseTypeName())
		nullable, ok := ct.Nullable()
		if !ok {
			nullable = true
		}

		col := schema.Column{
			Name:      ct.Name(),
			DataType:  dt,
			Nullable:  nullable,
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		}

		if dt == schema.TypeDecimal {
			if p, sc, ok := ct.DecimalSize(); ok {
				col.Precision = int(p)
				col.Scale = int(sc)
			}
			// StarRocks reports wider precision for aggregates (SUM) and DECIMAL256;
			// clamp so the Decimal128 builder does not overflow its bound.
			if col.Precision > maxDecimal128Precision {
				col.Precision = maxDecimal128Precision
			}
			if col.Scale > col.Precision {
				col.Scale = col.Precision
			}
		}
		if length, ok := ct.Length(); ok && length > 0 && length < 1<<31 {
			col.MaxLength = int(length)
		}

		columns[i] = col
	}
	return columns, nil
}

func (s *StarRocksSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[STARROCKS] Starting read from %s", table)

	schemaToUse := tableSchema
	if opts.Schema != nil {
		schemaToUse = opts.Schema
	}

	columns := filterColumns(schemaToUse.Columns, opts.ExcludeColumns)
	arrowSchema := buildArrowSchema(columns)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		query := s.buildSelectQuery(table, columns, opts)
		config.Debug("[STARROCKS] Query: %s", query)

		startQuery := time.Now()
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()
		config.Debug("[STARROCKS] Query started in %v", time.Since(startQuery))

		batchNum := 0
		totalRows := int64(0)

		for {
			startBatch := time.Now()
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}

			if count == 0 {
				break
			}

			batchNum++
			totalRows += count
			config.Debug("[STARROCKS] Batch %d: %d rows read in %v (total: %d)", batchNum, count, time.Since(startBatch), totalRows)

			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[STARROCKS] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func (s *StarRocksSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[STARROCKS] Executing custom query: %s", query)
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to execute custom query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()

		columns, err := columnsFromTypes(rows)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		arrowSchema := buildArrowSchema(columns)

		for {
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize)
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

func (s *StarRocksSource) parseTableName(table string) (catalog, database, tableName string) {
	tn, err := tablename.StarRocks.Parse(table, tablename.Defaults{Catalog: s.catalog, Schema: s.database})
	if err != nil {
		return s.catalog, s.database, table
	}
	return tn.Catalog, tn.Schema, tn.Table
}

func quoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

// buildFQN renders a backtick-quoted table reference, omitting empty leading
// components so an unqualified table still produces valid SQL.
func buildFQN(catalog, database, table string) string {
	parts := make([]string, 0, 3)
	if catalog != "" {
		parts = append(parts, quoteIdentifier(catalog))
	}
	if database != "" {
		parts = append(parts, quoteIdentifier(database))
	}
	parts = append(parts, quoteIdentifier(table))
	return strings.Join(parts, ".")
}

func (s *StarRocksSource) buildSelectQuery(table string, columns []schema.Column, opts source.ReadOptions) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = quoteIdentifier(col.Name)
	}

	catalog, database, tableName := s.parseTableName(table)
	fqn := buildFQN(catalog, database, tableName)

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), fqn)

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("%s >= '%s'", quoteIdentifier(opts.IncrementalKey), opts.IntervalStart.Format("2006-01-02 15:04:05")))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("%s <= '%s'", quoteIdentifier(opts.IncrementalKey), opts.IntervalEnd.Format("2006-01-02 15:04:05")))
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return query
}

func filterColumns(columns []schema.Column, exclude []string) []schema.Column {
	if len(exclude) == 0 {
		return columns
	}

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

func rowsToArrowRecordBatch(rows *sql.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))

	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	scanDest := make([]interface{}, len(columns))
	for i := range columns {
		scanDest[i] = new(interface{})
	}

	var rowCount int64
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			for _, b := range builders {
				b.Release()
			}
			return nil, 0, fmt.Errorf("failed to scan row: %w", err)
		}

		for i, dest := range scanDest {
			val := *dest.(*interface{})
			arrowconv.AppendValue(builders[i], convertValue(val))
		}
		rowCount++

		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	if rowCount == 0 {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, nil
	}

	if err := rows.Err(); err != nil {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
		b.Release()
	}

	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)

	for _, arr := range arrays {
		arr.Release()
	}

	return record, rowCount, nil
}

func convertValue(val interface{}) interface{} {
	switch v := val.(type) {
	case []byte:
		return string(v)
	default:
		return val
	}
}

// parseStarRocksURI converts a starrocks:// URI to a go-sql-driver/mysql DSN.
// Path [catalog/]database: one segment is the database, two are catalog/database.
func parseStarRocksURI(uri string) (dsn, catalog, database string, err error) {
	u, parseErr := url.Parse(uri)
	if parseErr != nil {
		return "", "", "", parseErr
	}

	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port := u.Port()
	if port == "" {
		// StarRocks FE MySQL protocol (query) port.
		port = "9030"
	}

	var user, password string
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}

	catalog = defaultCatalog
	if trimmed := strings.Trim(u.Path, "/"); trimmed != "" {
		parts := strings.Split(trimmed, "/")
		if len(parts) == 1 {
			database = parts[0]
		} else {
			catalog = parts[0]
			database = parts[1]
		}
	}

	query := u.Query()
	if tlsErr := applyTLSParams(query); tlsErr != nil {
		return "", "", "", tlsErr
	}

	dsn = ""
	if user != "" {
		dsn = user
		if password != "" {
			dsn += ":" + password
		}
		dsn += "@"
	}
	// Omit the database from the DSN: pinning it makes the driver run USE <db> on
	// connect, which fails for databases that live in external catalogs.
	dsn += fmt.Sprintf("tcp(%s:%s)/", host, port)

	query.Set("parseTime", "true")
	dsn += "?" + query.Encode()

	return dsn, catalog, database, nil
}

// applyTLSParams maps a user-facing `ssl` parameter to go-sql-driver's `tls`
// (a raw `tls` passes through). Unknown values are rejected, not downgraded.
func applyTLSParams(query url.Values) error {
	if query.Has("tls") {
		query.Del("ssl")
		return nil
	}

	ssl := strings.ToLower(strings.TrimSpace(query.Get("ssl")))
	query.Del("ssl")

	switch ssl {
	case "", "false", "0", "disable", "disabled":
		// plaintext; nothing to set
	case "true", "1", "enable", "enabled", "require", "required":
		query.Set("tls", "true")
	case "skip-verify", "skip_verify", "insecure":
		query.Set("tls", "skip-verify")
	default:
		return fmt.Errorf("invalid ssl value %q: use true, false, or skip-verify", ssl)
	}
	return nil
}

var _ source.Source = (*StarRocksSource)(nil)
