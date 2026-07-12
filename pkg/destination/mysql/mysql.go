package mysql

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/mysqluri"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	mysqldriver "github.com/go-sql-driver/mysql"
)

type MySQLDestination struct {
	db                  *sql.DB
	uri                 string
	database            string
	isVitess            bool
	vitessBackend       bool
	useLoadData         bool
	scheme              string
	lowerCaseTableNames int
}

const (
	mysqlInsertRowsPerStatement = 1000
)

var mysqlLoadDataReaderID uint64

func NewMySQLDestination() *MySQLDestination {
	return &MySQLDestination{
		useLoadData: true,
		scheme:      "mysql",
	}
}

func NewVitessCompatibleDestination(defaultScheme string) *MySQLDestination {
	return &MySQLDestination{
		isVitess:      true,
		vitessBackend: true,
		scheme:        defaultScheme,
	}
}

func (d *MySQLDestination) Schemes() []string {
	return []string{"mysql", "mysql+pymysql", "mariadb"}
}

func (d *MySQLDestination) Connect(ctx context.Context, uri string) error {
	dsn, database, err := uriToDSN(uri)
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

	var lowerCaseTableNames int
	if err := db.QueryRowContext(ctx, "SELECT @@lower_case_table_names").Scan(&lowerCaseTableNames); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to read MySQL lower_case_table_names: %w", err)
	}

	scheme := ""
	if u, err := mysqluri.ParseURL(uri); err == nil {
		scheme = u.Scheme
	}
	isVitessScheme := scheme == "vitess" || scheme == "ps_mysql"
	if d.vitessBackend && !isVitessScheme {
		_ = db.Close()
		return fmt.Errorf("Vitess/PlanetScale destination requires the vitess:// or ps_mysql:// scheme")
	}
	if !d.vitessBackend && isVitessScheme {
		_ = db.Close()
		return fmt.Errorf("regular MySQL destination does not accept the %s:// scheme", scheme)
	}

	if d.vitessBackend {
		config.Debug("[MYSQL] Vitess/PlanetScale destination (scheme=%s)", scheme)
		if sharded, err := isShardedKeyspace(ctx, db, database); err != nil {
			output.Warnf("[WARNING] could not verify whether Vitess/PlanetScale keyspace %q is sharded (%v); proceeding as unsharded, but a sharded keyspace will fail mid-load — ingestr supports only unsharded keyspaces as a destination\n", database, err)
		} else if sharded {
			_ = db.Close()
			return fmt.Errorf("keyspace %q is sharded; ingestr supports only unsharded (single-shard) Vitess/PlanetScale keyspaces as a destination", database)
		}
	} else if detectVitess(ctx, db) {
		_ = db.Close()
		return fmt.Errorf("server for keyspace %q identifies as Vitess/PlanetScale; use the vitess:// or ps_mysql:// scheme instead", database)
	}

	d.db = db
	d.uri = uri
	d.database = database
	d.isVitess = d.vitessBackend
	d.useLoadData = !d.vitessBackend
	d.lowerCaseTableNames = lowerCaseTableNames
	if scheme != "" {
		d.scheme = scheme
	}
	config.Debug("[MYSQL] Connected to database: %s", database)
	return nil
}

// detectVitess reports whether the server identifies as Vitess (this also covers
// PlanetScale, which is built on Vitess). On any probe error it returns false so
// the destination behaves as plain MySQL, matching the source dispatcher's fallback.
func detectVitess(ctx context.Context, db *sql.DB) bool {
	var version string
	if err := db.QueryRowContext(ctx, "SELECT @@version").Scan(&version); err != nil {
		config.Debug("[MYSQL] Vitess detection failed (assuming plain MySQL): %v", err)
		return false
	}
	return strings.Contains(strings.ToLower(version), "vitess")
}

// isShardedKeyspace reports whether the given Vitess keyspace has more than one
// shard. SHOW VITESS_SHARDS lists shards as "<keyspace>/<shard>"; an unsharded
// keyspace has exactly one shard (named "0" or "-").
func isShardedKeyspace(ctx context.Context, db *sql.DB, keyspace string) (bool, error) {
	if keyspace == "" {
		return false, nil
	}
	rows, err := db.QueryContext(ctx, "SHOW VITESS_SHARDS")
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	prefix := keyspace + "/"
	var count int
	for rows.Next() {
		var shard string
		if err := rows.Scan(&shard); err != nil {
			return false, err
		}
		if strings.HasPrefix(shard, prefix) {
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return count > 1, nil
}

// uriToDSN converts a MySQL-family URI to the DSN format expected by
// go-sql-driver/mysql, returning the DSN and the database name. The conversion
// lives in pkg/mysqluri, shared with the MySQL source.
func uriToDSN(uri string) (string, string, error) {
	return mysqluri.ToDSN(uri)
}

func (d *MySQLDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *MySQLDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := tablename.TwoLevel("mysql").CheckName(opts.Table); err != nil {
		return err
	}
	if database, _ := splitDatabaseTable(opts.Table); database != "" {
		if err := d.ensureDatabaseExists(ctx, database); err != nil {
			return err
		}
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTable(opts.Table))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[MYSQL] DROP TABLE took %v", time.Since(startDrop))
	}

	if opts.Schema != nil {
		startCreate := time.Now()
		createSQL := buildCreateTableSQL(opts.Table, opts.Schema.Columns, opts.PrimaryKeys)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
		config.Debug("[MYSQL] CREATE TABLE took %v", time.Since(startCreate))
	}

	return nil
}

func (d *MySQLDestination) ensureDatabaseExists(ctx context.Context, database string) error {
	if database == "" || database == d.database {
		return nil
	}
	// CREATE DATABASE IF NOT EXISTS still requires the global CREATE privilege
	// even when the database already exists. Check first so a pre-created DB
	// with table-level grants works without granting CREATE globally.
	var exists bool
	if err := d.db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = ?)",
		database).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check if database %s exists: %w", database, err)
	}
	if exists {
		return nil
	}
	// vtgate does not support CREATE DATABASE by default (it errors with "create
	// database not allowed", and does not ignore IF NOT EXISTS). Keyspaces must be
	// created out-of-band via the Vitess/PlanetScale control plane.
	if d.isVitess {
		return fmt.Errorf("keyspace %q does not exist; create it via your Vitess/PlanetScale control plane (CREATE DATABASE is not supported through vtgate)", database)
	}
	escaped := strings.ReplaceAll(database, "`", "``")
	createSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", escaped)
	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create database %s: %w", database, err)
	}
	config.Debug("[MYSQL] Ensured database exists: %s", database)
	return nil
}

