package d1

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

func (d *D1Destination) Schemes() []string { return []string{"d1", "cloudflare-d1"} }

func (d *D1Destination) GetScheme() string { return "d1" }

func (d *D1Destination) SupportsReplaceStrategy() bool      { return true }
func (d *D1Destination) SupportsAppendStrategy() bool       { return true }
func (d *D1Destination) SupportsMergeStrategy() bool        { return true }
func (d *D1Destination) SupportsDeleteInsertStrategy() bool { return true }
func (d *D1Destination) SupportsSCD2Strategy() bool         { return false }
func (d *D1Destination) SupportsAtomicSwap() bool           { return true }

func (d *D1Destination) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:    destination.ReplaceStagingTargetSchema,
		DefaultTargetSchema: "main",
	}
}

func (d *D1Destination) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return d.ReplaceStagingPolicy()
}

func quoteTable(table string) (string, error) {
	parts := tablename.Split(table)
	if len(parts) == 2 && strings.EqualFold(parts[0], "main") {
		parts = parts[1:]
	}
	if len(parts) != 1 || strings.TrimSpace(parts[0]) == "" || strings.ContainsRune(parts[0], 0) {
		return "", fmt.Errorf("D1 table name must be an unqualified table name or main.table, %q given", table)
	}
	return destination.QuoteIdentifier(parts[0]), nil
}

func quoteColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = destination.QuoteIdentifier(col)
	}
	return quoted
}

// TEXT-affinity declarations retain logical types for Arrow casts on later loads.
func typeName(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean, schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return "INTEGER"
	case schema.TypeFloat32, schema.TypeFloat64:
		return "REAL"
	case schema.TypeBinary:
		return "BLOB"
	case schema.TypeDecimal:
		precision := col.Precision
		if precision == 0 {
			precision = 38
		}
		return fmt.Sprintf("DECIMAL_TEXT(%d,%d)", precision, col.Scale)
	case schema.TypeDate:
		return "DATE_TEXT"
	case schema.TypeTime:
		return "TIME_TEXT"
	case schema.TypeTimestamp:
		return "TIMESTAMP_TEXT"
	case schema.TypeTimestampTZ:
		return "TIMESTAMPTZ_TEXT"
	case schema.TypeJSON, schema.TypeArray:
		return "JSON_TEXT"
	case schema.TypeUUID:
		return "UUID_TEXT"
	case schema.TypeInterval:
		return "DURATION_TEXT"
	default:
		return "TEXT"
	}
}

func buildCreateTableSQL(table string, columns []schema.Column, primaryKeys []string) string {
	definitions := make([]string, 0, len(columns)+1)
	for _, col := range columns {
		definition := destination.QuoteIdentifier(col.Name) + " " + typeName(col)
		if !col.Nullable {
			definition += " NOT NULL"
		}
		definitions = append(definitions, definition)
	}
	if len(primaryKeys) > 0 {
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(quoteColumns(primaryKeys), ", ")+")")
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", table, strings.Join(definitions, ", "))
}

func (d *D1Destination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	table, err := quoteTable(opts.Table)
	if err != nil {
		return err
	}
	if opts.CDCMode || opts.RequirePrimaryKeyMatch {
		return fmt.Errorf("D1 does not support CDC ingestion")
	}
	if opts.Schema != nil {
		if len(opts.Schema.Columns) == 0 {
			return fmt.Errorf("D1 tables require at least one column")
		}
		if len(opts.Schema.Columns) > 100 {
			return fmt.Errorf("D1 destination supports at most 100 columns per table; got %d", len(opts.Schema.Columns))
		}
		if destination.HasCDCDeletedColumn(opts.Schema.ColumnNames()) {
			return fmt.Errorf("D1 does not support CDC ingestion")
		}
	}
	var statements []statement
	if opts.DropFirst {
		statements = append(statements, statement{SQL: "DROP TABLE IF EXISTS " + table})
	}
	if opts.Schema != nil {
		statements = append(statements, statement{SQL: buildCreateTableSQL(table, opts.Schema.Columns, opts.PrimaryKeys)})
	}
	if err := d.executeBatch(ctx, statements); err != nil {
		return fmt.Errorf("prepare D1 table %s: %w", opts.Table, err)
	}
	return nil
}

