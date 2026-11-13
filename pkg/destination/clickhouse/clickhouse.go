package clickhouse

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	srcclickhouse "github.com/bruin-data/ingestr/pkg/source/clickhouse"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/shopspring/decimal"
)

type ClickHouseDestination struct {
	conn           driver.Conn
	uri            string
	database       string
	cluster        string
	engineType     string
	engineSettings map[string]string
}

func NewClickHouseDestination() *ClickHouseDestination {
	return &ClickHouseDestination{}
}

func (d *ClickHouseDestination) Schemes() []string {
	return []string{"clickhouse"}
}

func (d *ClickHouseDestination) Connect(ctx context.Context, uri string) error {
	opts, database, cluster, engineType, engineSettings, err := parseClickHouseURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse ClickHouse URI: %w", err)
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open ClickHouse connection: %w", err)
	}

	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	d.conn = conn
	d.uri = uri
	d.database = database
	d.cluster = cluster
	d.engineType = engineType
	d.engineSettings = engineSettings
	config.Debug("[CLICKHOUSE] Connected to database: %s (cluster=%q, engine=%q, settings=%v)", database, cluster, engineType, engineSettings)
	return nil
}

func (d *ClickHouseDestination) Close(ctx context.Context) error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

func (d *ClickHouseDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	return d.prepareTable(ctx, opts, false)
}

func (d *ClickHouseDestination) prepareTable(ctx context.Context, opts destination.PrepareOptions, exclusive bool) error {
	if err := tablename.TwoLevel("clickhouse").CheckName(opts.Table); err != nil {
		return err
	}
	if opts.Schema == nil && !opts.RequirePrimaryKeyMatch {
		return fmt.Errorf("schema is required")
	}

	database, tableName := d.parseTableName(opts.Table)
	if err := d.ensureDatabaseExists(ctx, database); err != nil {
		return fmt.Errorf("failed to ensure database exists: %w", err)
	}

	if opts.DropFirst && !exclusive {
		startDrop := time.Now()
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s%s", quoteIdentifier(database), quoteIdentifier(tableName), onClusterClause(d.cluster))
		if err := d.conn.Exec(ctx, dropSQL); err != nil {
			config.LogFailedQuery(dropSQL, err)
			return fmt.Errorf("failed to drop table: %w", err)
		}
		config.Debug("[CLICKHOUSE] DROP TABLE took %v", time.Since(startDrop))
	}

	if opts.Schema != nil {
		startCreate := time.Now()
		createSQL := buildCreateTableSQLWithOptions(database, tableName, opts.Schema.Columns, opts.PrimaryKeys, opts.CDCKeys, opts.CDCMode, isKeyedCDCTarget(opts), exclusive, d.engineType, d.engineSettings, d.cluster)
		config.Debug("[CLICKHOUSE] CREATE SQL: %s", createSQL)
		if err := d.conn.Exec(ctx, createSQL); err != nil {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("failed to create table: %w", err)
		}
		config.Debug("[CLICKHOUSE] CREATE TABLE took %v", time.Since(startCreate))
	}

	if opts.RequirePrimaryKeyMatch && len(opts.PrimaryKeys) > 0 {
		if err := d.validatePrimaryKey(ctx, "CDC target", opts.Table, opts.PrimaryKeys); err != nil {
			return err
		}
	}

	return nil
}

func (d *ClickHouseDestination) ensureDatabaseExists(ctx context.Context, database string) error {
	if database == "" || database == "default" {
		return nil
	}

	createDBSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s%s", quoteIdentifier(database), onClusterClause(d.cluster))
	if err := d.conn.Exec(ctx, createDBSQL); err != nil {
		config.LogFailedQuery(createDBSQL, err)
		return fmt.Errorf("failed to create database %s: %w", database, err)
	}
	config.Debug("[CLICKHOUSE] Ensured database exists: %s", database)
	return nil
}

func (d *ClickHouseDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeBatch(ctx, records, opts)
}

func (d *ClickHouseDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.writeBatch(ctx, records, opts)
}

func (d *ClickHouseDestination) writeBatch(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	config.Debug("[CLICKHOUSE] Starting write to %s", opts.Table)
	startTotal := time.Now()

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	if opts.CDCExpectedIncarnation == "" {
		database, tableName := d.parseTableName(opts.Table)
		totalRows, err := d.writeBatchesInto(ctx, records, database, tableName)
		if err != nil {
			return err
		}
		totalRate := float64(totalRows) / time.Since(startTotal).Seconds()
		config.Debug("[CLICKHOUSE] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), totalRate)
		return nil
	}

	plan, err := d.prepareCDCShadowPlan(ctx, opts.Table, opts.CDCExpectedIncarnation, true)
	if err != nil {
		return err
	}
	plans := []*cdcShadowPlan{plan}
	defer d.cleanupCDCShadowPlans(ctx, plans)

	totalRows, err := d.writeBatchesInto(ctx, records, plan.shadowDB, plan.shadowName)
	if err != nil {
		return err
	}
	if err := d.publishCDCShadowPlans(ctx, plans); err != nil {
		return err
	}

	totalRate := float64(totalRows) / time.Since(startTotal).Seconds()
	config.Debug("[CLICKHOUSE] Total: %d rows written in %v (%.0f rows/sec)", totalRows, time.Since(startTotal), totalRate)
	return nil
}

func (d *ClickHouseDestination) writeBatchesInto(ctx context.Context, records <-chan source.RecordBatchResult, database, tableName string) (int64, error) {
	var totalRows int64
	batchNum := 0

	for result := range records {
		batchNum++
		if result.Err != nil {
			return totalRows, result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		numRows := record.NumRows()
		numCols := record.NumCols()

		if numRows == 0 {
			record.Release()
			continue
		}

		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			record.Release()
			return totalRows, err
		}

		config.Debug("[CLICKHOUSE] Batch %d: received %d rows, %d cols", batchNum, numRows, numCols)

		startBatch := time.Now()

		columns := make([]string, numCols)
		for i := int64(0); i < numCols; i++ {
			columns[i] = record.ColumnName(int(i))
		}

		insertSQL := buildInsertSQL(database, tableName, columns)
		batch, err := d.conn.PrepareBatch(ctx, insertSQL)
		if err != nil {
			record.Release()
			return totalRows, fmt.Errorf("failed to prepare batch: %w", err)
		}

		for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
			row := make([]interface{}, numCols)
			for colIdx := int64(0); colIdx < numCols; colIdx++ {
				row[colIdx] = extractValue(record.Column(int(colIdx)), int(rowIdx))
			}
			if err := batch.Append(row...); err != nil {
				record.Release()
				return totalRows, fmt.Errorf("failed to append row %d: %w", rowIdx, err)
			}
		}

		if err := batch.Send(); err != nil {
			record.Release()
			return totalRows, fmt.Errorf("failed to send batch: %w", err)
		}

		record.Release()
		totalRows += numRows

		rate := float64(numRows) / time.Since(startBatch).Seconds()
		config.Debug("[CLICKHOUSE] Batch %d: %d rows in %v (%.0f rows/sec)", batchNum, numRows, time.Since(startBatch), rate)
	}

	return totalRows, nil
}

