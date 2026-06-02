package netsuite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	defaultConnectPort       = "1708"
	defaultServerDataSource  = "NetSuite2.com"
	defaultConnectBatchSize  = 1000
	defaultConnectHostSuffix = ".connect.api.netsuite.com"

	// bindableCharWidthLimit mirrors the alexbrainman/odbc wrapper's threshold
	// (column.go: NewVariableWidthColumn): a character/binary column whose
	// declared size is 0 (unbounded) or greater than this is fetched via the
	// chunked SQLGetData "long data" path (NonBindableColumn). That native path
	// is what segfaults on wide tables; columns at or below this width are bound
	// and fetched safely in a single SQLFetch. See netsuite_crash_findings.md.
	bindableCharWidthLimit = 1024

	// defaultWideTextMaxChars is the SUBSTR width used by wide_text=truncate. It
	// stays under bindableCharWidthLimit so the truncated column is bound rather
	// than fetched via the crashing chunked path.
	defaultWideTextMaxChars = 1000

	wideTextKeep     = "keep"
	wideTextTruncate = "truncate"
	wideTextExclude  = "exclude"
)

type NetSuiteSource struct {
	db                 *sql.DB
	connString         string
	excludeCLOBColumns bool
	wideTextMode       string
	wideTextMaxChars   int
}

type uriConfig struct {
	// connString is the full ODBC connection string for password auth, or the
	// credential-less base string when tba is set (UID/PWD are appended per
	// connection by tbaConnector).
	connString string
	tba        *tbaCredentials
	// excludeCLOBColumns drops CLOB-typed columns from introspected projections
	// (legacy knob; superseded by wideTextMode, which covers every column on the
	// crashing chunked-fetch path, not just CLOBs).
	excludeCLOBColumns bool
	// wideTextMode controls how "non-bindable" wide text columns (the chunked
	// SQLGetData path that segfaults the SuiteAnalytics driver on wide tables)
	// are projected: keep (default), truncate (SUBSTR to wideTextMaxChars), or
	// exclude (drop them).
	wideTextMode     string
	wideTextMaxChars int
}

type odbcParam struct {
	key   string
	value string
}

func NewNetSuiteSource() *NetSuiteSource {
	return &NetSuiteSource{}
}

func (s *NetSuiteSource) Schemes() []string {
	return []string{"netsuite", "netsuite+odbc"}
}

