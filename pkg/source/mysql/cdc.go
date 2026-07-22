package mysql

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/mysqluri"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	gomysql "github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"vitess.io/vitess/go/vt/sqlparser"
)

const (
	defaultMySQLCDCHeartbeat       = 1 * time.Second
	defaultMySQLCDCStreamBatchSize = 10000
	defaultMySQLCDCXABufferLimit   = 1000000
	defaultMySQLCDCXABufferBytes   = 256 * 1024 * 1024
	defaultMySQLCDCPendingXALimit  = 10000
	// MySQL DECIMAL is limited to 65 digits. This bound covers the boxed
	// decimal, big.Int header, limb capacity, and allocator rounding without
	// allocating a coefficient copy during accounting.
	maxMySQLCDCDecodedDecimalSize = 256
	// These bounds cover the pending-XA map entry/buckets and per-table map
	// entry/buckets, including temporary map growth. Row batches themselves are
	// retained as immutable chunks whose actual capacities are charged exactly.
	mysqlCDCXATransactionOverhead = 1024
	mysqlCDCXATableOverhead       = 512
	// mysqlCDCStreamStallTimeout bounds how long the catch-up loop waits without
	// receiving any event. Heartbeats arrive every defaultMySQLCDCHeartbeat on a
	// healthy connection, so a stall this long means the connection is broken
	// (the syncer reconnects silently and indefinitely by default).
	mysqlCDCStreamStallTimeout = 60 * time.Second
)

