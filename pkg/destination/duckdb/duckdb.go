package duckdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-adbc/go/adbc/drivermgr"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	srcduckdb "github.com/bruin-data/ingestr/pkg/source/duckdb"
)

type DuckDBDestination struct {
	filePath string
	mu       sync.Mutex

	db   adbc.Database
	conn adbc.Connection

	// schemas captures the schema each prepared table was created with, keyed by the
	// fully-qualified opts.Table name. SwapTable's cross-schema branch reads this to
	// recreate the target with full constraints (NOT NULL, PK) instead of losing them
	// via plain CTAS. Per-key writes mean parallel PrepareTable calls in multi-table
	// runs don't clobber each other.
	schemas   map[string]*schema.TableSchema
	schemasMu sync.Mutex
}

func NewDuckDBDestination() *DuckDBDestination {
	return &DuckDBDestination{}
}

func (d *DuckDBDestination) recordSchema(table string, sch *schema.TableSchema, pks []string) {
	if sch == nil {
		return
	}
	clone := *sch
	if len(pks) > 0 {
		clone.PrimaryKeys = pks
	}
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	if d.schemas == nil {
		d.schemas = map[string]*schema.TableSchema{}
	}
	d.schemas[table] = &clone
}

func (d *DuckDBDestination) lookupSchema(table string) *schema.TableSchema {
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	return d.schemas[table]
}

func (d *DuckDBDestination) forgetSchema(table string) {
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	delete(d.schemas, table)
}

func (d *DuckDBDestination) Schemes() []string {
	return []string{"duckdb", "motherduck", "md"}
}

func (d *DuckDBDestination) Connect(ctx context.Context, uri string) error {
	path, err := parseDuckDBPath(uri)
	if err != nil {
		return fmt.Errorf("failed to parse DuckDB URI: %w", err)
	}

	d.filePath = path
	config.Debug("[DUCKDB] Destination path: %s", d.filePath)

	isMotherDuck := strings.HasPrefix(d.filePath, "md:")
	if !isMotherDuck && d.filePath != ":memory:" {
		dir := filepath.Dir(d.filePath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		}
	}

	dialect := srcduckdb.NewDialect()
	if err := dialect.EnsureDriver(ctx); err != nil {
		return fmt.Errorf("failed to ensure DuckDB ADBC driver: %w", err)
	}

	db, err := (drivermgr.Driver{}).NewDatabaseWithContext(ctx, map[string]string{
		"driver": "duckdb",
		"path":   d.filePath,
	})
	if err != nil {
		return fmt.Errorf("failed to create DuckDB ADBC database: %w", err)
	}
	conn, err := db.Open(ctx)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to open DuckDB ADBC connection: %w", err)
	}

	d.db = db
	d.conn = conn

	// Ensure changes are committed/visible across connections by default.
	if opt, ok := conn.(adbc.PostInitOptions); ok {
		_ = opt.SetOption(adbc.OptionKeyAutoCommit, adbc.OptionValueEnabled)
	}

	if limit := os.Getenv("INGESTR_DUCKDB_MEMORY_LIMIT"); limit != "" {
		if strings.ContainsAny(limit, "';\n") {
			config.Debug("[DUCKDB] Ignoring invalid INGESTR_DUCKDB_MEMORY_LIMIT=%q", limit)
		} else if err := d.exec(ctx, fmt.Sprintf("SET memory_limit='%s'", limit)); err != nil {
			config.Debug("[DUCKDB] Failed to set memory_limit=%s: %v", limit, err)
		}
	}

	// Simple sanity check
	if err := d.exec(ctx, "SELECT 1"); err != nil {
		_ = d.conn.Close()
		_ = d.db.Close()
		d.conn = nil
		d.db = nil
		return fmt.Errorf("failed to validate DuckDB ADBC connection: %w", err)
	}

	return nil
}

func (d *DuckDBDestination) Close(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var err error
	if d.conn != nil {
		err = errorsJoin(err, d.conn.Close())
		d.conn = nil
	}
	if d.db != nil {
		err = errorsJoin(err, d.db.Close())
		d.db = nil
	}
	return err
}

