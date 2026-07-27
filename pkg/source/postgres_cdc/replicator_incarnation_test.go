package postgres_cdc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestReplicatorChecksTableIncarnationWithoutDatabase(t *testing.T) {
	repl, err := NewReplicator(
		NewPostgresCDCSource(),
		"public.items",
		&schema.TableSchema{},
		CDCConfig{DiscoverInterval: time.Nanosecond},
		0,
		true,
		"",
	)
	require.NoError(t, err)
	require.NoError(t, repl.ExpectTableIncarnation("41"))

	repl.incarnationLookup = func(context.Context) (string, error) { return "42", nil }
	err = repl.checkTableIncarnation(context.Background())
	var reincarnated *TableReincarnatedError
	require.ErrorAs(t, err, &reincarnated)
	require.Equal(t, "41", reincarnated.Previous)
	require.Equal(t, "42", reincarnated.Current)

	repl.lastIncarnationCheck = time.Time{}
	repl.incarnationLookup = func(context.Context) (string, error) { return "", errors.New("temporary catalog failure") }
	require.NoError(t, repl.checkTableIncarnation(context.Background()))
}

func TestTableIncarnationRequiresConnectedSource(t *testing.T) {
	_, err := NewPostgresCDCSource().TableIncarnation(context.Background(), "public.items")
	require.ErrorContains(t, err, "not connected")
}
