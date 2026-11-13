package redshift

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/source"
)

type RedshiftDestination struct {
	*postgres.PostgresDestination
	db                 redshiftDB
	dbOIDMu            sync.Mutex
	dbOID              int64
	evolveMu           sync.Mutex
	inFlightGuards     map[string]struct{}
	leaseTTL           time.Duration
	leaseRenewInterval time.Duration
	nowFn              func() time.Time
	openDB             func(ctx context.Context, uri string) (redshiftDB, error)
}

func (d *RedshiftDestination) now() time.Time {
	if d != nil && d.nowFn != nil {
		return d.nowFn()
	}
	return time.Now()
}

func NewRedshiftDestination() *RedshiftDestination {
	return &RedshiftDestination{
		PostgresDestination: &postgres.PostgresDestination{DefaultSchema: "public"},
		leaseTTL:            defaultRedshiftLeaseTTL,
		leaseRenewInterval:  defaultRedshiftLeaseRenewInterval,
	}
}

func (d *RedshiftDestination) Schemes() []string {
	return []string{"redshift", "redshift+psycopg2"}
}

func drainRecordBatches(records <-chan source.RecordBatchResult) {
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
}

func (d *RedshiftDestination) verifyCDCIncarnation(ctx context.Context, queryer redshiftQueryer, schemaName, tableName, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return nil
	}
	inc, exists, err := d.targetIncarnationInTx(ctx, queryer, schemaName, tableName)
	if err != nil {
		return err
	}
	if !exists || inc != expectedIncarnation {
		return fmt.Errorf("redshift CDC target %q physical incarnation changed", table)
	}
	return nil
}

func (d *RedshiftDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		drainRecordBatches(records)
		return err
	}

	var (
		db        redshiftDB
		targetRef string
	)
	resolveTarget := func() error {
		if db != nil {
			return nil
		}
		conn, err := d.requireDB()
		if err != nil {
			return err
		}
		if err := d.recoverInterruptedSchemaEvolutions(ctx, conn); err != nil {
			return err
		}
		schemaName, tableName, err := d.resolveSchemaTable(ctx, conn, opts.Table)
		if err != nil {
			return err
		}
		db = conn
		targetRef = quoteTable(schemaName, tableName)
		if err := d.verifyCDCIncarnation(ctx, db, schemaName, tableName, opts.Table, opts.CDCExpectedIncarnation); err != nil {
			return err
		}
		return nil
	}

	if opts.CDCExpectedIncarnation != "" {
		if err := resolveTarget(); err != nil {
			drainRecordBatches(records)
			return err
		}
	}

	for result := range records {
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			drainRecordBatches(records)
			return result.Err
		}
		record := result.Batch
		if record == nil {
			continue
		}

		numRows := int(record.NumRows())
		numCols := int(record.NumCols())

		if numRows == 0 {
			record.Release()
			continue
		}

		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			record.Release()
			drainRecordBatches(records)
			return err
		}
		if err := resolveTarget(); err != nil {
			record.Release()
			drainRecordBatches(records)
			return err
		}

		columns := make([]string, numCols)
		for i := 0; i < numCols; i++ {
			columns[i] = record.ColumnName(i)
		}

		const maxParams = 65535
		rowsPerInsert := maxParams / max(1, numCols)
		if rowsPerInsert > 1000 {
			rowsPerInsert = 1000
		}
		if rowsPerInsert < 1 {
			rowsPerInsert = 1
		}

		writeChunks := func(execFn func(sql string, args ...any) error) error {
			for offset := 0; offset < numRows; offset += rowsPerInsert {
				end := min(offset+rowsPerInsert, numRows)
				sql, args := buildInsertSQL(targetRef, columns, record, offset, end)
				if err := execFn(sql, args...); err != nil {
					return fmt.Errorf("failed to insert batch: %w", err)
				}
			}
			return nil
		}

		var err error
		if opts.CDCExpectedIncarnation != "" {
			err = d.withTx(ctx, func(tx redshiftTx) error {
				if _, _, _, err := d.lockAndValidateCDCIncarnation(ctx, tx, opts.Table, opts.CDCExpectedIncarnation); err != nil {
					return err
				}
				if err := writeChunks(func(sql string, args ...any) error {
					_, execErr := tx.Exec(ctx, sql, args...)
					return execErr
				}); err != nil {
					return err
				}
				return source.ConnectorLeaseLoss(ctx)
			})
		} else {
			err = writeChunks(func(sql string, args ...any) error {
				_, execErr := db.Exec(ctx, sql, args...)
				return execErr
			})
		}
		record.Release()
		if err != nil {
			drainRecordBatches(records)
			return err
		}
	}

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if opts.CDCExpectedIncarnation != "" {
		schemaName, tableName, err := d.resolveSchemaTable(ctx, db, opts.Table)
		if err != nil {
			return err
		}
		return d.verifyCDCIncarnation(ctx, db, schemaName, tableName, opts.Table, opts.CDCExpectedIncarnation)
	}
	return nil
}

