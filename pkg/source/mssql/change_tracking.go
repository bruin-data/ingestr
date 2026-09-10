package mssql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	ctVersionWidth = 20

	defaultCTPollInterval = 1 * time.Second

	// Bounds on the idle-poll heartbeat, whose interval is derived from the
	// database's CHANGE_RETENTION.
	maxCTHeartbeatInterval = 5 * time.Minute
	minCTHeartbeatInterval = 5 * time.Second
)

var ctMetadataColumns = []schema.Column{
	{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
	{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
	{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
}

type ctConfig struct {
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
}

type MSSQLChangeTrackingSource struct {
	MSSQLSource
	ctConfig ctConfig
	lag      ctLagState
}

type ctLagState struct {
	streaming atomic.Bool

	mu        sync.Mutex
	target    int64
	durable   int64
	seen      bool
	updatedAt time.Time
}

// ctCommitToken carries the version a read window ended at. The streaming
// executor hands it back only once the destination write is durable.
type ctCommitToken struct {
	version int64
}

// ctHeartbeatGate throttles the idle restamp that keeps the destination's
// _cdc_lsn cursor inside CHANGE_RETENTION. A zero interval restamps on every
// idle read, which is what a one-shot run wants.
type ctHeartbeatGate struct {
	interval time.Duration
	next     time.Time
}

func newCTHeartbeatGate(interval time.Duration) *ctHeartbeatGate {
	return &ctHeartbeatGate{interval: interval}
}

// A nil gate never heartbeats: the caller advanced the cursor by other means.
func (g *ctHeartbeatGate) shouldEmit(changeRows int64, now time.Time) bool {
	if g == nil || changeRows > 0 {
		return false
	}
	return !now.Before(g.next)
}

func (g *ctHeartbeatGate) cursorAdvanced(now time.Time) {
	if g == nil {
		return
	}
	g.next = now.Add(g.interval)
}

// emitCTResult unblocks on cancellation rather than wedging the producer
// goroutine when the consumer has gone away.
func emitCTResult(ctx context.Context, results chan<- source.RecordBatchResult, res source.RecordBatchResult) error {
	select {
	case results <- res:
		return nil
	case <-ctx.Done():
		if res.Batch != nil {
			res.Batch.Release()
		}
		return ctx.Err()
	}
}

type changeTrackingTable struct {
	source      *MSSQLChangeTrackingSource
	tableName   string
	tableSchema *schema.TableSchema
	primaryKeys []string
	strategy    config.IncrementalStrategy
}

type ctVersionExpiredError struct {
	table      string
	version    int64
	minVersion int64
}

func (e *ctVersionExpiredError) Error() string {
	return fmt.Sprintf("SQL Server Change Tracking version %d is no longer valid for %s; minimum valid version is %d; run with --full-refresh to rebuild the destination from a new snapshot", e.version, e.table, e.minVersion)
}

func NewMSSQLChangeTrackingSource() *MSSQLChangeTrackingSource {
	return &MSSQLChangeTrackingSource{ctConfig: ctConfig{PollInterval: defaultCTPollInterval}}
}

func (s *MSSQLChangeTrackingSource) Schemes() []string {
	return []string{"mssql+ct", "sqlserver+ct", "azuresql+ct", "azure-sql+ct"}
}

func (s *MSSQLChangeTrackingSource) Connect(ctx context.Context, uri string) error {
	cfg, normalizedURI, err := parseChangeTrackingURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse SQL Server Change Tracking URI: %w", err)
	}
	s.ctConfig = cfg

	connStr, driverName, err := URIToConnString(normalizedURI)
	if err != nil {
		return fmt.Errorf("failed to parse SQL Server URI: %w", err)
	}

	db, err := sql.Open(driverName, connStr)
	if err != nil {
		return fmt.Errorf("failed to open SQL Server connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping SQL Server: %w", err)
	}

	s.db = db
	s.uri = uri
	s.guidConversion = guidConversionEnabled(connStr)

	if err := s.ensureDatabaseChangeTracking(ctx); err != nil {
		_ = db.Close()
		s.db = nil
		return err
	}

	return nil
}

func (s *MSSQLChangeTrackingSource) HandlesIncrementality() bool {
	return true
}

// SupportsStreaming reports that Change Tracking can run in continuous mode:
// each poll asks the server for the current version and reads the changes up
// to it, exactly as a one-shot run does.
func (s *MSSQLChangeTrackingSource) SupportsStreaming() bool {
	return true
}

// DefaultStreamingStrategy returns merge, the only strategy Change Tracking
// supports: changes are keyed by primary key and deletes are soft.
func (s *MSSQLChangeTrackingSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

// ReplicationLag reports how far the durable destination version trails the
// server's current version. Change Tracking exposes neither a byte offset nor a
// commit time for a version, so only the positions are meaningful.
func (s *MSSQLChangeTrackingSource) ReplicationLag() (source.LagSnapshot, bool) {
	if !s.lag.streaming.Load() {
		return source.LagSnapshot{}, false
	}

	s.lag.mu.Lock()
	defer s.lag.mu.Unlock()
	if !s.lag.seen {
		return source.LagSnapshot{}, false
	}
	return source.LagSnapshot{
		Source:          "mssql_ct",
		ServerPosition:  formatCTVersion(s.lag.target),
		DurablePosition: formatCTVersion(s.lag.durable),
		CaughtUp:        s.lag.durable >= s.lag.target,
		UpdatedAt:       s.lag.updatedAt,
	}, true
}

// CommitStream records the version whose rows the destination has made
// durable. Until it is called, progress within a read window is not lag.
func (s *MSSQLChangeTrackingSource) CommitStream(_ context.Context, token any) error {
	commit, ok := token.(ctCommitToken)
	if !ok {
		return fmt.Errorf("unexpected SQL Server Change Tracking commit token %T", token)
	}
	s.noteCTDurableVersion(commit.version)
	return nil
}

func (s *MSSQLChangeTrackingSource) noteCTDurableVersion(version int64) {
	s.lag.mu.Lock()
	defer s.lag.mu.Unlock()
	if version > s.lag.durable {
		s.lag.durable = version
	}
	if version > s.lag.target {
		s.lag.target = version
	}
	s.lag.seen = true
	s.lag.updatedAt = time.Now()
}

func (s *MSSQLChangeTrackingSource) noteCTServerVersion(version int64) {
	if !s.lag.streaming.Load() {
		return
	}
	s.lag.mu.Lock()
	defer s.lag.mu.Unlock()
	if version > s.lag.target {
		s.lag.target = version
	}
	s.lag.seen = true
	s.lag.updatedAt = time.Now()
}

func (s *MSSQLChangeTrackingSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}

	if _, ok := source.IsCustomQuery(req.Name); ok {
		return nil, fmt.Errorf("custom queries are not supported for SQL Server Change Tracking sources")
	}

	strategy, err := resolveChangeTrackingStrategy(req.Strategy, req.StrategySet, req.FullRefresh)
	if err != nil {
		return nil, err
	}

	if err := s.ensureTableChangeTracking(ctx, req.Name); err != nil {
		return nil, err
	}

	tableSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}
	if len(pks) == 0 {
		return nil, fmt.Errorf("SQL Server Change Tracking table %s has no primary key; provide --primary-key or add a primary key to the source table", req.Name)
	}

	tableSchema.PrimaryKeys = pks
	tableSchema = addCTColumns(tableSchema)

	return &changeTrackingTable{
		source:      s,
		tableName:   req.Name,
		tableSchema: tableSchema,
		primaryKeys: pks,
		strategy:    strategy,
	}, nil
}

func (t *changeTrackingTable) Name() string {
	return t.tableName
}

func (t *changeTrackingTable) PrimaryKeys() []string {
	return t.primaryKeys
}

func (t *changeTrackingTable) IncrementalKey() string {
	return ""
}

func (t *changeTrackingTable) Strategy() config.IncrementalStrategy {
	return t.strategy
}

func (t *changeTrackingTable) HasKnownSchema() bool {
	return true
}

func (t *changeTrackingTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.tableSchema, nil
}

func (t *changeTrackingTable) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if opts.Limit > 0 {
		return nil, fmt.Errorf("SQL Server Change Tracking sources do not support --sql-limit because partial snapshots cannot safely advance the resume cursor")
	}
	if err := validateCTExcludeColumns(opts.ExcludeColumns, t.primaryKeys); err != nil {
		return nil, err
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		t.source.lag.streaming.Store(opts.Streaming)

		tableSchema := t.tableSchema
		if opts.Schema != nil {
			tableSchema = opts.Schema
		}

		pollInterval := t.source.ctConfig.PollInterval
		if pollInterval <= 0 {
			pollInterval = defaultCTPollInterval
		}
		heartbeatInterval := time.Duration(0)
		if opts.Streaming {
			heartbeatInterval = t.source.ctConfig.HeartbeatInterval
			if heartbeatInterval <= 0 {
				heartbeatInterval = maxCTHeartbeatInterval
			}
		}

		version, resumed := parseStoredCTVersion(opts.CDCResumeLSN)
		// A nil gate skips the heartbeat: a fresh snapshot's rows already
		// carry its version, so the first change read has nothing to restamp.
		var heartbeat *ctHeartbeatGate
		if resumed {
			heartbeat = newCTHeartbeatGate(heartbeatInterval)
		} else {
			snapshotVersion, err := t.source.snapshotCTTable(ctx, t.tableName, tableSchema, opts, results)
			if err != nil {
				_ = emitCTResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)})
				return
			}
			// --full-refresh and --stream are mutually exclusive at the
			// config layer, so a refresh always stops after the snapshot.
			if opts.FullRefresh {
				return
			}
			version = snapshotVersion
		}

		// The starting version is already in the destination, either as the
		// resume cursor or as the snapshot rows just emitted.
		t.source.noteCTDurableVersion(version)

		for {
			next, err := t.source.readCTChanges(ctx, t.tableName, tableSchema, t.primaryKeys, version, opts, results, heartbeat)
			if err != nil {
				_ = emitCTResult(ctx, results, source.RecordBatchResult{Err: err})
				return
			}
			version = next

			if !opts.Streaming {
				return
			}
			// Past the first pass the snapshot version is no longer fresh, so
			// idle polls take over keeping the cursor current.
			if heartbeat == nil {
				heartbeat = newCTHeartbeatGate(heartbeatInterval)
			}
			if err := emitCTResult(ctx, results, source.RecordBatchResult{
				CommitToken: ctCommitToken{version: version},
			}); err != nil {
				return
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
			}
		}
	}()

	return results, nil
}

