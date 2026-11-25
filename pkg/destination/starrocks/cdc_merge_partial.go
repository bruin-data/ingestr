package starrocks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

// cdcPartialMaxGroups bounds the upserts of one partial merge; batches with
// more distinct unresolved-column sets use the filtered merge instead.
const cdcPartialMaxGroups = 8

// cdcPartialSimpleGroup marks the keys none of whose staged changes carry
// values forward; it cannot collide with a '0'/'1' signature.
const cdcPartialSimpleGroup = "-"

// cdcPartialMergeHint forces column-list INSERT partial updates on StarRocks
// versions that expose enable_insert_partial_update, even if disabled globally.
const cdcPartialMergeHint = "/*+ SET_VAR(cbo_cte_reuse=true, cbo_cte_reuse_rate=0, enable_spill=true, enable_insert_partial_update=true) */"

// cdcPartialPlan resolves a fresh batch from staging alone. Keys none of whose
// staged changes carry values forward are written as their latest row. For
// the others, each payload column takes the newest value the change stream
// materialized in the batch; a column no staged change materialized keeps the
// target's value: the key's upsert omits it, which StarRocks applies as a partial
// update through the primary-key index.
type cdcPartialPlan struct {
	ctes      string
	columns   []string
	payload   []string
	signature string
	rank      string
	carry     string
}

func (d *StarRocksDestination) cdcPartialPlan(opts destination.MergeOptions, skipDeleteCarry bool) cdcPartialPlan {
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
	unique := func(base string) string {
		for slices.ContainsFunc(opts.Columns, func(col string) bool { return strings.EqualFold(base, col) }) {
			base += "_"
		}
		return quoteColumn(base)
	}
	rank, carry, signature := unique("__ingestr_rank"), unique("__ingestr_carry"), unique("__ingestr_partial")
	lsn, deleted := quoteColumn(destination.CDCLSNColumn), quoteColumn(destination.CDCDeletedColumn)
	unchangedCol := quoteColumn(destination.CDCUnchangedColsColumn)
	unchanged := "'[]'"
	if destination.HasCDCUnchangedColsColumn(opts.Columns) {
		unchanged = "__ingestr_k." + unchangedCol
	}
	staged := make([]string, len(cols))
	for i, col := range cols {
		staged[i] = "__ingestr_k." + quoteColumn(col)
	}
	ctes := []string{
		fmt.Sprintf("staged AS (SELECT %s, %s AS %s, MAX(IF(%s, 1, 0)) OVER (PARTITION BY %s) AS %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC, %s DESC) AS %s FROM %s __ingestr_k)",
			strings.Join(staged, ", "), unchanged, unchangedCol, cdcCarryForwardCondition(opts, skipDeleteCarry), keys, carry, keys, lsn, deleted, rank, d.tableRef(opts.StagingTable)),
		fmt.Sprintf("carried AS (SELECT * FROM staged WHERE %s = 1)", carry),
	}
	var selected, missing, joins, payload []string
	for i, col := range cols {
		if keySet[strings.ToLower(col)] || destination.IsCDCColumn(col) {
			selected = append(selected, "__ingestr_l."+quoteColumn(col))
			continue
		}
		payload = append(payload, col)
		name := fmt.Sprintf("__ingestr_value_%d", i)
		materialized := fmt.Sprintf("COALESCE(%s, FALSE) = FALSE AND NOT COALESCE(array_contains(CAST(parse_json(%s) AS ARRAY<VARCHAR>), %s), FALSE)", deleted, unchangedCol, sqlString(col))
		ctes = append(ctes, fmt.Sprintf("%s AS (SELECT %s, %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC) AS %s FROM carried WHERE %s)", name, keys, quoteColumn(col), keys, lsn, rank, materialized))
		joins = append(joins, fmt.Sprintf("LEFT JOIN %s ON %s AND %s.%s = 1", name, join("__ingestr_l", name), name, rank))
		selected = append(selected, name+"."+quoteColumn(col)+" AS "+quoteColumn(col))
		missing = append(missing, fmt.Sprintf("IF(%s.%s IS NULL, '1', '0')", name, quoteColumn(opts.PrimaryKeys[0])))
	}
	sig := "''"
	if len(missing) > 0 {
		sig = "CONCAT(" + strings.Join(missing, ", ") + ")"
		if skipDeleteCarry {
			// A hard-deleted key's tombstone is removed right after the
			// merge, so its unresolved values need not be preserved when all
			// payload columns are nullable.
			sig = fmt.Sprintf("IF(COALESCE(__ingestr_l.%s, FALSE), %s, %s)", deleted, sqlString(strings.Repeat("0", len(missing))), sig)
		}
	}
	ctes = append(ctes, fmt.Sprintf("resolved AS (SELECT %s, %s AS %s FROM carried __ingestr_l %s WHERE __ingestr_l.%s = 1)", strings.Join(selected, ", "), sig, signature, strings.Join(joins, "\n"), rank))
	return cdcPartialPlan{ctes: strings.Join(ctes, ",\n"), columns: cols, payload: payload, signature: signature, rank: rank, carry: carry}
}

