package postgres_cdc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPublicationGuardRechecksCoverage(t *testing.T) {
	calls := 0
	var checkErr error
	guard := publicationGuard{interval: time.Hour, check: func(context.Context) error { calls++; return checkErr }}
	require.NoError(t, guard.validate(t.Context(), false))
	require.Equal(t, 1, calls)
	require.NoError(t, guard.validate(t.Context(), publicationCheckRequired([]byte{'I'})))
	require.Equal(t, 1, calls, "ordinary row events must not query the catalog")
	require.NoError(t, guard.validate(t.Context(), publicationCheckRequired([]byte{'R'})))
	require.Equal(t, 2, calls)
	checkErr = errors.New("publication stopped publishing deletes")
	require.ErrorIs(t, guard.validate(t.Context(), publicationCheckRequired([]byte{'M'})), checkErr)
	guard.last = time.Now().Add(-2 * time.Hour)
	require.ErrorIs(t, guard.validate(t.Context(), false), checkErr, "quiet streams must also notice lost coverage")
}
