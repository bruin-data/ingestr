package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	_ "modernc.org/sqlite"
)

type SQLiteDestination struct {
	db              *sql.DB
	filePath        string
	attachedSchemas map[string]string
	attachMu        sync.Mutex

	// schemas captures the schema each prepared table was created with, keyed by
	// opts.Table. The cross-attached-database swap branch uses it to recreate the
	// target with full constraints. Per-key writes are safe under parallel
	// PrepareTable calls in multi-table runs.
	schemas   map[string]*schema.TableSchema
	schemasMu sync.Mutex

	incarnationMu sync.Mutex
}

func NewSQLiteDestination() *SQLiteDestination {
	return &SQLiteDestination{}
}

func (d *SQLiteDestination) recordSchema(table string, sch *schema.TableSchema, pks []string) {
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

func (d *SQLiteDestination) lookupSchema(table string) *schema.TableSchema {
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	return d.schemas[table]
}

func (d *SQLiteDestination) forgetSchema(table string) {
	d.schemasMu.Lock()
	defer d.schemasMu.Unlock()
	delete(d.schemas, table)
}

func (d *SQLiteDestination) Schemes() []string {
	return []string{"sqlite"}
}

func (d *SQLiteDestination) Connect(ctx context.Context, uri string) error {
	path, err := parseSQLitePath(uri)
	if err != nil {
		return fmt.Errorf("failed to parse SQLite URI: %w", err)
	}

	d.filePath = path
	config.Debug("[SQLITE] Destination file: %s", d.filePath)

	// Ensure directory exists
	dir := filepath.Dir(d.filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Open SQLite database
	db, err := sql.Open("sqlite", d.filePath)
	if err != nil {
		return fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Pin to one connection: ATTACH state and PRAGMAs are per-connection.
	db.SetMaxOpenConns(1)

	// Configure for better write performance
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to set journal mode: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA synchronous=FULL"); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to set synchronous mode: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA cache_size=-64000"); err != nil { // 64MB cache
		_ = db.Close()
		return fmt.Errorf("failed to set cache size: %w", err)
	}

	d.db = db
	d.attachedSchemas = map[string]string{}
	return nil
}

func (d *SQLiteDestination) ensureSchemaAttached(ctx context.Context, schemaName string) error {
	if schemaName == "" || schemaName == "main" || schemaName == "temp" {
		return nil
	}
	d.attachMu.Lock()
	defer d.attachMu.Unlock()
	if _, ok := d.attachedSchemas[schemaName]; ok {
		return nil
	}
	stagingPath := stagingFilePath(d.filePath, schemaName)
	attachSQL := fmt.Sprintf("ATTACH DATABASE '%s' AS %s",
		strings.ReplaceAll(stagingPath, "'", "''"),
		destination.QuoteIdentifier(schemaName))
	if _, err := d.db.ExecContext(ctx, attachSQL); err != nil {
		config.LogFailedQuery(attachSQL, err)
		return fmt.Errorf("failed to attach schema %s: %w", schemaName, err)
	}

	walSQL := fmt.Sprintf("PRAGMA %s.journal_mode=WAL", destination.QuoteIdentifier(schemaName))
	if _, err := d.db.ExecContext(ctx, walSQL); err != nil {
		config.LogFailedQuery(walSQL, err)
		return fmt.Errorf("failed to set WAL on attached schema %s: %w", schemaName, err)
	}
	synchronousSQL := fmt.Sprintf("PRAGMA %s.synchronous=FULL", destination.QuoteIdentifier(schemaName))
	if _, err := d.db.ExecContext(ctx, synchronousSQL); err != nil {
		config.LogFailedQuery(synchronousSQL, err)
		return fmt.Errorf("failed to set synchronous mode on attached schema %s: %w", schemaName, err)
	}
	d.attachedSchemas[schemaName] = stagingPath
	config.Debug("[SQLITE] Attached schema %q at %s", schemaName, stagingPath)
	return nil
}

// Leading underscores are omitted from the established attached-database filename.
func stagingFilePath(targetFile, schemaName string) string {
	ext := filepath.Ext(targetFile)
	base := strings.TrimSuffix(targetFile, ext)
	return fmt.Sprintf("%s__%s%s", base, strings.TrimLeft(schemaName, "_"), ext)
}

func schemaOf(table string) string {
	parts := tablename.Split(table)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func (d *SQLiteDestination) Close(ctx context.Context) error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *SQLiteDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := tablename.TwoLevel("sqlite").CheckName(opts.Table); err != nil {
		return err
	}
	d.recordSchema(opts.Table, opts.Schema, opts.PrimaryKeys)

	if err := d.ensureSchemaAttached(ctx, schemaOf(opts.Table)); err != nil {
		return err
	}

	if opts.DropFirst {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(opts.Table))
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[SQLITE] DROP TABLE took %v", time.Since(startDrop))
	}

	if opts.Schema != nil {
		startCreate := time.Now()
		createSQL := buildCreateTableSQL(destination.QuoteTableName(opts.Table), opts.Schema.Columns, opts.PrimaryKeys)
		if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
		config.Debug("[SQLITE] CREATE TABLE took %v", time.Since(startCreate))
	}

	return nil
}

func (d *SQLiteDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *SQLiteDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[SQLITE] Starting write to %s", opts.Table)

	for result := range records {
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			return result.Err
		}
		if result.Batch == nil {
			continue
		}

		batchNum++
		startBatch := time.Now()

		rows, err := d.writeRecordBatch(ctx, result.Batch, opts.Table)
		result.Batch.Release()
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		rate := float64(rows) / time.Since(startBatch).Seconds()
		config.Debug("[SQLITE] Batch %d: %d rows in %v (%.0f rows/sec, total: %d)",
			batchNum, rows, time.Since(startBatch), rate, totalRows)

	}

	totalRate := float64(totalRows) / time.Since(startTime).Seconds()
	config.Debug("[SQLITE] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTime), totalRate)
	return nil
}

func (d *SQLiteDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	numRows := record.NumRows()
	numCols := int(record.NumCols())

	if numRows == 0 {
		return 0, nil
	}

	// Build INSERT statement with placeholders
	colNames := make([]string, numCols)
	placeholders := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		colNames[i] = destination.QuoteIdentifier(record.Schema().Field(i).Name)
		placeholders[i] = "?"
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		destination.QuoteTableName(table),
		strings.Join(colNames, ", "),
		strings.Join(placeholders, ", "),
	)

	// Use a transaction for the batch
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	// Insert each row
	for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
		values := make([]interface{}, numCols)
		for colIdx := 0; colIdx < numCols; colIdx++ {
			values[colIdx] = extractValue(record.Column(colIdx), int(rowIdx))
		}

		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			config.LogFailedQuery(insertSQL, err)
			_ = tx.Rollback()
			return rowIdx, fmt.Errorf("failed to insert row %d: %w", rowIdx, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return numRows, nil
}

func (d *SQLiteDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable

	if err := d.ensureSchemaAttached(ctx, schemaOf(stagingTable)); err != nil {
		return err
	}
	if err := d.ensureSchemaAttached(ctx, schemaOf(targetTable)); err != nil {
		return err
	}

	stagingDB := schemaOf(stagingTable)
	if stagingDB == "" {
		stagingDB = "main"
	}
	targetDB := schemaOf(targetTable)
	if targetDB == "" {
		targetDB = "main"
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	dropTargetSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(targetTable))
	if _, err := tx.ExecContext(ctx, dropTargetSQL); err != nil {
		config.LogFailedQuery(dropTargetSQL, err)
		_ = tx.Rollback()
		return fmt.Errorf("failed to drop target table: %w", err)
	}

	if stagingDB == targetDB {
		// Same database — RENAME is supported.
		renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", destination.QuoteTableName(stagingTable), destination.QuoteIdentifier(extractTableName(targetTable)))
		if _, err := tx.ExecContext(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to rename staging table: %w", err)
		}
	} else {
		// Cross-attached-database swap: SQLite forbids RENAME across databases.
		// Recreate target with the staging's recorded schema (NOT NULL, PK preserved)
		// and copy rows. Schema is keyed by staging table name to avoid the multi-table
		// race that affected single-field cached schemas.
		sch := d.lookupSchema(stagingTable)
		if sch == nil {
			_ = tx.Rollback()
			return fmt.Errorf("cannot swap %s -> %s: no recorded schema for staging table", stagingTable, targetTable)
		}

		createSQL := buildCreateTableSQL(destination.QuoteTableName(targetTable), sch.Columns, sch.PrimaryKeys)
		if _, err := tx.ExecContext(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			_ = tx.Rollback()
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
		if _, err := tx.ExecContext(ctx, copySQL); err != nil {
			config.LogFailedQuery(copySQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to copy staging rows into target: %w", err)
		}

		dropStagingSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(stagingTable))
		if _, err := tx.ExecContext(ctx, dropStagingSQL); err != nil {
			config.LogFailedQuery(dropStagingSQL, err)
			_ = tx.Rollback()
			return fmt.Errorf("failed to drop staging table after copy: %w", err)
		}
		d.forgetSchema(stagingTable)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit swap: %w", err)
	}

	config.Debug("[SQLITE] Table swap completed in %v", time.Since(startSwap))
	return nil
}

func (d *SQLiteDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := d.db.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *SQLiteDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &sqliteTransaction{tx: tx}, nil
}

type sqliteTransaction struct {
	tx *sql.Tx
}

func (t *sqliteTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (t *sqliteTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *sqliteTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

// MergeTable performs an UPDATE + INSERT merge operation using a transaction.
func (d *SQLiteDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	columns := opts.Columns
	quotedColumns := quoteColumns(columns)
	targetColumns := destination.DestinationColumns(columns)
	quotedTargetColumns := quoteColumns(targetColumns)
	nonPKColumns := filterColumns(targetColumns, opts.PrimaryKeys)

	// Begin transaction for atomic merge
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)

	// Build dedup subquery to handle duplicate PKs in staging. For CDC data the
	// latest change per PK wins (LSN strings are fixed-width and sort
	// lexicographically); otherwise, when an incremental key is set the latest
	// row per PK wins, else arbitrary.
	quotedPKs := quoteColumns(opts.PrimaryKeys)
	isCDC := destination.HasCDCDeletedColumn(columns)
	hasUnchangedCols := destination.HasCDCUnchangedColsColumn(columns)
	dedupOrderBy := "(SELECT NULL)"
	if isCDC {
		dedupOrderBy = destination.CDCLatestOverallOrderBy(destination.QuoteIdentifier)
	} else if opts.IncrementalKey != "" {
		dedupOrderBy = destination.QuoteIdentifier(opts.IncrementalKey) + " DESC"
	}
	dedupSourceWithOrder := func(where, orderBy string) string {
		return fmt.Sprintf(
			`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s%s) AS _numbered WHERE __bruin_dedup_rn = 1) AS source`,
			strings.Join(quotedColumns, ", "),
			strings.Join(quotedColumns, ", "),
			strings.Join(quotedPKs, ", "),
			orderBy,
			destination.QuoteTableName(opts.StagingTable),
			where,
		)
	}
	dedupSource := func(where string) string { return dedupSourceWithOrder(where, dedupOrderBy) }

	// For CDC, updates and inserts use the latest active image when available;
	// delete-only keys use their latest tombstone. Deletes are applied below.
	updateSource := dedupSource("")
	insertSource := updateSource
	if isCDC {
		marker := destination.QuoteIdentifier("__ingestr_has_equal_lsn_delete")
		updateSource = fmt.Sprintf(
			`(SELECT %s, %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s, "_cdc_deleted" ORDER BY "_cdc_lsn" DESC) AS __bruin_active_rn, MAX("_cdc_deleted") OVER (PARTITION BY %s, "_cdc_lsn") AS %s FROM %s) AS _numbered WHERE "_cdc_deleted" = 0 AND __bruin_active_rn = 1) AS source`,
			strings.Join(quotedColumns, ", "),
			marker,
			strings.Join(quotedColumns, ", "),
			strings.Join(quotedPKs, ", "),
			strings.Join(quotedPKs, ", "),
			marker,
			destination.QuoteTableName(opts.StagingTable),
		)
		insertSource = dedupSourceWithOrder("", `"_cdc_deleted" ASC, "_cdc_lsn" DESC`)
	}
	primaryKeyMatchCondition := buildJoinConditionSQLite(opts.PrimaryKeys, "target", "source")
	matchCondition := destination.MergeJoinCondition(
		primaryKeyMatchCondition,
		opts.IncrementalPredicate,
	)
	insertMatchCondition := matchCondition
	if isCDC {
		insertMatchCondition = primaryKeyMatchCondition
	}

	runUpdate := func() error {
		if len(nonPKColumns) == 0 {
			return nil
		}
		updateTarget := quotedTargetTable + " AS target"
		updateSet := buildUpdateSetSQLite(nonPKColumns, "source")
		if isCDC && hasUnchangedCols {
			updateSet = buildCDCUpdateSetSQLite(nonPKColumns, "target", "source", "source."+destination.QuoteIdentifier(destination.CDCUnchangedColsColumn))
		}
		updateMatchCondition := matchCondition
		if isCDC {
			updateMatchCondition += ` AND (target."_cdc_lsn" IS NULL OR source."_cdc_lsn" > target."_cdc_lsn" OR (source."_cdc_lsn" = target."_cdc_lsn" AND COALESCE(target."_cdc_deleted", 0) = 0 AND source."__ingestr_has_equal_lsn_delete" = 1))`
		}
		updateSQL := fmt.Sprintf(
			`UPDATE %s SET %s FROM %s WHERE %s`,
			updateTarget,
			updateSet,
			updateSource,
			updateMatchCondition,
		)
		config.Debug("[MERGE] Executing UPDATE: %s", updateSQL)

		if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
			config.LogFailedQuery(updateSQL, err)
			return fmt.Errorf("failed to update existing records: %w", err)
		}
		return nil
	}

	runInsert := func() error {
		insertSQL := fmt.Sprintf(
			`INSERT INTO %s (%s) SELECT %s FROM %s WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)`,
			quotedTargetTable,
			strings.Join(quotedTargetColumns, ", "),
			strings.Join(quotedTargetColumns, ", "),
			insertSource,
			quotedTargetTable,
			insertMatchCondition,
		)
		config.Debug("[MERGE] Executing INSERT: %s", insertSQL)

		if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
			config.LogFailedQuery(insertSQL, err)
			return fmt.Errorf("failed to insert new records: %w", err)
		}
		return nil
	}

	// With a predicate, the INSERT runs first so its anti-join sees the
	// pre-update target: an UPDATE that moves a matched row out of the
	// predicate window would otherwise make the INSERT re-add it as a
	// duplicate. CDC anti-joins always use the primary key alone.
	steps := []func() error{runUpdate, runInsert}
	if strings.TrimSpace(opts.IncrementalPredicate) != "" {
		steps = []func() error{runInsert, runUpdate}
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}

	if isCDC {
		// Mark rows deleted only when the latest change for the PK is a delete,
		// carrying the delete's LSN so resume picks up after it.
		markDeletedSQL := fmt.Sprintf(
			`UPDATE %s AS target SET "_cdc_deleted" = 1, "_cdc_lsn" = source."_cdc_lsn", "_cdc_synced_at" = source."_cdc_synced_at" FROM %s WHERE %s AND source."_cdc_deleted" = 1 AND (target."_cdc_lsn" IS NULL OR source."_cdc_lsn" > target."_cdc_lsn" OR (source."_cdc_lsn" = target."_cdc_lsn" AND COALESCE(target."_cdc_deleted", 0) = 0))`,
			quotedTargetTable,
			dedupSource(""),
			matchCondition,
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

// DeleteInsertTable performs a DELETE + INSERT operation using a transaction.
func (d *SQLiteDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	quotedColumns := quoteColumns(opts.Columns)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	deleteSQL := fmt.Sprintf(
		`DELETE FROM %s WHERE %s >= ? AND %s <= ?`,
		quotedTargetTable, destination.QuoteIdentifier(opts.IncrementalKey), destination.QuoteIdentifier(opts.IncrementalKey),
	)
	config.Debug("[DELETE+INSERT] Executing DELETE: %s", deleteSQL)

	if _, err := tx.ExecContext(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	colList := strings.Join(quotedColumns, ", ")
	// Dedupe staging by primary key, keeping the latest row per key by incremental key.
	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), quotedStagingTable, quoteColumns([]string{opts.IncrementalKey})[0])
	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) %s`, quotedTargetTable, colList, selectClause)
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

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *SQLiteDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	quotedTargetTable := destination.QuoteTableName(opts.TargetTable)
	quotedStagingTable := destination.QuoteTableName(opts.StagingTable)

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditionsSQLite(nonPKColumns, quotedTargetTable, "source")
	onCondition := buildJoinConditionSQLite(opts.PrimaryKeys, quotedTargetTable, "source")

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	updateSQL := fmt.Sprintf(
		`
		UPDATE %s SET
			"_scd_valid_to" = source."_scd_valid_from",
			"_scd_is_current" = 0
		FROM %s AS source
		WHERE %s
		  AND %s."_scd_is_current" = 1
		  AND (%s)`,
		quotedTargetTable,
		quotedStagingTable,
		onCondition,
		quotedTargetTable,
		changeConditions,
	)
	config.Debug("[SQLITE SCD2] Step 1 - Close changed records: %s", updateSQL)

	if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			UPDATE %s SET
				"_scd_valid_to" = ?,
				"_scd_is_current" = 0
			WHERE %s."_scd_is_current" = 1
			  AND NOT EXISTS (SELECT 1 FROM %s AS source WHERE %s)`,
			quotedTargetTable,
			quotedTargetTable,
			quotedStagingTable,
			onCondition,
		)
		config.Debug("[SQLITE SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

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
			  AND target."_scd_is_current" = 1
		)`,
		quotedTargetTable,
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quotedStagingTable,
		quotedTargetTable,
		buildJoinConditionSQLite(opts.PrimaryKeys, "target", "source"),
	)
	config.Debug("[SQLITE SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	config.Debug("[SQLITE SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

// DropTable drops a table if it exists.
func (d *SQLiteDestination) DropTable(ctx context.Context, table string) error {
	if err := d.ensureSchemaAttached(ctx, schemaOf(table)); err != nil {
		return err
	}
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", destination.QuoteTableName(table))
	_, err := d.db.ExecContext(ctx, dropSQL)
	if err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[SQLITE] Dropped table: %s", table)
	return nil
}

// TruncateTable empties a table. SQLite has no TRUNCATE, so this uses an
// unconditional DELETE (which SQLite optimizes via its truncate-optimization).
func (d *SQLiteDestination) TruncateTable(ctx context.Context, table string) error {
	deleteSQL := fmt.Sprintf("DELETE FROM %s", destination.QuoteTableName(table))
	if _, err := d.db.ExecContext(ctx, deleteSQL); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[SQLITE] Truncated table: %s", table)
	return nil
}

// SupportsReplaceStrategy returns true as SQLite supports the replace strategy.
func (d *SQLiteDestination) SupportsReplaceStrategy() bool { return true }

// SupportsAppendStrategy returns true as SQLite supports the append strategy.
func (d *SQLiteDestination) SupportsAppendStrategy() bool { return true }

// SupportsMergeStrategy returns true as SQLite supports the merge strategy.
func (d *SQLiteDestination) SupportsMergeStrategy() bool { return true }

func (d *SQLiteDestination) SupportsIncrementalPredicate() bool { return true }

// SupportsDeleteInsertStrategy returns true as SQLite supports the delete+insert strategy.
func (d *SQLiteDestination) SupportsDeleteInsertStrategy() bool { return true }

// SupportsSCD2Strategy returns true as SQLite supports the SCD2 strategy.
func (d *SQLiteDestination) SupportsSCD2Strategy() bool { return true }

// SupportsAtomicSwap returns true as SQLite supports atomic table renames.
func (d *SQLiteDestination) SupportsAtomicSwap() bool { return true }

func (d *SQLiteDestination) SupportsCDCMerge() bool { return true }

func (d *SQLiteDestination) SupportsCDCUnchangedCols() bool { return true }

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table for CDC resume.
func (d *SQLiteDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	if err := d.ensureSchemaAttached(ctx, schemaOf(table)); err != nil {
		return "", err
	}

	var maxLSN sql.NullString
	query := fmt.Sprintf(`SELECT MAX("_cdc_lsn") FROM %s`, destination.QuoteTableName(table))
	err := d.db.QueryRowContext(ctx, query).Scan(&maxLSN)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "no such column") {
			return "", nil
		}
		return "", err
	}
	if !maxLSN.Valid {
		return "", nil
	}
	return maxLSN.String, nil
}

func (d *SQLiteDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	if err := d.ensureSchemaAttached(ctx, schemaOf(table)); err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`SELECT "event_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn" FROM %s WHERE "connector_id" = ?`, destination.QuoteTableName(table))
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
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

func (d *SQLiteDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	targetSchema := schemaOf(claim.DestinationTable)
	if targetSchema == "" {
		targetSchema = "main"
	}
	canonicalTarget := destination.CDCTargetKey(strings.ToLower(targetSchema), strings.ToLower(extractTableName(claim.DestinationTable)))
	query := fmt.Sprintf(`INSERT INTO %s ("destination_table", "connector_id", "claimed_at") VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT("destination_table") DO UPDATE SET "claimed_at" = "claimed_at" RETURNING "connector_id"`, destination.QuoteTableName(claimTable))
	var owner string
	if err := d.db.QueryRowContext(ctx, query, canonicalTarget, ownerID).Scan(&owner); err != nil {
		return err
	}
	if owner != ownerID {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	return nil
}

func (d *SQLiteDestination) ClaimAndPrepareEmptyCDCTarget(
	ctx context.Context,
	claimTable string,
	claim destination.CDCTargetClaim,
	opts destination.PrepareOptions,
) (string, error) {
	if opts.Schema == nil {
		return "", fmt.Errorf("schema is required")
	}
	ownerID, err := claim.OwnerID()
	if err != nil {
		return "", err
	}
	schemaName := schemaOf(claim.DestinationTable)
	if schemaName == "" {
		schemaName = "main"
	}
	if err := d.ensureSchemaAttached(ctx, schemaName); err != nil {
		return "", err
	}

	d.incarnationMu.Lock()
	defer d.incarnationMu.Unlock()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	createSQL := strings.Replace(
		buildCreateTableSQL(destination.QuoteTableName(claim.DestinationTable), opts.Schema.Columns, opts.PrimaryKeys),
		"CREATE TABLE IF NOT EXISTS", "CREATE TABLE", 1,
	)
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return "", fmt.Errorf("failed to exclusively create CDC target: %w", err)
	}
	canonicalTarget := destination.CDCTargetKey(strings.ToLower(schemaName), strings.ToLower(extractTableName(claim.DestinationTable)))
	claimSQL := fmt.Sprintf(`INSERT INTO %s ("destination_table", "connector_id", "claimed_at") VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT("destination_table") DO UPDATE SET "claimed_at" = "claimed_at" RETURNING "connector_id"`, destination.QuoteTableName(claimTable))
	var owner string
	if err := tx.QueryRowContext(ctx, claimSQL, canonicalTarget, ownerID).Scan(&owner); err != nil {
		return "", err
	}
	if owner != ownerID {
		return "", fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	tableName := extractTableName(claim.DestinationTable)
	marker, err := d.ensureSQLiteIncarnationMarker(ctx, tx, schemaName, tableName)
	if err != nil {
		return "", err
	}
	incarnation := destination.CDCTargetKey(strings.ToLower(schemaName), strings.ToLower(tableName), marker)
	if err := tx.Commit(); err != nil {
		return "", err
	}
	d.recordSchema(claim.DestinationTable, opts.Schema, opts.PrimaryKeys)
	return incarnation, nil
}

func (d *SQLiteDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	schemaName := schemaOf(table)
	if schemaName == "" {
		schemaName = "main"
	}
	if err := d.ensureSchemaAttached(ctx, schemaName); err != nil {
		return err
	}
	d.incarnationMu.Lock()
	defer d.incarnationMu.Unlock()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	current, exists, err := d.sqliteTargetIncarnation(ctx, tx, table)
	if err != nil {
		return err
	}
	if !exists || current == "" || current != expectedIncarnation {
		return fmt.Errorf("SQLite CDC target %q physical incarnation changed", table)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+destination.QuoteTableName(table)); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *SQLiteDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	targetSchema := schemaOf(table)
	if targetSchema == "" {
		targetSchema = "main"
	}
	return destination.CDCTargetKey(strings.ToLower(targetSchema), strings.ToLower(extractTableName(table))), nil
}

func (d *SQLiteDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	d.incarnationMu.Lock()
	defer d.incarnationMu.Unlock()
	return d.sqliteTargetIncarnation(ctx, d.db, table)
}

type sqliteIncarnationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (d *SQLiteDestination) sqliteTargetIncarnation(ctx context.Context, queryer sqliteIncarnationQueryer, table string) (string, bool, error) {
	schemaName := schemaOf(table)
	if schemaName == "" {
		schemaName = "main"
	}
	if err := d.ensureSchemaAttached(ctx, schemaName); err != nil {
		return "", false, err
	}
	tableName := extractTableName(table)
	var actualName string
	query := fmt.Sprintf(`SELECT name FROM %s.sqlite_schema WHERE type = 'table' AND name = ? COLLATE NOCASE`, destination.QuoteIdentifier(schemaName))
	err := queryer.QueryRowContext(ctx, query, tableName).Scan(&actualName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to inspect SQLite CDC target %s: %w", table, err)
	}
	marker, err := d.readSQLiteIncarnationMarker(ctx, queryer, schemaName, actualName)
	if err != nil {
		return "", false, fmt.Errorf("failed to read SQLite CDC target incarnation for %s: %w", table, err)
	}
	if marker == "" {
		return "", true, nil
	}
	return destination.CDCTargetKey(
		strings.ToLower(schemaName),
		strings.ToLower(actualName),
		marker,
	), true, nil
}

func (d *SQLiteDestination) readSQLiteIncarnationMarker(ctx context.Context, queryer sqliteIncarnationQueryer, schemaName, tableName string) (string, error) {
	targetDigest := sha256.Sum256([]byte(destination.CDCTargetKey(strings.ToLower(schemaName), strings.ToLower(tableName))))
	triggerPrefix := "_bruin_cdc_incarnation_" + hex.EncodeToString(targetDigest[:8]) + "_"
	triggerSQLQuery := fmt.Sprintf(`SELECT name, sql FROM %s.sqlite_schema WHERE type = 'trigger' AND name GLOB ? AND tbl_name = ? COLLATE NOCASE ORDER BY name LIMIT 1`, destination.QuoteIdentifier(schemaName))
	var triggerName, triggerSQL string
	err := queryer.QueryRowContext(ctx, triggerSQLQuery, triggerPrefix+"*", tableName).Scan(&triggerName, &triggerSQL)
	if err == nil {
		return sqliteIncarnationMarker(triggerName, triggerSQL), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return "", nil
}

func (d *SQLiteDestination) ensureSQLiteIncarnationMarker(ctx context.Context, queryer sqliteIncarnationQueryer, schemaName, tableName string) (string, error) {
	if marker, err := d.readSQLiteIncarnationMarker(ctx, queryer, schemaName, tableName); err != nil || marker != "" {
		return marker, err
	}
	targetDigest := sha256.Sum256([]byte(destination.CDCTargetKey(strings.ToLower(schemaName), strings.ToLower(tableName))))
	triggerPrefix := "_bruin_cdc_incarnation_" + hex.EncodeToString(targetDigest[:8]) + "_"
	triggerSQLQuery := fmt.Sprintf(`SELECT name, sql FROM %s.sqlite_schema WHERE type = 'trigger' AND name GLOB ? AND tbl_name = ? COLLATE NOCASE ORDER BY name LIMIT 1`, destination.QuoteIdentifier(schemaName))

	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	triggerName := triggerPrefix + hex.EncodeToString(token)
	createTriggerSQL := fmt.Sprintf(
		`CREATE TRIGGER %s.%s AFTER INSERT ON %s WHEN 0 BEGIN SELECT '%s'; END`,
		destination.QuoteIdentifier(schemaName),
		destination.QuoteIdentifier(triggerName),
		destination.QuoteIdentifier(tableName),
		hex.EncodeToString(token),
	)
	if _, err := queryer.ExecContext(ctx, createTriggerSQL); err != nil {
		return "", err
	}
	var triggerSQL string
	if err := queryer.QueryRowContext(ctx, triggerSQLQuery, triggerPrefix+"*", tableName).Scan(&triggerName, &triggerSQL); err != nil {
		return "", err
	}
	return sqliteIncarnationMarker(triggerName, triggerSQL), nil
}

func (d *SQLiteDestination) EnsureCDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	d.incarnationMu.Lock()
	defer d.incarnationMu.Unlock()
	schemaName := schemaOf(table)
	if schemaName == "" {
		schemaName = "main"
	}
	if err := d.ensureSchemaAttached(ctx, schemaName); err != nil {
		return "", false, err
	}
	tableName := extractTableName(table)
	var actualName string
	query := fmt.Sprintf(`SELECT name FROM %s.sqlite_schema WHERE type = 'table' AND name = ? COLLATE NOCASE`, destination.QuoteIdentifier(schemaName))
	if err := d.db.QueryRowContext(ctx, query, tableName).Scan(&actualName); errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	marker, err := d.ensureSQLiteIncarnationMarker(ctx, d.db, schemaName, actualName)
	if err != nil {
		return "", false, err
	}
	return destination.CDCTargetKey(strings.ToLower(schemaName), strings.ToLower(actualName), marker), true, nil
}

func sqliteIncarnationMarker(triggerName, triggerSQL string) string {
	digest := sha256.Sum256([]byte(destination.CDCTargetKey(triggerName, triggerSQL)))
	return hex.EncodeToString(digest[:])
}

func (d *SQLiteDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	if err := d.ensureSchemaAttached(ctx, schemaOf(table)); err != nil {
		return destination.CDCStateFence{}, err
	}
	quotedTable := destination.QuoteTableName(table)
	query := fmt.Sprintf(`SELECT DISTINCT "event_id", "state_generation" FROM %s WHERE "connector_id" = ? AND "state_kind" = 'run' AND "state_generation" = (SELECT MAX("state_generation") FROM %s WHERE "connector_id" = ? AND "state_kind" = 'run') ORDER BY "event_id"`, quotedTable, quotedTable)
	rows, err := d.db.QueryContext(ctx, query, connectorID, connectorID)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
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

func (d *SQLiteDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	if err := d.ensureSchemaAttached(ctx, schemaOf(table)); err != nil {
		return err
	}
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, connectorID)
	placeholders := make([]string, len(eventIDs))
	for i, eventID := range eventIDs {
		placeholders[i] = "?"
		args = append(args, eventID)
	}
	_, err := d.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE "connector_id" = ? AND "event_id" IN (%s)`, destination.QuoteTableName(table), strings.Join(placeholders, ", ")), args...)
	return err
}