func (p cdcPartialPlan) groupsSQL() string {
	return fmt.Sprintf("SELECT %s * FROM (WITH %s SELECT DISTINCT %s AS g FROM resolved UNION SELECT %s FROM staged WHERE %s = 1 AND %s = 0) __ingestr_groups",
		cdcMergeHint, p.ctes, p.signature, sqlString(cdcPartialSimpleGroup), p.rank, p.carry)
}

// upsertSQL writes one group: the complete latest images, or the carried
// keys whose unresolved payload columns match group, omitting those columns.
func (p cdcPartialPlan) upsertSQL(group string) string {
	if group == cdcPartialSimpleGroup {
		list := strings.Join(quoteColumns(p.columns), ", ")
		return fmt.Sprintf("(%s) WITH %s SELECT %s FROM staged WHERE %s = 1 AND %s = 0", list, p.ctes, list, p.rank, p.carry)
	}
	omitted := map[string]bool{}
	for i, col := range p.payload {
		if i < len(group) && group[i] == '1' {
			omitted[col] = true
		}
	}
	var cols []string
	for _, col := range p.columns {
		if !omitted[col] {
			cols = append(cols, quoteColumn(col))
		}
	}
	list := strings.Join(cols, ", ")
	return fmt.Sprintf("(%s) WITH %s SELECT %s FROM resolved WHERE %s = %s", list, p.ctes, list, p.signature, sqlString(group))
}

// mergeCDCPartial reports how many upserts it ran; none, having written
// nothing, when the server applies column-list INSERTs as full upserts or the
// batch has too many distinct unresolved-column sets.
func (d *StarRocksDestination) mergeCDCPartial(ctx context.Context, opts destination.MergeOptions, skipDeleteCarry bool) (int, error) {
	supported, hint, err := d.supportsInsertPartialUpdates(ctx)
	if err != nil || !supported {
		return 0, err
	}
	eligible, err := d.targetSupportsCDCPartialMerge(ctx, opts)
	if err != nil || !eligible {
		return 0, err
	}
	plan := d.cdcPartialPlan(opts, skipDeleteCarry)
	rows, err := d.db.QueryContext(ctx, plan.groupsSQL())
	if err != nil {
		return 0, fmt.Errorf("group StarRocks CDC partial merge: %w", err)
	}
	var groups []string
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			_ = rows.Close()
			return 0, err
		}
		groups = append(groups, group)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(groups) == 0 || len(groups) > cdcPartialMaxGroups {
		return 0, nil
	}
	slices.Sort(groups)
	for i, group := range groups {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return i, err
		}
		if err := d.insertVisibleWithHint(ctx, opts.TargetTable, hint, plan.upsertSQL(group)); err != nil {
			return i + 1, fmt.Errorf("merge StarRocks CDC changes: %w", err)
		}
	}
	return len(groups), nil
}

