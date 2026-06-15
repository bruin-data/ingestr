package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/connredact"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	mssqldb "github.com/microsoft/go-mssqldb"
	_ "github.com/microsoft/go-mssqldb/azuread"
)

const (
	sqlServerDriverName = "sqlserver"
	azureSQLDriverName  = "azuresql"
)

type MSSQLSource struct {
	db             *sql.DB
	uri            string
	guidConversion bool
}

func NewMSSQLSource() *MSSQLSource {
	return &MSSQLSource{}
}

func (s *MSSQLSource) Schemes() []string {
	return []string{"mssql", "sqlserver", "mssql+pyodbc", "azuresql", "azure-sql"}
}

func (s *MSSQLSource) Connect(ctx context.Context, uri string) error {
	connStr, driverName, err := URIToConnString(uri)
	if err != nil {
		return fmt.Errorf("failed to parse SQL Server URI: %w", connredact.Redact(uri, err))
	}

	db, err := sql.Open(driverName, connStr)
	if err != nil {
		return fmt.Errorf("failed to open SQL Server connection: %w", connredact.Redact(uri, err))
	}

	// Configure connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping SQL Server: %w", connredact.Redact(uri, err))
	}

	s.db = db
	s.uri = uri
	s.guidConversion = guidConversionEnabled(connStr)
	return nil
}

// URIToConnString converts SQL Server and Azure SQL URIs to the connection
// string format expected by go-mssqldb.
// URI format: mssql://user:pass@host:port/database?param=value
// Conn string format: sqlserver://user:pass@host:port?database=db&param=value
func URIToConnString(uri string) (string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", err
	}

	scheme := strings.ToLower(u.Scheme)
	isAzureSQL := scheme == "azuresql" || scheme == "azure-sql"
	if !strings.HasPrefix(scheme, "mssql") && scheme != "sqlserver" && !isAzureSQL {
		return "", "", fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "1433"
	}

	var user, password string
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}

	database := strings.TrimPrefix(u.Path, "/")
	query := u.Query()
	deleteQueryParamCI(query, "driver")

	auth, hasAuthentication := normalizeQueryParamCI(query, "authentication")
	if hasAuthentication {
		deleteQueryParamCI(query, "authentication")
		if _, hasFedAuth := normalizeQueryParamCI(query, "fedauth"); !hasFedAuth {
			if fedAuth := fedAuthFromAuthentication(auth); fedAuth != "" {
				query.Set("fedauth", fedAuth)
			}
		}
	}

	fedAuth, hasFedAuth := normalizeQueryParamCI(query, "fedauth")
	tenantID, hasTenantID := normalizeQueryParamCI(query, "tenant_id")
	if hasTenantID {
		deleteQueryParamCI(query, "tenant_id")
	}

	if isAzureSQL {
		if _, hasEncrypt := normalizeQueryParamCI(query, "encrypt"); !hasEncrypt {
			query.Set("encrypt", "true")
		}
		if !hasFedAuth {
			switch {
			case tenantID != "" && user != "":
				fedAuth = "ActiveDirectoryServicePrincipal"
				query.Set("fedauth", fedAuth)
			case user == "":
				fedAuth = "ActiveDirectoryDefault"
				query.Set("fedauth", fedAuth)
			}
		}
	}

	driverName := sqlServerDriverName
	if isAzureSQL || hasFedAuth || hasAuthentication {
		driverName = azureSQLDriverName
	}

	if tenantID != "" && user != "" && isServicePrincipalFedAuth(fedAuth) && !strings.Contains(user, "@") {
		user = user + "@" + tenantID
	}

	connURL := &url.URL{
		Scheme: "sqlserver",
		Host:   fmt.Sprintf("%s:%s", host, port),
	}

	if user != "" || password != "" {
		if password != "" {
			connURL.User = url.UserPassword(user, password)
		} else {
			connURL.User = url.User(user)
		}
	}

	if database != "" {
		query.Set("database", database)
	}
	connURL.RawQuery = query.Encode()

	return connURL.String(), driverName, nil
}

func normalizeQueryParamCI(query url.Values, canonical string) (string, bool) {
	for key, values := range query {
		if !strings.EqualFold(key, canonical) {
			continue
		}
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		if key != canonical {
			query.Del(key)
			query.Set(canonical, value)
		}
		return value, true
	}
	return "", false
}

func deleteQueryParamCI(query url.Values, key string) {
	for existing := range query {
		if strings.EqualFold(existing, key) {
			query.Del(existing)
		}
	}
}

func fedAuthFromAuthentication(authentication string) string {
	normalized := strings.ToLower(strings.ReplaceAll(authentication, " ", ""))
	switch normalized {
	case "activedirectoryaccesstoken", "activedirectoryserviceprincipalaccesstoken":
		return "ActiveDirectoryServicePrincipalAccessToken"
	case "activedirectoryserviceprincipal", "activedirectoryapplication":
		return "ActiveDirectoryServicePrincipal"
	case "activedirectorydefault":
		return "ActiveDirectoryDefault"
	case "activedirectorymanagedidentity", "activedirectorymsi":
		return "ActiveDirectoryManagedIdentity"
	case "activedirectorypassword":
		return "ActiveDirectoryPassword"
	case "activedirectoryazcli":
		return "ActiveDirectoryAzCli"
	case "activedirectorydevicecode":
		return "ActiveDirectoryDeviceCode"
	case "activedirectoryinteractive":
		return "ActiveDirectoryInteractive"
	default:
		return ""
	}
}

