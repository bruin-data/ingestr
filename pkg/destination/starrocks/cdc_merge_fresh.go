package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

type cdcMergeMode int

const (
	// cdcMergeAuto merges fresh batches without the stale-change guard and,
	// once the run has certified the table, writes carried keys as
	// partial-update upserts.
	cdcMergeAuto cdcMergeMode = iota
	// cdcMergeAtomic never splits a batch across statements.
	cdcMergeAtomic
	// cdcMergeLegacy always runs the guarded merge over the whole target.
	cdcMergeLegacy
)

// cdcMergeModeFromEnv reads INGESTR_STARROCKS_CDC_MERGE (auto, atomic or
// legacy), an escape hatch for comparing or disabling the fast paths.
func cdcMergeModeFromEnv() cdcMergeMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("INGESTR_STARROCKS_CDC_MERGE"))) {
	case "atomic":
		return cdcMergeAtomic
	case "legacy":
		return cdcMergeLegacy
	default:
		return cdcMergeAuto
	}
}

// cdcKeyFilterLimit keeps each IN list well below StarRocks'
// expr_children_limit (10000).
const cdcKeyFilterLimit = 4096

// cdcKeyFilter bounds target reads to a superset of the keys of some staged
// rows: an IN list per key column, or MIN/MAX bounds beyond the limit. Key
// columns without an exact literal round trip are left unbounded.
type cdcKeyFilter struct {
	keys   []string
	values [][]string
	lower  []string
	upper  []string
}

func (f cdcKeyFilter) predicate(qualifier string) string {
	var terms []string
	for i, key := range f.keys {
		col := qualifier + "." + quoteColumn(key)
		switch {
		case f.values != nil && len(f.values[i]) == 0:
			terms = append(terms, "FALSE")
		case f.values != nil:
			terms = append(terms, col+" IN ("+strings.Join(f.values[i], ", ")+")")
		case f.lower != nil && f.lower[i] != "":
			terms = append(terms, fmt.Sprintf("%s BETWEEN %s AND %s", col, f.lower[i], f.upper[i]))
		}
	}
	return strings.Join(terms, " AND ")
}

// cdcFilterLiteral renders a key value read back from StarRocks as a literal
// of the same column type; ok is false for types without an exact round trip.
func cdcFilterLiteral(dataType schema.DataType, value string) (string, bool) {
	switch dataType {
	case schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			if _, err := strconv.ParseUint(value, 10, 64); err != nil {
				return "", false
			}
		}
		return value, true
	case schema.TypeString, schema.TypeDate:
		return sqlString(value), true
	default:
		return "", false
	}
}

