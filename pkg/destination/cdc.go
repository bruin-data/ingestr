package destination

import "strings"

const (
	CDCLSNColumn      = "_cdc_lsn"
	CDCDeletedColumn  = "_cdc_deleted"
	CDCSyncedAtColumn = "_cdc_synced_at"
)

func HasCDCDeletedColumn(columns []string) bool {
	for _, col := range columns {
		if strings.EqualFold(col, CDCDeletedColumn) {
			return true
		}
	}
	return false
}

func IsCDCColumn(column string) bool {
	return strings.EqualFold(column, CDCLSNColumn) ||
		strings.EqualFold(column, CDCDeletedColumn) ||
		strings.EqualFold(column, CDCSyncedAtColumn)
}

// CDCLatestOverallOrderBy returns the ORDER BY expression that picks the
// latest change per PK overall: highest LSN first, preferring deletes on LSN
// ties (e.g. rows sharing a snapshot stamp) so a tied delete still marks the
// row deleted. LSN strings are fixed-width per source, so plain text ordering
// matches LSN order.
func CDCLatestOverallOrderBy(quote func(string) string) string {
	return quote(CDCLSNColumn) + " DESC, " + quote(CDCDeletedColumn) + " DESC"
}

// CDCSupersedes reports whether a change (lsn, deleted) replaces a previously
// seen change (prevLSN, prevDeleted) as the latest change overall for a PK.
// It mirrors CDCLatestOverallOrderBy for destinations that compose changes in
// Go instead of SQL.
func CDCSupersedes(lsn string, deleted bool, prevLSN string, prevDeleted bool) bool {
	if lsn != prevLSN {
		return lsn > prevLSN
	}
	return deleted && !prevDeleted
}

func CDCDataColumns(columns, primaryKeys []string) []string {
	pkSet := make(map[string]bool, len(primaryKeys))
	for _, pk := range primaryKeys {
		pkSet[strings.ToLower(pk)] = true
	}

	dataColumns := make([]string, 0, len(columns))
	for _, col := range columns {
		if pkSet[strings.ToLower(col)] || IsCDCColumn(col) {
			continue
		}
		dataColumns = append(dataColumns, col)
	}
	return dataColumns
}
