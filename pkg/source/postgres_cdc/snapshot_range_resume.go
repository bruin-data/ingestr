package postgres_cdc

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func validateSnapshotRanges(ctx context.Context, tx pgx.Tx, table string, keys, keyTypes, after, upper []string, ranges []snapshotRange) error {
	if len(keys) == 0 || len(keyTypes) != len(keys) || len(ranges) == 0 {
		return fmt.Errorf("saved snapshot ranges require non-null unique keys and a nonempty range plan")
	}
	if err := validateSnapshotChunkBounds(keys, after, upper); err != nil {
		return err
	}
	if !slices.Equal(ranges[0].After, after) || !slices.Equal(ranges[len(ranges)-1].Upper, upper) {
		return fmt.Errorf("saved snapshot ranges do not cover their outer bounds")
	}
	hasBounds := false
	for i, r := range ranges {
		if r.ID != strconv.Itoa(i) {
			return fmt.Errorf("saved snapshot range %d has invalid identity %q", i, r.ID)
		}
		for _, bound := range [][]string{r.After, r.Upper, r.LastKey} {
			if bound != nil {
				hasBounds = true
				if len(bound) != len(keys) {
					return fmt.Errorf("saved snapshot range %s has an incomplete key", r.ID)
				}
			}
		}
		if i > 0 && (r.After == nil || !slices.Equal(ranges[i-1].Upper, r.After)) {
			return fmt.Errorf("saved snapshot ranges %d and %d are not adjacent", i-1, i)
		}
	}
	if !hasBounds {
		return nil
	}
	collations, err := snapshotKeyCollations(ctx, tx, table, keys)
	if err != nil {
		return err
	}
	var args []any
	var predicates []string
	tuple := func(values []string) string {
		if values == nil {
			return ""
		}
		params := make([]string, len(keys))
		for i, value := range values {
			args = append(args, value)
			params[i] = fmt.Sprintf("$%d::text::%s", len(args), keyTypes[i])
			if collations[i] != "" {
				params[i] += " COLLATE " + collations[i]
			}
			predicates = append(predicates, "("+params[i]+") IS NOT NULL")
		}
		return "(" + strings.Join(params, ", ") + ")"
	}
	for _, r := range ranges {
		lower, last, high := tuple(r.After), tuple(r.LastKey), tuple(r.Upper)
		if lower != "" && high != "" {
			predicates = append(predicates, lower+" <= "+high)
		}
		if lower != "" && last != "" {
			predicates = append(predicates, lower+" < "+last)
		}
		if last != "" && high != "" {
			predicates = append(predicates, last+" <= "+high)
		}
	}
	var valid bool
	if err := tx.QueryRow(ctx, "SELECT "+strings.Join(predicates, " AND "), args...).Scan(&valid); err != nil {
		return fmt.Errorf("failed to validate saved snapshot range ordering for %s: %w", table, err)
	}
	if !valid {
		return fmt.Errorf("saved snapshot range keys for %s are outside their bounds or not in native key order", table)
	}
	return nil
}

func snapshotKeyCollations(ctx context.Context, tx pgx.Tx, table string, keys []string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT a.attname,
		CASE WHEN a.attcollation = 0 THEN '' ELSE format('%I.%I', n.nspname, c.collname) END
		FROM pg_attribute a
		LEFT JOIN pg_collation c ON c.oid = a.attcollation
		LEFT JOIN pg_namespace n ON n.oid = c.collnamespace
		WHERE a.attrelid = $1::regclass AND a.attname = ANY($2)
		  AND a.attnum > 0 AND NOT a.attisdropped`, quoteTableName(table), keys)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect snapshot key collations for %s: %w", table, err)
	}
	defer rows.Close()
	byName := make(map[string]string, len(keys))
	for rows.Next() {
		var name, collation string
		if err := rows.Scan(&name, &collation); err != nil {
			return nil, fmt.Errorf("failed to read snapshot key collation for %s: %w", table, err)
		}
		byName[name] = collation
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read snapshot key collations for %s: %w", table, err)
	}
	collations := make([]string, len(keys))
	for i, key := range keys {
		var ok bool
		if collations[i], ok = byName[key]; !ok {
			return nil, fmt.Errorf("snapshot key column %q of %s not found", key, table)
		}
	}
	return collations, nil
}
