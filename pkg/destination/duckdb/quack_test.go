package duckdb

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/stretchr/testify/require"
)

func TestQuackConnectFailureDoesNotLeakToken(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = stderr })

	connectErr := NewQuackDestination().Connect(context.Background(), "quack://127.0.0.1:1?token=leaky_secret_token&disable_ssl=true")
	config.PrintFailedQuery()
	require.NoError(t, w.Close())
	os.Stderr = stderr
	printed, err := io.ReadAll(r)
	require.NoError(t, err)

	require.Error(t, connectErr)
	require.NotContains(t, connectErr.Error(), "leaky_secret_token")
	require.Contains(t, string(printed), "ATTACH")
	require.NotContains(t, string(printed), "leaky_secret_token")
}

func TestQuackFlushRows(t *testing.T) {
	t.Setenv("INGESTR_DUCKDB_CHECKPOINT_ROWS", "")
	require.Equal(t, int64(1_000_000), quackFlushRows())
	t.Setenv("INGESTR_DUCKDB_CHECKPOINT_ROWS", "500")
	require.Equal(t, int64(500), quackFlushRows())
	t.Setenv("INGESTR_DUCKDB_CHECKPOINT_ROWS", "-1")
	require.Equal(t, int64(1), quackFlushRows())
}