func parseChangeTrackingURI(raw string) (ctConfig, string, error) {
	cfg := ctConfig{PollInterval: defaultCTPollInterval}

	parsed, err := url.Parse(raw)
	if err != nil {
		return cfg, "", err
	}

	switch strings.ToLower(parsed.Scheme) {
	case "mssql+ct":
		parsed.Scheme = "mssql"
	case "sqlserver+ct":
		parsed.Scheme = "sqlserver"
	case "azuresql+ct":
		parsed.Scheme = "azuresql"
	case "azure-sql+ct":
		parsed.Scheme = "azure-sql"
	default:
		return cfg, "", fmt.Errorf("unsupported Change Tracking scheme: %s", parsed.Scheme)
	}

	query := parsed.Query()
	if poll := query.Get("poll_interval"); poll != "" {
		d, err := time.ParseDuration(poll)
		if err != nil {
			return cfg, "", fmt.Errorf("invalid poll_interval: %w", err)
		}
		if d <= 0 {
			return cfg, "", fmt.Errorf("poll_interval must be positive")
		}
		cfg.PollInterval = d
	}
	query.Del("poll_interval")
	parsed.RawQuery = query.Encode()

	return cfg, parsed.String(), nil
}

func (s *MSSQLChangeTrackingSource) ensureDatabaseChangeTracking(ctx context.Context) error {
	var (
		autoCleanup sql.NullBool
		retention   sql.NullInt64
		units       sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT is_auto_cleanup_on, retention_period, retention_period_units_desc
		FROM sys.change_tracking_databases
		WHERE database_id = DB_ID()
	`).Scan(&autoCleanup, &retention, &units)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("SQL Server Change Tracking is not enabled for the current database; run ALTER DATABASE ... SET CHANGE_TRACKING = ON first")
	}
	if err != nil {
		return fmt.Errorf("failed to check SQL Server Change Tracking status: %w", err)
	}

	s.ctConfig.HeartbeatInterval = ctHeartbeatInterval(autoCleanup.Bool, ctRetentionPeriod(retention, units))
	config.Debug("[MSSQL CT] Idle streams will restamp the resume cursor every %s", s.ctConfig.HeartbeatInterval)
	return nil
}

func ctRetentionPeriod(period sql.NullInt64, units sql.NullString) time.Duration {
	if !period.Valid || !units.Valid || period.Int64 <= 0 {
		return 0
	}
	switch strings.ToUpper(strings.TrimSpace(units.String)) {
	case "MINUTES":
		return time.Duration(period.Int64) * time.Minute
	case "HOURS":
		return time.Duration(period.Int64) * time.Hour
	case "DAYS":
		return time.Duration(period.Int64) * 24 * time.Hour
	default:
		return 0
	}
}

// ctHeartbeatInterval keeps the idle restamp well inside CHANGE_RETENTION: a
// database that expires versions in minutes needs a faster heartbeat than one
// retaining them for days. Cleanup being off leaves nothing to outrun.
func ctHeartbeatInterval(autoCleanup bool, retention time.Duration) time.Duration {
	if !autoCleanup || retention <= 0 {
		return maxCTHeartbeatInterval
	}
	return min(max(retention/4, minCTHeartbeatInterval), maxCTHeartbeatInterval)
}

func (s *MSSQLChangeTrackingSource) ensureTableChangeTracking(ctx context.Context, table string) error {
	schemaName, tableName := parseTableName(table)

	var enabled int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sys.change_tracking_tables AS ctt
		JOIN sys.tables AS t ON t.object_id = ctt.object_id
		JOIN sys.schemas AS ss ON ss.schema_id = t.schema_id
		WHERE ss.name = @p1
		  AND t.name = @p2
	`, schemaName, tableName).Scan(&enabled)
	if err != nil {
		return fmt.Errorf("failed to query SQL Server Change Tracking metadata for %s: %w", table, err)
	}
	if enabled == 0 {
		return fmt.Errorf("table %s is not enabled for SQL Server Change Tracking", table)
	}
	return nil
}