// targetSupportsCDCPartialMerge reports whether StarRocks accepts partial
// updates omitting payload columns on opts.TargetTable. StarRocks rejects
// partial-update INSERTs that omit a decoupled ORDER BY sort-key column or a
// dependency of a generated column (other than __ingestr_cdc_order, which
// depends only on _cdc_lsn).
func (d *StarRocksDestination) targetSupportsCDCPartialMerge(ctx context.Context, opts destination.MergeOptions) (bool, error) {
	db, name := d.tableParts(opts.TargetTable)
	var primaryKey, sortKey sql.NullString
	err := d.db.QueryRowContext(ctx, `SELECT PRIMARY_KEY, SORT_KEY FROM information_schema.tables_config WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, db, name).Scan(&primaryKey, &sortKey)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read StarRocks CDC target sort key: %w", err)
	}
	if !sortKeyWithinPrimaryKeys(primaryKey.String, sortKey.String, opts.PrimaryKeys) {
		return false, nil
	}
	rows, err := d.db.QueryContext(ctx, `SELECT COLUMN_NAME, GENERATION_EXPRESSION FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, db, name)
	if err != nil {
		return false, fmt.Errorf("read StarRocks CDC target generated columns: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var col string
		var expr sql.NullString
		if err := rows.Scan(&col, &expr); err != nil {
			return false, err
		}
		if strings.TrimSpace(expr.String) != "" && !strings.EqualFold(col, cdcOrderColumn) {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return true, nil
}

func sortKeyWithinPrimaryKeys(primaryKey, sortKey string, primaryKeys []string) bool {
	sortKey = strings.TrimSpace(sortKey)
	if sortKey == "" || strings.EqualFold(sortKey, strings.TrimSpace(primaryKey)) {
		return true
	}
	keySet := make(map[string]bool, len(primaryKeys))
	for _, key := range primaryKeys {
		keySet[strings.ToLower(key)] = true
	}
	var parts []string
	if strings.HasPrefix(sortKey, "`") && strings.HasSuffix(sortKey, "`") {
		parts = strings.Split(sortKey[1:len(sortKey)-1], "`, `")
	} else {
		parts = strings.Split(sortKey, ",")
	}
	for _, part := range parts {
		col := strings.Trim(strings.TrimSpace(part), "`")
		if col == "" || !keySet[strings.ToLower(col)] {
			return false
		}
	}
	return true
}

// supportsInsertPartialUpdates reports whether an INSERT with a column list
// on a Primary Key table keeps the omitted columns, which StarRocks does from
// v3.3.1, and returns the optimizer hint to enforce partial updates when the
// server exposes enable_insert_partial_update.
func (d *StarRocksDestination) supportsInsertPartialUpdates(ctx context.Context) (bool, string, error) {
	d.insertPartialMu.Lock()
	defer d.insertPartialMu.Unlock()
	if !d.insertPartialChecked {
		var version string
		if err := d.db.QueryRowContext(ctx, "SELECT current_version()").Scan(&version); err != nil {
			return false, "", fmt.Errorf("read StarRocks version for CDC partial merges: %w", err)
		}
		if !versionAtLeast(version, 3, 3, 1) {
			d.insertPartialUpdates = false
			d.insertPartialChecked = true
			return false, "", nil
		}
		rows, err := d.db.QueryContext(ctx, "SHOW VARIABLES LIKE 'enable_insert_partial_update'")
		if err != nil {
			return false, "", fmt.Errorf("read StarRocks partial update setting: %w", err)
		}
		hasVar := rows.Next()
		rowErr := rows.Err()
		closeErr := rows.Close()
		if rowErr != nil {
			return false, "", fmt.Errorf("read StarRocks partial update setting: %w", rowErr)
		}
		if closeErr != nil {
			return false, "", fmt.Errorf("read StarRocks partial update setting: %w", closeErr)
		}
		d.insertPartialUpdates = true
		d.insertPartialHint = cdcMergeHint
		if hasVar {
			d.insertPartialHint = cdcPartialMergeHint
		}
		d.insertPartialChecked = true
	}
	hint := d.insertPartialHint
	if hint == "" {
		hint = cdcMergeHint
	}
	return d.insertPartialUpdates, hint, nil
}

func versionAtLeast(version string, want ...int) bool {
	parts := strings.Split(strings.SplitN(strings.TrimSpace(version), "-", 2)[0], ".")
	for i, minimum := range want {
		if i >= len(parts) {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return false
		}
		if n != minimum {
			return n > minimum
		}
	}
	return true
}
