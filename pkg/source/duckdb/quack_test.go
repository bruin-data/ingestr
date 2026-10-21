package duckdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQuackURI(t *testing.T) {
	t.Parallel()

	cfg, err := ParseQuackURI("quack://db.internal:9000?token=s3cr'et&disable_ssl=true")
	require.NoError(t, err)
	assert.Equal(t, "db.internal", cfg.Host)
	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, "s3cr'et", cfg.Token)
	require.NotNil(t, cfg.DisableSSL)
	assert.True(t, *cfg.DisableSSL)
	assert.Equal(t,
		"ATTACH 'quack:db.internal:9000' AS ingestr_quack (TOKEN 's3cr''et', DISABLE_SSL true)",
		QuackAttachStatement(cfg, QuackAttachAlias))

	cfg, err = ParseQuackURI("quack://[::1]")
	require.NoError(t, err)
	assert.Equal(t, "ATTACH 'quack:[::1]' AS ingestr_quack", QuackAttachStatement(cfg, QuackAttachAlias))

	for _, bad := range []string{"quack://", "duckdb://localhost", "quack://localhost?disable_ssl=maybe", "quack://localhost:port"} {
		_, err := ParseQuackURI(bad)
		assert.Error(t, err, bad)
	}
}

func TestQuackRemoteSQL(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		`FROM ingestr_quack.query('SELECT * FROM "a"."it''s"')`,
		QuackRemoteSQL(QuackAttachAlias, `SELECT * FROM "a"."it's"`))

	ts := time.Date(2026, 1, 2, 3, 4, 5, 6000, time.FixedZone("x", 3600))
	assert.Equal(t,
		"FROM ingestr_quack.query('PREPARE ingestr_quack_stmt AS DELETE FROM t WHERE ts >= ? AND name = ?;\n"+
			"EXECUTE ingestr_quack_stmt(TIMESTAMP ''2026-01-02 02:04:05.000006'', ''o''''k'');\n"+
			"DEALLOCATE ingestr_quack_stmt')",
		QuackRemoteSQL(QuackAttachAlias, "DELETE FROM t WHERE ts >= ? AND name = ?", ts, "o'k"))
}

func TestQuackSQLLiteral(t *testing.T) {
	t.Parallel()

	var nilTime *time.Time
	cases := []struct {
		in   any
		want string
	}{
		{nil, "NULL"},
		{nilTime, "NULL"},
		{true, "true"},
		{int64(-7), "-7"},
		{uint32(7), "7"},
		{1.5, "'1.5'::DOUBLE"},
		{"a'b", "'a''b'"},
		{[]byte{0xaa, 0x01}, "from_hex('aa01')"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, QuackSQLLiteral(c.in))
	}
}