func resolveChangeTrackingStrategy(strategy config.IncrementalStrategy, strategySet, fullRefresh bool) (config.IncrementalStrategy, error) {
	if fullRefresh {
		switch strategy {
		case "", config.StrategyReplace, config.StrategyAppend, config.StrategyDeleteInsert, config.StrategyMerge, config.StrategySCD2, config.StrategyNone:
			return config.StrategyMerge, nil
		default:
			return "", fmt.Errorf("SQL Server Change Tracking sources require a valid incremental strategy when full-refresh is enabled; got %q", strategy)
		}
	}

	switch strategy {
	case "", config.StrategyMerge:
		return config.StrategyMerge, nil
	case config.StrategyReplace:
		if strategySet && !fullRefresh {
			return "", fmt.Errorf("SQL Server Change Tracking sources require %q incremental strategy unless full-refresh is enabled; got %q", config.StrategyMerge, strategy)
		}
		return config.StrategyMerge, nil
	default:
		return "", fmt.Errorf("SQL Server Change Tracking sources require %q incremental strategy; got %q", config.StrategyMerge, strategy)
	}
}

func addCTColumns(original *schema.TableSchema) *schema.TableSchema {
	result := *original
	result.Columns = make([]schema.Column, 0, len(original.Columns)+len(ctMetadataColumns))
	result.Columns = append(result.Columns, original.Columns...)
	result.Columns = append(result.Columns, ctMetadataColumns...)
	return &result
}