// cdcBatchIsFresh reports whether every staged change is newer than every
// target row. The merge then accepts each staged row without comparing it to
// the target. Zone maps on _cdc_lsn answer the probe without reading rows
// when it holds.
func (d *StarRocksDestination) cdcBatchIsFresh(ctx context.Context, opts destination.MergeOptions) (bool, error) {
	lsn := quoteColumn(destination.CDCLSNColumn)
	var minLSN sql.NullString
	var missing int64
	if err := d.db.QueryRowContext(ctx, fmt.Sprintf("SELECT MIN(%s), COUNT(*) - COUNT(%s) FROM %s", lsn, lsn, d.tableRef(opts.StagingTable))).Scan(&minLSN, &missing); err != nil {
		return false, fmt.Errorf("read StarRocks CDC staged positions: %w", err)
	}
	if !minLSN.Valid || missing > 0 {
		return false, nil
	}
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf("SELECT 1 FROM %s WHERE %s >= %s LIMIT 1", d.tableRef(opts.TargetTable), lsn, sqlString(minLSN.String)))
	if err != nil {
		return false, fmt.Errorf("probe StarRocks CDC target positions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	newer := rows.Next()
	return !newer, rows.Err()
}

// cdcHardDeleteCanSkipCarry reports whether hard-delete tombstones can omit
// target payload reads without writing NULL into a NOT NULL non-key column.
func (d *StarRocksDestination) cdcHardDeleteCanSkipCarry(ctx context.Context, opts destination.MergeOptions) (bool, error) {
	if !opts.CDCHardDelete {
		return false, nil
	}
	db, name := d.tableParts(opts.TargetTable)
	rows, err := d.db.QueryContext(ctx, `SELECT COLUMN_NAME, IS_NULLABLE FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, db, name)
	if err != nil {
		return false, fmt.Errorf("read StarRocks CDC target nullability: %w", err)
	}
	defer func() { _ = rows.Close() }()
	keySet := map[string]bool{}
	for _, key := range opts.PrimaryKeys {
		keySet[strings.ToLower(key)] = true
	}
	for rows.Next() {
		var col, isNullable string
		if err := rows.Scan(&col, &isNullable); err != nil {
			return false, err
		}
		if strings.EqualFold(isNullable, "YES") || keySet[strings.ToLower(col)] || destination.IsCDCColumn(col) || strings.EqualFold(col, cdcOrderColumn) {
			continue
		}
		return false, nil
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return true, nil
}

// cdcCarryForwardCondition selects the staged rows whose merged result takes
// values from the target row: unchanged TOAST columns, and tombstones unless
// skipDeleteCarry is true (hard deletes on a table with nullable payload columns).
func cdcCarryForwardCondition(opts destination.MergeOptions, skipDeleteCarry bool) string {
	deleted := "COALESCE(__ingestr_k." + quoteColumn(destination.CDCDeletedColumn) + ", FALSE)"
	unchanged := "FALSE"
	if destination.HasCDCUnchangedColsColumn(opts.Columns) {
		unchanged = "COALESCE(array_length(CAST(parse_json(__ingestr_k." + quoteColumn(destination.CDCUnchangedColsColumn) + ") AS ARRAY<VARCHAR>)), 0) > 0"
	}
	if skipDeleteCarry {
		return "NOT " + deleted + " AND " + unchanged
	}
	return deleted + " OR " + unchanged
}

// cdcFilterStarRocksType maps a StarRocks DATA_TYPE to its filter literal type
// when values round-trip losslessly and sort identically across staging and
// target. LARGEINT and CHAR are excluded so they remain unbounded.
func cdcFilterStarRocksType(dataType string) (schema.DataType, bool) {
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "tinyint":
		return schema.TypeInt8, true
	case "smallint":
		return schema.TypeInt16, true
	case "int", "integer":
		return schema.TypeInt32, true
	case "bigint":
		return schema.TypeInt64, true
	case "varchar", "string":
		return schema.TypeString, true
	case "date":
		return schema.TypeDate, true
	default:
		return schema.TypeString, false
	}
}

func (d *StarRocksDestination) tableColumnTypes(ctx context.Context, table string) (map[string]string, error) {
	db, name := d.tableParts(table)
	rows, err := d.db.QueryContext(ctx, `SELECT COLUMN_NAME, DATA_TYPE FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, db, name)
	if err != nil {
		return nil, fmt.Errorf("read StarRocks column types for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	types := map[string]string{}
	for rows.Next() {
		var col, dataType string
		if err := rows.Scan(&col, &dataType); err != nil {
			return nil, err
		}
		types[strings.ToLower(col)] = dataType
	}
	return types, rows.Err()
}

// stagedKeyFilter bounds the keys of the staged rows matching where.
func (d *StarRocksDestination) stagedKeyFilter(ctx context.Context, opts destination.MergeOptions, where string) (cdcKeyFilter, error) {
	var filter cdcKeyFilter
	if len(opts.PrimaryKeys) == 0 {
		return filter, nil
	}
	targetTypes, err := d.tableColumnTypes(ctx, opts.TargetTable)
	if err != nil {
		return filter, err
	}
	stagingTypes, err := d.tableColumnTypes(ctx, opts.StagingTable)
	if err != nil {
		return filter, err
	}
	var types []schema.DataType
	for _, key := range opts.PrimaryKeys {
		lower := strings.ToLower(key)
		targetType, targetOK := cdcFilterStarRocksType(targetTypes[lower])
		stagingType, stagingOK := cdcFilterStarRocksType(stagingTypes[lower])
		if !targetOK || !stagingOK || targetType != stagingType {
			continue
		}
		filter.keys = append(filter.keys, key)
		types = append(types, targetType)
	}
	if len(filter.keys) == 0 {
		return filter, nil
	}
	source := d.tableRef(opts.StagingTable) + " __ingestr_k"
	if where != "" {
		source += " WHERE " + where
	}
	casts := make([]string, len(filter.keys))
	for i, key := range filter.keys {
		casts[i] = "CAST(__ingestr_k." + quoteColumn(key) + " AS VARCHAR)"
	}
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf("SELECT DISTINCT %s FROM %s LIMIT %d", strings.Join(casts, ", "), source, cdcKeyFilterLimit+1))
	if err != nil {
		return filter, fmt.Errorf("read StarRocks CDC staged keys: %w", err)
	}
	sets := make([]map[string]bool, len(filter.keys))
	for i := range sets {
		sets[i] = map[string]bool{}
	}
	tuples, exact := 0, true
	for rows.Next() {
		raw := make([]sql.NullString, len(filter.keys))
		ptrs := make([]any, len(raw))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			_ = rows.Close()
			return filter, err
		}
		tuples++
		for i, v := range raw {
			literal, ok := cdcFilterLiteral(types[i], v.String)
			if !v.Valid || !ok {
				exact = false
				continue
			}
			sets[i][literal] = true
		}
	}
	if err := rows.Close(); err != nil {
		return filter, err
	}
	if err := rows.Err(); err != nil {
		return filter, err
	}
	if !exact {
		return cdcKeyFilter{}, nil
	}
	if tuples <= cdcKeyFilterLimit {
		filter.values = make([][]string, len(filter.keys))
		for i, set := range sets {
			filter.values[i] = []string{}
			for literal := range set {
				filter.values[i] = append(filter.values[i], literal)
			}
			slices.Sort(filter.values[i])
		}
		return filter, nil
	}
	bounds := make([]string, 0, 2*len(filter.keys))
	for _, key := range filter.keys {
		bounds = append(bounds, "CAST(MIN(__ingestr_k."+quoteColumn(key)+") AS VARCHAR)", "CAST(MAX(__ingestr_k."+quoteColumn(key)+") AS VARCHAR)")
	}
	raw := make([]sql.NullString, len(bounds))
	ptrs := make([]any, len(raw))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := d.db.QueryRowContext(ctx, fmt.Sprintf("SELECT %s FROM %s", strings.Join(bounds, ", "), source)).Scan(ptrs...); err != nil {
		return filter, fmt.Errorf("read StarRocks CDC staged key bounds: %w", err)
	}
	filter.lower, filter.upper = make([]string, len(filter.keys)), make([]string, len(filter.keys))
	for i := range filter.keys {
		lo, okLo := cdcFilterLiteral(types[i], raw[2*i].String)
		hi, okHi := cdcFilterLiteral(types[i], raw[2*i+1].String)
		if !raw[2*i].Valid || !raw[2*i+1].Valid || !okLo || !okHi {
			continue
		}
		filter.lower[i], filter.upper[i] = lo, hi
	}
	return filter, nil
}

// buildCDCFreshMergeSQL merges a batch that is newer than every target row in
// one INSERT. Keys none of whose staged changes carry values forward are
// written from staging without reading the target. Only keys with a change
// that carries values forward (unchanged TOAST columns, soft-delete
// tombstones) resolve each payload column against their staged history and
// current target row, read through filter.
func (d *StarRocksDestination) buildCDCFreshMergeSQL(opts destination.MergeOptions, filter cdcKeyFilter, skipDeleteCarry bool) string {
	cols := destination.DestinationColumns(opts.Columns)
	keys := strings.Join(quoteColumns(opts.PrimaryKeys), ", ")
	keySet := map[string]bool{}
	for _, key := range opts.PrimaryKeys {
		keySet[strings.ToLower(key)] = true
	}
	join := func(a, b string) string {
		parts := make([]string, len(opts.PrimaryKeys))
		for i, key := range opts.PrimaryKeys {
			parts[i] = a + "." + quoteColumn(key) + " = " + b + "." + quoteColumn(key)
		}
		return strings.Join(parts, " AND ")
	}
	prefixed := func(alias string) string {
		values := make([]string, len(cols))
		for i, col := range cols {
			values[i] = alias + "." + quoteColumn(col)
		}
		return strings.Join(values, ", ")
	}
	unique := func(base string) string {
		for slices.ContainsFunc(opts.Columns, func(col string) bool { return strings.EqualFold(base, col) }) {
			base += "_"
		}
		return quoteColumn(base)
	}
	rank, carry, origin := unique("__ingestr_rank"), unique("__ingestr_carry"), unique("__ingestr_target")
	lsn, deleted := quoteColumn(destination.CDCLSNColumn), quoteColumn(destination.CDCDeletedColumn)
	unchangedCol := quoteColumn(destination.CDCUnchangedColsColumn)
	unchanged := "'[]'"
	if destination.HasCDCUnchangedColsColumn(opts.Columns) {
		unchanged = "__ingestr_k." + unchangedCol
	}
	need := cdcCarryForwardCondition(opts, skipDeleteCarry)
	targetConditions := []string{fmt.Sprintf("EXISTS (SELECT 1 FROM %s __ingestr_k WHERE %s AND (%s))", d.tableRef(opts.StagingTable), join("__ingestr_k", "__ingestr_f"), need)}
	if predicate := filter.predicate("__ingestr_f"); predicate != "" {
		targetConditions = append([]string{predicate}, targetConditions...)
	}
	ctes := []string{
		fmt.Sprintf("staged AS (SELECT %s, %s AS %s, MAX(IF(%s, 1, 0)) OVER (PARTITION BY %s) AS %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC, %s DESC) AS %s FROM %s __ingestr_k)",
			prefixed("__ingestr_k"), unchanged, unchangedCol, need, keys, carry, keys, lsn, deleted, rank, d.tableRef(opts.StagingTable)),
		fmt.Sprintf("__ingestr_tgt AS (SELECT %s FROM %s __ingestr_f WHERE %s)", prefixed("__ingestr_f"), d.tableRef(opts.TargetTable), strings.Join(targetConditions, " AND ")),
		fmt.Sprintf(`history AS (
 SELECT %s, %s, 0 AS %s FROM staged __ingestr_s WHERE __ingestr_s.%s = 1
 UNION ALL
 SELECT %s, '[]', 1 FROM __ingestr_tgt __ingestr_t
)`, prefixed("__ingestr_s"), "__ingestr_s."+unchangedCol, origin, carry, prefixed("__ingestr_t")),
		fmt.Sprintf("latest AS (SELECT *, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC, %s DESC, %s DESC) AS %s FROM history)", keys, lsn, deleted, origin, rank),
	}
	selected, joins := make([]string, len(cols)), []string{}
	for i, col := range cols {
		selected[i] = "__ingestr_l." + quoteColumn(col)
		if keySet[strings.ToLower(col)] || destination.IsCDCColumn(col) {
			continue
		}
		materialized := fmt.Sprintf("%s = 1 OR (COALESCE(%s, FALSE) = FALSE AND NOT COALESCE(array_contains(CAST(parse_json(%s) AS ARRAY<VARCHAR>), %s), FALSE))", origin, deleted, unchangedCol, sqlString(col))
		name := fmt.Sprintf("__ingestr_value_%d", i)
		ctes = append(ctes, fmt.Sprintf("%s AS (SELECT %s, %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC, %s ASC) AS %s FROM history WHERE %s)", name, keys, quoteColumn(col), keys, lsn, origin, rank, materialized))
		joins = append(joins, fmt.Sprintf("LEFT JOIN %s ON %s AND %s.%s = 1", name, join("__ingestr_l", name), name, rank))
		selected[i] = name + "." + quoteColumn(col)
	}
	return fmt.Sprintf("(%s) WITH %s SELECT %s FROM latest __ingestr_l %s WHERE __ingestr_l.%s = 1\nUNION ALL\nSELECT %s FROM staged __ingestr_k WHERE __ingestr_k.%s = 1 AND __ingestr_k.%s = 0",
		strings.Join(quoteColumns(cols), ", "), strings.Join(ctes, ",\n"), strings.Join(selected, ", "), strings.Join(joins, "\n"), rank, prefixed("__ingestr_k"), rank, carry)
}
