//go:build integration

package trino

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/bruin-data/ingestr/pkg/source/duckdb"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	hiveCatalogProperties = `connector.name=hive
hive.metastore.uri=thrift://hadoop-master:9083
fs.hadoop.enabled=true
hive.config.resources=/etc/trino/hdfs-site.xml
hive.metastore-cache-ttl=0s
hive.single-statement-writes=false
`
	hdfsSiteXML = `<?xml version="1.0" encoding="UTF-8"?>
<configuration>
  <property>
    <name>fs.defaultFS</name>
    <value>hdfs://hadoop-master:9000</value>
  </property>
  <property>
    <name>dfs.client.use.datanode.hostname</name>
    <value>true</value>
  </property>
  <property>
    <name>dfs.datanode.use.datanode.hostname</name>
    <value>true</value>
  </property>
  <property>
    <name>dfs.replication</name>
    <value>1</value>
  </property>
  <property>
    <name>dfs.client.read.shortcircuit</name>
    <value>false</value>
  </property>
</configuration>
`
)

type hiveTransactionTestRow struct {
	id          int64
	value       string
	partitionID int64
}

func TestDeleteInsertStrategyHiveTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Trino integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	testNetwork, err := network.New(ctx)
	require.NoError(t, err)
	defer func() { _ = testNetwork.Remove(context.Background()) }()

	hadoop, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "ghcr.io/trinodb/testing/hive3.1:129",
			Hostname:     "hadoop-master",
			Env:          map[string]string{"TZ": "UTC"},
			ExposedPorts: []string{"9083/tcp"},
			Networks:     []string{testNetwork.Name},
			NetworkAliases: map[string][]string{
				testNetwork.Name: {"hadoop-master"},
			},
			WaitingFor: wait.ForLog("success: hive-metastore entered RUNNING state").
				WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	defer func() { _ = hadoop.Terminate(context.Background()) }()

	trinoContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "trinodb/trino:483",
			ExposedPorts: []string{"8080/tcp"},
			Networks:     []string{testNetwork.Name},
			Files: []testcontainers.ContainerFile{
				{
					Reader:            strings.NewReader(hiveCatalogProperties),
					ContainerFilePath: "/etc/trino/catalog/hive.properties",
					FileMode:          0o644,
				},
				{
					Reader:            strings.NewReader(hdfsSiteXML),
					ContainerFilePath: "/etc/trino/hdfs-site.xml",
					FileMode:          0o644,
				},
			},
			WaitingFor: wait.ForLog("======== SERVER STARTED ========").
				WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	defer func() { _ = trinoContainer.Terminate(context.Background()) }()

	host, err := trinoContainer.Host(ctx)
	require.NoError(t, err)
	port, err := trinoContainer.MappedPort(ctx, "8080/tcp")
	require.NoError(t, err)

	dest := NewTrinoDestination()
	require.NoError(t, dest.Connect(ctx, fmt.Sprintf("trino://test@%s:%s/hive/ingestr_it", host, port.Port())))
	defer func() { _ = dest.Close(context.Background()) }()
	require.True(t, dest.SupportsDeleteInsertStrategy())

	statements := []string{
		"CREATE SCHEMA hive.ingestr_it",
		"CREATE TABLE hive.ingestr_it.target (id BIGINT, value VARCHAR, partition_id BIGINT) WITH (format = 'ORC', partitioned_by = ARRAY['partition_id'])",
		"INSERT INTO hive.ingestr_it.target VALUES (1, 'keep', 1), (2, 'old', 2)",
		"CREATE TABLE hive.ingestr_it.stage_bad (id BIGINT, value ARRAY(VARCHAR), partition_id BIGINT) WITH (format = 'ORC')",
		"INSERT INTO hive.ingestr_it.stage_bad VALUES (20, ARRAY['invalid'], 2)",
	}
	for _, statement := range statements {
		require.NoError(t, dest.Exec(ctx, statement))
	}

	// A non-transactional Hive table can only delete complete partitions.
	opts := destination.DeleteInsertOptions{
		StagingTable:   "hive.ingestr_it.stage_bad",
		TargetTable:    "hive.ingestr_it.target",
		IncrementalKey: "partition_id",
		IntervalStart:  int64(2),
		IntervalEnd:    int64(2),
		Columns:        []string{"id", "value", "partition_id"},
		PrimaryKeys:    []string{"id"},
	}
	err = dest.DeleteInsertTable(ctx, opts)
	require.ErrorContains(t, err, "failed to insert records")
	require.ErrorContains(t, err, "mismatched column types")
	require.Equal(t, []hiveTransactionTestRow{
		{id: 1, value: "keep", partitionID: 1},
		{id: 2, value: "old", partitionID: 2},
	}, readHiveTransactionTestRows(t, ctx, dest))

	require.NoError(t, dest.Exec(ctx, "CREATE TABLE hive.ingestr_it.stage_good (id BIGINT, value VARCHAR, partition_id BIGINT) WITH (format = 'ORC')"))
	require.NoError(t, dest.Exec(ctx, "INSERT INTO hive.ingestr_it.stage_good VALUES (20, 'new', 2)"))
	opts.StagingTable = "hive.ingestr_it.stage_good"
	require.NoError(t, dest.DeleteInsertTable(ctx, opts))
	require.Equal(t, []hiveTransactionTestRow{
		{id: 1, value: "keep", partitionID: 1},
		{id: 20, value: "new", partitionID: 2},
	}, readHiveTransactionTestRows(t, ctx, dest))

	sourcePath := filepath.Join(t.TempDir(), "source.duckdb")
	sourceDialect := duckdb.NewDialect()
	require.NoError(t, sourceDialect.EnsureDriver(ctx))
	sourceDB, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", sourcePath))
	require.NoError(t, err)
	require.NoError(t, sourceDB.PingContext(ctx))
	_, err = sourceDB.ExecContext(ctx, "CREATE TABLE events (id BIGINT, value VARCHAR, partition_id BIGINT)")
	require.NoError(t, err)
	_, err = sourceDB.ExecContext(ctx, "INSERT INTO events VALUES (30, 'materialized', 2)")
	require.NoError(t, err)
	require.NoError(t, sourceDB.Close())

	require.NoError(t, dest.Exec(ctx, "CREATE TABLE hive.ingestr_it.materialized_target (id BIGINT, value VARCHAR, partition_id BIGINT) WITH (format = 'ORC', partitioned_by = ARRAY['partition_id'])"))
	require.NoError(t, dest.Exec(ctx, "INSERT INTO hive.ingestr_it.materialized_target VALUES (1, 'outside-interval', 1), (2, 'replace-me', 2)"))

	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("duckdb:///%s", sourcePath),
		SourceTable:         "events",
		DestURI:             fmt.Sprintf("trino://test@%s:%s/hive/ingestr_it", host, port.Port()),
		DestTable:           "ingestr_it.materialized_target",
		IncrementalStrategy: config.StrategyDeleteInsert,
		IncrementalKey:      "partition_id",
		PrimaryKeys:         []string{"id"},
		NoLoadTimestamp:     true,
		NoRunID:             true,
	}
	sourceConnector := adbc.NewADBCSource(sourceDialect)
	require.NoError(t, sourceConnector.Connect(ctx, cfg.SourceURI))
	defer func() { _ = sourceConnector.Close(context.Background()) }()
	sourceTable, err := sourceConnector.GetTable(ctx, source.TableRequest{
		Name:           cfg.SourceTable,
		Strategy:       cfg.IncrementalStrategy,
		IncrementalKey: cfg.IncrementalKey,
		PrimaryKeys:    cfg.PrimaryKeys,
	})
	require.NoError(t, err)
	sourceSchema, err := sourceTable.GetSchema(ctx)
	require.NoError(t, err)

	require.NoError(t, (&strategy.DeleteInsertStrategy{}).Execute(ctx, &strategy.IngestionJob{
		Config:       cfg,
		Table:        sourceTable,
		Destination:  dest,
		Schema:       sourceSchema,
		SourceSchema: sourceSchema,
	}))
	require.Equal(t, []hiveTransactionTestRow{
		{id: 1, value: "outside-interval", partitionID: 1},
		{id: 30, value: "materialized", partitionID: 2},
	}, readHiveTransactionTestTableRows(t, ctx, dest, "materialized_target"))
	var stagingTableCount int
	require.NoError(t, dest.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM hive.information_schema.tables
		WHERE table_schema = '_bruin_staging'
		  AND table_name LIKE 'ingestr_it__materialized_target_di_%'
	`).Scan(&stagingTableCount))
	require.Zero(t, stagingTableCount)
}

func readHiveTransactionTestRows(t *testing.T, ctx context.Context, dest *TrinoDestination) []hiveTransactionTestRow {
	return readHiveTransactionTestTableRows(t, ctx, dest, "target")
}

func readHiveTransactionTestTableRows(t *testing.T, ctx context.Context, dest *TrinoDestination, table string) []hiveTransactionTestRow {
	t.Helper()

	rows, err := dest.db.QueryContext(ctx, fmt.Sprintf("SELECT id, value, partition_id FROM hive.ingestr_it.%s ORDER BY partition_id", quoteIdentifier(table)))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var result []hiveTransactionTestRow
	for rows.Next() {
		var row hiveTransactionTestRow
		require.NoError(t, rows.Scan(&row.id, &row.value, &row.partitionID))
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}