func splitDatabaseTable(table string) (string, string) {
	parts := tablename.Split(table)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	if len(parts) == 1 {
		return "", parts[0]
	}
	return "", table
}

func (d *MySQLDestination) DropTable(ctx context.Context, table string) error {
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTable(table))
	_, err := d.db.ExecContext(ctx, dropSQL)
	if err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[MYSQL] Dropped table: %s", table)
	return nil
}

func (d *MySQLDestination) TruncateTable(ctx context.Context, table string) error {
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[MYSQL] Truncated table: %s", table)
	return nil
}

func (d *MySQLDestination) TruncateCDCTable(ctx context.Context, table string) error {
	deleteSQL := fmt.Sprintf("DELETE FROM %s", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, deleteSQL); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[MYSQL] Truncated table: %s", table)
	return nil
}

func (d *MySQLDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *MySQLDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeSequential(ctx, records, opts)
}

func (d *MySQLDestination) writeSequential(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[MYSQL] Starting sequential write to %s", opts.Table)

	for result := range records {
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			return result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		batchNum++
		startBatch := time.Now()
		rows, err := d.writeRecordBatch(ctx, record, opts.Table)
		record.Release()
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		rate := float64(rows) / time.Since(startBatch).Seconds()
		config.Debug("[MYSQL] Batch %d: %d rows in %v (%.0f rows/sec, total: %d)",
			batchNum, rows, time.Since(startBatch), rate, totalRows)
	}

	totalRate := float64(totalRows) / time.Since(startTime).Seconds()
	config.Debug("[MYSQL] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTime), totalRate)
	return nil
}

func (d *MySQLDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	if d.useLoadData {
		rows, err := d.writeRecordBatchLoadData(ctx, record, table)
		if err == nil {
			return rows, nil
		}
		if isLoadDataLocalDisabledError(err) {
			output.Warnf("[WARNING] MySQL LOAD DATA LOCAL INFILE is unavailable (%v); falling back to multi-row INSERT for this batch\n", err)
			return d.writeRecordBatchInsert(ctx, record, table)
		}
		return rows, err
	}
	return d.writeRecordBatchInsert(ctx, record, table)
}

func (d *MySQLDestination) writeRecordBatchInsert(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	numRows := record.NumRows()
	numCols := int(record.NumCols())

	if numRows == 0 {
		return 0, nil
	}

	colNames := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = quoteColumn(record.Schema().Field(i).Name)
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	rowsWritten := int64(0)
	for start := int64(0); start < numRows; start += mysqlInsertRowsPerStatement {
		if err := ctx.Err(); err != nil {
			_ = tx.Rollback()
			return rowsWritten, fmt.Errorf("write canceled before insert: %w", err)
		}

		end := min(start+mysqlInsertRowsPerStatement, numRows)
		chunkRows := int(end - start)
		insertSQL := buildMultiRowInsertSQL(table, colNames, chunkRows)

		values := make([]interface{}, 0, chunkRows*numCols)
		for rowIdx := start; rowIdx < end; rowIdx++ {
			for colIdx := 0; colIdx < numCols; colIdx++ {
				values = append(values, extractValue(record.Column(colIdx), int(rowIdx)))
			}
		}

		if _, err := tx.ExecContext(ctx, insertSQL, values...); err != nil {
			config.LogFailedQuery(insertSQL, err)
			_ = tx.Rollback()
			return rowsWritten, fmt.Errorf("failed to insert rows %d-%d: %w", start, end-1, err)
		}
		rowsWritten += int64(chunkRows)
	}

	if err := ctx.Err(); err != nil {
		_ = tx.Rollback()
		return rowsWritten, fmt.Errorf("write canceled before commit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return numRows, nil
}

func (d *MySQLDestination) writeRecordBatchLoadData(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	numRows := record.NumRows()
	numCols := int(record.NumCols())

	if numRows == 0 {
		return 0, nil
	}

	colNames := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = quoteColumn(record.Schema().Field(i).Name)
	}

	handlerName := fmt.Sprintf("ingestr_load_%d", atomic.AddUint64(&mysqlLoadDataReaderID, 1))
	mysqldriver.RegisterReaderHandler(handlerName, func() io.Reader {
		return newLoadDataRecordReader(record)
	})
	defer mysqldriver.DeregisterReaderHandler(handlerName)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	loadSQL := buildLoadDataSQL(table, colNames, handlerName)
	if _, err := tx.ExecContext(ctx, loadSQL); err != nil {
		config.LogFailedQuery(loadSQL, err)
		_ = tx.Rollback()
		return 0, fmt.Errorf("failed to load rows with LOAD DATA LOCAL INFILE: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("write canceled before commit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return numRows, nil
}

func buildMultiRowInsertSQL(table string, colNames []string, rows int) string {
	placeholders := make([]string, len(colNames))
	for i := range colNames {
		placeholders[i] = "?"
	}
	rowPlaceholder := "(" + strings.Join(placeholders, ", ") + ")"

	rowPlaceholders := make([]string, rows)
	for i := range rowPlaceholders {
		rowPlaceholders[i] = rowPlaceholder
	}

	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES %s",
		quoteTable(table),
		strings.Join(colNames, ", "),
		strings.Join(rowPlaceholders, ", "),
	)
}

func buildLoadDataSQL(table string, colNames []string, handlerName string) string {
	return fmt.Sprintf(
		"LOAD DATA LOCAL INFILE %s INTO TABLE %s FIELDS TERMINATED BY '\\t' ESCAPED BY '\\\\' LINES TERMINATED BY '\\n' (%s)",
		quoteStringLiteral("Reader::"+handlerName),
		quoteTable(table),
		strings.Join(colNames, ", "),
	)
}

func quoteStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func newLoadDataRecordReader(record arrow.RecordBatch) io.Reader {
	reader, writer := io.Pipe()
	go func() {
		buf := bufio.NewWriter(writer)
		err := writeRecordBatchTSV(buf, record)
		if flushErr := buf.Flush(); err == nil {
			err = flushErr
		}
		_ = writer.CloseWithError(err)
	}()
	return reader
}

func writeRecordBatchTSV(w io.Writer, record arrow.RecordBatch) error {
	numRows := record.NumRows()
	numCols := int(record.NumCols())

	for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
		for colIdx := 0; colIdx < numCols; colIdx++ {
			if colIdx > 0 {
				if _, err := io.WriteString(w, "\t"); err != nil {
					return err
				}
			}
			if err := writeLoadDataField(w, extractValue(record.Column(colIdx), int(rowIdx))); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return nil
}

func writeLoadDataField(w io.Writer, value interface{}) error {
	switch v := value.(type) {
	case nil:
		_, err := io.WriteString(w, `\N`)
		return err
	case []byte:
		return writeEscapedLoadDataBytes(w, v)
	case string:
		return writeEscapedLoadDataString(w, v)
	case int:
		_, err := io.WriteString(w, strconv.Itoa(v))
		return err
	case int8:
		_, err := io.WriteString(w, strconv.FormatInt(int64(v), 10))
		return err
	case int16:
		_, err := io.WriteString(w, strconv.FormatInt(int64(v), 10))
		return err
	case int32:
		_, err := io.WriteString(w, strconv.FormatInt(int64(v), 10))
		return err
	case int64:
		_, err := io.WriteString(w, strconv.FormatInt(v, 10))
		return err
	case uint:
		_, err := io.WriteString(w, strconv.FormatUint(uint64(v), 10))
		return err
	case uint8:
		_, err := io.WriteString(w, strconv.FormatUint(uint64(v), 10))
		return err
	case uint16:
		_, err := io.WriteString(w, strconv.FormatUint(uint64(v), 10))
		return err
	case uint32:
		_, err := io.WriteString(w, strconv.FormatUint(uint64(v), 10))
		return err
	case uint64:
		_, err := io.WriteString(w, strconv.FormatUint(v, 10))
		return err
	case float32:
		_, err := io.WriteString(w, strconv.FormatFloat(float64(v), 'g', -1, 32))
		return err
	case float64:
		_, err := io.WriteString(w, strconv.FormatFloat(v, 'g', -1, 64))
		return err
	case time.Time:
		return writeEscapedLoadDataString(w, v.Format("2006-01-02 15:04:05.000000"))
	default:
		return writeEscapedLoadDataString(w, fmt.Sprintf("%v", v))
	}
}

func writeEscapedLoadDataString(w io.Writer, value string) error {
	return writeEscapedLoadDataBytes(w, []byte(value))
}

func writeEscapedLoadDataBytes(w io.Writer, value []byte) error {
	for _, b := range value {
		var s string
		switch b {
		case 0:
			s = `\0`
		case '\t':
			s = `\t`
		case '\n':
			s = `\n`
		case '\r':
			s = `\r`
		case '\\':
			s = `\\`
		case 26:
			s = `\Z`
		default:
			if _, err := w.Write([]byte{b}); err != nil {
				return err
			}
			continue
		}
		if _, err := io.WriteString(w, s); err != nil {
			return err
		}
	}
	return nil
}

func isLoadDataLocalDisabledError(err error) bool {
	var myErr *mysqldriver.MySQLError
	if errors.As(err, &myErr) {
		switch myErr.Number {
		case 3948, 1148:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "loading local data is disabled") ||
		strings.Contains(msg, "used command is not allowed") ||
		strings.Contains(msg, "local infile") &&
			(strings.Contains(msg, "disabled") || strings.Contains(msg, "not allowed"))
}

func (d *MySQLDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()
	if err := tablename.TwoLevel("mysql").CheckName(opts.StagingTable); err != nil {
		return err
	}
	if err := tablename.TwoLevel("mysql").CheckName(opts.TargetTable); err != nil {
		return err
	}

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable

	targetDB, targetTableName := splitDatabaseTable(targetTable)
	if targetDB == "" {
		targetDB = d.database
	}
	stagingDB, stagingTableName := splitDatabaseTable(stagingTable)
	if stagingDB == "" {
		stagingDB = d.database
	}
	targetRef := quoteMySQLTable(targetDB, targetTableName)
	stagingRef := quoteMySQLTable(stagingDB, stagingTableName)

	// Replace only PrepareTables the staging side, so the target database may
	// not exist yet. RENAME TABLE doesn't auto-create databases.
	if err := d.ensureDatabaseExists(ctx, targetDB); err != nil {
		return fmt.Errorf("failed to ensure target database exists: %w", err)
	}

	oldNameCandidate := fmt.Sprintf("%s_old_%d", targetTableName, time.Now().UnixNano())
	oldTableName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("mysql"))
	oldRef := quoteMySQLTable(targetDB, oldTableName)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	var exists int
	err = tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = '%s' AND table_name = '%s'",
		strings.ReplaceAll(targetDB, "'", "''"),
		strings.ReplaceAll(targetTableName, "'", "''"),
	)).Scan(&exists)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to check if table exists: %w", err)
	}

	if exists > 0 {
		renameSQL := fmt.Sprintf("RENAME TABLE %s TO %s, %s TO %s",
			targetRef, oldRef,
			stagingRef, targetRef)
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to rename tables: %w", err)
		}

		dropOldSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", oldRef)
		if _, err := tx.ExecContext(ctx, dropOldSQL); err != nil {
			config.LogFailedQuery(dropOldSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to drop old table: %w", err)
		}
	} else {
		renameSQL := fmt.Sprintf("RENAME TABLE %s TO %s", stagingRef, targetRef)
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to rename staging table: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit swap: %w", err)
	}

	config.Debug("[MYSQL] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func quoteMySQLTable(database, table string) string {
	quotedTable := fmt.Sprintf("`%s`", strings.ReplaceAll(table, "`", "``"))
	if database == "" {
		return quotedTable
	}
	return fmt.Sprintf("`%s`.%s", strings.ReplaceAll(database, "`", "``"), quotedTable)
}

func (d *MySQLDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	columns := opts.Columns
	quotedColumns := quoteColumns(columns)
	targetColumns := destination.DestinationColumns(columns)
	quotedTargetColumns := quoteColumns(targetColumns)
	nonPKColumns := filterColumns(targetColumns, opts.PrimaryKeys)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build dedup subquery to handle duplicate PKs in staging. For CDC data the
	// latest change per PK wins (LSN strings are fixed-width and sort
	// lexicographically); otherwise, when an incremental key is set the latest
	// row per PK wins, else arbitrary.
	quotedPKs := quoteColumns(opts.PrimaryKeys)
	isCDC := destination.HasCDCDeletedColumn(columns)
	hasUnchangedCols := destination.HasCDCUnchangedColsColumn(columns)
	dedupOrderBy := "(SELECT NULL)"
	if isCDC {
		dedupOrderBy = destination.CDCLatestOverallOrderBy(quoteColumn)
	} else if opts.IncrementalKey != "" {
		dedupOrderBy = quoteColumns([]string{opts.IncrementalKey})[0] + " DESC"
	}
	dedupSource := func(where string) string {
		return fmt.Sprintf(
			`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s%s) AS _numbered WHERE __bruin_dedup_rn = 1) AS source`,
			strings.Join(quotedColumns, ", "),
			strings.Join(quotedColumns, ", "),
			strings.Join(quotedPKs, ", "),
			dedupOrderBy,
			quoteTable(opts.StagingTable),
			where,
		)
	}

	// For CDC, upsert from the latest non-deleted change per PK so a delete
	// followed by nothing doesn't clobber row data; deletes are applied below.
	upsertSource := dedupSource("")
	if isCDC {
		upsertSource = dedupSource(" WHERE `_cdc_deleted` = 0")
	}

	if len(nonPKColumns) > 0 {
		updateSet := buildUpdateSet(nonPKColumns, "target", "source")
		if isCDC && hasUnchangedCols {
			updateSet = buildCDCUpdateSet(nonPKColumns, "target", "source", "source."+quoteColumn(destination.CDCUnchangedColsColumn))
		}
		updateSQL := fmt.Sprintf(
			`UPDATE %s AS target INNER JOIN %s ON %s SET %s`,
			quoteTable(opts.TargetTable),
			upsertSource,
			buildJoinCondition(opts.PrimaryKeys, "target", "source"),
			updateSet,
		)
		config.Debug("[MERGE] Executing UPDATE: %s", updateSQL)

		if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
			config.LogFailedQuery(updateSQL, err)
			return fmt.Errorf("failed to update existing records: %w", err)
		}
	}

	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (%s) SELECT %s FROM %s WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)`,
		quoteTable(opts.TargetTable),
		strings.Join(quotedTargetColumns, ", "),
		strings.Join(quotedTargetColumns, ", "),
		upsertSource,
		quoteTable(opts.TargetTable),
		buildJoinCondition(opts.PrimaryKeys, "target", "source"),
	)
	config.Debug("[MERGE] Executing INSERT: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new records: %w", err)
	}

	if isCDC {
		// Mark rows deleted only when the latest change for the PK is a delete,
		// carrying the delete's LSN so resume picks up after it.
		markDeletedSQL := fmt.Sprintf(
			"UPDATE %s AS target INNER JOIN %s ON %s SET target.`_cdc_deleted` = 1, target.`_cdc_lsn` = source.`_cdc_lsn`, target.`_cdc_synced_at` = source.`_cdc_synced_at` WHERE source.`_cdc_deleted` = 1",
			quoteTable(opts.TargetTable),
			dedupSource(""),
			buildJoinCondition(opts.PrimaryKeys, "target", "source"),
		)
		config.Debug("[MERGE] Executing CDC delete marking: %s", markDeletedSQL)

		if _, err := tx.ExecContext(ctx, markDeletedSQL); err != nil {
			config.LogFailedQuery(markDeletedSQL, err)
			return fmt.Errorf("failed to mark deleted records: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *MySQLDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	quotedColumns := quoteColumns(opts.Columns)

	conn, err := d.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	releaseLock, err := acquireDeleteInsertLock(ctx, conn, opts.TargetTable)
	if err != nil {
		return err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := releaseLock(releaseCtx); err != nil {
			config.Debug("[DELETE+INSERT] Warning: failed to release target table lock: %v", err)
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	deleteSQL := fmt.Sprintf(
		"DELETE FROM %s WHERE %s >= ? AND %s <= ?",
		quoteTable(opts.TargetTable), quoteColumn(opts.IncrementalKey), quoteColumn(opts.IncrementalKey),
	)
	config.Debug("[DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if _, err := tx.ExecContext(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	colList := strings.Join(quotedColumns, ", ")
	// Dedupe staging by primary key, keeping the latest row per key by incremental key.
	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), quoteTable(opts.StagingTable), quoteColumns([]string{opts.IncrementalKey})[0])
	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) %s`, quoteTable(opts.TargetTable), colList, selectClause)
	config.Debug("[DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

func acquireDeleteInsertLock(ctx context.Context, conn *sql.Conn, targetTable string) (func(context.Context) error, error) {
	lockName := deleteInsertLockName(targetTable)
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 60)", lockName).Scan(&acquired); err != nil {
		return nil, fmt.Errorf("failed to acquire target table lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return nil, fmt.Errorf("timed out acquiring target table lock")
	}

	return func(ctx context.Context) error {
		var released sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lockName).Scan(&released); err != nil {
			return fmt.Errorf("failed to release target table lock: %w", err)
		}
		if !released.Valid || released.Int64 != 1 {
			return fmt.Errorf("target table lock was not released")
		}
		return nil
	}, nil
}