func (s *NetSuiteSource) Connect(ctx context.Context, rawURI string) error {
	cfg, err := parseURI(rawURI)
	if err != nil {
		return err
	}
	if !odbcDriverRegistered() {
		return fmt.Errorf("ODBC driver support is not available in this ingestr build; use a Linux or Windows build with the ODBC manager installed")
	}

	db, err := openConnection(cfg)
	if err != nil {
		return fmt.Errorf("failed to open NetSuite ODBC connection: %w", err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping NetSuite SuiteAnalytics Connect over ODBC: %w", err)
	}

	s.db = db
	s.connString = cfg.connString
	s.excludeCLOBColumns = cfg.excludeCLOBColumns
	s.wideTextMode = cfg.wideTextMode
	s.wideTextMaxChars = cfg.wideTextMaxChars
	config.Debug("[NETSUITE] Connected to SuiteAnalytics Connect over ODBC")
	return nil
}

func (s *NetSuiteSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *NetSuiteSource) HandlesIncrementality() bool {
	return false
}

func (s *NetSuiteSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableName := strings.TrimSpace(req.Name)
	if tableName == "" {
		return nil, fmt.Errorf("table name is required for netsuite source")
	}

	primaryKeys := req.PrimaryKeys
	if len(primaryKeys) == 0 {
		primaryKeys = []string{"id"}
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("netsuite SuiteAnalytics Connect source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readTable(ctx, tableName, opts)
		},
	}, nil
}

func (s *NetSuiteSource) readTable(ctx context.Context, tableName string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	// Project columns explicitly rather than SELECT *. NetSuite tables can be
	// extremely wide (e.g. `transaction` has ~700 columns), and SELECT * over
	// such tables crashes the SuiteAnalytics Connect ODBC driver. We discover
	// the columns from the driver's oa_columns catalog and list them; if that
	// catalog is unavailable we fall back to SELECT *.
	columns, err := s.tableColumns(ctx, tableName, opts.ExcludeColumns)
	if err != nil {
		config.Debug("[NETSUITE] column introspection failed for %q, falling back to SELECT *: %v", tableName, err)
		columns = nil
	}

	query := buildSuiteAnalyticsQuery(tableName, columns, opts)
	return s.readQuery(ctx, query, opts)
}

// tableColumns returns the projection expressions for a SuiteAnalytics table,
// read from the OpenAccess oa_columns catalog and excluding any names in
// exclude. Columns the alexbrainman/odbc wrapper would fetch via the chunked
// SQLGetData "long data" path (the path that segfaults the SuiteAnalytics driver
// on wide tables) are handled per wideTextMode: kept (and ordered last),
// SUBSTR-truncated to a bindable width, or dropped. It returns a nil slice (so
// the caller falls back to SELECT *) when the table has no catalog entry.
func (s *NetSuiteSource) tableColumns(ctx context.Context, tableName string, exclude []string) ([]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("netsuite source is not connected")
	}

	lookup := unqualifyTableName(tableName)
	if lookup == "" {
		return nil, nil
	}

	query := fmt.Sprintf("SELECT column_name, type_name, data_type, oa_precision FROM oa_columns WHERE table_name = '%s'", strings.ReplaceAll(lookup, "'", "''"))
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to read oa_columns for %q: %w", lookup, err)
	}
	defer func() { _ = rows.Close() }()

	excluded := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		excluded[strings.ToLower(strings.TrimSpace(name))] = true
	}

	type column struct {
		name        string
		isCLOB      bool
		nonBindable bool // fetched via the crashing chunked SQLGetData path
		isCharType  bool // SUBSTR-truncatable (vs binary long data)
	}
	var cols []column
	skippedCLOB := 0
	skippedWide := 0
	truncatedWide := 0
	for rows.Next() {
		var name, typeName string
		var dataType, precision sql.NullInt64
		if err := rows.Scan(&name, &typeName, &dataType, &precision); err != nil {
			return nil, fmt.Errorf("failed to scan oa_columns row: %w", err)
		}
		name = strings.TrimSpace(name)
		if name == "" || excluded[strings.ToLower(name)] {
			continue
		}
		isCLOB := strings.EqualFold(strings.TrimSpace(typeName), "CLOB")
		if isCLOB && s.excludeCLOBColumns {
			skippedCLOB++
			continue
		}
		cols = append(cols, column{
			name:        name,
			isCLOB:      isCLOB,
			nonBindable: isNonBindable(dataType, precision, isCLOB),
			isCharType:  isCharDataType(dataType) || isCLOB,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read oa_columns rows: %w", err)
	}

	// Keep bindable columns first and any kept non-bindable columns last. The
	// ODBC SQLGetData contract retrieves long/LOB columns after fixed-width ones;
	// trailing them is the safe order even though it is not, on its own,
	// sufficient to prevent the driver crash. Within each group, sort for
	// deterministic, layout-stable projections.
	sort.SliceStable(cols, func(i, j int) bool {
		if cols[i].nonBindable != cols[j].nonBindable {
			return !cols[i].nonBindable
		}
		return cols[i].name < cols[j].name
	})

	projections := make([]string, 0, len(cols))
	for _, c := range cols {
		if !c.nonBindable {
			projections = append(projections, c.name)
			continue
		}
		switch s.wideTextMode {
		case wideTextExclude:
			skippedWide++
		case wideTextTruncate:
			if c.isCharType {
				projections = append(projections, fmt.Sprintf("SUBSTR(%s, 1, %d) AS %s", c.name, s.wideTextMaxCharsOrDefault(), c.name))
				truncatedWide++
			} else {
				projections = append(projections, c.name)
			}
		default: // wideTextKeep
			projections = append(projections, c.name)
		}
	}

	config.Debug("[NETSUITE] introspected %d columns for %q (mode=%s: skipped %d CLOB, %d wide; truncated %d wide)",
		len(projections), lookup, s.wideTextModeOrDefault(), skippedCLOB, skippedWide, truncatedWide)
	return projections, nil
}

func (s *NetSuiteSource) wideTextModeOrDefault() string {
	if s.wideTextMode == "" {
		return wideTextKeep
	}
	return s.wideTextMode
}

func (s *NetSuiteSource) wideTextMaxCharsOrDefault() int {
	if s.wideTextMaxChars <= 0 || s.wideTextMaxChars > bindableCharWidthLimit {
		return defaultWideTextMaxChars
	}
	return s.wideTextMaxChars
}

// isNonBindable reports whether the alexbrainman/odbc wrapper would fetch this
// column via the chunked SQLGetData "long data" path (NonBindableColumn) rather
// than binding it — the path that segfaults the SuiteAnalytics driver on wide
// tables. Long types are always non-bindable; bounded char/binary types are
// non-bindable only when their declared size is unknown (0) or exceeds the
// wrapper's bindable limit (mirrors column.go: NewVariableWidthColumn). When the
// catalog lacks a usable data_type, it falls back to the CLOB type name.
func isNonBindable(dataType, precision sql.NullInt64, isCLOB bool) bool {
	if !dataType.Valid {
		return isCLOB
	}
	switch dataType.Int64 {
	case sqlLongVarChar, sqlWLongVarChar, sqlLongVarBinary:
		return true
	case sqlChar, sqlVarChar, sqlWChar, sqlWVarChar, sqlBinary, sqlVarBinary:
		return !precision.Valid || precision.Int64 == 0 || precision.Int64 > bindableCharWidthLimit
	default:
		return false
	}
}

func isCharDataType(dataType sql.NullInt64) bool {
	if !dataType.Valid {
		return false
	}
	switch dataType.Int64 {
	case sqlChar, sqlVarChar, sqlWChar, sqlWVarChar, sqlLongVarChar, sqlWLongVarChar:
		return true
	default:
		return false
	}
}

// ODBC SQL type codes (sql.h / sqlext.h / sqlucode.h in the driver headers) used
// to classify catalog columns by how the ODBC wrapper fetches them.
const (
	sqlChar          = 1
	sqlVarChar       = 12
	sqlLongVarChar   = -1
	sqlBinary        = -2
	sqlVarBinary     = -3
	sqlLongVarBinary = -4
	sqlWChar         = -8
	sqlWVarChar      = -9
	sqlWLongVarChar  = -10
)

// unqualifyTableName strips any schema qualifier and surrounding quotes so the
// bare table name can be matched against oa_columns.table_name.
func unqualifyTableName(tableName string) string {
	name := strings.TrimSpace(tableName)
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	return strings.Trim(name, "\"")
}

func (s *NetSuiteSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	query = strings.TrimSpace(query)
	query = strings.TrimSuffix(query, ";")
	if query == "" {
		return nil, fmt.Errorf("netsuite custom query cannot be empty")
	}
	return s.readQuery(ctx, query, opts)
}

func (s *NetSuiteSource) readQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.db == nil {
		return nil, fmt.Errorf("netsuite source is not connected")
	}

	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = defaultConnectBatchSize
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		config.Debug("[NETSUITE] SuiteAnalytics Connect query: %s", query)
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to query NetSuite SuiteAnalytics Connect: %w", err)}
			return
		}

		readErr := streamRows(ctx, rows, pageSize, opts.Limit, opts.ExcludeColumns, results)
		closeErr := rows.Close()
		if readErr != nil {
			results <- source.RecordBatchResult{Err: readErr}
			return
		}
		if closeErr != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to close NetSuite SuiteAnalytics Connect rows: %w", closeErr)}
			return
		}
		if err := rows.Err(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read NetSuite SuiteAnalytics Connect rows: %w", err)}
		}
	}()

	return results, nil
}

