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
