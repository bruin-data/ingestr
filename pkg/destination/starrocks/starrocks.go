package starrocks

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	_ "github.com/go-sql-driver/mysql"
)

const defaultHTTPPort = 8030

// syntheticSortKeyColumn is added as an auto-increment key only when a table
// has no column StarRocks can use as a sort key (all columns non-keyable).
const syntheticSortKeyColumn = "__ingestr_sort_key"

// StarRocksDestination loads data into StarRocks. DDL and table swaps go over
// the MySQL protocol (FE query port); row data is loaded via the Stream Load
// HTTP API (FE http port).
type StarRocksDestination struct {
	db                 *sql.DB
	loader             *streamLoader
	database           string
	replicationNum     string // empty => use the cluster default
	labelCounter       int64
	legacyLoadMetadata atomic.Bool

	swapMu sync.Mutex
	// preparedSwaps maps cross-database staging tables to the copies that
	// CDCConditionalSwapIncarnations placed in the target database.
	preparedSwaps map[string]string
	keptSwaps     map[string]bool
}

func NewStarRocksDestination() *StarRocksDestination {
	return &StarRocksDestination{}
}

func (d *StarRocksDestination) Schemes() []string { return []string{"starrocks"} }

func (d *StarRocksDestination) GetScheme() string { return "starrocks" }

func (d *StarRocksDestination) Connect(ctx context.Context, uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("failed to parse StarRocks URI: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port := u.Port()
	if port == "" {
		port = "9030"
	}
	var user, password string
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}
	// Accept a [catalog/]database path: a connection shared with the source may
	// carry a catalog, but the destination writes to the native catalog, so use
	// the last path segment as the database.
	if trimmed := strings.Trim(u.Path, "/"); trimmed != "" {
		parts := strings.Split(trimmed, "/")
		d.database = parts[len(parts)-1]
	}

	query := u.Query()
	httpPort := defaultHTTPPort
	if v := query.Get("http_port"); v != "" {
		p, convErr := strconv.Atoi(v)
		if convErr != nil || p <= 0 || p > 65535 {
			return fmt.Errorf("invalid StarRocks http_port %q", v)
		}
		httpPort = p
	}
	d.replicationNum = query.Get("replication_num")

	tls, err := tlsParam(query.Get("ssl"))
	if err != nil {
		return err
	}

	dsn := ""
	if user != "" {
		dsn = user
		if password != "" {
			dsn += ":" + password
		}
		dsn += "@"
	}
	// The database is omitted from the DSN: it may not exist yet (the destination
	// creates it), and pinning it would make the driver USE it on connect.
	dsn += fmt.Sprintf("tcp(%s:%s)/?parseTime=true&interpolateParams=true", host, port)
	if tls != "" {
		dsn += "&tls=" + tls
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open StarRocks connection: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping StarRocks: %w", err)
	}

	d.db = db
	d.legacyLoadMetadata.Store(false)
	d.loader = newStreamLoader(host, httpPort, user, password)
	d.loader.waitForPublish = d.waitForPublish
	config.Debug("[STARROCKS] Connected (database: %s, http_port: %d)", d.database, httpPort)
	return nil
}

func (d *StarRocksDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *StarRocksDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	return d.prepareTable(ctx, opts, false)
}

// prepareTable creates the table; exclusive fails if it already exists.
func (d *StarRocksDestination) prepareTable(ctx context.Context, opts destination.PrepareOptions, exclusive bool) error {
	if err := tablename.TwoLevel("starrocks").CheckName(opts.Table); err != nil {
		return err
	}
	if opts.Schema != nil {
		for _, col := range opts.Schema.Columns {
			if strings.Contains(col.Name, "`") {
				return fmt.Errorf("StarRocks cannot store column %q: column names containing a backtick are not supported", col.Name)
			}
		}
	}
	db, err := d.requireDatabase(opts.Table)
	if err != nil {
		return err
	}
	createDB := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", quoteColumn(db))
	if _, err := d.db.ExecContext(ctx, createDB); err != nil {
		config.LogFailedQuery(createDB, err)
		return fmt.Errorf("failed to ensure database %s: %w", db, err)
	}

	if opts.DropFirst {
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", d.tableRef(opts.Table))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
	}

	if opts.Schema != nil {
		tableSchema := opts.Schema
		if len(opts.PrimaryKeys) > 0 {
			tableSchema = destination.DestinationTableSchema(tableSchema)
		}
		createSQL := d.buildCreateTableSQL(opts.Table, tableSchema.Columns, opts.PrimaryKeys)
		if exclusive {
			createSQL = strings.Replace(createSQL, "CREATE TABLE IF NOT EXISTS", "CREATE TABLE", 1)
		}
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
	}
	if opts.RequirePrimaryKeyMatch {
		return d.validatePrimaryKey(ctx, "CDC target", opts.Table, opts.PrimaryKeys)
	}
	return nil
}