func streamRows(ctx context.Context, rows *sql.Rows, pageSize, limit int, excludeColumns []string, results chan<- source.RecordBatchResult) error {
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to read NetSuite SuiteAnalytics Connect columns: %w", err)
	}
	columns = uniqueColumnNames(columns)

	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	batch := make([]map[string]interface{}, 0, pageSize)
	emitted := 0
	for rows.Next() {
		if limit > 0 && emitted >= limit {
			break
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return fmt.Errorf("failed to scan NetSuite SuiteAnalytics Connect row: %w", err)
		}

		item := make(map[string]interface{}, len(columns))
		for i, column := range columns {
			item[column] = normalizeSQLValue(values[i])
		}
		batch = append(batch, item)
		emitted++

		if len(batch) >= pageSize {
			if err := sendBatch(ctx, batch, excludeColumns, results); err != nil {
				return err
			}
			batch = make([]map[string]interface{}, 0, pageSize)
		}
	}

	if len(batch) > 0 {
		if err := sendBatch(ctx, batch, excludeColumns, results); err != nil {
			return err
		}
	}

	return nil
}

func sendBatch(ctx context.Context, rows []map[string]interface{}, excludeColumns []string, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, excludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record for netsuite query: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case results <- source.RecordBatchResult{Batch: record}:
		return nil
	}
}

