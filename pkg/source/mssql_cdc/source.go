package mssql_cdc

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/mssql"
	mssqldb "github.com/microsoft/go-mssqldb"
)

const (
	defaultPollInterval = 1 * time.Second

	// mssqlLagRefreshInterval throttles the sys.fn_cdc_map_lsn_to_time
	// round-trip taken while behind, so lag reporting cannot add latency to
	// each poll cycle of a catch-up.
	mssqlLagRefreshInterval = 5 * time.Second

	// maxTransientReadFailures bounds how many consecutive deadlock-victim
	// change reads are retried per table before the failure is treated as
	// permanent.
	maxTransientReadFailures = 20

	// captureHealthInterval throttles the best-effort sys.dm_cdc_errors
	// probe that surfaces capture-job failures during streaming. Without it
	// a dead capture job freezes the harvest watermark and the stream reads
	// as caught up while the source backlog grows.
	captureHealthInterval = 5 * time.Minute
)

func sleepPoll(ctx context.Context, interval time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(interval):
		return nil
	}
}

// emitResult sends a result without wedging the producer goroutine when the
// consumer has gone away: cancellation releases the batch and unblocks.
func emitResult(ctx context.Context, results chan<- source.RecordBatchResult, res source.RecordBatchResult) error {
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

var mssqlCDCColumns = []schema.Column{
	{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
	{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
	{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
}

type CDCConfig struct {
	DestSchema      string
	CaptureInstance string
	PollInterval    time.Duration
}

type MSSQLCDCSource struct {
	db             *sql.DB
	uri            string
	cdcConfig      CDCConfig
	lag            *mssqlLagState
	health         captureHealth
	guidConversion bool
}

// captureHealth throttles capture-job error probes and deduplicates the
// warnings they produce. Only errors recorded after the stream started are
// reported, each once.
type captureHealth struct {
	mu            sync.Mutex
	nextCheck     time.Time
	warnedThrough time.Time
}

func (h *captureHealth) reset(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextCheck = now.Add(captureHealthInterval)
	h.warnedThrough = now
}

func (h *captureHealth) shouldCheck(now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if now.Before(h.nextCheck) {
		return false
	}
	h.nextCheck = now.Add(captureHealthInterval)
	return true
}

func (h *captureHealth) claimWarn(entry time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !entry.After(h.warnedThrough) {
		return false
	}
	h.warnedThrough = entry
	return true
}

// checkCaptureHealth surfaces capture-job failures recorded in
// sys.dm_cdc_errors. Best-effort: a missing VIEW DATABASE STATE permission or
// an empty view stays silent.
func (s *MSSQLCDCSource) checkCaptureHealth(ctx context.Context) {
	if !s.health.shouldCheck(time.Now()) {
		return
	}
	var entryTime sql.NullTime
	var message sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT TOP (1) entry_time, error_message FROM sys.dm_cdc_errors ORDER BY entry_time DESC").Scan(&entryTime, &message)
	if err != nil || !entryTime.Valid {
		return
	}
	if !s.health.claimWarn(entryTime.Time) {
		return
	}
	output.Warnf("Warning: SQL Server CDC capture job error at %s: %s\n", entryTime.Time.Format(time.RFC3339), strings.TrimSpace(message.String))
}

// mssqlLagState holds the last observed capture watermark and how far behind the
// processed position is in wall-clock terms. SQL Server LSNs are binary(10)
// values whose difference is not a log distance, so lag is expressed in seconds
// via sys.fn_cdc_map_lsn_to_time. The mutex is only ever held to copy small
// fields, never across a query, so a metrics scrape cannot stall the poll loop.
type mssqlLagState struct {
	mu            sync.Mutex
	processed     string
	target        string
	secondsBehind float64
	hasSeconds    bool
	updatedAt     time.Time
	lastQueryAt   time.Time

	streaming atomic.Bool
}

func newMSSQLLagState() *mssqlLagState {
	return &mssqlLagState{}
}

// observePositions records the watermark without touching the seconds reading,
// for cycles that skip the LSN-to-time query.
func (l *mssqlLagState) observePositions(processed, target string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.processed = processed
	l.target = target
	l.updatedAt = time.Now()
}

func (l *mssqlLagState) observe(processed, target string, secondsBehind float64, hasSeconds bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.processed = processed
	l.target = target
	l.secondsBehind = secondsBehind
	l.hasSeconds = hasSeconds
	l.updatedAt = time.Now()
}

// shouldQuery reports whether enough time has passed to spend another
// LSN-to-time round-trip, and claims the slot if so.
func (l *mssqlLagState) shouldQuery(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastQueryAt) < mssqlLagRefreshInterval {
		return false
	}
	l.lastQueryAt = now
	return true
}

type tableMetadata struct {
	SourceSchema    string
	SourceName      string
	CaptureInstance string
	ChangeTable     string
	CurrentColumns  map[string]bool
}

type CDCTable struct {
	source      *MSSQLCDCSource
	tableName   string
	metadata    tableMetadata
	tableSchema *schema.TableSchema
	primaryKeys []string
	strategy    config.IncrementalStrategy
}

func NewMSSQLCDCSource() *MSSQLCDCSource {
	return &MSSQLCDCSource{lag: newMSSQLLagState()}
}

var _ source.LagReporter = (*MSSQLCDCSource)(nil)

// ReplicationLag reports how far the processed CDC position trails the capture
// watermark, in seconds.
func (s *MSSQLCDCSource) ReplicationLag() (source.LagSnapshot, bool) {
	if s.lag == nil || !s.lag.streaming.Load() {
		return source.LagSnapshot{}, false
	}

	s.lag.mu.Lock()
	processed, target := s.lag.processed, s.lag.target
	secs, hasSecs, updatedAt := s.lag.secondsBehind, s.lag.hasSeconds, s.lag.updatedAt
	s.lag.mu.Unlock()

	if processed == "" || target == "" {
		return source.LagSnapshot{}, false
	}

	snap := source.LagSnapshot{
		Source:          "mssql_cdc",
		ServerPosition:  target,
		DurablePosition: processed,
		CaughtUp:        compareLSNHex(processed, target) >= 0,
		UpdatedAt:       updatedAt,
	}
	if hasSecs {
		snap.SecondsBehind = &secs
	}
	return snap, true
}

// noteLag records the current watermark and asks the server how much change
// time separates the processed LSN from it. Lag is measured against the capture
// watermark rather than wall-clock now: on an idle database the two LSNs are
// equal and lag is zero, whereas "now minus the time of the last change" would
// climb forever with nothing actually behind.
//
// A mapping failure (an LSN the capture job has aged out, or one never
// committed) is not an error: lag simply goes unreported for this cycle.
func (s *MSSQLCDCSource) noteLag(ctx context.Context, processed, target string) {
	if !s.lag.streaming.Load() || processed == "" || target == "" {
		return
	}

	if compareLSNHex(processed, target) >= 0 {
		s.lag.observe(processed, target, 0, true)
		return
	}

	// Behind: mapping LSNs to times costs a round-trip to a server that is
	// already busy shipping us the backlog, so throttle it. The watermark
	// itself is free and stays current.
	if !s.lag.shouldQuery(time.Now()) {
		s.lag.observePositions(processed, target)
		return
	}

	var secs sql.NullFloat64
	err := s.db.QueryRowContext(
		ctx,
		`SELECT DATEDIFF(second,
			sys.fn_cdc_map_lsn_to_time(CONVERT(binary(10), @p1, 2)),
			sys.fn_cdc_map_lsn_to_time(CONVERT(binary(10), @p2, 2)))`,
		processed, target,
	).Scan(&secs)
	if err != nil {
		config.Debug("[MSSQL CDC] Failed to map LSNs %s..%s to time: %v", processed, target, err)
		s.lag.observe(processed, target, 0, false)
		return
	}

	value := secs.Float64
	if value < 0 {
		value = 0
	}
	s.lag.observe(processed, target, value, secs.Valid)
}

func (s *MSSQLCDCSource) Schemes() []string {
	return []string{"mssql+cdc", "sqlserver+cdc", "azuresql+cdc", "azure-sql+cdc"}
}

func (s *MSSQLCDCSource) Connect(ctx context.Context, uri string) error {
	cdcConfig, normalizedURI, err := parseURIConfig(uri)
	if err != nil {
		return fmt.Errorf("failed to parse SQL Server CDC config: %w", err)
	}

	connStr, driverName, err := mssql.URIToConnString(normalizedURI)
	if err != nil {
		return fmt.Errorf("failed to parse SQL Server URI: %w", err)
	}
	s.guidConversion = mssql.GUIDConversionEnabled(connStr)

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

	var cdcEnabled bool
	if err := db.QueryRowContext(ctx, "SELECT CAST(is_cdc_enabled AS bit) FROM sys.databases WHERE database_id = DB_ID()").Scan(&cdcEnabled); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to check SQL Server CDC status: %w", err)
	}
	if !cdcEnabled {
		_ = db.Close()
		return fmt.Errorf("SQL Server CDC is not enabled for the current database; run sys.sp_cdc_enable_db first")
	}

	// A NULL harvest watermark means the capture job has never processed the
	// log — the most common cause is SQL Server Agent not running, which
	// otherwise reads as an eternally idle, "caught up" stream.
	var watermark sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT CONVERT(varchar(20), sys.fn_cdc_get_max_lsn(), 2)").Scan(&watermark); err == nil && !watermark.Valid {
		output.Warnf("Warning: SQL Server CDC has not harvested any changes yet; ensure the CDC capture job is running (SQL Server Agent on self-hosted instances)\n")
	}

	s.db = db
	s.uri = uri
	s.cdcConfig = cdcConfig
	return nil
}

func (s *MSSQLCDCSource) Close(ctx context.Context) error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *MSSQLCDCSource) HandlesIncrementality() bool {
	return true
}