func deleteInsertLockName(targetTable string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(targetTable))
	return fmt.Sprintf("ingestr_di_%016x", h.Sum64())
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *MySQLDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditionsMySQL(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	updateSQL := fmt.Sprintf(
		`
		UPDATE %s AS target
		INNER JOIN %s AS source ON %s
		SET target._scd_valid_to = source._scd_valid_from,
		    target._scd_is_current = 0
		WHERE target._scd_is_current = 1
		  AND (%s)`,
		quoteTable(opts.TargetTable),
		quoteTable(opts.StagingTable),
		onCondition,
		changeConditions,
	)
	config.Debug("[MYSQL SCD2] Step 1 - Close changed records: %s", updateSQL)

	if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s AS target
			LEFT JOIN %s AS source ON %s
			SET target._scd_valid_to = ?,
			    target._scd_is_current = 0
			WHERE target._scd_is_current = 1
			  AND source.%s IS NULL`,
			quoteTable(opts.TargetTable),
			quoteTable(opts.StagingTable),
			onCondition,
			quoteColumn(opts.PrimaryKeys[0]),
		)
		config.Debug("[MYSQL SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if _, err := tx.ExecContext(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := quoteColumns(allColumns)

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s (%s)
		SELECT %s FROM %s AS source
		WHERE NOT EXISTS (
			SELECT 1 FROM %s AS target
			WHERE %s
			  AND target._scd_is_current = 1
		)`,
		quoteTable(opts.TargetTable),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quoteTable(opts.StagingTable),
		quoteTable(opts.TargetTable),
		onCondition,
	)
	config.Debug("[MYSQL SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[MYSQL SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *MySQLDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *MySQLDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &mysqlTransaction{tx: tx}, nil
}

