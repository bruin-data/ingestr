package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
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
)

const ctVersionWidth = 20

var ctMetadataColumns = []schema.Column{
	{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: false},
	{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: false},
	{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
}

type MSSQLChangeTrackingSource struct {
	MSSQLSource
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
	return &MSSQLChangeTrackingSource{}
}

func (s *MSSQLChangeTrackingSource) Schemes() []string {
	return []string{"mssql+ct", "sqlserver+ct", "azuresql+ct", "azure-sql+ct"}
}

func (s *MSSQLChangeTrackingSource) Connect(ctx context.Context, uri string) error {
	normalizedURI, err := normalizeChangeTrackingURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse SQL Server Change Tracking URI: %w", err)
	}

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

		tableSchema := t.tableSchema
		if opts.Schema != nil {
			tableSchema = opts.Schema
		}

		if version, ok := parseStoredCTVersion(opts.CDCResumeLSN); ok {
			_, err := t.source.readCTChanges(ctx, t.tableName, tableSchema, t.primaryKeys, version, opts, results, true)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			return
		}

		snapshotVersion, err := t.source.snapshotCTTable(ctx, t.tableName, tableSchema, opts, results)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)}
			return
		}

		if opts.FullRefresh {
			return
		}

		_, err = t.source.readCTChanges(ctx, t.tableName, tableSchema, t.primaryKeys, snapshotVersion, opts, results, false)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
	}()

	return results, nil
}

func normalizeChangeTrackingURI(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("unsupported Change Tracking scheme: %s", parsed.Scheme)
	}

	return parsed.String(), nil
}

