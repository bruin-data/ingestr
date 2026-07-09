package mssql_cdc

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
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
	"github.com/bruin-data/ingestr/pkg/source/mssql"
)

type CDCMode string

const (
	ModeBatch  CDCMode = "batch"
	ModeStream CDCMode = "stream"

	defaultPollInterval = 1 * time.Second
)

var mssqlCDCColumns = []schema.Column{
	{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
	{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
	{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
}

type CDCConfig struct {
	Mode            CDCMode
	DestSchema      string
	CaptureInstance string
	PollInterval    time.Duration
}

type MSSQLCDCSource struct {
	db        *sql.DB
	uri       string
	cdcConfig CDCConfig
	lag       *mssqlLagState
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

	streaming atomic.Bool
}

func newMSSQLLagState() *mssqlLagState {
	return &mssqlLagState{}
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
func (s *MSSQLCDCSource) SupportsStreaming() bool {
	return true
}

// DefaultStreamingStrategy returns merge: CDC changes (including deletes) are
// applied by primary key.
func (s *MSSQLCDCSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

// applyStreamingMode forces continuous polling when --stream is set, regardless
// of the URI ?mode= parameter. SQL Server CDC reads change tables via polling
// and has no consumer-side acknowledgement, so there is no StreamCommitter:
// at-least-once is provided by resuming from the destination's max _cdc_lsn.
func (s *MSSQLCDCSource) applyStreamingMode(streaming bool) {
	if streaming {
		s.cdcConfig.Mode = ModeStream
	}
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
	tableSchema.PrimaryKeys = pks

	strategy := config.StrategyMerge
	if req.Strategy != "" && req.Strategy != config.StrategyReplace {
		strategy = req.Strategy
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
	metas, err := s.getAllTableMetadata(ctx)
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return nil, fmt.Errorf("no SQL Server CDC-enabled tables found")
	}

	tables := make([]source.SourceTableInfo, 0, len(metas))
	for _, meta := range metas {
		tableSchema, err := s.getCapturedSchema(ctx, meta)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema for %s: %w", tableName(meta), err)
		}
		tableSchema = addCDCColumns(tableSchema)
		if len(tableSchema.PrimaryKeys) == 0 {
			return nil, fmt.Errorf("SQL Server CDC table %s has no primary key; multi-table CDC requires source primary keys", tableName(meta))
		}

		tables = append(tables, source.SourceTableInfo{
			Name:        tableName(meta),
			Schema:      tableSchema,
			PrimaryKeys: tableSchema.PrimaryKeys,
			DestSchema:  s.cdcConfig.DestSchema,
		})
	}

	return tables, nil
}

func (s *MSSQLCDCSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	s.applyStreamingMode(opts.Streaming)
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

		startByTable := make(map[string]string, len(selected))
		metaByTable := make(map[string]tableMetadata, len(selected))

		for _, table := range selected {
			meta, err := s.getTableMetadata(ctx, table.Name, "")
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			metaByTable[table.Name] = meta

			resume := opts.CDCResumeLSNs[table.Name]
			startHex := startLSNFromStored(resume)
			if startHex != "" {
				canResume, err := s.canResume(ctx, meta, startHex)
				if err != nil {
					results <- source.RecordBatchResult{Err: err}
					return
				}
				if canResume {
					startByTable[table.Name] = startHex
					continue
				}
			}

			snapshotLSN, err := s.snapshotTable(ctx, meta, table.Schema, opts.ReadOptions, results, table.Name)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed for %s: %w", table.Name, err)}
				return
			}
			startByTable[table.Name] = snapshotLSN
		}

		if err := s.streamTables(ctx, selected, metaByTable, startByTable, opts.ReadOptions, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func parseURIConfig(rawURI string) (CDCConfig, string, error) {
	cfg := CDCConfig{
		Mode:         ModeBatch,
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

	if mode := query.Get("mode"); mode != "" {
		switch strings.ToLower(mode) {
		case string(ModeBatch):
			cfg.Mode = ModeBatch
		case string(ModeStream):
			cfg.Mode = ModeStream
		default:
			return cfg, "", fmt.Errorf("invalid mode: %s (must be 'batch' or 'stream')", mode)
		}
	}

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

	return &schema.TableSchema{
		Name:        meta.SourceName,
		Schema:      meta.SourceSchema,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
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
	t.source.applyStreamingMode(opts.Streaming)
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		startHex := startLSNFromStored(opts.CDCResumeLSN)
		if startHex != "" {
			canResume, err := t.source.canResume(ctx, t.metadata, startHex)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			if canResume {
				if err := t.source.streamTable(ctx, t.metadata, t.tableSchema, startHex, opts, results, ""); err != nil {
					results <- source.RecordBatchResult{Err: err}
				}
				return
			}
			config.Debug("[MSSQL CDC] Resume LSN %s is no longer valid for %s; taking a fresh snapshot", startHex, t.tableName)
		}

		snapshotLSN, err := t.source.snapshotTable(ctx, t.metadata, t.tableSchema, opts, results, "")
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)}
			return
		}

		if err := t.source.streamTable(ctx, t.metadata, t.tableSchema, snapshotLSN, opts, results, ""); err != nil {
			results <- source.RecordBatchResult{Err: err}
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
	snapshotLSN, err := s.snapshotTableWithIsolation(ctx, meta, tableSchema, opts, results, resultTable, sql.LevelSnapshot)
	if err == nil {
		return snapshotLSN, nil
	}
	config.Debug("[MSSQL CDC] SNAPSHOT isolation snapshot failed for %s: %v; retrying with table lock", tableName(meta), err)
	return s.snapshotTableWithIsolation(ctx, meta, tableSchema, opts, results, resultTable, sql.LevelSerializable)
}

func (s *MSSQLCDCSource) snapshotTableWithIsolation(ctx context.Context, meta tableMetadata, tableSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string, isolation sql.IsolationLevel) (string, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: isolation, ReadOnly: isolation == sql.LevelSnapshot})
	if err != nil {
		return "", fmt.Errorf("failed to begin snapshot transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	lock := isolation != sql.LevelSnapshot
	snapshotLSN, err := s.maxLSNFromQueryer(ctx, tx)
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

	query := buildSnapshotQuery(meta, sourceColumnsWithoutCDC(tableSchema), lock)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to query snapshot for %s: %w", tableName(meta), err)
	}
	defer func() { _ = rows.Close() }()

	if err := s.rowsToSnapshotBatches(rows, tableSchema, opts, snapshotLSN, results, resultTable); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit snapshot transaction: %w", err)
	}

	return snapshotLSN, nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *MSSQLCDCSource) maxLSN(ctx context.Context) (string, error) {
	return s.maxLSNFromQueryer(ctx, s.db)
}

func (s *MSSQLCDCSource) maxLSNFromQueryer(ctx context.Context, q queryer) (string, error) {
	var lsn sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT CONVERT(varchar(20), sys.fn_cdc_get_max_lsn(), 2)").Scan(&lsn); err != nil {
		return "", fmt.Errorf("failed to get SQL Server CDC max LSN: %w", err)
	}
	if !lsn.Valid {
		return "", nil
	}
	return normalizeLSNHex(lsn.String), nil
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

func (s *MSSQLCDCSource) streamTables(ctx context.Context, tables []source.SourceTableInfo, metas map[string]tableMetadata, startByTable map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	s.lag.streaming.Store(opts.Streaming)
	for {
		targetLSN, err := s.maxLSN(ctx)
		if err != nil {
			return err
		}
		if targetLSN == "" || isZeroLSN(targetLSN) {
			if s.cdcConfig.Mode == ModeBatch {
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

		for _, table := range tables {
			start := startByTable[table.Name]
			if start == "" || compareLSNHex(start, targetLSN) >= 0 {
				startByTable[table.Name] = targetLSN
				continue
			}

			if err := s.readChanges(ctx, metas[table.Name], table.Schema, start, targetLSN, opts, results, table.Name); err != nil {
				return err
			}
			startByTable[table.Name] = targetLSN
		}

		if s.cdcConfig.Mode == ModeBatch {
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

func (s *MSSQLCDCSource) streamTable(ctx context.Context, meta tableMetadata, tableSchema *schema.TableSchema, startLSN string, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string) error {
	s.lag.streaming.Store(opts.Streaming)
	current := startLSN
	for {
		targetLSN, err := s.maxLSN(ctx)
		if err != nil {
			return err
		}
		s.noteLag(ctx, current, targetLSN)
		if targetLSN != "" && current != "" && compareLSNHex(current, targetLSN) < 0 {
			if err := s.readChanges(ctx, meta, tableSchema, current, targetLSN, opts, results, resultTable); err != nil {
				return err
			}
			current = targetLSN
		}

		if s.cdcConfig.Mode == ModeBatch {
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

func (s *MSSQLCDCSource) readChanges(ctx context.Context, meta tableMetadata, tableSchema *schema.TableSchema, fromLSN string, toLSN string, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string) error {
	if fromLSN == "" || toLSN == "" || compareLSNHex(fromLSN, toLSN) >= 0 {
		return nil
	}

	sourceColumns := sourceColumnsWithoutCDC(tableSchema)
	query := buildChangesQuery(meta, sourceColumns)
	rows, err := s.db.QueryContext(ctx, query, fromLSN, toLSN)
	if err != nil {
		return fmt.Errorf("failed to query CDC changes for %s: %w", tableName(meta), err)
	}
	defer func() { _ = rows.Close() }()

	return s.rowsToChangeBatches(rows, tableSchema, opts, results, resultTable)
}

func (s *MSSQLCDCSource) rowsToSnapshotBatches(rows *sql.Rows, tableSchema *schema.TableSchema, opts source.ReadOptions, snapshotLSN string, results chan<- source.RecordBatchResult, resultTable string) error {
	sourceColumns := sourceColumnsWithoutCDC(tableSchema)
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}
	cdcLSN := formatStoredLSN(snapshotLSN, "", 0)
	syncedAt := time.Now().UTC()

	for {
		record, count, err := buildBatch(rows, tableSchema, sourceColumns, batchSize, func(builders []array.Builder) {
			appendCDCValues(builders, len(sourceColumns), cdcLSN, false, syncedAt)
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

func (s *MSSQLCDCSource) rowsToChangeBatches(rows *sql.Rows, tableSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult, resultTable string) error {
	sourceColumns := sourceColumnsWithoutCDC(tableSchema)
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}
	syncedAt := time.Now().UTC()

	for {
		record, count, err := buildChangeBatch(rows, tableSchema, sourceColumns, batchSize, syncedAt)
		if err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		results <- source.RecordBatchResult{Batch: record, TableName: resultTable}
	}
}

func buildBatch(rows *sql.Rows, tableSchema *schema.TableSchema, sourceColumns []schema.Column, batchSize int, appendCDC func([]array.Builder)) (arrow.RecordBatch, int64, error) {
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
			arrowconv.AppendValue(builders[i], *dest.(*any))
		}
		appendCDC(builders)

		rowCount++
		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	return finishBatch(rows, arrowSchema, builders, rowCount)
}

func buildChangeBatch(rows *sql.Rows, tableSchema *schema.TableSchema, sourceColumns []schema.Column, batchSize int, syncedAt time.Time) (arrow.RecordBatch, int64, error) {
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
	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			releaseBuilders(builders)
			return nil, 0, fmt.Errorf("failed to scan CDC row: %w", err)
		}

		for i := range sourceColumns {
			arrowconv.AppendValue(builders[i], *scanDest[i].(*any))
		}

		lsn := fmt.Sprintf("%v", *scanDest[len(sourceColumns)].(*any))
		op, err := operationValue(*scanDest[len(sourceColumns)+1].(*any))
		if err != nil {
			releaseBuilders(builders)
			return nil, 0, err
		}
		appendCDCValues(builders, len(sourceColumns), lsn, op == 1, syncedAt)

		rowCount++
		if batchSize > 0 && rowCount >= int64(batchSize) {
			break
		}
	}

	return finishBatch(rows, arrowSchema, builders, rowCount)
}

func finishBatch(rows *sql.Rows, arrowSchema *arrow.Schema, builders []array.Builder, rowCount int64) (arrow.RecordBatch, int64, error) {
	if rowCount == 0 {
		releaseBuilders(builders)
		return nil, 0, nil
	}

	if err := rows.Err(); err != nil {
		releaseBuilders(builders)
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

func buildChangesQuery(meta tableMetadata, columns []schema.Column) string {
	selects := make([]string, 0, len(columns)+2)
	for _, col := range columns {
		selects = append(selects, quoteIdentifier(col.Name))
	}
	selects = append(
		selects,
		`CONCAT(CONVERT(varchar(20), __$start_lsn, 2), ':', CONVERT(varchar(20), __$seqval, 2), ':', RIGHT('00' + CONVERT(varchar(2), __$operation), 2)) AS __ingestr_cdc_lsn`,
		`__$operation AS __ingestr_cdc_operation`,
	)

	return fmt.Sprintf(`
		DECLARE @from_lsn binary(10) = sys.fn_cdc_increment_lsn(CONVERT(binary(10), @p1, 2));
		DECLARE @to_lsn binary(10) = CONVERT(binary(10), @p2, 2);
		SELECT %s
		FROM %s(@from_lsn, @to_lsn, N'all update old')
		WHERE __$operation IN (1, 2, 4)
		ORDER BY __$start_lsn, __$seqval, __$operation
	`, strings.Join(selects, ", "), quoteFunction("fn_cdc_get_all_changes_"+meta.CaptureInstance))
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
