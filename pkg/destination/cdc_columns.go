package destination

import "slices"

// CDC metadata column names. Postgres CDC source and destinations must agree on these.
const (
	CDCLSNColumn           = "_cdc_lsn"
	CDCDeletedColumn       = "_cdc_deleted"
	CDCSyncedAtColumn      = "_cdc_synced_at"
	CDCUnchangedColsColumn = "_cdc_unchanged_cols"
)

func CDCMetadataColumns() []string {
	return []string{CDCLSNColumn, CDCDeletedColumn, CDCSyncedAtColumn, CDCUnchangedColsColumn}
}

func IsCDCMetaColumn(col string) bool {
	return slices.Contains(CDCMetadataColumns(), col)
}

// CDCStagingOnlyColumns are CDC metadata columns that are only needed while the
// merge runs (read from the staging table) and are intentionally not persisted
// in the destination table. _cdc_unchanged_cols carries Postgres TOAST "unchanged
// column" markers used to decide which columns to preserve during an UPDATE; it
// has no meaning once a row has landed.
func CDCStagingOnlyColumns() []string {
	return []string{CDCUnchangedColsColumn}
}

func IsCDCStagingOnlyColumn(col string) bool {
	return slices.Contains(CDCStagingOnlyColumns(), col)
}

// FilterCDCStagingOnlyColumns returns columns with staging-only CDC columns removed.
// Used by destinations to build the column list written to the target table.
func FilterCDCStagingOnlyColumns(columns []string) []string {
	result := make([]string, 0, len(columns))
	for _, col := range columns {
		if !IsCDCStagingOnlyColumn(col) {
			result = append(result, col)
		}
	}
	return result
}