// GetScheme returns the primary URI scheme for SQLite.
func (d *SQLiteDestination) GetScheme() string { return "sqlite" }

// GetTableSchema returns the current schema of a table, or nil if table doesn't exist.
func (d *SQLiteDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	schemaName := schemaOf(table)
	tableName := extractTableName(table)
	if err := d.ensureSchemaAttached(ctx, schemaName); err != nil {
		return nil, err
	}
	if schemaName == "" {
		schemaName = "main"
	}

	query := fmt.Sprintf("PRAGMA %s.table_info(%s)", destination.QuoteIdentifier(schemaName), destination.QuoteIdentifier(tableName))
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query table info: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue *string

		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		columns = append(columns, schema.Column{
			Name:         name,
			DataType:     mapSQLiteTypeToSchema(colType),
			Nullable:     notNull == 0,
			IsPrimaryKey: pk > 0,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, nil
	}

	return &schema.TableSchema{
		Name:    tableName,
		Columns: columns,
	}, nil
}

func mapSQLiteTypeToSchema(colType string) schema.DataType {
	colType = strings.ToUpper(strings.TrimSpace(colType))

	switch {
	case colType == "BOOLEAN" || colType == "BOOL":
		return schema.TypeBoolean
	case colType == "INTERVAL":
		return schema.TypeString
	case strings.Contains(colType, "INT"):
		return schema.TypeInt64
	case strings.Contains(colType, "REAL") || strings.Contains(colType, "FLOA") || strings.Contains(colType, "DOUB"):
		return schema.TypeFloat64
	case strings.Contains(colType, "CHAR") || strings.Contains(colType, "CLOB") || strings.Contains(colType, "TEXT"):
		return schema.TypeString
	case strings.Contains(colType, "BLOB"):
		return schema.TypeBinary
	case colType == "NUMERIC" || strings.HasPrefix(colType, "DECIMAL"):
		return schema.TypeDecimal
	case colType == "JSON":
		return schema.TypeJSON
	default:
		return schema.TypeString
	}
}

// quoteColumns returns column names wrapped in double quotes.
func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = fmt.Sprintf(`"%s"`, strings.ReplaceAll(col, `"`, `""`))
	}
	return quoted
}

