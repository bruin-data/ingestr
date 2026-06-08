package destination

import (
	"slices"

	"github.com/bruin-data/ingestr/pkg/schema"
)

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

// IsCDCStagingOnlyColumn reports columns used during CDC merge but not persisted on the destination.
func IsCDCStagingOnlyColumn(col string) bool {
	return col == CDCUnchangedColsColumn
}

// DestinationColumns returns columns that should exist on the destination table.
func DestinationColumns(columns []string) []string {
	if len(columns) == 0 {
		return columns
	}
	out := make([]string, 0, len(columns))
	for _, col := range columns {
		if !IsCDCStagingOnlyColumn(col) {
			out = append(out, col)
		}
	}
	return out
}

// DestinationTableSchema returns a copy of the schema without staging-only CDC columns.
func DestinationTableSchema(s *schema.TableSchema) *schema.TableSchema {
	if s == nil {
		return nil
	}
	filtered := make([]schema.Column, 0, len(s.Columns))
	for _, col := range s.Columns {
		if !IsCDCStagingOnlyColumn(col.Name) {
			filtered = append(filtered, col)
		}
	}
	if len(filtered) == len(s.Columns) {
		return s
	}
	result := *s
	result.Columns = filtered
	return &result
}