func (d *DuckDBDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return fmt.Errorf("schema is required")
	}

	d.recordSchema(opts.Table, opts.Schema, opts.PrimaryKeys)

	d.mu.Lock()
	defer d.mu.Unlock()

	schemaName, _ := parseSchemaTable(opts.Table)
	if err := d.ensureSchemaExistsLocked(ctx, schemaName); err != nil {
		return fmt.Errorf("failed to ensure schema exists: %w", err)
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(opts.Table))
		if err := d.exec(ctx, dropSQL); err != nil {
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[DUCKDB] DROP TABLE took %v", time.Since(startDrop))
	}

	startCreate := time.Now()
	createSQL := buildCreateTableSQL(destination.QuoteTableName(opts.Table), opts.Schema.Columns, opts.PrimaryKeys)
	config.Debug("[DUCKDB] CREATE SQL: %s", createSQL)
	if err := d.exec(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	config.Debug("[DUCKDB] CREATE TABLE took %v", time.Since(startCreate))

	return nil
}

func (d *DuckDBDestination) ensureSchemaExists(ctx context.Context, schemaName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ensureSchemaExistsLocked(ctx, schemaName)
}

// ensureSchemaExistsLocked assumes the mutex is already held.
func (d *DuckDBDestination) ensureSchemaExistsLocked(ctx context.Context, schemaName string) error {
	if schemaName == "" || schemaName == "main" {
		return nil
	}

	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", destination.QuoteIdentifier(schemaName))
	if err := d.exec(ctx, createSchemaSQL); err != nil {
		return fmt.Errorf("failed to create schema %s: %w", schemaName, err)
	}
	config.Debug("[DUCKDB] Ensured schema exists: %s", schemaName)
	return nil
}

func (d *DuckDBDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeViaADBCIngest(ctx, records, opts)
}

func (d *DuckDBDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeViaADBCIngest(ctx, records, opts)
}

func (d *DuckDBDestination) writeViaADBCIngest(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	config.Debug("[DUCKDB] Starting write to %s", opts.Table)
	startTotal := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	schemaName, tableName := parseSchemaTable(opts.Table)
	ingestOpts := adbc.IngestStreamOptions{}
	if schemaName != "" {
		ingestOpts.DBSchema = schemaName
	}

	// Optional periodic CHECKPOINT to bound DuckDB's WAL/buffer pool growth
	// during large ingests. Off by default. Set INGESTR_DUCKDB_CHECKPOINT_ROWS=<n>
	// to checkpoint after every n rows (-1 = after every batch).
	var checkpointEvery int64
	if v := os.Getenv("INGESTR_DUCKDB_CHECKPOINT_ROWS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			checkpointEvery = n
		} else {
			config.Debug("[DUCKDB] Invalid INGESTR_DUCKDB_CHECKPOINT_ROWS=%q, checkpointing disabled: %v", v, err)
		}
	}

	var totalRows int64
	var rowsSinceCheckpoint int64
	for res := range records {
		if res.Err != nil {
			return res.Err
		}
		if res.Batch == nil {
			continue
		}
		if res.Batch.NumRows() == 0 {
			res.Batch.Release()
			continue
		}

		// Use standard Arrow RecordReader for each batch
		reader, readerErr := array.NewRecordReader(res.Batch.Schema(), []arrow.RecordBatch{res.Batch})
		if readerErr != nil {
			res.Batch.Release()
			return fmt.Errorf("failed to create record reader: %w", readerErr)
		}

		_, ingestErr := adbc.IngestStream(ctx, d.conn, reader, tableName, adbc.OptionValueIngestModeAppend, ingestOpts)
		reader.Release()

		if ingestErr != nil {
			config.Debug("[DUCKDB] IngestStream error: %v", ingestErr)
			res.Batch.Release()
			return fmt.Errorf("failed to ingest batch: %w", ingestErr)
		}

		totalRows += res.Batch.NumRows()
		rowsSinceCheckpoint += res.Batch.NumRows()
		res.Batch.Release()

		shouldCheckpoint := checkpointEvery == -1 ||
			(checkpointEvery > 0 && rowsSinceCheckpoint >= checkpointEvery)
		if shouldCheckpoint {
			if err := d.exec(ctx, "CHECKPOINT"); err != nil {
				config.Debug("[DUCKDB] CHECKPOINT failed: %v", err)
			}
			rowsSinceCheckpoint = 0
		}
	}

	totalRate := float64(totalRows) / time.Since(startTotal).Seconds()
	config.Debug("[DUCKDB] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), totalRate)
	return nil
}