func parseURI(rawURI string) (uriConfig, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return uriConfig{}, fmt.Errorf("invalid netsuite URI: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "netsuite" && scheme != "netsuite+odbc" {
		return uriConfig{}, fmt.Errorf("invalid netsuite URI: must start with netsuite:// or netsuite+odbc://")
	}

	values := parsed.Query()
	excludeCLOB := parseBool(firstNonEmpty(values.Get("exclude_clob_columns"), values.Get("skip_clob_columns")))
	wideMode, wideMax, err := parseWideTextOptions(values)
	if err != nil {
		return uriConfig{}, err
	}

	base := uriConfig{
		excludeCLOBColumns: excludeCLOB,
		wideTextMode:       wideMode,
		wideTextMaxChars:   wideMax,
	}

	connString := firstNonEmpty(values.Get("odbc_connect_string"), values.Get("connection_string"), values.Get("conn_str"))
	if connString != "" {
		base.connString = connString
		return base, nil
	}

	dsn := values.Get("dsn")
	accountID, roleID := resolveAccountAndRole(parsed, values, dsn)

	tba, err := extractTBACredentials(accountID, values)
	if err != nil {
		return uriConfig{}, err
	}
	base.tba = tba

	if dsn != "" {
		base.connString = buildDSNConnectionString(parsed, values, dsn, accountID, roleID, tba)
		return base, nil
	}

	connString, err = buildDriverConnectionString(parsed, values, accountID, roleID, tba)
	if err != nil {
		return uriConfig{}, err
	}
	base.connString = connString
	return base, nil
}

// parseWideTextOptions reads the wide_text strategy and its truncation width.
// wide_text controls how columns on the SuiteAnalytics driver's crashing
// chunked-fetch path are projected; see uriConfig.wideTextMode.
func parseWideTextOptions(values url.Values) (string, int, error) {
	mode := strings.ToLower(strings.TrimSpace(firstNonEmpty(values.Get("wide_text"), values.Get("wide_text_mode"))))
	if mode == "" {
		mode = wideTextKeep
	}
	switch mode {
	case wideTextKeep, wideTextTruncate, wideTextExclude:
	default:
		return "", 0, fmt.Errorf("invalid netsuite wide_text %q (expected keep, truncate, or exclude)", mode)
	}

	maxChars := defaultWideTextMaxChars
	if raw := firstNonEmpty(values.Get("wide_text_max_chars"), values.Get("wide_text_max_length")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return "", 0, fmt.Errorf("invalid netsuite wide_text_max_chars %q (expected a positive integer)", raw)
		}
		maxChars = n
	}
	// A SUBSTR wider than the wrapper's bindable limit would land back on the
	// crashing chunked path, defeating the purpose; clamp to keep it bindable.
	if maxChars > bindableCharWidthLimit {
		maxChars = bindableCharWidthLimit
	}
	return mode, maxChars, nil
}

// resolveAccountAndRole determines the effective account ID and role ID. Values
// supplied on the URI win; otherwise, when a DSN is used, they are read from the
// DSN's CustomProperties in the ODBC ini (so a configured DSN need not repeat
// them on the URI).
func resolveAccountAndRole(parsed *url.URL, values url.Values, dsn string) (string, string) {
	accountID := accountIDFromURI(parsed, values)
	roleID := values.Get("role_id")
	if dsn != "" && (accountID == "" || roleID == "") {
		props := dsnCustomProperties(dsn)
		if accountID == "" {
			accountID = props["AccountID"]
		}
		if roleID == "" {
			roleID = props["RoleID"]
		}
	}
	return accountID, roleID
}

