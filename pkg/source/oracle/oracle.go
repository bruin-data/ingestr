package oracle

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
	_ "github.com/sijms/go-ora/v2"
)

type OracleSource struct {
	db        *sql.DB
	uri       string
	tzColumns map[string]bool // columns that are TIMESTAMP WITH TIME ZONE — need SYS_EXTRACT_UTC
}

func NewOracleSource() *OracleSource {
	return &OracleSource{}
}

func (s *OracleSource) Schemes() []string {
	return []string{"oracle", "oracle+cx_oracle"}
}

func (s *OracleSource) Connect(ctx context.Context, uri string) error {
	connStrs, err := buildConnStrings(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Oracle URI: %w", err)
	}

	var db *sql.DB
	var lastErr error
	for _, connStr := range connStrs {
		db, err = sql.Open("oracle", connStr)
		if err != nil {
			lastErr = err
			continue
		}

		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)

		if err = db.PingContext(ctx); err != nil {
			_ = db.Close()
			lastErr = err
			config.Debug("[SOURCE] Oracle connection attempt failed: %v", err)
			continue
		}

		s.db = db
		s.uri = uri
		return nil
	}

	return fmt.Errorf("failed to connect to Oracle: %w", lastErr)
}

// buildConnStrings returns one or more go-ora connection strings to try.
// When ?service_name= or ?SID= is explicit, returns a single string.
// When only /dbname is in the path (ingestr format), returns two strings:
// first try as SID (ingestr behavior), then as service name (fallback for modern Oracle).
//
// Supported formats:
//   - oracle://user:pass@host:port/dbname              (try SID first, then service name)
//   - oracle+cx_oracle://user:pass@host:port/dbname    (ingestr scheme, same as above)
//   - oracle://user:pass@host:port/?service_name=X     (service name only)
//   - oracle://user:pass@host:port?SID=X               (SID only)
func buildConnStrings(uri string) ([]string, error) {
	normalized := uri
	if strings.HasPrefix(strings.ToLower(uri), "oracle+cx_oracle://") {
		normalized = "oracle://" + uri[len("oracle+cx_oracle://"):]
	}

	u, err := url.Parse(normalized)
	if err != nil {
		return nil, err
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "oracle" {
		return nil, fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "1521"
	}

	var userInfo *url.Userinfo
	if u.User != nil {
		user := u.User.Username()
		password, hasPass := u.User.Password()
		if hasPass {
			userInfo = url.UserPassword(user, password)
		} else {
			userInfo = url.User(user)
		}
	}

	query := u.Query()
	pathValue := strings.TrimPrefix(u.Path, "/")
	serviceName := query.Get("service_name")

	buildURL := func(path string, q url.Values) string {
		connURL := &url.URL{
			Scheme: "oracle",
			Host:   fmt.Sprintf("%s:%s", host, port),
			Path:   path,
			User:   userInfo,
		}
		if len(q) > 0 {
			connURL.RawQuery = q.Encode()
		}
		return connURL.String()
	}

	if serviceName != "" {
		// Explicit service name — single attempt
		q := u.Query()
		q.Del("service_name")
		return []string{buildURL("/"+serviceName, q)}, nil
	}

	if query.Get("SID") != "" {
		// Explicit SID — single attempt
		return []string{buildURL("/", query)}, nil
	}

	if pathValue != "" {
		// Path with no explicit param — try SID first (ingestr compat), then service name (modern Oracle)
		sidQuery := u.Query()
		sidQuery.Set("SID", pathValue)
		svcQuery := u.Query()
		return []string{
			buildURL("/", sidQuery),
			buildURL("/"+pathValue, svcQuery),
		}, nil
	}

	return []string{buildURL("/", query)}, nil
}

func (s *OracleSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *OracleSource) HandlesIncrementality() bool {
	return false
}

func (s *OracleSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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
	tzCols := s.tzColumns // snapshot per table to avoid shared state across GetTable calls

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
			return s.read(ctx, tableName, tableSchema, opts, tzCols)
		},
	}, nil
}