// buildCreateTableSQL renders StarRocks DDL. With primary keys it creates a
// PRIMARY KEY table (key columns declared first and NOT NULL, hash-distributed
// on the keys); otherwise a duplicate-key table with random distribution.
// isStarRocksKeyable reports whether a column type may be a StarRocks key (and
// therefore the leading column of a duplicate-key table).
func isStarRocksKeyable(col schema.Column) bool {
	switch col.DataType {
	case schema.TypeFloat32, schema.TypeFloat64, schema.TypeBinary, schema.TypeJSON, schema.TypeArray:
		return false
	default:
		return true
	}
}

// keyableFirst returns columns with the first keyable column moved to the front
// when the leading column isn't keyable, leaving order unchanged otherwise. If
// no column is keyable the input is returned as-is.
func keyableFirst(columns []schema.Column) []schema.Column {
	if len(columns) == 0 || isStarRocksKeyable(columns[0]) {
		return columns
	}
	for i, c := range columns {
		if isStarRocksKeyable(c) {
			out := make([]schema.Column, 0, len(columns))
			out = append(out, c)
			out = append(out, columns[:i]...)
			out = append(out, columns[i+1:]...)
			return out
		}
	}
	return columns
}

func (d *StarRocksDestination) buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	pkSet := make(map[string]bool, len(primaryKeys))
	for _, k := range primaryKeys {
		pkSet[strings.ToLower(k)] = true
	}

	colDef := func(col schema.Column, forceNotNull bool) string {
		def := fmt.Sprintf("%s %s", quoteColumn(col.Name), MapDataTypeToStarRocks(col))
		if forceNotNull {
			def += " NOT NULL"
		}
		return def
	}

	var colDefs []string
	syntheticKey := false
	if len(primaryKeys) > 0 {
		// Key columns must be declared first, in key order, and NOT NULL.
		byName := make(map[string]schema.Column, len(columns))
		for _, c := range columns {
			byName[strings.ToLower(c.Name)] = c
		}
		for _, k := range primaryKeys {
			if c, ok := byName[strings.ToLower(k)]; ok {
				colDefs = append(colDefs, colDef(c, true))
			}
		}
		for _, c := range columns {
			if !pkSet[strings.ToLower(c.Name)] {
				colDefs = append(colDefs, colDef(c, false))
			}
		}
	} else {
		// A duplicate-key table's leading column becomes the sort key, which
		// StarRocks rejects for non-keyable types (FLOAT/DOUBLE/ARRAY/JSON/
		// VARBINARY). Move the first keyable column to the front so table
		// creation succeeds; Stream Load and the swap/merge SQL map by name, so
		// column order is not otherwise significant.
		ordered := keyableFirst(columns)
		if len(ordered) > 0 && !isStarRocksKeyable(ordered[0]) {
			// No column can be a sort key, so synthesize an auto-increment key.
			// Stream Load fills it automatically since it isn't in the batch.
			syntheticKey = true
			colDefs = append(colDefs, quoteColumn(syntheticSortKeyColumn)+" BIGINT AUTO_INCREMENT")
		}
		for _, c := range ordered {
			colDefs = append(colDefs, colDef(c, false))
		}
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)", d.tableRef(table), strings.Join(colDefs, ",\n  "))

	switch {
	case len(primaryKeys) > 0:
		keys := quoteColumns(primaryKeys)
		joined := strings.Join(keys, ", ")
		sql += fmt.Sprintf("\nPRIMARY KEY (%s)\nDISTRIBUTED BY HASH (%s)", joined, joined)
	case syntheticKey:
		k := quoteColumn(syntheticSortKeyColumn)
		sql += fmt.Sprintf("\nDUPLICATE KEY (%s)\nDISTRIBUTED BY HASH (%s)", k, k)
	default:
		sql += "\nDISTRIBUTED BY RANDOM"
	}

	if d.replicationNum != "" {
		sql += fmt.Sprintf("\nPROPERTIES (\"replication_num\" = \"%s\")", d.replicationNum)
	}
	return sql
}