func (d *D1Destination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	quoted, err := quoteTable(table)
	if err != nil {
		return nil, err
	}
	results, err := d.query(ctx, "PRAGMA table_info("+quoted+")")
	if err != nil {
		return nil, fmt.Errorf("inspect D1 table %s: %w", table, err)
	}
	if len(results) == 0 || len(results[0].Results) == 0 {
		return nil, nil
	}
	columns := make([]schema.Column, 0, len(results[0].Results))
	keys := make(map[int]string)
	for _, row := range results[0].Results {
		name, ok := row["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("D1 table_info returned an invalid column name")
		}
		declaredType, _ := row["type"].(string)
		pk, err := metadataInt(row["pk"])
		if err != nil {
			return nil, fmt.Errorf("read D1 primary key ordinal for %q: %w", name, err)
		}
		notNull, err := metadataInt(row["notnull"])
		if err != nil {
			return nil, fmt.Errorf("read D1 nullability for %q: %w", name, err)
		}
		column := schema.Column{
			Name: name, DataType: schemaType(declaredType), Nullable: notNull == 0, IsPrimaryKey: pk > 0,
		}
		if column.DataType == schema.TypeDecimal {
			column.Precision, column.Scale, err = decimalMetadata(declaredType)
			if err != nil {
				return nil, fmt.Errorf("read D1 decimal type for %q: %w", name, err)
			}
		}
		columns = append(columns, column)
		if pk > 0 {
			keys[pk] = name
		}
	}
	primaryKeys := make([]string, len(keys))
	for ordinal, name := range keys {
		if ordinal < 1 || ordinal > len(primaryKeys) {
			return nil, fmt.Errorf("D1 table_info returned an invalid primary key ordinal %d", ordinal)
		}
		primaryKeys[ordinal-1] = name
	}
	parts := tablename.Split(table)
	return &schema.TableSchema{Name: parts[len(parts)-1], Columns: columns, PrimaryKeys: primaryKeys}, nil
}

func metadataInt(value interface{}) (int, error) {
	return strconv.Atoi(fmt.Sprint(value))
}

func schemaType(declaredType string) schema.DataType {
	declaredType = strings.ToUpper(strings.TrimSpace(declaredType))
	base, _, _ := strings.Cut(declaredType, "(")
	switch strings.TrimSpace(base) {
	case "DECIMAL_TEXT":
		return schema.TypeDecimal
	case "DATE_TEXT":
		return schema.TypeDate
	case "TIME_TEXT":
		return schema.TypeTime
	case "TIMESTAMP_TEXT":
		return schema.TypeTimestamp
	case "TIMESTAMPTZ_TEXT":
		return schema.TypeTimestampTZ
	case "JSON_TEXT":
		return schema.TypeJSON
	case "UUID_TEXT":
		return schema.TypeUUID
	case "DURATION_TEXT":
		return schema.TypeInterval
	}
	switch {
	case strings.Contains(declaredType, "INT"):
		return schema.TypeInt64
	case strings.Contains(declaredType, "CHAR"), strings.Contains(declaredType, "CLOB"), strings.Contains(declaredType, "TEXT"):
		return schema.TypeString
	case strings.Contains(declaredType, "BLOB"), declaredType == "":
		return schema.TypeBinary
	case strings.Contains(declaredType, "REAL"), strings.Contains(declaredType, "FLOA"), strings.Contains(declaredType, "DOUB"):
		return schema.TypeFloat64
	default:
		return schema.TypeString
	}
}

func decimalMetadata(declaration string) (int, int, error) {
	_, metadata, hasMetadata := strings.Cut(declaration, "(")
	if !hasMetadata {
		return 38, 0, nil
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimSpace(metadata), ")"), ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid decimal declaration %q", declaration)
	}
	precision, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid decimal precision: %w", err)
	}
	scale, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid decimal scale: %w", err)
	}
	return precision, scale, nil
}

