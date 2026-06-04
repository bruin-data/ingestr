package redshift

import (
	"context"
	"encoding/json"
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

func (d *RedshiftDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	for result := range records {
		if result.Err != nil {
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

	mergeSQL := d.buildMergeSQL(opts.StagingTable, opts.TargetTable, opts.PrimaryKeys, opts.Columns)
	config.Debug("[MERGE] Executing MERGE: %s", mergeSQL)

	if err := d.Exec(ctx, mergeSQL); err != nil {
		return fmt.Errorf("failed to execute merge: %w", err)
	}

	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *RedshiftDestination) buildMergeSQL(stagingTable, targetTable string, primaryKeys, allColumns []string) string {
	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf(`target."%s" = source."%s"`, pk, pk)
	}
	onClause := strings.Join(onConditions, " AND ")

	pkMap := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkMap[strings.ToLower(pk)] = true
	}

	hasCDCDeleted := slices.Contains(allColumns, destination.CDCDeletedColumn)

	unchangedRef := fmt.Sprintf(`source."%s"`, destination.CDCUnchangedColsColumn)
	var updateSets []string
	for _, col := range allColumns {
		if !pkMap[strings.ToLower(col)] {
			if hasCDCDeleted && !destination.IsCDCMetaColumn(col) {
				updateSets = append(updateSets, cdcMergeAssign(
					col,
					fmt.Sprintf(`target."%s"`, col),
					fmt.Sprintf(`source."%s"`, col),
					unchangedRef,
				))
			} else {
				updateSets = append(updateSets, fmt.Sprintf(`"%s" = source."%s"`, col, col))
			}
		}
	}

	quotedCols := make([]string, len(allColumns))
	sourceCols := make([]string, len(allColumns))
	for i, col := range allColumns {
		quotedCols[i] = fmt.Sprintf(`"%s"`, col)
		sourceCols[i] = fmt.Sprintf(`source."%s"`, col)
	}

	// Build dedup subquery to handle duplicate PKs in staging
	quotedPKsForPartition := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		quotedPKsForPartition[i] = fmt.Sprintf(`"%s"`, pk)
	}
	dedupSource := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY (SELECT NULL)) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1)`,
		strings.Join(quotedCols, ", "),
		strings.Join(quotedCols, ", "),
		strings.Join(quotedPKsForPartition, ", "),
		destination.QuoteTableName(stagingTable),
	)

	var sql strings.Builder
	fmt.Fprintf(&sql, "MERGE INTO %s AS target\n", destination.QuoteTableName(targetTable))
	fmt.Fprintf(&sql, "USING %s AS source\n", dedupSource)
	fmt.Fprintf(&sql, "ON %s\n", onClause)

	if hasCDCDeleted {
		if len(updateSets) > 0 {
			sql.WriteString(`WHEN MATCHED AND source."_cdc_deleted" = false THEN` + "\n")
			fmt.Fprintf(&sql, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}

		sql.WriteString(`WHEN MATCHED AND source."_cdc_deleted" = true THEN` + "\n")
		sql.WriteString(`  UPDATE SET "_cdc_deleted" = true, "_cdc_lsn" = source."_cdc_lsn", "_cdc_synced_at" = source."_cdc_synced_at"` + "\n")

		sql.WriteString(`WHEN NOT MATCHED AND source."_cdc_deleted" = false THEN` + "\n")
		fmt.Fprintf(&sql, "  INSERT (%s)\n", strings.Join(quotedCols, ", "))
		fmt.Fprintf(&sql, "  VALUES (%s)", strings.Join(sourceCols, ", "))
	} else {
		if len(updateSets) > 0 {
			sql.WriteString("WHEN MATCHED THEN\n")
			fmt.Fprintf(&sql, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}

		sql.WriteString("WHEN NOT MATCHED THEN\n")
		fmt.Fprintf(&sql, "  INSERT (%s)\n", strings.Join(quotedCols, ", "))
		fmt.Fprintf(&sql, "  VALUES (%s)", strings.Join(sourceCols, ", "))
	}

	return sql.String()
}

func (d *RedshiftDestination) SupportsCDCMerge() bool {
	return true
}

func cdcMergeAssign(col, targetExpr, sourceExpr, unchangedColsExpr string) string {
	member, _ := json.Marshal(col)
	first := strings.ReplaceAll("["+string(member), "'", "''")
	second := strings.ReplaceAll(","+string(member), "'", "''")
	return fmt.Sprintf(
		`"%s" = CASE WHEN CHARINDEX('%s', COALESCE(%s, '[]')) > 0 OR CHARINDEX('%s', COALESCE(%s, '[]')) > 0 THEN %s ELSE %s END`,
		col, first, unchangedColsExpr, second, unchangedColsExpr, targetExpr, sourceExpr,
	)
}
