package destination

import (
	"fmt"
	"strings"
)

// DedupStagingSelect builds a SELECT over the staging table that keeps a single
// row per primary key. When quotedOrderByCol is non-empty, ROW_NUMBER orders by
// it DESC so the latest row per key wins (e.g. the incremental key); otherwise
// it falls back to a no-op order for engines that require an ORDER BY inside
// ROW_NUMBER. quotedColumns and quotedPrimaryKeys are already dialect-quoted,
// comma-joined lists, and stagingExpr is the quoted staging table reference.
// When there are no primary keys it returns a plain SELECT (no dedup).
func DedupStagingSelect(quotedColumns, quotedPrimaryKeys, stagingExpr, quotedOrderByCol string) string {
	return DedupStagingSelectWithRowNumberAlias(
		quotedColumns,
		quotedPrimaryKeys,
		stagingExpr,
		quotedOrderByCol,
		"__bruin_dedup_rn",
	)
}

// DedupStagingSelectWithRowNumberAlias is DedupStagingSelect with a
// caller-selected, already-quoted ROW_NUMBER alias.
func DedupStagingSelectWithRowNumberAlias(quotedColumns, quotedPrimaryKeys, stagingExpr, quotedOrderByCol, quotedRowNumberAlias string) string {
	if quotedPrimaryKeys == "" {
		return fmt.Sprintf("SELECT %s FROM %s", quotedColumns, stagingExpr)
	}

	orderBy := "(SELECT NULL)"
	if quotedOrderByCol != "" {
		orderBy = quotedOrderByCol + " DESC"
	}

	return fmt.Sprintf(
		"SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s) AS _numbered WHERE %s = 1",
		quotedColumns, quotedColumns, quotedPrimaryKeys, orderBy, quotedRowNumberAlias, stagingExpr, quotedRowNumberAlias,
	)
}

// UniqueInternalColumnName returns a case-insensitively unique helper column
// name for a staging query.
func UniqueInternalColumnName(columns []string, base string) string {
	used := make(map[string]struct{}, len(columns)+1)
	for _, column := range columns {
		used[strings.ToLower(column)] = struct{}{}
	}
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
		if _, exists := used[strings.ToLower(candidate)]; !exists {
			return candidate
		}
	}
}