func (d *StarRocksDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *StarRocksDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := d.checkCDCIncarnation(ctx, opts.Table, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	db, table := splitDatabaseTable(opts.Table)
	if db == "" {
		db = d.database
	}

	for result := range records {
		if result.Err != nil {
			return result.Err
		}
		batchNum++

		rows, err := d.loadRecord(ctx, db, table, result.Batch)
		result.Batch.Release()
		if err != nil {
			return fmt.Errorf("failed to stream load batch %d: %w", batchNum, err)
		}
		totalRows += rows
	}

	config.Debug("[STARROCKS] Wrote %d rows in %d batches in %v", totalRows, batchNum, time.Since(startTime))
	return d.checkCDCIncarnation(ctx, opts.Table, opts.CDCExpectedIncarnation)
}

// maxStreamLoadJSONBytes stays below StarRocks' default 100 MB JSON Stream
// Load limit (streaming_load_max_batch_size_mb).
var maxStreamLoadJSONBytes = 90 << 20

// loadRecord stream-loads a batch, splitting it until each JSON body fits.
func (d *StarRocksDestination) loadRecord(ctx context.Context, db, table string, rec arrow.RecordBatch) (int64, error) {
	body, rows, binaryCols, err := recordBatchToJSON(rec)
	if err != nil {
		return 0, fmt.Errorf("encode batch: %w", err)
	}
	if rows == 0 {
		return 0, nil
	}
	if len(body) > maxStreamLoadJSONBytes {
		if rows == 1 {
			return 0, fmt.Errorf("a single row encodes to %d bytes, above the %d-byte Stream Load limit", len(body), maxStreamLoadJSONBytes)
		}
		mid := rec.NumRows() / 2
		var loaded int64
		for _, bounds := range [][2]int64{{0, mid}, {mid, rec.NumRows()}} {
			part := rec.NewSlice(bounds[0], bounds[1])
			n, err := d.loadRecord(ctx, db, table, part)
			part.Release()
			loaded += n
			if err != nil {
				return loaded, err
			}
		}
		return loaded, nil
	}
	if err := d.loader.load(ctx, db, table, d.nextLabel(table), body, columnsHeader(rec.Schema(), binaryCols)); err != nil {
		return 0, err
	}
	return rows, nil
}

// nextLabel returns a unique load label. StarRocks labels must match
// ^[-\w]{1,128}$, so the table part keeps only ASCII word characters, is
// truncated, and is followed by a hash of the full name.
func (d *StarRocksDestination) nextLabel(table string) string {
	n := atomic.AddInt64(&d.labelCounter, 1)
	safe := []byte(table)
	for i, c := range safe {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			safe[i] = '_'
		}
	}
	if len(safe) > 48 {
		safe = safe[:48]
	}
	sum := sha1.Sum([]byte(table))
	return fmt.Sprintf("ingestr_%s_%s_%d_%d", safe, hex.EncodeToString(sum[:4]), time.Now().UnixNano(), n)
}