type mysqlTransaction struct {
	tx *sql.Tx
}

func (t *mysqlTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *mysqlTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *mysqlTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

// ReplaceStagingPolicy governs where replace stages its intermediate table. Plain
// MySQL uses the managed _bruin_staging database (auto-created on demand). Vitess and
// PlanetScale cannot auto-create that keyspace (CREATE DATABASE is unsupported via
// vtgate), so staging goes into the target keyspace instead.
func (d *MySQLDestination) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	if d.isVitess {
		return destination.ReplaceStagingPolicy{
			DefaultPlacement:    destination.ReplaceStagingTargetSchema,
			DefaultTargetSchema: d.database,
		}
	}
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingManagedSchema,
		DefaultManagedSchema: "_bruin_staging",
	}
}

// ManagedStagingPolicy governs merge / delete-insert / scd2 staging placement; it
// follows the same rule as ReplaceStagingPolicy.
func (d *MySQLDestination) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return d.ReplaceStagingPolicy()
}

func (d *MySQLDestination) SupportsReplaceStrategy() bool      { return true }
func (d *MySQLDestination) SupportsAppendStrategy() bool       { return true }
func (d *MySQLDestination) SupportsMergeStrategy() bool        { return true }
func (d *MySQLDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *MySQLDestination) SupportsSCD2Strategy() bool         { return true }
func (d *MySQLDestination) SupportsAtomicSwap() bool           { return true }
func (d *MySQLDestination) SupportsCDCMerge() bool             { return true }
func (d *MySQLDestination) SupportsCDCUnchangedCols() bool     { return true }

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table for CDC resume.
func (d *MySQLDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	var maxLSN sql.NullString
	query := fmt.Sprintf("SELECT MAX(`_cdc_lsn`) FROM %s", quoteTable(table))
	err := d.db.QueryRowContext(ctx, query).Scan(&maxLSN)
	if err != nil {
		if isMySQLMissingTableError(err) {
			return "", nil
		}
		return "", err
	}
	if !maxLSN.Valid {
		return "", nil
	}
	return maxLSN.String, nil
}