func (d *D1Destination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	staging, err := quoteTable(opts.StagingTable)
	if err != nil {
		return err
	}
	target, err := quoteTable(opts.TargetTable)
	if err != nil {
		return err
	}
	if strings.EqualFold(staging, target) {
		return fmt.Errorf("D1 staging and target tables must be different")
	}
	if err := d.executeBatch(ctx, []statement{
		{SQL: "DROP TABLE IF EXISTS " + target},
		{SQL: fmt.Sprintf("ALTER TABLE %s RENAME TO %s", staging, target)},
	}); err != nil {
		return fmt.Errorf("swap D1 table: %w", err)
	}
	return nil
}

func (d *D1Destination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	staging, err := quoteTable(opts.StagingTable)
	if err != nil {
		return err
	}
	target, err := quoteTable(opts.TargetTable)
	if err != nil {
		return err
	}
	if len(opts.PrimaryKeys) == 0 || len(opts.Columns) == 0 {
		return fmt.Errorf("D1 merge requires primary keys and columns")
	}
	if opts.IncrementalPredicate != "" {
		return fmt.Errorf("D1 does not support incremental predicates")
	}
	if opts.Schema != nil {
		for _, col := range opts.Schema.Columns {
			if strings.EqualFold(col.Name, opts.IncrementalKey) && col.DataType == schema.TypeDecimal {
				return fmt.Errorf("D1 stores decimals as text and cannot order a decimal incremental key; cast the source key to an integer or float")
			}
		}
	}
	if destination.HasCDCDeletedColumn(opts.Columns) {
		return fmt.Errorf("D1 does not support CDC ingestion")
	}
	columns := strings.Join(quoteColumns(opts.Columns), ", ")
	order := ""
	if opts.IncrementalKey != "" {
		order = destination.QuoteIdentifier(opts.IncrementalKey)
	}
	selectSQL := destination.DedupStagingSelect(columns, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), staging, order)
	if opts.StagingPrimaryKeysUnique {
		selectSQL = "SELECT " + columns + " FROM " + staging
	}
	joins := make([]string, len(opts.PrimaryKeys))
	keySet := make(map[string]bool, len(opts.PrimaryKeys))
	for i, key := range opts.PrimaryKeys {
		quoted := destination.QuoteIdentifier(key)
		joins[i] = "target." + quoted + " IS source." + quoted
		keySet[strings.ToLower(key)] = true
	}
	join := strings.Join(joins, " AND ")
	var updates []string
	for _, col := range opts.Columns {
		if !keySet[strings.ToLower(col)] {
			quoted := destination.QuoteIdentifier(col)
			updates = append(updates, quoted+" = source."+quoted)
		}
	}
	var statements []statement
	if len(updates) > 0 {
		statements = append(statements, statement{SQL: fmt.Sprintf(
			"UPDATE %s AS target SET %s FROM (%s) AS source WHERE %s", target, strings.Join(updates, ", "), selectSQL, join)})
	}
	sourceColumns := quoteColumns(opts.Columns)
	for i := range sourceColumns {
		sourceColumns[i] = "source." + sourceColumns[i]
	}
	statements = append(statements, statement{SQL: fmt.Sprintf(
		"INSERT INTO %s (%s) SELECT %s FROM (%s) AS source WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)",
		target, columns, strings.Join(sourceColumns, ", "), selectSQL, target, join)})
	if err := d.executeBatch(ctx, statements); err != nil {
		return fmt.Errorf("merge D1 table: %w", err)
	}
	return nil
}

func (d *D1Destination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	staging, err := quoteTable(opts.StagingTable)
	if err != nil {
		return err
	}
	target, err := quoteTable(opts.TargetTable)
	if err != nil {
		return err
	}
	if opts.IncrementalKey == "" || len(opts.Columns) == 0 || opts.IntervalStart == nil || opts.IntervalEnd == nil {
		return fmt.Errorf("D1 delete+insert requires columns, an incremental key, and both interval bounds")
	}
	if opts.IncrementalKeyType == schema.TypeDecimal {
		return fmt.Errorf("D1 stores decimals as text and cannot compare a decimal incremental key; cast the source key to an integer or float")
	}
	key := destination.QuoteIdentifier(opts.IncrementalKey)
	start, err := intervalParam(opts.IntervalStart, opts.IncrementalKeyType)
	if err != nil {
		return fmt.Errorf("D1 interval start: %w", err)
	}
	end, err := intervalParam(opts.IntervalEnd, opts.IncrementalKeyType)
	if err != nil {
		return fmt.Errorf("D1 interval end: %w", err)
	}
	columns := strings.Join(quoteColumns(opts.Columns), ", ")
	selectSQL := destination.DedupStagingSelect(columns, strings.Join(quoteColumns(opts.PrimaryKeys), ", "), staging, key)
	statements := []statement{
		{SQL: fmt.Sprintf("DELETE FROM %s WHERE %s >= ? AND %s <= ?", target, key, key), Params: []interface{}{start, end}},
		{SQL: fmt.Sprintf("INSERT INTO %s (%s) %s", target, columns, selectSQL)},
	}
	if err := d.executeBatch(ctx, statements); err != nil {
		return fmt.Errorf("delete and insert D1 table: %w", err)
	}
	return nil
}

