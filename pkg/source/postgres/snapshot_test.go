package postgres

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestParseSnapshotURI(t *testing.T) {
	for _, scheme := range []string{"postgres", "postgresql", "postgresql+psycopg2"} {
		t.Run(scheme, func(t *testing.T) {
			normalized, snapshot, err := parseSnapshotURI(scheme + "://user:password@localhost/database?snapshot=00000003-0000001B-1&sslmode=disable&application_name=initial%20load")
			require.NoError(t, err)
			require.Equal(t, "00000003-0000001B-1", snapshot)
			parsed, err := url.Parse(normalized)
			require.NoError(t, err)
			require.False(t, parsed.Query().Has("snapshot"))
			require.Equal(t, "disable", parsed.Query().Get("sslmode"))
			require.Equal(t, "initial load", parsed.Query().Get("application_name"))
			cfg, err := pgxpool.ParseConfig(normalized)
			require.NoError(t, err)
			require.NotContains(t, cfg.ConnConfig.RuntimeParams, "snapshot")
		})
	}
	const ordinaryURI = "postgres://user:password@localhost/database?sslmode=disable"
	normalized, snapshot, err := parseSnapshotURI(ordinaryURI)
	require.NoError(t, err)
	require.Equal(t, ordinaryURI, normalized)
	require.Empty(t, snapshot)
}

func TestParseSnapshotURIRejectsInvalidSnapshot(t *testing.T) {
	for _, params := range []string{
		"snapshot=",
		"snapshot=0%2F16B6C50",
		"snapshot=00000003-0000001B-0",
		"snapshot=00000003-0000001B-1%27%3BSELECT+1",
		"snapshot=00000003-0000001B-1%0A",
		"snapshot=00000003-0000001B-1&snapshot=00000003-0000001B-1",
		"snapshot=%zz",
	} {
		t.Run(params, func(t *testing.T) {
			_, _, err := parseSnapshotURI("postgres://localhost/database?" + params)
			require.Error(t, err)
		})
	}
}
