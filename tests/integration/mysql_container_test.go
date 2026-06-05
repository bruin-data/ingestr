//go:build integration

package integration

import (
	"context"
	"fmt"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

const (
	mysqlUser     = "root"
	mysqlPassword = "testpassword"
	mysqlDB       = "testdb"
)

func startMySQLContainerRaw(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, err := mysql.Run(
		ctx,
		"mysql:8.0",
		mysql.WithDatabase(mysqlDB),
		mysql.WithUsername(mysqlUser),
		mysql.WithPassword(mysqlPassword),
	)
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "3306")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("mysql://%s:%s@%s:%s/%s",
		mysqlUser, mysqlPassword, host, port.Port(), mysqlDB)

	_ = name
	return container, uri, nil
}

func startMySQLContainerForMain(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, uri, err := startMySQLContainerRaw(ctx, name)
	if err != nil {
		return nil, "", err
	}
	return container, uri, nil
}
