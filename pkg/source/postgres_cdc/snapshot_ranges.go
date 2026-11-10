package postgres_cdc

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type snapshotRange struct {
	ID       string
	After    []string
	Upper    []string
	LastKey  []string
	Complete bool
}

// boundedSnapshotQuery compares (after, upper] in PostgreSQL's native key
// ordering. Only the extra progress columns are serialized to text.
func boundedSnapshotQuery(columns []string, table string, keys, keyTypes, after, upper []string) (string, []any) {
	if len(keys) == 0 {
		return fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), table), nil
	}
	quotedKeys, textKeys := snapshotRangeKeyColumns(keys)
	selected := append(slices.Clone(columns), textKeys...)
	query := fmt.Sprintf("SELECT %s FROM %s AS __ingestr_snapshot", strings.Join(selected, ", "), table)
	where, args := snapshotRangePredicate(quotedKeys, keyTypes, after, upper)
	return query + where + " ORDER BY " + strings.Join(quotedKeys, ", "), args
}

func snapshotRangeKeyColumns(keys []string) ([]string, []string) {
	qualified, text := make([]string, len(keys)), make([]string, len(keys))
	for i, key := range keys {
		qualified[i] = "__ingestr_snapshot." + quoteIdentifier(key)
		text[i] = fmt.Sprintf("%s::text AS %s", qualified[i], quoteIdentifier(fmt.Sprintf("__ingestr_key_%d", i)))
	}
	return qualified, text
}

func snapshotRangePredicate(keys, keyTypes, after, upper []string) (string, []any) {
	var predicates []string
	var args []any
	for _, bound := range []struct {
		values []string
		op     string
	}{{after, ">"}, {upper, "<="}} {
		if bound.values == nil {
			continue
		}
		params := make([]string, len(keys))
		for i := range keys {
			params[i] = fmt.Sprintf("$%d::text::%s", len(args)+1, keyTypes[i])
			args = append(args, bound.values[i])
		}
		predicates = append(predicates, fmt.Sprintf("(%s) %s (%s)", strings.Join(keys, ", "), bound.op, strings.Join(params, ", ")))
	}
	if len(predicates) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(predicates, " AND "), args
}