func buildDSNConnectionString(parsed *url.URL, values url.Values, dsn, accountID, roleID string, tba *tbaCredentials) string {
	params := []odbcParam{{key: "DSN", value: dsn}}
	if tba == nil {
		params = appendCredentials(params, parsed, values)
	}
	// Role is not required on the DSN path: a configured DSN already supplies
	// AccountID/RoleID to the driver. We only emit CustomProperties when we have
	// values to set (URI overrides or ini-resolved).
	if customProperties := buildCustomProperties(accountID, roleID, values); customProperties != "" {
		params = append(params, odbcParam{key: "CustomProperties", value: customProperties})
	}
	return formatODBCConnectionString(params)
}

func buildDriverConnectionString(parsed *url.URL, values url.Values, accountID, roleID string, tba *tbaCredentials) (string, error) {
	driver := firstNonEmpty(values.Get("driver"), values.Get("driver_name"))
	if driver == "" {
		return "", fmt.Errorf("dsn, driver, or odbc_connect_string is required for netsuite SuiteAnalytics Connect over ODBC")
	}

	host := values.Get("host")
	if host == "" && parsed.Hostname() != "" && strings.Contains(parsed.Hostname(), ".") {
		host = parsed.Hostname()
	}
	if host == "" && accountID != "" {
		host = defaultConnectHost(accountID)
	}
	if host == "" {
		return "", fmt.Errorf("account_id or host is required for DSN-less netsuite SuiteAnalytics Connect over ODBC")
	}

	port := firstNonEmpty(values.Get("port"), parsed.Port(), defaultConnectPort)
	if _, err := strconv.Atoi(port); err != nil {
		return "", fmt.Errorf("invalid netsuite SuiteAnalytics Connect port %q", port)
	}

	serverDataSource := firstNonEmpty(values.Get("server_data_source"), values.Get("sdsn"), defaultServerDataSource)
	params := []odbcParam{
		{key: "DRIVER", value: normalizeDriverName(driver)},
		{key: "Host", value: host},
		{key: "Port", value: port},
		{key: "Encrypted", value: firstNonEmpty(values.Get("encrypted"), "1")},
		{key: "AllowSinglePacketLogout", value: firstNonEmpty(values.Get("allow_single_packet_logout"), values.Get("allow_single_packet_logout_enabled"), "1")},
		{key: "SDSN", value: serverDataSource},
	}
	if truststore := values.Get("truststore"); truststore != "" {
		params = append(params, odbcParam{key: "Truststore", value: truststore})
	}
	if tba == nil {
		params = appendCredentials(params, parsed, values)
	}

	// On the DSN-less driver path there is no DSN to supply the role, so it must
	// be provided when an account ID is present.
	if accountID != "" && roleID == "" && !containsODBCProperty(values.Get("custom_properties"), "RoleID") {
		return "", fmt.Errorf("role_id is required when account_id is provided for netsuite SuiteAnalytics Connect over ODBC")
	}

	if customProperties := buildCustomProperties(accountID, roleID, values); customProperties != "" {
		params = append(params, odbcParam{key: "CustomProperties", value: customProperties})
	}

	return formatODBCConnectionString(params), nil
}

func appendCredentials(params []odbcParam, parsed *url.URL, values url.Values) []odbcParam {
	username := firstNonEmpty(values.Get("username"), values.Get("user"))
	password := firstNonEmpty(values.Get("password"), values.Get("token_password"))
	if parsed.User != nil {
		username = firstNonEmpty(username, parsed.User.Username())
		if parsedPassword, ok := parsed.User.Password(); ok {
			password = firstNonEmpty(password, parsedPassword)
		}
	}
	if username != "" {
		params = append(params, odbcParam{key: "UID", value: username})
	}
	if password != "" {
		params = append(params, odbcParam{key: "PWD", value: password})
	}
	return params
}

func buildCustomProperties(accountID, roleID string, values url.Values) string {
	var properties []string
	extra := strings.Trim(values.Get("custom_properties"), "; ")

	if accountID != "" {
		properties = append(properties, "AccountID="+accountID)
	}
	if roleID != "" {
		properties = append(properties, "RoleID="+roleID)
	}
	if parseBool(values.Get("static_schema")) {
		properties = append(properties, "StaticSchema=1")
	}
	if parseBool(values.Get("uppercase")) {
		properties = append(properties, "Uppercase=1")
	}
	if extra != "" {
		properties = append(properties, extra)
	}

	return strings.Join(properties, ";")
}

