package duckdb

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

const QuackAttachAlias = "ingestr_quack"

type QuackConfig struct {
	Host       string
	Port       int
	Token      string
	DisableSSL *bool
}

// ParseQuackURI parses quack://host[:port]?token=...&disable_ssl=true.
func ParseQuackURI(raw string) (*QuackConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid quack URI: %w", err)
	}
	if u.Scheme != "quack" {
		return nil, fmt.Errorf("expected quack scheme, got %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("quack URI must include a host, e.g. quack://localhost:9494")
	}

	cfg := &QuackConfig{Host: u.Hostname()}
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid quack port %q: %w", p, err)
		}
		cfg.Port = port
	}

	q := u.Query()
	cfg.Token = q.Get("token")
	if v := q.Get("disable_ssl"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid disable_ssl %q: %w", v, err)
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
	if c.Port != 0 {
		return "quack:" + host + ":" + strconv.Itoa(c.Port)
	}
	return "quack:" + host
}

func QuackAttachStatement(cfg *QuackConfig, alias string) string {
	var opts []string
	if cfg.Token != "" {
		opts = append(opts, "TOKEN "+quoteSQLStringLiteral(cfg.Token))
	}
	if cfg.DisableSSL != nil {
		opts = append(opts, "DISABLE_SSL "+strconv.FormatBool(*cfg.DisableSSL))
	}
	stmt := "ATTACH " + quoteSQLStringLiteral(cfg.endpoint()) + " AS " + alias
	if len(opts) > 0 {
		stmt += " (" + strings.Join(opts, ", ") + ")"
	}
	return stmt
}

// QuackBootstrapStatements loads the quack extension and attaches the server.
func QuackBootstrapStatements(cfg *QuackConfig, alias string) []string {
	return []string{"INSTALL quack", "LOAD quack", QuackAttachStatement(cfg, alias)}
}

func WrapQuackBootstrapError(cfg *QuackConfig, stmt string, err error) error {
	if strings.HasPrefix(stmt, "ATTACH") {
		if strings.Contains(err.Error(), "Unrecognized storage type") {
			return fmt.Errorf("quack: failed to attach %s (Quack requires the DuckDB ADBC driver v1.5.3 or newer; upgrade with `dbc install duckdb`): %w", cfg.endpoint(), err)
		}
		if msg := err.Error(); strings.Contains(msg, "HTTP 500") || strings.Contains(msg, "Failed to deserialize") {
			return fmt.Errorf("quack: failed to attach %s (the server may run a different DuckDB version than ingestr's DuckDB driver; Quack needs both on the same release, e.g. install a matching driver with `dbc install duckdb=<server version>`): %w", cfg.endpoint(), err)
		}
		return fmt.Errorf("quack: failed to attach %s: %w", cfg.endpoint(), err)
	}
	return fmt.Errorf("quack: %s failed: %w", stmt, err)
}

var quackStmtSeq atomic.Uint64

// QuackRedactedStatement masks the token in a bootstrap statement so it can be logged.
func QuackRedactedStatement(cfg *QuackConfig, stmt string) string {
	if cfg.Token == "" {
		return stmt
	}
	return strings.ReplaceAll(stmt, quoteSQLStringLiteral(cfg.Token), "'***'")
}

// QuackRemoteSQL wraps a statement so it runs on the Quack server. An attached
// Quack catalog only supports scans, CREATE and INSERT, so everything else must
// be shipped verbatim. Arguments are inlined through a server-side PREPARE.
func QuackRemoteSQL(alias, query string, args ...any) string {
	if len(args) > 0 {
		literals := make([]string, len(args))
		for i, a := range args {
			literals[i] = QuackSQLLiteral(a)
		}
		name := "ingestr_quack_stmt_" + strconv.FormatUint(quackStmtSeq.Add(1), 10)
		query = "PREPARE " + name + " AS " + query +
			";\nEXECUTE " + name + "(" + strings.Join(literals, ", ") + ")" +
			";\nDEALLOCATE " + name
	}
	return "FROM " + alias + ".query(" + quoteSQLStringLiteral(query) + ")"
}

func QuackSQLLiteral(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case *time.Time:
		if x == nil {
			return "NULL"
		}
		return QuackSQLLiteral(*x)
	case time.Time:
		return "TIMESTAMP " + quoteSQLStringLiteral(x.UTC().Format("2006-01-02 15:04:05.999999"))
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float32:
		return quoteSQLStringLiteral(strconv.FormatFloat(float64(x), 'g', -1, 64)) + "::DOUBLE"
	case float64:
		return quoteSQLStringLiteral(strconv.FormatFloat(x, 'g', -1, 64)) + "::DOUBLE"
	case string:
		return quoteSQLStringLiteral(x)
	case []byte:
		return "from_hex(" + quoteSQLStringLiteral(hex.EncodeToString(x)) + ")"
	default:
		return quoteSQLStringLiteral(fmt.Sprintf("%v", x))
	}
}

type QuackDialect struct {
	*Dialect
	cfg *QuackConfig
}

func NewQuackDialect() *QuackDialect {
	return &QuackDialect{Dialect: NewDialect()}
}

func (d *QuackDialect) Name() string      { return "QUACK" }
func (d *QuackDialect) Schemes() []string { return []string{"quack"} }

func (d *QuackDialect) BuildConnectionString(uri string) (string, error) {
	cfg, err := ParseQuackURI(uri)
	if err != nil {
		return "", err
	}
	d.cfg = cfg
	return d.Dialect.BuildConnectionString("duckdb://:memory:")
}

func (d *QuackDialect) InitConnection(ctx context.Context, db *sql.DB) error {
	for _, stmt := range QuackBootstrapStatements(d.cfg, QuackAttachAlias) {
		if strings.HasPrefix(stmt, "ATTACH") {
			config.Debug("[QUACK] ATTACH %s AS %s", d.cfg.endpoint(), QuackAttachAlias)
		} else {
			config.Debug("[QUACK] %s", stmt)
		}
		rows, err := db.QueryContext(ctx, stmt)
		if err == nil {
			err = rows.Close()
		}
		if err != nil {
			return WrapQuackBootstrapError(d.cfg, stmt, err)
		}
	}
	return nil
}

// RewriteQuery runs reads and metadata queries on the server: the client's
// information_schema doesn't cover attached Quack catalogs, and scanning a table
// outside the main schema drops the schema qualifier (DuckDB 1.5.x).
func (d *QuackDialect) RewriteQuery(query string, args ...any) string {
	return QuackRemoteSQL(QuackAttachAlias, query, args...)
}