func (d *ClickHouseDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	startSwap := time.Now()

	stagingTable := opts.StagingTable
	targetTable := opts.TargetTable
	stagingDB, stagingName := d.parseTableName(stagingTable)
	targetDB, targetName := d.parseTableName(targetTable)

	if opts.CDCExpectedIncarnation != "" || opts.CDCExpectedStagingIncarnation != "" || opts.CDCExpectedResultIncarnation != "" {
		if err := d.swapTableFenced(ctx, opts); err != nil {
			return err
		}
		config.Debug("[CLICKHOUSE] Fenced table swap completed in %v", time.Since(startSwap))
		return nil
	}

	// Replace only PrepareTables the staging side, so the target database may
	// not exist yet. Both EXCHANGE TABLES and RENAME TABLE require it.
	if err := d.ensureDatabaseExists(ctx, targetDB); err != nil {
		return fmt.Errorf("failed to ensure target database exists: %w", err)
	}

	exchangeSQL := fmt.Sprintf("EXCHANGE TABLES %s.%s AND %s.%s%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName), quoteIdentifier(targetDB), quoteIdentifier(targetName), onClusterClause(d.cluster))
	if err := d.conn.Exec(ctx, exchangeSQL); err != nil {
		config.Debug("[CLICKHOUSE] EXCHANGE TABLES failed, falling back to RENAME: %v", err)

		oldNameCandidate := fmt.Sprintf("%s_old_%d", targetName, time.Now().UnixNano())
		oldName := destination.ShortenIdentifier(oldNameCandidate, oldNameCandidate, destination.MaxIdentifierLength("clickhouse"))

		renameOldSQL := fmt.Sprintf("RENAME TABLE %s.%s TO %s.%s%s", quoteIdentifier(targetDB), quoteIdentifier(targetName), quoteIdentifier(targetDB), quoteIdentifier(oldName), onClusterClause(d.cluster))
		renamedOld := false
		if err := d.conn.Exec(ctx, renameOldSQL); err != nil {
			config.Debug("[CLICKHOUSE] No existing table to rename (this is OK for first run)")
		} else {
			renamedOld = true
		}

		restoreOldSQL := fmt.Sprintf("RENAME TABLE %s.%s TO %s.%s%s", quoteIdentifier(targetDB), quoteIdentifier(oldName), quoteIdentifier(targetDB), quoteIdentifier(targetName), onClusterClause(d.cluster))
		renameNewSQL := fmt.Sprintf("RENAME TABLE %s.%s TO %s.%s%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName), quoteIdentifier(targetDB), quoteIdentifier(targetName), onClusterClause(d.cluster))
		if err := d.conn.Exec(ctx, renameNewSQL); err != nil {
			if renamedOld {
				_ = d.conn.Exec(context.WithoutCancel(ctx), restoreOldSQL)
			}
			config.LogFailedQuery(renameNewSQL, err)
			return fmt.Errorf("failed to rename staging to target: %w", err)
		}

		if renamedOld {
			dropOldSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s%s", quoteIdentifier(targetDB), quoteIdentifier(oldName), onClusterClause(d.cluster))
			_ = d.conn.Exec(context.WithoutCancel(ctx), dropOldSQL)
		}
	}

	config.Debug("[CLICKHOUSE] Table swap completed in %v", time.Since(startSwap))
	return nil
}

// swapTableFenced publishes a CDC full-refresh replacement with a single
// atomic statement and never falls back to multi-step renames, so the target
// name always resolves to a complete table. With an expected target it uses
// EXCHANGE TABLES and verifies the displaced table is the expected
// incarnation, exchanging back untouched otherwise. Without one it uses
// RENAME TABLE, which fails atomically if any table took the target name.
func (d *ClickHouseDestination) swapTableFenced(ctx context.Context, opts destination.SwapOptions) error {
	stagingDB, stagingName := d.parseTableName(opts.StagingTable)
	targetDB, targetName := d.parseTableName(opts.TargetTable)
	stagingRef := fmt.Sprintf("%s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName))
	targetRef := fmt.Sprintf("%s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName))
	changed := fmt.Errorf("ClickHouse CDC target %q physical incarnation changed", opts.TargetTable)

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	targetMeta, targetExists, err := d.readTableMeta(ctx, targetDB, targetName)
	if err != nil {
		return err
	}
	if opts.CDCExpectedIncarnation != "" {
		if !targetExists || destination.CDCTargetKey(targetDB, targetName, targetMeta.incarnationID()) != opts.CDCExpectedIncarnation {
			return changed
		}
	} else if targetExists {
		return changed
	}
	stagingMeta, stagingExists, err := d.readTableMeta(ctx, stagingDB, stagingName)
	if err != nil {
		return err
	}
	if !stagingExists {
		return fmt.Errorf("ClickHouse swap staging table %q does not exist", opts.StagingTable)
	}
	if opts.CDCExpectedStagingIncarnation != "" && destination.CDCTargetKey(stagingDB, stagingName, stagingMeta.incarnationID()) != opts.CDCExpectedStagingIncarnation {
		return fmt.Errorf("ClickHouse CDC target %q physical incarnation changed", opts.StagingTable)
	}
	if err := d.ensureDatabaseExists(ctx, targetDB); err != nil {
		return fmt.Errorf("failed to ensure target database exists: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	if opts.CDCExpectedIncarnation == "" {
		renameSQL := fmt.Sprintf("RENAME TABLE %s TO %s%s", stagingRef, targetRef, onClusterClause(d.cluster))
		if err := d.conn.Exec(ctx, renameSQL); err != nil {
			config.LogFailedQuery(renameSQL, err)
			if _, exists, checkErr := d.readTableMeta(verifyCtx, targetDB, targetName); checkErr == nil && exists {
				return changed
			}
			return fmt.Errorf("failed to rename staging to target: %w", err)
		}
		if opts.CDCExpectedResultIncarnation != "" {
			if err := d.checkCDCIncarnation(verifyCtx, opts.TargetTable, opts.CDCExpectedResultIncarnation); err != nil {
				revertSQL := fmt.Sprintf("RENAME TABLE %s TO %s%s", targetRef, stagingRef, onClusterClause(d.cluster))
				return errors.Join(err, d.conn.Exec(verifyCtx, revertSQL))
			}
		}
		return nil
	}

	exchangeSQL := fmt.Sprintf("EXCHANGE TABLES %s AND %s%s", stagingRef, targetRef, onClusterClause(d.cluster))
	if err := d.conn.Exec(ctx, exchangeSQL); err != nil {
		config.LogFailedQuery(exchangeSQL, err)
		return fmt.Errorf("fenced ClickHouse CDC swap of %s requires EXCHANGE TABLES (Atomic, Replicated, or Shared database): %w", opts.TargetTable, err)
	}
	displaced, displacedExists, verifyErr := d.readTableMeta(verifyCtx, stagingDB, stagingName)
	if verifyErr == nil && (!displacedExists ||
		displaced.physicalID() != targetMeta.physicalID() ||
		destination.CDCTargetKey(targetDB, targetName, displaced.incarnationID()) != opts.CDCExpectedIncarnation) {
		verifyErr = changed
	}
	if verifyErr == nil && opts.CDCExpectedResultIncarnation != "" {
		verifyErr = d.checkCDCIncarnation(verifyCtx, opts.TargetTable, opts.CDCExpectedResultIncarnation)
	}
	if verifyErr != nil {
		if err := d.conn.Exec(verifyCtx, exchangeSQL); err != nil {
			config.LogFailedQuery(exchangeSQL, err)
			return errors.Join(verifyErr, fmt.Errorf("restore ClickHouse CDC target %s after a failed fenced swap; the displaced table is at %s: %w", opts.TargetTable, opts.StagingTable, err))
		}
		return verifyErr
	}
	return nil
}

func (d *ClickHouseDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if destination.HasCDCDeletedColumn(opts.Columns) && len(opts.PrimaryKeys) > 0 {
		return d.mergeCDC(ctx, opts)
	}

	startMerge := time.Now()

	stagingDB, stagingName := d.parseTableName(opts.StagingTable)
	targetDB, targetName := d.parseTableName(opts.TargetTable)
	cols := opts.Columns
	if destination.HasCDCDeletedColumn(cols) {
		cols = destination.DestinationColumns(cols)
	}

	if opts.CDCExpectedIncarnation == "" {
		if err := d.insertStagingInto(ctx, targetDB, targetName, stagingDB, stagingName, cols); err != nil {
			return err
		}
		config.Debug("[CLICKHOUSE MERGE] Merge completed in %v", time.Since(startMerge))
		return nil
	}

	plan, err := d.prepareCDCShadowPlan(ctx, opts.TargetTable, opts.CDCExpectedIncarnation, true)
	if err != nil {
		return err
	}
	plans := []*cdcShadowPlan{plan}
	defer d.cleanupCDCShadowPlans(ctx, plans)

	if err := d.insertStagingInto(ctx, plan.shadowDB, plan.shadowName, stagingDB, stagingName, cols); err != nil {
		return err
	}
	if err := d.publishCDCShadowPlans(ctx, plans); err != nil {
		return err
	}

	config.Debug("[CLICKHOUSE MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *ClickHouseDestination) insertStagingInto(ctx context.Context, targetDB, targetName, stagingDB, stagingName string, columns []string) error {
	quotedColumns := quoteColumns(columns)
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s.%s (%s) SELECT %s FROM %s.%s",
		quoteIdentifier(targetDB), quoteIdentifier(targetName),
		strings.Join(quotedColumns, ", "),
		strings.Join(quotedColumns, ", "),
		quoteIdentifier(stagingDB), quoteIdentifier(stagingName),
	)
	config.Debug("[CLICKHOUSE MERGE] Executing INSERT: %s", insertSQL)

	if err := d.conn.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	optimizeSQL := fmt.Sprintf("OPTIMIZE TABLE %s.%s FINAL", quoteIdentifier(targetDB), quoteIdentifier(targetName))
	if err := d.conn.Exec(ctx, optimizeSQL); err != nil {
		config.LogFailedQuery(optimizeSQL, err)
		config.Debug("[CLICKHOUSE MERGE] OPTIMIZE FINAL completed with note: %v", err)
	}
	return nil
}

// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table for CDC resume.
func (d *ClickHouseDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	db, name := d.parseTableName(table)
	hasColumn, err := d.tableHasColumn(ctx, db, name, destination.CDCLSNColumn)
	if err != nil || !hasColumn {
		return "", err
	}

	query := fmt.Sprintf("SELECT ifNull(MAX(%s), '') FROM %s.%s", quoteIdentifier(destination.CDCLSNColumn), quoteIdentifier(db), quoteIdentifier(name))
	var maxLSN string
	if err := d.conn.QueryRow(ctx, query).Scan(&maxLSN); err != nil {
		if isMissingTableOrDB(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read ClickHouse CDC cursor for %s: %w", table, err)
	}
	return maxLSN, nil
}

func (d *ClickHouseDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	startOp := time.Now()

	deleteSQL, insertSQL, targetDB, targetName := d.buildDeleteInsertStatements(opts)

	config.Debug("[CLICKHOUSE DELETE+INSERT] Executing DELETE: %s", deleteSQL)
	if err := d.conn.Exec(ctx, deleteSQL); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to delete records: %w", err)
	}

	if err := d.waitForMutations(ctx, targetDB, targetName); err != nil {
		return fmt.Errorf("failed waiting for delete mutation: %w", err)
	}

	config.Debug("[CLICKHOUSE DELETE+INSERT] Executing INSERT: %s", insertSQL)
	if err := d.conn.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert records: %w", err)
	}

	config.Debug("[CLICKHOUSE DELETE+INSERT] Delete+Insert completed in %v", time.Since(startOp))
	return nil
}

// SCD2Table performs SCD2 (Slowly Changing Dimensions Type 2) merge logic.
func (d *ClickHouseDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	startOp := time.Now()

	stagingDB, stagingName := d.parseTableName(opts.StagingTable)
	targetDB, targetName := d.parseTableName(opts.TargetTable)

	// Build column comparison for change detection (excluding SCD columns and PKs)
	nonPKColumns := filterColumns(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	changeConditions := buildChangeConditionsClickHouse(nonPKColumns, "target", "source")
	onCondition := buildJoinCondition(opts.PrimaryKeys, "target", "source")
	// Format timestamp as ClickHouse DateTime64 literal
	tsLiteral := fmt.Sprintf("toDateTime64('%s', 6)", opts.Timestamp.Format("2006-01-02 15:04:05.999999"))

	// Replicas re-run mutations against their own copies, so the subqueries need identical data everywhere.
	mutationSettings := ""
	replicated := false
	if d.cluster != "" && strings.EqualFold(d.engineType, "replicated_merge_tree") {
		var engine string
		if err := d.conn.QueryRow(ctx, "SELECT engine FROM system.tables WHERE database = ? AND name = ?", targetDB, targetName).Scan(&engine); err != nil {
			return fmt.Errorf("failed to read engine of %s.%s: %w", targetDB, targetName, err)
		}
		replicated = strings.HasPrefix(engine, "Replicated")
	}
	if replicated {
		for _, table := range []string{
			fmt.Sprintf("%s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName)),
			fmt.Sprintf("%s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName)),
		} {
			syncSQL := fmt.Sprintf("SYSTEM SYNC REPLICA%s %s", onClusterClause(d.cluster), table)
			if err := d.conn.Exec(ctx, syncSQL); err != nil {
				config.LogFailedQuery(syncSQL, err)
				return fmt.Errorf("failed to sync replicas of %s: %w", table, err)
			}
		}
		mutationSettings = " SETTINGS allow_nondeterministic_mutations = 1, mutations_sync = 2"
	}

	// Step 1: Close changed records (update _scd_valid_to and _scd_is_current)
	// ClickHouse doesn't support UPDATE...FROM, so we use ALTER TABLE UPDATE with a subquery
	// that finds PKs of records that exist in staging AND have changed
	pkSelectTarget := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		pkSelectTarget[i] = fmt.Sprintf("target.%s", quoteIdentifier(pk))
	}
	updateSQL := fmt.Sprintf(
		`
		ALTER TABLE %s.%s%s
		UPDATE
			_scd_valid_to = %s,
			_scd_is_current = 0
		WHERE _scd_is_current = 1
		  AND (%s) IN (
			SELECT %s FROM %s.%s AS target
			INNER JOIN %s.%s AS source ON %s
			WHERE target._scd_is_current = 1 AND (%s)
		  )%s`,
		quoteIdentifier(targetDB), quoteIdentifier(targetName), onClusterClause(d.cluster),
		tsLiteral,
		strings.Join(quoteColumns(opts.PrimaryKeys), ", "),
		strings.Join(pkSelectTarget, ", "), quoteIdentifier(targetDB), quoteIdentifier(targetName),
		quoteIdentifier(stagingDB), quoteIdentifier(stagingName), onCondition,
		changeConditions, mutationSettings,
	)
	config.Debug("[CLICKHOUSE SCD2] Step 1 - Close changed records: %s", updateSQL)

	if err := d.conn.Exec(ctx, updateSQL); err != nil {
		config.LogFailedQuery(updateSQL, err)
		return fmt.Errorf("failed to close changed records: %w", err)
	}

	// Wait for mutations
	if err := d.waitForMutations(ctx, targetDB, targetName); err != nil {
		return fmt.Errorf("failed waiting for SCD2 update mutation: %w", err)
	}

	// Step 2: Soft-delete missing records (only if no incremental_key)
	if opts.IncrementalKey == "" {
		softDeleteSQL := fmt.Sprintf(
			`
			ALTER TABLE %s.%s%s
			UPDATE
				_scd_valid_to = %s,
				_scd_is_current = 0
			WHERE _scd_is_current = 1
			  AND (%s) NOT IN (SELECT %s FROM %s.%s)%s`,
			quoteIdentifier(targetDB), quoteIdentifier(targetName), onClusterClause(d.cluster),
			tsLiteral,
			strings.Join(quoteColumns(opts.PrimaryKeys), ", "),
			strings.Join(quoteColumns(opts.PrimaryKeys), ", "), quoteIdentifier(stagingDB), quoteIdentifier(stagingName),
			mutationSettings,
		)
		config.Debug("[CLICKHOUSE SCD2] Step 2 - Soft-delete missing: %s", softDeleteSQL)

		if err := d.conn.Exec(ctx, softDeleteSQL); err != nil {
			config.LogFailedQuery(softDeleteSQL, err)
			return fmt.Errorf("failed to soft-delete missing records: %w", err)
		}

		if err := d.waitForMutations(ctx, targetDB, targetName); err != nil {
			return fmt.Errorf("failed waiting for SCD2 soft-delete mutation: %w", err)
		}
	}

	// Step 3: Insert new versions + net-new records
	allColumns := destination.AppendSCD2Columns(opts.Columns)
	quotedColumns := quoteColumns(allColumns)
	sourceColumns := make([]string, len(quotedColumns))
	for i, col := range quotedColumns {
		sourceColumns[i] = "source." + col
	}

	// ANTI JOIN works with either join_use_nulls setting; unmatched keys normally contain defaults.
	insertSQL := fmt.Sprintf(
		`
		INSERT INTO %s.%s (%s)
		SELECT %s FROM %s.%s AS source
		LEFT ANTI JOIN %s.%s AS target
		  ON %s AND target._scd_is_current = 1`,
		quoteIdentifier(targetDB), quoteIdentifier(targetName),
		strings.Join(quotedColumns, ", "),
		strings.Join(sourceColumns, ", "), quoteIdentifier(stagingDB), quoteIdentifier(stagingName),
		quoteIdentifier(targetDB), quoteIdentifier(targetName),
		onCondition,
	)
	config.Debug("[CLICKHOUSE SCD2] Step 3 - Insert new versions: %s", insertSQL)

	if err := d.conn.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to insert new versions: %w", err)
	}

	config.Debug("[CLICKHOUSE SCD2] SCD2 merge completed in %v", time.Since(startOp))
	return nil
}

const (
	mutationWaitAttempts = 60
	mutationWaitInterval = 500 * time.Millisecond
)

type mutationStatusFunc func(context.Context) (pending uint64, failure string, err error)

func (d *ClickHouseDestination) waitForMutations(ctx context.Context, database, tableName string) error {
	waitSQL := "SELECT count(), anyIf(latest_fail_reason, latest_fail_reason != '') FROM system.mutations WHERE database = ? AND table = ? AND is_done = 0"
	status := func(ctx context.Context) (uint64, string, error) {
		rows, err := d.conn.Query(ctx, waitSQL, database, tableName)
		if err != nil {
			return 0, "", fmt.Errorf("failed to query ClickHouse mutation status: %w", err)
		}
		if !rows.Next() {
			rowsErr := rows.Err()
			closeErr := rows.Close()
			if rowsErr != nil {
				return 0, "", fmt.Errorf("failed to read ClickHouse mutation status: %w", rowsErr)
			}
			if closeErr != nil {
				return 0, "", fmt.Errorf("failed to close ClickHouse mutation status rows: %w", closeErr)
			}
			return 0, "", errors.New("ClickHouse mutation status query returned no rows")
		}
		var pending uint64
		var failure string
		if err := rows.Scan(&pending, &failure); err != nil {
			_ = rows.Close()
			return 0, "", fmt.Errorf("failed to scan ClickHouse mutation status: %w", err)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return 0, "", fmt.Errorf("failed to read ClickHouse mutation status: %w", err)
		}
		if err := rows.Close(); err != nil {
			return 0, "", fmt.Errorf("failed to close ClickHouse mutation status rows: %w", err)
		}
		return pending, failure, nil
	}
	if err := waitForMutationCompletion(ctx, mutationWaitAttempts, mutationWaitInterval, status); err != nil {
		return fmt.Errorf("ClickHouse mutation for %s.%s did not complete: %w", database, tableName, err)
	}
	return nil
}

func waitForMutationCompletion(ctx context.Context, attempts int, interval time.Duration, status mutationStatusFunc) error {
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		pending, failure, err := status(ctx)
		if err != nil {
			return err
		}
		if failure != "" {
			return fmt.Errorf("mutation failed: %s", failure)
		}
		if pending == 0 {
			return nil
		}
		if attempt == attempts-1 {
			break
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("timed out with mutations still pending after %d attempts", attempts)
}

func (d *ClickHouseDestination) DropTable(ctx context.Context, table string) error {
	database, tableName := d.parseTableName(table)
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s%s", quoteIdentifier(database), quoteIdentifier(tableName), onClusterClause(d.cluster))
	if err := d.conn.Exec(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	config.Debug("[CLICKHOUSE] Dropped table: %s", table)
	return nil
}

func (d *ClickHouseDestination) TruncateTable(ctx context.Context, table string) error {
	database, tableName := d.parseTableName(table)
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s.%s%s", quoteIdentifier(database), quoteIdentifier(tableName), onClusterClause(d.cluster))
	if err := d.conn.Exec(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	config.Debug("[CLICKHOUSE] Truncated table: %s", table)
	return nil
}

func (d *ClickHouseDestination) InsertFromStaging(ctx context.Context, opts destination.InsertFromStagingOptions) error {
	return d.MergeTable(ctx, destination.MergeOptions{
		StagingTable: opts.StagingTable,
		TargetTable:  opts.TargetTable,
		Columns:      destination.DestinationColumns(opts.Columns),
	})
}

func (d *ClickHouseDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	err := d.conn.Exec(ctx, sql, args...)
	if err != nil {
		config.LogFailedQuery(sql, err)
	}
	return err
}

func (d *ClickHouseDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	_ = ctx
	return nil, errors.New("clickhouse destination does not support transactions")
}

func (d *ClickHouseDestination) SupportsReplaceStrategy() bool      { return true }
func (d *ClickHouseDestination) SupportsAppendStrategy() bool       { return true }
func (d *ClickHouseDestination) SupportsMergeStrategy() bool        { return true }
func (d *ClickHouseDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *ClickHouseDestination) SupportsSCD2Strategy() bool         { return true }
func (d *ClickHouseDestination) SupportsAtomicSwap() bool           { return true }

func (d *ClickHouseDestination) GetScheme() string { return "clickhouse" }

func (d *ClickHouseDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	tableSchema, _, err := d.readTableColumns(ctx, table)
	return tableSchema, err
}

// readTableColumns returns the table schema and each column's native
// ClickHouse type keyed by lower-cased column name.
func (d *ClickHouseDestination) readTableColumns(ctx context.Context, table string) (*schema.TableSchema, map[string]string, error) {
	database, tableName := d.parseTableName(table)

	query := `
		SELECT name, type, is_in_primary_key
		FROM system.columns
		WHERE database = ? AND table = ?
		ORDER BY position`

	rows, err := d.conn.Query(ctx, query, database, tableName)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, nil, fmt.Errorf("failed to read table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	var fallbackPKs []string
	nativeTypes := make(map[string]string)
	for rows.Next() {
		var colName, colType string
		var inPK uint8
		if err := rows.Scan(&colName, &colType, &inPK); err != nil {
			return nil, nil, fmt.Errorf("failed to scan column: %w", err)
		}
		nativeTypes[strings.ToLower(colName)] = colType

		dataType, precision, scale, arrayType := srcclickhouse.MapClickHouseToDataType(colType)
		// The shared mapper collapses every DateTime64 to TypeTimestamp, but the
		// destination writes TypeTimestampTZ as DateTime64(..., 'UTC'); keep that
		// distinction so a timezone-aware column reads back as it was written.
		if dataType == schema.TypeTimestamp && strings.Contains(strings.ToUpper(colType), "UTC") {
			dataType = schema.TypeTimestampTZ
		}
		if inPK > 0 {
			fallbackPKs = append(fallbackPKs, colName)
		}
		columns = append(columns, schema.Column{
			Name:         colName,
			DataType:     dataType,
			Precision:    precision,
			Scale:        scale,
			ArrayType:    arrayType,
			Nullable:     strings.Contains(strings.ToLower(colType), "nullable"),
			IsPrimaryKey: inPK > 0,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, nil, nil
	}

	primaryKeys := fallbackPKs
	if len(fallbackPKs) > 0 {
		var pkExpr string
		if err := d.conn.QueryRow(ctx, "SELECT primary_key FROM system.tables WHERE database = ? AND name = ?", database, tableName).Scan(&pkExpr); err == nil {
			if parsed := parseClickHouseKeyList(pkExpr); len(parsed) > 0 {
				primaryKeys = parsed
			}
		}
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      database,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nativeTypes, nil
}

func parseClickHouseURI(uri string) (*clickhouse.Options, string, string, string, map[string]string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, "", "", "", nil, fmt.Errorf("invalid URI: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		host = "localhost"
	}

	port := parsed.Port()
	if port == "" {
		port = "9000"
	}

	database := strings.TrimPrefix(parsed.Path, "/")
	if database == "" {
		database = "default"
	}

	username := ""
	password := ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	if username == "" {
		username = "default"
	}

	opts := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%s", host, port)},
		Auth: clickhouse.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
	}

	query := parsed.Query()
	secure := query.Get("secure")
	if secure == "true" || secure == "1" {
		opts.TLS = &tls.Config{}
	}

	skipVerify := query.Get("skip_verify")
	if (skipVerify == "true" || skipVerify == "1") && opts.TLS != nil {
		opts.TLS.InsecureSkipVerify = true
	}

	engineType := query.Get("engine")
	cluster := query.Get("cluster")
	engineSettings := map[string]string{}
	for key, values := range query {
		if !strings.HasPrefix(key, "engine.") || len(values) == 0 {
			continue
		}
		engineSettings[strings.TrimPrefix(key, "engine.")] = values[0]
	}

	return opts, database, cluster, engineType, engineSettings, nil
}

func (d *ClickHouseDestination) parseTableName(table string) (string, string) {
	parts := tablename.Split(table)
	if len(parts) >= 2 {
		return parts[len(parts)-2], parts[len(parts)-1]
	}
	if len(parts) == 1 && parts[0] != "" {
		return d.database, parts[0]
	}
	return d.database, table
}

func (d *ClickHouseDestination) tableRef(table string) string {
	database, tableName := d.parseTableName(table)
	return fmt.Sprintf("%s.%s", quoteIdentifier(database), quoteIdentifier(tableName))
}

func (d *ClickHouseDestination) ListStagingTables(ctx context.Context, schemaName, prefix string) ([]string, error) {
	database := schemaName
	if database == "" {
		database = d.database
	}
	rows, err := d.conn.Query(ctx, "SELECT name FROM system.tables WHERE database = ? AND startsWith(name, ?) ORDER BY name", database, prefix)
	if err != nil {
		return nil, fmt.Errorf("list ClickHouse staging tables in %s: %w", database, err)
	}
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func buildCreateTableSQL(database, table string, columns []schema.Column, primaryKeys []string, engineType string, engineSettings map[string]string, cluster string) string {
	return buildCreateTableSQLWithOptions(database, table, columns, primaryKeys, nil, false, false, false, engineType, engineSettings, cluster)
}

func buildCreateTableSQLWithOptions(database, table string, columns []schema.Column, primaryKeys, cdcKeys []string, cdcMode, keyedCDCTarget, exclusive bool, engineType string, engineSettings map[string]string, cluster string) string {
	pkSet := make(map[string]bool, len(primaryKeys))
	for _, pk := range primaryKeys {
		pkSet[strings.ToLower(pk)] = true
	}
	cdcKeySet := make(map[string]bool, len(cdcKeys))
	for _, key := range cdcKeys {
		cdcKeySet[strings.ToLower(key)] = true
	}

	var colDefs []string
	for _, col := range columns {
		lower := strings.ToLower(col.Name)
		isPK := pkSet[lower]
		if cdcMode && !isPK && !cdcKeySet[lower] && !destination.IsCDCColumn(col.Name) && !destination.IsCDCStagingOnlyColumn(col.Name) {
			col.Nullable = true
		}
		if cdcMode && (isPK || cdcKeySet[lower]) {
			col.Nullable = false
		}
		colType := mapDataTypeForColumn(col, isPK)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col.Name), colType))
	}

	engine, isMergeTree := validateEngineType(engineType, len(primaryKeys) > 0)
	if keyedCDCTarget && len(primaryKeys) > 0 {
		engine = replacingEngine(engine)
	}

	createKeyword := "CREATE TABLE IF NOT EXISTS"
	if exclusive {
		createKeyword = "CREATE TABLE"
	}

	parts := []string{
		fmt.Sprintf("%s %s.%s%s (\n  %s\n) ENGINE = %s",
			createKeyword, quoteIdentifier(database), quoteIdentifier(table), onClusterClause(cluster), strings.Join(colDefs, ",\n  "), engine),
	}

	if isMergeTree {
		var orderBy string
		if len(primaryKeys) > 0 {
			quotedKeys := make([]string, len(primaryKeys))
			for i, k := range primaryKeys {
				quotedKeys[i] = quoteIdentifier(k)
			}
			orderBy = strings.Join(quotedKeys, ", ")
		} else {
			orderBy = "tuple()"
		}
		parts = append(parts, fmt.Sprintf("ORDER BY (%s)", orderBy))
	}

	if extra := buildEngineSettingsClause(engineSettings); extra != "" {
		parts = append(parts, extra)
	}

	return strings.Join(parts, "\n")
}

// isKeyedCDCTarget reports whether a table will receive keyed CDC merges, which
// rely on ReplacingMergeTree collapsing rows by key. Soft-delete full refreshes
// are not CDCMode but still carry _cdc_deleted; append keeps every change row.
func isKeyedCDCTarget(opts destination.PrepareOptions) bool {
	if len(opts.PrimaryKeys) == 0 || opts.Strategy == string(config.StrategyAppend) {
		return false
	}
	if opts.CDCMode || opts.RequirePrimaryKeyMatch {
		return true
	}
	for _, col := range opts.Schema.Columns {
		if col.Name == destination.CDCDeletedColumn {
			return true
		}
	}
	return false
}

// replacingEngine maps a MergeTree-family engine to its replacing variant so
// keyed CDC targets also work with engine=replicated_merge_tree or shared_merge_tree.
func replacingEngine(engine string) string {
	switch engine {
	case "MergeTree()":
		return "ReplacingMergeTree()"
	case "ReplicatedMergeTree()":
		return "ReplicatedReplacingMergeTree()"
	case "SharedMergeTree()":
		return "SharedReplacingMergeTree()"
	}
	return engine
}

func onClusterClause(cluster string) string {
	if cluster == "" {
		return ""
	}
	return " ON CLUSTER " + quoteIdentifier(cluster)
}

var engineTypeMap = map[string]struct {
	name      string
	mergeTree bool
}{
	"merge_tree":            {"MergeTree()", true},
	"replacing_merge_tree":  {"ReplacingMergeTree()", true},
	"shared_merge_tree":     {"SharedMergeTree()", true},
	"replicated_merge_tree": {"ReplicatedMergeTree()", true},
}

func validateEngineType(engineType string, hasPrimaryKeys bool) (string, bool) {
	if engineType != "" {
		if e, ok := engineTypeMap[strings.ToLower(engineType)]; ok {
			// Without a sorting key ReplacingMergeTree collapses every row into one.
			if !hasPrimaryKeys && e.name == "ReplacingMergeTree()" {
				return "MergeTree()", true
			}
			return e.name, e.mergeTree
		}
		valid := make([]string, 0, len(engineTypeMap))
		for k := range engineTypeMap {
			valid = append(valid, k)
		}
		sort.Strings(valid)
		output.Warnf("[WARNING] unsupported ClickHouse engine %q, defaulting based on primary keys (valid: %s)\n", engineType, strings.Join(valid, ", "))
	}
	if hasPrimaryKeys {
		return "ReplacingMergeTree()", true
	}
	return "MergeTree()", true
}

var engineSettingKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func buildEngineSettingsClause(engineSettings map[string]string) string {
	if len(engineSettings) == 0 {
		return ""
	}
	keys := make([]string, 0, len(engineSettings))
	for k := range engineSettings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		if !engineSettingKeyRE.MatchString(k) {
			config.Debug("[CLICKHOUSE] skipping invalid engine setting key: %q", k)
			continue
		}
		v := engineSettings[k]
		if _, err := strconv.ParseInt(v, 10, 64); err == nil {
			pairs = append(pairs, fmt.Sprintf("%s = %s", k, v))
			continue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			pairs = append(pairs, fmt.Sprintf("%s = %s", k, v))
			continue
		}
		if lv := strings.ToLower(v); lv == "true" || lv == "false" {
			pairs = append(pairs, fmt.Sprintf("%s = %s", k, lv))
			continue
		}
		escaped := strings.NewReplacer(
			`\`, `\\`,
			`'`, `\'`,
		).Replace(v)
		pairs = append(pairs, fmt.Sprintf("%s = '%s'", k, escaped))
	}
	if len(pairs) == 0 {
		return ""
	}
	return "SETTINGS " + strings.Join(pairs, ", ")
}

func mapDataTypeForColumn(col schema.Column, isPrimaryKey bool) string {
	if isPrimaryKey {
		return mapBaseType(col)
	}
	return MapDataTypeToClickHouse(col)
}

func buildInsertSQL(database, table string, columns []string) string {
	return fmt.Sprintf("INSERT INTO %s.%s (%s)", quoteIdentifier(database), quoteIdentifier(table), strings.Join(quoteColumns(columns), ", "))
}

func (d *ClickHouseDestination) buildDeleteInsertStatements(opts destination.DeleteInsertOptions) (string, string, string, string) {
	stagingDB, stagingName := d.parseTableName(opts.StagingTable)
	targetDB, targetName := d.parseTableName(opts.TargetTable)

	quotedColumns := quoteColumns(opts.Columns)
	colList := strings.Join(quotedColumns, ", ")
	stagingFQN := fmt.Sprintf("%s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName))
	targetFQN := fmt.Sprintf("%s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName))
	incrementalKey := quoteIdentifier(opts.IncrementalKey)

	deleteSQL := fmt.Sprintf(
		"ALTER TABLE %s%s DELETE WHERE %s >= %s AND %s <= %s",
		targetFQN,
		onClusterClause(d.cluster),
		incrementalKey,
		formatClickHouseLiteral(opts.IntervalStart, opts.IncrementalKeyType),
		incrementalKey,
		formatClickHouseLiteral(opts.IntervalEnd, opts.IncrementalKeyType),
	)

	selectClause := destination.DedupStagingSelect(colList, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), stagingFQN, incrementalKey)
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) %s", targetFQN, colList, selectClause)

	return deleteSQL, insertSQL, targetDB, targetName
}

func formatClickHouseLiteral(value interface{}, dataType schema.DataType) string {
	if p, ok := value.(*time.Time); ok {
		if p == nil {
			return "NULL"
		}
		value = *p
	}
	switch v := value.(type) {
	case time.Time:
		switch dataType {
		case schema.TypeDate:
			return fmt.Sprintf("toDate('%s')", v.Format("2006-01-02"))
		case schema.TypeTimestamp, schema.TypeTimestampTZ:
			return fmt.Sprintf("toDateTime64('%s', 6)", v.Format("2006-01-02 15:04:05.000000"))
		default:
			return fmt.Sprintf("'%s'", escapeClickHouseString(v.Format(time.RFC3339Nano)))
		}
	case string:
		switch dataType {
		case schema.TypeDate:
			return fmt.Sprintf("toDate('%s')", escapeClickHouseString(v))
		case schema.TypeTimestamp, schema.TypeTimestampTZ:
			return fmt.Sprintf("toDateTime64('%s', 6)", escapeClickHouseString(v))
		case schema.TypeString, schema.TypeUUID:
			return fmt.Sprintf("'%s'", escapeClickHouseString(v))
		default:
			return fmt.Sprintf("'%s'", escapeClickHouseString(v))
		}
	case bool:
		if v {
			return "1"
		}
		return "0"
	}

	switch dataType {
	case schema.TypeString, schema.TypeUUID:
		return fmt.Sprintf("'%s'", escapeClickHouseString(fmt.Sprint(value)))
	default:
		return fmt.Sprint(value)
	}
}

func escapeClickHouseString(value string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
	).Replace(value)
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteIdentifier(col)
	}
	return quoted
}

func quoteIdentifier(s string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(s, "`", "``"))
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
		conditions[i] = fmt.Sprintf("%s.%s = %s.%s", targetAlias, quoteIdentifier(key), sourceAlias, quoteIdentifier(key))
	}
	return strings.Join(conditions, " AND ")
}

func buildChangeConditionsClickHouse(columns []string, targetAlias, sourceAlias string) string {
	if len(columns) == 0 {
		return "0"
	}
	conditions := make([]string, len(columns))
	for i, col := range columns {
		qc := quoteIdentifier(col)
		conditions[i] = fmt.Sprintf("NOT (ifNull(%s.%s = %s.%s, 0) OR (%s.%s IS NULL AND %s.%s IS NULL))",
			targetAlias, qc, sourceAlias, qc,
			targetAlias, qc, sourceAlias, qc)
	}
	return strings.Join(conditions, " OR ")
}

func extractValue(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		// ClickHouse arrays can't be NULL, so a NULL list is written as empty.
		if _, ok := arr.(*array.List); ok {
			return []interface{}{}
		}
		return nil
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx)
	case *array.Int8:
		return a.Value(idx)
	case *array.Int16:
		return a.Value(idx)
	case *array.Int32:
		return a.Value(idx)
	case *array.Int64:
		return a.Value(idx)
	case *array.Uint8:
		return a.Value(idx)
	case *array.Uint16:
		return a.Value(idx)
	case *array.Uint32:
		return a.Value(idx)
	case *array.Uint64:
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
		return a.Value(idx).ToTime()
	case *array.Date64:
		return a.Value(idx).ToTime()
	case *array.Time64:
		micros := int64(a.Value(idx))
		return time.Duration(micros) * time.Microsecond
	case *array.Timestamp:
		ts := a.Value(idx)
		return ts.ToTime(arrow.Microsecond)
	case *array.Decimal128:
		v := a.Value(idx)
		dt := a.DataType().(*arrow.Decimal128Type)
		return decimal.NewFromBigInt(v.BigInt(), -dt.Scale)
	case *array.Decimal256:
		v := a.Value(idx)
		dt := a.DataType().(*arrow.Decimal256Type)
		return decimal.NewFromBigInt(v.BigInt(), -dt.Scale)
	case array.ExtensionArray:
		storage := a.Storage()
		return extractValue(storage, idx)
	case *array.List:
		start, end := a.ValueOffsets(idx)
		listValues := a.ListValues()
		result := make([]interface{}, 0, end-start)
		for i := int(start); i < int(end); i++ {
			result = append(result, extractValue(listValues, i))
		}
		return result
	default:
		return fmt.Sprintf("%v", arr)
	}
}