// filterColumns returns columns excluding those in the exclude list.
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

// buildJoinConditionSQLite builds a SQL join condition for primary keys.
func buildJoinConditionSQLite(keys []string, targetAlias, sourceAlias string) string {
	conditions := make([]string, len(keys))
	for i, key := range keys {
		conditions[i] = fmt.Sprintf(`%s.%s = %s.%s`, targetAlias, destination.QuoteIdentifier(key), sourceAlias, destination.QuoteIdentifier(key))
	}
	return strings.Join(conditions, " AND ")
}

// buildUpdateSetSQLite builds the SET clause for an UPDATE statement.
func buildUpdateSetSQLite(columns []string, sourceAlias string) string {
	sets := make([]string, len(columns))
	for i, col := range columns {
		sets[i] = fmt.Sprintf(`%s = %s.%s`, destination.QuoteIdentifier(col), sourceAlias, destination.QuoteIdentifier(col))
	}
	return strings.Join(sets, ", ")
}

func buildCDCUpdateSetSQLite(columns []string, targetAlias, sourceAlias, unchangedRef string) string {
	sets := make([]string, len(columns))
	for i, col := range columns {
		quoted := destination.QuoteIdentifier(col)
		source := sourceAlias + "." + quoted
		if destination.IsCDCColumn(col) {
			sets[i] = quoted + " = " + source
			continue
		}
		unchanged := fmt.Sprintf(
			"EXISTS (SELECT 1 FROM json_each(COALESCE(%s, '[]')) WHERE value = '%s' COLLATE BINARY)",
			unchangedRef,
			strings.ReplaceAll(col, "'", "''"),
		)
		sets[i] = fmt.Sprintf("%s = CASE WHEN %s THEN %s.%s ELSE %s END", quoted, unchanged, targetAlias, quoted, source)
	}
	return strings.Join(sets, ", ")
}

