package adbc

import (
	"context"
	"database/sql"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

// Dialect encapsulates database-specific behavior for ADBC-based sources.
// Each supported database implements this interface to provide its own
// SQL templates, type mappings, and driver management.
type Dialect interface {
	// Name returns the dialect name (e.g., "duckdb", "snowflake")
	Name() string

	// Schemes returns the URI schemes this dialect handles (e.g., []string{"duckdb"})
	Schemes() []string

	// DriverName returns the ADBC driver name used with dbc tool (e.g., "duckdb", "snowflake")
	DriverName() string

	// EnsureDriver ensures the ADBC driver is installed and available.
	// Uses the native dbc client to install missing drivers.
	EnsureDriver(ctx context.Context) error

	// BuildConnectionString converts a URI to an ADBC connection string.
	// Input: "duckdb:///path/to/db.db" or "snowflake://user:pass@account/db"
	// Output: "driver=duckdb;path=/path/to/db.db" or similar
	BuildConnectionString(uri string) (string, error)

	// DefaultSchema returns the default schema name when not specified.
	// PostgreSQL: "public", DuckDB: "main", Snowflake: "PUBLIC"
	DefaultSchema() string

	// ParseTableName parses "schema.table" into (schemaName, tableName).
	// Returns default schema if schema is not specified.
	ParseTableName(table string) (schemaName, tableName string)

	// SchemaQuery returns the SQL query for fetching table column information.
	// The query should accept two parameters: schemaName, tableName
	// and return columns: column_name, data_type, is_nullable
	SchemaQuery() string

	// PrimaryKeyQuery returns the SQL query for fetching primary key columns.
	// The query should accept parameters based on the database's needs.
	// Returns empty string if PK detection is not supported.
	PrimaryKeyQuery() string

	// MapDataType maps a database-specific type string to the internal schema.DataType.
	// Returns: dataType, precision, scale, arrayElementType
	MapDataType(dbType string) (dataType schema.DataType, precision int, scale int, arrayType schema.DataType)

	// QuoteIdentifier returns a properly quoted identifier for this database.
	// PostgreSQL/DuckDB: "name", MySQL: `name`, SQL Server: [name]
	QuoteIdentifier(name string) string

	// ParsePrimaryKeyResult processes the raw result from the primary key query.
	// Different databases return PKs in different formats (comma-separated, array, etc.)
	ParsePrimaryKeyResult(rawValue interface{}) []string
}

// DatasetAwareDialect is implemented by databases like BigQuery where the
// dataset (schema) must be embedded in INFORMATION_SCHEMA query paths rather
// than passed as parameters.
type DatasetAwareDialect interface {
	Dialect

	// ValidateTableName validates that the table name includes required parts.
	// For BigQuery, this ensures dataset.table format is used.
	ValidateTableName(table string) error

	// SchemaQueryForDataset returns the schema query with dataset embedded in the path.
	SchemaQueryForDataset(dataset string) string

	// PrimaryKeyQueryForDataset returns the PK query with dataset embedded in the path.
	PrimaryKeyQueryForDataset(dataset string) string
}

// DatasetConnector is implemented by dialects like BigQuery that require
// the dataset_id to be set in the connection string for queries to work.
type DatasetConnector interface {
	// BuildConnectionStringWithDataset returns a connection string with the dataset included.
	// This is needed for databases that require dataset_id at connection level.
	BuildConnectionStringWithDataset(uri string, dataset string) (string, error)
}

// SchemaProvider is an optional interface for dialects that can fetch schema
// directly using native APIs instead of SQL queries. This is typically much faster
// than querying INFORMATION_SCHEMA.
type SchemaProvider interface {
	// GetSchema retrieves the schema for a table using native APIs.
	// Returns the table schema with columns and primary keys.
	GetSchema(ctx context.Context, table string) (*schema.TableSchema, error)
}

// StorageReader is an optional interface for dialects that support native
// storage APIs for faster data reading (e.g., BigQuery Storage Read API).
// Implementations should return errors for unsupported operations, which
// will trigger automatic fallback to SQL-based reading.
type StorageReader interface {
	// ReadWithStorageAPI reads data using native storage API instead of SQL.
	// Returns Arrow records via channel, similar to standard Read() method.
	// If this returns an error, the caller should fall back to SQL queries.
	ReadWithStorageAPI(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error)
}

// ConnectionAware is an optional interface for dialects that need access to the
// database connection for optimized schema fetching or other operations.
// Dialects implementing this interface will receive the connection after Connect().
type ConnectionAware interface {
	// SetConnection provides the dialect with access to the database connection.
	// This is called after successful connection in ADBCSource.Connect().
	SetConnection(db *sql.DB)
}

// URIAware is an optional interface for dialects that need access to the
// original connection URI for native ADBC operations.
type URIAware interface {
	// SetURI provides the dialect with the original connection URI.
	// This is called after successful connection in ADBCSource.Connect().
	SetURI(uri string)
}