func sourceColumnsWithoutCT(tableSchema *schema.TableSchema) []schema.Column {
	columns := make([]schema.Column, 0, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		if destination.IsCDCColumn(col.Name) {
			continue
		}
		columns = append(columns, col)
	}
	return columns
}

type ctQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *MSSQLChangeTrackingSource) minValidCTVersion(ctx context.Context, q ctQueryer, table string) (int64, error) {
	var minVersion sql.NullInt64
	err := q.QueryRowContext(ctx, "SELECT CHANGE_TRACKING_MIN_VALID_VERSION(OBJECT_ID(@p1))", objectIDName(table)).Scan(&minVersion)
	if err != nil {
		return 0, fmt.Errorf("failed to get SQL Server Change Tracking minimum valid version for %s: %w", table, err)
	}
	if !minVersion.Valid {
		return 0, fmt.Errorf("table %s is not enabled for SQL Server Change Tracking", table)
	}
	return minVersion.Int64, nil
}

func (s *MSSQLChangeTrackingSource) currentCTVersion(ctx context.Context, q ctQueryer) (int64, error) {
	var version sql.NullInt64
	if err := q.QueryRowContext(ctx, "SELECT CHANGE_TRACKING_CURRENT_VERSION()").Scan(&version); err != nil {
		return 0, fmt.Errorf("failed to get SQL Server Change Tracking current version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return version.Int64, nil
}

func (s *MSSQLChangeTrackingSource) ensureCTVersionValid(ctx context.Context, q ctQueryer, table string, version int64) error {
	minVersion, err := s.minValidCTVersion(ctx, q, table)
	if err != nil {
		return err
	}
	if version < minVersion {
		return &ctVersionExpiredError{table: table, version: version, minVersion: minVersion}
	}
	return nil
}

// Integration tests use this to invalidate a cursor before CHANGETABLE runs.
var ctCursorValidationHook atomic.Pointer[func(ctx context.Context, sourceURI, table string) error]

// Probe before SNAPSHOT so fallback never re-emits a partially streamed scan.
func (s *MSSQLChangeTrackingSource) snapshotCTTable(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult) (int64, error) {
	useSnapshot, err := SnapshotIsolationAllowed(ctx, s.db)
	if err != nil {
		return 0, err
	}
	if useSnapshot {
		version, emittedRows, err := s.snapshotCTTableWithIsolation(ctx, table, tableSchema, opts, results, sql.LevelSnapshot)
		if err == nil {
			return version, nil
		}
		if emittedRows > 0 {
			return 0, fmt.Errorf("SNAPSHOT isolation snapshot for %s failed after emitting %d rows; not retrying with table lock to avoid duplicate records: %w", table, emittedRows, err)
		}
		if !IsSnapshotIsolationUnavailableError(err) {
			return 0, err
		}
		config.Debug("[MSSQL CT] SNAPSHOT isolation became unavailable (%v); retrying snapshot of %s with a table lock", err, table)
	} else {
		config.Debug("[MSSQL CT] Snapshot isolation is not allowed for this database; snapshotting %s with a table lock", table)
	}
	version, _, err := s.snapshotCTTableWithIsolation(ctx, table, tableSchema, opts, results, sql.LevelSerializable)
	return version, err
}

func resetCTConnection(ctx context.Context, conn *sql.Conn) {
	resetCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(resetCtx, "SET TRANSACTION ISOLATION LEVEL READ COMMITTED"); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = conn.Close()
}

func (s *MSSQLChangeTrackingSource) snapshotCTTableWithIsolation(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult, isolation sql.IsolationLevel) (int64, int64, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to acquire snapshot connection: %w", err)
	}
	defer resetCTConnection(ctx, conn)

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to begin snapshot transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	version, err := s.currentCTVersion(ctx, tx)
	if err != nil {
		return 0, 0, err
	}

	columns := tableSchema.Columns
	query := buildCTSnapshotQuery(table, sourceColumnsWithoutCT(tableSchema), version, isolation != sql.LevelSnapshot)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query snapshot for %s: %w", table, err)
	}

	emittedRows, err := s.rowsToCTBatches(ctx, rows, columns, opts, results)
	if err != nil {
		_ = rows.Close()
		return 0, emittedRows, err
	}
	if err := rows.Close(); err != nil {
		return 0, emittedRows, fmt.Errorf("failed to read snapshot for %s: %w", table, err)
	}
	if emittedRows == 0 && !opts.FullRefresh {
		if err := emitSyntheticCTHeartbeat(ctx, tableSchema.Columns, tableSchema.PrimaryKeys, version, results); err != nil {
			return 0, 0, err
		}
		emittedRows = 1
	}

	if err := tx.Commit(); err != nil {
		return 0, emittedRows, fmt.Errorf("failed to commit snapshot transaction: %w", err)
	}

	return version, emittedRows, nil
}