func (d *DuckDBDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable
	targetSchema, targetName := parseSchemaTable(targetTable)
	stagingSchema, _ := parseSchemaTable(stagingTable)

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()

	if stagingSchema == targetSchema {
		// Same schema: cheap rename swap.
		oldNameCandidate := fmt.Sprintf("%s_old_%d", targetName, time.Now().UnixNano())
		oldName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("duckdb"))
		oldTable := oldName
		if stagingSchema != "" {
			oldTable = stagingSchema + "." + oldName
		}

		if err := d.exec(ctx, fmt.Sprintf("ALTER TABLE IF EXISTS %s RENAME TO %s", destination.QuoteTableName(targetTable), destination.QuoteIdentifier(oldName))); err != nil {
			config.Debug("[DUCKDB] No existing table to rename (this is OK for first run)")
		}

		if err := d.exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", destination.QuoteTableName(stagingTable), destination.QuoteIdentifier(targetName))); err != nil {
			return fmt.Errorf("failed to rename staging to target: %w", err)
		}

		_ = d.exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(oldTable)))
	} else {
		// Cross-schema swap: DuckDB's ALTER TABLE RENAME doesn't support cross-schema.
		// Recreate the target with the staging table's recorded schema (preserving
		// NOT NULL / PK constraints) and copy rows. The schema is looked up by the
		// staging table name so parallel multi-table PrepareTable calls don't race.
		sch := d.lookupSchema(stagingTable)
		if sch == nil {
			return fmt.Errorf("cannot swap %s -> %s: no recorded schema for staging table", stagingTable, targetTable)
		}

		// Replace only PrepareTables the staging side, so the target schema may
		// not exist yet (DuckDB doesn't auto-create "public" for fresh DBs).
		if err := d.ensureSchemaExistsLocked(ctx, targetSchema); err != nil {
			return fmt.Errorf("failed to ensure target schema exists: %w", err)
		}

		if err := d.exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(targetTable))); err != nil {
			return fmt.Errorf("failed to drop target table: %w", err)
		}

		createSQL := buildCreateTableSQL(destination.QuoteTableName(targetTable), sch.Columns, sch.PrimaryKeys)
		if err := d.exec(ctx, createSQL); err != nil {
			return fmt.Errorf("failed to recreate target table: %w", err)
		}

		quotedCols := make([]string, len(sch.Columns))
		for i, c := range sch.Columns {
			quotedCols[i] = destination.QuoteIdentifier(c.Name)
		}
		colList := strings.Join(quotedCols, ", ")
		copySQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
			destination.QuoteTableName(targetTable),
			colList, colList,
			destination.QuoteTableName(stagingTable))
		if err := d.exec(ctx, copySQL); err != nil {
			return fmt.Errorf("failed to copy staging rows into target: %w", err)
		}

		if err := d.exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(stagingTable))); err != nil {
			return fmt.Errorf("failed to drop staging table: %w", err)
		}
		d.forgetSchema(stagingTable)
	}

	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit swap: %w", err)
	}
	commit = true

	config.Debug("[DUCKDB] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *DuckDBDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	stagingColumns := opts.Columns
	destColumns := destination.DestinationColumns(stagingColumns)
	stagingQuoted := quoteColumns(stagingColumns)
	destQuoted := quoteColumns(destColumns)
	nonPKColumns := filterColumns(destColumns, opts.PrimaryKeys)

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")
	cdcMerge := slices.Contains(opts.Columns, destination.CDCDeletedColumn)

	// Build dedup subquery to handle duplicate PKs in staging. When an
	// incremental key is set the latest row per PK wins; otherwise arbitrary.
	quotedPKs := quoteColumns(opts.PrimaryKeys)
	dedupOrderBy := "(SELECT NULL)"
	if opts.IncrementalKey != "" {
		dedupOrderBy = destination.QuoteIdentifier(opts.IncrementalKey) + " DESC"
	}
	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1) AS source`,
		strings.Join(stagingQuoted, ", "),
		strings.Join(stagingQuoted, ", "),
		strings.Join(quotedPKs, ", "),
		dedupOrderBy,
		destination.QuoteTableName(opts.StagingTable),
	)

	if len(nonPKColumns) > 0 {
		updateSQL := fmt.Sprintf(
			`UPDATE %s AS target SET %s FROM %s WHERE %s`,
			quotedTargetTable,
			buildUpdateSet(nonPKColumns, "target", "source", cdcMerge),
			dedupSource,
			onCondition,
		)
		config.Debug("[DUCKDB MERGE] Executing UPDATE: %s", updateSQL)

		if err := d.exec(ctx, updateSQL); err != nil {
			return fmt.Errorf("failed to update existing records: %w", err)
		}
	}

	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (%s) SELECT %s FROM %s WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)`,
		quotedTargetTable,
		strings.Join(destQuoted, ", "),
		strings.Join(destQuoted, ", "),
		dedupSource,
		quotedTargetTable,
		onCondition,
	)
	config.Debug("[DUCKDB MERGE] Executing INSERT: %s", insertSQL)

	if err := d.exec(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert new records: %w", err)
	}

	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commit = true

	config.Debug("[DUCKDB MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *DuckDBDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	quotedColumns := quoteColumns(opts.Columns)

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	deleteSQL := fmt.Sprintf(
		`DELETE FROM %s WHERE "%s" >= ? AND "%s" <= ?`,
		quotedTargetTable, opts.IncrementalKey, opts.IncrementalKey,
	)
	config.Debug("[DUCKDB DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if err := d.exec(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		return fmt.Errorf("failed to delete records: %w", err)
	}

	colList := strings.Join(quotedColumns, ", ")
	// Dedupe staging by primary key, keeping the latest row per key by incremental key.
	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), quotedStagingTable, quoteColumns([]string{opts.IncrementalKey})[0])
	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) %s`, quotedTargetTable, colList, selectClause)
	config.Debug("[DUCKDB DELETE+INSERT] Executing INSERT: %s", insertSQL)

	if err := d.exec(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert records: %w", err)
	}

	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commit = true

	config.Debug("[DUCKDB DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *DuckDBDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	commit := false
	defer func() {
		if !commit {
			_ = d.exec(ctx, "ROLLBACK")
		}
	}()

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditions(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	updateSQL := fmt.Sprintf(
		`
		UPDATE %s AS target SET
			"_scd_valid_to" = source."_scd_valid_from",
			"_scd_is_current" = false
		FROM %s AS source
		WHERE %s
		  AND target."_scd_is_current" = true
		  AND (%s)`,
		quotedTargetTable,
		quotedStagingTable,
		onCondition,
		changeConditions,
	)
	config.Debug("[DUCKDB SCD2] Step 1 - Close changed records: %s", updateSQL)

	if err := d.exec(ctx, updateSQL); err != nil {
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s AS target SET
				"_scd_valid_to" = $1,
				"_scd_is_current" = false
			WHERE target."_scd_is_current" = true
			  AND NOT EXISTS (SELECT 1 FROM %s AS source WHERE %s)`,
			quotedTargetTable,
			quotedStagingTable,
			onCondition,
		)
		config.Debug("[DUCKDB SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if err := d.exec(ctx, softDeleteSQL, opts.Timestamp); err != nil {
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	// Insert records that either:
	// - Don't exist at all (net-new)
	// - Exist but have changed (new version - the old version was closed in step 1)
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := quoteColumns(allColumns)

	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s (%s)
		SELECT %s FROM %s AS source
		WHERE NOT EXISTS (
			SELECT 1 FROM %s AS target
			WHERE %s
			  AND target."_scd_is_current" = true
		)`,
		quotedTargetTable,
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quotedStagingTable,
		quotedTargetTable,
		onCondition,
	)
	config.Debug("[DUCKDB SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if err := d.exec(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commit = true

	config.Debug("[DUCKDB SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

func (d *DuckDBDestination) DropTable(ctx context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(table))); err != nil {
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[DUCKDB] Dropped table: %s", table)
	return nil
}

func (d *DuckDBDestination) TruncateTable(ctx context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", destination.QuoteTableName(table))); err != nil {
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[DUCKDB] Truncated table: %s", table)
	return nil
}

func (d *DuckDBDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.exec(ctx, sql, args...)
}

func (d *DuckDBDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	d.mu.Lock()
	if err := d.exec(ctx, "BEGIN"); err != nil {
		d.mu.Unlock()
		return nil, err
	}
	return &duckdbTransaction{d: d}, nil
}

type duckdbTransaction struct {
	d      *DuckDBDestination
	closed bool
}

func (t *duckdbTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	if t.closed {
		return fmt.Errorf("transaction is closed")
	}
	return t.d.exec(ctx, sql, args...)
}

func (t *duckdbTransaction) Commit(ctx context.Context) error {
	if t.closed {
		return nil
	}
	t.closed = true
	defer t.d.mu.Unlock()
	return t.d.exec(ctx, "COMMIT")
}

func (t *duckdbTransaction) Rollback(ctx context.Context) error {
	if t.closed {
		return nil
	}
	t.closed = true
	defer t.d.mu.Unlock()
	return t.d.exec(ctx, "ROLLBACK")
}

func (d *DuckDBDestination) SupportsReplaceStrategy() bool      { return true }
func (d *DuckDBDestination) SupportsAppendStrategy() bool       { return true }
func (d *DuckDBDestination) SupportsMergeStrategy() bool        { return true }
func (d *DuckDBDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *DuckDBDestination) SupportsSCD2Strategy() bool         { return true }
func (d *DuckDBDestination) SupportsAtomicSwap() bool           { return true }

// GetScheme returns the primary URI scheme for DuckDB.
func (d *DuckDBDestination) GetScheme() string { return "duckdb" }

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table for CDC resume.
func (d *DuckDBDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := fmt.Sprintf(`SELECT MAX("_cdc_lsn") FROM %s`, destination.QuoteTableName(table))
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return "", err
	}
	defer func() { _ = stmt.Close() }()

	if err := stmt.SetSqlQuery(query); err != nil {
		// Table doesn't exist
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "Catalog Error") {
			return "", nil
		}
		return "", err
	}

	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		// Table doesn't exist or column doesn't exist
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "Catalog Error") ||
			strings.Contains(err.Error(), "not found") {
			return "", nil
		}
		return "", err
	}
	defer reader.Release()

	if reader.Next() {
		batch := reader.RecordBatch()
		if batch.NumRows() > 0 && batch.NumCols() > 0 {
			col := batch.Column(0)
			if col.IsNull(0) {
				return "", nil
			}
			if strCol, ok := col.(*array.String); ok {
				return strCol.Value(0), nil
			}
		}
	}

	return "", nil
}

// GetTableSchema returns the current schema of a table, or nil if table doesn't exist.
func (d *DuckDBDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseSchemaTable(table)

	d.mu.Lock()
	defer d.mu.Unlock()

	query := fmt.Sprintf("DESCRIBE %s", destination.QuoteTableName(table))
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return nil, fmt.Errorf("failed to create statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	if err := stmt.SetSqlQuery(query); err != nil {
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "Catalog Error") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to set query: %w", err)
	}

	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "Catalog Error") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	defer reader.Release()

	var columns []schema.Column
	for reader.Next() {
		batch := reader.RecordBatch()
		for i := int64(0); i < batch.NumRows(); i++ {
			colName := batch.Column(0).(*array.String).Value(int(i))
			colType := batch.Column(1).(*array.String).Value(int(i))
			nullable := true
			if batch.NumCols() > 2 {
				nullStr := batch.Column(2).(*array.String).Value(int(i))
				nullable = nullStr == "YES"
			}

			columns = append(columns, schema.Column{
				Name:     strings.Clone(colName),
				DataType: mapDuckDBTypeToSchema(colType),
				Nullable: nullable,
			})
		}
	}

	if len(columns) == 0 {
		return nil, nil
	}

	return &schema.TableSchema{
		Name:    tableName,
		Schema:  schemaName,
		Columns: columns,
	}, nil
}

func mapDuckDBTypeToSchema(colType string) schema.DataType {
	colType = strings.ToUpper(colType)

	switch {
	case colType == "BOOLEAN" || colType == "BOOL":
		return schema.TypeBoolean
	case colType == "TINYINT" || colType == "INT1":
		return schema.TypeInt16
	case colType == "SMALLINT" || colType == "INT2":
		return schema.TypeInt16
	case colType == "INTEGER" || colType == "INT4" || colType == "INT":
		return schema.TypeInt32
	case colType == "BIGINT" || colType == "INT8":
		return schema.TypeInt64
	case colType == "REAL" || colType == "FLOAT4" || colType == "FLOAT":
		return schema.TypeFloat32
	case colType == "DOUBLE" || colType == "FLOAT8":
		return schema.TypeFloat64
	case strings.HasPrefix(colType, "DECIMAL") || strings.HasPrefix(colType, "NUMERIC"):
		return schema.TypeDecimal
	case colType == "VARCHAR" || colType == "TEXT" || colType == "STRING" || strings.HasPrefix(colType, "VARCHAR"):
		return schema.TypeString
	case colType == "BLOB" || colType == "BYTEA":
		return schema.TypeBinary
	case colType == "DATE":
		return schema.TypeDate
	case colType == "TIME":
		return schema.TypeTime
	case colType == "TIMESTAMP" || colType == "DATETIME":
		return schema.TypeTimestamp
	case colType == "TIMESTAMPTZ" || colType == "TIMESTAMP WITH TIME ZONE":
		return schema.TypeTimestampTZ
	case colType == "INTERVAL":
		return schema.TypeInterval
	case colType == "JSON":
		return schema.TypeJSON
	case colType == "UUID":
		return schema.TypeUUID
	case strings.HasSuffix(colType, "[]") || strings.HasPrefix(colType, "ARRAY"):
		return schema.TypeArray
	default:
		return schema.TypeString
	}
}

func parseDuckDBPath(uri string) (string, error) {
	if uri == "duckdb://:memory:" || uri == "duckdb:///:memory:" {
		return ":memory:", nil
	}

	if strings.HasPrefix(uri, "motherduck://") || strings.HasPrefix(uri, "md://") {
		return parseMotherDuckURI(uri)
	}

	if !strings.HasPrefix(uri, "duckdb://") {
		return "", fmt.Errorf("invalid duckdb URI: %s", uri)
	}

	path := strings.TrimPrefix(uri, "duckdb://")
	if path == "" {
		return ":memory:", nil
	}

	// Normalize accidental extra leading slash for absolute paths, e.g.
	// fmt.Sprintf("duckdb:///%s", "/tmp/x.duckdb") -> "duckdb:////tmp/x.duckdb".
	for strings.HasPrefix(path, "//") && (len(path) <= 3 || path[3] != ':') {
		path = path[1:]
	}

	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}

	if strings.HasPrefix(path, "/") && !strings.Contains(path[1:], "/") {
		path = "." + path
	}

	return path, nil
}

func parseMotherDuckURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("failed to parse MotherDuck URI: %w", err)
	}

	token := parsed.Query().Get("token")
	if token == "" {
		return "", fmt.Errorf("MotherDuck token is required (use ?token=<your-token> in URI)")
	}

	database := strings.TrimPrefix(parsed.Host+parsed.Path, "/")
	database = strings.TrimPrefix(database, "/")

	if database == "" {
		return fmt.Sprintf("md:?motherduck_token=%s", token), nil
	}
	return fmt.Sprintf("md:%s?motherduck_token=%s", database, token), nil
}

func parseSchemaTable(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", table
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	for _, col := range columns {
		colType := MapDataTypeToDuckDB(col)
		colDefs = append(colDefs, fmt.Sprintf(`"%s" %s`, col.Name, colType))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s", table, strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = fmt.Sprintf(`"%s"`, k)
		}
		sql += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(quotedKeys, ", "))
	}

	sql += "\n)"
	return sql
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = fmt.Sprintf(`"%s"`, col)
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
		conditions[i] = fmt.Sprintf(`%s."%s" = %s."%s"`, targetAlias, key, sourceAlias, key)
	}
	return strings.Join(conditions, " AND ")
}

func buildUpdateSet(columns []string, targetAlias, sourceAlias string, cdcMerge bool) string {
	unchangedRef := fmt.Sprintf(`%s."%s"`, sourceAlias, destination.CDCUnchangedColsColumn)
	sets := make([]string, len(columns))
	for i, col := range columns {
		if cdcMerge && !destination.IsCDCMetaColumn(col) {
			sets[i] = cdcMergeAssign(
				col,
				fmt.Sprintf(`%s."%s"`, targetAlias, col),
				fmt.Sprintf(`%s."%s"`, sourceAlias, col),
				unchangedRef,
			)
		} else {
			sets[i] = fmt.Sprintf(`"%s" = %s."%s"`, col, sourceAlias, col)
		}
	}
	return strings.Join(sets, ", ")
}

func cdcUnchangedColJSONNeedle(colName string) string {
	b, _ := json.Marshal([]string{colName})
	return strings.ReplaceAll(string(b), "'", "''")
}

func cdcMergeAssign(col, targetExpr, sourceExpr, unchangedColsExpr string) string {
	needle := cdcUnchangedColJSONNeedle(col)
	return fmt.Sprintf(
		`"%s" = CASE WHEN json_contains(%s, '%s') THEN %s ELSE %s END`,
		col, unchangedColsExpr, needle, targetExpr, sourceExpr,
	)
}

// buildChangeConditions builds change detection conditions using IS DISTINCT FROM.
func buildChangeConditions(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "false"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		conditions[i] = fmt.Sprintf(`%s."%s" IS DISTINCT FROM %s."%s"`, targetAlias, col, sourceAlias, col)
	}
	return strings.Join(conditions, " OR ")
}

func (d *DuckDBDestination) exec(ctx context.Context, sql string, args ...interface{}) error {
	if d.conn == nil {
		return fmt.Errorf("DuckDB destination not connected")
	}

	stmt, err := d.conn.NewStatement()
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	if err := stmt.SetSqlQuery(sql); err != nil {
		config.LogFailedQuery(sql, err)
		return err
	}

	if len(args) > 0 {
		if err := stmt.Prepare(ctx); err != nil {
			config.LogFailedQuery(sql, err)
			return err
		}
		params, err := buildParameterRecord(args)
		if err != nil {
			config.LogFailedQuery(sql, err)
			return err
		}
		defer params.Release()

		if err := stmt.Bind(ctx, params); err != nil {
			config.LogFailedQuery(sql, err)
			return err
		}
	}

	if len(args) == 0 {
		rdr, _, qerr := stmt.ExecuteQuery(ctx)
		if qerr == nil {
			defer rdr.Release()
			for rdr.Next() {
				// drain
			}
			if err := rdr.Err(); err != nil {
				config.LogFailedQuery(sql, err)
				return err
			}
			return nil
		}
		_, uerr := stmt.ExecuteUpdate(ctx)
		if uerr == nil {
			return nil
		}
		config.LogFailedQuery(sql, qerr)
		return qerr
	}

	_, uerr := stmt.ExecuteUpdate(ctx)
	if uerr == nil {
		return nil
	}
	rdr, _, qerr := stmt.ExecuteQuery(ctx)
	if qerr == nil {
		defer rdr.Release()
		for rdr.Next() {
			// drain
		}
		if err := rdr.Err(); err != nil {
			config.LogFailedQuery(sql, err)
			return err
		}
		return nil
	}
	config.LogFailedQuery(sql, uerr)
	return uerr
}

func buildParameterRecord(args []interface{}) (arrow.RecordBatch, error) {
	fields := make([]arrow.Field, len(args))
	values := make([]interface{}, len(args))
	valid := make([]bool, len(args))

	for i, a := range args {
		switch v := a.(type) {
		case nil:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.BinaryTypes.String, Nullable: true}
			values[i] = nil
			valid[i] = false
		case *time.Time:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: &arrow.TimestampType{Unit: arrow.Microsecond}, Nullable: true}
			if v == nil {
				values[i] = nil
				valid[i] = false
			} else {
				values[i] = *v
				valid[i] = true
			}
		case time.Time:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: &arrow.TimestampType{Unit: arrow.Microsecond}, Nullable: true}
			values[i] = v
			valid[i] = true
		case bool:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.FixedWidthTypes.Boolean, Nullable: true}
			values[i] = v
			valid[i] = true
		case int:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Int64, Nullable: true}
			values[i] = int64(v)
			valid[i] = true
		case int32:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Int64, Nullable: true}
			values[i] = int64(v)
			valid[i] = true
		case int64:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Int64, Nullable: true}
			values[i] = v
			valid[i] = true
		case uint:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Uint64, Nullable: true}
			values[i] = uint64(v)
			valid[i] = true
		case uint32:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Uint64, Nullable: true}
			values[i] = uint64(v)
			valid[i] = true
		case uint64:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Uint64, Nullable: true}
			values[i] = v
			valid[i] = true
		case float32:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Float64, Nullable: true}
			values[i] = float64(v)
			valid[i] = true
		case float64:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.PrimitiveTypes.Float64, Nullable: true}
			values[i] = v
			valid[i] = true
		case string:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.BinaryTypes.String, Nullable: true}
			values[i] = v
			valid[i] = true
		case []byte:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.BinaryTypes.Binary, Nullable: true}
			values[i] = v
			valid[i] = true
		default:
			fields[i] = arrow.Field{Name: fmt.Sprintf("p%d", i+1), Type: arrow.BinaryTypes.String, Nullable: true}
			values[i] = fmt.Sprintf("%v", v)
			valid[i] = true
		}
	}

	schema := arrow.NewSchema(fields, nil)
	b := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer b.Release()

	for i, field := range fields {
		switch field.Type.ID() {
		case arrow.BOOL:
			builder := b.Field(i).(*array.BooleanBuilder)
			if valid[i] {
				builder.Append(values[i].(bool))
			} else {
				builder.AppendNull()
			}
		case arrow.INT64:
			builder := b.Field(i).(*array.Int64Builder)
			if valid[i] {
				builder.Append(values[i].(int64))
			} else {
				builder.AppendNull()
			}
		case arrow.UINT64:
			builder := b.Field(i).(*array.Uint64Builder)
			if valid[i] {
				builder.Append(values[i].(uint64))
			} else {
				builder.AppendNull()
			}
		case arrow.FLOAT64:
			builder := b.Field(i).(*array.Float64Builder)
			if valid[i] {
				builder.Append(values[i].(float64))
			} else {
				builder.AppendNull()
			}
		case arrow.STRING:
			builder := b.Field(i).(*array.StringBuilder)
			if valid[i] {
				builder.Append(values[i].(string))
			} else {
				builder.AppendNull()
			}
		case arrow.BINARY:
			builder := b.Field(i).(*array.BinaryBuilder)
			if valid[i] {
				builder.Append(values[i].([]byte))
			} else {
				builder.AppendNull()
			}
		case arrow.TIMESTAMP:
			builder := b.Field(i).(*array.TimestampBuilder)
			if valid[i] {
				t := values[i].(time.Time)
				builder.Append(arrow.Timestamp(t.UnixMicro()))
			} else {
				builder.AppendNull()
			}
		default:
			builder := b.Field(i).(*array.StringBuilder)
			if valid[i] {
				builder.Append(values[i].(string))
			} else {
				builder.AppendNull()
			}
		}
	}

	return b.NewRecordBatch(), nil
}

func errorsJoin(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return fmt.Errorf("%v; %w", a, b)
}
