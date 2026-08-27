//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	databaseadmin "cloud.google.com/go/spanner/admin/database/apiv1"
	databasepb "cloud.google.com/go/spanner/admin/database/apiv1/databasepb"
	instanceadmin "cloud.google.com/go/spanner/admin/instance/apiv1"
	instancepb "cloud.google.com/go/spanner/admin/instance/apiv1/instancepb"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMSSQLChangeTrackingMaxBatchBytesLive(t *testing.T) {
	ctx := context.Background()
	dbName, db := setupMSSQLCTDatabase(t, ctx)
	createMSSQLCTItemsTable(t, ctx, db)
	sourceURI := mssqlURIForDatabase(t, mssqlDest.uri, "mssql+ct", dbName, nil)
	assertRemainingLiveBatchRows(t, sourceURI, "dbo.items", "", 3)

	var version int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT CHANGE_TRACKING_CURRENT_VERSION()`).Scan(&version))
	_, err := db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES
		(4, N'item4', 400), (5, N'item5', 500), (6, N'item6', 600),
		(7, N'item7', 700), (8, N'item8', 800)`)
	require.NoError(t, err)
	assertRemainingLiveBatchRows(t, sourceURI, "dbo.items", fmt.Sprintf("%020d", version), 5)
}

func TestSpannerMaxBatchBytesLive(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "gcr.io/cloud-spanner-emulator/emulator:latest",
			ExposedPorts: []string{"9010/tcp", "9020/tcp"},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("9010/tcp"),
				wait.ForLog("gRPC server listening at 0.0.0.0:9010"),
			).WithStartupTimeoutDefault(120 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	host, err := container.Host(ctx)
	require.NoError(t, err)
	grpcPort, err := container.MappedPort(ctx, "9010")
	require.NoError(t, err)
	t.Setenv("SPANNER_EMULATOR_HOST", fmt.Sprintf("%s:%s", host, grpcPort.Port()))

	instanceClient, err := instanceadmin.NewInstanceAdminClient(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = instanceClient.Close() })
	instancePath := "projects/test-project/instances/test-instance"
	instanceOp, err := instanceClient.CreateInstance(ctx, &instancepb.CreateInstanceRequest{
		Parent:     "projects/test-project",
		InstanceId: "test-instance",
		Instance: &instancepb.Instance{
			Name: instancePath, Config: "projects/test-project/instanceConfigs/emulator-config",
			DisplayName: "test-instance", NodeCount: 1,
		},
	})
	require.NoError(t, err)
	_, err = instanceOp.Wait(ctx)
	require.NoError(t, err)

	databaseClient, err := databaseadmin.NewDatabaseAdminClient(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = databaseClient.Close() })
	databaseOp, err := databaseClient.CreateDatabase(ctx, &databasepb.CreateDatabaseRequest{
		Parent:          instancePath,
		CreateStatement: "CREATE DATABASE `test-db`",
		ExtraStatements: []string{"CREATE TABLE Probe (Id INT64 NOT NULL, Payload STRING(256)) PRIMARY KEY (Id)"},
	})
	require.NoError(t, err)
	database, err := databaseOp.Wait(ctx)
	require.NoError(t, err)

	dataClient, err := spanner.NewClient(ctx, database.Name)
	require.NoError(t, err)
	t.Cleanup(dataClient.Close)
	mutations := make([]*spanner.Mutation, 5)
	for i := range mutations {
		mutations[i] = spanner.Insert("Probe", []string{"Id", "Payload"}, []interface{}{int64(i + 1), strings.Repeat("x", 256)})
	}
	_, err = dataClient.Apply(ctx, mutations)
	require.NoError(t, err)

	assertRemainingLiveBatchRows(t,
		"spanner://?project_id=test-project&instance_id=test-instance&database=test-db", "Probe", "", 5)
}

func assertRemainingLiveBatchRows(t *testing.T, sourceURI, tableName, resumeLSN string, expectedRows int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	src, err := uri.DefaultRegistry.GetSource(sourceURI)
	require.NoError(t, err)
	require.NoError(t, src.Connect(ctx, sourceURI))
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	table, err := src.GetTable(ctx, source.TableRequest{Name: tableName})
	require.NoError(t, err)
	records, err := table.Read(ctx, source.ReadOptions{PageSize: 1000, MaxBatchBytes: 1, CDCResumeLSN: resumeLSN})
	require.NoError(t, err)
	rows := make([]int64, 0, expectedRows)
	for result := range records {
		require.NoError(t, result.Err)
		if result.Batch == nil {
			continue
		}
		rows = append(rows, result.Batch.NumRows())
		result.Batch.Release()
	}
	require.Len(t, rows, expectedRows)
	for _, count := range rows {
		require.Equal(t, int64(1), count)
	}
}
