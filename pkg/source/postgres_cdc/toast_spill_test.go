package postgres_cdc

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func TestToastStateSurvivesDiskCacheEviction(t *testing.T) {
	state := newToastState()
	state.limit = 64 << 10
	t.Cleanup(func() { require.NoError(t, state.close()) })
	ctx := t.Context()
	payload := strings.Repeat("x", 128<<10)
	for i := range 200 {
		key := fmt.Sprint(i)
		require.NoError(t, state.put(ctx, "items", key, []interface{}{key + payload, []byte{0, 1, 255}, nil, tupleUnchangedMarker}, pglogrepl.LSN(i+1)))
	}
	require.Nil(t, state.memory)
	require.Zero(t, state.bytes)
	info, err := os.Stat(state.path)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(8<<20), "dirty pages must spill beyond SQLite's bounded cache")
	for _, i := range []int{0, 100, 199} {
		key := fmt.Sprint(i)
		values, err := state.get(ctx, "items", key)
		require.NoError(t, err)
		require.Equal(t, []interface{}{key + payload, []byte{0, 1, 255}, nil, tupleUnchangedMarker}, values)
	}
	require.NoError(t, state.prune(ctx, 100))
	values, err := state.get(ctx, "items", "0")
	require.NoError(t, err)
	require.Nil(t, values)
	values, err = state.get(ctx, "items", "199")
	require.NoError(t, err)
	require.Equal(t, "199"+payload, values[0])
}