// SupportsStreaming reports that SQL Server CDC can run in continuous mode.
// It reads change tables by polling and has no consumer-side acknowledgement,
// so there is no StreamCommitter: at-least-once is provided by resuming from
// the destination's max _cdc_lsn.
func (s *MSSQLCDCSource) SupportsStreaming() bool {
	return true
}

// DefaultStreamingStrategy returns merge: CDC changes (including deletes) are
// applied by primary key.
func (s *MSSQLCDCSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *MSSQLCDCSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}

	meta, err := s.getTableMetadata(ctx, req.Name, s.cdcConfig.CaptureInstance)
	if err != nil {
		return nil, err
	}

	tableSchema, err := s.getCapturedSchema(ctx, meta)
	if err != nil {
		return nil, err
	}
	tableSchema = addCDCColumns(tableSchema)

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}
	if len(pks) == 0 {
		return nil, fmt.Errorf("SQL Server CDC table %s has no primary key; provide --primary-key or add a primary key to the source table", req.Name)
	}
	if err := validateCapturedPrimaryKeys(tableSchema, pks); err != nil {
		return nil, err
	}
	tableSchema.PrimaryKeys = pks

	// Replace resolves to merge outside full-refresh runs, matching the
	// pipeline's managed-change resolution. Anything else cannot represent
	// CDC deletes and boundary re-deliveries correctly.
	strategy := config.StrategyMerge
	switch req.Strategy {
	case "", config.StrategyMerge, config.StrategyReplace:
	default:
		return nil, fmt.Errorf("incremental strategy %q is not supported for SQL Server CDC; use merge or replace", req.Strategy)
	}

	return &CDCTable{
		source:      s,
		tableName:   tableName(meta),
		metadata:    meta,
		tableSchema: tableSchema,
		primaryKeys: pks,
		strategy:    strategy,
	}, nil
}

func (s *MSSQLCDCSource) IsMultiTable() bool {
	return true
}

func (s *MSSQLCDCSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	if s.cdcConfig.CaptureInstance != "" {
		output.Warnf("Warning: capture_instance is ignored in multi-table SQL Server CDC mode; the newest capture instance of each table is used\n")
	}
	tables, skipped, err := s.getTables(ctx)
	if err != nil {
		return nil, err
	}
	for _, st := range skipped {
		output.Warnf("Warning: skipping SQL Server CDC table %s: %s\n", st.name, st.reason)
	}
	return tables, nil
}

type skippedTable struct {
	name   string
	reason string
}

// selectTables applies the requested-table filter. A filter entry that
// matches nothing is a hard error: silently ingesting nothing (and, in
// streaming mode, polling forever) would hide a typo or an unqualified
// table name.
func selectTables(allTables []source.SourceTableInfo, skipped []skippedTable, requested []string) ([]source.SourceTableInfo, error) {
	filter := map[string]string{}
	for _, table := range requested {
		table = strings.TrimSpace(table)
		if table == "" {
			continue
		}
		filter[strings.ToLower(table)] = table
	}
	filtered := len(filter) > 0

	selected := make([]source.SourceTableInfo, 0, len(allTables))
	for _, table := range allTables {
		key := strings.ToLower(table.Name)
		if filtered && filter[key] == "" {
			continue
		}
		delete(filter, key)
		selected = append(selected, table)
	}

	for _, st := range skipped {
		if requestedName, ok := filter[strings.ToLower(st.name)]; ok {
			return nil, fmt.Errorf("SQL Server CDC table %s cannot be ingested: %s", requestedName, st.reason)
		}
	}
	if len(filter) > 0 {
		missing := make([]string, 0, len(filter))
		for _, requestedName := range filter {
			missing = append(missing, requestedName)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("tables not found among SQL Server CDC-enabled tables: %s; use schema-qualified names (e.g. dbo.users)", strings.Join(missing, ", "))
	}
	return selected, nil
}

// getTables lists ingestible CDC-enabled tables. Tables that cannot join a
// multi-table run (no primary key, so merge has nothing to apply changes by)
// are returned separately instead of failing the run: one such table anywhere
// in the database must not block ingesting the others.
func (s *MSSQLCDCSource) getTables(ctx context.Context) ([]source.SourceTableInfo, []skippedTable, error) {
	metas, err := s.getAllTableMetadata(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(metas) == 0 {
		return nil, nil, fmt.Errorf("no SQL Server CDC-enabled tables found")
	}

	tables := make([]source.SourceTableInfo, 0, len(metas))
	var skipped []skippedTable
	for _, meta := range metas {
		tableSchema, err := s.getCapturedSchema(ctx, meta)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get schema for %s: %w", tableName(meta), err)
		}
		tableSchema = addCDCColumns(tableSchema)
		if len(tableSchema.PrimaryKeys) == 0 {
			skipped = append(skipped, skippedTable{
				name:   tableName(meta),
				reason: "it has no primary key; add one, or ingest it alone with --source-table and --primary-key",
			})
			continue
		}

		tables = append(tables, source.SourceTableInfo{
			Name:        tableName(meta),
			Schema:      tableSchema,
			PrimaryKeys: tableSchema.PrimaryKeys,
			DestSchema:  s.cdcConfig.DestSchema,
		})
	}

	if len(tables) == 0 {
		return nil, skipped, fmt.Errorf("no SQL Server CDC-enabled tables with a primary key found")
	}
	return tables, skipped, nil
}

func (s *MSSQLCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	allTables, skipped, err := s.getTables(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := selectTables(allTables, skipped, opts.Tables)
	if err != nil {
		return nil, err
	}

	results := make(chan source.RecordBatchResult, 16)
	go func() {
		defer close(results)

		startByTable := make(map[string]string, len(selected))
		metaByTable := make(map[string]tableMetadata, len(selected))
		reReadByTable := make(map[string]bool, len(selected))

		for _, table := range selected {
			meta, err := s.getTableMetadata(ctx, table.Name, "")
			if err != nil {
				_ = emitResult(ctx, results, source.RecordBatchResult{Err: err})
				return
			}
			metaByTable[table.Name] = meta

			resume := opts.CDCResumeLSNs[table.Name]
			if isIncompleteSnapshotStamp(resume) {
				config.Debug("[MSSQL CDC] Stored position %s marks an unfinished snapshot of %s; taking a fresh snapshot", strings.TrimSpace(resume), table.Name)
				resume = ""
			}
			startHex := startLSNFromStored(resume)
			if startHex != "" {
				canResume, err := s.canResume(ctx, meta, startHex)
				if err != nil {
					_ = emitResult(ctx, results, source.RecordBatchResult{Err: err})
					return
				}
				if canResume {
					startByTable[table.Name] = startHex
					reReadByTable[table.Name] = !isSnapshotStamp(resume)
					continue
				}
			}

			snapshotLSN, err := s.snapshotTable(ctx, meta, table.Schema, opts.ReadOptions, results, table.Name)
			if err != nil {
				_ = emitResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("snapshot failed for %s: %w", table.Name, err)})
				return
			}
			startByTable[table.Name] = snapshotLSN
		}

		if err := s.streamTables(ctx, selected, metaByTable, startByTable, reReadByTable, opts.ReadOptions, results); err != nil {
			_ = emitResult(ctx, results, source.RecordBatchResult{Err: err})
		}
	}()

	return results, nil
}

