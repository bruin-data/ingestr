package duckdb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseQuackURI(t *testing.T) {
	cfg, err := ParseQuackURI("duckdb://:abc@0.0.0.0:9494?quack=true")
	require.NoError(t, err)
	require.Equal(t, &QuackConfig{Host: "0.0.0.0", Port: "9494", Token: "abc"}, cfg)

	cfg, err = ParseQuackURI("duckdb://:a%40b%3Ac@localhost?quack=true")
	require.NoError(t, err)
	require.Equal(t, "a@b:c", cfg.Token)

	cfg, err = ParseQuackURI("duckdb://:abc@my-server.com?quack=true&disable_ssl=true")
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

	for _, uri := range []string{
		"duckdb://myserver?token=secret123",
		"duckdb://:secret123@myserver",
		"duckdb://secret123@myserver?quack=true",
		"duckdb://:secret123@myserver:abc?quack=true",
	} {
		_, err = ParseQuackURI(uri)
		require.Error(t, err, uri)
		require.NotContains(t, err.Error(), "secret123", uri)
	}

	for uri, msg := range map[string]string{
		"duckdb://localhost:9494?quack=true":                  "token is required",
		"duckdb://localhost:9494?quack=true&token=abc":        "unsupported URI parameter",
		"duckdb://abc@localhost:9494?quack=true":              "must not include a username",
		"duckdb://:secret@localhost:abc?quack=true":           "invalid DuckDB URI",
		"duckdb://localhost:abc?token=secret":                 "invalid DuckDB URI",
		"duckdb://:abc@?quack=true":                           "must include the server host",
		"duckdb://:abc@localhost/file.db?quack=true":          "must not include a database path",
		"duckdb://:abc@localhost?quack=true&disable_ssl=nope": "invalid disable_ssl value",
		"duckdb://:abc@localhost?quack=true&tokn=x":           "unsupported URI parameter",
		"duckdb://:abc@myserver":                              "unsupported DuckDB URI credentials",
		"duckdb://myserver?quak=true":                         "unsupported DuckDB URI parameters",
		"duckdb://myserver?token=abc":                         "unsupported DuckDB URI parameters",
		"duckdb://:abc@localhost:9494?quack=false":            "unsupported DuckDB URI credentials",
		"duckdb:///tmp/a.db?access_mode=read_only":            "unsupported DuckDB URI parameters",
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
	connStr, err := d.BuildConnectionString("duckdb://:abc@localhost:9494?quack=true")
	require.NoError(t, err)
	require.Equal(t, "driver=duckdb;path=:memory:", connStr)
	require.NotNil(t, d.quack)
}