func (s *OracleSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseTableName(table)

	// If no schema specified, get current user as default schema
	if schemaName == "" {
		var currentUser string
		err := s.db.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&currentUser)
		if err != nil {
			return nil, fmt.Errorf("failed to get current user: %w", err)
		}
		schemaName = currentUser
	}

	query := `
		SELECT
			COLUMN_NAME,
			DATA_TYPE,
			NULLABLE,
			DATA_PRECISION,
			DATA_SCALE,
			CHAR_LENGTH
		FROM ALL_TAB_COLUMNS
		WHERE OWNER = :1 AND TABLE_NAME = :2
		ORDER BY COLUMN_ID
	`

	rows, err := s.db.QueryContext(ctx, query, strings.ToUpper(schemaName), strings.ToUpper(tableName))
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	s.tzColumns = make(map[string]bool)

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, nullable string
		var dataPrecision, dataScale, charLength sql.NullInt64

		if err := rows.Scan(&columnName, &dataType, &nullable, &dataPrecision, &dataScale, &charLength); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Track TIMESTAMP WITH TIME ZONE columns — go-ora drops the TZ offset,
		// so we need to wrap these with SYS_EXTRACT_UTC() in queries.
		upperDataType := strings.ToUpper(dataType)
		if strings.Contains(upperDataType, "WITH TIME ZONE") || strings.Contains(upperDataType, "WITH LOCAL TIME ZONE") {
			s.tzColumns[columnName] = true
		}

		dt, precision, scale, arrayType := MapOracleToDataType(dataType)

		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  nullable == "Y",
			ArrayType: arrayType,
		}

		if dataPrecision.Valid {
			col.Precision = int(dataPrecision.Int64)
		} else if dt == schema.TypeDecimal && dataScale.Valid && dataScale.Int64 == 0 {
			// Oracle INTEGER/SMALLINT aliases report as NUMBER with NULL precision and scale=0.
			// Map these to Int64 (BIGINT) to match ingestr/SQLAlchemy behavior.
			col.DataType = schema.TypeInt64
			col.Precision = 0
		} else if precision > 0 {
			col.Precision = precision
		}

		if dataScale.Valid {
			col.Scale = int(dataScale.Int64)
		} else if scale > 0 {
			col.Scale = scale
		}

		if charLength.Valid {
			col.MaxLength = int(charLength.Int64)
		}

		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	pkQuery := `
		SELECT cc.COLUMN_NAME
		FROM ALL_CONSTRAINTS c
		JOIN ALL_CONS_COLUMNS cc
			ON c.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
			AND c.OWNER = cc.OWNER
		WHERE c.CONSTRAINT_TYPE = 'P'
			AND c.OWNER = :1
			AND c.TABLE_NAME = :2
		ORDER BY cc.POSITION
	`

	var primaryKeys []string
	pkRows, err := s.db.QueryContext(ctx, pkQuery, strings.ToUpper(schemaName), strings.ToUpper(tableName))
	if err == nil {
		defer func() { _ = pkRows.Close() }()
		for pkRows.Next() {
			var pkName string
			if err := pkRows.Scan(&pkName); err == nil {
				primaryKeys = append(primaryKeys, pkName)
			}
		}
	}

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

func (s *OracleSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions, tzColumns map[string]bool) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[SOURCE] Starting read from %s", table)

	columns := filterColumns(tableSchema.Columns, opts.ExcludeColumns)
	arrowSchema := buildArrowSchema(columns)

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		query := buildSelectQuery(table, columns, opts, tzColumns)

		startQuery := time.Now()
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query: %w", err)}
			return
		}
		defer func() { _ = rows.Close() }()
		config.Debug("[SOURCE] Query started in %v", time.Since(startQuery))

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
			config.Debug("[SOURCE] Batch %d: %d rows read in %v (total: %d)", batchNum, count, time.Since(startBatch), totalRows)

			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[SOURCE] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func (s *OracleSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[SOURCE] Executing custom query: %s", query)
		rows, err := s.db.QueryContext(ctx, query)
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
			dt, precision, scale, arrayType := MapOracleToDataType(ct.DatabaseTypeName())
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

func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", table
}

func filterColumns(columns []schema.Column, exclude []string) []schema.Column {
	if len(exclude) == 0 {
		return columns
	}

	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToUpper(col)] = true
	}

	var filtered []schema.Column
	for _, col := range columns {
		if !excludeMap[strings.ToUpper(col.Name)] {
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

func buildSelectQuery(table string, columns []schema.Column, opts source.ReadOptions, tzColumns map[string]bool) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		if tzColumns[col.Name] {
			// go-ora drops TZ offset from TIMESTAMP WITH TIME ZONE columns.
			// SYS_EXTRACT_UTC converts to UTC at the Oracle level so the
			// absolute instant is preserved when go-ora reads it as a plain timestamp.
			colNames[i] = fmt.Sprintf("SYS_EXTRACT_UTC(%s) AS %s", quoteIdentifier(col.Name), quoteIdentifier(col.Name))
		} else {
			colNames[i] = quoteIdentifier(col.Name)
		}
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quoteTable(table))

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("%s >= TO_TIMESTAMP('%s', 'YYYY-MM-DD HH24:MI:SS')", quoteIdentifier(opts.IncrementalKey), opts.IntervalStart.Format("2006-01-02 15:04:05")))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("%s <= TO_TIMESTAMP('%s', 'YYYY-MM-DD HH24:MI:SS')", quoteIdentifier(opts.IncrementalKey), opts.IntervalEnd.Format("2006-01-02 15:04:05")))
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" FETCH FIRST %d ROWS ONLY", opts.Limit)
	}

	return query
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteTable(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return quoteIdentifier(strings.ToUpper(parts[0])) + "." + quoteIdentifier(strings.ToUpper(parts[1]))
	}
	return quoteIdentifier(strings.ToUpper(table))
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
			arrowconv.AppendValue(builders[i], val)
		}
		rowCount++

		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	if err := rows.Err(); err != nil {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	if rowCount == 0 {
		for _, b := range builders {
			b.Release()
		}
		return nil, 0, nil
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
	}

	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)

	for _, arr := range arrays {
		arr.Release()
	}

	return record, rowCount, nil
}