func (d *RedshiftDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	// Redshift doesn't support PostgreSQL's COPY FROM STDIN protocol; use the INSERT-based writer.
	return d.Write(ctx, records, opts)
}

func buildInsert(table string, columns []string, record interface {
	NumRows() int64
	NumCols() int64
	Column(int) arrow.Array
}, startRow, endRow int,
) (string, []interface{}) {
	return buildInsertSQL(destination.QuoteTableName(table), columns, record, startRow, endRow)
}

func buildInsertSQL(targetRef string, columns []string, record interface {
	NumRows() int64
	NumCols() int64
	Column(int) arrow.Array
}, startRow, endRow int,
) (string, []interface{}) {
	var b strings.Builder

	b.WriteString("INSERT INTO ")
	b.WriteString(targetRef)
	b.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(`"`)
		b.WriteString(col)
		b.WriteString(`"`)
	}
	b.WriteString(") VALUES ")

	numCols := len(columns)
	args := make([]interface{}, 0, (endRow-startRow)*numCols)
	argIdx := 1

	for r := startRow; r < endRow; r++ {
		if r > startRow {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for c := 0; c < numCols; c++ {
			if c > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "$%d", argIdx)
			argIdx++
			args = append(args, arrowutil.Value(record.Column(c), r))
		}
		b.WriteString(")")
	}

	return b.String(), args
}

func (d *RedshiftDestination) GetScheme() string { return "redshift" }

type mergeParts struct {
	dedupSource    string
	joinCondition  string
	updateSets     []string
	destQuoted     []string
	destSourceCols []string
	hasCDCDeleted  bool
}

func (d *RedshiftDestination) buildMergeSQL(stagingTable, targetTable string, primaryKeys, allColumns []string) string {
	parts := buildMergeParts(stagingTable, primaryKeys, allColumns)
	var sql strings.Builder
	fmt.Fprintf(&sql, "MERGE INTO %s AS target\n", destination.QuoteTableName(targetTable))
	fmt.Fprintf(&sql, "USING %s AS source\n", parts.dedupSource)
	fmt.Fprintf(&sql, "ON %s\n", parts.joinCondition)

	if parts.hasCDCDeleted {
		if len(parts.updateSets) > 0 {
			sql.WriteString(`WHEN MATCHED AND source."_cdc_deleted" = false THEN` + "\n")
			fmt.Fprintf(&sql, "  UPDATE SET %s\n", strings.Join(parts.updateSets, ", "))
		}

		sql.WriteString(`WHEN MATCHED AND source."_cdc_deleted" = true THEN` + "\n")
		sql.WriteString(`  UPDATE SET "_cdc_deleted" = true, "_cdc_lsn" = source."_cdc_lsn", "_cdc_synced_at" = source."_cdc_synced_at"` + "\n")

		sql.WriteString(`WHEN NOT MATCHED AND source."_cdc_deleted" = false THEN` + "\n")
		fmt.Fprintf(&sql, "  INSERT (%s)\n", strings.Join(parts.destQuoted, ", "))
		fmt.Fprintf(&sql, "  VALUES (%s)", strings.Join(parts.destSourceCols, ", "))
	} else {
		if len(parts.updateSets) > 0 {
			sql.WriteString("WHEN MATCHED THEN\n")
			fmt.Fprintf(&sql, "  UPDATE SET %s\n", strings.Join(parts.updateSets, ", "))
		}

		sql.WriteString("WHEN NOT MATCHED THEN\n")
		fmt.Fprintf(&sql, "  INSERT (%s)\n", strings.Join(parts.destQuoted, ", "))
		fmt.Fprintf(&sql, "  VALUES (%s)", strings.Join(parts.destSourceCols, ", "))
	}

	return sql.String()
}

func buildMergeParts(stagingTable string, primaryKeys, allColumns []string) mergeParts {
	destColumns := destination.DestinationColumns(allColumns)

	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf(`target.%s = source.%s`, destination.QuoteIdentifier(pk), destination.QuoteIdentifier(pk))
	}

	pkMap := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	hasCDCDeleted := slices.Contains(allColumns, destination.CDCDeletedColumn)
	// _cdc_unchanged_cols is only emitted by sources that can mark columns as
	// unchanged (e.g. Postgres TOAST); other CDC sources materialize full rows
	// and their staging tables have no such column to reference.
	hasUnchangedCols := slices.Contains(allColumns, destination.CDCUnchangedColsColumn)

	unchangedRef := fmt.Sprintf("source.%s", destination.QuoteIdentifier(destination.CDCUnchangedColsColumn))
	var updateSets []string
	for _, col := range destColumns {
		if !pkMap[strings.ToLower(col)] {
			if hasCDCDeleted && hasUnchangedCols && !destination.IsCDCMetaColumn(col) {
				updateSets = append(updateSets, cdcMergeAssign(
					col,
					fmt.Sprintf("target.%s", destination.QuoteIdentifier(col)),
					fmt.Sprintf("source.%s", destination.QuoteIdentifier(col)),
					unchangedRef,
				))
			} else {
				updateSets = append(updateSets, fmt.Sprintf(`%s = source.%s`, destination.QuoteIdentifier(col), destination.QuoteIdentifier(col)))
			}
		}
	}

	stagingQuoted := make([]string, len(allColumns))
	for i, col := range allColumns {
		stagingQuoted[i] = destination.QuoteIdentifier(col)
	}
	destQuoted := make([]string, len(destColumns))
	destSourceCols := make([]string, len(destColumns))
	for i, col := range destColumns {
		destQuoted[i] = destination.QuoteIdentifier(col)
		destSourceCols[i] = fmt.Sprintf("source.%s", destination.QuoteIdentifier(col))
	}

	// Build dedup subquery to handle duplicate PKs in staging
	quotedPKsForPartition := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		quotedPKsForPartition[i] = destination.QuoteIdentifier(pk)
	}
	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY (SELECT NULL)) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
		strings.Join(stagingQuoted, ", "),
		strings.Join(stagingQuoted, ", "),
		strings.Join(quotedPKsForPartition, ", "),
		destination.QuoteTableName(stagingTable),
	)

	return mergeParts{
		dedupSource:    dedupSource,
		joinCondition:  strings.Join(onConditions, " AND "),
		updateSets:     updateSets,
		destQuoted:     destQuoted,
		destSourceCols: destSourceCols,
		hasCDCDeleted:  hasCDCDeleted,
	}
}

