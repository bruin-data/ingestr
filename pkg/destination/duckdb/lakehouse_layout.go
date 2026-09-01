package duckdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	srcduckdb "github.com/bruin-data/ingestr/pkg/source/duckdb"
)

type duckLakeTableLayout struct {
	partitionSet    bool
	partitionColumn string
	partitionByDay  bool
	sortSet         bool
	sortColumns     []string
}

func (l duckLakeTableLayout) empty() bool {
	return !l.partitionSet && !l.sortSet
}

func buildDuckLakeTableLayout(opts destination.PrepareOptions) (duckLakeTableLayout, error) {
	requestedSort := make([]string, 0, len(opts.ClusterBy))
	for _, requested := range opts.ClusterBy {
		if strings.TrimSpace(requested) != "" {
			requestedSort = append(requestedSort, requested)
		}
	}
	layout := duckLakeTableLayout{
		partitionSet: strings.TrimSpace(opts.PartitionBy) != "",
		sortSet:      len(requestedSort) > 0,
	}
	if layout.empty() {
		return layout, nil
	}
	if opts.Schema == nil {
		return layout, fmt.Errorf("ducklake: schema is required to configure table layout")
	}

	if layout.partitionSet {
		column, err := resolveDuckLakeLayoutColumn(opts.Schema, opts.PartitionBy)
		if err != nil {
			return layout, err
		}
		switch column.DataType {
		case schema.TypeDate:
		case schema.TypeTimestamp, schema.TypeTimestampTZ:
			layout.partitionByDay = true
		default:
			return layout, fmt.Errorf("ducklake: partition column %q must be a date or timestamp, got %s", column.Name, column.DataType)
		}
		layout.partitionColumn = column.Name
	}

	if layout.sortSet {
		layout.sortColumns = make([]string, len(requestedSort))
		for i, requested := range requestedSort {
			column, err := resolveDuckLakeLayoutColumn(opts.Schema, requested)
			if err != nil {
				return layout, err
			}
			layout.sortColumns[i] = column.Name
		}
	}

	return layout, nil
}

func resolveDuckLakeLayoutColumn(tableSchema *schema.TableSchema, requested string) (schema.Column, error) {
	requested = strings.TrimSpace(requested)
	for _, column := range tableSchema.Columns {
		if column.Name == requested || duckDBIdentifierKey(column.Name) == duckDBIdentifierKey(requested) {
			return column, nil
		}
	}
	return schema.Column{}, fmt.Errorf("ducklake: layout column %q does not exist in the destination schema", requested)
}

func duckLakeLayoutAppliesToSchema(layout duckLakeTableLayout, tableSchema *schema.TableSchema) bool {
	if tableSchema == nil {
		return false
	}
	if layout.partitionSet {
		column, err := resolveDuckLakeLayoutColumn(tableSchema, layout.partitionColumn)
		if err != nil {
			return false
		}
		if layout.partitionByDay && column.DataType != schema.TypeDate && column.DataType != schema.TypeTimestamp && column.DataType != schema.TypeTimestampTZ {
			return false
		}
	}
	for _, column := range layout.sortColumns {
		if _, err := resolveDuckLakeLayoutColumn(tableSchema, column); err != nil {
			return false
		}
	}
	return true
}