func intervalParam(value interface{}, dataType schema.DataType) (interface{}, error) {
	if dataType == schema.TypeTimestamp || dataType == schema.TypeTimestampTZ || dataType == schema.TypeDate {
		var temporal time.Time
		switch v := value.(type) {
		case time.Time:
			temporal = v
		case string:
			parsedBound := false
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02"} {
				if parsed, err := time.Parse(layout, v); err == nil {
					temporal = parsed
					parsedBound = true
					break
				}
			}
			if !parsedBound {
				return nil, fmt.Errorf("invalid temporal bound %q", v)
			}
		default:
			return nil, fmt.Errorf("invalid temporal bound type %T", value)
		}
		if dataType == schema.TypeDate {
			return temporal.UTC().Format("2006-01-02"), nil
		}
		return temporal.UTC().Format("2006-01-02T15:04:05.000000Z"), nil
	}
	switch v := value.(type) {
	case time.Time:
		return v.UTC().Format("2006-01-02T15:04:05.000000Z"), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		return fmt.Sprint(v), nil
	default:
		return value, nil
	}
}

func (d *D1Destination) SCD2Table(context.Context, destination.SCD2Options) error {
	return fmt.Errorf("D1 does not support the SCD2 strategy")
}

func (d *D1Destination) DropTable(ctx context.Context, table string) error {
	quoted, err := quoteTable(table)
	if err != nil {
		return err
	}
	return d.Exec(ctx, "DROP TABLE IF EXISTS "+quoted)
}

func (d *D1Destination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := d.query(ctx, sql, args...)
	return err
}

func (d *D1Destination) BeginTransaction(context.Context) (destination.Transaction, error) {
	return nil, fmt.Errorf("D1 does not support interactive transactions; use a single SQL batch")
}

func (d *D1Destination) NormalizeSchemaEvolutionColumn(col schema.Column) schema.Column {
	col.DataType = schemaType(typeName(col))
	if col.DataType != schema.TypeDecimal {
		col.Precision, col.Scale = 0, 0
	} else if col.Precision == 0 {
		col.Precision = 38
	}
	col.MaxLength = 0
	col.ArrayType = schema.TypeUnknown
	return col
}

func (d *D1Destination) SupportsColumnTypeChanges() bool { return false }

func (d *D1Destination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	if _, err := quoteTable(table); err != nil {
		return nil, err
	}
	sqlStatements, warnings, err := destination.RenderEvolution(&dialect{}, table, comparison)
	if err != nil {
		return nil, err
	}
	statements := make([]statement, len(sqlStatements))
	for i, sql := range sqlStatements {
		statements[i] = statement{SQL: sql}
	}
	if err := d.executeBatch(ctx, statements); err != nil {
		return warnings, fmt.Errorf("evolve D1 table: %w", err)
	}
	return warnings, nil
}

type dialect struct{}

func (d *dialect) Name() string { return "Cloudflare D1" }

func (d *dialect) AddColumnSQL(table string, col schema.Column) string {
	quoted, _ := quoteTable(table)
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", quoted, destination.QuoteIdentifier(col.Name), typeName(col))
}

func (d *dialect) AlterColumnTypeSQL(string, string, schema.Column) string { return "" }
func (d *dialect) SupportsAlterType() bool                                 { return false }
func (d *dialect) TypeName(col schema.Column) string                       { return typeName(col) }
func (d *dialect) QuoteIdentifier(name string) string                      { return destination.QuoteIdentifier(name) }
