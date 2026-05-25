package destination

import "slices"

// SCD2 metadata column names. Strategies and destinations must agree on these.
const (
	SCD2ValidFromColumn = "_scd_valid_from"
	SCD2ValidToColumn   = "_scd_valid_to"
	SCD2IsCurrentColumn = "_scd_is_current"
)

func SCD2MetadataColumns() []string {
	return []string{SCD2ValidFromColumn, SCD2ValidToColumn, SCD2IsCurrentColumn}
}

// SCD2NonDataColumns returns the list of columns to exclude when computing
// "data columns" for SCD2 change detection (primary keys and SCD metadata
// columns).
func SCD2NonDataColumns(primaryKeys []string) []string {
	scd := SCD2MetadataColumns()
	out := make([]string, 0, len(primaryKeys)+len(scd))
	out = append(out, primaryKeys...)
	out = append(out, scd...)
	return out
}

// AppendSCD2Columns appends the SCD2 metadata column names to cols, skipping
// any that are already present.
func AppendSCD2Columns(cols []string) []string {
	scd := SCD2MetadataColumns()
	out := make([]string, len(cols), len(cols)+len(scd))
	copy(out, cols)
	for _, c := range scd {
		if !slices.Contains(cols, c) {
			out = append(out, c)
		}
	}
	return out
}