func (d *MySQLDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	query := fmt.Sprintf("SELECT `event_id`, `source_table`, `destination_table`, `state_kind`, `state_generation`, `state_status`, `_cdc_lsn` FROM %s WHERE `connector_id` = ?", quoteTable(table))
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if isMySQLMissingTableError(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []destination.CDCStateEntry
	for rows.Next() {
		var entry destination.CDCStateEntry
		if err := rows.Scan(&entry.EventID, &entry.SourceTable, &entry.DestinationTable, &entry.StateKind, &entry.Generation, &entry.Status, &entry.Position); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (d *MySQLDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	database, tableName := splitDatabaseTable(claim.DestinationTable)
	if database == "" {
		database = d.database
	}
	canonicalTarget := canonicalMySQLTarget(database, tableName, d.lowerCaseTableNames)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	insert := fmt.Sprintf("INSERT INTO %s (`destination_table`, `connector_id`, `claimed_at`) VALUES (?, ?, CURRENT_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE `destination_table` = VALUES(`destination_table`)", quoteTable(claimTable))
	if _, err := tx.ExecContext(ctx, insert, canonicalTarget, ownerID); err != nil {
		return err
	}
	var owner string
	query := fmt.Sprintf("SELECT `connector_id` FROM %s WHERE `destination_table` = ?", quoteTable(claimTable))
	if err := tx.QueryRowContext(ctx, query, canonicalTarget).Scan(&owner); err != nil {
		return err
	}
	if owner != ownerID {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	return tx.Commit()
}

func (d *MySQLDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	return d.mysqlTargetIncarnation(ctx, d.db, table)
}

type mysqlIncarnationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (d *MySQLDestination) mysqlTargetIncarnation(ctx context.Context, queryer mysqlIncarnationQueryer, table string) (string, bool, error) {
	if d.isVitess {
		return "", false, errors.New("vitess and planetscale do not expose a durable physical table incarnation")
	}
	database, tableName := splitDatabaseTable(table)
	if database == "" {
		database = d.database
	}
	var engine string
	err := queryer.QueryRowContext(ctx, `SELECT ENGINE FROM information_schema.tables WHERE table_schema = ? AND table_name = ?`, database, tableName).Scan(&engine)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to inspect MySQL CDC target %s: %w", table, err)
	}
	if !strings.EqualFold(engine, "InnoDB") {
		return "", false, fmt.Errorf("MySQL CDC target %s uses %s, which has no supported durable table incarnation", table, engine)
	}

	if d.lowerCaseTableNames != 0 {
		database = strings.ToLower(database)
		tableName = strings.ToLower(tableName)
	}
	internalName := database + "/" + tableName
	var tableID uint64
	err = queryer.QueryRowContext(ctx, `SELECT TABLE_ID FROM information_schema.INNODB_TABLES WHERE NAME = ?`, internalName).Scan(&tableID)
	if isMySQLMissingInnoDBDictionaryTableError(err) {
		err = queryer.QueryRowContext(ctx, `SELECT TABLE_ID FROM information_schema.INNODB_SYS_TABLES WHERE NAME = ?`, internalName).Scan(&tableID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read durable InnoDB table identity for %s: %w", table, err)
	}
	return mysqlTableIncarnation(database, tableName, tableID), true, nil
}

func (d *MySQLDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return fmt.Errorf("cannot conditionally truncate MySQL table %q without a bound incarnation", table)
	}
	if d.isVitess {
		return errors.New("vitess and planetscale do not expose a durable physical table incarnation")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT 1 FROM %s LIMIT 1 FOR UPDATE", quoteTable(table)))
	if err != nil {
		return fmt.Errorf("failed to lock MySQL CDC target %s: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	current, exists, err := d.mysqlTargetIncarnation(ctx, tx, table)
	if err != nil {
		return err
	}
	if !exists || current != expectedIncarnation {
		return fmt.Errorf("MySQL CDC target %q physical incarnation changed", table)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", quoteTable(table))); err != nil {
		return fmt.Errorf("failed to truncate MySQL CDC target %s: %w", table, err)
	}
	return tx.Commit()
}

func mysqlTableIncarnation(database, table string, tableID uint64) string {
	return destination.CDCTargetKey(database, table, strconv.FormatUint(tableID, 10))
}

func (d *MySQLDestination) ValidateManagedCDCState() error {
	if d.isVitess {
		return errors.New("vitess and planetscale do not expose a durable physical table incarnation")
	}
	return nil
}

func (d *MySQLDestination) ValidateManagedCDCTarget(ctx context.Context, table string) error {
	if err := d.ValidateManagedCDCState(); err != nil {
		return err
	}
	if d.db == nil {
		return errors.New("MySQL destination is not connected")
	}
	_, _, err := d.mysqlTargetIncarnation(ctx, d.db, table)
	if err == nil {
		return nil
	}
	if isMySQLInnoDBDictionaryPermissionError(err) {
		return fmt.Errorf("managed CDC cannot read durable InnoDB table identity; grant the global MySQL PROCESS privilege if this server requires it: %w", err)
	}
	return err
}

func isMySQLMissingInnoDBDictionaryTableError(err error) bool {
	if err == nil {
		return false
	}
	var myErr *mysqldriver.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == 1109 || myErr.Number == 1146
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown table") || strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "does not exist")
}

func isMySQLInnoDBDictionaryPermissionError(err error) bool {
	var myErr *mysqldriver.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == 1044 || myErr.Number == 1142 || myErr.Number == 1227
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access denied") || strings.Contains(msg, "command denied") || strings.Contains(msg, "process privilege")
}

func canonicalMySQLTarget(database, table string, lowerCaseTableNames int) string {
	if lowerCaseTableNames != 0 {
		database = strings.ToLower(database)
		table = strings.ToLower(table)
	}
	return destination.CDCTargetKey(database, table)
}

func (d *MySQLDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	database, tableName := splitDatabaseTable(table)
	if database == "" {
		database = d.database
	}
	return canonicalMySQLTarget(database, tableName, d.lowerCaseTableNames), nil
}

func (d *MySQLDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	quotedTable := quoteTable(table)
	query := fmt.Sprintf("SELECT DISTINCT `event_id`, `state_generation` FROM %s WHERE `connector_id` = ? AND `state_kind` = 'run' AND `state_generation` = (SELECT MAX(`state_generation`) FROM %s WHERE `connector_id` = ? AND `state_kind` = 'run') ORDER BY `event_id`", quotedTable, quotedTable)
	rows, err := d.db.QueryContext(ctx, query, connectorID, connectorID)
	if err != nil {
		if isMySQLMissingTableError(err) {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, err
	}
	defer func() { _ = rows.Close() }()

	var fence destination.CDCStateFence
	for rows.Next() {
		var eventID string
		var generation int64
		if err := rows.Scan(&eventID, &generation); err != nil {
			return destination.CDCStateFence{}, err
		}
		fence.Generation = generation
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	return fence, rows.Err()
}

func (d *MySQLDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, connectorID)
	placeholders := make([]string, len(eventIDs))
	for i, eventID := range eventIDs {
		placeholders[i] = "?"
		args = append(args, eventID)
	}
	_, err := d.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE `connector_id` = ? AND `event_id` IN (%s)", quoteTable(table), strings.Join(placeholders, ", ")), args...)
	return err
}

// isMySQLMissingTableError reports whether the queried table or its database
// does not exist. Managed CDC reads its state before PrepareTable creates the
// _bruin_staging database on a first run.
func isMySQLMissingTableError(err error) bool {
	var myErr *mysqldriver.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == 1146 || myErr.Number == 1051 || myErr.Number == 1049
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "does not exist") || strings.Contains(msg, "unknown database")
}

func (d *MySQLDestination) GetScheme() string {
	if d.scheme != "" {
		return d.scheme
	}
	return "mysql"
}

func (d *MySQLDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	database, tableName := splitDatabaseTable(table)
	if database == "" {
		database = d.database
	}

	query := `
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE,
		       NUMERIC_PRECISION, NUMERIC_SCALE, CHARACTER_MAXIMUM_LENGTH,
		       COLUMN_TYPE
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ` + mysqlSchemaFilterExpr(database) + ` AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`

	rows, err := d.db.QueryContext(ctx, query, mysqlSchemaFilterArgs(database, tableName)...)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var colName, dataType, isNullable, columnType string
		var numPrecision, numScale, charMaxLen *int

		if err := rows.Scan(&colName, &dataType, &isNullable, &numPrecision, &numScale, &charMaxLen, &columnType); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		col := schema.Column{
			Name:     colName,
			DataType: mapMySQLTypeToSchema(dataType, columnType),
			Nullable: isNullable == "YES",
		}

		if numPrecision != nil {
			col.Precision = *numPrecision
		}
		if numScale != nil {
			col.Scale = *numScale
		}
		if charMaxLen != nil && !isMySQLTextFamily(dataType) {
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

	return &schema.TableSchema{
		Name:    tableName,
		Schema:  database,
		Columns: columns,
	}, nil
}

// mysqlSchemaFilterExpr and mysqlSchemaFilterArgs build the TABLE_SCHEMA filter
// for information_schema lookups. Destination tables can be qualified with a
// database other than the connection default (e.g. multi-table CDC with
// dest_schema), so the qualifier must win over DATABASE().
func mysqlSchemaFilterExpr(database string) string {
	if database == "" {
		return "DATABASE()"
	}
	return "?"
}

func mysqlSchemaFilterArgs(database, tableName string) []interface{} {
	if database == "" {
		return []interface{}{tableName}
	}
	return []interface{}{database, tableName}
}

// isMySQLTextFamily reports whether dataType is a TEXT-family column.
// Their character_maximum_length is the type's intrinsic engine cap, not a
// user constraint. So must not roundtrip back into CREATE TABLE as VARCHAR(N).
func isMySQLTextFamily(dataType string) bool {
	switch strings.ToLower(dataType) {
	case "text", "tinytext", "mediumtext", "longtext":
		return true
	}
	return false
}

func mapMySQLTypeToSchema(dataType, columnType string) schema.DataType {
	dataType = strings.ToLower(dataType)
	columnType = strings.ToLower(columnType)

	switch dataType {
	case "tinyint":
		if strings.Contains(columnType, "tinyint(1)") {
			return schema.TypeBoolean
		}
		return schema.TypeInt16
	case "smallint":
		return schema.TypeInt16
	case "mediumint", "int", "integer":
		return schema.TypeInt32
	case "bigint":
		return schema.TypeInt64
	case "float":
		return schema.TypeFloat32
	case "double", "real":
		return schema.TypeFloat64
	case "decimal", "numeric":
		return schema.TypeDecimal
	case "char", "varchar", "text", "tinytext", "mediumtext", "longtext":
		return schema.TypeString
	case "binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob":
		return schema.TypeBinary
	case "date":
		return schema.TypeDate
	case "time":
		return schema.TypeTime
	case "datetime":
		return schema.TypeTimestamp
	case "timestamp":
		return schema.TypeTimestampTZ
	case "json":
		return schema.TypeJSON
	default:
		return schema.TypeString
	}
}

func quoteTable(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("`%s`.`%s`", strings.ReplaceAll(parts[0], "`", "``"), strings.ReplaceAll(parts[1], "`", "``"))
	}
	return fmt.Sprintf("`%s`", strings.ReplaceAll(table, "`", "``"))
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = fmt.Sprintf("`%s`", strings.ReplaceAll(col, "`", "``"))
	}
	return quoted
}

func filterColumns(columns []string, exclude []string) []string {
	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToLower(col)] = true
	}

	var result []string
	for _, col := range columns {
		if !excludeMap[strings.ToLower(col)] {
			result = append(result, col)
		}
	}
	return result
}

func buildJoinCondition(keys []string, targetAlias, sourceAlias string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf("%s.%s = %s.%s", targetAlias, quoteColumn(key), sourceAlias, quoteColumn(key))
	}
	return strings.Join(conditions, " AND ")
}

func buildUpdateSet(columns []string, targetAlias, sourceAlias string) string {
	sets := make([]string, len(columns))
	for i, col := range columns {
		sets[i] = fmt.Sprintf("%s.%s = %s.%s", targetAlias, quoteColumn(col), sourceAlias, quoteColumn(col))
	}
	return strings.Join(sets, ", ")
}

func buildCDCUpdateSet(columns []string, targetAlias, sourceAlias, unchangedRef string) string {
	sets := make([]string, len(columns))
	for i, col := range columns {
		target := targetAlias + "." + quoteColumn(col)
		source := sourceAlias + "." + quoteColumn(col)
		if destination.IsCDCColumn(col) {
			sets[i] = target + " = " + source
			continue
		}
		unchanged := fmt.Sprintf(
			"JSON_CONTAINS(COALESCE(%s, '[]'), JSON_QUOTE(%s))",
			unchangedRef,
			mysqlUTF8Expression(col),
		)
		sets[i] = fmt.Sprintf("%s = CASE WHEN %s THEN %s ELSE %s END", target, unchanged, target, source)
	}
	return strings.Join(sets, ", ")
}

func mysqlUTF8Expression(value string) string {
	return "CONVERT(0x" + hex.EncodeToString([]byte(value)) + " USING utf8mb4)"
}

func quoteColumn(col string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(col, "`", "``"))
}

// buildChangeConditionsMySQL builds change detection conditions using COALESCE for NULL handling.
func buildChangeConditionsMySQL(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "0"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		// MySQL doesn't have IS DISTINCT FROM, use COALESCE or NULL-safe comparison
		conditions[i] = fmt.Sprintf(`NOT (%s.%s <=> %s.%s)`, targetAlias, quoteColumn(col), sourceAlias, quoteColumn(col))
	}
	return strings.Join(conditions, " OR ")
}

