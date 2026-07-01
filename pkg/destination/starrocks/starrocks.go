package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

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
	db             *sql.DB
	loader         *streamLoader
	database       string
	replicationNum string // empty => use the cluster default
	labelCounter   int64
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
		if p, convErr := strconv.Atoi(v); convErr == nil {
			httpPort = p
		}
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
	dsn += fmt.Sprintf("tcp(%s:%s)/?parseTime=true", host, port)
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
	d.loader = newStreamLoader(host, httpPort, user, password)
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
	if err := tablename.TwoLevel("starrocks").CheckName(opts.Table); err != nil {
		return err
	}
	if db, _ := splitDatabaseTable(opts.Table); db != "" {
		createDB := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", quoteColumn(db))
		if _, err := d.db.ExecContext(ctx, createDB); err != nil {
			config.LogFailedQuery(createDB, err)
			return fmt.Errorf("failed to ensure database %s: %w", db, err)
		}
	}

	if opts.DropFirst {
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTable(opts.Table))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
	}

	if opts.Schema != nil {
		createSQL := d.buildCreateTableSQL(opts.Table, opts.Schema.Columns, opts.PrimaryKeys)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
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

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)", quoteTable(table), strings.Join(colDefs, ",\n  "))

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

		body, rows, err := recordBatchToJSON(result.Batch)
		if err != nil {
			result.Batch.Release()
			return fmt.Errorf("failed to encode batch %d: %w", batchNum, err)
		}
		if rows > 0 {
			label := d.nextLabel(table)
			if err := d.loader.load(ctx, db, table, label, body); err != nil {
				result.Batch.Release()
				return fmt.Errorf("failed to stream load batch %d: %w", batchNum, err)
			}
			totalRows += rows
		}
		result.Batch.Release()
	}

	config.Debug("[STARROCKS] Wrote %d rows in %d batches in %v", totalRows, batchNum, time.Since(startTime))
	return nil
}

func (d *StarRocksDestination) nextLabel(table string) string {
	n := atomic.AddInt64(&d.labelCounter, 1)
	safe := strings.NewReplacer(".", "_", "`", "", "-", "_").Replace(table)
	return fmt.Sprintf("ingestr_%s_%d_%d", safe, time.Now().UnixNano(), n)
}

// SwapTable atomically replaces the target's data from staging. StarRocks
// SWAP WITH / RENAME are single-database only (and ingestr stages into a
// separate database), so instead it uses INSERT OVERWRITE: StarRocks loads into
// temporary partitions and swaps them in only on success, leaving the target's
// existing data intact if the load fails.
func (d *StarRocksDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	if db, _ := splitDatabaseTable(opts.TargetTable); db != "" {
		createDB := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", quoteColumn(db))
		if _, err := d.db.ExecContext(ctx, createDB); err != nil {
			config.LogFailedQuery(createDB, err)
			return fmt.Errorf("failed to ensure target database: %w", err)
		}
	}
	if opts.Schema != nil {
		createSQL := d.buildCreateTableSQL(opts.TargetTable, opts.Schema.Columns, opts.PrimaryKeys)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create target table: %w", err)
		}
	}

	overwriteSQL := fmt.Sprintf("INSERT OVERWRITE %s SELECT * FROM %s",
		quoteTable(opts.TargetTable), quoteTable(opts.StagingTable))
	if opts.Schema != nil && len(opts.Schema.Columns) > 0 {
		cols := make([]string, len(opts.Schema.Columns))
		for i, c := range opts.Schema.Columns {
			cols[i] = quoteColumn(c.Name)
		}
		list := strings.Join(cols, ", ")
		overwriteSQL = fmt.Sprintf("INSERT OVERWRITE %s (%s) SELECT %s FROM %s",
			quoteTable(opts.TargetTable), list, list, quoteTable(opts.StagingTable))
	}
	if _, err := d.db.ExecContext(ctx, overwriteSQL); err != nil {
		config.LogFailedQuery(overwriteSQL, err)
		return fmt.Errorf("failed to overwrite target table: %w", err)
	}
	return d.DropTable(ctx, opts.StagingTable)
}

func (d *StarRocksDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	cols := quoteColumns(opts.Columns)
	colList := strings.Join(cols, ", ")
	// The target is a PRIMARY KEY table, so INSERT upserts on the key.
	mergeSQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
		quoteTable(opts.TargetTable), colList, colList, quoteTable(opts.StagingTable))
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
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTable(table))
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
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, NUMERIC_PRECISION, NUMERIC_SCALE, CHARACTER_MAXIMUM_LENGTH
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
		var colName, dataType, isNullable string
		var numPrecision, numScale, charMaxLen *int
		if err := rows.Scan(&colName, &dataType, &isNullable, &numPrecision, &numScale, &charMaxLen); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}
		col := schema.Column{
			Name:     colName,
			DataType: mapStarRocksTypeToSchema(dataType),
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
		columns = append(columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}
	if len(columns) == 0 {
		return nil, nil
	}
	return &schema.TableSchema{Name: tableName, Schema: db, Columns: columns}, nil
}

func (d *StarRocksDestination) SupportsReplaceStrategy() bool      { return true }
func (d *StarRocksDestination) SupportsAppendStrategy() bool       { return true }
func (d *StarRocksDestination) SupportsMergeStrategy() bool        { return true }
func (d *StarRocksDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *StarRocksDestination) SupportsSCD2Strategy() bool         { return false }
func (d *StarRocksDestination) SupportsAtomicSwap() bool           { return true }

var _ destination.Destination = (*StarRocksDestination)(nil)
