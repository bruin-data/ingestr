package destination_test

import (
	"github.com/bruin-data/gong/pkg/destination"
	"github.com/bruin-data/gong/pkg/destination/athena"
	"github.com/bruin-data/gong/pkg/destination/bigquery"
	"github.com/bruin-data/gong/pkg/destination/clickhouse"
	"github.com/bruin-data/gong/pkg/destination/cratedb"
	"github.com/bruin-data/gong/pkg/destination/databricks"
	"github.com/bruin-data/gong/pkg/destination/duckdb"
	"github.com/bruin-data/gong/pkg/destination/mssql"
	"github.com/bruin-data/gong/pkg/destination/mysql"
	"github.com/bruin-data/gong/pkg/destination/postgres"
	"github.com/bruin-data/gong/pkg/destination/redshift"
	"github.com/bruin-data/gong/pkg/destination/snowflake"
	"github.com/bruin-data/gong/pkg/destination/sqlite"
	"github.com/bruin-data/gong/pkg/destination/synapse"
	"github.com/bruin-data/gong/pkg/destination/trino"
)

var (
	_ destination.Destination = (*athena.AthenaDestination)(nil)
	_ destination.Destination = (*bigquery.BigQueryDestination)(nil)
	_ destination.Destination = (*clickhouse.ClickHouseDestination)(nil)
	_ destination.Destination = (*cratedb.CrateDBDestination)(nil)
	_ destination.Destination = (*databricks.DatabricksDestination)(nil)
	_ destination.Destination = (*duckdb.DuckDBDestination)(nil)
	_ destination.Destination = (*mssql.MSSQLDestination)(nil)
	_ destination.Destination = (*mysql.MySQLDestination)(nil)
	_ destination.Destination = (*postgres.PostgresDestination)(nil)
	_ destination.Destination = (*redshift.RedshiftDestination)(nil)
	_ destination.Destination = (*snowflake.SnowflakeDestination)(nil)
	_ destination.Destination = (*sqlite.SQLiteDestination)(nil)
	_ destination.Destination = (*synapse.SynapseDestination)(nil)
	_ destination.Destination = (*trino.TrinoDestination)(nil)
)