// buildChangeConditionsSQLite builds change detection conditions using IS NOT.
func buildChangeConditionsSQLite(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "0"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		conditions[i] = fmt.Sprintf(`%s.%s IS NOT %s.%s`, targetAlias, destination.QuoteIdentifier(col), sourceAlias, destination.QuoteIdentifier(col))
	}
	return strings.Join(conditions, " OR ")
}

// parseSQLitePath extracts the file path from a sqlite:// URI
func parseSQLitePath(uri string) (string, error) {
	if !strings.HasPrefix(uri, "sqlite://") {
		return "", fmt.Errorf("invalid sqlite URI: %s", uri)
	}

	path := strings.TrimPrefix(uri, "sqlite://")
	if path == "" {
		return "", fmt.Errorf("empty file path in URI")
	}

	// sqlite:///absolute/path.db -> /absolute/path.db (absolute)
	// sqlite://relative.db -> relative.db (relative, current dir)
	// sqlite://./relative.db -> ./relative.db (relative)
	// If path starts with / but is just /filename.db, treat as relative
	if strings.HasPrefix(path, "/") && !strings.Contains(path[1:], "/") {
		// sqlite:///file.db -> ./file.db (relative to current directory)
		path = "." + path
	}

	return path, nil
}

func extractTableName(table string) string {
	parts := tablename.Split(table)
	return parts[len(parts)-1]
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	var colDefs []string
	for _, col := range columns {
		colType := MapDataTypeToSQLite(col)
		colDefs = append(colDefs, fmt.Sprintf(`%s %s`, destination.QuoteIdentifier(col.Name), colType))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s", table, strings.Join(colDefs, ",\n  "))

	if len(primaryKeys) > 0 {
		quotedKeys := make([]string, len(primaryKeys))
		for i, k := range primaryKeys {
			quotedKeys[i] = destination.QuoteIdentifier(k)
		}
		sql += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(quotedKeys, ", "))
	}

	sql += "\n)"
	return sql
}

// extractValue extracts a Go value from an Arrow array at the given index
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
		frac := (micros % 1000000)
		return fmt.Sprintf("%02d:%02d:%02d.%06d", hours, mins, secs, frac)
	case *array.Timestamp:
		ts := a.Value(idx)
		return ts.ToTime(a.DataType().(*arrow.TimestampType).Unit).Format("2006-01-02 15:04:05.000000")
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case array.ExtensionArray:
		// Handle extension types by extracting the underlying storage value
		storage := a.Storage()
		return extractValue(storage, idx)
	default:
		return fmt.Sprintf("%v", arr)
	}
}