func parseURIConfig(rawURI string) (CDCConfig, string, error) {
	cfg := CDCConfig{
		PollInterval: defaultPollInterval,
	}

	parsed, err := url.Parse(rawURI)
	if err != nil {
		return cfg, "", err
	}

	switch strings.ToLower(parsed.Scheme) {
	case "mssql+cdc":
		parsed.Scheme = "mssql"
	case "sqlserver+cdc":
		parsed.Scheme = "sqlserver"
	case "azuresql+cdc":
		parsed.Scheme = "azuresql"
	case "azure-sql+cdc":
		parsed.Scheme = "azure-sql"
	default:
		return cfg, "", fmt.Errorf("unsupported SQL Server CDC scheme: %s", parsed.Scheme)
	}

	query := parsed.Query()
	cfg.CaptureInstance = query.Get("capture_instance")
	cfg.DestSchema = query.Get("dest_schema")

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

	query.Del("capture_instance")
	query.Del("dest_schema")
	query.Del("mode")
	query.Del("poll_interval")
	parsed.RawQuery = query.Encode()

	return cfg, parsed.String(), nil
}

func (s *MSSQLCDCSource) getTableMetadata(ctx context.Context, table string, captureInstance string) (tableMetadata, error) {
	schemaName, tableNameOnly := parseTableName(table)

	query := `
		SELECT TOP (1)
			ss.name,
			st.name,
			ct.capture_instance,
			OBJECT_SCHEMA_NAME(ct.object_id) + N'.' + OBJECT_NAME(ct.object_id)
		FROM cdc.change_tables AS ct
		JOIN sys.tables AS st ON st.object_id = ct.source_object_id
		JOIN sys.schemas AS ss ON ss.schema_id = st.schema_id
		WHERE ss.name = @p1
		  AND st.name = @p2
		  AND ct.end_lsn IS NULL
	`
	args := []any{schemaName, tableNameOnly}
	if captureInstance != "" {
		query += " AND ct.capture_instance = @p3"
		args = append(args, captureInstance)
	}
	query += " ORDER BY ct.start_lsn DESC"

	var meta tableMetadata
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&meta.SourceSchema, &meta.SourceName, &meta.CaptureInstance, &meta.ChangeTable)
	if err != nil {
		if err == sql.ErrNoRows {
			return meta, fmt.Errorf("table %s is not enabled for SQL Server CDC", table)
		}
		return meta, fmt.Errorf("failed to query CDC metadata for %s: %w", table, err)
	}

	currentColumns, err := s.currentSourceColumns(ctx, meta)
	if err != nil {
		return meta, err
	}
	meta.CurrentColumns = currentColumns
	return meta, nil
}

func (s *MSSQLCDCSource) getAllTableMetadata(ctx context.Context) ([]tableMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ss.name,
			st.name,
			ct.capture_instance,
			OBJECT_SCHEMA_NAME(ct.object_id) + N'.' + OBJECT_NAME(ct.object_id)
		FROM cdc.change_tables AS ct
		JOIN sys.tables AS st ON st.object_id = ct.source_object_id
		JOIN sys.schemas AS ss ON ss.schema_id = st.schema_id
		WHERE ct.end_lsn IS NULL
		ORDER BY ss.name, st.name, ct.start_lsn DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query CDC tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	seen := map[string]bool{}
	var metas []tableMetadata
	for rows.Next() {
		var meta tableMetadata
		if err := rows.Scan(&meta.SourceSchema, &meta.SourceName, &meta.CaptureInstance, &meta.ChangeTable); err != nil {
			return nil, fmt.Errorf("failed to scan CDC table metadata: %w", err)
		}
		key := strings.ToLower(tableName(meta))
		if seen[key] {
			continue
		}
		seen[key] = true

		currentColumns, err := s.currentSourceColumns(ctx, meta)
		if err != nil {
			return nil, err
		}
		meta.CurrentColumns = currentColumns
		metas = append(metas, meta)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return metas, nil
}

func (s *MSSQLCDCSource) currentSourceColumns(ctx context.Context, meta tableMetadata) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.name
		FROM sys.columns AS c
		WHERE c.object_id = OBJECT_ID(@p1)
	`, tableName(meta))
	if err != nil {
		return nil, fmt.Errorf("failed to query source columns for %s: %w", tableName(meta), err)
	}
	defer func() { _ = rows.Close() }()

	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[strings.ToLower(name)] = true
	}
	return columns, rows.Err()
}

func (s *MSSQLCDCSource) getCapturedSchema(ctx context.Context, meta tableMetadata) (*schema.TableSchema, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.name,
			t.name,
			CAST(c.is_nullable AS bit),
			CONVERT(int, c.precision),
			CONVERT(int, c.scale),
			CONVERT(int,
				CASE
					WHEN t.name IN ('nchar', 'nvarchar') AND c.max_length > 0 THEN c.max_length / 2
					ELSE c.max_length
				END
			)
		FROM sys.columns AS c
		JOIN sys.types AS t ON c.user_type_id = t.user_type_id
		WHERE c.object_id = OBJECT_ID(@p1)
		  AND c.name NOT LIKE '__$%'
		ORDER BY c.column_id
	`, meta.ChangeTable)
	if err != nil {
		return nil, fmt.Errorf("failed to query captured schema for %s: %w", tableName(meta), err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType string
		var nullable bool
		var precision, scale, maxLength int

		if err := rows.Scan(&columnName, &dataType, &nullable, &precision, &scale, &maxLength); err != nil {
			return nil, fmt.Errorf("failed to scan captured column: %w", err)
		}

		dt, fallbackPrecision, fallbackScale, arrayType := mssql.MapMSSQLToDataType(dataType)
		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  nullable,
			ArrayType: arrayType,
		}
		if precision > 0 {
			col.Precision = precision
		} else if fallbackPrecision > 0 {
			col.Precision = fallbackPrecision
		}
		if scale > 0 {
			col.Scale = scale
		} else if fallbackScale > 0 {
			col.Scale = fallbackScale
		}
		if maxLength > 0 {
			col.MaxLength = maxLength
		}
		columns = append(columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("CDC capture instance %s has no captured columns", meta.CaptureInstance)
	}

	// Captured columns the source table no longer has are snapshotted as
	// NULL, so they must read as nullable regardless of the change table's
	// original constraint.
	if len(meta.CurrentColumns) > 0 {
		for i := range columns {
			if !meta.CurrentColumns[strings.ToLower(columns[i].Name)] {
				columns[i].Nullable = true
			}
		}
	}

	primaryKeys, err := s.primaryKeys(ctx, meta)
	if err != nil {
		return nil, err
	}
	for i := range columns {
		for _, pk := range primaryKeys {
			if strings.EqualFold(columns[i].Name, pk) {
				columns[i].IsPrimaryKey = true
				columns[i].Nullable = false
				break
			}
		}
	}

	tableSchema := &schema.TableSchema{
		Name:        meta.SourceName,
		Schema:      meta.SourceSchema,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}
	if err := validateCapturedPrimaryKeys(tableSchema, primaryKeys); err != nil {
		return nil, err
	}
	return tableSchema, nil
}