// buildPredicateMergeStatements emulates a predicate-scoped MERGE with
// UPDATE ... FROM plus an anti-join INSERT, because Redshift's MERGE only
// accepts source/target equality predicates in its ON clause. The INSERT
// runs first so its anti-join sees the pre-update target: an UPDATE that
// moves a matched row out of the predicate window would otherwise make the
// INSERT re-add that row as a duplicate.
func buildPredicateMergeStatements(stagingTable, targetTable string, primaryKeys, allColumns []string, predicate string) []string {
	parts := buildMergeParts(stagingTable, primaryKeys, allColumns)
	target := destination.QuoteTableName(targetTable)
	matchCondition := destination.MergeJoinCondition(parts.joinCondition, predicate)

	var statements []string
	if parts.hasCDCDeleted {
		statements = append(statements, fmt.Sprintf(
			"INSERT INTO %s (%s) SELECT %s FROM %s AS source WHERE source.\"_cdc_deleted\" = false AND NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)",
			target, strings.Join(parts.destQuoted, ", "), strings.Join(parts.destSourceCols, ", "), parts.dedupSource, target, matchCondition,
		))
		if len(parts.updateSets) > 0 {
			statements = append(statements, fmt.Sprintf(
				"UPDATE %s AS target SET %s FROM %s AS source WHERE %s AND source.\"_cdc_deleted\" = false",
				target, strings.Join(parts.updateSets, ", "), parts.dedupSource, matchCondition,
			))
		}
		statements = append(statements, fmt.Sprintf(
			`UPDATE %s AS target SET "_cdc_deleted" = true, "_cdc_lsn" = source."_cdc_lsn", "_cdc_synced_at" = source."_cdc_synced_at" FROM %s AS source WHERE %s AND source."_cdc_deleted" = true`,
			target, parts.dedupSource, matchCondition,
		))
	} else {
		statements = append(statements, fmt.Sprintf(
			"INSERT INTO %s (%s) SELECT %s FROM %s AS source WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)",
			target, strings.Join(parts.destQuoted, ", "), strings.Join(parts.destSourceCols, ", "), parts.dedupSource, target, matchCondition,
		))
		if len(parts.updateSets) > 0 {
			statements = append(statements, fmt.Sprintf(
				"UPDATE %s AS target SET %s FROM %s AS source WHERE %s",
				target, strings.Join(parts.updateSets, ", "), parts.dedupSource, matchCondition,
			))
		}
	}
	return statements
}

func (d *RedshiftDestination) SupportsCDCUnchangedCols() bool { return true }

func (d *RedshiftDestination) SupportsIncrementalPredicate() bool { return true }

func (d *RedshiftDestination) SupportsCDCMerge() bool {
	return true
}

// cdcMergeAssign emulates a JSON array-contains check with CHARINDEX because
// Redshift lacks a native equivalent. It matches the quoted element preceded
// by '[' (first element) or ',' (any later element), which is only correct
// because _cdc_unchanged_cols is always produced by json.Marshal and is
// therefore compact: no whitespace after '[' or ','. The needle includes the
// element's closing quote, so prefix collisions ("col" vs "colour") cannot
// match.
func cdcMergeAssign(col, targetExpr, sourceExpr, unchangedColsExpr string) string {
	member, _ := json.Marshal(col)
	first := strings.ReplaceAll("["+string(member), "'", "''")
	second := strings.ReplaceAll(","+string(member), "'", "''")
	return fmt.Sprintf(
		`"%s" = CASE WHEN CHARINDEX('%s', COALESCE(%s, '[]')) > 0 OR CHARINDEX('%s', COALESCE(%s, '[]')) > 0 THEN %s ELSE %s END`,
		col, first, unchangedColsExpr, second, unchangedColsExpr, targetExpr, sourceExpr,
	)
}