// Keep validation and enumeration in one SNAPSHOT transaction. The READ
// COMMITTED fallback revalidates after enumeration to detect cleanup races.
func (s *MSSQLChangeTrackingSource) readCTChanges(ctx context.Context, table string, tableSchema *schema.TableSchema, primaryKeys []string, fromVersion int64, opts source.ReadOptions, results chan<- source.RecordBatchResult, heartbeat *ctHeartbeatGate) (int64, error) {
	useSnapshot, err := SnapshotIsolationAllowed(ctx, s.db)
	if err != nil {
		return 0, err
	}
	if useSnapshot {
		readThroughVersion, emittedRows, err := s.readCTChangesWithIsolation(ctx, table, tableSchema, primaryKeys, fromVersion, opts, results, sql.LevelSnapshot, heartbeat)
		if err == nil {
			if emittedRows > 0 {
				heartbeat.cursorAdvanced(time.Now())
			}
			return readThroughVersion, nil
		}
		if emittedRows > 0 || !IsSnapshotIsolationUnavailableError(err) {
			return 0, wrapCTReadError(err, "SNAPSHOT")
		}
		config.Debug("[MSSQL CT] SNAPSHOT isolation became unavailable (%v); reading changes for %s under READ COMMITTED", err, table)
	} else {
		config.Debug("[MSSQL CT] Snapshot isolation is not allowed for this database; reading changes for %s under READ COMMITTED with post-read cursor validation", table)
	}

	readThroughVersion, emittedRows, err := s.readCTChangesWithIsolation(ctx, table, tableSchema, primaryKeys, fromVersion, opts, results, sql.LevelReadCommitted, heartbeat)
	if err != nil {
		return 0, wrapCTReadError(err, "READ COMMITTED")
	}
	if emittedRows > 0 {
		heartbeat.cursorAdvanced(time.Now())
	}
	return readThroughVersion, nil
}

