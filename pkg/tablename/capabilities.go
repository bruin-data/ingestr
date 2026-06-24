package tablename

import "strings"

// Per-platform capabilities. Values mirror Bruin CLI's pkg/tablename
// (bruin-data/bruin#2231) so the two tools agree on what each platform accepts.

// SchemeSupportsCatalog reports whether a destination scheme treats the leading
// component of a multi-part table name as a catalog/database/project (three-part
// naming) rather than as part of a dotted table name. It gates catalog-aware
// staging-name generation so two-level engines (Postgres, MySQL, …) keep their
// legacy behavior — important for multi-table CDC, where a destination name like
// "destschema.sourceschema.table" must be read as schema + dotted table.
func SchemeSupportsCatalog(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "snowflake", "bigquery", "databricks", "trino", "duckdb", "ducklake", "motherduck", "md", "mssql", "sqlserver", "maxcompute":
		return true
	default:
		return false
	}
}

// TwoLevel returns the capability for a platform that supports at most
// schema.table (most relational engines: Postgres, MySQL, etc.). A bare table
// name is allowed; a three-part name is rejected.
func TwoLevel(platform string) Capability {
	return Capability{
		Platform:      platform,
		MinComponents: 1,
		MaxComponents: 2,
		Labels:        [3]string{"", "schema", "table"},
		FormatDesc:    "schema.table",
	}
}

var (
	// Snowflake: database.schema.table.
	Snowflake = Capability{
		Platform: "snowflake", MinComponents: 1, MaxComponents: 3,
		Labels: [3]string{"database", "schema", "table"}, FormatDesc: "database.schema.table",
	}

	// BigQuery: project.dataset.table (dataset is required).
	BigQuery = Capability{
		Platform: "bigquery", MinComponents: 2, MaxComponents: 3,
		Labels: [3]string{"project", "dataset", "table"}, FormatDesc: "project.dataset.table",
	}

	// Databricks: catalog.schema.table (schema is required).
	Databricks = Capability{
		Platform: "databricks", MinComponents: 2, MaxComponents: 3,
		Labels: [3]string{"catalog", "schema", "table"}, FormatDesc: "catalog.schema.table",
	}

	// Trino: catalog.schema.table.
	Trino = Capability{
		Platform: "trino", MinComponents: 1, MaxComponents: 3,
		Labels: [3]string{"catalog", "schema", "table"}, FormatDesc: "catalog.schema.table",
	}

	// DuckDB / MotherDuck: catalog.schema.table (catalog = attached database).
	DuckDB = Capability{
		Platform: "duckdb", MinComponents: 1, MaxComponents: 3,
		Labels: [3]string{"catalog", "schema", "table"}, FormatDesc: "catalog.schema.table",
	}

	// MSSQL: database.schema.table, plus linked-server names with more
	// components (server.database.schema.table), hence Unbounded.
	MSSQL = Capability{
		Platform: "mssql", MinComponents: 1, MaxComponents: 3, Unbounded: true,
		Labels: [3]string{"database", "schema", "table"}, FormatDesc: "database.schema.table",
	}

	// Couchbase: bucket.scope.collection (bucket may come from the default).
	Couchbase = Capability{
		Platform: "couchbase", MinComponents: 2, MaxComponents: 3,
		Labels: [3]string{"bucket", "scope", "collection"}, FormatDesc: "bucket.scope.collection",
	}

	// MaxCompute: project.schema.table.
	MaxCompute = Capability{
		Platform: "maxcompute", MinComponents: 1, MaxComponents: 3,
		Labels: [3]string{"project", "schema", "table"}, FormatDesc: "project.schema.table",
	}
)
