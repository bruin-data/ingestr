package redshift

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/internal/config"
	intredshift "github.com/bruin-data/ingestr/internal/redshift"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/source"
)

type RedshiftDestination struct {
	*postgres.PostgresDestination
}

func NewRedshiftDestination() *RedshiftDestination {
	return &RedshiftDestination{PostgresDestination: postgres.NewPostgresDestination()}
}

func (d *RedshiftDestination) Schemes() []string {
	return []string{"redshift", "redshift+psycopg2"}
}

func (d *RedshiftDestination) Connect(ctx context.Context, uri string) error {
	return d.PostgresDestination.Connect(ctx, intredshift.NormalizeURI(uri))
}

func (d *RedshiftDestination) ValidateManagedCDCState() error {
	return errors.New("redshift does not support destination-managed PostgreSQL CDC state")
}

func (d *RedshiftDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
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

		numRows := int(record.NumRows())
		numCols := int(record.NumCols())

		if numRows == 0 {
			record.Release()
			continue
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

		for offset := 0; offset < numRows; offset += rowsPerInsert {
			end := offset + rowsPerInsert
			if end > numRows {
				end = numRows
			}

			sql, args := buildInsert(opts.Table, columns, record, offset, end)
			if err := d.Exec(ctx, sql, args...); err != nil {
				record.Release()
				return fmt.Errorf("failed to insert batch: %w", err)
			}
		}

		record.Release()
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
	var b strings.Builder

	b.WriteString("INSERT INTO ")
	b.WriteString(destination.QuoteTableName(table))
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (d *RedshiftDestination) GetScheme() string { return "redshift" }

// MergeTable performs a merge operation using Redshift's native MERGE statement.
// For CDC sources (detected by presence of _cdc_deleted column), it handles
// deleted rows specially by only updating CDC columns (preserving original data).
func (d *RedshiftDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()

	if len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("merge requires at least one primary key")
	}

	// Redshift MERGE only accepts equality predicates between source and target
	// columns in its ON clause, so predicate merges run as UPDATE + anti-join INSERT.
	if predicate := strings.TrimSpace(opts.IncrementalPredicate); predicate != "" {
		if err := d.mergeWithPredicate(ctx, opts, predicate); err != nil {
			return err
		}
		config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
		return nil
	}

	mergeSQL := d.buildMergeSQL(opts.StagingTable, opts.TargetTable, opts.PrimaryKeys, opts.Columns)
	config.Debug("[MERGE] Executing MERGE: %s", mergeSQL)

	if err := d.Exec(ctx, mergeSQL); err != nil {
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *RedshiftDestination) mergeWithPredicate(ctx context.Context, opts destination.MergeOptions, predicate string) error {
	statements := buildPredicateMergeStatements(opts.StagingTable, opts.TargetTable, opts.PrimaryKeys, opts.Columns, predicate)

	tx, err := d.BeginTransaction(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, stmt := range statements {
		config.Debug("[MERGE] Executing predicate merge statement: %s", stmt)
		if err := tx.Exec(ctx, stmt); err != nil {
			config.LogFailedQuery(stmt, err)
			return fmt.Errorf("failed to execute predicate merge statement: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

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
