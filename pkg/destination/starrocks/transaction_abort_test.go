package starrocks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTransactionConfirmAbortedRequiresTerminalState(t *testing.T) {
	for _, state := range []string{"ABORTED", "COMMITTED", "VISIBLE", "PREPARED", "PREPARE", "UNKNOWN", ""} {
		t.Run(state, func(t *testing.T) {
			checks := 0
			tx := &streamLoadTransaction{id: 42, database: "db", loader: &streamLoader{
				transactionState: func(ctx context.Context, database string, id int64) (string, error) {
					checks++
					require.Equal(t, "db", database)
					require.EqualValues(t, 42, id)
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					require.LessOrEqual(t, time.Until(deadline), 2*time.Minute)
					return state, nil
				},
			}}
			aborted, err := tx.confirmAborted(t.Context())
			require.NoError(t, err)
			require.Equal(t, state == "ABORTED", aborted)
			require.Equal(t, state == "ABORTED", tx.closed)
			require.False(t, tx.committed)
			if aborted {
				aborted, err = tx.confirmAborted(t.Context())
				require.NoError(t, err)
				require.True(t, aborted)
				require.Equal(t, 1, checks)
			}
		})
	}
}

func TestTransactionConfirmAbortedPreservesUncertainty(t *testing.T) {
	want := errors.New("transaction status unavailable")
	tx := &streamLoadTransaction{id: 42, loader: &streamLoader{
		transactionState: func(context.Context, string, int64) (string, error) { return "", want },
	}}
	aborted, err := tx.confirmAborted(t.Context())
	require.ErrorIs(t, err, want)
	require.False(t, aborted)
	require.False(t, tx.closed)

	tx.loader.transactionState = func(context.Context, string, int64) (string, error) {
		t.Fatal("a committed transaction must not be reconsidered for replay")
		return "ABORTED", nil
	}
	tx.committed = true
	aborted, err = tx.confirmAborted(t.Context())
	require.NoError(t, err)
	require.False(t, aborted)
}
