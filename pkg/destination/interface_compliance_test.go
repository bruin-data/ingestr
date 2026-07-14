package destination_test

import (
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/athena"
	"github.com/bruin-data/ingestr/pkg/destination/bigquery"
	"github.com/bruin-data/ingestr/pkg/destination/cassandra"
	"github.com/bruin-data/ingestr/pkg/destination/clickhouse"
	"github.com/bruin-data/ingestr/pkg/destination/cratedb"
	"github.com/bruin-data/ingestr/pkg/destination/databricks"
	"github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/destination/fabric"
	"github.com/bruin-data/ingestr/pkg/destination/iceberg"
	"github.com/bruin-data/ingestr/pkg/destination/mssql"
	"github.com/bruin-data/ingestr/pkg/destination/mysql"
	"github.com/bruin-data/ingestr/pkg/destination/onelake"
	"github.com/bruin-data/ingestr/pkg/destination/oracle"
	"github.com/bruin-data/ingestr/pkg/destination/planetscale"
	"github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/destination/redshift"
	"github.com/bruin-data/ingestr/pkg/destination/snowflake"
	"github.com/bruin-data/ingestr/pkg/destination/sqlite"
	"github.com/bruin-data/ingestr/pkg/destination/synapse"
	"github.com/bruin-data/ingestr/pkg/destination/trino"
	"github.com/bruin-data/ingestr/pkg/destination/vitess"
)

// Destinations eligible for destination-managed PostgreSQL CDC state must
// support one connector-scoped read from the shared state table.
var (
	_ destination.CDCStateReader = (*bigquery.BigQueryDestination)(nil)
	_ destination.CDCStateReader = (*duckdb.DuckDBDestination)(nil)
	_ destination.CDCStateReader = (*duckdb.DuckLakeDestination)(nil)
	_ destination.CDCStateReader = (*mssql.MSSQLDestination)(nil)
	_ destination.CDCStateReader = (*mysql.MySQLDestination)(nil)
	_ destination.CDCStateReader = (*oracle.OracleDestination)(nil)
	_ destination.CDCStateReader = (*planetscale.Destination)(nil)
	_ destination.CDCStateReader = (*postgres.PostgresDestination)(nil)
	_ destination.CDCStateReader = (*redshift.RedshiftDestination)(nil)
	_ destination.CDCStateReader = (*sqlite.SQLiteDestination)(nil)
	_ destination.CDCStateReader = (*vitess.Destination)(nil)
)

var (
	_ destination.CDCStateFenceReader = (*bigquery.BigQueryDestination)(nil)
	_ destination.CDCStateFenceReader = (*duckdb.DuckDBDestination)(nil)
	_ destination.CDCStateFenceReader = (*duckdb.DuckLakeDestination)(nil)
	_ destination.CDCStateFenceReader = (*mssql.MSSQLDestination)(nil)
	_ destination.CDCStateFenceReader = (*mysql.MySQLDestination)(nil)
	_ destination.CDCStateFenceReader = (*oracle.OracleDestination)(nil)
	_ destination.CDCStateFenceReader = (*planetscale.Destination)(nil)
	_ destination.CDCStateFenceReader = (*postgres.PostgresDestination)(nil)
	_ destination.CDCStateFenceReader = (*redshift.RedshiftDestination)(nil)
	_ destination.CDCStateFenceReader = (*sqlite.SQLiteDestination)(nil)
	_ destination.CDCStateFenceReader = (*vitess.Destination)(nil)
)

var (
	_ destination.CDCStateWriter                 = (*bigquery.BigQueryDestination)(nil)
	_ destination.ManagedCDCStateValidator       = (*mysql.MySQLDestination)(nil)
	_ destination.ManagedCDCStateValidator       = (*planetscale.Destination)(nil)
	_ destination.ManagedCDCStateValidator       = (*redshift.RedshiftDestination)(nil)
	_ destination.ManagedCDCStateValidator       = (*vitess.Destination)(nil)
	_ destination.ManagedCDCStateCatalogProvider = (*bigquery.BigQueryDestination)(nil)
)

var (
	_ destination.CDCTargetIdentityProvider = (*bigquery.BigQueryDestination)(nil)
	_ destination.CDCTargetIdentityProvider = (*duckdb.DuckDBDestination)(nil)
	_ destination.CDCTargetIdentityProvider = (*duckdb.DuckLakeDestination)(nil)
	_ destination.CDCTargetIdentityProvider = (*mssql.MSSQLDestination)(nil)
	_ destination.CDCTargetIdentityProvider = (*mysql.MySQLDestination)(nil)
	_ destination.CDCTargetIdentityProvider = (*oracle.OracleDestination)(nil)
	_ destination.CDCTargetIdentityProvider = (*planetscale.Destination)(nil)
	_ destination.CDCTargetIdentityProvider = (*postgres.PostgresDestination)(nil)
	_ destination.CDCTargetIdentityProvider = (*redshift.RedshiftDestination)(nil)
	_ destination.CDCTargetIdentityProvider = (*sqlite.SQLiteDestination)(nil)
	_ destination.CDCTargetIdentityProvider = (*vitess.Destination)(nil)
)

