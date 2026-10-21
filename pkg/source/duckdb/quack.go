package duckdb

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source/adbc"
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
		return fmt.Errorf("quack: failed to attach %s: %w", cfg.endpoint(), err)
	}
	return fmt.Errorf("quack: %s failed: %w", stmt, err)
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
		query = "PREPARE ingestr_quack_stmt AS " + query +
			";\nEXECUTE ingestr_quack_stmt(" + strings.Join(literals, ", ") + ")" +
			";\nDEALLOCATE ingestr_quack_stmt"
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
	db  *sql.DB
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
	d.db = db
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

// RewriteQuery runs reads on the server: scanning an attached Quack table outside
// the main schema drops the schema qualifier (DuckDB 1.5.x).
func (d *QuackDialect) RewriteQuery(query string) string {
	return QuackRemoteSQL(QuackAttachAlias, query)
}

// GetSchema runs the metadata queries on the server: the client's
// information_schema and duckdb_constraints() don't cover attached Quack catalogs.
func (d *QuackDialect) GetSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	catalog, schemaName, tableName := d.ParseTableNameWithCatalog(table)
	schemaSQL, pkSQL, args := schemaQuerySQL, primaryKeyQuerySQL, []any{schemaName, tableName}
	if catalog != "" {
		schemaSQL, pkSQL, args = schemaQueryForCatalogSQL, primaryKeyQueryForCatalogSQL, []any{catalog, schemaName, tableName}
	}

	rows, err := d.db.QueryContext(ctx, QuackRemoteSQL(QuackAttachAlias, schemaSQL, args...))
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, isNullable string
		if err := rows.Scan(&columnName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		dt, precision, scale, arrayType := d.MapDataType(dataType)
		columns = append(columns, schema.Column{
			Name:      adbc.CopyString(columnName),
			DataType:  dt,
			Nullable:  isNullable == "YES",
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	var primaryKeys []string
	var rawPK any
	if err := d.db.QueryRowContext(ctx, QuackRemoteSQL(QuackAttachAlias, pkSQL, args...)).Scan(&rawPK); err != nil {
		config.Debug("[QUACK] Failed to query primary keys: %v", err)
	} else {
		primaryKeys = d.ParsePrimaryKeyResult(adbc.CopyValue(rawPK))
	}
	for i := range columns {
		for _, pk := range primaryKeys {
			if columns[i].Name == pk {
				columns[i].IsPrimaryKey = true
				break
			}
		}
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}
