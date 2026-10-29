package starrocks

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
)

func (d *StarRocksDestination) SupportsCDCMerge() bool         { return true }
func (d *StarRocksDestination) SupportsCDCUnchangedCols() bool { return true }
func (d *StarRocksDestination) SupportsCDCHardDelete() bool    { return true }

func (d *StarRocksDestination) mergeCDC(ctx context.Context, opts destination.MergeOptions) error {
	if opts.IncrementalPredicate != "" {
		return fmt.Errorf("StarRocks CDC merge does not support incremental predicates")
	}
	if len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("StarRocks CDC merge requires primary keys")
	}
	if err := d.validatePrimaryKey(ctx, "CDC target", opts.TargetTable, opts.PrimaryKeys); err != nil {
		return err
	}
	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if err := d.insertVisibleWithHint(ctx, opts.TargetTable, cdcMergeHint, d.buildCDCMergeSQL(opts)); err != nil {
		return fmt.Errorf("merge StarRocks CDC changes: %w", err)
	}
	if opts.CDCHardDelete {
		query := d.buildCDCHardDeleteSQL(opts)
		if err := d.deleteVisible(ctx, opts.TargetTable, query); err != nil {
			config.LogFailedQuery(query, err)
			return fmt.Errorf("delete StarRocks CDC-deleted rows: %w", err)
		}
	}
	return d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation)
}

// buildCDCHardDeleteSQL removes the tombstones the preceding merge published
// for this batch's deletes. StarRocks has no multi-statement DML transactions
// and INSERT cannot delete, so the merge first publishes upserts and tombstones
// together; a crash before this DELETE leaves only tombstones, which the
// replayed batch removes. A tombstone matches when the batch holds a delete at
// its exact position: tombstones the merge just wrote and older ones the batch
// re-deletes, never newer ones a stale batch could not have produced.
func (d *StarRocksDestination) buildCDCHardDeleteSQL(opts destination.MergeOptions) string {
	// StarRocks DELETE takes no target alias; columns are qualified by the
	// bare table name instead.
	_, name := d.tableParts(opts.TargetTable)
	t := quoteColumn(name)
	lsn, deleted := quoteColumn(destination.CDCLSNColumn), quoteColumn(destination.CDCDeletedColumn)
	conditions := make([]string, 0, len(opts.PrimaryKeys)+3)
	for _, key := range opts.PrimaryKeys {
		conditions = append(conditions, t+"."+quoteColumn(key)+" = s."+quoteColumn(key))
	}
	conditions = append(conditions, t+"."+deleted+" = TRUE", "s."+deleted+" = TRUE", "s."+lsn+" = "+t+"."+lsn)
	return fmt.Sprintf("DELETE FROM %s USING %s s WHERE %s",
		d.tableRef(opts.TargetTable), d.tableRef(opts.StagingTable), strings.Join(conditions, " AND "))
}

// cdcMergeHint makes StarRocks materialize each CTE of the CDC merge once. The
// cost model otherwise inlines them, re-evaluating the history (and the full
// target scans inside it) for every payload column, so a merge cost about 27
// target scans and grew with table size rather than batch size.
const cdcMergeHint = "/*+ SET_VAR(cbo_cte_reuse=true, cbo_cte_reuse_rate=0) */"

func sqlString(value string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), "'", "''") + "'"
}

// The history contains only changes newer than the target plus its current
// row. Resolve each payload column independently so multiple TOAST updates in
// one flush retain the most recent materialized value, including explicit NULL.
// Publish payload and tombstone metadata together in a single INSERT SELECT.
func (d *StarRocksDestination) buildCDCMergeSQL(opts destination.MergeOptions) string {
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
	origin, rank := unique("__ingestr_target"), unique("__ingestr_rank")
	unchanged := "'[]'"
	if destination.HasCDCUnchangedColsColumn(opts.Columns) {
		unchanged = "s." + quoteColumn(destination.CDCUnchangedColsColumn)
	}
	target, stage := d.tableRef(opts.TargetTable), d.tableRef(opts.StagingTable)
	lsn, deleted := quoteColumn(destination.CDCLSNColumn), quoteColumn(destination.CDCDeletedColumn)
	ctes := []string{
		fmt.Sprintf(`changes AS (
 SELECT %s, %s AS %s FROM %s s LEFT JOIN %s t ON %s
 WHERE t.%s IS NULL OR t.%s IS NULL OR s.%s > t.%s
 OR (s.%s = t.%s AND COALESCE(t.%s, FALSE) = FALSE
     AND EXISTS (SELECT 1 FROM %s tombstone WHERE %s AND tombstone.%s = s.%s AND tombstone.%s = TRUE))
)`, prefixed("s"), unchanged, quoteColumn(destination.CDCUnchangedColsColumn), stage, target, join("s", "t"), quoteColumn(opts.PrimaryKeys[0]), lsn, lsn, lsn, lsn, lsn, deleted, stage, join("s", "tombstone"), lsn, lsn, deleted),
		fmt.Sprintf(`history AS (
 SELECT %s, %s, 0 AS %s FROM changes
 UNION ALL
 SELECT %s, '[]', 1 FROM %s t WHERE EXISTS (SELECT 1 FROM changes s WHERE %s)
)`, strings.Join(quoteColumns(cols), ", "), quoteColumn(destination.CDCUnchangedColsColumn), origin, prefixed("t"), target, join("s", "t")),
		fmt.Sprintf("latest AS (SELECT *, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC, %s DESC, %s DESC) AS %s FROM history)", keys, lsn, deleted, origin, rank),
	}
	selected, joins := make([]string, len(cols)), []string{}
	for i, col := range cols {
		selected[i] = "l." + quoteColumn(col)
		if keySet[strings.ToLower(col)] || destination.IsCDCColumn(col) {
			continue
		}
		materialized := fmt.Sprintf("%s = 1 OR (COALESCE(%s, FALSE) = FALSE AND NOT COALESCE(array_contains(CAST(parse_json(%s) AS ARRAY<VARCHAR>), %s), FALSE))", origin, deleted, quoteColumn(destination.CDCUnchangedColsColumn), sqlString(col))
		name := fmt.Sprintf("value_%d", i)
		ctes = append(ctes, fmt.Sprintf("%s AS (SELECT %s, %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC, %s ASC) AS %s FROM history WHERE %s)", name, keys, quoteColumn(col), keys, lsn, origin, rank, materialized))
		joins = append(joins, fmt.Sprintf("LEFT JOIN %s ON %s AND %s.%s = 1", name, join("l", name), name, rank))
		selected[i] = name + "." + quoteColumn(col)
	}
	return fmt.Sprintf("(%s) WITH %s SELECT %s FROM latest l %s WHERE l.%s = 1", strings.Join(quoteColumns(cols), ", "), strings.Join(ctes, ",\n"), strings.Join(selected, ", "), strings.Join(joins, "\n"), rank)
}
