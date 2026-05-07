package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/bruin-data/ingestr/pkg/uv"
)

// SQL templates for DuckDB
const (
	schemaQuerySQL = `
		SELECT
			column_name,
			data_type,
			is_nullable
		FROM information_schema.columns
		WHERE table_catalog = current_database() AND table_schema = ? AND table_name = ?
		ORDER BY ordinal_position
	`

	primaryKeyQuerySQL = `
		SELECT string_agg(col, ',') as pk_cols
		FROM (
			SELECT unnest(constraint_column_names) as col
			FROM duckdb_constraints()
			WHERE database_name = current_database() AND table_name = ? AND constraint_type = 'PRIMARY KEY'
		)
	`
)

var (
	driverOnce sync.Once
	driverErr  error
	uvChecker  = &uv.Checker{}
)

// Dialect implements the adbc.Dialect interface for DuckDB.
type Dialect struct{}

// NewDialect creates a new DuckDB dialect.
func NewDialect() *Dialect {
	return &Dialect{}
}

func (d *Dialect) Name() string {
	return "DUCKDB"
}

func (d *Dialect) Schemes() []string {
	return []string{"duckdb", "motherduck", "md"}
}

func (d *Dialect) DriverName() string {
	return "duckdb"
}

func (d *Dialect) EnsureDriver(ctx context.Context) error {
	driverOnce.Do(func() {
		driverErr = ensureDriverInstalled(ctx)
	})
	return driverErr
}

func ensureDriverInstalled(ctx context.Context) error {
	config.Debug("[DUCKDB] Checking if ADBC driver is available...")

	if tryLoadDriver() {
		config.Debug("[DUCKDB] ADBC driver already available")
		return nil
	}

	config.Debug("[DUCKDB] ADBC driver not found, installing...")

	uvPath, err := uvChecker.EnsureUvInstalled(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure uv is installed: %w", err)
	}

	// Install DBC tool
	config.Debug("[DUCKDB] Installing dbc tool via uv...")
	cmd := exec.CommandContext(ctx, uvPath, "tool", "install", "--quiet", "--no-config", "dbc")
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run() // Ignore error if already installed

	// Install DuckDB driver via DBC
	config.Debug("[DUCKDB] Installing DuckDB ADBC driver via dbc...")
	cmd = exec.CommandContext(ctx, uvPath, "tool", "run", "--no-config", "dbc", "install", "duckdb")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dbc install duckdb failed: %w", err)
	}

	// Verify driver is now available
	if !tryLoadDriver() {
		return errors.New("DuckDB ADBC driver still not available after installation")
	}

	config.Debug("[DUCKDB] ADBC driver installed successfully")
	return nil
}

func tryLoadDriver() bool {
	db, err := sql.Open(adbc.ADBCDriverName, "driver=duckdb;path=:memory:")
	if err != nil {
		return false
	}
	defer func() { _ = db.Close() }()
	return db.Ping() == nil
}

func (d *Dialect) BuildConnectionString(uri string) (string, error) {
	dbPath, err := parseDBPath(uri)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("driver=duckdb;path=%s", dbPath), nil
}

// parseDBPath extracts the database path from a duckdb://, motherduck://, or md:// URI
func parseDBPath(uri string) (string, error) {
	if strings.HasPrefix(uri, "motherduck://") || strings.HasPrefix(uri, "md://") {
		parsed, err := url.Parse(uri)
		if err != nil {
			return "", fmt.Errorf("failed to parse MotherDuck URI: %w", err)
		}

		token := parsed.Query().Get("token")
		if token == "" {
			return "", fmt.Errorf("MotherDuck token is required (use ?token=<your-token> in URI)")
		}

		database := strings.TrimPrefix(parsed.Host+parsed.Path, "/")
		database = strings.TrimPrefix(database, "/")

		if database == "" {
			return fmt.Sprintf("md:?motherduck_token=%s", token), nil
		}
		return fmt.Sprintf("md:%s?motherduck_token=%s", database, token), nil
	}

	// Handle simple cases
	if uri == "duckdb://:memory:" || uri == "duckdb:///:memory:" {
		return ":memory:", nil
	}

	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	// duckdb:///path/to/db.db -> /path/to/db.db
	path := u.Path
	if path == "" {
		path = u.Host + u.Path
	}

	// Remove leading slashes for relative paths on Windows
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}

	if path == "" {
		return ":memory:", nil
	}

	// Handle relative paths: duckdb:///file.db -> ./file.db
	// If path starts with / but is just /filename.db (no nested dirs), treat as relative
	if strings.HasPrefix(path, "/") && !strings.Contains(path[1:], "/") {
		path = "." + path
	}

	return path, nil
}

func (d *Dialect) DefaultSchema() string {
	return "main"
}

func (d *Dialect) ParseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return d.DefaultSchema(), table
}

func (d *Dialect) SchemaQuery() string {
	return schemaQuerySQL
}

func (d *Dialect) PrimaryKeyQuery() string {
	return primaryKeyQuerySQL
}

func (d *Dialect) MapDataType(dbType string) (schema.DataType, int, int, schema.DataType) {
	return MapDuckDBToDataType(dbType)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, name)
}

func (d *Dialect) ParsePrimaryKeyResult(rawValue interface{}) []string {
	if rawValue == nil {
		return nil
	}

	// DuckDB returns PKs as comma-separated string from string_agg
	if pkStr, ok := rawValue.(string); ok && pkStr != "" {
		pkStr = strings.Trim(pkStr, "[]")
		parts := strings.Split(pkStr, ",")
		var result []string
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				result = append(result, p)
			}
		}
		return result
	}
	return nil
}