func containsODBCProperty(properties, key string) bool {
	for _, property := range strings.Split(properties, ";") {
		name, _, ok := strings.Cut(strings.TrimSpace(property), "=")
		if ok && strings.EqualFold(strings.TrimSpace(name), key) {
			return true
		}
	}
	return false
}

func accountIDFromURI(parsed *url.URL, values url.Values) string {
	accountID := values.Get("account_id")
	if parsed.Host != "" && !strings.Contains(parsed.Hostname(), ".") && accountID == "" {
		accountID = parsed.Hostname()
	}
	return accountID
}

func formatODBCConnectionString(params []odbcParam) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		if param.value == "" {
			continue
		}
		parts = append(parts, param.key+"="+odbcValue(param.value))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ";") + ";"
}

func normalizeDriverName(driver string) string {
	driver = strings.TrimSpace(driver)
	if strings.HasPrefix(driver, "{") && strings.HasSuffix(driver, "}") {
		return driver
	}
	return "{" + driver + "}"
}

func odbcValue(value string) string {
	if strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") {
		return value
	}
	if strings.ContainsAny(value, ";{}") || strings.TrimSpace(value) != value {
		return "{" + strings.ReplaceAll(value, "}", "}}") + "}"
	}
	return value
}

func buildSuiteAnalyticsQuery(tableName string, columns []string, opts source.ReadOptions) string {
	// SuiteAnalytics Connect runs on the OpenAccess SDK SQL engine, which uses
	// SQL Server-style TOP for row limiting (FETCH FIRST is rejected).
	projection := "*"
	if len(columns) > 0 {
		projection = strings.Join(columns, ", ")
	}

	selectClause := "SELECT"
	if opts.Limit > 0 {
		selectClause = fmt.Sprintf("SELECT TOP %d", opts.Limit)
	}
	query := fmt.Sprintf("%s %s FROM %s", selectClause, projection, strings.TrimSpace(tableName))

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("%s >= %s", opts.IncrementalKey, suiteAnalyticsTimestampLiteral(*opts.IntervalStart)))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("%s < %s", opts.IncrementalKey, suiteAnalyticsTimestampLiteral(*opts.IntervalEnd)))
		}
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Intentionally no ORDER BY: ingestr does not checkpoint mid-stream, so the
	// interval-bounded merge/delete+insert/replace strategies don't depend on
	// row order. More importantly, ORDER BY over a wide result set (e.g. the
	// 685-column `transaction` table) crashes the SuiteAnalytics Connect ODBC
	// driver and forces a slow server-side sort.
	return query
}

func suiteAnalyticsTimestampLiteral(t time.Time) string {
	return fmt.Sprintf("TO_TIMESTAMP('%s', 'YYYY-MM-DD HH24:MI:SSxFF')", t.UTC().Format("2006-01-02 15:04:05.000000000"))
}

func defaultConnectHost(accountID string) string {
	normalized := strings.ToLower(strings.ReplaceAll(accountID, "_", "-"))
	return normalized + defaultConnectHostSuffix
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func uniqueColumnNames(columns []string) []string {
	seen := make(map[string]int, len(columns))
	names := make([]string, len(columns))
	for i, column := range columns {
		name := strings.TrimSpace(column)
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}

		key := strings.ToLower(name)
		count := seen[key]
		seen[key] = count + 1
		if count > 0 {
			candidate := fmt.Sprintf("%s_%d", name, count+1)
			for seen[strings.ToLower(candidate)] > 0 {
				count++
				candidate = fmt.Sprintf("%s_%d", name, count+1)
			}
			seen[strings.ToLower(candidate)]++
			name = candidate
		}
		names[i] = name
	}
	return names
}

func normalizeSQLValue(value interface{}) interface{} {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	case sql.RawBytes:
		return string(v)
	default:
		return value
	}
}

func odbcDriverRegistered() bool {
	for _, driver := range sql.Drivers() {
		if driver == "odbc" {
			return true
		}
	}
	return false
}

var _ source.Source = (*NetSuiteSource)(nil)