func wrapCTReadError(err error, isolation string) error {
	var expired *ctVersionExpiredError
	if errors.As(err, &expired) {
		return err
	}
	return fmt.Errorf("failed to read SQL Server Change Tracking changes using %s isolation: %w", isolation, err)
}

func (s *MSSQLChangeTrackingSource) readCTChangesWithIsolation(ctx context.Context, table string, tableSchema *schema.TableSchema, primaryKeys []string, fromVersion int64, opts source.ReadOptions, results chan<- source.RecordBatchResult, isolation sql.IsolationLevel, heartbeat *ctHeartbeatGate) (int64, int64, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to acquire Change Tracking connection: %w", err)
	}
	defer resetCTConnection(ctx, conn)

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to begin Change Tracking transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.ensureCTVersionValid(ctx, tx, table, fromVersion); err != nil {
		return 0, 0, err
	}
	if hook := ctCursorValidationHook.Load(); hook != nil {
		if err := (*hook)(ctx, s.uri, table); err != nil {
			return 0, 0, err
		}
	}

	targetVersion, err := s.currentCTVersion(ctx, tx)
	if err != nil {
		return 0, 0, err
	}
	s.noteCTServerVersion(targetVersion)
	// A database version that has not moved cannot have aged past the cursor,
	// so an idle read has nothing to restamp.
	if targetVersion <= fromVersion {
		if err := tx.Commit(); err != nil {
			return 0, 0, fmt.Errorf("failed to commit Change Tracking transaction: %w", err)
		}
		return fromVersion, 0, nil
	}

	columns := tableSchema.Columns
	query := buildCTChangesQuery(table, sourceColumnsWithoutCT(tableSchema), primaryKeys)
	rows, err := tx.QueryContext(ctx, query, fromVersion, targetVersion)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query Change Tracking changes for %s: %w", table, err)
	}

	changeRows, err := s.rowsToCTBatches(ctx, rows, columns, opts, results)
	if err != nil {
		_ = rows.Close()
		return 0, changeRows, err
	}
	if err := rows.Close(); err != nil {
		return 0, changeRows, fmt.Errorf("failed to close Change Tracking rows for %s: %w", table, err)
	}

	emittedRows := changeRows
	if heartbeat.shouldEmit(changeRows, time.Now()) {
		if err := s.emitCTHeartbeat(ctx, tx, table, tableSchema, primaryKeys, targetVersion, opts, results); err != nil {
			return 0, emittedRows, err
		}
		emittedRows++
	}

	if isolation != sql.LevelSnapshot {
		if err := s.ensureCTVersionValid(ctx, tx, table, fromVersion); err != nil {
			return 0, emittedRows, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, emittedRows, fmt.Errorf("failed to commit Change Tracking transaction: %w", err)
	}

	return targetVersion, emittedRows, nil
}

func (s *MSSQLChangeTrackingSource) emitCTHeartbeat(ctx context.Context, tx *sql.Tx, table string, tableSchema *schema.TableSchema, primaryKeys []string, targetVersion int64, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	query := buildCTHeartbeatQuery(table, sourceColumnsWithoutCT(tableSchema), primaryKeys, targetVersion)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query Change Tracking heartbeat for %s: %w", table, err)
	}

	emittedRows, err := s.rowsToCTBatches(ctx, rows, tableSchema.Columns, opts, results)
	if err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("failed to read Change Tracking heartbeat for %s: %w", table, err)
	}
	if emittedRows == 0 {
		return emitSyntheticCTHeartbeat(ctx, tableSchema.Columns, primaryKeys, targetVersion, results)
	}
	return nil
}

func emitSyntheticCTHeartbeat(ctx context.Context, columns []schema.Column, primaryKeys []string, targetVersion int64, results chan<- source.RecordBatchResult) error {
	record, err := syntheticCTHeartbeatRecord(columns, primaryKeys, targetVersion)
	if err != nil {
		return err
	}
	return emitCTResult(ctx, results, source.RecordBatchResult{Batch: record})
}