func extractTableName(table string) string {
	parts := strings.Split(table, ".")
	return parts[len(parts)-1]
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	binaryClaimKey := isCDCTargetClaimTable(columns, primaryKeys)
	for _, col := range columns {
		colType := MapDataTypeToMySQL(col)
		if binaryClaimKey && col.Name == "destination_table" {
			colType += " CHARACTER SET ascii COLLATE ascii_bin"
		}
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteColumn(col.Name), colType))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s", quoteTable(table), strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = quoteColumn(k)
		}
		sql += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(quotedKeys, ", "))
	}

	sql += "\n)"
	return sql
}

func isCDCTargetClaimTable(columns []schema.Column, primaryKeys []string) bool {
	if len(columns) != 3 || len(primaryKeys) != 1 || primaryKeys[0] != "destination_table" {
		return false
	}
	want := map[string]bool{"destination_table": false, "connector_id": false, "claimed_at": false}
	for _, col := range columns {
		if _, ok := want[col.Name]; !ok {
			return false
		}
		want[col.Name] = true
	}
	return want["destination_table"] && want["connector_id"] && want["claimed_at"]
}

func extractValue(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return nil
	}

	switch a := arr.(type) {
	case *array.Boolean:
		if a.Value(idx) {
			return 1
		}
		return 0
	case *array.Int8:
		return a.Value(idx)
	case *array.Int16:
		return a.Value(idx)
	case *array.Int32:
		return a.Value(idx)
	case *array.Int64:
		return a.Value(idx)
	case *array.Float32:
		return a.Value(idx)
	case *array.Float64:
		return a.Value(idx)
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		return a.Value(idx)
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Time64:
		micros := int64(a.Value(idx))
		t := time.Duration(micros) * time.Microsecond
		hours := int(t.Hours())
		mins := int(t.Minutes()) % 60
		secs := int(t.Seconds()) % 60
		micros = micros % 1000000
		return fmt.Sprintf("%02d:%02d:%02d.%06d", hours, mins, secs, micros)
	case *array.Timestamp:
		ts := a.Value(idx)
		return ts.ToTime(a.DataType().(*arrow.TimestampType).Unit).Format("2006-01-02 15:04:05.000000")
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case array.ExtensionArray:
		storage := a.Storage()
		return extractValue(storage, idx)
	default:
		return fmt.Sprintf("%v", arr)
	}
}