// SwapTable publishes a complete replacement. When the target already has the
// staging table's columns and keys, INSERT OVERWRITE replaces its data in
// place and keeps the table's grants and properties. Otherwise, and for
// CDC-fenced swaps, the staging table replaces the target via SWAP WITH or
// RENAME, which carries the new schema and keys. StarRocks swaps only within
// a database, so cross-database staging is first copied into the target
// database. Expected incarnations are checked immediately before and after
// the atomic statement.
func (d *StarRocksDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	copied, prepared := d.takePreparedSwap(opts.StagingTable)
	if prepared {
		defer d.dropQuietly(ctx, copied)
	}
	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if _, err := d.requireDatabase(opts.TargetTable); err != nil {
		return err
	}
	targetDB, targetName := d.tableParts(opts.TargetTable)
	stageDB, stageName := d.tableParts(opts.StagingTable)
	if _, err := d.db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+quoteColumn(targetDB)); err != nil {
		return fmt.Errorf("ensure StarRocks swap database: %w", err)
	}
	oldID, exists, err := d.tableID(ctx, targetDB, targetName)
	if err != nil {
		return err
	}
	if exists && opts.CDCExpectedIncarnation == "" && !prepared {
		same, err := d.sameTableLayout(ctx, opts.TargetTable, opts.StagingTable)
		if err != nil {
			return err
		}
		if same {
			return d.overwriteTable(ctx, opts)
		}
	}
	replacement := opts.StagingTable
	if stageDB != targetDB {
		if !prepared {
			if err := d.checkCDCIncarnation(ctx, opts.StagingTable, opts.CDCExpectedStagingIncarnation); err != nil {
				return err
			}
			if copied, err = d.copySwapStaging(ctx, opts.StagingTable, targetDB, targetName); err != nil {
				return err
			}
			defer d.dropQuietly(ctx, copied)
		}
		replacement = copied
		_, stageName = d.tableParts(copied)
	}
	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if replacement == opts.StagingTable || prepared {
		if err := d.checkCDCIncarnation(ctx, replacement, opts.CDCExpectedStagingIncarnation); err != nil {
			return err
		}
	}
	query := fmt.Sprintf("ALTER TABLE %s RENAME %s", d.tableRef(replacement), quoteColumn(targetName))
	if exists {
		query = fmt.Sprintf("ALTER TABLE %s SWAP WITH %s", d.tableRef(opts.TargetTable), quoteColumn(stageName))
	}
	if _, err := d.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("publish StarRocks replacement: %w", err)
	}
	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedResultIncarnation); err != nil {
		return fmt.Errorf("StarRocks CDC target changed during publication: %w", err)
	}
	if exists {
		// The old target now sits under the replacement's name. If it is not
		// the table that was validated, someone replaced the target just
		// before the swap: keep it for inspection instead of dropping it.
		swappedID, _, err := d.tableID(ctx, targetDB, stageName)
		if err != nil {
			return err
		}
		if swappedID != oldID {
			kept := targetDB + "." + stageName
			if stageDB != targetDB {
				d.forgetSwapCopy(copied)
			} else {
				// The caller drops its staging table on error; move the kept
				// table out of that name first.
				renamed := fmt.Sprintf("%s_replaced_%d", targetName, time.Now().UnixNano())
				if _, err := d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME %s", d.tableRef(kept), quoteColumn(renamed))); err == nil {
					kept = targetDB + "." + renamed
				}
			}
			return fmt.Errorf("StarRocks target %s was replaced concurrently; the previous table was kept as %s", opts.TargetTable, kept)
		}
	}
	if exists || stageDB != targetDB {
		return d.DropTable(ctx, opts.StagingTable)
	}
	return nil
}

// overwriteTable replaces the target's rows from staging in one atomic
// INSERT OVERWRITE, leaving the target table itself in place.
func (d *StarRocksDestination) overwriteTable(ctx context.Context, opts destination.SwapOptions) error {
	query := fmt.Sprintf("INSERT OVERWRITE %s SELECT * FROM %s", d.tableRef(opts.TargetTable), d.tableRef(opts.StagingTable))
	if opts.Schema != nil && len(opts.Schema.Columns) > 0 {
		list := strings.Join(quoteColumns(opts.Schema.ColumnNames()), ", ")
		query = fmt.Sprintf("INSERT OVERWRITE %s (%s) SELECT %s FROM %s", d.tableRef(opts.TargetTable), list, list, d.tableRef(opts.StagingTable))
	}
	if _, err := d.db.ExecContext(ctx, query); err != nil {
		config.LogFailedQuery(query, err)
		return fmt.Errorf("failed to overwrite target table: %w", err)
	}
	return d.DropTable(ctx, opts.StagingTable)
}

// sameTableLayout reports whether two tables have identical columns (names,
// types, nullability, key membership, order) and the same key model.
func (d *StarRocksDestination) sameTableLayout(ctx context.Context, a, b string) (bool, error) {
	layout := func(table string) (string, error) {
		db, name := d.tableParts(table)
		var model string
		if err := d.db.QueryRowContext(ctx, "SELECT TABLE_MODEL FROM information_schema.tables_config WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", db, name).Scan(&model); err != nil {
			return "", fmt.Errorf("read StarRocks table model: %w", err)
		}
		rows, err := d.db.QueryContext(ctx, "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", db, name)
		if err != nil {
			return "", fmt.Errorf("read StarRocks table columns: %w", err)
		}
		defer func() { _ = rows.Close() }()
		var out strings.Builder
		out.WriteString(model)
		for rows.Next() {
			var col, typ, nullable, key string
			if err := rows.Scan(&col, &typ, &nullable, &key); err != nil {
				return "", err
			}
			fmt.Fprintf(&out, "\x00%s\x01%s\x01%s\x01%s", strings.ToLower(col), strings.ToLower(typ), nullable, key)
		}
		return out.String(), rows.Err()
	}
	left, err := layout(a)
	if err != nil {
		return false, err
	}
	right, err := layout(b)
	if err != nil {
		return false, err
	}
	return left == right, nil
}