// snapshotRangeKeysSupported checks unique ordering in PostgreSQL catalogs.
// Range planning repeats this check inside the imported snapshot.
func snapshotRangeKeysSupported(ctx context.Context, query snapshotResumeSlotQuerier, table string, keys []string) (bool, error) {
	if len(keys) == 0 {
		return false, nil
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if seen[key] {
			return false, nil
		}
		seen[key] = true
	}
	var supported bool
	err := query.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_class idx ON idx.oid = i.indexrelid
		JOIN pg_am am ON am.oid = idx.relam
		WHERE i.indrelid = $1::regclass AND c.relkind = 'r'
		  AND NOT EXISTS (SELECT 1 FROM pg_inherits inh WHERE inh.inhparent = c.oid)
		  AND i.indisunique AND i.indisvalid AND i.indisready
		  AND i.indpred IS NULL AND i.indexprs IS NULL AND am.amname = 'btree'
		  AND (SELECT array_agg(a.attname::text ORDER BY k.ord)
		       FROM unnest(i.indkey) WITH ORDINALITY k(attnum, ord)
		       JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
		       WHERE k.ord <= COALESCE((to_jsonb(i)->>'indnkeyatts')::int, i.indnatts)) = $2::text[]
		  AND NOT EXISTS (
			SELECT 1 FROM unnest(i.indkey) WITH ORDINALITY k(attnum, ord)
			JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
			JOIN pg_opclass op ON op.oid = i.indclass[k.ord::int - 1]
			WHERE k.ord <= COALESCE((to_jsonb(i)->>'indnkeyatts')::int, i.indnatts)
			  AND (NOT a.attnotnull OR NOT op.opcdefault
			       OR i.indcollation[k.ord::int - 1] != a.attcollation
			       OR (i.indoption[k.ord::int - 1] & 1) != 0)
		  )
	)`, quoteTableName(table), keys).Scan(&supported)
	if err != nil {
		return false, fmt.Errorf("failed to inspect snapshot range keys for %s: %w", table, err)
	}
	return supported, nil
}

func planSnapshotRanges(ctx context.Context, tx pgx.Tx, table string, keys, keyTypes, after, upper []string, parallelism int) ([]snapshotRange, error) {
	if len(keys) == 0 {
		if after != nil || upper != nil {
			return nil, fmt.Errorf("snapshot range bounds require non-null unique keys")
		}
		return snapshotRangesFromSamples(nil, nil, nil, 1), nil
	}
	if len(keyTypes) != len(keys) {
		return nil, fmt.Errorf("snapshot range types must contain one type per key column")
	}
	if err := validateSnapshotChunkBounds(keys, after, upper); err != nil {
		return nil, err
	}
	if parallelism <= 1 {
		return snapshotRangesFromSamples(nil, after, upper, 1), nil
	}

	var estimatedRows float64
	err := tx.QueryRow(ctx, `SELECT GREATEST(CASE WHEN c.reltuples > 0 AND c.relpages > 0
		THEN c.reltuples::float8 * sizes.pages / c.relpages
		ELSE sizes.pages * 256 END, 1)
		FROM pg_class c CROSS JOIN LATERAL (
			SELECT pg_relation_size(c.oid)::float8 / current_setting('block_size')::float8 AS pages
		) sizes WHERE c.oid = $1::regclass`, quoteTableName(table)).Scan(&estimatedRows)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate snapshot range sample for %s: %w", table, err)
	}
	targetSamples := min(max(parallelism, 4), 16) * 256
	samplePercent := min(100, float64(targetSamples)*100/estimatedRows)
	query, args := snapshotRangeSampleQuery(table, keys, keyTypes, after, upper, samplePercent, targetSamples*2)
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to sample snapshot ranges for %s: %w", table, err)
	}
	defer rows.Close()
	var samples [][]string
	for rows.Next() {
		key := make([]string, len(keys))
		dest := make([]any, len(keys))
		for i := range key {
			dest[i] = &key[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("failed to scan snapshot range sample for %s: %w", table, err)
		}
		samples = append(samples, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read snapshot range samples for %s: %w", table, err)
	}
	return snapshotRangesFromSamples(samples, after, upper, parallelism), nil
}

func snapshotRangeSampleQuery(table string, keys, keyTypes, after, upper []string, percent float64, limit int) (string, []any) {
	qualified, textKeys := snapshotRangeKeyColumns(keys)
	where, args := snapshotRangePredicate(qualified, keyTypes, after, upper)
	args = append(args, percent, limit)
	// The inner LIMIT bounds the input to DISTINCT/ORDER BY. Typed sample
	// columns retain their collations until the final text projection.
	query := fmt.Sprintf(`WITH __ingestr_sample AS (
		SELECT %s FROM %s AS __ingestr_snapshot TABLESAMPLE SYSTEM ($%d)%s LIMIT $%d
	), __ingestr_distinct AS (
		SELECT DISTINCT * FROM __ingestr_sample
	)
	SELECT %s FROM __ingestr_distinct AS __ingestr_snapshot ORDER BY %s`,
		strings.Join(qualified, ", "), quoteTableName(table), len(args)-1, where, len(args),
		strings.Join(textKeys, ", "), strings.Join(qualified, ", "))
	return query, args
}

// samples are distinct and ordered by PostgreSQL; text keys must never be
// sorted in Go because numeric ordering and database collations differ.
func snapshotRangesFromSamples(samples [][]string, after, upper []string, parallelism int) []snapshotRange {
	distinct := make([][]string, 0, len(samples))
	for _, sample := range samples {
		if len(distinct) == 0 || !slices.Equal(distinct[len(distinct)-1], sample) {
			distinct = append(distinct, sample)
		}
	}
	count := min(max(parallelism, 1), max(len(distinct), 1))
	ranges := make([]snapshotRange, 0, count)
	lower := after
	for i := 1; i < count; i++ {
		boundary := distinct[len(distinct)*i/count-1]
		ranges = append(ranges, snapshotRange{ID: strconv.Itoa(len(ranges)), After: slices.Clone(lower), Upper: slices.Clone(boundary)})
		lower = boundary
	}
	return append(ranges, snapshotRange{ID: strconv.Itoa(len(ranges)), After: slices.Clone(lower), Upper: slices.Clone(upper)})
}
