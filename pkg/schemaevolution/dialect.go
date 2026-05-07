package schemaevolution

import (
	"github.com/bruin-data/ingestr/pkg/schema"
)

// Dialect defines the interface for database-specific SQL generation.
type Dialect interface {
	// Name returns the name of the dialect.
	Name() string

	// AddColumnSQL generates SQL for adding a new column.
	AddColumnSQL(table string, col schema.Column) string

	// AlterColumnTypeSQL generates SQL for changing a column's type.
	// Returns empty string if the database doesn't support ALTER COLUMN TYPE.
	AlterColumnTypeSQL(table, colName string, newType schema.Column) string

	// SupportsAlterType returns true if the database supports ALTER COLUMN TYPE.
	SupportsAlterType() bool

	// TypeName returns the database-specific type name for a column.
	TypeName(col schema.Column) string

	// QuoteIdentifier quotes an identifier (table/column name) for the database.
	QuoteIdentifier(name string) string
}

// BatchColumnAdder is an optional interface for dialects that support adding
// multiple columns in a single ALTER TABLE statement. This avoids the overhead
// of issuing one query job per column.
type BatchColumnAdder interface {
	BatchAddColumnsSQL(table string, cols []schema.Column) string
}

// dialectRegistry stores the registered dialects.
var dialectRegistry = make(map[string]Dialect)

// RegisterDialect registers a dialect for a URI scheme.
func RegisterDialect(scheme string, dialect Dialect) {
	dialectRegistry[scheme] = dialect
}

// GetDialect returns the dialect for a URI scheme.
// Returns nil if no dialect is registered.
func GetDialect(scheme string) Dialect {
	return dialectRegistry[scheme]
}

// GetAllDialects returns all registered dialects.
func GetAllDialects() map[string]Dialect {
	result := make(map[string]Dialect, len(dialectRegistry))
	for k, v := range dialectRegistry {
		result[k] = v
	}
	return result
}