func syntheticCTHeartbeatRecord(columns []schema.Column, primaryKeys []string, targetVersion int64) (arrow.RecordBatch, error) {
	arrowSchema := buildArrowSchema(columns)
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}
	defer func() {
		for _, b := range builders {
			b.Release()
		}
	}()

	pkSet := make(map[string]bool, len(primaryKeys))
	for _, pk := range primaryKeys {
		pkSet[strings.ToLower(pk)] = true
	}

	for i, col := range columns {
		value := syntheticCTHeartbeatValue(col, pkSet, targetVersion)
		if value == nil {
			builders[i].AppendNull()
			continue
		}
		arrowconv.AppendValue(builders[i], value)
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
	}
	defer func() {
		for _, arr := range arrays {
			arr.Release()
		}
	}()

	return array.NewRecordBatch(arrowSchema, arrays, 1), nil
}

func syntheticCTHeartbeatValue(col schema.Column, pkSet map[string]bool, targetVersion int64) any {
	switch {
	case strings.EqualFold(col.Name, destination.CDCLSNColumn):
		return formatCTVersion(targetVersion)
	case strings.EqualFold(col.Name, destination.CDCDeletedColumn):
		return true
	case strings.EqualFold(col.Name, destination.CDCSyncedAtColumn):
		return time.Now().UTC()
	case pkSet[strings.ToLower(col.Name)]:
		return syntheticCTPrimaryKeyValue(col)
	default:
		return nil
	}
}

func syntheticCTPrimaryKeyValue(col schema.Column) any {
	switch col.DataType {
	case schema.TypeBoolean:
		return false
	case schema.TypeInt8:
		return int8(math.MinInt8)
	case schema.TypeInt16:
		return int16(math.MinInt16)
	case schema.TypeInt32:
		return int32(math.MinInt32)
	case schema.TypeInt64:
		return int64(math.MinInt64)
	case schema.TypeFloat32, schema.TypeFloat64:
		return float64(math.SmallestNonzeroFloat64)
	case schema.TypeDecimal:
		return "0"
	case schema.TypeBinary:
		return []byte("__ingestr_ct_heartbeat__")
	case schema.TypeDate, schema.TypeTime, schema.TypeTimestamp, schema.TypeTimestampTZ:
		return time.Unix(0, 0).UTC()
	case schema.TypeUUID:
		return "00000000-0000-0000-0000-000000000000"
	default:
		return "__ingestr_ct_heartbeat__"
	}
}

func validateCTExcludeColumns(excludeColumns []string, primaryKeys []string) error {
	pkSet := make(map[string]bool, len(primaryKeys))
	for _, pk := range primaryKeys {
		pkSet[strings.ToLower(pk)] = true
	}
	for _, col := range excludeColumns {
		if destination.IsCDCMetaColumn(col) {
			return fmt.Errorf("SQL Server Change Tracking sources require metadata column %q; remove it from --sql-exclude-columns", col)
		}
		if pkSet[strings.ToLower(col)] {
			return fmt.Errorf("SQL Server Change Tracking sources require primary key column %q; remove it from --sql-exclude-columns", col)
		}
	}
	return nil
}

func (s *MSSQLChangeTrackingSource) rowsToCTBatches(ctx context.Context, rows *sql.Rows, columns []schema.Column, opts source.ReadOptions, results chan<- source.RecordBatchResult) (int64, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}
	arrowSchema := buildArrowSchema(columns)
	var totalRows int64

	for {
		record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize, opts.MaxBatchBytes, s.guidConversion)
		if err != nil {
			return totalRows, err
		}
		if count == 0 {
			return totalRows, nil
		}
		totalRows += count
		if err := emitCTResult(ctx, results, source.RecordBatchResult{Batch: record}); err != nil {
			return totalRows, err
		}
	}
}

func buildCTSnapshotQuery(table string, columns []schema.Column, version int64, lock bool) string {
	selects := make([]string, 0, len(columns)+len(ctMetadataColumns))
	for _, col := range columns {
		selects = append(selects, quoteColumn(col.Name))
	}
	selects = append(selects, ctMetadataSelects(ctVersionExpr(strconv.FormatInt(version, 10)), "0")...)

	hint := ""
	if lock {
		hint = " WITH (HOLDLOCK)"
	}
	return fmt.Sprintf("SELECT %s FROM %s%s", strings.Join(selects, ", "), quoteTable(table), hint)
}

