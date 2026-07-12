package destination

import (
	"math/big"
	"strings"
)

func HasCDCDeletedColumn(columns []string) bool {
	for _, col := range columns {
		if strings.EqualFold(col, CDCDeletedColumn) {
			return true
		}
	}
	return false
}

func HasCDCUnchangedColsColumn(columns []string) bool {
	for _, col := range columns {
		if strings.EqualFold(col, CDCUnchangedColsColumn) {
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
	if comparison := CompareCDCPositions(lsn, prevLSN); comparison != 0 {
		return comparison > 0
	}
	return deleted && !prevDeleted
}

// CompareCDCPositions compares decimal cursors, PostgreSQL X/Y hexadecimal
// LSNs, and finally opaque strings.
func CompareCDCPositions(left, right string) int {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return 0
	}
	if left == "" {
		return -1
	}
	if right == "" {
		return 1
	}
	if leftLSN, ok := postgresLSNValue(left); ok {
		if rightLSN, rightOK := postgresLSNValue(right); rightOK {
			return leftLSN.Cmp(rightLSN)
		}
	}
	if leftInt, ok := new(big.Int).SetString(left, 10); ok {
		if rightInt, rightOK := new(big.Int).SetString(right, 10); rightOK {
			return leftInt.Cmp(rightInt)
		}
	}
	return strings.Compare(left, right)
}

func postgresLSNValue(value string) (*big.Int, bool) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return nil, false
	}
	high, ok := new(big.Int).SetString(parts[0], 16)
	if !ok {
		return nil, false
	}
	low, ok := new(big.Int).SetString(parts[1], 16)
	if !ok || low.BitLen() > 32 {
		return nil, false
	}
	return new(big.Int).Add(new(big.Int).Lsh(high, 32), low), true
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
