package integration

import (
	"context"
	"flag"
	"os"
	"sync"
	"testing"

	"github.com/bruin-data/gong/internal/testutil"
	"github.com/testcontainers/testcontainers-go"
)

// Shared Postgres containers for integration tests.
// Starting containers once avoids slow per-test startup.

type postgresEnv struct {
	container testcontainers.Container
	uri       string
}

type clickhouseEnv struct {
	container testcontainers.Container
	uri       string
}

type mysqlEnv struct {
	container testcontainers.Container
	uri       string
}

type mssqlEnv struct {
	container testcontainers.Container
	uri       string
}

type cratedbEnv struct {
	container testcontainers.Container
	uri       string
}

var (
	pgSource       postgresEnv
	pgDest         postgresEnv
	chDest         clickhouseEnv
	mysqlDest      mysqlEnv
	mssqlDest      mssqlEnv
	cratedbDest    cratedbEnv
	minioShared    minioEnv
	dynamoDBDest   dynamoDBEnv
	rabbitmqShared rabbitmqEnv
)

func TestMain(m *testing.M) {
	flag.Parse()

	// If tests are invoked with -short, integration tests will be skipped anyway.
	// Avoid starting containers in that mode.
	if testing.Short() {
		os.Exit(m.Run())
	}

	ctx := context.Background()

	if !testutil.DockerProviderHealthy(ctx) {
		// Treat as a skip: these tests require Docker (testcontainers).
		_, _ = os.Stderr.WriteString("skipping integration tests: Docker provider is not available/healthy\n")
		os.Exit(0)
	}

	var wg sync.WaitGroup

	wg.Add(8)
	go func() {
		defer wg.Done()
		if c, uri, err := startPostgresContainerForMain(ctx, "shared-source"); err == nil {
			pgSource = postgresEnv{container: c, uri: uri}
		}
	}()
	go func() {
		defer wg.Done()
		if c, uri, err := startPostgresContainerForMain(ctx, "shared-dest"); err == nil {
			pgDest = postgresEnv{container: c, uri: uri}
		}
	}()
	go func() {
		defer wg.Done()
		if c, uri, err := startClickHouseContainerForMain(ctx, "shared-clickhouse"); err == nil {
			chDest = clickhouseEnv{container: c, uri: uri}
		}
	}()
	go func() {
		defer wg.Done()
		if c, uri, err := startMySQLContainerForMain(ctx, "shared-mysql"); err == nil {
			mysqlDest = mysqlEnv{container: c, uri: uri}
		}
	}()
	go func() {
		defer wg.Done()
		if c, uri, err := startMSSQLContainerForMain(ctx, "shared-mssql"); err == nil {
			mssqlDest = mssqlEnv{container: c, uri: uri}
		}
	}()
	go func() {
		defer wg.Done()
		if c, uri, err := startCrateDBContainerForMain(ctx, "shared-cratedb"); err == nil {
			cratedbDest = cratedbEnv{container: c, uri: uri}
		}
	}()
	go func() {
		defer wg.Done()
		if c, endpoint, uri, err := startMinioContainerForMain(ctx); err == nil {
			minioShared = minioEnv{container: c, endpoint: endpoint, uri: uri}
		}
	}()
	go func() {
		defer wg.Done()
		if c, uri, err := startDynamoDBContainerForMain(ctx); err == nil {
			dynamoDBDest = dynamoDBEnv{container: c, uri: uri}
		}
	}()
	wg.Wait()

	// Start RabbitMQ container
	rmqC, rmqURI, rmqErr := startRabbitMQContainerForMain(ctx)
	if rmqErr == nil {
		rabbitmqShared = rabbitmqEnv{container: rmqC, uri: rmqURI}
	}

	code := m.Run()

	containers := []testcontainers.Container{
		pgSource.container, pgDest.container, chDest.container,
		mysqlDest.container, mssqlDest.container, cratedbDest.container,
		minioShared.container, dynamoDBDest.container,
	}
	var twg sync.WaitGroup
	for _, c := range containers {
		if c != nil {
			twg.Add(1)
			go func(c testcontainers.Container) {
				defer twg.Done()
				_ = c.Terminate(ctx)
			}(c)
		}
	}
	twg.Wait()
	if rabbitmqShared.container != nil {
		_ = rabbitmqShared.container.Terminate(ctx)
	}

	os.Exit(code)
}

// startPostgresContainerForMain mirrors startPostgresContainer but returns errors
// instead of failing a test (TestMain has no *testing.T).
func startPostgresContainerForMain(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, uri, err := startPostgresContainerRaw(ctx, name)
	if err != nil {
		return nil, "", err
	}
	return container, uri, nil
}