func buildCTHeartbeatQuery(table string, columns []schema.Column, primaryKeys []string, version int64) string {
	selects := make([]string, 0, len(columns)+len(ctMetadataColumns))
	for _, col := range columns {
		selects = append(selects, quoteColumn(col.Name))
	}
	selects = append(selects, ctMetadataSelects(ctVersionExpr(strconv.FormatInt(version, 10)), "0")...)

	orderBy := make([]string, 0, len(primaryKeys))
	for _, pk := range primaryKeys {
		orderBy = append(orderBy, quoteColumn(pk))
	}

	query := fmt.Sprintf("SELECT TOP 1 %s FROM %s", strings.Join(selects, ", "), quoteTable(table))
	if len(orderBy) > 0 {
		query += " ORDER BY " + strings.Join(orderBy, ", ")
	}
	return query
}

func buildCTChangesQuery(table string, columns []schema.Column, primaryKeys []string) string {
	pkSet := make(map[string]bool, len(primaryKeys))
	for _, pk := range primaryKeys {
		pkSet[strings.ToLower(pk)] = true
	}

	selects := make([]string, 0, len(columns)+len(ctMetadataColumns))
	for _, col := range columns {
		if pkSet[strings.ToLower(col.Name)] {
			selects = append(selects, fmt.Sprintf("CT.%s AS %s", quoteColumn(col.Name), quoteColumn(col.Name)))
		} else {
			selects = append(selects, fmt.Sprintf("T.%s AS %s", quoteColumn(col.Name), quoteColumn(col.Name)))
		}
	}
	selects = append(selects, ctMetadataSelects(ctVersionExpr("CT.SYS_CHANGE_VERSION"), "CASE WHEN CT.SYS_CHANGE_OPERATION = 'D' THEN 1 ELSE 0 END")...)

	joinConditions := make([]string, len(primaryKeys))
	orderBy := []string{"CT.SYS_CHANGE_VERSION"}
	for i, pk := range primaryKeys {
		quoted := quoteColumn(pk)
		joinConditions[i] = fmt.Sprintf("T.%s = CT.%s", quoted, quoted)
		orderBy = append(orderBy, fmt.Sprintf("CT.%s", quoted))
	}

	return fmt.Sprintf(`
		SELECT %s
		FROM CHANGETABLE(CHANGES %s, @p1) AS CT
		LEFT JOIN %s AS T ON %s
		WHERE CT.SYS_CHANGE_VERSION <= @p2
		ORDER BY %s
	`, strings.Join(selects, ", "), quoteTable(table), quoteTable(table), strings.Join(joinConditions, " AND "), strings.Join(orderBy, ", "))
}

func ctMetadataSelects(versionExpr, deletedExpr string) []string {
	return []string{
		fmt.Sprintf("%s AS %s", versionExpr, quoteColumn(destination.CDCLSNColumn)),
		fmt.Sprintf("CAST(%s AS bit) AS %s", deletedExpr, quoteColumn(destination.CDCDeletedColumn)),
		fmt.Sprintf("SYSUTCDATETIME() AS %s", quoteColumn(destination.CDCSyncedAtColumn)),
	}
}

func ctVersionExpr(expr string) string {
	return fmt.Sprintf("RIGHT(REPLICATE('0', %d) + CONVERT(varchar(%d), %s), %d)", ctVersionWidth, ctVersionWidth, expr, ctVersionWidth)
}

func formatCTVersion(version int64) string {
	return fmt.Sprintf("%0*d", ctVersionWidth, version)
}

func parseStoredCTVersion(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	raw, _, _ = strings.Cut(raw, ":")
	raw = strings.TrimLeft(raw, "0")
	if raw == "" {
		return 0, true
	}
	version, err := strconv.ParseInt(raw, 10, 64)
	return version, err == nil
}

func objectIDName(table string) string {
	tableRef := parseMSSQLTableRef(table)
	if len(tableRef.parts) == 1 {
		return quoteIdentifierPath([]string{"dbo", tableRef.tableName})
	}
	return quoteIdentifierPath(tableRef.parts)
}

var (
	_ source.StreamingSource = (*MSSQLChangeTrackingSource)(nil)
	_ source.StreamCommitter = (*MSSQLChangeTrackingSource)(nil)
	_ source.LagReporter     = (*MSSQLChangeTrackingSource)(nil)
)
