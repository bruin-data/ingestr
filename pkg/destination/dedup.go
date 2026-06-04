package destination

import "fmt"

// DedupStagingSelect builds a SELECT over the staging table that keeps a single
// row per primary key. When quotedOrderByCol is non-empty, ROW_NUMBER orders by
// it DESC so the latest row per key wins (e.g. the incremental key); otherwise
// it falls back to a no-op order for engines that require an ORDER BY inside
// ROW_NUMBER. quotedColumns and quotedPrimaryKeys are already dialect-quoted,
// comma-joined lists, and stagingExpr is the quoted staging table reference.
// When there are no primary keys it returns a plain SELECT (no dedup).
func DedupStagingSelect(quotedColumns, quotedPrimaryKeys, stagingExpr, quotedOrderByCol string) string {
	if quotedPrimaryKeys == "" {
		return fmt.Sprintf("SELECT %s FROM %s", quotedColumns, stagingExpr)
	}

	orderBy := "(SELECT NULL)"
	if quotedOrderByCol != "" {
		orderBy = quotedOrderByCol + " DESC"
	}

	return fmt.Sprintf(
		"SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS __bruin_dedup_rn FROM %s) AS _numbered WHERE __bruin_dedup_rn = 1",
		quotedColumns, quotedColumns, quotedPrimaryKeys, orderBy, stagingExpr,
	)
}
