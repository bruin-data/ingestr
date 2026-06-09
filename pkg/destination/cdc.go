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
