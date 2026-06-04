package postgres_cdc

import (
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

const (
	CDCLSNColumn           = destination.CDCLSNColumn
	CDCDeletedColumn       = destination.CDCDeletedColumn
	CDCSyncedAtColumn      = destination.CDCSyncedAtColumn
	CDCUnchangedColsColumn = destination.CDCUnchangedColsColumn
)

func cdcMetadataColumnCount() int { return len(destination.CDCMetadataColumns()) }

func sourceColumnCount(tableSchema *schema.TableSchema) int {
	return len(tableSchema.Columns) - cdcMetadataColumnCount()
}