func isServicePrincipalFedAuth(fedAuth string) bool {
	return strings.EqualFold(fedAuth, "ActiveDirectoryServicePrincipal") ||
		strings.EqualFold(fedAuth, "ActiveDirectoryApplication")
}

func guidConversionEnabled(connStr string) bool {
	u, err := url.Parse(connStr)
	if err != nil {
		return false
	}

	raw, ok := normalizeQueryParamCI(u.Query(), "guid conversion")
	if !ok || raw == "" {
		return false
	}

	enabled, err := strconv.ParseBool(raw)
	return err == nil && enabled
}

func (s *MSSQLSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *MSSQLSource) HandlesIncrementality() bool {
	return false
}

func (s *MSSQLSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
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

func (s *MSSQLSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	tableRef := parseMSSQLTableRef(table)
	schemaName, tableName := tableRef.schemaName, tableRef.tableName
	query, pkQuery := schemaMetadataQueries(tableRef)

	rows, err := s.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, isNullable string
		var numericPrecision, numericScale, charMaxLen sql.NullInt64

		if err := rows.Scan(&columnName, &dataType, &isNullable, &numericPrecision, &numericScale, &charMaxLen); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		dt, precision, scale, arrayType := MapMSSQLToDataType(dataType)

		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  isNullable == "YES",
			ArrayType: arrayType,
		}

		if numericPrecision.Valid {
			col.Precision = int(numericPrecision.Int64)
		} else if precision > 0 {
			col.Precision = precision
		}

		if numericScale.Valid {
			col.Scale = int(numericScale.Int64)
		} else if scale > 0 {
			col.Scale = scale
		}

		if charMaxLen.Valid {
			col.MaxLength = int(charMaxLen.Int64)
		}

		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	var primaryKeys []string
	pkRows, err := s.db.QueryContext(ctx, pkQuery, schemaName, tableName)
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

func schemaMetadataQueries(tableRef mssqlTableRef) (string, string) {
	infoSchema := tableRef.informationSchemaQualifier()
	columnsQuery := fmt.Sprintf(`
		SELECT
			COLUMN_NAME,
			DATA_TYPE,
			IS_NULLABLE,
			NUMERIC_PRECISION,
			NUMERIC_SCALE,
			CHARACTER_MAXIMUM_LENGTH
		FROM %s.COLUMNS
		WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2
		ORDER BY ORDINAL_POSITION
	`, infoSchema)

	pkQuery := fmt.Sprintf(`
		SELECT c.COLUMN_NAME
		FROM %s.TABLE_CONSTRAINTS tc
		JOIN %s.KEY_COLUMN_USAGE c
			ON tc.CONSTRAINT_NAME = c.CONSTRAINT_NAME
			AND tc.TABLE_SCHEMA = c.TABLE_SCHEMA
		WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
			AND tc.TABLE_SCHEMA = @p1
			AND tc.TABLE_NAME = @p2
		ORDER BY c.ORDINAL_POSITION
	`, infoSchema, infoSchema)

	return columnsQuery, pkQuery
}

func (s *MSSQLSource) read(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[SOURCE] Starting read from %s", table)

	schemaToUse := tableSchema
	if opts.Schema != nil {
		schemaToUse = opts.Schema
		config.Debug("[SOURCE] Using provided schema (%d columns)", len(schemaToUse.Columns))
	} else {
		config.Debug("[SOURCE] Using pre-fetched schema (%d columns)", len(schemaToUse.Columns))
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

		query := buildSelectQuery(table, columns, opts)

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
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize, s.guidConversion)
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

func (s *MSSQLSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
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
			dt, precision, scale, arrayType := MapMSSQLToDataType(ct.DatabaseTypeName())
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
			record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize, s.guidConversion)
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

type mssqlTableRef struct {
	parts        []string
	catalogParts []string
	schemaName   string
	tableName    string
}

func parseTableName(table string) (string, string) {
	tableRef := parseMSSQLTableRef(table)
	return tableRef.schemaName, tableRef.tableName
}

func parseMSSQLTableRef(table string) mssqlTableRef {
	parts := splitMSSQLIdentifierPath(table)
	tableName := parts[len(parts)-1]
	schemaName := "dbo"
	if len(parts) > 1 && parts[len(parts)-2] != "" {
		schemaName = parts[len(parts)-2]
	}

	var catalogParts []string
	if len(parts) > 2 {
		catalogParts = parts[:len(parts)-2]
	}

	return mssqlTableRef{
		parts:        parts,
		catalogParts: catalogParts,
		schemaName:   schemaName,
		tableName:    tableName,
	}
}

func (r mssqlTableRef) informationSchemaQualifier() string {
	if len(r.catalogParts) == 0 {
		return "INFORMATION_SCHEMA"
	}
	quotedCatalog := quoteCatalogIdentifierPath(r.catalogParts)
	if quotedCatalog == "" {
		return "INFORMATION_SCHEMA"
	}
	return quotedCatalog + ".INFORMATION_SCHEMA"
}

func splitMSSQLIdentifierPath(path string) []string {
	path = strings.TrimSpace(path)
	var rawParts []string
	var current strings.Builder
	inBracket := false

	for i := 0; i < len(path); i++ {
		ch := path[i]
		if inBracket {
			current.WriteByte(ch)
			if ch == ']' {
				if i+1 < len(path) && path[i+1] == ']' {
					i++
					current.WriteByte(path[i])
					continue
				}
				inBracket = false
			}
			continue
		}

		switch ch {
		case '[':
			inBracket = true
			current.WriteByte(ch)
		case '.':
			rawParts = append(rawParts, current.String())
			current.Reset()
		default:
			current.WriteByte(ch)
		}
	}
	rawParts = append(rawParts, current.String())

	parts := make([]string, len(rawParts))
	for i, part := range rawParts {
		parts[i] = normalizeMSSQLIdentifierPart(part)
	}
	return parts
}

func normalizeMSSQLIdentifierPart(part string) string {
	part = strings.TrimSpace(part)
	if len(part) >= 2 && part[0] == '[' && part[len(part)-1] == ']' {
		return strings.ReplaceAll(part[1:len(part)-1], "]]", "]")
	}
	return part
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

func buildSelectQuery(table string, columns []schema.Column, opts source.ReadOptions) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = quoteColumn(col.Name)
	}

	// Handle TOP for limit (SQL Server uses TOP instead of LIMIT)
	selectClause := "SELECT"
	if opts.Limit > 0 {
		selectClause = fmt.Sprintf("SELECT TOP %d", opts.Limit)
	}

	query := fmt.Sprintf("%s %s FROM %s", selectClause, strings.Join(colNames, ", "), quoteTable(table))

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("%s >= '%s'", quoteColumn(opts.IncrementalKey), opts.IntervalStart.Format("2006-01-02 15:04:05")))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("%s <= '%s'", quoteColumn(opts.IncrementalKey), opts.IntervalEnd.Format("2006-01-02 15:04:05")))
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	return query
}

