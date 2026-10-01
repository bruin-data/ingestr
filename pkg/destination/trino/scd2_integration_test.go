//go:build integration

package trino

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestSCD2Iceberg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Trino integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	testNetwork, err := network.New(ctx)
	require.NoError(t, err)
	defer func() { _ = testNetwork.Remove(context.Background()) }()
	nessie, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "ghcr.io/projectnessie/nessie:0.104.5", ExposedPorts: []string{"19120/tcp"},
			Networks: []string{testNetwork.Name}, NetworkAliases: map[string][]string{testNetwork.Name: {"nessie"}},
			Env:        map[string]string{"NESSIE_VERSION_STORE_TYPE": "IN_MEMORY"},
			WaitingFor: wait.ForHTTP("/api/v2/config").WithPort("19120/tcp").WithStartupTimeout(2 * time.Minute),
		}, Started: true,
	})
	require.NoError(t, err)
	defer func() { _ = nessie.Terminate(context.Background()) }()
	trinoContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "trinodb/trino:483", ExposedPorts: []string{"8080/tcp"}, Networks: []string{testNetwork.Name},
			Files: []testcontainers.ContainerFile{{
				Reader: strings.NewReader(`connector.name=iceberg
iceberg.catalog.type=nessie
iceberg.nessie-catalog.uri=http://nessie:19120/api/v2
iceberg.nessie-catalog.default-warehouse-dir=local:///warehouse
fs.local.enabled=true
local.location=/tmp
`),
				ContainerFilePath: "/etc/trino/catalog/iceberg.properties", FileMode: 0o644,
			}},
			WaitingFor: wait.ForLog("======== SERVER STARTED ========").WithStartupTimeout(2 * time.Minute),
		}, Started: true,
	})
	require.NoError(t, err)
	defer func() { _ = trinoContainer.Terminate(context.Background()) }()
	host, err := trinoContainer.Host(ctx)
	require.NoError(t, err)
	port, err := trinoContainer.MappedPort(ctx, "8080/tcp")
	require.NoError(t, err)
	dest := NewTrinoDestination()
	require.NoError(t, dest.Connect(ctx, fmt.Sprintf("trino://test@%s:%s/iceberg/scd2", host, port.Port())))
	defer func() { _ = dest.Close(context.Background()) }()
	require.NoError(t, dest.Exec(ctx, "CREATE SCHEMA iceberg.scd2"))

	for _, incremental := range []bool{false, true} {
		name := "snapshot"
		if incremental {
			name = "incremental"
		}
		t.Run(name, func(t *testing.T) {
			target, stage := name+"_target", name+"_stage"
			for _, table := range []string{target, stage} {
				require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE iceberg.scd2.%s (
tenant BIGINT, id BIGINT, value VARCHAR, _scd_valid_from TIMESTAMP(6),
_scd_valid_to TIMESTAMP(6), _scd_is_current BOOLEAN)`, table)))
			}
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO iceberg.scd2.%s VALUES
(1, 1, 'old', TIMESTAMP '2026-01-01', NULL, true),
(1, 2, 'gone', TIMESTAMP '2026-01-01', NULL, true),
(1, 3, NULL, TIMESTAMP '2026-01-01', NULL, true),
(2, 1, 'keep', TIMESTAMP '2026-01-01', NULL, true)`, target)))
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO iceberg.scd2.%s VALUES
(1, 1, 'new', TIMESTAMP '2026-02-03 04:05:06.123456', NULL, true),
(1, 3, 'set', TIMESTAMP '2026-02-03 04:05:06.123456', NULL, true),
(1, 4, 'fresh', TIMESTAMP '2026-02-03 04:05:06.123456', NULL, true),
(2, 1, 'keep', TIMESTAMP '2026-02-03 04:05:06.123456', NULL, true)`, stage)))
			opts := destination.SCD2Options{
				TargetTable: target, StagingTable: stage, Columns: []string{"tenant", "id", "value"},
				PrimaryKeys: []string{"tenant", "id"}, Timestamp: time.Date(2026, 2, 4, 4, 5, 6, 123456000, time.UTC),
			}
			if incremental {
				opts.IncrementalKey = "id"
			}
			require.NoError(t, dest.SCD2Table(ctx, opts))
			rows, err := dest.db.QueryContext(ctx, fmt.Sprintf(`SELECT tenant, id, coalesce(value, '<NULL>'),
_scd_is_current, coalesce(CAST(_scd_valid_to AS VARCHAR), '') FROM iceberg.scd2.%s
ORDER BY tenant, id, _scd_is_current`, target))
			require.NoError(t, err)
			defer func() { _ = rows.Close() }()
			var got []string
			for rows.Next() {
				var tenant, id int64
				var current bool
				var value, validTo string
				require.NoError(t, rows.Scan(&tenant, &id, &value, &current, &validTo))
				got = append(got, fmt.Sprintf("%d/%d %s %t %s", tenant, id, value, current, validTo))
			}
			require.NoError(t, rows.Err())
			missing := "1/2 gone false 2026-02-04 04:05:06.123456"
			if incremental {
				missing = "1/2 gone true "
			}
			require.Equal(t, []string{
				"1/1 old false 2026-02-03 04:05:06.123456", "1/1 new true ", missing,
				"1/3 <NULL> false 2026-02-03 04:05:06.123456", "1/3 set true ",
				"1/4 fresh true ", "2/1 keep true ",
			}, got)
		})
	}
}
