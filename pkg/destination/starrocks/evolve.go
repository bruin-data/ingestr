package starrocks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

type Dialect struct {
	columns map[string]schema.Column
	// current holds the existing columns, so type changes StarRocks rejects
	// fail before any DDL runs instead of after a partial migration.
	current map[string]schema.Column
	// rawTypes holds COLUMN_TYPE; columns whose type ingestr cannot render
	// back (LARGEINT, CHAR, MAP, STRUCT, ...) are never modified.
	rawTypes map[string]string
	// distribution holds DISTRIBUTED BY HASH columns, which StarRocks never
	// modifies; frozen marks key models (UNIQUE/AGGREGATE) whose key columns
	// ingestr does not track.
	distribution map[string]bool
	frozen       bool
	// Duplicate Key sort columns must be modified with KEY, or StarRocks
	// rejects the change as an aggregation/key-order change.
	sortKeys map[string]bool
}

func (*Dialect) Name() string                       { return "StarRocks" }
func (*Dialect) SupportsAlterType() bool            { return true }
func (*Dialect) TypeName(col schema.Column) string  { return mapStarRocksType(col) }
func (*Dialect) QuoteIdentifier(name string) string { return quoteColumn(name) }
func (*Dialect) AddColumnSQL(table string, col schema.Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s NULL", table, quoteColumn(col.Name), MapDataTypeToStarRocks(col))
}

func (d *Dialect) AlterColumnTypeSQL(table, name string, col schema.Column) string {
	if planned, ok := d.columns[strings.ToLower(name)]; ok {
		col = planned
	}
	if old, ok := d.current[strings.ToLower(name)]; ok && (!d.rendersExisting(name) || !canModifyColumnType(old, col, d.sortKeys[strings.ToLower(name)])) {
		return ""
	}
	nullable := " NULL"
	if col.IsPrimaryKey || !col.Nullable {
		nullable = " NOT NULL"
	}
	return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s%s%s", table, quoteColumn(name), mapStarRocksType(col), d.keySuffix(name), nullable)
}

// canModifyColumnType mirrors the MODIFY COLUMN conversions StarRocks 3.3
// accepts; everything else fails with "Can not change X to Y".
func canModifyColumnType(old, col schema.Column, sortKey bool) bool {
	from, to := starRocksBaseType(old), starRocksBaseType(col)
	if sortKey && (to == "FLOAT" || to == "DOUBLE" || to == "JSON" || to == "ARRAY") {
		return false
	}
	if from == to {
		switch from {
		case "VARCHAR":
			return true
		case "DECIMAL":
			op, os := starRocksDecimal(old.Precision, old.Scale)
			np, ns := starRocksDecimal(col.Precision, col.Scale)
			return ns >= os && np-ns >= op-os
		}
		return false
	}
	integers := map[string]int{"TINYINT": 1, "SMALLINT": 2, "INT": 3, "BIGINT": 4}
	switch {
	case integers[from] > 0 && integers[to] > integers[from]:
		return true
	case (integers[from] > 0 || from == "FLOAT") && to == "DOUBLE":
		return true
	case to == "VARCHAR":
		// StarRocks rejects DECIMAL and JSON targets below these lengths;
		// numbers are accepted but the job is cancelled if a value is longer.
		minLength := map[string]int{"TINYINT": 4, "SMALLINT": 6, "INT": 11, "BIGINT": 20, "FLOAT": 24, "DOUBLE": 24, "JSON": 1025}[from]
		if from == "DECIMAL" {
			precision, _ := starRocksDecimal(old.Precision, old.Scale)
			minLength = precision + 5
		}
		return minLength > 0 && (col.MaxLength <= 0 || col.MaxLength >= minLength)
	case from == "VARCHAR":
		return to != "BOOLEAN" && to != "DATETIME" && to != "ARRAY" && to != "VARBINARY"
	case from == "DATE" && to == "DATETIME", from == "DATETIME" && to == "DATE":
		return true
	}
	return false
}

func starRocksBaseType(col schema.Column) string {
	name := mapStarRocksType(col)
	if i := strings.IndexAny(name, "(<"); i >= 0 {
		name = name[:i]
	}
	return name
}