func (s *MSSQLChangeTrackingSource) ensureDatabaseChangeTracking(ctx context.Context) error {
	var enabled int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.change_tracking_databases WHERE database_id = DB_ID()").Scan(&enabled)
	if err != nil {
		return fmt.Errorf("failed to check SQL Server Change Tracking status: %w", err)
	}
	if enabled == 0 {
		return fmt.Errorf("SQL Server Change Tracking is not enabled for the current database; run ALTER DATABASE ... SET CHANGE_TRACKING = ON first")
	}
	return nil
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
		case "", config.StrategyReplace, config.StrategyTruncateInsert, config.StrategyAppend, config.StrategyDeleteInsert, config.StrategyMerge, config.StrategySCD2, config.StrategyNone:
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

func (s *MSSQLChangeTrackingSource) snapshotCTTable(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult) (int64, error) {
	version, emittedRows, err := s.snapshotCTTableWithIsolation(ctx, table, tableSchema, opts, results, sql.LevelSnapshot)
	if err == nil {
		return version, nil
	}
	if emittedRows > 0 {
		return 0, fmt.Errorf("SNAPSHOT isolation snapshot for %s failed after emitting %d rows; not retrying with table lock to avoid duplicate records: %w", table, emittedRows, err)
	}
	config.Debug("[MSSQL CT] SNAPSHOT isolation snapshot failed for %s: %v; retrying with table lock", table, err)
	version, _, err = s.snapshotCTTableWithIsolation(ctx, table, tableSchema, opts, results, sql.LevelSerializable)
	return version, err
}

func (s *MSSQLChangeTrackingSource) snapshotCTTableWithIsolation(ctx context.Context, table string, tableSchema *schema.TableSchema, opts source.ReadOptions, results chan<- source.RecordBatchResult, isolation sql.IsolationLevel) (int64, int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
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
	defer func() { _ = rows.Close() }()

	emittedRows, err := s.rowsToCTBatches(rows, columns, opts, results)
	if err != nil {
		return 0, emittedRows, err
	}
	if emittedRows == 0 && !opts.FullRefresh {
		if err := emitSyntheticCTHeartbeat(tableSchema.Columns, tableSchema.PrimaryKeys, version, results); err != nil {
			return 0, 0, err
		}
		emittedRows = 1
	}

	if err := tx.Commit(); err != nil {
		return 0, emittedRows, fmt.Errorf("failed to commit snapshot transaction: %w", err)
	}

	return version, emittedRows, nil
}

func (s *MSSQLChangeTrackingSource) readCTChanges(ctx context.Context, table string, tableSchema *schema.TableSchema, primaryKeys []string, fromVersion int64, opts source.ReadOptions, results chan<- source.RecordBatchResult, emitHeartbeat bool) (int64, error) {
	readThroughVersion, err := s.readCTChangesWithIsolation(ctx, table, tableSchema, primaryKeys, fromVersion, opts, results, sql.LevelReadCommitted, emitHeartbeat)
	if err == nil {
		return readThroughVersion, nil
	}
	var expired *ctVersionExpiredError
	if errors.As(err, &expired) {
		return 0, err
	}
	return 0, fmt.Errorf("failed to read SQL Server Change Tracking changes using READ COMMITTED isolation: %w", err)
}

func (s *MSSQLChangeTrackingSource) readCTChangesWithIsolation(ctx context.Context, table string, tableSchema *schema.TableSchema, primaryKeys []string, fromVersion int64, opts source.ReadOptions, results chan<- source.RecordBatchResult, isolation sql.IsolationLevel, emitHeartbeat bool) (int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
	if err != nil {
		return 0, fmt.Errorf("failed to begin Change Tracking transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	minVersion, err := s.minValidCTVersion(ctx, tx, table)
	if err != nil {
		return 0, err
	}
	if fromVersion < minVersion {
		return 0, &ctVersionExpiredError{table: table, version: fromVersion, minVersion: minVersion}
	}

	targetVersion, err := s.currentCTVersion(ctx, tx)
	if err != nil {
		return 0, err
	}
	if targetVersion <= fromVersion {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("failed to commit Change Tracking transaction: %w", err)
		}
		return fromVersion, nil
	}

	columns := tableSchema.Columns
	query := buildCTChangesQuery(table, sourceColumnsWithoutCT(tableSchema), primaryKeys)
	rows, err := tx.QueryContext(ctx, query, fromVersion, targetVersion)
	if err != nil {
		return 0, fmt.Errorf("failed to query Change Tracking changes for %s: %w", table, err)
	}

	changeRows, err := s.rowsToCTBatches(rows, columns, opts, results)
	if err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("failed to close Change Tracking rows for %s: %w", table, err)
	}

	if shouldEmitCTHeartbeat(emitHeartbeat, changeRows) {
		if err := s.emitCTHeartbeat(ctx, tx, table, tableSchema, primaryKeys, targetVersion, opts, results); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit Change Tracking transaction: %w", err)
	}

	return targetVersion, nil
}

func (s *MSSQLChangeTrackingSource) emitCTHeartbeat(ctx context.Context, tx *sql.Tx, table string, tableSchema *schema.TableSchema, primaryKeys []string, targetVersion int64, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	query := buildCTHeartbeatQuery(table, sourceColumnsWithoutCT(tableSchema), primaryKeys, targetVersion)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query Change Tracking heartbeat for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	emittedRows, err := s.rowsToCTBatches(rows, tableSchema.Columns, opts, results)
	if err != nil {
		return err
	}
	if emittedRows == 0 {
		return emitSyntheticCTHeartbeat(tableSchema.Columns, primaryKeys, targetVersion, results)
	}
	return nil
}

func shouldEmitCTHeartbeat(emitHeartbeat bool, changeRows int64) bool {
	return emitHeartbeat && changeRows == 0
}

func emitSyntheticCTHeartbeat(columns []schema.Column, primaryKeys []string, targetVersion int64, results chan<- source.RecordBatchResult) error {
	record, err := syntheticCTHeartbeatRecord(columns, primaryKeys, targetVersion)
	if err != nil {
		return err
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
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

func (s *MSSQLChangeTrackingSource) rowsToCTBatches(rows *sql.Rows, columns []schema.Column, opts source.ReadOptions, results chan<- source.RecordBatchResult) (int64, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}
	arrowSchema := buildArrowSchema(columns)
	var totalRows int64

	for {
		record, count, err := rowsToArrowRecordBatch(rows, arrowSchema, columns, batchSize, s.guidConversion)
		if err != nil {
			return totalRows, err
		}
		if count == 0 {
			return totalRows, nil
		}
		totalRows += count
		results <- source.RecordBatchResult{Batch: record}
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
