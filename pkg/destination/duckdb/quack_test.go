package duckdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInlineQuackArgs(t *testing.T) {
	ts := time.Date(2026, 10, 24, 10, 0, 0, 0, time.FixedZone("x", 3*3600))

	sql, err := inlineQuackArgs(`DELETE FROM "a?b" WHERE "k" >= ? AND "k" <= ? AND note = 'why?'`, []interface{}{ts, int64(7)})
	require.NoError(t, err)
	require.Equal(t, `DELETE FROM "a?b" WHERE "k" >= TIMESTAMP '2026-10-24 07:00:00' AND "k" <= 7 AND note = 'why?'`, sql)

	sql, err = inlineQuackArgs(`UPDATE t SET a = $1, b = $2, c = $1`, []interface{}{"it's", nil})
	require.NoError(t, err)
	require.Equal(t, `UPDATE t SET a = 'it''s', b = NULL, c = 'it''s'`, sql)

	_, err = inlineQuackArgs(`SELECT ?, ?`, []interface{}{1})
	require.Error(t, err)
	_, err = inlineQuackArgs(`SELECT $2`, []interface{}{1})
	require.Error(t, err)
}