func (d *Dialect) keySuffix(name string) string {
	if d.sortKeys[strings.ToLower(name)] {
		return " KEY"
	}
	return ""
}

func (d *Dialect) RelaxColumnNullabilitySQL(table, name string) string {
	col, ok := d.columns[strings.ToLower(name)]
	if !ok || col.IsPrimaryKey || !d.rendersExisting(name) {
		return ""
	}
	return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s%s NULL", table, quoteColumn(name), mapStarRocksType(col), d.keySuffix(name))
}

// rendersExisting reports whether StarRocks may modify the existing column
// and its DDL type is what ingestr renders for it, so a MODIFY COLUMN can
// neither fail mid-migration nor silently change the type.
func (d *Dialect) rendersExisting(name string) bool {
	key := strings.ToLower(name)
	if d.frozen || d.distribution[key] {
		return false
	}
	raw, ok := d.rawTypes[key]
	if !ok {
		return true
	}
	return normalizeColumnType(raw) == normalizeColumnType(mapStarRocksType(d.current[key]))
}

var (
	integerDisplayWidth = regexp.MustCompile(`\b(tinyint|smallint|int|bigint)\(\d+\)`)
	sizedDecimal        = regexp.MustCompile(`\bdecimal(32|64|128)\(`)
)

func normalizeColumnType(t string) string {
	t = strings.ReplaceAll(strings.ToLower(t), " ", "")
	t = strings.ReplaceAll(t, "tinyint(1)", "boolean")
	t = integerDisplayWidth.ReplaceAllString(t, "$1")
	return sizedDecimal.ReplaceAllString(t, "decimal(")
}

func (*StarRocksDestination) SupportsColumnTypeChanges() bool { return true }
func (*StarRocksDestination) MaxDecimalPrecision() int        { return maxDecimal128Precision }

func (d *StarRocksDestination) NormalizeSchemaEvolutionColumn(col schema.Column) schema.Column {
	switch col.DataType {
	case schema.TypeTimestampTZ:
		col.DataType = schema.TypeTimestamp
	case schema.TypeUUID, schema.TypeInterval:
		col.DataType = schema.TypeString
	case schema.TypeTime:
		col.DataType, col.MaxLength = schema.TypeString, 16
	case schema.TypeArray:
		if _, ok := starRocksArrayElement(col); !ok {
			col.DataType, col.ArrayType = schema.TypeJSON, schema.TypeUnknown
		} else {
			element := d.NormalizeSchemaEvolutionColumn(schema.Column{DataType: col.ArrayType})
			col.ArrayType = element.DataType
		}
	}
	if col.DataType == schema.TypeString && col.MaxLength > maxVarcharBytes {
		col.MaxLength = maxVarcharBytes
	}
	// Compare decimals as the DECIMAL(p,s) they are created with.
	if col.DataType == schema.TypeDecimal || (col.DataType == schema.TypeArray && col.ArrayType == schema.TypeDecimal) {
		col.Precision, col.Scale = starRocksDecimal(col.Precision, col.Scale)
	}
	return col
}

// Source string lengths are characters; destination lengths are bytes.
// Primary-key columns cannot be modified, so a key that already fits its
// existing declaration is not widened.
func (d *StarRocksDestination) NormalizeSchemaEvolutionSourceColumn(source, dest schema.Column) schema.Column {
	if source.DataType != schema.TypeString {
		return source
	}
	// Unbounded sources keep legacy VARCHAR(65533) columns (widening them would
	// alter every existing table) and primary keys (which cannot be modified);
	// narrower columns widen to the maximum.
	if source.MaxLength <= 0 && dest.DataType == schema.TypeString && (dest.MaxLength >= 65533 || (dest.IsPrimaryKey && dest.MaxLength > 0)) {
		source.MaxLength = dest.MaxLength
		return source
	}
	if dest.IsPrimaryKey && dest.DataType == schema.TypeString && source.MaxLength > 0 && source.MaxLength <= dest.MaxLength {
		source.MaxLength = dest.MaxLength
		return source
	}
	source.MaxLength = varcharBytes(source.MaxLength)
	return source
}

