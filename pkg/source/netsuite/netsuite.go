package netsuite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
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
)

type NetSuiteSource struct {
	db         *sql.DB
	connString string
}

type uriConfig struct {
	connString string
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

	db, err := sql.Open("odbc", cfg.connString)
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
	query := buildSuiteAnalyticsQuery(tableName, opts)
	return s.readQuery(ctx, query, opts)
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
		defer rows.Close()

		if err := streamRows(ctx, rows, pageSize, opts.Limit, opts.ExcludeColumns, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
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
	connString := firstNonEmpty(values.Get("odbc_connect_string"), values.Get("connection_string"), values.Get("conn_str"))
	if connString != "" {
		return uriConfig{connString: connString}, nil
	}

	if dsn := values.Get("dsn"); dsn != "" {
		connString, err := buildDSNConnectionString(parsed, values, dsn)
		if err != nil {
			return uriConfig{}, err
		}
		return uriConfig{connString: connString}, nil
	}

	connString, err = buildDriverConnectionString(parsed, values)
	if err != nil {
		return uriConfig{}, err
	}
	return uriConfig{connString: connString}, nil
}

func buildDSNConnectionString(parsed *url.URL, values url.Values, dsn string) (string, error) {
	params := []odbcParam{{key: "DSN", value: dsn}}
	params = appendCredentials(params, parsed, values)
	customProperties, err := buildCustomProperties(parsed, values)
	if err != nil {
		return "", err
	}
	if customProperties != "" {
		params = append(params, odbcParam{key: "CustomProperties", value: customProperties})
	}
	return formatODBCConnectionString(params), nil
}

func buildDriverConnectionString(parsed *url.URL, values url.Values) (string, error) {
	driver := firstNonEmpty(values.Get("driver"), values.Get("driver_name"))
	if driver == "" {
		return "", fmt.Errorf("dsn, driver, or odbc_connect_string is required for netsuite SuiteAnalytics Connect over ODBC")
	}

	accountID := accountIDFromURI(parsed, values)
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
	params = appendCredentials(params, parsed, values)

	customProperties, err := buildCustomProperties(parsed, values)
	if err != nil {
		return "", err
	}
	if customProperties != "" {
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

func buildCustomProperties(parsed *url.URL, values url.Values) (string, error) {
	var properties []string
	accountID := accountIDFromURI(parsed, values)
	roleID := values.Get("role_id")
	extra := strings.Trim(values.Get("custom_properties"), "; ")

	if accountID != "" && roleID == "" && !containsODBCProperty(extra, "RoleID") {
		return "", fmt.Errorf("role_id is required when account_id is provided for netsuite SuiteAnalytics Connect over ODBC")
	}

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

	return strings.Join(properties, ";"), nil
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

func buildSuiteAnalyticsQuery(tableName string, opts source.ReadOptions) string {
	query := fmt.Sprintf("SELECT * FROM %s", strings.TrimSpace(tableName))

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
		query += " ORDER BY " + opts.IncrementalKey + " ASC"
	}

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
			name = fmt.Sprintf("%s_%d", name, count+1)
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