// copySwapStaging copies cross-database staging into a new table in the
// target database and returns its qualified name.
func (d *StarRocksDestination) copySwapStaging(ctx context.Context, stagingTable, targetDB, targetName string) (string, error) {
	sum := sha1.Sum([]byte(targetName))
	prefix := "__ingestr_swap_" + hex.EncodeToString(sum[:4]) + "_"
	d.dropStaleSwapTables(ctx, targetDB, prefix)
	copied := targetDB + "." + fmt.Sprintf("%s%d%04d", prefix, time.Now().UnixNano(), atomic.AddInt64(&d.labelCounter, 1)%10000)
	query := fmt.Sprintf("CREATE TABLE %s LIKE %s", d.tableRef(copied), d.tableRef(stagingTable))
	if _, err := d.db.ExecContext(ctx, query); err != nil {
		return "", fmt.Errorf("prepare StarRocks swap: %w", err)
	}
	if err := d.insertVisible(ctx, copied, "SELECT * FROM "+d.tableRef(stagingTable)); err != nil {
		d.dropQuietly(ctx, copied)
		return "", fmt.Errorf("copy StarRocks swap staging: %w", err)
	}
	return copied, nil
}

// dropStaleSwapTables drops swap tables left in the target database by runs
// killed mid-publication. Without the connector lease a concurrent run may own
// a swap table, so only ones older than ManagedStagingTTL are dropped.
func (d *StarRocksDestination) dropStaleSwapTables(ctx context.Context, db, prefix string) {
	leased := source.ConnectorLeaseGuardFromContext(ctx) != nil
	tables, err := d.ListStagingTables(ctx, db, prefix)
	if err != nil {
		config.Debug("[STARROCKS] Could not list stale swap tables in %s: %v", db, err)
		return
	}
	now := time.Now()
	for _, table := range tables {
		if !isStaleSwapSuffix(table[len(prefix):], leased, now) {
			continue
		}
		if err := d.DropTable(ctx, db+"."+table); err != nil {
			config.Debug("[STARROCKS] Could not drop stale swap table %s.%s: %v", db, table, err)
		}
	}
}

// isStaleSwapSuffix reports whether a swap table suffix (UnixNano followed by
// a 4-digit counter) belongs to a table that can be dropped.
func isStaleSwapSuffix(suffix string, leased bool, now time.Time) bool {
	if len(suffix) <= 4 {
		return false
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	if leased {
		return true
	}
	nanos, err := strconv.ParseInt(suffix[:len(suffix)-4], 10, 64)
	if err != nil {
		return false
	}
	return now.Sub(time.Unix(0, nanos)) > destination.ManagedStagingTTL
}

func (d *StarRocksDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if destination.HasCDCDeletedColumn(opts.Columns) {
		return d.mergeCDC(ctx, opts)
	}
	if err := d.validatePrimaryKey(ctx, "merge target", opts.TargetTable, opts.PrimaryKeys); err != nil {
		return err
	}
	cols := quoteColumns(opts.Columns)
	colList := strings.Join(cols, ", ")
	orderBy := ""
	if opts.IncrementalKey != "" {
		orderBy = quoteColumn(opts.IncrementalKey)
	} else if len(opts.PrimaryKeys) > 0 {
		// StarRocks rejects the helper's scalar-subquery fallback. A partition
		// key is constant within each group and likewise leaves ties unordered.
		orderBy = quoteColumn(opts.PrimaryKeys[0])
	}
	selectSQL := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), d.tableRef(opts.StagingTable), orderBy)
	// Select the winner before the PRIMARY KEY engine upserts; its replacement
	// order does not honor the incremental key.
	mergeSQL := fmt.Sprintf("INSERT INTO %s (%s) %s",
		d.tableRef(opts.TargetTable), colList, selectSQL)
	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to merge into table: %w", err)
	}
	return nil
}

