package duckdb

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const QuackAlias = "quack_db"

type QuackConfig struct {
	Host       string
	Port       string
	Token      string
	DisableSSL *bool
}

// ParseQuackURI parses duckdb://host[:port]?quack=true&token=...; it returns nil
// for URIs that don't enable quack.
func ParseQuackURI(uri string) (*QuackConfig, error) {
	if !strings.HasPrefix(uri, "duckdb://") {
		return nil, nil
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, nil //nolint:nilerr // not a URL-shaped path, so not a quack URI
	}
	q := u.Query()
	// Without quack mode the query string would become part of the local file name.
	if q.Get("quack") != "true" {
		if u.RawQuery != "" {
			keys := make([]string, 0, len(q))
			for key := range q {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			return nil, fmt.Errorf("unsupported DuckDB URI parameters %v; to connect to a Quack server use duckdb://host:port?quack=true&token=<token>", keys)
		}
		return nil, nil
	}
	for key := range q {
		if key != "quack" && key != "token" && key != "disable_ssl" {
			return nil, fmt.Errorf("quack: unsupported URI parameter %q", key)
		}
	}

	cfg := &QuackConfig{Host: u.Hostname(), Port: u.Port(), Token: q.Get("token")}
	if cfg.Host == "" {
		return nil, fmt.Errorf("quack: URI must include the server host, e.g. duckdb://localhost:9494?quack=true&token=<token>")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("quack: URI must not include a database path")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("quack: token is required (use ?token=<token> in URI)")
	}
	if v := q.Get("disable_ssl"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("quack: invalid disable_ssl value %q", v)
		}
		cfg.DisableSSL = &b
	}
	return cfg, nil
}

func (c *QuackConfig) endpoint() string {
	host := c.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if c.Port != "" {
		host += ":" + c.Port
	}
	return "quack:" + host
}

func (c *QuackConfig) AttachStatements() []string {
	endpoint := quoteSQLStringLiteral(c.endpoint())
	attach := "ATTACH " + endpoint + " AS " + QuackAlias
	if c.DisableSSL != nil {
		attach += " (DISABLE_SSL " + strconv.FormatBool(*c.DisableSSL) + ")"
	}
	return []string{
		"INSTALL quack",
		"LOAD quack",
		"CREATE OR REPLACE SECRET ingestr_quack (TYPE quack, TOKEN " + quoteSQLStringLiteral(c.Token) + ", SCOPE " + endpoint + ")",
		attach,
		"USE " + QuackAlias,
		// Pin the server session to UTC, as the local session is.
		"FROM " + QuackAlias + ".query('SET TimeZone=''UTC''')",
	}
}

// AttachError keeps the token out of bootstrap errors.
func (c *QuackConfig) AttachError(stmt string, err error) error {
	return fmt.Errorf("quack: %w", BootstrapError(RedactSecret(stmt), err))
}
