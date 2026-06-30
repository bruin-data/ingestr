package destination_test

import (
	"github.com/bruin-data/ingestr/pkg/destination/athena"
	"github.com/bruin-data/ingestr/pkg/destination/bigquery"
	"github.com/bruin-data/ingestr/pkg/destination/cassandra"
	"github.com/bruin-data/ingestr/pkg/destination/clickhouse"
	"github.com/bruin-data/ingestr/pkg/destination/cratedb"
	"github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/destination/fabric"
	"github.com/bruin-data/ingestr/pkg/destination/maxcompute"
	"github.com/bruin-data/ingestr/pkg/destination/mssql"
	"github.com/bruin-data/ingestr/pkg/destination/mysql"
	"github.com/bruin-data/ingestr/pkg/destination/oracle"
	"github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/destination/redshift"
	"github.com/bruin-data/ingestr/pkg/destination/snowflake"
	"github.com/bruin-data/ingestr/pkg/destination/sqlite"
	"github.com/bruin-data/ingestr/pkg/destination/synapse"
	"github.com/bruin-data/ingestr/pkg/destination/trino"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// Every dialect-backed destination must turn the abstract evolution plan into
// DDL itself, i.e. implement schemaevolution.SchemaEvolver. These compile-time
// assertions fail the build if a destination loses that capability.
var (
	_ schemaevolution.SchemaEvolver = (*athena.AthenaDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*bigquery.BigQueryDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*cassandra.CassandraDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*clickhouse.ClickHouseDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*cratedb.CrateDBDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*duckdb.DuckDBDestination)(nil)
	// DuckLake is duckdb-backed and intentionally inherits evolution via embedding.
	_ schemaevolution.SchemaEvolver = (*duckdb.DuckLakeDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*fabric.FabricDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*maxcompute.MaxComputeDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*mssql.MSSQLDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*mysql.MySQLDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*oracle.OracleDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*postgres.PostgresDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*redshift.RedshiftDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*snowflake.SnowflakeDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*sqlite.SQLiteDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*synapse.SynapseDestination)(nil)
	_ schemaevolution.SchemaEvolver = (*trino.TrinoDestination)(nil)
)
