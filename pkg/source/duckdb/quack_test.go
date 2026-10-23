package duckdb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseQuackURI(t *testing.T) {
	cfg, err := ParseQuackURI("duckdb://0.0.0.0:9494?quack=true&token=abc")
	require.NoError(t, err)
	require.Equal(t, &QuackConfig{Host: "0.0.0.0", Port: "9494", Token: "abc"}, cfg)

	cfg, err = ParseQuackURI("duckdb://my-server.com?quack=true&token=abc&disable_ssl=true")
	require.NoError(t, err)
	require.Equal(t, "quack:my-server.com", cfg.endpoint())
	require.NotNil(t, cfg.DisableSSL)
	require.True(t, *cfg.DisableSSL)

	for _, uri := range []string{
		"duckdb:///tmp/a.db",
		"duckdb://data.db",
		"duckdb://:memory:",
		"motherduck://db?token=abc",
	} {
		cfg, err := ParseQuackURI(uri)
		require.NoError(t, err, uri)
		require.Nil(t, cfg, uri)
	}

	_, err = ParseQuackURI("duckdb://myserver?token=secret123")
	require.NotContains(t, err.Error(), "secret123")

	for uri, msg := range map[string]string{
		"duckdb://localhost:9494?quack=true":                       "token is required",
		"duckdb://localhost:abc?quack=true&token=secret":           "invalid DuckDB URI",
		"duckdb://localhost:abc?token=secret":                      "invalid DuckDB URI",
		"duckdb://?quack=true&token=abc":                           "must include the server host",
		"duckdb://localhost/file.db?quack=true&token=abc":          "must not include a database path",
		"duckdb://localhost?quack=true&token=abc&disable_ssl=nope": "invalid disable_ssl value",
		"duckdb://localhost?quack=true&token=abc&tokn=x":           "unsupported URI parameter",
		"duckdb://myserver?quak=true&token=abc":                    "unsupported DuckDB URI parameters",
		"duckdb://myserver?token=abc":                              "unsupported DuckDB URI parameters",
		"duckdb://localhost:9494?quack=false&token=abc":            "unsupported DuckDB URI parameters",
		"duckdb:///tmp/a.db?access_mode=read_only":                 "unsupported DuckDB URI parameters",
	} {
		_, err := ParseQuackURI(uri)
		require.ErrorContains(t, err, msg, uri)
	}
}

func TestQuackAttachStatements(t *testing.T) {
	disable := true
	cfg := &QuackConfig{Host: "localhost", Port: "9494", Token: "it's", DisableSSL: &disable}
	stmts := cfg.AttachStatements()

	require.Contains(t, stmts, "CREATE OR REPLACE SECRET ingestr_quack (TYPE quack, TOKEN 'it''s', SCOPE 'quack:localhost:9494')")
	require.Contains(t, stmts, "ATTACH 'quack:localhost:9494' AS quack_db (DISABLE_SSL true)")
	require.Contains(t, stmts, "USE quack_db")
	for _, stmt := range stmts {
		if strings.Contains(stmt, "SECRET") {
			require.NotContains(t, RedactSecret(stmt), "it''s")
		}
	}
}

func TestDialectBuildConnectionString_Quack(t *testing.T) {
	d := NewDialect()
	connStr, err := d.BuildConnectionString("duckdb://localhost:9494?quack=true&token=abc")
	require.NoError(t, err)
	require.Equal(t, "driver=duckdb;path=:memory:", connStr)
	require.NotNil(t, d.quack)
}
