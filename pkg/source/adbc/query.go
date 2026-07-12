package adbc

import (
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

// FilterColumns filters columns by exclusion list.
func FilterColumns(columns []schema.Column, exclude []string) []schema.Column {
	if len(exclude) == 0 {
		return columns
	}

	excludeMap := make(map[string]bool)
	for _, col := range exclude {
		excludeMap[strings.ToLower(col)] = true
	}

	var filtered []schema.Column
	for _, col := range columns {
		if !excludeMap[strings.ToLower(col.Name)] {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

// BuildArrowSchema creates an Arrow schema from column definitions.
func BuildArrowSchema(columns []schema.Column) *arrow.Schema {
	fields := make([]arrow.Field, len(columns))
	for i, col := range columns {
		fields[i] = arrow.Field{
			Name:     col.Name,
			Type:     schema.DataTypeToArrowType(col),
			Nullable: col.Nullable,
		}
	}
	return arrow.NewSchema(fields, nil)
}

// BuildSelectQuery builds a SELECT query with optional conditions.
// The dialect's QuoteIdentifier function is used to properly quote column names.
func BuildSelectQuery(table string, columns []schema.Column, opts source.ReadOptions, quoteFunc func(string) string) string {
	colNames := make([]string, len(columns))
	for i, col := range columns {
		colNames[i] = quoteFunc(col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), table)

	var conditions []string
	config.Debug("[QUERY] Building query with IncrementalKey=%s, IntervalStart=%v, IntervalEnd=%v", opts.IncrementalKey, opts.IntervalStart, opts.IntervalEnd)
	if opts.IncrementalKey != "" {
		conditions = append(conditions, source.SQLTemporalRangeConditions(opts.IncrementalKey, opts.IncrementalKeyDataType, opts.IntervalStart, opts.IntervalEnd, "<=", quoteFunc, source.DefaultSQLTimeFormat)...)
	}
	conditions = append(conditions, source.SQLExtractPartitionConditions(opts, quoteFunc, source.DefaultSQLTimeFormat)...)

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
		config.Debug("[QUERY] Final query with %d conditions: %s", len(conditions), query)
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	return query
}

// DefaultQuoteIdentifier provides the default quoting style (double quotes).
// Used by PostgreSQL, DuckDB, Snowflake, BigQuery, SQL Server (ANSI mode).
func DefaultQuoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}

// BacktickQuoteIdentifier uses backticks for quoting (MySQL style).
func BacktickQuoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

// BracketQuoteIdentifier uses brackets for quoting (SQL Server style).
func BracketQuoteIdentifier(name string) string {
	return fmt.Sprintf("[%s]", strings.ReplaceAll(name, "]", "]]"))
}