var mysqlCDCColumns = []schema.Column{
	{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
	{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
	{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
}

type MySQLCDCConfig struct {
	DestSchema     string
	ServerID       uint32
	Flavor         string
	XABufferLimit  uint64
	XABufferBytes  uint64
	PendingXALimit uint64
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
	db                *sql.DB
	uri               string
	database          string
	lineageIdentity   string
	cdcConfig         MySQLCDCConfig
	connInfo          mysqlCDCConnInfo
	stateMu           sync.Mutex
	state             source.CDCStateCommitToken
	schemaMu          sync.Mutex
	discoveredSchemas map[string]*schema.TableSchema
}

type mysqlCDCCheckpoint struct {
	Position gomysql.Position
	Identity string
	GTIDSet  string
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

var _ source.CDCStateProvider = (*MySQLCDCSource)(nil)

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

type mysqlCDCXAChanges struct {
	byTable map[string]*mysqlCDCXATableChanges
	start   gomysql.Position
	count   uint64
	bytes   uint64
}

type mysqlCDCXATableChanges struct {
	head *mysqlCDCXAChangeChunk
	tail *mysqlCDCXAChangeChunk
}

type mysqlCDCXAChangeChunk struct {
	changes []mysqlCDCChange
	next    *mysqlCDCXAChangeChunk
}

type mysqlCDCXAAction int

const (
	mysqlCDCXAActionNone mysqlCDCXAAction = iota
	mysqlCDCXAActionStart
	mysqlCDCXAActionEnd
	mysqlCDCXAActionCommit
	mysqlCDCXAActionRollback
)

func mysqlCDCChangesSize(changes []mysqlCDCChange) uint64 {
	total := saturatingMySQLCDCSizeMul(uint64(len(changes)), 64)
	for _, change := range changes {
		total = saturatingMySQLCDCSizeAdd(total, uint64(len(change.lsn)))
		total = saturatingMySQLCDCSizeAdd(total, saturatingMySQLCDCSizeMul(uint64(len(change.values)), 16))
		for _, value := range change.values {
			total = saturatingMySQLCDCSizeAdd(total, mysqlCDCValueSize(value))
		}
	}
	return total
}

func mysqlCDCXATransactionSize(xaID string) uint64 {
	return saturatingMySQLCDCSizeAdd(mysqlCDCXATransactionOverhead, uint64(len(xaID)))
}

func mysqlCDCXATableSize(table string) uint64 {
	return saturatingMySQLCDCSizeAdd(mysqlCDCXATableOverhead, uint64(len(table)))
}

func mysqlCDCXAChunkSize(changes []mysqlCDCChange) uint64 {
	backingBytes := saturatingMySQLCDCSizeMul(
		uint64(cap(changes)),
		uint64(reflect.TypeOf(mysqlCDCChange{}).Size()),
	)
	return saturatingMySQLCDCSizeAdd(uint64(reflect.TypeOf(mysqlCDCXAChangeChunk{}).Size()), backingBytes)
}

func mysqlCDCValueSize(value interface{}) uint64 {
	if value == nil {
		return 0
	}
	switch value := value.(type) {
	case string:
		return saturatingMySQLCDCSizeAdd(uint64(reflect.TypeOf(value).Size()), uint64(len(value)))
	case []byte:
		return saturatingMySQLCDCSizeAdd(uint64(reflect.TypeOf(value).Size()), uint64(len(value)))
	case decimal.Decimal:
		return mysqlCDCDecimalSize(value)
	case *decimal.Decimal:
		if value == nil {
			return 0
		}
		return mysqlCDCDecimalSize(*value)
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.String:
		return saturatingMySQLCDCSizeAdd(uint64(rv.Type().Size()), uint64(rv.Len()))
	case reflect.Array:
		return uint64(rv.Type().Size())
	case reflect.Slice:
		length := uint64(rv.Len())
		elementSize := uint64(rv.Type().Elem().Size())
		return saturatingMySQLCDCSizeAdd(uint64(rv.Type().Size()), saturatingMySQLCDCSizeMul(length, elementSize))
	default:
		return uint64(rv.Type().Size())
	}
}

func mysqlCDCDecimalSize(decimal.Decimal) uint64 {
	return maxMySQLCDCDecodedDecimalSize
}

func saturatingMySQLCDCSizeAdd(left, right uint64) uint64 {
	if left > ^uint64(0)-right {
		return ^uint64(0)
	}
	return left + right
}

func saturatingMySQLCDCSizeMul(left, right uint64) uint64 {
	if left != 0 && right > ^uint64(0)/left {
		return ^uint64(0)
	}
	return left * right
}

type mysqlCDCBinlogStreamer interface {
	GetEvent(context.Context) (*replication.BinlogEvent, error)
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

	if isVitess, _ := isVitessServer(ctx, db); isVitess {
		_ = db.Close()
		return fmt.Errorf("server for database %q identifies as Vitess/PlanetScale, which has no MySQL binlog; use the vitess+cdc:// or ps_mysql+cdc:// scheme instead", database)
	}

	if err := checkMySQLBinlogSettings(ctx, db); err != nil {
		_ = db.Close()
		return err
	}
	lineageIdentity, err := loadMySQLCDCLineageIdentity(ctx, db, cdcConfig.Flavor)
	if err != nil {
		_ = db.Close()
		return err
	}

	s.db = db
	s.uri = rawURI
	s.database = database
	s.lineageIdentity = lineageIdentity
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

func (s *MySQLCDCSource) MySQLServerIdentity() string {
	return s.lineageIdentity
}

func (s *MySQLCDCSource) MySQLDatabaseName() string {
	return s.database
}

func (s *MySQLCDCSource) resetCDCState() {
	s.stateMu.Lock()
	s.state = source.CDCStateCommitToken{
		SnapshotPositions: make(map[string]string),
	}
	s.stateMu.Unlock()
}

func (s *MySQLCDCSource) recordMySQLCDCSnapshot(table string, checkpoint mysqlCDCCheckpoint) {
	if table == "" || checkpoint.Position.Name == "" {
		return
	}
	s.stateMu.Lock()
	if s.state.SnapshotPositions == nil {
		s.state.SnapshotPositions = make(map[string]string)
	}
	s.state.SnapshotPositions[table] = formatStoredMySQLCheckpoint(checkpoint)
	s.stateMu.Unlock()
}

func (s *MySQLCDCSource) recordMySQLCDCCheckpoint(checkpoint mysqlCDCCheckpoint) {
	if checkpoint.Position.Name == "" {
		return
	}
	s.stateMu.Lock()
	s.state.Position = formatStoredMySQLCheckpoint(checkpoint)
	s.stateMu.Unlock()
}

func (s *MySQLCDCSource) CDCState() source.CDCStateCommitToken {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	snapshots := make(map[string]string, len(s.state.SnapshotPositions))
	for table, position := range s.state.SnapshotPositions {
		snapshots[table] = position
	}
	return source.CDCStateCommitToken{
		Position:          s.state.Position,
		SnapshotPositions: snapshots,
	}
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
	if err := validateMySQLCDCSourceColumns(fullSchema, req.Name); err != nil {
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

	if req.Strategy != "" && req.Strategy != config.StrategyMerge && req.Strategy != config.StrategyReplace {
		return nil, fmt.Errorf("MySQL CDC only supports the merge strategy (replace is accepted as a request for the initial snapshot)")
	}

	return &MySQLCDCTable{
		source:      s,
		tableName:   req.Name,
		metadata:    newMySQLCDCMetadata(req.Name, fullSchema, s.database),
		tableSchema: tableSchema,
		primaryKeys: pks,
		strategy:    config.StrategyMerge,
	}, nil
}

func (s *MySQLCDCSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return getMySQLSchema(ctx, s.db, s.database, table)
}

func (s *MySQLCDCSource) IsMultiTable() bool {
	return true
}

func (s *MySQLCDCSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	tables, err := s.getTables(ctx, true)
	if err != nil {
		return nil, err
	}
	s.schemaMu.Lock()
	s.discoveredSchemas = make(map[string]*schema.TableSchema, len(tables))
	for _, table := range tables {
		s.discoveredSchemas[table.Name] = cloneMySQLCDCTableSchema(table.Schema)
	}
	s.schemaMu.Unlock()
	return tables, nil
}

func (s *MySQLCDCSource) getTables(ctx context.Context, validateSupported bool) ([]source.SourceTableInfo, error) {
	tableNames, err := s.getMySQLCDCTableNames(ctx, s.db)
	if err != nil {
		return nil, err
	}
	tables, err := s.loadMySQLCDCTables(ctx, tableNames, validateSupported)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("no MySQL tables found in database %s", s.database)
	}
	return tables, nil
}

func (s *MySQLCDCSource) getMySQLCDCTableNames(ctx context.Context, q mysqlCDCPositionQueryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
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

	var tableNames []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("failed to scan MySQL table: %w", err)
		}
		tableNames = append(tableNames, tableName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tableNames, nil
}

func (s *MySQLCDCSource) loadMySQLCDCTables(ctx context.Context, tableNames []string, validateSupported bool) ([]source.SourceTableInfo, error) {
	tables := make([]source.SourceTableInfo, 0, len(tableNames))
	for _, tableName := range tableNames {
		fullSchema, err := s.getSchema(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema for %s: %w", tableName, err)
		}
		if err := validateMySQLCDCSourceColumns(fullSchema, tableName); err != nil {
			return nil, err
		}
		if validateSupported {
			if err := validateMySQLCDCTableSupported(ctx, s.db, s.database, tableName); err != nil {
				return nil, err
			}
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
	return tables, nil
}

func (s *MySQLCDCSource) getSelectedTables(ctx context.Context, opts source.MultiTableReadOptions) ([]source.SourceTableInfo, error) {
	tableNames, err := s.getMySQLCDCTableNames(ctx, s.db)
	if err != nil {
		return nil, err
	}
	if len(tableNames) == 0 && len(opts.Tables) == 0 {
		return nil, fmt.Errorf("no MySQL tables found in database %s", s.database)
	}

	filter := map[string]bool{}
	for _, table := range opts.Tables {
		filter[strings.ToLower(table)] = true
	}

	selectedNames := make([]string, 0, len(tableNames))
	for _, tableName := range tableNames {
		if len(filter) > 0 && !filter[strings.ToLower(tableName)] {
			continue
		}
		selectedNames = append(selectedNames, tableName)
	}
	selected, err := s.loadMySQLCDCTables(ctx, selectedNames, false)
	if err != nil {
		return nil, err
	}
	for _, table := range selected {
		if err := validateMySQLCDCTableSupported(ctx, s.db, s.database, table.Name); err != nil {
			return nil, err
		}
	}
	return selected, nil
}

func (s *MySQLCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	selected, err := s.getSelectedTables(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := s.validateMySQLCDCDiscoveredSchemas(selected, opts.Tables); err != nil {
		return nil, err
	}

	results := make(chan source.RecordBatchResult, 16)
	go func() {
		defer close(results)
		s.resetCDCState()

		startByTable := make(map[string]gomysql.Position, len(selected))
		metaByTable := make(map[string]mysqlCDCTableMetadata, len(selected))

		// Multi-table CDC captures an independent snapshot position per table.
		for _, table := range selected {
			fullSchema := destination.DestinationTableSchema(table.Schema)
			fullSchema = removeMySQLCDCColumns(fullSchema)
			meta := newMySQLCDCMetadata(table.Name, fullSchema, s.database)
			metaByTable[table.Name] = meta

			storedLSN := strings.TrimSpace(opts.CDCResumeLSNs[table.Name])
			if resume, ok := parseStoredMySQLCheckpoint(storedLSN); ok {
				canResume, err := s.canResume(ctx, resume)
				if err != nil {
					results <- source.RecordBatchResult{Err: err}
					return
				}
				if canResume {
					startByTable[table.Name] = resume.Position
					continue
				}
				results <- source.RecordBatchResult{Err: mysqlCDCResumeExpiredError(table.Name, resume.Position)}
				return
			} else if storedLSN != "" {
				results <- source.RecordBatchResult{Err: mysqlCDCResumeInvalidError(table.Name, storedLSN)}
				return
			}

			inventoryValidator := func(validationCtx context.Context, q mysqlCDCPositionQueryer) error {
				return s.validateMySQLCDCInventory(validationCtx, q, opts.Tables)
			}
			snapshotCheckpoint, err := s.snapshotTable(ctx, meta, table.Schema, opts.ReadOptions, results, table.Name, inventoryValidator)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed for %s: %w", table.Name, err)}
				return
			}
			startByTable[table.Name] = snapshotCheckpoint.Position
			s.recordMySQLCDCSnapshot(table.Name, snapshotCheckpoint)
		}

		if err := s.streamTables(ctx, selected, metaByTable, startByTable, opts.ReadOptions, results, true); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func cloneMySQLCDCTableSchema(tableSchema *schema.TableSchema) *schema.TableSchema {
	if tableSchema == nil {
		return nil
	}
	cloned := *tableSchema
	cloned.Columns = append([]schema.Column(nil), tableSchema.Columns...)
	cloned.PrimaryKeys = append([]string(nil), tableSchema.PrimaryKeys...)
	return &cloned
}

func (s *MySQLCDCSource) validateMySQLCDCDiscoveredSchemas(selected []source.SourceTableInfo, requested []string) error {
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.discoveredSchemas == nil {
		s.discoveredSchemas = make(map[string]*schema.TableSchema, len(selected))
		for _, table := range selected {
			s.discoveredSchemas[table.Name] = cloneMySQLCDCTableSchema(table.Schema)
		}
	}
	current := make(map[string]*schema.TableSchema, len(selected))
	currentFolded := make(map[string]struct{}, len(selected))
	for _, table := range selected {
		current[table.Name] = table.Schema
		currentFolded[strings.ToLower(table.Name)] = struct{}{}
		expected, exists := s.discoveredSchemas[table.Name]
		if !exists {
			return fmt.Errorf("MySQL table %s appeared after CDC table discovery and before snapshots began; retry so it can be prepared safely", table.Name)
		}
		if !reflect.DeepEqual(expected, table.Schema) {
			return fmt.Errorf("MySQL table %s changed after CDC schema discovery and before its snapshot; run with --full-refresh to restart with the current schema", table.Name)
		}
	}
	if len(requested) == 0 {
		for table := range s.discoveredSchemas {
			if _, exists := current[table]; !exists {
				return fmt.Errorf("MySQL table %s changed after CDC schema discovery and before its snapshot; run with --full-refresh to restart with the current schema", table)
			}
		}
		return nil
	}
	for _, table := range requested {
		if _, exists := currentFolded[strings.ToLower(table)]; !exists {
			return fmt.Errorf("selected MySQL table %s is no longer available after CDC table discovery; run with --full-refresh to restart with the current schema", table)
		}
	}
	return nil
}

func (s *MySQLCDCSource) validateMySQLCDCInventory(ctx context.Context, q mysqlCDCPositionQueryer, requested []string) error {
	tableNames, err := s.getMySQLCDCTableNames(ctx, q)
	if err != nil {
		return err
	}
	current := make(map[string]struct{}, len(tableNames))
	for _, table := range tableNames {
		current[strings.ToLower(table)] = struct{}{}
	}

	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	expected := make(map[string]string, len(s.discoveredSchemas))
	for table := range s.discoveredSchemas {
		expected[strings.ToLower(table)] = table
	}
	if len(requested) == 0 {
		for table, original := range expected {
			if _, exists := current[table]; !exists {
				return fmt.Errorf("MySQL table %s changed after CDC schema discovery and before snapshots began; run with --full-refresh to restart with the current schema", original)
			}
		}
		for table := range current {
			if _, exists := expected[table]; !exists {
				return fmt.Errorf("MySQL table %s appeared after CDC table discovery and before snapshots began; retry so it can be prepared safely", table)
			}
		}
		return nil
	}
	for _, table := range requested {
		folded := strings.ToLower(table)
		if _, discovered := expected[folded]; !discovered {
			return fmt.Errorf("selected MySQL table %s appeared after CDC table discovery; retry so it can be prepared safely", table)
		}
		if _, exists := current[folded]; !exists {
			return fmt.Errorf("selected MySQL table %s is no longer available after CDC table discovery; run with --full-refresh to restart with the current schema", table)
		}
	}
	return nil
}

func parseMySQLCDCURI(rawURI string) (MySQLCDCConfig, string, mysqlCDCConnInfo, error) {
	cfg := MySQLCDCConfig{
		ServerID:       randomMySQLServerID(),
		Flavor:         gomysql.MySQLFlavor,
		XABufferLimit:  defaultMySQLCDCXABufferLimit,
		XABufferBytes:  defaultMySQLCDCXABufferBytes,
		PendingXALimit: defaultMySQLCDCPendingXALimit,
	}

	parsed, err := mysqluri.ParseURL(rawURI)
	if err != nil {
		return cfg, "", mysqlCDCConnInfo{}, err
	}

	baseScheme, ok := strings.CutSuffix(strings.ToLower(parsed.Scheme), "+cdc")
	if !ok {
		return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("unsupported MySQL CDC scheme: %s", parsed.Scheme)
	}
	switch baseScheme {
	case "mysql", "mysql+pymysql", "vitess", "ps_mysql":
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
	if rawServerID := strings.TrimSpace(query.Get("server_id")); rawServerID != "" {
		serverID, err := strconv.ParseUint(rawServerID, 10, 32)
		if err != nil || serverID == 0 {
			return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("server_id must be a positive uint32")
		}
		cfg.ServerID = uint32(serverID)
	}
	if rawLimit := strings.TrimSpace(query.Get("xa_buffer_limit")); rawLimit != "" {
		limit, err := strconv.ParseUint(rawLimit, 10, 64)
		if err != nil || limit == 0 {
			return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("xa_buffer_limit must be a positive integer")
		}
		cfg.XABufferLimit = limit
	}
	if rawLimit := strings.TrimSpace(query.Get("xa_buffer_bytes_limit")); rawLimit != "" {
		limit, err := strconv.ParseUint(rawLimit, 10, 64)
		if err != nil || limit == 0 {
			return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("xa_buffer_bytes_limit must be a positive integer")
		}
		cfg.XABufferBytes = limit
	}
	if rawLimit := strings.TrimSpace(query.Get("xa_pending_limit")); rawLimit != "" {
		limit, err := strconv.ParseUint(rawLimit, 10, 64)
		if err != nil || limit == 0 {
			return cfg, "", mysqlCDCConnInfo{}, fmt.Errorf("xa_pending_limit must be a positive integer")
		}
		cfg.PendingXALimit = limit
	}

	query.Del("dest_schema")
	query.Del("flavor")
	query.Del("mode")
	query.Del("server_id")
	query.Del("xa_buffer_limit")
	query.Del("xa_buffer_bytes_limit")
	query.Del("xa_pending_limit")

	// Vitess-only parameters: irrelevant to (and rejected by) the MySQL DSN. The
	// tls parameter is deliberately kept — it secures the MySQL-protocol probe and
	// is also inherited by the VStream gRPC connection.
	query.Del("grpc_port")
	query.Del("grpc_host")
	query.Del("grpc_tls")
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
	parsed, err := mysqluri.ParseURL(normalizedURI)
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
		t.source.resetCDCState()

		storedLSN := strings.TrimSpace(opts.CDCResumeLSN)
		if resume, ok := parseStoredMySQLCheckpoint(storedLSN); ok {
			canResume, err := t.source.canResume(ctx, resume)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			if canResume {
				internalName := t.metadata.Name
				startByTable := map[string]gomysql.Position{internalName: resume.Position}
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
			results <- source.RecordBatchResult{Err: mysqlCDCResumeExpiredError(t.tableName, resume.Position)}
			return
		} else if storedLSN != "" {
			results <- source.RecordBatchResult{Err: mysqlCDCResumeInvalidError(t.tableName, storedLSN)}
			return
		}

		snapshotCheckpoint, err := t.source.snapshotTable(ctx, t.metadata, outputSchema, opts, results, "", nil)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)}
			return
		}
		t.source.recordMySQLCDCSnapshot(t.tableName, snapshotCheckpoint)

		internalName := t.metadata.Name
		startByTable := map[string]gomysql.Position{internalName: snapshotCheckpoint.Position}
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

func (s *MySQLCDCSource) canResume(ctx context.Context, checkpoint mysqlCDCCheckpoint) (bool, error) {
	pos := checkpoint.Position
	if s.lineageIdentity != "" {
		if checkpoint.Identity == "" {
			return false, fmt.Errorf("MySQL CDC resume checkpoint has no source lineage identity; run with --full-refresh to establish a lineage-aware checkpoint")
		}
		if checkpoint.Identity != s.lineageIdentity {
			return false, fmt.Errorf("MySQL CDC resume checkpoint belongs to source server %q, but the connected server is %q; run with --full-refresh instead of reusing file positions across servers", checkpoint.Identity, s.lineageIdentity)
		}
		currentGTIDSet, err := s.currentMySQLCDCLineageGTIDSet(ctx)
		if err != nil {
			return false, err
		}
		contains, err := mysqlCDCGTIDSetContains(s.cdcConfig.Flavor, currentGTIDSet, checkpoint.GTIDSet)
		if err != nil {
			return false, err
		}
		if !contains {
			return false, fmt.Errorf("MySQL CDC resume checkpoint GTID history is not contained in the connected server history; the server may have been reset or replaced, so run with --full-refresh")
		}
	}
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

func (s *MySQLCDCSource) snapshotTable(ctx context.Context, meta mysqlCDCTableMetadata, outputSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string, validateInventory func(context.Context, mysqlCDCPositionQueryer) error) (mysqlCDCCheckpoint, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return mysqlCDCCheckpoint{}, fmt.Errorf("failed to acquire MySQL connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	snapshotPos, err := beginMySQLConsistentSnapshotWithValidation(ctx, conn, validateInventory)
	if err != nil {
		return mysqlCDCCheckpoint{}, err
	}
	snapshotCheckpoint, err := s.checkpointForPosition(ctx, conn, snapshotPos)
	if err != nil {
		return mysqlCDCCheckpoint{}, err
	}
	freshSchema, err := getMySQLSchema(ctx, conn, s.database, meta.Name)
	if err != nil {
		return mysqlCDCCheckpoint{}, fmt.Errorf("failed to validate snapshot schema for %s: %w", meta.Name, err)
	}
	if err := validateMySQLCDCSnapshotSchema(removeMySQLCDCColumns(meta.FullSchema), freshSchema, meta.Name); err != nil {
		return mysqlCDCCheckpoint{}, err
	}

	sourceColumns := sourceColumnsWithoutMySQLCDC(outputSchema)
	query := buildMySQLCDCSnapshotQuery(meta, sourceColumns)
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return mysqlCDCCheckpoint{}, fmt.Errorf("failed to query snapshot for %s: %w", meta.Name, err)
	}
	if opts.CDCSnapshotReplace {
		results <- source.RecordBatchResult{TableName: resultTable, Truncate: true}
	}

	if err := rowsToMySQLCDCSnapshotBatches(rows, outputSchema, opts, snapshotPos, results, resultTable); err != nil {
		_ = rows.Close()
		return mysqlCDCCheckpoint{}, err
	}
	if err := rows.Close(); err != nil {
		return mysqlCDCCheckpoint{}, err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return mysqlCDCCheckpoint{}, fmt.Errorf("failed to commit MySQL snapshot transaction: %w", err)
	}
	committed = true

	return snapshotCheckpoint, nil
}

func validateMySQLCDCSnapshotSchema(expected, current *schema.TableSchema, table string) error {
	if reflect.DeepEqual(expected, current) {
		return nil
	}
	return fmt.Errorf("MySQL table %s changed after CDC schema discovery and before its snapshot; run with --full-refresh to restart with the current schema", table)
}

func beginMySQLConsistentSnapshot(ctx context.Context, conn mysqlCDCSnapshotConn) (gomysql.Position, error) {
	return beginMySQLConsistentSnapshotWithValidation(ctx, conn, nil)
}

func beginMySQLConsistentSnapshotWithValidation(ctx context.Context, conn mysqlCDCSnapshotConn, validate func(context.Context, mysqlCDCPositionQueryer) error) (gomysql.Position, error) {
	// The binlog stream decodes TIMESTAMP columns as UTC epoch instants. Pin the
	// snapshot session to UTC so the driver (loc=UTC) reads the same instants;
	// otherwise snapshot rows shift by the server's time zone offset.
	if _, err := conn.ExecContext(ctx, "SET time_zone = '+00:00'"); err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to set MySQL snapshot session time zone to UTC: %w", err)
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

	if _, err := conn.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to set MySQL snapshot isolation: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "START TRANSACTION WITH CONSISTENT SNAPSHOT"); err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to start MySQL snapshot transaction: %w", err)
	}

	snapshotPos, err := currentMySQLBinlogPosition(ctx, conn)
	if err != nil {
		return gomysql.Position{}, err
	}
	if err := ensureNoPreparedMySQLXA(ctx, conn); err != nil {
		return gomysql.Position{}, err
	}
	if validate != nil {
		if err := validate(ctx, conn); err != nil {
			return gomysql.Position{}, err
		}
	}

	if _, err := conn.ExecContext(ctx, "UNLOCK TABLES"); err != nil {
		return gomysql.Position{}, fmt.Errorf("failed to unlock MySQL tables after snapshot: %w", err)
	}
	locked = false

	return snapshotPos, nil
}

func ensureNoPreparedMySQLXA(ctx context.Context, conn mysqlCDCPositionQueryer) error {
	rows, err := conn.QueryContext(ctx, "XA RECOVER")
	if err != nil {
		return fmt.Errorf("failed to inspect prepared MySQL XA transactions before the CDC snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		return fmt.Errorf("cannot start a safe MySQL CDC snapshot while prepared XA transactions exist; commit or roll back the prepared transactions and retry")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to inspect prepared MySQL XA transactions before the CDC snapshot: %w", err)
	}
	return nil
}

func (s *MySQLCDCSource) streamTables(ctx context.Context, tables []source.SourceTableInfo, metas map[string]mysqlCDCTableMetadata, startByTable map[string]gomysql.Position, opts source.ReadOptions, results chan<- source.RecordBatchResult, tagResults bool) error {
	if len(tables) == 0 {
		return nil
	}

	target, err := currentMySQLBinlogPosition(ctx, s.db)
	if err != nil {
		return err
	}
	targetCheckpoint, err := s.checkpointForPosition(ctx, s.db, target)
	if err != nil {
		return err
	}
	start := minMySQLPosition(startByTable)
	if start.Name == "" {
		return nil
	}
	if start.Compare(target) >= 0 {
		s.recordMySQLCDCCheckpoint(targetCheckpoint)
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

	// Termination relies exclusively on events delivered through GetEvent:
	// positions of delivered events reaching target, or a heartbeat (the server
	// only sends one when it has nothing newer, and the stream is FIFO, so a
	// heartbeat means every earlier event was already consumed). The syncer's
	// GetNextPosition is deliberately not consulted — it is updated by the
	// producer goroutine before the event is delivered, so using it can skip
	// events that are parsed but not yet consumed.
	current := start
	batchSize := mysqlCDCStreamBatchSize(opts)
	buffers := make(map[string]*mysqlCDCChangeBuffer, len(tables))
	pendingXA := make(map[string]*mysqlCDCXAChanges)
	activeXA := ""
	pendingXAChanges := uint64(0)
	pendingXABytes := uint64(0)
	xaBufferLimit := s.cdcConfig.XABufferLimit
	if xaBufferLimit == 0 {
		xaBufferLimit = defaultMySQLCDCXABufferLimit
	}
	xaBufferBytes := s.cdcConfig.XABufferBytes
	if xaBufferBytes == 0 {
		xaBufferBytes = defaultMySQLCDCXABufferBytes
	}
	pendingXALimit := s.cdcConfig.PendingXALimit
	if pendingXALimit == 0 {
		pendingXALimit = defaultMySQLCDCPendingXALimit
	}
	safeCheckpoint := func(position gomysql.Position) gomysql.Position {
		for _, pending := range pendingXA {
			if pending.start.Name == "" {
				continue
			}
			if position.Name == "" || pending.start.Compare(position) < 0 {
				position = pending.start
			}
		}
		return position
	}
	finish := func(position gomysql.Position) error {
		if err := flushMySQLCDCChangeBuffers(buffers, results); err != nil {
			return err
		}
		checkpoint := targetCheckpoint
		checkpoint.Position = safeCheckpoint(position)
		s.recordMySQLCDCCheckpoint(checkpoint)
		return nil
	}
	appendChanges := func(table string, changes []mysqlCDCChange) error {
		if len(changes) == 0 {
			return nil
		}
		meta := metas[table]
		outputSchema := schemaByTable[table]
		if outputSchema == nil {
			return fmt.Errorf("missing output schema for MySQL CDC table %s", table)
		}
		resultTable := mysqlCDCResultTableName(meta.Name, len(tables), tagResults)
		return appendMySQLCDCBufferedChanges(buffers, meta.Name, outputSchema, resultTable, changes, batchSize, results)
	}
	releaseXA := func(xaID string, commit bool) error {
		pending := pendingXA[xaID]
		if pending == nil {
			// An outcome for an XA transaction prepared before the resume
			// position: finish() rolls the checkpoint back before the START of
			// any still-pending XA, and snapshots reject outstanding prepared
			// XA (ensureNoPreparedMySQLXA), so a transaction unseen since the
			// resume position was fully released — rows emitted and
			// checkpointed — by the run that recorded it. Replaying its
			// terminal event alone is benign.
			return nil
		}
		if commit {
			tableNames := make([]string, 0, len(pending.byTable))
			for table := range pending.byTable {
				tableNames = append(tableNames, table)
			}
			sort.Strings(tableNames)
			for _, table := range tableNames {
				for chunk := pending.byTable[table].head; chunk != nil; chunk = chunk.next {
					if err := appendChanges(table, chunk.changes); err != nil {
						return err
					}
				}
			}
		}
		pendingXAChanges -= pending.count
		pendingXABytes -= pending.bytes
		delete(pendingXA, xaID)
		if activeXA == xaID {
			activeXA = ""
		}
		return nil
	}
	lastDelivery := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if current.Compare(target) >= 0 {
			return finish(current)
		}

		event, err, eventCtxErr := readMySQLCDCEvent(ctx, streamer)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isMySQLCDCReadPollTimeout(ctx, err, eventCtxErr) {
				if time.Since(lastDelivery) >= mysqlCDCStreamStallTimeout {
					return fmt.Errorf("MySQL binlog stream stalled at %s while catching up to %s: no events or heartbeats received for %s; check connectivity to the server", current, target, mysqlCDCStreamStallTimeout)
				}
				continue
			}
			return fmt.Errorf("failed to read MySQL binlog event: %w", err)
		}
		if event == nil {
			continue
		}
		lastDelivery = time.Now()

		if event.Header != nil {
			if event.Header.EventType == replication.PARTIAL_UPDATE_ROWS_EVENT {
				return fmt.Errorf("MySQL CDC does not support PARTIAL_UPDATE_ROWS_EVENT; disable binlog_row_value_options=PARTIAL_JSON")
			}
			if event.Header.EventType == replication.HEARTBEAT_EVENT || event.Header.EventType == replication.HEARTBEAT_LOG_EVENT_V2 {
				// current < target here is normal, not an anomaly: non-data
				// events (FORMAT_DESCRIPTION, PREVIOUS_GTIDS, artificial
				// MariaDB events) carry LogPos=0 and don't advance current.
				// The last emitted _cdc_lsn may then sit slightly before
				// target, so the next run re-streams a short suffix of
				// events; merge-by-LSN dedup makes that harmless.
				return finish(current)
			}
		}

		previousPosition := current
		eventPos := mysqlEventPosition(event, current)
		current = eventPos
		if eventPos.Compare(target) > 0 {
			return finish(previousPosition)
		}
		logicalEvents, err := mysqlBinlogEvents(event)
		if err != nil {
			return err
		}
		rowSeq := 0
		for _, logicalEvent := range logicalEvents {
			if queryEvent, ok := logicalEvent.Event.(*replication.QueryEvent); ok {
				xaAction, xaID, xaFlag, err := mysqlCDCXAQuery(string(queryEvent.Query))
				if err != nil {
					return err
				}
				switch xaAction {
				case mysqlCDCXAActionStart:
					if activeXA != "" {
						return fmt.Errorf("MySQL CDC encountered XA START for %s while XA transaction %s is active", xaID, activeXA)
					}
					if _, exists := pendingXA[xaID]; exists {
						if !xaFlag {
							return fmt.Errorf("MySQL CDC encountered duplicate XA transaction %s", xaID)
						}
					} else {
						if xaFlag {
							return fmt.Errorf("MySQL CDC encountered XA continuation for unknown transaction %s", xaID)
						}
						if uint64(len(pendingXA)) >= pendingXALimit {
							return fmt.Errorf("MySQL CDC pending XA transactions exceed xa_pending_limit=%d; resolve prepared XA transactions or increase xa_pending_limit and retry", pendingXALimit)
						}
						transactionBytes := mysqlCDCXATransactionSize(xaID)
						if transactionBytes > xaBufferBytes || pendingXABytes > xaBufferBytes-transactionBytes {
							return fmt.Errorf("MySQL CDC buffered XA payloads exceed xa_buffer_bytes_limit=%d; increase xa_buffer_bytes_limit and retry", xaBufferBytes)
						}
						pendingXA[xaID] = &mysqlCDCXAChanges{
							byTable: make(map[string]*mysqlCDCXATableChanges),
							start:   previousPosition,
							bytes:   transactionBytes,
						}
						pendingXABytes += transactionBytes
					}
					activeXA = xaID
				case mysqlCDCXAActionEnd:
					if activeXA == "" || activeXA != xaID {
						return fmt.Errorf("MySQL CDC encountered XA END for %s while XA transaction %s is active", xaID, activeXA)
					}
					activeXA = ""
				case mysqlCDCXAActionCommit:
					if err := releaseXA(xaID, true); err != nil {
						return err
					}
				case mysqlCDCXAActionRollback:
					if err := releaseXA(xaID, false); err != nil {
						return err
					}
				}
				if xaAction != mysqlCDCXAActionNone {
					continue
				}
				truncated, err := mysqlCDCQueryTruncatesAfter(queryEvent, s.database, metaByAlias, eventPos, startByTable)
				if err != nil {
					return err
				}
				if len(truncated) > 0 {
					if err := flushMySQLCDCChangeBuffers(buffers, results); err != nil {
						return err
					}
					for _, meta := range truncated {
						results <- source.RecordBatchResult{
							TableName:      mysqlCDCResultTableName(meta.Name, len(tables), tagResults),
							Truncate:       true,
							CDCWALTruncate: true,
						}
					}
				}
				continue
			}
			if logicalEvent.Header != nil && logicalEvent.Header.EventType == replication.XA_PREPARE_LOG_EVENT {
				xaID, onePhase, err := mysqlCDCXAPrepare(logicalEvent)
				if err != nil {
					return err
				}
				if activeXA != "" && activeXA != xaID {
					return fmt.Errorf("MySQL CDC encountered XA PREPARE for %s while XA transaction %s is active", xaID, activeXA)
				}
				if pendingXA[xaID] == nil {
					return fmt.Errorf("MySQL CDC encountered XA PREPARE for unknown transaction %s", xaID)
				}
				activeXA = ""
				if onePhase {
					if err := releaseXA(xaID, true); err != nil {
						return err
					}
				}
				continue
			}

			rowsEvent, ok := logicalEvent.Event.(*replication.RowsEvent)
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
			if err := validateMySQLCDCFullRowImage(rowsEvent, len(sourceColumnsWithoutMySQLCDC(meta.FullSchema))); err != nil {
				return fmt.Errorf("failed to decode MySQL CDC rows for %s: %w", meta.Name, err)
			}
			changes, nextRowSeq, err := mysqlRowsEventToChangesFromSequence(rowsEvent.Type(), rowsEvent.Rows, meta.FullSchema, outputSchema, eventPos, rowSeq)
			if err != nil {
				return fmt.Errorf("failed to decode MySQL CDC rows for %s: %w", meta.Name, err)
			}
			rowSeq = nextRowSeq
			if len(changes) == 0 {
				continue
			}
			if activeXA != "" {
				pending := pendingXA[activeXA]
				if pending == nil {
					return fmt.Errorf("MySQL CDC lost pending state for XA transaction %s", activeXA)
				}
				changeCount := uint64(len(changes))
				if changeCount > xaBufferLimit || pendingXAChanges > xaBufferLimit-changeCount {
					return fmt.Errorf("MySQL CDC buffered XA changes exceed xa_buffer_limit=%d; increase xa_buffer_limit and retry", xaBufferLimit)
				}
				changeBytes := saturatingMySQLCDCSizeAdd(mysqlCDCChangesSize(changes), mysqlCDCXAChunkSize(changes))
				tableChanges := pending.byTable[meta.Name]
				if tableChanges == nil {
					changeBytes = saturatingMySQLCDCSizeAdd(changeBytes, mysqlCDCXATableSize(meta.Name))
				}
				if changeBytes > xaBufferBytes || pendingXABytes > xaBufferBytes-changeBytes {
					return fmt.Errorf("MySQL CDC buffered XA payloads exceed xa_buffer_bytes_limit=%d; increase xa_buffer_bytes_limit and retry", xaBufferBytes)
				}
				if tableChanges == nil {
					tableChanges = &mysqlCDCXATableChanges{}
					pending.byTable[meta.Name] = tableChanges
				}
				chunk := &mysqlCDCXAChangeChunk{changes: changes}
				if tableChanges.tail == nil {
					tableChanges.head = chunk
				} else {
					tableChanges.tail.next = chunk
				}
				tableChanges.tail = chunk
				pending.count += changeCount
				pending.bytes += changeBytes
				pendingXAChanges += changeCount
				pendingXABytes += changeBytes
				continue
			}
			if err := appendChanges(meta.Name, changes); err != nil {
				return err
			}
		}
	}
}

func mysqlBinlogEvents(event *replication.BinlogEvent) ([]*replication.BinlogEvent, error) {
	if event == nil {
		return nil, nil
	}
	if event.Header != nil && event.Header.EventType == replication.PARTIAL_UPDATE_ROWS_EVENT {
		return nil, fmt.Errorf("MySQL CDC does not support PARTIAL_UPDATE_ROWS_EVENT; disable binlog_row_value_options=PARTIAL_JSON")
	}
	payload, ok := event.Event.(*replication.TransactionPayloadEvent)
	if !ok {
		return []*replication.BinlogEvent{event}, nil
	}
	var events []*replication.BinlogEvent
	for _, nested := range payload.Events {
		nestedEvents, err := mysqlBinlogEvents(nested)
		if err != nil {
			return nil, err
		}
		events = append(events, nestedEvents...)
	}
	return events, nil
}

func validateMySQLCDCFullRowImage(rowsEvent *replication.RowsEvent, expectedColumns int) error {
	if rowsEvent.ColumnCount != 0 && rowsEvent.ColumnCount != uint64(expectedColumns) {
		return fmt.Errorf("binlog row image declares %d columns but the table schema has %d; the table structure changed since the sync started; run with --full-refresh to rebuild the destination safely", rowsEvent.ColumnCount, expectedColumns)
	}
	if len(rowsEvent.SkippedColumns) != 0 && len(rowsEvent.SkippedColumns) != len(rowsEvent.Rows) {
		return fmt.Errorf("binlog row image metadata has %d images for %d rows", len(rowsEvent.SkippedColumns), len(rowsEvent.Rows))
	}
	for image, skipped := range rowsEvent.SkippedColumns {
		if len(skipped) == 0 {
			continue
		}
		return fmt.Errorf("binlog row image %d omits columns %v; producer sessions must use binlog_row_image=FULL", image, skipped)
	}
	return nil
}

func mysqlCDCQueryTruncates(queryEvent *replication.QueryEvent, defaultDatabase string, metaByAlias map[string]mysqlCDCTableMetadata) ([]mysqlCDCTableMetadata, error) {
	return mysqlCDCQueryTruncatesAfter(queryEvent, defaultDatabase, metaByAlias, gomysql.Position{}, nil)
}

func mysqlCDCQueryTruncatesAfter(queryEvent *replication.QueryEvent, defaultDatabase string, metaByAlias map[string]mysqlCDCTableMetadata, eventPosition gomysql.Position, startByTable map[string]gomysql.Position) ([]mysqlCDCTableMetadata, error) {
	query := normalizeMySQLCDCQuery(string(queryEvent.Query))
	if !mysqlCDCIsDDL(query) {
		return nil, nil
	}
	if mysqlCDCIsKnownNonTableDDL(query) {
		return nil, nil
	}
	statement, err := sqlparser.NewTestParser().ParseStrictDDL(query)
	if err != nil {
		if !mysqlCDCUnparseableDDLMentionsCaptured(query, defaultDatabase, metaByAlias) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to parse MySQL DDL event %q: %w", query, err)
	}
	ddl, ok := statement.(sqlparser.DDLStatement)
	if !ok {
		if databaseDDL, databaseOK := statement.(sqlparser.DBDDLStatement); databaseOK {
			if !strings.EqualFold(databaseDDL.GetDatabaseName(), defaultDatabase) {
				return nil, nil
			}
			if _, dropsDatabase := statement.(*sqlparser.DropDatabase); dropsDatabase {
				return nil, fmt.Errorf("MySQL CDC cannot safely apply DDL event %q for the captured database; run with --full-refresh to rebuild the destination", query)
			}
			return nil, nil
		}
		return nil, nil
	}

	eventDatabase := string(queryEvent.Schema)
	if eventDatabase == "" {
		eventDatabase = defaultDatabase
	}
	affected := make(map[string]mysqlCDCTableMetadata)
	affectedTables := ddl.AffectedTables()
	if alter, alterOK := statement.(*sqlparser.AlterTable); alterOK && alter.PartitionSpec != nil && alter.PartitionSpec.Action == sqlparser.ExchangeAction {
		affectedTables = append(affectedTables, alter.PartitionSpec.TableName)
	}
	for _, table := range affectedTables {
		database := table.Qualifier.String()
		if database == "" {
			database = eventDatabase
		}
		tableKey := mysqlCDCEventTableName(database, table.Name.String(), defaultDatabase)
		if meta, exists := metaByAlias[strings.ToLower(tableKey)]; exists {
			if start, bounded := startByTable[meta.Name]; bounded && eventPosition.Compare(start) <= 0 {
				continue
			}
			affected[meta.Name] = meta
		}
	}
	if len(affected) == 0 {
		return nil, nil
	}
	if _, ok := statement.(*sqlparser.TruncateTable); !ok {
		return nil, fmt.Errorf("MySQL CDC cannot safely apply DDL event %q for a captured table; run with --full-refresh to rebuild the destination", query)
	}

	tables := make([]string, 0, len(affected))
	for table := range affected {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	truncated := make([]mysqlCDCTableMetadata, 0, len(tables))
	for _, table := range tables {
		truncated = append(truncated, affected[table])
	}
	return truncated, nil
}

// mysqlCDCUnparseableDDLMentionsCaptured reports whether a DDL statement the
// parser rejected could refer to a captured table or database. The binlog
// carries DDL from every database on the server, and table-affecting DDL must
// name its object, so a statement that mentions no captured identifier cannot
// touch a captured table and is safe to skip; anything that does mention one
// fails closed at the caller.
func mysqlCDCUnparseableDDLMentionsCaptured(query, defaultDatabase string, metaByAlias map[string]mysqlCDCTableMetadata) bool {
	names := make(map[string]struct{}, 2*len(metaByAlias)+1)
	if defaultDatabase != "" {
		names[strings.ToLower(defaultDatabase)] = struct{}{}
	}
	for _, meta := range metaByAlias {
		if meta.SourceSchema != "" {
			names[strings.ToLower(meta.SourceSchema)] = struct{}{}
		}
		if meta.SourceName != "" {
			names[strings.ToLower(meta.SourceName)] = struct{}{}
		}
	}
	lowerQuery := strings.ToLower(query)
	tokens := mysqlCDCIdentifierTokens(lowerQuery)
	for name := range names {
		if mysqlCDCIsIdentifierWord(name) {
			if _, ok := tokens[name]; ok {
				return true
			}
			continue
		}
		if strings.Contains(lowerQuery, name) {
			return true
		}
	}
	return false
}

func mysqlCDCIdentifierTokens(lowerQuery string) map[string]struct{} {
	tokens := make(map[string]struct{})
	start := -1
	for i := 0; i <= len(lowerQuery); i++ {
		if i < len(lowerQuery) && mysqlCDCIsIdentifierByte(lowerQuery[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			tokens[lowerQuery[start:i]] = struct{}{}
			start = -1
		}
	}
	return tokens
}

func mysqlCDCIsIdentifierByte(b byte) bool {
	return b == '_' || b == '$' || b >= 0x80 ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z')
}

func mysqlCDCIsIdentifierWord(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !mysqlCDCIsIdentifierByte(name[i]) {
			return false
		}
	}
	return true
}

func mysqlCDCIsDDL(query string) bool {
	fields := strings.Fields(normalizeMySQLCDCQuery(query))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToUpper(fields[0]) {
	case "ALTER", "CREATE", "DROP", "RENAME", "TRUNCATE":
		return true
	default:
		return false
	}
}

func normalizeMySQLCDCQuery(query string) string {
	return strings.TrimSpace(stripMySQLCDCComments(query))
}

func stripMySQLCDCComments(query string) string {
	var result strings.Builder
	result.Grow(len(query))
	for i := 0; i < len(query); {
		switch query[i] {
		case '\'', '"', '`':
			quote := query[i]
			start := i
			i++
			for i < len(query) {
				if query[i] == '\\' && quote != '`' && i+1 < len(query) {
					i += 2
					continue
				}
				if query[i] == quote {
					if i+1 < len(query) && query[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			result.WriteString(query[start:i])
		case '/':
			if i+1 >= len(query) || query[i+1] != '*' {
				result.WriteByte(query[i])
				i++
				continue
			}
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				result.WriteString(query[i:])
				return result.String()
			}
			end += i + 2
			executableBody := -1
			if i+2 < len(query) && query[i+2] == '!' {
				executableBody = i + 3
			} else if i+3 < len(query) && (query[i+2] == 'M' || query[i+2] == 'm') && query[i+3] == '!' {
				executableBody = i + 4
			}
			if executableBody >= 0 {
				body := strings.TrimSpace(query[executableBody:end])
				versionEnd := 0
				for versionEnd < len(body) && body[versionEnd] >= '0' && body[versionEnd] <= '9' {
					versionEnd++
				}
				if versionEnd >= 5 {
					body = strings.TrimSpace(body[versionEnd:])
				}
				result.WriteByte(' ')
				result.WriteString(stripMySQLCDCComments(body))
				result.WriteByte(' ')
			} else {
				result.WriteByte(' ')
			}
			i = end + 2
		case '#':
			for i < len(query) && query[i] != '\n' {
				i++
			}
			result.WriteByte(' ')
		case '-':
			if i+2 >= len(query) || query[i+1] != '-' || query[i+2] > ' ' {
				result.WriteByte(query[i])
				i++
				continue
			}
			for i < len(query) && query[i] != '\n' {
				i++
			}
			result.WriteByte(' ')
		default:
			result.WriteByte(query[i])
			i++
		}
	}
	return result.String()
}

func mysqlCDCIsKnownNonTableDDL(query string) bool {
	fields := strings.Fields(strings.ToUpper(query))
	if len(fields) < 2 {
		return false
	}
	for i := 1; i < len(fields); i++ {
		field := strings.Trim(fields[i], "(),;")
		switch field {
		case "TABLE", "DATABASE", "SCHEMA", "INDEX":
			return false
		case "EVENT", "FUNCTION", "INSTANCE", "PROCEDURE", "ROLE", "SERVER", "TABLESPACE", "TRIGGER", "USER", "VIEW":
			return true
		case "LOGFILE":
			return i+1 < len(fields) && strings.Trim(fields[i+1], "(),;") == "GROUP"
		case "RESOURCE":
			return i+1 < len(fields) && strings.Trim(fields[i+1], "(),;") == "GROUP"
		case "SPATIAL":
			return i+2 < len(fields) && strings.Trim(fields[i+1], "(),;") == "REFERENCE" && strings.Trim(fields[i+2], "(),;") == "SYSTEM"
		}
	}
	return false
}

func mysqlCDCXAQuery(query string) (mysqlCDCXAAction, string, bool, error) {
	normalized := normalizeMySQLCDCQuery(query)
	normalized = strings.TrimSuffix(normalized, ";")
	tokenizer := sqlparser.NewTestParser().NewStringTokenizer(normalized)
	_, first := mysqlCDCNextNonCommentToken(tokenizer)
	if !strings.EqualFold(first, "XA") {
		return mysqlCDCXAActionNone, "", false, nil
	}
	_, command := mysqlCDCNextNonCommentToken(tokenizer)
	var action mysqlCDCXAAction
	switch strings.ToUpper(command) {
	case "START", "BEGIN":
		action = mysqlCDCXAActionStart
	case "END":
		action = mysqlCDCXAActionEnd
	case "COMMIT":
		action = mysqlCDCXAActionCommit
	case "ROLLBACK":
		action = mysqlCDCXAActionRollback
	default:
		return mysqlCDCXAActionNone, "", false, nil
	}
	remainder := strings.TrimSpace(sqlparser.StripLeadingComments(normalized[tokenizer.Pos:]))
	xaID, modifier, err := mysqlCDCCanonicalXID(remainder)
	if err != nil {
		return mysqlCDCXAActionNone, "", false, fmt.Errorf("failed to parse MySQL XA event %q: %w", normalized, err)
	}
	modifier = strings.Join(strings.Fields(strings.ToUpper(modifier)), " ")
	onePhase := false
	switch action {
	case mysqlCDCXAActionStart:
		if modifier != "" && modifier != "JOIN" && modifier != "RESUME" {
			return mysqlCDCXAActionNone, "", false, fmt.Errorf("unsupported MySQL XA START modifier %q", modifier)
		}
		onePhase = modifier != ""
	case mysqlCDCXAActionEnd:
		if modifier != "" && modifier != "SUSPEND" && modifier != "SUSPEND FOR MIGRATE" {
			return mysqlCDCXAActionNone, "", false, fmt.Errorf("unsupported MySQL XA END modifier %q", modifier)
		}
	case mysqlCDCXAActionCommit:
		if modifier != "" && modifier != "ONE PHASE" {
			return mysqlCDCXAActionNone, "", false, fmt.Errorf("unsupported MySQL XA COMMIT modifier %q", modifier)
		}
		onePhase = modifier == "ONE PHASE"
	case mysqlCDCXAActionRollback:
		if modifier != "" {
			return mysqlCDCXAActionNone, "", false, fmt.Errorf("unsupported MySQL XA ROLLBACK modifier %q", modifier)
		}
	}
	return action, xaID, onePhase, nil
}

func mysqlCDCNextNonCommentToken(tokenizer *sqlparser.Tokenizer) (int, string) {
	for {
		token, value := tokenizer.Scan()
		if token != sqlparser.COMMENT {
			return token, value
		}
	}
}

func mysqlCDCCanonicalXID(input string) (string, string, error) {
	parts, modifier, err := splitMySQLXID(input)
	if err != nil {
		return "", "", err
	}
	if len(parts) == 0 || len(parts) > 3 {
		return "", "", fmt.Errorf("an XID must contain one to three components")
	}
	gtrid, err := mysqlCDCXIDBytes(parts[0])
	if err != nil {
		return "", "", fmt.Errorf("invalid XID gtrid: %w", err)
	}
	var bqual []byte
	if len(parts) >= 2 {
		bqual, err = mysqlCDCXIDBytes(parts[1])
		if err != nil {
			return "", "", fmt.Errorf("invalid XID bqual: %w", err)
		}
	}
	formatID := uint64(1)
	if len(parts) == 3 {
		formatID, err = strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 32)
		if err != nil {
			return "", "", fmt.Errorf("invalid XID format ID: %w", err)
		}
	}
	if len(gtrid) == 0 || len(gtrid) > 64 || len(bqual) > 64 || len(gtrid)+len(bqual) > 128 {
		return "", "", fmt.Errorf("XID component lengths are out of range")
	}
	return fmt.Sprintf("%d:%s:%s", formatID, hex.EncodeToString(gtrid), hex.EncodeToString(bqual)), modifier, nil
}

func splitMySQLXID(input string) ([]string, string, error) {
	input = strings.TrimSpace(input)
	var parts []string
	start := 0
	quote := byte(0)
	for i := 0; i < len(input); i++ {
		c := input[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(input) {
				i++
				continue
			}
			if c == quote {
				if i+1 < len(input) && input[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == ',' {
			parts = append(parts, strings.TrimSpace(input[start:i]))
			start = i + 1
		}
	}
	if quote != 0 {
		return nil, "", fmt.Errorf("unterminated quoted XID component")
	}
	parts = append(parts, strings.TrimSpace(input[start:]))
	last := parts[len(parts)-1]
	if split := mysqlXIDModifierOffset(last); split >= 0 {
		parts[len(parts)-1] = strings.TrimSpace(last[:split])
		return parts, strings.TrimSpace(last[split:]), nil
	}
	return parts, "", nil
}

func mysqlXIDModifierOffset(input string) int {
	quote := byte(0)
	for i := 0; i < len(input); i++ {
		c := input[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(input) {
				i++
				continue
			}
			if c == quote {
				if i+1 < len(input) && input[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			return i
		}
	}
	return -1
}

func mysqlCDCXIDBytes(input string) ([]byte, error) {
	expr, err := sqlparser.NewTestParser().ParseExpr(strings.TrimSpace(input))
	if err != nil {
		return nil, err
	}
	literal, ok := expr.(*sqlparser.Literal)
	if !ok {
		return nil, fmt.Errorf("XID component must be a string, hex, or bit literal")
	}
	switch literal.Type {
	case sqlparser.StrVal, sqlparser.HexVal, sqlparser.HexNum, sqlparser.BitNum:
	default:
		return nil, fmt.Errorf("XID component must be a string, hex, or bit literal")
	}
	value, err := sqlparser.LiteralToValue(literal)
	if err != nil {
		return nil, err
	}
	return value.Raw(), nil
}

func mysqlCDCXAPrepare(event *replication.BinlogEvent) (string, bool, error) {
	generic, ok := event.Event.(*replication.GenericEvent)
	if !ok || len(generic.Data) < 13 {
		return "", false, fmt.Errorf("MySQL CDC encountered a malformed XA PREPARE event")
	}
	data := generic.Data
	formatID := binary.LittleEndian.Uint32(data[1:5])
	gtridLength := int(binary.LittleEndian.Uint32(data[5:9]))
	bqualLength := int(binary.LittleEndian.Uint32(data[9:13]))
	if gtridLength <= 0 || gtridLength > 64 || bqualLength < 0 || bqualLength > 64 || gtridLength+bqualLength > 128 || len(data) != 13+gtridLength+bqualLength {
		return "", false, fmt.Errorf("MySQL CDC encountered invalid XID lengths in an XA PREPARE event")
	}
	gtrid := data[13 : 13+gtridLength]
	bqual := data[13+gtridLength:]
	xaID := fmt.Sprintf("%d:%s:%s", formatID, hex.EncodeToString(gtrid), hex.EncodeToString(bqual))
	return xaID, data[0] != 0, nil
}

func readMySQLCDCEvent(ctx context.Context, streamer mysqlCDCBinlogStreamer) (*replication.BinlogEvent, error, error) {
	eventCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	event, err := streamer.GetEvent(eventCtx)
	eventCtxErr := eventCtx.Err()
	cancel()
	return event, err, eventCtxErr
}

func isMySQLCDCReadPollTimeout(ctx context.Context, eventErr, eventCtxErr error) bool {
	return ctx.Err() == nil && errors.Is(eventErr, context.DeadlineExceeded) && errors.Is(eventCtxErr, context.DeadlineExceeded)
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

func mysqlRowsEventToChanges(eventType replication.EnumRowsEventType, rows [][]interface{}, fullSchema *schema.TableSchema, outputSchema *schema.TableSchema, pos gomysql.Position) ([]mysqlCDCChange, error) {
	changes, _, err := mysqlRowsEventToChangesFromSequence(eventType, rows, fullSchema, outputSchema, pos, 0)
	return changes, err
}

func mysqlRowsEventToChangesFromSequence(eventType replication.EnumRowsEventType, rows [][]interface{}, fullSchema *schema.TableSchema, outputSchema *schema.TableSchema, pos gomysql.Position, rowSeq int) ([]mysqlCDCChange, int, error) {
	fullSourceColumns := sourceColumnsWithoutMySQLCDC(fullSchema)
	outputSourceColumns := sourceColumnsWithoutMySQLCDC(outputSchema)
	indexes, err := sourceIndexes(fullSourceColumns, outputSourceColumns)
	if err != nil {
		return nil, rowSeq, err
	}

	pkIndexes := primaryKeyIndexes(fullSourceColumns, outputSchema.PrimaryKeys)
	changes := make([]mysqlCDCChange, 0, len(rows))

	switch eventType {
	case replication.EnumRowsEventTypeInsert:
		for _, row := range rows {
			values, err := projectMySQLCDCRow(row, fullSourceColumns, outputSourceColumns, indexes)
			if err != nil {
				return nil, rowSeq, err
			}
			changes = append(changes, mysqlCDCChange{values: values, lsn: formatStoredMySQLPosition(pos, rowSeq), deleted: false})
			rowSeq++
		}
	case replication.EnumRowsEventTypeDelete:
		for _, row := range rows {
			values, err := projectMySQLCDCRow(row, fullSourceColumns, outputSourceColumns, indexes)
			if err != nil {
				return nil, rowSeq, err
			}
			changes = append(changes, mysqlCDCChange{values: values, lsn: formatStoredMySQLPosition(pos, rowSeq), deleted: true})
			rowSeq++
		}
	case replication.EnumRowsEventTypeUpdate:
		if len(rows)%2 != 0 {
			return nil, rowSeq, fmt.Errorf("update rows event has odd row count")
		}
		for i := 0; i < len(rows); i += 2 {
			before := rows[i]
			after := rows[i+1]
			if primaryKeyChanged(before, after, pkIndexes) {
				beforeValues, err := projectMySQLCDCRow(before, fullSourceColumns, outputSourceColumns, indexes)
				if err != nil {
					return nil, rowSeq, err
				}
				changes = append(changes, mysqlCDCChange{values: beforeValues, lsn: formatStoredMySQLPosition(pos, rowSeq), deleted: true})
				rowSeq++
			}
			afterValues, err := projectMySQLCDCRow(after, fullSourceColumns, outputSourceColumns, indexes)
			if err != nil {
				return nil, rowSeq, err
			}
			changes = append(changes, mysqlCDCChange{values: afterValues, lsn: formatStoredMySQLPosition(pos, rowSeq), deleted: false})
			rowSeq++
		}
	default:
		return nil, rowSeq, fmt.Errorf("unsupported MySQL rows event type: %s", eventType)
	}

	return changes, rowSeq, nil
}

func projectMySQLCDCRow(row []interface{}, fullSourceColumns []schema.Column, outputSourceColumns []schema.Column, indexes []int) ([]interface{}, error) {
	// The binlog row is matched to the table schema by position, so any column
	// count mismatch means the mapping cannot be trusted: a column added or
	// dropped since the schema was read (or a non-FULL row image) would silently
	// shift values into the wrong columns.
	if len(row) != len(fullSourceColumns) {
		return nil, fmt.Errorf("binlog row image has %d columns but the table schema has %d; the table structure changed since the sync started, or binlog_row_image is not FULL; run with --full-refresh to rebuild the destination safely", len(row), len(fullSourceColumns))
	}

	values := make([]interface{}, len(outputSourceColumns))
	for i, sourceIdx := range indexes {
		if sourceIdx < 0 || sourceIdx >= len(row) {
			values[i] = nil
			continue
		}
		if _, partialJSON := row[sourceIdx].(*replication.JsonDiff); partialJSON {
			return nil, fmt.Errorf("binlog row image contains a partial JSON value; producer sessions must leave binlog_row_value_options empty")
		}
		values[i] = convertMySQLCDCBinlogValue(row[sourceIdx], outputSourceColumns[i])
	}
	return values, nil
}

// convertMySQLCDCBinlogValue converts a value decoded from a binlog row image.
// The binlog stores integers without signedness, and go-mysql always decodes
// them as signed, so values for unsigned columns are reinterpreted here. The
// column's DataType is already widened by MapMySQLToDataType to fit the
// unsigned range.
func convertMySQLCDCBinlogValue(value interface{}, col schema.Column) interface{} {
	if col.Unsigned {
		switch v := value.(type) {
		case int8:
			return int16(uint8(v))
		case int16:
			return int32(uint16(v))
		case int32:
			if col.DataType == schema.TypeInt32 {
				// MEDIUMINT UNSIGNED: go-mysql sign-extends the 3-byte value.
				return int32(uint32(v) & 0xFFFFFF)
			}
			return int64(uint32(v))
		case int64:
			// BIGINT UNSIGNED: the column is DECIMAL(20,0); the decimal string
			// preserves values above MaxInt64.
			return strconv.FormatUint(uint64(v), 10)
		}
	}
	return convertMySQLCDCValue(value, col)
}

func convertMySQLCDCValue(value interface{}, col schema.Column) interface{} {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		return strings.Clone(v)
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
		if !primaryKeyValuesEqual(before[idx], after[idx]) {
			return true
		}
	}
	return false
}

func primaryKeyValuesEqual(before, after interface{}) bool {
	beforeTime, ok := before.(time.Time)
	if ok {
		afterTime, ok := after.(time.Time)
		return ok && beforeTime.Equal(afterTime)
	}
	return reflect.DeepEqual(before, after)
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
	if rows != nil {
		if err := rows.Err(); err != nil {
			releaseMySQLCDCBuilders(builders)
			return nil, 0, fmt.Errorf("error iterating MySQL rows: %w", err)
		}
	}

	if rowCount == 0 {
		releaseMySQLCDCBuilders(builders)
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

type mysqlCDCLineageQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

func loadMySQLCDCLineageIdentity(ctx context.Context, q mysqlCDCLineageQueryer, flavor string) (string, error) {
	if flavor == gomysql.MariaDBFlavor {
		var identity string
		if err := q.QueryRowContext(ctx, "SELECT @@GLOBAL.server_uid").Scan(&identity); err != nil {
			return "", fmt.Errorf("MariaDB CDC requires a server version exposing immutable @@GLOBAL.server_uid for checkpoint lineage validation: %w", err)
		}
		if strings.TrimSpace(identity) == "" {
			return "", fmt.Errorf("MariaDB CDC server_uid is empty; checkpoint lineage cannot be validated safely")
		}
		return "mariadb:" + identity, nil
	}

	var identity, gtidMode string
	if err := q.QueryRowContext(ctx, "SELECT @@GLOBAL.server_uuid, @@GLOBAL.gtid_mode").Scan(&identity, &gtidMode); err != nil {
		return "", fmt.Errorf("failed to read MySQL server identity and GTID mode: %w", err)
	}
	if strings.TrimSpace(identity) == "" {
		return "", fmt.Errorf("MySQL server_uuid is empty; checkpoint lineage cannot be validated safely")
	}
	if !strings.EqualFold(strings.TrimSpace(gtidMode), "ON") {
		return "", fmt.Errorf("MySQL CDC requires gtid_mode=ON for lineage-safe checkpoints; current mode is %q", gtidMode)
	}
	return "mysql:" + identity, nil
}

func (s *MySQLCDCSource) currentMySQLCDCLineageGTIDSet(ctx context.Context) (string, error) {
	return currentMySQLCDCLineageGTIDSet(ctx, s.db, s.cdcConfig.Flavor)
}

func currentMySQLCDCLineageGTIDSet(ctx context.Context, q mysqlCDCLineageQueryer, flavor string) (string, error) {
	variable := "@@GLOBAL.gtid_executed"
	if flavor == gomysql.MariaDBFlavor {
		// gtid_binlog_state is the full per-(domain, server_id) history;
		// gtid_binlog_pos only holds the latest GTID per domain, so a
		// primary switch or server_id change would make containment fail
		// even though the binlog history is intact.
		variable = "@@GLOBAL.gtid_binlog_state"
	}
	var value sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT "+variable).Scan(&value); err != nil {
		return "", fmt.Errorf("failed to read MySQL CDC GTID history: %w", err)
	}
	return value.String, nil
}

func (s *MySQLCDCSource) checkpointForPosition(ctx context.Context, q mysqlCDCLineageQueryer, position gomysql.Position) (mysqlCDCCheckpoint, error) {
	checkpoint := mysqlCDCCheckpoint{Position: position, Identity: s.lineageIdentity}
	if s.lineageIdentity == "" {
		return checkpoint, nil
	}
	gtidSet, err := currentMySQLCDCLineageGTIDSet(ctx, q, s.cdcConfig.Flavor)
	if err != nil {
		return mysqlCDCCheckpoint{}, err
	}
	checkpoint.GTIDSet = gtidSet
	return checkpoint, nil
}

func mysqlCDCGTIDSetContains(flavor, current, checkpoint string) (bool, error) {
	currentSet, err := gomysql.ParseGTIDSet(flavor, strings.TrimSpace(current))
	if err != nil {
		return false, fmt.Errorf("failed to parse current MySQL CDC GTID history: %w", err)
	}
	checkpointSet, err := gomysql.ParseGTIDSet(flavor, strings.TrimSpace(checkpoint))
	if err != nil {
		return false, fmt.Errorf("failed to parse checkpoint MySQL CDC GTID history: %w", err)
	}
	return currentSet.Contain(checkpointSet), nil
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

func validateMySQLCDCSourceColumns(tableSchema *schema.TableSchema, table string) error {
	if tableSchema == nil {
		return nil
	}
	for _, column := range tableSchema.Columns {
		if destination.IsCDCMetaColumn(column.Name) {
			return fmt.Errorf("MySQL CDC source table %s contains reserved metadata column %q; rename the source column before enabling CDC", table, column.Name)
		}
	}
	return nil
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

func mysqlEventPosition(event *replication.BinlogEvent, current gomysql.Position) gomysql.Position {
	if rotate, ok := event.Event.(*replication.RotateEvent); ok {
		return gomysql.Position{Name: string(rotate.NextLogName), Pos: uint32(rotate.Position)}
	}

	pos := current
	if event.Header.LogPos > 0 {
		pos.Pos = event.Header.LogPos
	}
	return pos
}

var (
	storedMySQLPositionRegex    = regexp.MustCompile(`^(\d{20}):([^:]+):(\d{20})(?::\d{6,})?(?::l1:([^:]+):([^:]*))?$`)
	legacyStoredMySQLPositionRE = regexp.MustCompile(`^([^:]+):(\d{20})(?::\d{6,})?$`)
)

func formatStoredMySQLPosition(pos gomysql.Position, rowSeq int) string {
	return fmt.Sprintf("%020d:%s:%020d:%020d", mysqlBinlogSequence(pos.Name), pos.Name, pos.Pos, rowSeq)
}

func formatStoredMySQLCheckpoint(checkpoint mysqlCDCCheckpoint) string {
	position := formatStoredMySQLPosition(checkpoint.Position, 0)
	if checkpoint.Identity == "" {
		return position
	}
	encode := base64.RawURLEncoding.EncodeToString
	return position + ":l1:" + encode([]byte(checkpoint.Identity)) + ":" + encode([]byte(checkpoint.GTIDSet))
}

func parseStoredMySQLCheckpoint(stored string) (mysqlCDCCheckpoint, bool) {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return mysqlCDCCheckpoint{}, false
	}
	if matches := storedMySQLPositionRegex.FindStringSubmatch(stored); len(matches) == 6 {
		pos, err := strconv.ParseUint(matches[3], 10, 32)
		if err != nil {
			return mysqlCDCCheckpoint{}, false
		}
		checkpoint := mysqlCDCCheckpoint{Position: gomysql.Position{Name: matches[2], Pos: uint32(pos)}}
		if matches[4] != "" {
			identity, err := base64.RawURLEncoding.DecodeString(matches[4])
			if err != nil {
				return mysqlCDCCheckpoint{}, false
			}
			gtidSet, err := base64.RawURLEncoding.DecodeString(matches[5])
			if err != nil {
				return mysqlCDCCheckpoint{}, false
			}
			checkpoint.Identity = string(identity)
			checkpoint.GTIDSet = string(gtidSet)
		}
		return checkpoint, true
	}
	if matches := legacyStoredMySQLPositionRE.FindStringSubmatch(stored); len(matches) == 3 {
		pos, err := strconv.ParseUint(matches[2], 10, 32)
		if err != nil {
			return mysqlCDCCheckpoint{}, false
		}
		return mysqlCDCCheckpoint{Position: gomysql.Position{Name: matches[1], Pos: uint32(pos)}}, true
	}
	return mysqlCDCCheckpoint{}, false
}

func parseStoredMySQLPosition(stored string) (gomysql.Position, bool) {
	checkpoint, ok := parseStoredMySQLCheckpoint(stored)
	return checkpoint.Position, ok
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