func (d *StarRocksDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return fmt.Errorf("starrocks: delete+insert strategy is not supported")
}

func (d *StarRocksDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return fmt.Errorf("starrocks: scd2 strategy is not supported")
}

func (d *StarRocksDestination) DropTable(ctx context.Context, table string) error {
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", d.tableRef(table))
	if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	return nil
}

func (d *StarRocksDestination) Exec(ctx context.Context, query string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, query, args...)
	return err
}

func (d *StarRocksDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	// StarRocks has no general multi-statement DML transactions; statements run
	// directly (auto-committed). Only delete+insert needs a real transaction,
	// and that strategy is unsupported here.
	return &starRocksTransaction{db: d.db}, nil
}

type starRocksTransaction struct {
	db *sql.DB
}

func (t *starRocksTransaction) Exec(ctx context.Context, query string, args ...interface{}) error {
	_, err := t.db.ExecContext(ctx, query, args...)
	return err
}
func (t *starRocksTransaction) Commit(ctx context.Context) error   { return nil }
func (t *starRocksTransaction) Rollback(ctx context.Context) error { return nil }

func (d *StarRocksDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	db, tableName := splitDatabaseTable(table)
	if db == "" {
		db = d.database
	}

	query := `
		SELECT COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, IS_NULLABLE, NUMERIC_PRECISION, NUMERIC_SCALE, CHARACTER_MAXIMUM_LENGTH
		FROM information_schema.columns
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`

	rows, err := d.db.QueryContext(ctx, query, db, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType, columnType, isNullable string
		var numPrecision, numScale, charMaxLen *int
		if err := rows.Scan(&colName, &dataType, &columnType, &isNullable, &numPrecision, &numScale, &charMaxLen); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}
		if colName == syntheticSortKeyColumn {
			continue
		}
		col := schema.Column{
			Name:     colName,
			DataType: mapStarRocksColumnType(dataType, columnType),
			Nullable: strings.EqualFold(isNullable, "YES"),
		}
		if numPrecision != nil {
			col.Precision = *numPrecision
		}
		if numScale != nil {
			col.Scale = *numScale
		}
		if charMaxLen != nil {
			col.MaxLength = *charMaxLen
		}
		if strings.EqualFold(dataType, "array") {
			col.DataType = schema.TypeArray
			col.ArrayType, col.Precision, col.Scale = mapStarRocksArrayType(columnType)
		}
		columns = append(columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}
	if len(columns) == 0 {
		return nil, nil
	}
	keys, _, err := d.primaryKeys(ctx, table)
	if err != nil {
		return nil, err
	}
	for i := range columns {
		for _, key := range keys {
			if strings.EqualFold(columns[i].Name, key) {
				columns[i].IsPrimaryKey = true
			}
		}
	}
	return &schema.TableSchema{Name: tableName, Schema: db, Columns: columns, PrimaryKeys: keys}, nil
}

func (d *StarRocksDestination) SupportsReplaceStrategy() bool      { return true }
func (d *StarRocksDestination) SupportsAppendStrategy() bool       { return true }
func (d *StarRocksDestination) SupportsMergeStrategy() bool        { return true }
func (d *StarRocksDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *StarRocksDestination) SupportsSCD2Strategy() bool         { return false }
func (d *StarRocksDestination) SupportsAtomicSwap() bool           { return true }

var _ destination.Destination = (*StarRocksDestination)(nil)

// rejectBacktickName refuses database/table names with backticks: StarRocks
// drops escaped backticks, so DDL would hit a different table than the
// information_schema lookups.
func rejectBacktickName(db, name string) error {
	if strings.Contains(db, "`") || strings.Contains(name, "`") {
		return fmt.Errorf("StarRocks cannot use table %q: database and table names containing a backtick are not supported", db+"."+name)
	}
	return nil
}

func (d *StarRocksDestination) requireDatabase(table string) (string, error) {
	db, name := d.tableParts(table)
	if err := rejectBacktickName(db, name); err != nil {
		return "", err
	}
	if db == "" {
		return "", fmt.Errorf("StarRocks table %q has no database: add one to the destination URI path (starrocks://host:9030/<database>) or qualify the table as <database>.<table>", table)
	}
	return db, nil
}
