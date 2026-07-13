package postgres_cdc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBlockedStandbyWriteShutdownIsBounded(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	started := time.Now()
	err := withWriteDeadline(context.Background(), client, 25*time.Millisecond, func() error {
		_, err := client.Write(make([]byte, 64))
		return err
	})
	require.Error(t, err)
	require.Less(t, time.Since(started), time.Second, "blocked standby write did not respect its socket deadline")
}

func TestStandbyWriteCancellationUnblocksSocket(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(25*time.Millisecond, cancel)
	started := time.Now()
	err := withWriteDeadline(ctx, client, time.Hour, func() error {
		_, err := client.Write(make([]byte, 64))
		return err
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(started), time.Second, "canceled standby write did not unblock")
}