// validateCapturedPrimaryKeys ensures every primary-key column is captured by
// the CDC capture instance. Merge applies changes by primary key, so a key
// column missing from the change table would silently corrupt replication.
func validateCapturedPrimaryKeys(tableSchema *schema.TableSchema, pks []string) error {
	captured := make(map[string]bool, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		captured[strings.ToLower(col.Name)] = true
	}
	for _, pk := range pks {
		if !captured[strings.ToLower(pk)] {
			return fmt.Errorf("primary key column %q of %s.%s is not captured by the SQL Server CDC capture instance; re-enable CDC capturing all key columns", pk, tableSchema.Schema, tableSchema.Name)
		}
	}
	return nil
}

func (s *MSSQLCDCSource) primaryKeys(ctx context.Context, meta tableMetadata) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.name
		FROM sys.key_constraints AS kc
		JOIN sys.index_columns AS ic
			ON ic.object_id = kc.parent_object_id
			AND ic.index_id = kc.unique_index_id
		JOIN sys.columns AS c
			ON c.object_id = ic.object_id
			AND c.column_id = ic.column_id
		WHERE kc.parent_object_id = OBJECT_ID(@p1)
		  AND kc.type = 'PK'
		ORDER BY ic.key_ordinal
	`, tableName(meta))
	if err != nil {
		return nil, fmt.Errorf("failed to query primary keys for %s: %w", tableName(meta), err)
	}
	defer func() { _ = rows.Close() }()

	var pks []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		pks = append(pks, name)
	}
	return pks, rows.Err()
}

func (t *CDCTable) Name() string {
	return t.tableName
}

func (t *CDCTable) PrimaryKeys() []string {
	return t.primaryKeys
}

func (t *CDCTable) IncrementalKey() string {
	return ""
}

func (t *CDCTable) Strategy() config.IncrementalStrategy {
	return t.strategy
}

func (t *CDCTable) HasKnownSchema() bool {
	return true
}

func (t *CDCTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.tableSchema, nil
}

func (t *CDCTable) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		resume := opts.CDCResumeLSN
		if isIncompleteSnapshotStamp(resume) {
			config.Debug("[MSSQL CDC] Stored position %s marks an unfinished snapshot of %s; taking a fresh snapshot", strings.TrimSpace(resume), t.tableName)
			resume = ""
		}
		startHex := startLSNFromStored(resume)
		if startHex != "" {
			canResume, err := t.source.canResume(ctx, t.metadata, startHex)
			if err != nil {
				_ = emitResult(ctx, results, source.RecordBatchResult{Err: err})
				return
			}
			if canResume {
				if err := t.source.streamTable(ctx, t.metadata, t.tableSchema, startHex, !isSnapshotStamp(resume), opts, results, ""); err != nil {
					_ = emitResult(ctx, results, source.RecordBatchResult{Err: err})
				}
				return
			}
			config.Debug("[MSSQL CDC] Resume LSN %s is no longer valid for %s; taking a fresh snapshot", startHex, t.tableName)
		}

		snapshotLSN, err := t.source.snapshotTable(ctx, t.metadata, t.tableSchema, opts, results, "")
		if err != nil {
			_ = emitResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)})
			return
		}

		if err := t.source.streamTable(ctx, t.metadata, t.tableSchema, snapshotLSN, false, opts, results, ""); err != nil {
			_ = emitResult(ctx, results, source.RecordBatchResult{Err: err})
		}
	}()

	return results, nil
}

func (s *MSSQLCDCSource) canResume(ctx context.Context, meta tableMetadata, startHex string) (bool, error) {
	minLSN, err := s.minLSN(ctx, meta)
	if err != nil {
		return false, err
	}
	if minLSN == "" || isZeroLSN(minLSN) {
		return false, nil
	}
	return compareLSNHex(startHex, minLSN) >= 0, nil
}

func (s *MSSQLCDCSource) snapshotTable(ctx context.Context, meta tableMetadata, tableSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string) (string, error) {
	// Probe instead of try-and-fall-back: a failed SNAPSHOT attempt leaves
	// the session isolation level set on the pooled connection (SQL Server
	// scopes SET TRANSACTION ISOLATION LEVEL to the session, not the
	// transaction), poisoning later queries with error 3952.
	useSnapshot, err := s.snapshotIsolationAllowed(ctx)
	if err != nil {
		return "", err
	}
	if useSnapshot {
		snapshotLSN, err := s.snapshotTableWithIsolation(ctx, meta, tableSchema, opts, results, resultTable, sql.LevelSnapshot)
		if err == nil {
			return snapshotLSN, nil
		}
		// Only fall back when the database rejected SNAPSHOT isolation after
		// all (the setting can flip between probe and scan). Retrying every
		// failure would rescan the whole table under HOLDLOCK, holding shared
		// locks on a live table for no benefit.
		if !isSnapshotIsolationUnavailableError(err) {
			return "", err
		}
		config.Debug("[MSSQL CDC] SNAPSHOT isolation became unavailable (%v); retrying snapshot of %s with a table lock", err, tableName(meta))
	} else {
		config.Debug("[MSSQL CDC] Snapshot isolation is not allowed for this database; snapshotting %s with a table lock", tableName(meta))
	}
	return s.snapshotTableWithIsolation(ctx, meta, tableSchema, opts, results, resultTable, sql.LevelSerializable)
}

// snapshotIsolationAllowed reports whether the database permits SNAPSHOT
// isolation (sys.databases.snapshot_isolation_state = 1).
func (s *MSSQLCDCSource) snapshotIsolationAllowed(ctx context.Context) (bool, error) {
	var state int
	if err := s.db.QueryRowContext(ctx, "SELECT CONVERT(int, snapshot_isolation_state) FROM sys.databases WHERE database_id = DB_ID()").Scan(&state); err != nil {
		return false, fmt.Errorf("failed to check snapshot isolation state: %w", err)
	}
	return state == 1, nil
}

// isSnapshotIsolationUnavailableError matches error 3952: the database does
// not allow SNAPSHOT isolation (ALLOW_SNAPSHOT_ISOLATION is OFF).
func isSnapshotIsolationUnavailableError(err error) bool {
	var sqlErr mssqldb.Error
	return errors.As(err, &sqlErr) && sqlErr.Number == 3952
}

func (s *MSSQLCDCSource) snapshotTableWithIsolation(ctx context.Context, meta tableMetadata, tableSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string, isolation sql.IsolationLevel) (string, error) {
	// Read the harvest watermark before opening the snapshot transaction.
	// fn_cdc_get_max_lsn reports current capture progress even inside a
	// snapshot-isolation transaction, so reading it after BeginTx could place
	// it ahead of the scan's consistent point and lose changes committed in
	// between. A watermark read before BeginTx only risks re-delivering
	// changes the scan already includes, which the merge absorbs.
	snapshotLSN, err := s.maxLSN(ctx)
	if err != nil {
		return "", err
	}
	// The harvest watermark (fn_cdc_get_max_lsn) can lag behind the capture
	// instance's min LSN, e.g. for a freshly enabled table. Stamping the
	// snapshot below min LSN would invalidate the next run's resume check and
	// force an endless re-snapshot loop, so clamp up to the instance min LSN.
	// Both bounds precede any post-snapshot change, so this never skips data.
	minLSN, err := s.minLSN(ctx, meta)
	if err != nil {
		return "", err
	}
	if minLSN != "" && !isZeroLSN(minLSN) {
		if snapshotLSN == "" || isZeroLSN(snapshotLSN) || compareLSNHex(minLSN, snapshotLSN) > 0 {
			snapshotLSN = minLSN
		}
	}
	if snapshotLSN == "" {
		snapshotLSN = "00000000000000000000"
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer func() {
		// BeginTx's isolation level sticks to the session, so restore the
		// default before the connection re-enters the pool.
		_, _ = conn.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL READ COMMITTED")
		_ = conn.Close()
	}()

	// go-mssqldb rejects ReadOnly transactions outright ("read-only
	// transactions are not supported"), so requesting one here used to fail
	// every SNAPSHOT-isolation attempt and silently push all snapshots onto
	// the SERIALIZABLE+HOLDLOCK fallback, which blocks writers.
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
	if err != nil {
		return "", fmt.Errorf("failed to begin snapshot transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	lock := isolation != sql.LevelSnapshot
	query := buildSnapshotQuery(meta, sourceColumnsWithoutCDC(tableSchema), lock)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to query snapshot for %s: %w", tableName(meta), err)
	}
	defer func() { _ = rows.Close() }()

	if opts.CDCSnapshotReplace {
		// A snapshot is a complete replacement boundary. Consumers that opt in
		// discard target rows left by an interrupted earlier attempt or a lost
		// resume position, so source deletes missed while the position was
		// invalid cannot linger.
		if err := emitResult(ctx, results, source.RecordBatchResult{Truncate: true, TableName: resultTable}); err != nil {
			return "", err
		}
	}

	if err := s.rowsToSnapshotBatches(ctx, rows, tableSchema, opts, snapshotLSN, results, resultTable); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit snapshot transaction: %w", err)
	}

	return snapshotLSN, nil
}

// isInvalidLSNRangeError matches error 313, which fn_cdc_get_all_changes
// raises with a famously misleading message ("an insufficient number of
// arguments were supplied") when the requested window falls outside the
// capture instance's valid LSN range — in practice, when cleanup advanced the
// low watermark past the cursor mid-stream.
func isInvalidLSNRangeError(err error) bool {
	var sqlErr mssqldb.Error
	return errors.As(err, &sqlErr) && sqlErr.Number == 313
}

// isTransientMSSQLError reports errors SQL Server tells the client to retry:
// a deadlock victim (1205), most commonly the watermark or change-table reads
// losing a lock race against the CDC capture job under write-heavy load.
func isTransientMSSQLError(err error) bool {
	var sqlErr mssqldb.Error
	return errors.As(err, &sqlErr) && sqlErr.Number == 1205
}

func (s *MSSQLCDCSource) maxLSN(ctx context.Context) (string, error) {
	var lsn sql.NullString
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		err = s.db.QueryRowContext(ctx, "SELECT CONVERT(varchar(20), sys.fn_cdc_get_max_lsn(), 2)").Scan(&lsn)
		if err == nil {
			if !lsn.Valid {
				return "", nil
			}
			return normalizeLSNHex(lsn.String), nil
		}
		if !isTransientMSSQLError(err) {
			break
		}
		config.Debug("[MSSQL CDC] Retrying max LSN read after transient error: %v", err)
	}
	return "", fmt.Errorf("failed to get SQL Server CDC max LSN: %w", err)
}

// minLSN returns the capture instance's low-watermark LSN. It reads
// cdc.change_tables.start_lsn directly instead of sys.fn_cdc_get_min_lsn
// because the latter returns NULL until the capture job first processes the
// instance, while start_lsn is set at enable time. Cleanup raises start_lsn,
// so it carries the same change-retention semantics.
func (s *MSSQLCDCSource) minLSN(ctx context.Context, meta tableMetadata) (string, error) {
	var lsn sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT CONVERT(varchar(20), start_lsn, 2) FROM cdc.change_tables WHERE capture_instance = @p1 AND end_lsn IS NULL", meta.CaptureInstance).Scan(&lsn)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get SQL Server CDC min LSN for %s: %w", tableName(meta), err)
	}
	if !lsn.Valid {
		return "", nil
	}
	return normalizeLSNHex(lsn.String), nil
}

// lowestLSN returns the least-advanced position across tables, ignoring tables
// that have no position yet. Empty when no table has one.
func lowestLSN(byTable map[string]string) string {
	lowest := ""
	for _, lsn := range byTable {
		if lsn == "" {
			continue
		}
		if lowest == "" || compareLSNHex(lsn, lowest) < 0 {
			lowest = lsn
		}
	}
	return lowest
}

func (s *MSSQLCDCSource) streamTables(ctx context.Context, tables []source.SourceTableInfo, metas map[string]tableMetadata, startByTable map[string]string, reReadByTable map[string]bool, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	s.lag.streaming.Store(opts.Streaming)
	if opts.Streaming {
		s.health.reset(time.Now())
	}
	// Each table's initial position (a resume cursor or a snapshot stamp) may
	// sit mid-transaction, so the first read includes it; see streamTable.
	inclusiveByTable := make(map[string]bool, len(tables))
	for _, table := range tables {
		inclusiveByTable[table.Name] = true
	}
	transientFailures := make(map[string]int, len(tables))
	for {
		if opts.Streaming {
			s.checkCaptureHealth(ctx)
		}
		targetLSN, err := s.maxLSN(ctx)
		if err != nil {
			return err
		}
		if targetLSN == "" || isZeroLSN(targetLSN) {
			if !opts.Streaming {
				return nil
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.cdcConfig.PollInterval):
				continue
			}
		}

		// The laggiest table defines the backlog: sample before processing,
		// while the gap to the watermark is still open.
		s.noteLag(ctx, lowestLSN(startByTable), targetLSN)

		retryCycle := false
		for _, table := range tables {
			start, readWindow := planChangeWindow(startByTable[table.Name], targetLSN, reReadByTable[table.Name])
			startByTable[table.Name] = start
			if !readWindow {
				continue
			}

			if err := s.readChanges(ctx, metas[table.Name], table.Schema, start, targetLSN, inclusiveByTable[table.Name], opts, results, table.Name); err != nil {
				// A deadlock victim under write-heavy load must not kill the
				// stream: the cursor was not advanced, so re-reading the same
				// window next cycle only re-delivers rows merge absorbs.
				if isTransientMSSQLError(err) && transientFailures[table.Name] < maxTransientReadFailures {
					transientFailures[table.Name]++
					config.Debug("[MSSQL CDC] Retrying change read for %s after transient error (%d consecutive): %v", table.Name, transientFailures[table.Name], err)
					retryCycle = true
					break
				}
				return err
			}
			transientFailures[table.Name] = 0
			inclusiveByTable[table.Name] = false
			reReadByTable[table.Name] = false
			startByTable[table.Name] = targetLSN
		}
		if retryCycle {
			if err := sleepPoll(ctx, s.cdcConfig.PollInterval); err != nil {
				return err
			}
			continue
		}

		if !opts.Streaming {
			return nil
		}

		s.noteLag(ctx, targetLSN, targetLSN)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.cdcConfig.PollInterval):
		}
	}
}

// planChangeWindow keeps each table's cursor monotonic while deciding whether
// the current harvest watermark opens a readable window. A newly created
// capture instance can report a minimum LSN just ahead of the global maximum;
// that table must wait for the capture job rather than regress below its valid
// range. An equal resume boundary is read once when it names a change row so a
// transaction tail cannot be skipped.
func planChangeWindow(start, target string, reReadBoundary bool) (string, bool) {
	if start == "" {
		return target, false
	}
	cmp := compareLSNHex(start, target)
	if cmp > 0 || (cmp == 0 && !reReadBoundary) {
		return start, false
	}
	return start, true
}

func (s *MSSQLCDCSource) streamTable(ctx context.Context, meta tableMetadata, tableSchema *schema.TableSchema, startLSN string, reReadBoundary bool, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string) error {
	s.lag.streaming.Store(opts.Streaming)
	if opts.Streaming {
		s.health.reset(time.Now())
	}
	current := startLSN
	// The initial position may sit mid-transaction: a resume cursor names the
	// destination's last durable change row, whose transaction can continue
	// past it, and a snapshot stamp must re-deliver changes at the boundary
	// LSN. The first read therefore includes the position; merge is idempotent
	// for the rows this re-delivers. Once the stream has advanced to a harvest
	// watermark, everything at that LSN has been delivered, so later polls
	// start strictly after it.
	inclusive := true
	transientFailures := 0
	for {
		if opts.Streaming {
			s.checkCaptureHealth(ctx)
		}
		targetLSN, err := s.maxLSN(ctx)
		if err != nil {
			return err
		}
		s.noteLag(ctx, current, targetLSN)
		cmp := compareLSNHex(current, targetLSN)
		// A resume cursor that caught up to the watermark still earns one
		// re-read when it names a delivered change row (reReadBoundary): the
		// cursor may sit mid-transaction, and only re-reading the boundary
		// transaction restores its tail.
		if targetLSN != "" && current != "" && (cmp < 0 || (reReadBoundary && cmp == 0)) {
			if err := s.readChanges(ctx, meta, tableSchema, current, targetLSN, inclusive, opts, results, resultTable); err != nil {
				// See streamTables: a deadlock victim retries the same window
				// next cycle; the cursor was not advanced so nothing is lost.
				if isTransientMSSQLError(err) && transientFailures < maxTransientReadFailures {
					transientFailures++
					config.Debug("[MSSQL CDC] Retrying change read for %s after transient error (%d consecutive): %v", tableName(meta), transientFailures, err)
					if err := sleepPoll(ctx, s.cdcConfig.PollInterval); err != nil {
						return err
					}
					continue
				}
				return err
			}
			transientFailures = 0
			inclusive = false
			reReadBoundary = false
			current = targetLSN
		}

		if !opts.Streaming {
			return nil
		}

		s.noteLag(ctx, current, targetLSN)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.cdcConfig.PollInterval):
		}
	}
}

func (s *MSSQLCDCSource) readChanges(ctx context.Context, meta tableMetadata, tableSchema *schema.TableSchema, fromLSN string, toLSN string, inclusive bool, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string) error {
	cmp := compareLSNHex(fromLSN, toLSN)
	if fromLSN == "" || toLSN == "" || cmp > 0 || (cmp == 0 && !inclusive) {
		return nil
	}

	sourceColumns := sourceColumnsWithoutCDC(tableSchema)
	query := buildChangesQuery(meta, sourceColumns, inclusive)
	rows, err := s.db.QueryContext(ctx, query, fromLSN, toLSN)
	if err != nil {
		if isInvalidLSNRangeError(err) {
			return fmt.Errorf("CDC changes for %s from LSN %s are no longer available: change-table cleanup advanced past the cursor (retention exceeded); the next run will take a fresh snapshot: %w", tableName(meta), fromLSN, err)
		}
		return fmt.Errorf("failed to query CDC changes for %s: %w", tableName(meta), err)
	}
	defer func() { _ = rows.Close() }()

	return s.rowsToChangeBatches(ctx, rows, tableSchema, opts, results, resultTable)
}

func (s *MSSQLCDCSource) rowsToSnapshotBatches(ctx context.Context, rows *sql.Rows, tableSchema *schema.TableSchema, opts source.ReadOptions, snapshotLSN string, results chan<- source.RecordBatchResult, resultTable string) error {
	sourceColumns := sourceColumnsWithoutCDC(tableSchema)
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}
	// Snapshot rows are stamped incomplete until the last batch. Resume
	// trusts the destination's MAX(_cdc_lsn), and streaming destinations
	// commit batches incrementally, so a crash mid-snapshot leaves partial
	// rows whose stamp must not read as a resumable position. Batches are
	// emitted one behind the read, and only the final batch carries the
	// complete stamp, which sorts above the incomplete one: MAX names a
	// complete stamp exactly when the whole snapshot became durable.
	incompleteLSN := snapshotIncompleteLSN(snapshotLSN)
	completeLSN := snapshotCompleteLSN(snapshotLSN)
	syncedAt := time.Now().UTC()

	var pending arrow.RecordBatch
	for {
		record, count, err := buildBatch(rows, tableSchema, sourceColumns, batchSize, s.guidConversion, func(builders []array.Builder) {
			appendCDCValues(builders, len(sourceColumns), incompleteLSN, false, syncedAt)
		})
		if err != nil {
			if pending != nil {
				pending.Release()
			}
			return err
		}
		if count == 0 {
			break
		}
		if pending != nil {
			if err := emitResult(ctx, results, source.RecordBatchResult{Batch: pending, TableName: resultTable}); err != nil {
				record.Release()
				return err
			}
		}
		pending = record
	}
	if pending == nil {
		return nil
	}
	return emitResult(ctx, results, source.RecordBatchResult{Batch: restampSnapshotLSN(pending, len(sourceColumns), completeLSN), TableName: resultTable})
}

// restampSnapshotLSN rebuilds a snapshot batch's _cdc_lsn column with the
// given stamp. Used to mark the final batch of a snapshot complete after the
// scan proves no rows follow it.
func restampSnapshotLSN(record arrow.RecordBatch, lsnCol int, lsn string) arrow.RecordBatch {
	mem := memory.NewGoAllocator()
	b := array.NewStringBuilder(mem)
	defer b.Release()
	for range int(record.NumRows()) {
		b.Append(lsn)
	}
	arr := b.NewArray()
	defer arr.Release()

	cols := make([]arrow.Array, record.NumCols())
	for i := range cols {
		cols[i] = record.Column(i)
	}
	cols[lsnCol] = arr
	out := array.NewRecordBatch(record.Schema(), cols, record.NumRows())
	record.Release()
	return out
}

func (s *MSSQLCDCSource) rowsToChangeBatches(ctx context.Context, rows *sql.Rows, tableSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string) error {
	sourceColumns := sourceColumnsWithoutCDC(tableSchema)
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}
	syncedAt := time.Now().UTC()

	pairer, err := newUpdatePairer(tableSchema, sourceColumns)
	if err != nil {
		return err
	}

	for {
		record, count, err := buildChangeBatch(rows, tableSchema, sourceColumns, batchSize, s.guidConversion, syncedAt, pairer)
		if err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		if err := emitResult(ctx, results, source.RecordBatchResult{Batch: record, TableName: resultTable}); err != nil {
			return err
		}
	}
}

// changeRow is one row to emit: the scanned source-column values plus the CDC
// metadata derived from the change record.
type changeRow struct {
	values  []any
	lsn     string
	deleted bool
}

// updatePairer pairs an update's before-image (__$operation 3) with its
// after-image (__$operation 4), which the change query orders adjacently
// within the same start_lsn/seqval. When a primary-key column moved, the
// before-image is replayed as a delete of the old key so the destination does
// not keep the orphaned row; key-preserving before-images are dropped. The
// pending before-image survives batch boundaries inside one change read.
type updatePairer struct {
	pkIdx         []int
	hasPending    bool
	pendingValues []any
	pendingLSN    string
}

func newUpdatePairer(tableSchema *schema.TableSchema, sourceColumns []schema.Column) (*updatePairer, error) {
	idx := make([]int, 0, len(tableSchema.PrimaryKeys))
	for _, pk := range tableSchema.PrimaryKeys {
		found := -1
		for i, col := range sourceColumns {
			if strings.EqualFold(col.Name, pk) {
				found = i
				break
			}
		}
		if found < 0 {
			return nil, fmt.Errorf("primary key column %q of %s is not captured by the SQL Server CDC capture instance", pk, tableSchema.Name)
		}
		idx = append(idx, found)
	}
	return &updatePairer{pkIdx: idx}, nil
}

// push consumes one scanned change row and returns the rows to emit for it:
// the row itself, preceded by a delete of the pending before-image when an
// update moved its primary key.
func (p *updatePairer) push(values []any, lsn string, op int) []changeRow {
	if op == 3 {
		out := p.flush()
		p.pendingValues = values
		p.pendingLSN = lsn
		p.hasPending = true
		return out
	}

	var pendingDelete *changeRow
	if p.hasPending {
		// A before-image whose identity or operation does not line up with its
		// after-image cannot be verified as key-preserving, so it is treated as
		// an identity move, mirroring postgres_cdc's expandUpdates.
		keyMoved := op != 4 || lsnIdentity(p.pendingLSN) != lsnIdentity(lsn) || !p.primaryKeysEqual(values)
		if keyMoved {
			pendingDelete = &changeRow{values: p.pendingValues, lsn: p.pendingLSN, deleted: true}
		}
		p.hasPending = false
		p.pendingValues = nil
		p.pendingLSN = ""
	}

	row := changeRow{values: values, lsn: lsn, deleted: op == 1}
	if pendingDelete != nil {
		return []changeRow{*pendingDelete, row}
	}
	return []changeRow{row}
}

// flush emits any pending before-image as an identity-move delete. Called
// when the stream ends or another before-image arrives, where no matching
// after-image can confirm the update was key-preserving.
func (p *updatePairer) flush() []changeRow {
	if !p.hasPending {
		return nil
	}
	out := []changeRow{{values: p.pendingValues, lsn: p.pendingLSN, deleted: true}}
	p.hasPending = false
	p.pendingValues = nil
	p.pendingLSN = ""
	return out
}

func (p *updatePairer) primaryKeysEqual(after []any) bool {
	for _, i := range p.pkIdx {
		if !cdcValueEqual(p.pendingValues[i], after[i]) {
			return false
		}
	}
	return true
}

// lsnIdentity returns the start_lsn:seqval prefix that pairs a before-image
// with its after-image.
func lsnIdentity(lsn string) string {
	parts := strings.SplitN(lsn, ":", 3)
	if len(parts) < 2 {
		return lsn
	}
	return parts[0] + ":" + parts[1]
}

func cdcValueEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if ab, ok := a.([]byte); ok {
		bb, ok := b.([]byte)
		return ok && bytes.Equal(ab, bb)
	}
	if at, ok := a.(time.Time); ok {
		bt, ok := b.(time.Time)
		return ok && at.Equal(bt)
	}
	return reflect.DeepEqual(a, b)
}

// normalizeSourceValue converts driver-specific representations into the
// values the Arrow builders expect. uniqueidentifier arrives as raw bytes
// whose order depends on the connection's "guid conversion" setting.
func normalizeSourceValue(col schema.Column, val any, guidConversion bool) (any, error) {
	if col.DataType != schema.TypeUUID {
		return val, nil
	}
	normalized, err := mssql.NormalizeUUIDValue(val, guidConversion)
	if err != nil {
		return nil, fmt.Errorf("failed to convert uniqueidentifier column %q: %w", col.Name, err)
	}
	return normalized, nil
}

func buildBatch(rows *sql.Rows, tableSchema *schema.TableSchema, sourceColumns []schema.Column, batchSize int, guidConversion bool, appendCDC func([]array.Builder)) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	arrowSchema := buildArrowSchema(tableSchema.Columns)

	builders := make([]array.Builder, len(tableSchema.Columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	scanDest := make([]any, len(sourceColumns))
	for i := range scanDest {
		scanDest[i] = new(any)
	}

	var rowCount int64
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			releaseBuilders(builders)
			return nil, 0, fmt.Errorf("failed to scan row: %w", err)
		}

		for i, dest := range scanDest {
			val, err := normalizeSourceValue(sourceColumns[i], *dest.(*any), guidConversion)
			if err != nil {
				releaseBuilders(builders)
				return nil, 0, err
			}
			arrowconv.AppendValue(builders[i], val)
		}
		appendCDC(builders)

		rowCount++
		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	return finishBatch(rows, arrowSchema, builders, rowCount)
}

func buildChangeBatch(rows *sql.Rows, tableSchema *schema.TableSchema, sourceColumns []schema.Column, batchSize int, guidConversion bool, syncedAt time.Time, pairer *updatePairer) (arrow.RecordBatch, int64, error) {
	mem := memory.NewGoAllocator()
	arrowSchema := buildArrowSchema(tableSchema.Columns)

	builders := make([]array.Builder, len(tableSchema.Columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	scanDest := make([]any, len(sourceColumns)+2)
	for i := range scanDest {
		scanDest[i] = new(any)
	}

	var rowCount int64
	exhausted := true
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			releaseBuilders(builders)
			return nil, 0, fmt.Errorf("failed to scan CDC row: %w", err)
		}

		values := make([]any, len(sourceColumns))
		for i := range values {
			val, err := normalizeSourceValue(sourceColumns[i], *scanDest[i].(*any), guidConversion)
			if err != nil {
				releaseBuilders(builders)
				return nil, 0, err
			}
			values[i] = val
		}
		lsn := fmt.Sprintf("%v", *scanDest[len(sourceColumns)].(*any))
		op, err := operationValue(*scanDest[len(sourceColumns)+1].(*any))
		if err != nil {
			releaseBuilders(builders)
			return nil, 0, err
		}

		for _, change := range pairer.push(values, lsn, op) {
			for i, v := range change.values {
				arrowconv.AppendValue(builders[i], v)
			}
			appendCDCValues(builders, len(sourceColumns), change.lsn, change.deleted, syncedAt)
			rowCount++
		}
		if batchSize > 0 && rowCount >= int64(batchSize) {
			exhausted = false
			break
		}
	}
	if exhausted {
		for _, change := range pairer.flush() {
			for i, v := range change.values {
				arrowconv.AppendValue(builders[i], v)
			}
			appendCDCValues(builders, len(sourceColumns), change.lsn, change.deleted, syncedAt)
			rowCount++
		}
	}

	return finishBatch(rows, arrowSchema, builders, rowCount)
}

func finishBatch(rows *sql.Rows, arrowSchema *arrow.Schema, builders []array.Builder, rowCount int64) (arrow.RecordBatch, int64, error) {
	if err := rows.Err(); err != nil {
		releaseBuilders(builders)
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	if rowCount == 0 {
		releaseBuilders(builders)
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
	releaseBuilders(builders)
	return record, rowCount, nil
}

func releaseBuilders(builders []array.Builder) {
	for _, builder := range builders {
		builder.Release()
	}
}

func appendCDCValues(builders []array.Builder, offset int, lsn string, deleted bool, syncedAt time.Time) {
	builders[offset].(*array.StringBuilder).Append(lsn)
	builders[offset+1].(*array.BooleanBuilder).Append(deleted)
	builders[offset+2].(*array.TimestampBuilder).Append(arrow.Timestamp(syncedAt.UnixMicro()))
}

func operationValue(v any) (int, error) {
	switch op := v.(type) {
	case int:
		return op, nil
	case int32:
		return int(op), nil
	case int64:
		return int(op), nil
	case int16:
		return int(op), nil
	case []byte:
		return strconv.Atoi(string(op))
	case string:
		return strconv.Atoi(op)
	default:
		return 0, fmt.Errorf("unexpected SQL Server CDC operation type %T", v)
	}
}

func buildSnapshotQuery(meta tableMetadata, columns []schema.Column, lock bool) string {
	selects := make([]string, len(columns))
	for i, col := range columns {
		if meta.CurrentColumns[strings.ToLower(col.Name)] {
			selects[i] = quoteIdentifier(col.Name)
		} else {
			selects[i] = fmt.Sprintf("NULL AS %s", quoteIdentifier(col.Name))
		}
	}

	hint := ""
	if lock {
		hint = " WITH (HOLDLOCK)"
	}
	return fmt.Sprintf("SELECT %s FROM %s%s", strings.Join(selects, ", "), quoteTable(tableName(meta)), hint)
}

// buildChangesQuery selects change rows for one capture instance, ordered so
// an update's before-image (__$operation 3) immediately precedes its
// after-image (__$operation 4) within the same start_lsn/seqval. When
// inclusive is false, the window starts strictly after fromLSN; when true, at
// it. Inclusive windows re-deliver the transaction holding the bound, which
// keeps a resume cursor that landed mid-transaction from dropping its tail.
func buildChangesQuery(meta tableMetadata, columns []schema.Column, inclusive bool) string {
	selects := make([]string, 0, len(columns)+2)
	for _, col := range columns {
		selects = append(selects, quoteIdentifier(col.Name))
	}
	selects = append(
		selects,
		`CONCAT(CONVERT(varchar(20), __$start_lsn, 2), ':', CONVERT(varchar(20), __$seqval, 2), ':', RIGHT('00' + CONVERT(varchar(2), __$operation), 2)) AS __ingestr_cdc_lsn`,
		`__$operation AS __ingestr_cdc_operation`,
	)

	fromExpr := "sys.fn_cdc_increment_lsn(CONVERT(binary(10), @p1, 2))"
	if inclusive {
		fromExpr = "CONVERT(binary(10), @p1, 2)"
	}

	return fmt.Sprintf(`
		DECLARE @from_lsn binary(10) = %s;
		DECLARE @to_lsn binary(10) = CONVERT(binary(10), @p2, 2);
		SELECT %s
		FROM %s(@from_lsn, @to_lsn, N'all update old')
		WHERE __$operation IN (1, 2, 3, 4)
		ORDER BY __$start_lsn, __$seqval, __$operation
	`, fromExpr, strings.Join(selects, ", "), quoteFunction("fn_cdc_get_all_changes_"+meta.CaptureInstance))
}

func buildArrowSchema(columns []schema.Column) *arrow.Schema {
	fields := make([]arrow.Field, len(columns))
	for i, col := range columns {
		fields[i] = arrow.Field{Name: col.Name, Type: schema.DataTypeToArrowType(col), Nullable: col.Nullable}
	}
	return arrow.NewSchema(fields, nil)
}

func addCDCColumns(tableSchema *schema.TableSchema) *schema.TableSchema {
	copied := *tableSchema
	copied.Columns = append(append([]schema.Column{}, tableSchema.Columns...), mssqlCDCColumns...)
	return &copied
}

func sourceColumnsWithoutCDC(tableSchema *schema.TableSchema) []schema.Column {
	return tableSchema.Columns[:len(tableSchema.Columns)-len(mssqlCDCColumns)]
}

func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "dbo", table
}

func tableName(meta tableMetadata) string {
	return meta.SourceSchema + "." + meta.SourceName
}

func quoteIdentifier(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

func quoteTable(table string) string {
	schemaName, tableName := parseTableName(table)
	return quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)
}

func quoteFunction(name string) string {
	return quoteIdentifier("cdc") + "." + quoteIdentifier(name)
}

var lsnHexRegex = regexp.MustCompile(`(?i)[0-9a-f]{20}`)

func startLSNFromStored(stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return ""
	}
	match := lsnHexRegex.FindString(stored)
	if match == "" {
		return ""
	}
	return normalizeLSNHex(match)
}

func formatStoredLSN(startHex, seqHex string, op int) string {
	startHex = normalizeLSNHex(startHex)
	if seqHex == "" {
		seqHex = "00000000000000000000"
	} else {
		seqHex = normalizeLSNHex(seqHex)
	}
	return fmt.Sprintf("%s:%s:%02d", startHex, seqHex, op)
}

// Snapshot stamps carry a zero seqval, which no delivered change row can
// have, plus a final byte encoding completeness. The incomplete stamp sorts
// below the complete one so the destination's MAX(_cdc_lsn) only names a
// complete stamp once the snapshot's final batch became durable.
//
// Positions written by versions that predate the completeness byte used the
// incomplete encoding for finished snapshots; they re-snapshot once after an
// upgrade, which is the safe direction (they cannot prove completeness).
func snapshotIncompleteLSN(startHex string) string {
	return formatStoredLSN(startHex, "", 0)
}

func snapshotCompleteLSN(startHex string) string {
	return formatStoredLSN(startHex, "", 1)
}

// isSnapshotStamp reports whether a stored position was written by a snapshot
// (zero seqval) rather than by a delivered change row. Only a change-row
// position can sit mid-transaction, so only it earns a boundary re-read; a
// snapshot stamp already covers everything at its LSN.
func isSnapshotStamp(stored string) bool {
	s := strings.ToUpper(strings.TrimSpace(stored))
	return strings.HasSuffix(s, ":00000000000000000000:00") || strings.HasSuffix(s, ":00000000000000000000:01")
}

// isIncompleteSnapshotStamp reports whether a stored position marks a
// snapshot that never finished: resuming from it would skip every row the
// interrupted scan had not reached, so it forces a fresh snapshot instead.
func isIncompleteSnapshotStamp(stored string) bool {
	return strings.HasSuffix(strings.ToUpper(strings.TrimSpace(stored)), ":00000000000000000000:00")
}

func normalizeLSNHex(lsn string) string {
	lsn = strings.TrimSpace(lsn)
	lsn = strings.TrimPrefix(strings.TrimPrefix(lsn, "0x"), "0X")
	return strings.ToUpper(lsn)
}

func compareLSNHex(left, right string) int {
	left = normalizeLSNHex(left)
	right = normalizeLSNHex(right)
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func isZeroLSN(lsn string) bool {
	return strings.Trim(lsn, "0") == ""
}

var (
	_ source.Source           = (*MSSQLCDCSource)(nil)
	_ source.MultiTableSource = (*MSSQLCDCSource)(nil)
	_ source.StreamingSource  = (*MSSQLCDCSource)(nil)
	_ source.SourceTable      = (*CDCTable)(nil)
)
