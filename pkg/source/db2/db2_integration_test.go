//go:build integration

package db2

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	db2IntegrationImage    = "icr.io/db2_community/db2"
	db2IntegrationUser     = "db2inst1"
	db2IntegrationPassword = "password"
	db2IntegrationDatabase = "testdb"
	db2IntegrationTable    = "INGESTR_TEST"
)

func TestDb2SourceWithIBMContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if os.Getenv("INGESTR_TEST_DB2") != "1" {
		t.Skip("Skipping Db2 integration test; set INGESTR_TEST_DB2=1 to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()

	container, uri := startDb2IntegrationContainer(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	seedDb2IntegrationData(t, ctx, container)

	src := NewDb2Source()
	require.NoError(t, src.Connect(ctx, uri))
	t.Cleanup(func() { _ = src.Close(context.Background()) })

	table, err := src.GetTable(ctx, source.TableRequest{Name: db2IntegrationTable})
	require.NoError(t, err)

	tableSchema, err := table.GetSchema(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"ID"}, tableSchema.PrimaryKeys)
	require.Equal(t, []string{"ID", "NAME", "AMOUNT", "CREATED_AT"}, tableSchema.ColumnNames())

	results, err := table.Read(ctx, source.ReadOptions{PageSize: 1})
	require.NoError(t, err)

	var records []arrow.RecordBatch
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			result.Batch.Retain()
			records = append(records, result.Batch)
		}
	}
	t.Cleanup(func() {
		for _, record := range records {
			record.Release()
		}
	})

	require.Len(t, records, 2)
	require.EqualValues(t, 1, records[0].NumRows())
	require.EqualValues(t, 1, records[1].NumRows())

	ids := records[0].Column(0).(*array.Int32)
	names := records[0].Column(1).(*array.String)
	amounts := records[0].Column(2).(*array.Decimal128)
	createdAt := records[0].Column(3).(*array.Timestamp)

	require.EqualValues(t, 1, ids.Value(0))
	require.Equal(t, "alpha", names.Value(0))
	require.Equal(t, "12.34", amounts.ValueStr(0))
	require.EqualValues(t, time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC).UnixMicro(), createdAt.Value(0))

	ids = records[1].Column(0).(*array.Int32)
	names = records[1].Column(1).(*array.String)
	amounts = records[1].Column(2).(*array.Decimal128)
	createdAt = records[1].Column(3).(*array.Timestamp)

	require.EqualValues(t, 2, ids.Value(0))
	require.Equal(t, "beta", names.Value(0))
	require.True(t, amounts.IsNull(0))
	require.EqualValues(t, time.Date(2024, 1, 3, 4, 5, 6, 1000, time.UTC).UnixMicro(), createdAt.Value(0))
}

func startDb2IntegrationContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()

	container, err := testcontainers.Run(
		ctx,
		db2IntegrationImage,
		testcontainers.WithImagePlatform("linux/amd64"),
		testcontainers.WithEnv(map[string]string{
			"LICENSE":              "accept",
			"DB2INST1_PASSWORD":    db2IntegrationPassword,
			"DBNAME":               db2IntegrationDatabase,
			"ARCHIVE_LOGS":         "false",
			"TEXT_SEARCH":          "false",
			"ENABLE_ORACLE_COMPAT": "false",
		}),
		testcontainers.WithExposedPorts("50000/tcp"),
		testcontainers.WithHostConfigModifier(func(hostConfig *dockercontainer.HostConfig) {
			hostConfig.Privileged = true
		}),
		testcontainers.WithWaitStrategyAndDeadline(
			8*time.Minute,
			wait.ForLog("(*) Setup has completed.").WithStartupTimeout(8*time.Minute),
		),
	)
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "50000/tcp")
	require.NoError(t, err)

	u := &url.URL{
		Scheme: "db2",
		User:   url.UserPassword(db2IntegrationUser, db2IntegrationPassword),
		Host:   net.JoinHostPort(host, port.Port()),
		Path:   "/" + db2IntegrationDatabase,
	}
	return container, u.String()
}

func seedDb2IntegrationData(t *testing.T, ctx context.Context, container testcontainers.Container) {
	t.Helper()

	seedSQL := fmt.Sprintf(`CREATE TABLE %s (
	ID INTEGER NOT NULL PRIMARY KEY,
	NAME VARCHAR(50),
	AMOUNT DECIMAL(10,2),
	CREATED_AT TIMESTAMP
);
INSERT INTO %s (ID, NAME, AMOUNT, CREATED_AT) VALUES
	(1, 'alpha', 12.34, '2024-01-02-03.04.05.123456'),
	(2, 'beta', NULL, '2024-01-03-04.05.06.000001');
`, db2IntegrationTable, db2IntegrationTable)

	require.NoError(t, container.CopyToContainer(ctx, []byte(seedSQL), "/tmp/ingestr_db2_seed.sql", 0o644))

	exitCode, output, err := container.Exec(ctx, []string{
		"su", "-", db2IntegrationUser, "-c",
		fmt.Sprintf("db2 connect to %s >/dev/null && db2 -tvf /tmp/ingestr_db2_seed.sql", db2IntegrationDatabase),
	})
	require.NoError(t, err)
	rawOutput, _ := io.ReadAll(output)
	require.Equalf(t, 0, exitCode, "Db2 seed command failed: %s", string(rawOutput))
}