func (d *StarRocksDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	current, err := d.GetTableSchema(ctx, table)
	if err != nil {
		return nil, err
	}
	sortKeys, err := d.duplicateSortKeys(ctx, table)
	if err != nil {
		return nil, err
	}
	if comparison != nil {
		for _, change := range comparison.Changes {
			if change.Type == schemaevolution.ChangeAddColumn && strings.Contains(change.ColumnName, "`") {
				return nil, fmt.Errorf("StarRocks cannot add column %q: column names containing a backtick are not supported", change.ColumnName)
			}
		}
	}
	comparison = overridesInBytes(comparison)
	rawTypes, err := d.columnTypes(ctx, table)
	if err != nil {
		return nil, err
	}
	model, distributeKey, err := d.tableLayout(ctx, table)
	if err != nil {
		return nil, err
	}
	dialect := &Dialect{columns: map[string]schema.Column{}, current: map[string]schema.Column{}, rawTypes: rawTypes, sortKeys: sortKeys, distribution: map[string]bool{}}
	dialect.frozen = model != "" && !strings.EqualFold(model, "DUP_KEYS") && !strings.EqualFold(model, "PRIMARY_KEYS")
	// DISTRIBUTE_KEY renders `a`, `b`; StarRocks names cannot contain
	// backticks, so splitting on the separator is exact.
	if key := strings.TrimSpace(distributeKey); len(key) >= 2 {
		for _, col := range strings.Split(key[1:len(key)-1], "`, `") {
			dialect.distribution[strings.ToLower(col)] = true
		}
	}
	if current != nil {
		for _, col := range current.Columns {
			dialect.current[strings.ToLower(col.Name)] = col
		}
		for _, col := range schemaevolution.BuildFinalSchema(current, comparison).Columns {
			for _, key := range current.PrimaryKeys {
				if strings.EqualFold(col.Name, key) {
					col.IsPrimaryKey = true
				}
			}
			dialect.columns[strings.ToLower(col.Name)] = col
		}
	}
	if comparison != nil {
		for _, change := range comparison.Changes {
			if change.Type == schemaevolution.ChangeRelaxNullability || change.Type == schemaevolution.ChangeRemoveColumn {
				key := strings.ToLower(change.ColumnName)
				col := dialect.columns[key]
				col.Nullable = true
				dialect.columns[key] = col
			}
		}
	}
	if comparison != nil {
		for _, change := range comparison.Changes {
			if change.Type != schemaevolution.ChangeWidenType && change.Type != schemaevolution.ChangeOverrideType {
				continue
			}
			if col, ok := dialect.columns[strings.ToLower(change.ColumnName)]; ok && col.IsPrimaryKey {
				return nil, fmt.Errorf("StarRocks cannot change the type of primary key column %q in %s; rerun with --full-refresh to rebuild the table", change.ColumnName, table)
			}
		}
	}
	statements, warnings, err := destination.RenderEvolution(dialect, d.tableRef(table), comparison)
	if err != nil {
		return nil, fmt.Errorf("%w; rerun with --full-refresh to rebuild the table", err)
	}
	for _, statement := range statements {
		prior, err := d.showStatus(ctx, d.schemaChangeQuery(table))
		if err != nil {
			return warnings, fmt.Errorf("read previous StarRocks schema change: %w", err)
		}
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return warnings, fmt.Errorf("apply StarRocks schema evolution: %w", err)
		}
		if err := d.waitForSchemaChange(ctx, table, prior["JobId"]); err != nil {
			return warnings, err
		}
	}
	return warnings, nil
}

// An override that turns a non-string column into a string carries the
// source's character length; every other evolved length is already in bytes.
func overridesInBytes(comparison *schemaevolution.SchemaComparison) *schemaevolution.SchemaComparison {
	if comparison == nil {
		return nil
	}
	converted := *comparison
	converted.Changes = slices.Clone(comparison.Changes)
	for i, change := range converted.Changes {
		if change.Type != schemaevolution.ChangeOverrideType || change.OldColumn == nil || change.OldColumn.DataType == schema.TypeString {
			continue
		}
		switch change.NewColumn.DataType {
		case schema.TypeString, schema.TypeUUID, schema.TypeInterval:
			converted.Changes[i].NewColumn.MaxLength = varcharBytes(change.NewColumn.MaxLength)
		}
	}
	return &converted
}

