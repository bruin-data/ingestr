package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	clickhouseUser     = "default"
	clickhousePassword = "clickhouse"
	clickhouseDB       = "testdb"
)

func startClickHouseContainerRaw(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, err := clickhouse.Run(
		ctx,
		"clickhouse/clickhouse-server:24.3",
		clickhouse.WithDatabase(clickhouseDB),
		clickhouse.WithUsername(clickhouseUser),
		clickhouse.WithPassword(clickhousePassword),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForListeningPort("9000/tcp"),
				wait.ForHTTP("/ping").WithPort("8123/tcp").WithStatusCodeMatcher(func(status int) bool {
					return status == 200
				}),
			).WithDeadline(120*time.Second),
		),
	)
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "9000")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("clickhouse://%s:%s@%s:%s/%s",
		clickhouseUser, clickhousePassword, host, port.Port(), clickhouseDB)

	_ = name
	return container, uri, nil
}

func startClickHouseContainerForMain(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, uri, err := startClickHouseContainerRaw(ctx, name)
	if err != nil {
		return nil, "", err
	}
	return container, uri, nil
}