func (d *DuckLakeDestination) applyTableLayout(ctx context.Context, table string, layout duckLakeTableLayout) error {
	if layout.empty() {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("ducklake: begin table layout transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = d.exec(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()

	if err := d.execTableLayout(ctx, table, layout); err != nil {
		return err
	}

	if err := d.exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("ducklake: commit table layout transaction: %w", err)
	}
	committed = true
	return nil
}

type duckLakeLayoutStatement struct {
	kind        string
	sql         string
	sortColumns []string
}

func duckLakeLayoutStatements(table string, layout duckLakeTableLayout) []duckLakeLayoutStatement {
	stmts := make([]duckLakeLayoutStatement, 0, 2)
	if layout.partitionSet {
		stmts = append(stmts, duckLakeLayoutStatement{
			kind: "partition",
			sql:  buildDuckLakePartitionSQL(table, layout.partitionColumn, layout.partitionByDay),
		})
	}
	if layout.sortSet {
		stmts = append(stmts, duckLakeLayoutStatement{
			kind:        "sort",
			sql:         buildDuckLakeSortSQL(table, layout.sortColumns),
			sortColumns: layout.sortColumns,
		})
	}
	return stmts
}

// execTableLayout issues the layout DDL. Callers own d.mu and the surrounding
// transaction.
func (d *DuckLakeDestination) execTableLayout(ctx context.Context, table string, layout duckLakeTableLayout) error {
	return d.execLayoutStatements(ctx, table, duckLakeLayoutStatements(table, layout), true)
}

func (d *DuckLakeDestination) execLayoutStatements(ctx context.Context, table string, stmts []duckLakeLayoutStatement, skipMatchingSort bool) error {
	for _, stmt := range stmts {
		if stmt.kind == "sort" && skipMatchingSort {
			matches, err := d.currentSortLayoutMatches(ctx, table, stmt.sortColumns)
			if err != nil {
				config.Debug("[DUCKLAKE] Could not inspect current sort layout for %s: %v", table, err)
			} else if matches {
				config.Debug("[DUCKLAKE] Sort layout already matches for %s", table)
				continue
			}
		}
		config.Debug("[DUCKLAKE] Applying %s layout: %s", stmt.kind, stmt.sql)
		if err := d.exec(ctx, stmt.sql); err != nil {
			if stmt.kind == "sort" && strings.Contains(err.Error(), "Unsupported ALTER TABLE type") {
				return fmt.Errorf("ducklake: apply sort layout to %s: %w (SET SORTED BY requires the ducklake extension shipped with DuckDB 1.5 or newer)", table, err)
			}
			return fmt.Errorf("ducklake: apply %s layout to %s: %w", stmt.kind, table, err)
		}
	}
	return nil
}

type duckLakeSortExpression struct {
	expression string
	direction  string
	nullOrder  string
}

func (d *DuckLakeDestination) currentSortLayoutMatches(ctx context.Context, table string, expected []string) (bool, error) {
	if len(expected) == 0 {
		return false, nil
	}
	tn := duckTable(table)
	schemaName := tn.Schema
	if schemaName == "" {
		schemaName = "main"
	}
	metadataCatalog := destination.QuoteIdentifier("__ducklake_metadata_" + srcduckdb.AttachAlias)
	query := fmt.Sprintf(`
		SELECT expression.expression, expression.sort_direction, expression.null_order
		FROM %s.main.ducklake_sort_info AS info
		JOIN %s.main.ducklake_sort_expression AS expression
		  ON expression.sort_id = info.sort_id AND expression.table_id = info.table_id
		JOIN %s.main.ducklake_table AS tbl ON tbl.table_id = info.table_id
		JOIN %s.main.ducklake_schema AS sch ON sch.schema_id = tbl.schema_id
		WHERE info.end_snapshot IS NULL
		  AND tbl.end_snapshot IS NULL
		  AND sch.end_snapshot IS NULL
		  AND tbl.table_name = %s
		  AND sch.schema_name = %s
		ORDER BY sort_key_index`,
		metadataCatalog, metadataCatalog, metadataCatalog, metadataCatalog,
		duckLakeStringLiteral(tn.Table), duckLakeStringLiteral(schemaName),
	)
	stmt, err := d.conn.NewStatement()
	if err != nil {
		return false, err
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.SetSqlQuery(query); err != nil {
		return false, err
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		return false, err
	}
	defer reader.Release()

	var actual []duckLakeSortExpression
	for reader.Next() {
		batch := reader.RecordBatch()
		expressions, expressionsOK := batch.Column(0).(*array.String)
		directions, directionsOK := batch.Column(1).(*array.String)
		nullOrders, nullOrdersOK := batch.Column(2).(*array.String)
		if !expressionsOK || !directionsOK || !nullOrdersOK {
			return false, fmt.Errorf("ducklake: unexpected sort metadata types")
		}
		for row := 0; row < int(batch.NumRows()); row++ {
			actual = append(actual, duckLakeSortExpression{
				expression: strings.Clone(expressions.Value(row)),
				direction:  strings.Clone(directions.Value(row)),
				nullOrder:  strings.Clone(nullOrders.Value(row)),
			})
		}
	}
	if err := reader.Err(); err != nil {
		return false, err
	}
	return duckLakeSortLayoutMatches(expected, actual), nil
}

func duckLakeSortLayoutMatches(expected []string, actual []duckLakeSortExpression) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i, column := range expected {
		actualColumn, ok := duckLakeSortExpressionColumn(actual[i].expression)
		if !ok || duckDBIdentifierKey(column) != duckDBIdentifierKey(actualColumn) || actual[i].direction != "ASC" || actual[i].nullOrder != "NULLS_LAST" {
			return false
		}
	}
	return true
}

func duckLakeSortExpressionColumn(expression string) (string, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return "", false
	}
	if expression[0] != '"' {
		return expression, !strings.ContainsAny(expression, " \t\r\n.()")
	}

	var column strings.Builder
	for i := 1; i < len(expression); i++ {
		if expression[i] != '"' {
			column.WriteByte(expression[i])
			continue
		}
		if i+1 < len(expression) && expression[i+1] == '"' {
			column.WriteByte('"')
			i++
			continue
		}
		return column.String(), i == len(expression)-1
	}
	return "", false
}

func duckLakeStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// buildDuckLakePartitionSQL partitions timestamps by year/month/day rather than
// day() alone: DuckLake's day() extracts the day of the month, so on its own it
// would collapse every month and year into the same 31 partitions.
func buildDuckLakePartitionSQL(table, column string, byDay bool) string {
	quoted := destination.QuoteIdentifier(column)
	expression := quoted
	if byDay {
		expression = fmt.Sprintf("year(%s), month(%s), day(%s)", quoted, quoted, quoted)
	}
	return fmt.Sprintf("ALTER TABLE %s SET PARTITIONED BY (%s)", destination.QuoteTableName(table), expression)
}

func buildDuckLakeSortSQL(table string, columns []string) string {
	expressions := make([]string, len(columns))
	for i, column := range columns {
		expressions[i] = destination.QuoteIdentifier(column) + " ASC"
	}
	return fmt.Sprintf("ALTER TABLE %s SET SORTED BY (%s)", destination.QuoteTableName(table), strings.Join(expressions, ", "))
}