func (d *StarRocksDestination) duplicateSortKeys(ctx context.Context, table string) (map[string]bool, error) {
	db, name := d.tableParts(table)
	rows, err := d.db.QueryContext(ctx, "SELECT COLUMN_NAME FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_KEY = 'DUP'", db, name)
	if err != nil {
		return nil, fmt.Errorf("read StarRocks sort keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	keys := map[string]bool{}
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		keys[strings.ToLower(col)] = true
	}
	return keys, rows.Err()
}

func (d *StarRocksDestination) columnTypes(ctx context.Context, table string) (map[string]string, error) {
	db, name := d.tableParts(table)
	rows, err := d.db.QueryContext(ctx, "SELECT COLUMN_NAME, COLUMN_TYPE FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", db, name)
	if err != nil {
		return nil, fmt.Errorf("read StarRocks column types: %w", err)
	}
	defer func() { _ = rows.Close() }()
	types := map[string]string{}
	for rows.Next() {
		var col, columnType string
		if err := rows.Scan(&col, &columnType); err != nil {
			return nil, err
		}
		types[strings.ToLower(col)] = columnType
	}
	return types, rows.Err()
}

func (d *StarRocksDestination) tableLayout(ctx context.Context, table string) (model, distributeKey string, err error) {
	db, name := d.tableParts(table)
	var key sql.NullString
	err = d.db.QueryRowContext(ctx, `SELECT TABLE_MODEL, DISTRIBUTE_KEY FROM information_schema.tables_config WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, db, name).Scan(&model, &key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read StarRocks table layout: %w", err)
	}
	return model, key.String, nil
}

func (d *StarRocksDestination) schemaChangeQuery(table string) string {
	db, name := d.tableParts(table)
	return fmt.Sprintf("SHOW ALTER TABLE COLUMN FROM %s WHERE TableName = %s ORDER BY JobId DESC LIMIT 1", quoteColumn(db), sqlString(name))
}

func (d *StarRocksDestination) waitForSchemaChange(ctx context.Context, table, previousJob string) error {
	// MODIFY COLUMN rewrites data and may run long on large tables; abandoning
	// it would only leave the next run blocked behind the same job.
	query := d.schemaChangeQuery(table)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := d.showStatus(ctx, query)
		if err != nil {
			return fmt.Errorf("read StarRocks schema change: %w", err)
		}
		// Fast schema evolution completes inside ALTER and creates no new job.
		if len(status) == 0 || status["JobId"] == previousJob {
			return nil
		}
		switch status["State"] {
		case "FINISHED":
			return nil
		case "CANCELLED":
			return fmt.Errorf("StarRocks schema change cancelled: %s", status["Msg"])
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for StarRocks schema change: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (d *StarRocksDestination) showStatus(ctx context.Context, query string) (map[string]string, error) {
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	status := map[string]string{}
	if rows.Next() {
		values, pointers := make([]sql.NullString, len(columns)), make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		for i, col := range columns {
			status[col] = values[i].String
		}
	}
	return status, rows.Err()
}

func (d *StarRocksDestination) primaryKeys(ctx context.Context, table string) ([]string, string, error) {
	db, name := d.tableParts(table)
	var model string
	err := d.db.QueryRowContext(ctx, `SELECT TABLE_MODEL FROM information_schema.tables_config WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, db, name).Scan(&model)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("read StarRocks primary key: %w", err)
	}
	if !strings.EqualFold(model, "PRIMARY_KEYS") {
		return nil, model, nil
	}
	// PRIMARY_KEY is a rendered list that cannot be split safely when names
	// contain commas or spaces; key columns lead the table in key order.
	rows, err := d.db.QueryContext(ctx, `SELECT COLUMN_NAME FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_KEY = 'PRI' ORDER BY ORDINAL_POSITION`, db, name)
	if err != nil {
		return nil, "", fmt.Errorf("read StarRocks primary key columns: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, "", err
		}
		keys = append(keys, col)
	}
	return keys, model, rows.Err()
}

var _ schemaevolution.SchemaEvolver = (*StarRocksDestination)(nil)