var (
	_ destination.CDCTargetIncarnationProvider = (*bigquery.BigQueryDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*duckdb.DuckDBDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*duckdb.DuckLakeDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*mssql.MSSQLDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*mysql.MySQLDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*oracle.OracleDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*planetscale.Destination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*postgres.PostgresDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*redshift.RedshiftDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*sqlite.SQLiteDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*vitess.Destination)(nil)
)

var (
	_ destination.CDCTargetClaimer = (*bigquery.BigQueryDestination)(nil)
	_ destination.CDCTargetClaimer = (*duckdb.DuckDBDestination)(nil)
	_ destination.CDCTargetClaimer = (*duckdb.DuckLakeDestination)(nil)
	_ destination.CDCTargetClaimer = (*mssql.MSSQLDestination)(nil)
	_ destination.CDCTargetClaimer = (*mysql.MySQLDestination)(nil)
	_ destination.CDCTargetClaimer = (*oracle.OracleDestination)(nil)
	_ destination.CDCTargetClaimer = (*planetscale.Destination)(nil)
	_ destination.CDCTargetClaimer = (*postgres.PostgresDestination)(nil)
	_ destination.CDCTargetClaimer = (*redshift.RedshiftDestination)(nil)
	_ destination.CDCTargetClaimer = (*sqlite.SQLiteDestination)(nil)
	_ destination.CDCTargetClaimer = (*vitess.Destination)(nil)
)

var (
	_ destination.CDCStatePruner = (*bigquery.BigQueryDestination)(nil)
	_ destination.CDCStatePruner = (*duckdb.DuckDBDestination)(nil)
	_ destination.CDCStatePruner = (*duckdb.DuckLakeDestination)(nil)
	_ destination.CDCStatePruner = (*mssql.MSSQLDestination)(nil)
	_ destination.CDCStatePruner = (*mysql.MySQLDestination)(nil)
	_ destination.CDCStatePruner = (*oracle.OracleDestination)(nil)
	_ destination.CDCStatePruner = (*planetscale.Destination)(nil)
	_ destination.CDCStatePruner = (*postgres.PostgresDestination)(nil)
	_ destination.CDCStatePruner = (*redshift.RedshiftDestination)(nil)
	_ destination.CDCStatePruner = (*sqlite.SQLiteDestination)(nil)
	_ destination.CDCStatePruner = (*vitess.Destination)(nil)
)

var _ destination.CDCStatePruneBatchSizer = (*bigquery.BigQueryDestination)(nil)

var (
	_ destination.Destination = (*athena.AthenaDestination)(nil)
	_ destination.Destination = (*bigquery.BigQueryDestination)(nil)
	_ destination.Destination = (*cassandra.CassandraDestination)(nil)
	_ destination.Destination = (*clickhouse.ClickHouseDestination)(nil)
	_ destination.Destination = (*cratedb.CrateDBDestination)(nil)
	_ destination.Destination = (*databricks.DatabricksDestination)(nil)
	_ destination.Destination = (*duckdb.DuckDBDestination)(nil)
	_ destination.Destination = (*fabric.FabricDestination)(nil)
	_ destination.Destination = (*iceberg.Destination)(nil)
	_ destination.Destination = (*mssql.MSSQLDestination)(nil)
	_ destination.Destination = (*mysql.MySQLDestination)(nil)
	_ destination.Destination = (*onelake.OneLakeDestination)(nil)
	_ destination.Destination = (*oracle.OracleDestination)(nil)
	_ destination.Destination = (*postgres.PostgresDestination)(nil)
	_ destination.Destination = (*redshift.RedshiftDestination)(nil)
	_ destination.Destination = (*snowflake.SnowflakeDestination)(nil)
	_ destination.Destination = (*sqlite.SQLiteDestination)(nil)
	_ destination.Destination = (*synapse.SynapseDestination)(nil)
	_ destination.Destination = (*trino.TrinoDestination)(nil)
)

// Optional strategy interfaces the Iceberg destination implements natively.
var (
	_ destination.TruncateCapable       = (*iceberg.Destination)(nil)
	_ destination.CDCMergeAware         = (*iceberg.Destination)(nil)
	_ destination.CDCUnchangedColsAware = (*iceberg.Destination)(nil)
)
