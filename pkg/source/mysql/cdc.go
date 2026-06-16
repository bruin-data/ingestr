package mysql

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	gomysql "github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	mysqldriver "github.com/go-sql-driver/mysql"
)

type MySQLCDCMode string

const (
	MySQLCDCModeBatch  MySQLCDCMode = "batch"
	MySQLCDCModeStream MySQLCDCMode = "stream"

	defaultMySQLCDCHeartbeat       = 1 * time.Second
	defaultMySQLCDCStreamBatchSize = 10000
)

var mysqlCDCColumns = []schema.Column{
	{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
	{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
	{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
}

type MySQLCDCConfig struct {
	Mode       MySQLCDCMode
	DestSchema string
	ServerID   uint32
	Flavor     string
}

type mysqlCDCConnInfo struct {
	Host        string
	Port        uint16
	User        string
	Password    string
	Database    string
	TLSConfig   *tls.Config
	Charset     string
	ReadTimeout time.Duration
}

type MySQLCDCSource struct {
	db        *sql.DB
	uri       string
	database  string
	cdcConfig MySQLCDCConfig
	connInfo  mysqlCDCConnInfo
}

type mysqlCDCTableMetadata struct {
	Name         string
	SourceSchema string
	SourceName   string
	FullSchema   *schema.TableSchema
}

type MySQLCDCTable struct {
	source      *MySQLCDCSource
	tableName   string
	metadata    mysqlCDCTableMetadata
	tableSchema *schema.TableSchema
	primaryKeys []string
	strategy    config.IncrementalStrategy
}

type mysqlCDCChange struct {
	values  []interface{}
	lsn     string
	deleted bool
}

type mysqlCDCChangeBuffer struct {
	tableSchema *schema.TableSchema
	tableName   string
	changes     []mysqlCDCChange
}

func NewMySQLCDCSource() *MySQLCDCSource {
	return &MySQLCDCSource{}
}

func (s *MySQLCDCSource) Schemes() []string {
	return []string{"mysql+cdc", "mysql+pymysql+cdc", "mariadb+cdc"}
}

func (s *MySQLCDCSource) Connect(ctx context.Context, rawURI string) error {
	cdcConfig, normalizedURI, connInfo, err := parseMySQLCDCURI(rawURI)
	if err != nil {
		return fmt.Errorf("failed to parse MySQL CDC URI: %w", err)
	}
	if connInfo.Database == "" {
		return fmt.Errorf("MySQL CDC source URI must include a database")
	}

	dsn, database, err := uriToDSN(normalizedURI)
	if err != nil {
		return fmt.Errorf("failed to parse MySQL URI: %w", err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open MySQL connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping MySQL: %w", err)
	}

	if err := checkMySQLBinlogSettings(ctx, db); err != nil {
		_ = db.Close()
		return err
	}

	s.db = db
	s.uri = rawURI
	s.database = database
	s.cdcConfig = cdcConfig
	s.connInfo = connInfo
	return nil
}

func (s *MySQLCDCSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *MySQLCDCSource) HandlesIncrementality() bool {
	return true
}

func (s *MySQLCDCSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}

	fullSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	if err := validateMySQLCDCTableSupported(ctx, s.db, s.database, req.Name); err != nil {
		return nil, err
	}
	tableSchema := addMySQLCDCColumns(fullSchema)

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}
	if len(pks) == 0 {
		return nil, fmt.Errorf("MySQL CDC table %s has no primary key; provide --primary-key or add a primary key to the source table", req.Name)
	}
	tableSchema.PrimaryKeys = pks

	strategy := config.StrategyMerge
	if req.Strategy != "" && req.Strategy != config.StrategyReplace {
		strategy = req.Strategy
	}

	return &MySQLCDCTable{
		source:      s,
		tableName:   req.Name,
		metadata:    newMySQLCDCMetadata(req.Name, fullSchema, s.database),
		tableSchema: tableSchema,
		primaryKeys: pks,
		strategy:    strategy,
	}, nil
}

func (s *MySQLCDCSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return getMySQLSchema(ctx, s.db, s.database, table)
}

func (s *MySQLCDCSource) IsMultiTable() bool {
	return true
}

func (s *MySQLCDCSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_SCHEMA = ?
		  AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`, s.database)
	if err != nil {
		return nil, fmt.Errorf("failed to query MySQL tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []source.SourceTableInfo
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("failed to scan MySQL table: %w", err)
		}

		fullSchema, err := s.getSchema(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema for %s: %w", tableName, err)
		}
		if err := validateMySQLCDCTableSupported(ctx, s.db, s.database, tableName); err != nil {
			return nil, err
		}
		tableSchema := addMySQLCDCColumns(fullSchema)
		if len(tableSchema.PrimaryKeys) == 0 {
			return nil, fmt.Errorf("MySQL CDC table %s has no primary key; multi-table CDC requires source primary keys", tableName)
		}

		tables = append(tables, source.SourceTableInfo{
			Name:        tableName,
			Schema:      tableSchema,
			PrimaryKeys: tableSchema.PrimaryKeys,
			DestSchema:  s.cdcConfig.DestSchema,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("no MySQL tables found in database %s", s.database)
	}
	return tables, nil
}

func (s *MySQLCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	allTables, err := s.GetTables(ctx)
	if err != nil {
		return nil, err
	}

	filter := map[string]bool{}
	for _, table := range opts.Tables {
		filter[strings.ToLower(table)] = true
	}

	selected := make([]source.SourceTableInfo, 0, len(allTables))
	for _, table := range allTables {
		if len(filter) > 0 && !filter[strings.ToLower(table.Name)] {
			continue
		}
		selected = append(selected, table)
	}

	results := make(chan source.RecordBatchResult, 16)
	go func() {
		defer close(results)

		startByTable := make(map[string]gomysql.Position, len(selected))
		metaByTable := make(map[string]mysqlCDCTableMetadata, len(selected))

		for _, table := range selected {
			fullSchema := destination.DestinationTableSchema(table.Schema)
			fullSchema = removeMySQLCDCColumns(fullSchema)
			meta := newMySQLCDCMetadata(table.Name, fullSchema, s.database)
			metaByTable[table.Name] = meta

			storedLSN := strings.TrimSpace(opts.CDCResumeLSNs[table.Name])
			if resume, ok := parseStoredMySQLPosition(storedLSN); ok {
				canResume, err := s.canResume(ctx, resume)
				if err != nil {
					results <- source.RecordBatchResult{Err: err}
					return
				}
				if canResume {
					startByTable[table.Name] = resume
					continue
				}
				results <- source.RecordBatchResult{Err: mysqlCDCResumeExpiredError(table.Name, resume)}
				return
			} else if storedLSN != "" {
				results <- source.RecordBatchResult{Err: mysqlCDCResumeInvalidError(table.Name, storedLSN)}
				return
			}

			snapshotPos, err := s.snapshotTable(ctx, meta, table.Schema, opts.ReadOptions, results, table.Name)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed for %s: %w", table.Name, err)}
				return
			}
			startByTable[table.Name] = snapshotPos
		}

		if err := s.streamTables(ctx, selected, metaByTable, startByTable, opts.ReadOptions, results, true); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func parseMySQLCDCURI(rawURI string) (MySQLCDCConfig, string, mysqlCDCConnInfo, error) {
	cfg := MySQLCDCConfig{
		Mode:     MySQLCDCModeBatch,
		ServerID: randomMySQLServerID(),
		Flavor:   gomysql.MySQLFlavor,
	}

	parsed, err := url.Parse(rawURI)
	if err != nil {
		return cfg, "", mysqlCDCConnInfo{}, err
	}

	baseScheme, ok := strings.CutSuffix(strings.ToLower(parsed.Scheme), "+cdc")
	if !ok {
		return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("unsupported MySQL CDC scheme: %s", parsed.Scheme)
	}
	switch baseScheme {
	case "mysql", "mysql+pymysql":
		parsed.Scheme = baseScheme
	case "mariadb":
		parsed.Scheme = "mariadb"
		cfg.Flavor = gomysql.MariaDBFlavor
	default:
		return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("unsupported MySQL CDC scheme: %s", parsed.Scheme)
	}

	query := parsed.Query()
	cfg.DestSchema = query.Get("dest_schema")
	if flavor := strings.ToLower(strings.TrimSpace(query.Get("flavor"))); flavor != "" {
		if err := gomysql.ValidateFlavor(flavor); err != nil {
			return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("invalid flavor: %w", err)
		}
		cfg.Flavor = flavor
	}
	if mode := strings.ToLower(strings.TrimSpace(query.Get("mode"))); mode != "" {
		switch mode {
		case string(MySQLCDCModeBatch):
			cfg.Mode = MySQLCDCModeBatch
		case string(MySQLCDCModeStream):
			return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("MySQL CDC stream mode is not supported; use mode=batch")
		default:
			return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("invalid mode: %s (must be 'batch')", mode)
		}
	}
	if rawServerID := strings.TrimSpace(query.Get("server_id")); rawServerID != "" {
		serverID, err := strconv.ParseUint(rawServerID, 10, 32)
		if err != nil || serverID == 0 {
			return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("server_id must be a positive uint32")
		}
		cfg.ServerID = uint32(serverID)
	}

	query.Del("dest_schema")
	query.Del("flavor")
	query.Del("mode")
	query.Del("server_id")
	parsed.RawQuery = query.Encode()

	normalizedURI := parsed.String()
	connInfo, err := mysqlCDCConnInfoFromURI(normalizedURI)
	if err != nil {
		return cfg, "", mysqlCDCConnInfo{}, err
	}

	return cfg, normalizedURI, connInfo, nil
}

func mysqlCDCConnInfoFromURI(normalizedURI string) (mysqlCDCConnInfo, error) {
	dsn, _, err := uriToDSN(normalizedURI)
	if err != nil {
		return mysqlCDCConnInfo{}, err
	}
	driverCfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return mysqlCDCConnInfo{}, fmt.Errorf("failed to parse MySQL CDC connection parameters: %w", err)
	}
	parsed, err := url.Parse(normalizedURI)
	if err != nil {
		return mysqlCDCConnInfo{}, err
	}

	port := uint64(3306)
	if rawPort := parsed.Port(); rawPort != "" {
		parsedPort, err := strconv.ParseUint(rawPort, 10, 16)
		if err != nil {
			return mysqlCDCConnInfo{}, fmt.Errorf("invalid port: %w", err)
		}
		port = parsedPort
	}

	password := ""
	if parsed.User != nil {
		password, _ = parsed.User.Password()
	}

	connInfo := mysqlCDCConnInfo{
		Host:        parsed.Hostname(),
		Port:        uint16(port),
		Password:    password,
		Database:    strings.TrimPrefix(parsed.Path, "/"),
		TLSConfig:   driverCfg.TLS,
		Charset:     firstMySQLCDCCharset(parsed.Query().Get("charset")),
		ReadTimeout: driverCfg.ReadTimeout,
	}
	if parsed.User != nil {
		connInfo.User = parsed.User.Username()
	}
	return connInfo, nil
}

func firstMySQLCDCCharset(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	first, _, _ := strings.Cut(raw, ",")
	return strings.TrimSpace(first)
}

func randomMySQLServerID() uint32 {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err == nil {
		id := binary.BigEndian.Uint32(buf[:])
		if id != 0 {
			return id
		}
	}
	return uint32(time.Now().UnixNano() & 0xffffffff)
}

func checkMySQLBinlogSettings(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SHOW VARIABLES
		WHERE Variable_name IN ('log_bin', 'binlog_format', 'binlog_row_image', 'binlog_row_value_options')
	`)
	if err != nil {
		return fmt.Errorf("failed to check MySQL binlog settings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	settings := map[string]string{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return fmt.Errorf("failed to scan MySQL binlog setting: %w", err)
		}
		settings[strings.ToLower(name)] = strings.ToUpper(value)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if logBin := settings["log_bin"]; logBin != "ON" && logBin != "1" {
		return fmt.Errorf("MySQL CDC requires binary logging to be enabled (log_bin=ON)")
	}
	if settings["binlog_format"] != "ROW" {
		return fmt.Errorf("MySQL CDC requires binlog_format=ROW (current: %s)", settings["binlog_format"])
	}
	if rowImage := settings["binlog_row_image"]; rowImage != "" && rowImage != "FULL" {
		return fmt.Errorf("MySQL CDC requires binlog_row_image=FULL (current: %s)", rowImage)
	}
	if rowValueOptions := settings["binlog_row_value_options"]; strings.Contains(rowValueOptions, "PARTIAL_JSON") {
		return fmt.Errorf("MySQL CDC does not support binlog_row_value_options=PARTIAL_JSON")
	}
	return nil
}

func validateMySQLCDCTableSupported(ctx context.Context, db *sql.DB, database string, table string) error {
	schemaName, tableName := parseMySQLTableName(database, table)
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME, DATA_TYPE
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		  AND LOWER(DATA_TYPE) IN ('enum', 'set', 'bit')
		ORDER BY ORDINAL_POSITION
	`, schemaName, tableName)
	if err != nil {
		return fmt.Errorf("failed to check MySQL CDC column support: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var unsupported []string
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return fmt.Errorf("failed to scan MySQL CDC column support: %w", err)
		}
		unsupported = append(unsupported, fmt.Sprintf("%s %s", name, strings.ToUpper(dataType)))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("MySQL CDC does not support ENUM, SET, or BIT columns yet; unsupported columns in %s: %s", table, strings.Join(unsupported, ", "))
	}
	return nil
}

func mysqlCDCResumeExpiredError(table string, pos gomysql.Position) error {
	return fmt.Errorf("MySQL CDC resume position %s for %s is no longer available in binary logs; run with --full-refresh to rebuild the destination safely", pos, table)
}

func mysqlCDCResumeInvalidError(table string, stored string) error {
	return fmt.Errorf("MySQL CDC resume position %q for %s is invalid; run with --full-refresh to rebuild the destination safely", stored, table)
}

func (t *MySQLCDCTable) Name() string {
	return t.tableName
}

func (t *MySQLCDCTable) PrimaryKeys() []string {
	return t.primaryKeys
}

func (t *MySQLCDCTable) IncrementalKey() string {
	return ""
}

func (t *MySQLCDCTable) Strategy() config.IncrementalStrategy {
	return t.strategy
}

func (t *MySQLCDCTable) HasKnownSchema() bool {
	return true
}

func (t *MySQLCDCTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.tableSchema, nil
}

func (t *MySQLCDCTable) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	outputSchema := t.tableSchema
	if opts.Schema != nil {
		outputSchema = opts.Schema
	}

	go func() {
		defer close(results)

		storedLSN := strings.TrimSpace(opts.CDCResumeLSN)
		if resume, ok := parseStoredMySQLPosition(storedLSN); ok {
			canResume, err := t.source.canResume(ctx, resume)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			if canResume {
				internalName := t.metadata.Name
				startByTable := map[string]gomysql.Position{internalName: resume}
				metaByTable := map[string]mysqlCDCTableMetadata{internalName: t.metadata}
				tableInfo := source.SourceTableInfo{
					Name:        internalName,
					Schema:      outputSchema,
					PrimaryKeys: outputSchema.PrimaryKeys,
				}
				if err := t.source.streamTables(ctx, []source.SourceTableInfo{tableInfo}, metaByTable, startByTable, opts, results, false); err != nil {
					results <- source.RecordBatchResult{Err: err}
				}
				return
			}
			results <- source.RecordBatchResult{Err: mysqlCDCResumeExpiredError(t.tableName, resume)}
			return
		} else if storedLSN != "" {
			results <- source.RecordBatchResult{Err: mysqlCDCResumeInvalidError(t.tableName, storedLSN)}
			return
		}

		snapshotPos, err := t.source.snapshotTable(ctx, t.metadata, outputSchema, opts, results, "")
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)}
			return
		}

		internalName := t.metadata.Name
		startByTable := map[string]gomysql.Position{internalName: snapshotPos}
		metaByTable := map[string]mysqlCDCTableMetadata{internalName: t.metadata}
		tableInfo := source.SourceTableInfo{
			Name:        internalName,
			Schema:      outputSchema,
			PrimaryKeys: outputSchema.PrimaryKeys,
		}
		if err := t.source.streamTables(ctx, []source.SourceTableInfo{tableInfo}, metaByTable, startByTable, opts, results, false); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *MySQLCDCSource) canResume(ctx context.Context, pos gomysql.Position) (bool, error) {
	rows, err := s.db.QueryContext(ctx, "SHOW BINARY LOGS")
	if err != nil {
		rows, err = s.db.QueryContext(ctx, "SHOW MASTER LOGS")
	}
	if err != nil {
		return false, fmt.Errorf("failed to query MySQL binary logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		cols, err := scanStrings(rows)
		if err != nil {
			return false, err
		}
		if len(cols) < 2 {
			continue
		}
		if cols[0] != pos.Name {
			continue
		}
		size, err := strconv.ParseUint(cols[1], 10, 32)
		if err != nil {
			return false, fmt.Errorf("failed to parse binary log size %q: %w", cols[1], err)
		}
		return uint64(pos.Pos) <= size, nil
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func (s *MySQLCDCSource) snapshotTable(ctx context.Context, meta mysqlCDCTableMetadata, outputSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string) (gomysql.Position, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to acquire MySQL connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	snapshotPos, err := beginMySQLConsistentSnapshot(ctx, conn)
	if err != nil {
		return gomysql.Position{}, err
	}

	sourceColumns := sourceColumnsWithoutMySQLCDC(outputSchema)
	query := buildMySQLCDCSnapshotQuery(meta, sourceColumns)
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to query snapshot for %s: %w", meta.Name, err)
	}

	if err := rowsToMySQLCDCSnapshotBatches(rows, outputSchema, opts, snapshotPos, results, resultTable); err != nil {
		_ = rows.Close()
		return gomysql.Position{}, err
	}
	if err := rows.Close(); err != nil {
		return gomysql.Position{}, err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to commit MySQL snapshot transaction: %w", err)
	}
	committed = true

	return snapshotPos, nil
}

func beginMySQLConsistentSnapshot(ctx context.Context, conn mysqlCDCSnapshotConn) (gomysql.Position, error) {
	if _, err := conn.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to set MySQL snapshot isolation: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "FLUSH TABLES WITH READ LOCK"); err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to lock MySQL tables for snapshot: %w", err)
	}

	locked := true
	defer func() {
		if locked {
			_, _ = conn.ExecContext(context.Background(), "UNLOCK TABLES")
		}
	}()

	if _, err := conn.ExecContext(ctx, "START TRANSACTION WITH CONSISTENT SNAPSHOT"); err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to start MySQL snapshot transaction: %w", err)
	}

	snapshotPos, err := currentMySQLBinlogPosition(ctx, conn)
	if err != nil {
		return gomysql.Position{}, err
	}

	if _, err := conn.ExecContext(ctx, "UNLOCK TABLES"); err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to unlock MySQL tables after snapshot: %w", err)
	}
	locked = false

	return snapshotPos, nil
}

func (s *MySQLCDCSource) streamTables(ctx context.Context, tables []source.SourceTableInfo, metas map[string]mysqlCDCTableMetadata, startByTable map[string]gomysql.Position, opts source.ReadOptions, results chan<- source.RecordBatchResult, tagResults bool) error {
	if len(tables) == 0 {
		return nil
	}

	target, err := currentMySQLBinlogPosition(ctx, s.db)
	if err != nil {
		return err
	}
	start := minMySQLPosition(startByTable)
	if start.Name == "" {
		return nil
	}
	if s.cdcConfig.Mode == MySQLCDCModeBatch && start.Compare(target) >= 0 {
		return nil
	}

	syncer := s.newBinlogSyncer()
	streamer, err := syncer.StartSync(start)
	if err != nil {
		return fmt.Errorf("failed to start MySQL binlog stream at %s: %w", start, err)
	}
	defer syncer.Close()

	metaByAlias := make(map[string]mysqlCDCTableMetadata)
	schemaByTable := make(map[string]*schema.TableSchema, len(tables))
	for _, table := range tables {
		meta := metas[table.Name]
		for _, alias := range meta.aliases(s.database) {
			metaByAlias[strings.ToLower(alias)] = meta
		}
		schemaByTable[table.Name] = table.Schema
	}

	current := start
	batchSize := mysqlCDCStreamBatchSize(opts)
	buffers := make(map[string]*mysqlCDCChangeBuffer, len(tables))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if s.cdcConfig.Mode == MySQLCDCModeBatch && current.Compare(target) >= 0 {
			return flushMySQLCDCChangeBuffers(buffers, results)
		}

		eventCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		event, err := streamer.GetEvent(eventCtx)
		cancel()
		if err != nil {
			if eventCtx.Err() != nil {
				current = nextMySQLPosition(syncer, current)
				continue
			}
			return fmt.Errorf("failed to read MySQL binlog event: %w", err)
		}
		if event == nil {
			current = nextMySQLPosition(syncer, current)
			continue
		}

		if event.Header != nil && event.Header.EventType == replication.PARTIAL_UPDATE_ROWS_EVENT {
			return fmt.Errorf("MySQL CDC does not support PARTIAL_UPDATE_ROWS_EVENT; disable binlog_row_value_options=PARTIAL_JSON")
		}

		eventPos := mysqlEventPosition(event, current, syncer)
		current = eventPos
		if s.cdcConfig.Mode == MySQLCDCModeBatch && eventPos.Compare(target) > 0 {
			return flushMySQLCDCChangeBuffers(buffers, results)
		}

		rowsEvent, ok := event.Event.(*replication.RowsEvent)
		if !ok {
			continue
		}
		tableKey := mysqlCDCEventTableName(string(rowsEvent.Table.Schema), string(rowsEvent.Table.Table), s.database)
		meta, ok := metaByAlias[strings.ToLower(tableKey)]
		if !ok {
			continue
		}

		startForTable := startByTable[meta.Name]
		if eventPos.Compare(startForTable) <= 0 {
			continue
		}

		outputSchema := schemaByTable[meta.Name]
		if outputSchema == nil {
			continue
		}
		changes, err := mysqlRowsEventToChanges(rowsEvent, meta.FullSchema, outputSchema, eventPos)
		if err != nil {
			return fmt.Errorf("failed to decode MySQL CDC rows for %s: %w", meta.Name, err)
		}
		if len(changes) == 0 {
			continue
		}

		resultTable := mysqlCDCResultTableName(meta.Name, len(tables), tagResults)
		if err := appendMySQLCDCBufferedChanges(buffers, meta.Name, outputSchema, resultTable, changes, batchSize, results); err != nil {
			return err
		}
	}
}

func (s *MySQLCDCSource) newBinlogSyncer() *replication.BinlogSyncer {
	return replication.NewBinlogSyncer(s.binlogSyncerConfig())
}

func (s *MySQLCDCSource) binlogSyncerConfig() replication.BinlogSyncerConfig {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return replication.BinlogSyncerConfig{
		ServerID:        s.cdcConfig.ServerID,
		Flavor:          s.cdcConfig.Flavor,
		Host:            s.connInfo.Host,
		Port:            s.connInfo.Port,
		User:            s.connInfo.User,
		Password:        s.connInfo.Password,
		Charset:         s.connInfo.Charset,
		TLSConfig:       s.connInfo.TLSConfig,
		ParseTime:       true,
		UseDecimal:      true,
		HeartbeatPeriod: defaultMySQLCDCHeartbeat,
		ReadTimeout:     s.connInfo.ReadTimeout,
		Logger:          logger,
		FillZeroLogPos:  s.cdcConfig.Flavor == gomysql.MariaDBFlavor,
	}
}

func mysqlRowsEventToChanges(event *replication.RowsEvent, fullSchema *schema.TableSchema, outputSchema *schema.TableSchema, pos gomysql.Position) ([]mysqlCDCChange, error) {
	fullSourceColumns := sourceColumnsWithoutMySQLCDC(fullSchema)
	outputSourceColumns := sourceColumnsWithoutMySQLCDC(outputSchema)
	indexes, err := sourceIndexes(fullSourceColumns, outputSourceColumns)
	if err != nil {
		return nil, err
	}

	pkIndexes := primaryKeyIndexes(fullSourceColumns, outputSchema.PrimaryKeys)
	eventType := event.Type()
	changes := make([]mysqlCDCChange, 0, len(event.Rows))
	rowSeq := 0

	switch eventType {
	case replication.EnumRowsEventTypeInsert:
		for _, row := range event.Rows {
			values, err := projectMySQLCDCRow(row, fullSourceColumns, outputSourceColumns, indexes)
			if err != nil {
				return nil, err
			}
			changes = append(changes, mysqlCDCChange{values: values, lsn: formatStoredMySQLPosition(pos, rowSeq), deleted: false})
			rowSeq++
		}
	case replication.EnumRowsEventTypeDelete:
		for _, row := range event.Rows {
			values, err := projectMySQLCDCRow(row, fullSourceColumns, outputSourceColumns, indexes)
			if err != nil {
				return nil, err
			}
			changes = append(changes, mysqlCDCChange{values: values, lsn: formatStoredMySQLPosition(pos, rowSeq), deleted: true})
			rowSeq++
		}
	case replication.EnumRowsEventTypeUpdate:
		if len(event.Rows)%2 != 0 {
			return nil, fmt.Errorf("update rows event has odd row count")
		}
		for i := 0; i < len(event.Rows); i += 2 {
			before := event.Rows[i]
			after := event.Rows[i+1]
			if primaryKeyChanged(before, after, pkIndexes) {
				beforeValues, err := projectMySQLCDCRow(before, fullSourceColumns, outputSourceColumns, indexes)
				if err != nil {
					return nil, err
				}
				changes = append(changes, mysqlCDCChange{values: beforeValues, lsn: formatStoredMySQLPosition(pos, rowSeq), deleted: true})
				rowSeq++
			}
			afterValues, err := projectMySQLCDCRow(after, fullSourceColumns, outputSourceColumns, indexes)
			if err != nil {
				return nil, err
			}
			changes = append(changes, mysqlCDCChange{values: afterValues, lsn: formatStoredMySQLPosition(pos, rowSeq), deleted: false})
			rowSeq++
		}
	default:
		return nil, fmt.Errorf("unsupported MySQL rows event type: %s", eventType)
	}

	return changes, nil
}

func projectMySQLCDCRow(row []interface{}, fullSourceColumns []schema.Column, outputSourceColumns []schema.Column, indexes []int) ([]interface{}, error) {
	if len(row) < len(fullSourceColumns) {
		return nil, fmt.Errorf("row image has %d columns, expected %d; set binlog_row_image=FULL", len(row), len(fullSourceColumns))
	}

	values := make([]interface{}, len(outputSourceColumns))
	for i, sourceIdx := range indexes {
		if sourceIdx < 0 || sourceIdx >= len(row) {
			values[i] = nil
			continue
		}
		values[i] = convertMySQLCDCValue(row[sourceIdx], outputSourceColumns[i])
	}
	return values, nil
}

func convertMySQLCDCValue(value interface{}, col schema.Column) interface{} {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []byte:
		if col.DataType == schema.TypeBinary {
			copied := make([]byte, len(v))
			copy(copied, v)
			return copied
		}
		return string(v)
	default:
		return v
	}
}

func primaryKeyChanged(before, after []interface{}, pkIndexes []int) bool {
	for _, idx := range pkIndexes {
		if idx < 0 || idx >= len(before) || idx >= len(after) {
			continue
		}
		if fmt.Sprintf("%T:%v", before[idx], before[idx]) != fmt.Sprintf("%T:%v", after[idx], after[idx]) {
			return true
		}
	}
	return false
}

func primaryKeyIndexes(columns []schema.Column, primaryKeys []string) []int {
	indexes := make([]int, 0, len(primaryKeys))
	for _, pk := range primaryKeys {
		for i, col := range columns {
			if strings.EqualFold(col.Name, pk) {
				indexes = append(indexes, i)
				break
			}
		}
	}
	return indexes
}

func sourceIndexes(fullSourceColumns []schema.Column, outputSourceColumns []schema.Column) ([]int, error) {
	byName := make(map[string]int, len(fullSourceColumns))
	for i, col := range fullSourceColumns {
		byName[strings.ToLower(col.Name)] = i
	}

	indexes := make([]int, len(outputSourceColumns))
	for i, col := range outputSourceColumns {
		idx, ok := byName[strings.ToLower(col.Name)]
		if !ok {
			return nil, fmt.Errorf("source column %s not found in current MySQL table schema", col.Name)
		}
		indexes[i] = idx
	}
	return indexes, nil
}

func rowsToMySQLCDCSnapshotBatches(rows *sql.Rows, tableSchema *schema.TableSchema, opts source.ReadOptions, snapshotPos gomysql.Position, results chan<- source.RecordBatchResult, resultTable string) error {
	sourceColumns := sourceColumnsWithoutMySQLCDC(tableSchema)
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}
	cdcLSN := formatStoredMySQLPosition(snapshotPos, 0)
	syncedAt := time.Now().UTC()

	for {
		record, count, err := buildMySQLCDCSQLBatch(rows, tableSchema, sourceColumns, batchSize, func(builders []array.Builder) {
			appendMySQLCDCValues(builders, len(sourceColumns), cdcLSN, false, syncedAt)
		})
		if err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		results <- source.RecordBatchResult{Batch: record, TableName: resultTable}
	}
}

func buildMySQLCDCSQLBatch(rows *sql.Rows, tableSchema *schema.TableSchema, sourceColumns []schema.Column, batchSize int, appendCDC func([]array.Builder)) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	arrowSchema := buildArrowSchema(tableSchema.Columns)

	builders := make([]array.Builder, len(tableSchema.Columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	scanDest := make([]interface{}, len(sourceColumns))
	for i := range scanDest {
		scanDest[i] = new(interface{})
	}

	var rowCount int64
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			releaseMySQLCDCBuilders(builders)
			return nil, 0, fmt.Errorf("failed to scan MySQL snapshot row: %w", err)
		}

		for i, dest := range scanDest {
			arrowconv.AppendValue(builders[i], convertMySQLCDCValue(*dest.(*interface{}), sourceColumns[i]))
		}
		appendCDC(builders)

		rowCount++
		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	return finishMySQLCDCBatch(rows, arrowSchema, builders, rowCount)
}

func mysqlCDCChangesToBatch(changes []mysqlCDCChange, tableSchema *schema.TableSchema) (arrow.RecordBatch, error) {
	sourceColumns := sourceColumnsWithoutMySQLCDC(tableSchema)
	mem := memory.NewGoAllocator()
	arrowSchema := buildArrowSchema(tableSchema.Columns)

	builders := make([]array.Builder, len(tableSchema.Columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	syncedAt := time.Now().UTC()
	for i, change := range changes {
		for colIdx := range sourceColumns {
			var val interface{}
			if colIdx < len(change.values) {
				val = change.values[colIdx]
			}
			arrowconv.AppendValue(builders[colIdx], val)
		}
		appendMySQLCDCValues(builders, len(sourceColumns), change.lsn, change.deleted, syncedAt.Add(time.Duration(i)*time.Microsecond))
	}

	record, _, err := finishMySQLCDCBatch(nil, arrowSchema, builders, int64(len(changes)))
	return record, err
}

func mysqlCDCStreamBatchSize(opts source.ReadOptions) int {
	if opts.PageSize > 0 {
		return opts.PageSize
	}
	return defaultMySQLCDCStreamBatchSize
}

func appendMySQLCDCBufferedChanges(buffers map[string]*mysqlCDCChangeBuffer, key string, tableSchema *schema.TableSchema, tableName string, changes []mysqlCDCChange, batchSize int, results chan<- source.RecordBatchResult) error {
	if len(changes) == 0 {
		return nil
	}
	buffer := buffers[key]
	if buffer == nil {
		buffer = &mysqlCDCChangeBuffer{
			tableSchema: tableSchema,
			tableName:   tableName,
		}
		buffers[key] = buffer
	}
	buffer.changes = append(buffer.changes, changes...)
	if len(buffer.changes) < batchSize {
		return nil
	}
	return buffer.flush(results)
}

func flushMySQLCDCChangeBuffers(buffers map[string]*mysqlCDCChangeBuffer, results chan<- source.RecordBatchResult) error {
	for _, buffer := range buffers {
		if err := buffer.flush(results); err != nil {
			return err
		}
	}
	return nil
}

func (b *mysqlCDCChangeBuffer) flush(results chan<- source.RecordBatchResult) error {
	if b == nil || len(b.changes) == 0 {
		return nil
	}
	record, err := mysqlCDCChangesToBatch(b.changes, b.tableSchema)
	if err != nil {
		return err
	}
	results <- source.RecordBatchResult{Batch: record, TableName: b.tableName}
	b.changes = b.changes[:0]
	return nil
}

func finishMySQLCDCBatch(rows *sql.Rows, arrowSchema *arrow.Schema, builders []array.Builder, rowCount int64) (arrow.RecordBatch, int64, error) {
	if rowCount == 0 {
		releaseMySQLCDCBuilders(builders)
		return nil, 0, nil
	}

	if rows != nil {
		if err := rows.Err(); err != nil {
			releaseMySQLCDCBuilders(builders)
			return nil, 0, fmt.Errorf("error iterating MySQL rows: %w", err)
		}
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
	}
	record := array.NewRecordBatch(arrowSchema, arrays, rowCount)
	for _, arr := range arrays {
		arr.Release()
	}
	releaseMySQLCDCBuilders(builders)
	return record, rowCount, nil
}

func releaseMySQLCDCBuilders(builders []array.Builder) {
	for _, builder := range builders {
		builder.Release()
	}
}

func appendMySQLCDCValues(builders []array.Builder, offset int, lsn string, deleted bool, syncedAt time.Time) {
	builders[offset].(*array.StringBuilder).Append(lsn)
	builders[offset+1].(*array.BooleanBuilder).Append(deleted)
	builders[offset+2].(*array.TimestampBuilder).Append(arrow.Timestamp(syncedAt.UnixMicro()))
}

type mysqlCDCPositionQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

type mysqlCDCSnapshotConn interface {
	mysqlCDCPositionQueryer
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func currentMySQLBinlogPosition(ctx context.Context, q mysqlCDCPositionQueryer) (gomysql.Position, error) {
	pos, ok, err := currentMySQLBinlogPositionFromStatement(ctx, q, "SHOW BINARY LOG STATUS")
	if err == nil && ok {
		return pos, nil
	}
	pos, ok, fallbackErr := currentMySQLBinlogPositionFromStatement(ctx, q, "SHOW MASTER STATUS")
	if fallbackErr == nil && ok {
		return pos, nil
	}
	if err != nil && fallbackErr != nil {
		return gomysql.Position{}, fmt.Errorf("failed to query MySQL binlog position: %w", fallbackErr)
	}
	return gomysql.Position{}, fmt.Errorf("MySQL binary logging is enabled but no current binlog position was returned")
}

func currentMySQLBinlogPositionFromStatement(ctx context.Context, q mysqlCDCPositionQueryer, stmt string) (gomysql.Position, bool, error) {
	rows, err := q.QueryContext(ctx, stmt)
	if err != nil {
		return gomysql.Position{}, false, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return gomysql.Position{}, false, rows.Err()
	}
	values, err := scanStrings(rows)
	if err != nil {
		return gomysql.Position{}, false, err
	}
	if len(values) < 2 || values[0] == "" {
		return gomysql.Position{}, false, nil
	}
	pos, err := strconv.ParseUint(values[1], 10, 32)
	if err != nil {
		return gomysql.Position{}, false, fmt.Errorf("failed to parse MySQL binlog position %q: %w", values[1], err)
	}
	return gomysql.Position{Name: values[0], Pos: uint32(pos)}, true, nil
}

func scanStrings(rows *sql.Rows) ([]string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]sql.NullString, len(cols))
	dest := make([]interface{}, len(cols))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	out := make([]string, len(values))
	for i, value := range values {
		if value.Valid {
			out[i] = value.String
		}
	}
	return out, nil
}

func buildMySQLCDCSnapshotQuery(meta mysqlCDCTableMetadata, columns []schema.Column) string {
	selects := make([]string, len(columns))
	for i, col := range columns {
		selects[i] = quoteColumn(col.Name)
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(selects, ", "), quoteTable(meta.SourceSchema+"."+meta.SourceName))
}

func newMySQLCDCMetadata(name string, fullSchema *schema.TableSchema, defaultDatabase string) mysqlCDCTableMetadata {
	schemaName, tableName := parseMySQLCDCTableName(name, defaultDatabase)
	return mysqlCDCTableMetadata{
		Name:         mysqlCDCEventTableName(schemaName, tableName, defaultDatabase),
		SourceSchema: schemaName,
		SourceName:   tableName,
		FullSchema:   addMySQLCDCColumns(fullSchema),
	}
}

func (m mysqlCDCTableMetadata) aliases(defaultDatabase string) []string {
	fullName := m.SourceSchema + "." + m.SourceName
	shortName := mysqlCDCEventTableName(m.SourceSchema, m.SourceName, defaultDatabase)
	if strings.EqualFold(fullName, shortName) {
		return []string{m.Name, fullName}
	}
	return []string{m.Name, shortName, fullName}
}

func parseMySQLCDCTableName(table string, defaultDatabase string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return defaultDatabase, table
}

func mysqlCDCEventTableName(schemaName string, tableName string, defaultDatabase string) string {
	if schemaName == "" || strings.EqualFold(schemaName, defaultDatabase) {
		return tableName
	}
	return schemaName + "." + tableName
}

func mysqlCDCResultTableName(tableName string, tableCount int, tagResults bool) string {
	if !tagResults && tableCount <= 1 {
		return ""
	}
	return tableName
}

func addMySQLCDCColumns(tableSchema *schema.TableSchema) *schema.TableSchema {
	copied := *tableSchema
	copied.Columns = append(append([]schema.Column{}, tableSchema.Columns...), mysqlCDCColumns...)
	return &copied
}

func removeMySQLCDCColumns(tableSchema *schema.TableSchema) *schema.TableSchema {
	if tableSchema == nil {
		return nil
	}
	copied := *tableSchema
	columns := make([]schema.Column, 0, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		if destination.IsCDCMetaColumn(col.Name) {
			continue
		}
		columns = append(columns, col)
	}
	copied.Columns = columns
	return &copied
}

func sourceColumnsWithoutMySQLCDC(tableSchema *schema.TableSchema) []schema.Column {
	if tableSchema == nil || len(tableSchema.Columns) < len(mysqlCDCColumns) {
		return nil
	}
	return tableSchema.Columns[:len(tableSchema.Columns)-len(mysqlCDCColumns)]
}

func minMySQLPosition(positions map[string]gomysql.Position) gomysql.Position {
	var min gomysql.Position
	first := true
	for _, pos := range positions {
		if pos.Name == "" {
			continue
		}
		if first || pos.Compare(min) < 0 {
			min = pos
			first = false
		}
	}
	return min
}

func nextMySQLPosition(syncer *replication.BinlogSyncer, fallback gomysql.Position) gomysql.Position {
	pos := syncer.GetNextPosition()
	if pos.Name == "" {
		return fallback
	}
	return pos
}

func mysqlEventPosition(event *replication.BinlogEvent, current gomysql.Position, syncer *replication.BinlogSyncer) gomysql.Position {
	if rotate, ok := event.Event.(*replication.RotateEvent); ok {
		return gomysql.Position{Name: string(rotate.NextLogName), Pos: uint32(rotate.Position)}
	}

	pos := current
	if pos.Name == "" {
		pos = nextMySQLPosition(syncer, current)
	}
	if event.Header.LogPos > 0 {
		pos.Pos = event.Header.LogPos
	}
	return pos
}

var (
	storedMySQLPositionRegex    = regexp.MustCompile(`^(\d{20}):([^:]+):(\d{20})(?::\d{6,})?$`)
	legacyStoredMySQLPositionRE = regexp.MustCompile(`^([^:]+):(\d{20})(?::\d{6,})?$`)
)

func formatStoredMySQLPosition(pos gomysql.Position, rowSeq int) string {
	return fmt.Sprintf("%020d:%s:%020d:%06d", mysqlBinlogSequence(pos.Name), pos.Name, pos.Pos, rowSeq)
}

func parseStoredMySQLPosition(stored string) (gomysql.Position, bool) {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return gomysql.Position{}, false
	}
	if matches := storedMySQLPositionRegex.FindStringSubmatch(stored); len(matches) == 4 {
		pos, err := strconv.ParseUint(matches[3], 10, 32)
		if err != nil {
			return gomysql.Position{}, false
		}
		return gomysql.Position{Name: matches[2], Pos: uint32(pos)}, true
	}
	if matches := legacyStoredMySQLPositionRE.FindStringSubmatch(stored); len(matches) == 3 {
		pos, err := strconv.ParseUint(matches[2], 10, 32)
		if err != nil {
			return gomysql.Position{}, false
		}
		return gomysql.Position{Name: matches[1], Pos: uint32(pos)}, true
	}
	return gomysql.Position{}, false
}

func mysqlBinlogSequence(name string) uint64 {
	idx := strings.LastIndex(name, ".")
	if idx == -1 || idx == len(name)-1 {
		return 0
	}
	seq, err := strconv.ParseUint(name[idx+1:], 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

var (
	_ source.Source           = (*MySQLCDCSource)(nil)
	_ source.MultiTableSource = (*MySQLCDCSource)(nil)
	_ source.SourceTable      = (*MySQLCDCTable)(nil)
)