func quoteTable(table string) string {
	tableRef := parseMSSQLTableRef(table)
	return quoteIdentifierPath(tableRef.parts)
}

// quoteIdentifierPath joins parts as bracket-quoted SQL Server identifiers.
// Empty parts are kept as empty strings so two-dot notation (e.g. [db]..[table])
// round-trips correctly when tableRef.parts is passed directly.
func quoteIdentifierPath(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			quoted = append(quoted, "")
			continue
		}
		quoted = append(quoted, quoteColumn(part))
	}
	return strings.Join(quoted, ".")
}

func quoteCatalogIdentifierPath(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		quoted = append(quoted, quoteColumn(part))
	}
	return strings.Join(quoted, ".")
}

func quoteColumn(name string) string {
	return fmt.Sprintf("[%s]", strings.ReplaceAll(name, "]", "]]"))
}

func rowsToArrowRecordBatch(rows *sql.Rows, arrowSchema *arrow.Schema, columns []schema.Column, batchSize int, guidConversion bool) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))

	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	// Prepare scan destinations
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
			if columns[i].DataType == schema.TypeUUID {
				var err error
				val, err = normalizeUUIDValue(val, guidConversion)
				if err != nil {
					for _, b := range builders {
						b.Release()
					}
					return nil, 0, fmt.Errorf("failed to convert uniqueidentifier column %q: %w", columns[i].Name, err)
				}
			}
			arrowconv.AppendValue(builders[i], val)
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
	}

	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)

	for _, arr := range arrays {
		arr.Release()
	}

	return record, rowCount, nil
}

func normalizeUUIDValue(val interface{}, guidConversion bool) (interface{}, error) {
	switch v := val.(type) {
	case nil:
		return nil, nil
	case []byte:
		return formatUUIDBytes(v, guidConversion)
	case mssqldb.UniqueIdentifier:
		return v.String(), nil
	case *mssqldb.UniqueIdentifier:
		if v == nil {
			return nil, nil
		}
		return v.String(), nil
	case mssqldb.NullUniqueIdentifier:
		if !v.Valid {
			return nil, nil
		}
		return v.UUID.String(), nil
	case *mssqldb.NullUniqueIdentifier:
		if v == nil || !v.Valid {
			return nil, nil
		}
		return v.UUID.String(), nil
	default:
		return val, nil
	}
}

func formatUUIDBytes(raw []byte, guidConversion bool) (string, error) {
	if len(raw) != 16 {
		return "", fmt.Errorf("invalid uniqueidentifier length %d", len(raw))
	}

	if guidConversion {
		return fmt.Sprintf("%X-%X-%X-%X-%X", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
	}

	var uuid mssqldb.UniqueIdentifier
	if err := uuid.Scan(raw); err != nil {
		return "", err
	}
	return uuid.String(), nil
}
