//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/testcontainers/testcontainers-go"
	tccassandra "github.com/testcontainers/testcontainers-go/modules/cassandra"
	"github.com/testcontainers/testcontainers-go/wait"
)

const cassandraKeyspace = "ingestr_it"

type cassandraEnv struct {
	container testcontainers.Container
	uri       string
	host      string
	port      int
}

func startCassandraContainerRaw(ctx context.Context) (testcontainers.Container, string, string, int, error) {
	container, err := tccassandra.Run(
		ctx,
		"cassandra:4.1",
		testcontainers.WithEnv(map[string]string{
			"MAX_HEAP_SIZE": "512M",
			"HEAP_NEWSIZE":  "128M",
		}),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForLog("Startup complete"),
				wait.ForListeningPort("9042/tcp"),
			).WithDeadline(3*time.Minute),
		),
	)
	if err != nil {
		return nil, "", "", 0, err
	}

	connectionHost, err := container.ConnectionHost(ctx)
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, "", "", 0, err
	}
	host, portStr, err := net.SplitHostPort(connectionHost)
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, "", "", 0, fmt.Errorf("invalid Cassandra connection host %q: %w", connectionHost, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, "", "", 0, err
	}

	cluster := gocql.NewCluster(host)
	cluster.Port = port
	cluster.Consistency = gocql.One
	cluster.DisableInitialHostLookup = true
	session, err := cluster.CreateSession()
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, "", "", 0, err
	}
	defer session.Close()

	createKeyspace := fmt.Sprintf(
		"CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}",
		cassandraKeyspace,
	)
	if err := session.Query(createKeyspace).ExecContext(ctx); err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, "", "", 0, err
	}
	if err := session.AwaitSchemaAgreement(ctx); err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, "", "", 0, err
	}

	uri := fmt.Sprintf("cassandra://%s:%d/%s?consistency=one&disable_initial_host_lookup=true", host, port, cassandraKeyspace)
	return container, uri, host, port, nil
}

func startCassandraContainerForMain(ctx context.Context) (testcontainers.Container, string, string, int, error) {
	return startCassandraContainerRaw(ctx)
}

func uniqueCassandraTable(prefix string) string {
	return strings.ToLower(prefix + "_" + uniqueSuffix())
}
