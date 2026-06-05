//go:build integration

package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mssql"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	mssqlPassword    = "TestPassword123!"
	mssqlPasswordEnc = "TestPassword123%21" // URL-encoded version of password
	mssqlDB          = "master"
)

func startMSSQLContainerRaw(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, err := mssql.Run(
		ctx,
		"mcr.microsoft.com/mssql/server:2022-latest",
		mssql.WithAcceptEULA(),
		mssql.WithPassword(mssqlPassword),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForListeningPort("1433/tcp"),
				wait.ForLog("SQL Server is now ready for client connections"),
			).WithDeadline(2*time.Minute),
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
	port, err := container.MappedPort(ctx, "1433")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("mssql://sa:%s@%s:%s/%s?encrypt=disable",
		mssqlPasswordEnc, host, port.Port(), mssqlDB)

	_ = name
	return container, uri, nil
}

func startMSSQLContainerForMain(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, uri, err := startMSSQLContainerRaw(ctx, name)
	if err != nil {
		return nil, "", err
	}
	return container, uri, nil
}
