package snowflake

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	sfauth "github.com/bruin-data/ingestr/pkg/snowflake"
	"github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

// SQL templates for Snowflake
const (
	// INFORMATION_SCHEMA query for schema fetching
	// Note: This is relatively slow (~1s) due to Snowflake's metadata system
	// but it's the most reliable way to get column information
	schemaQuerySQL = `
		SELECT
			COLUMN_NAME,
			DATA_TYPE,
			IS_NULLABLE
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`

	// snowflakeMaxVarcharLength is the length Snowflake reports for a bare VARCHAR;
	// treated as "unbounded" so it isn't propagated as a literal VARCHAR(16777216).
	snowflakeMaxVarcharLength = 16777216
)

var (
	driverOnce sync.Once
	driverErr  error
)

// Dialect implements the adbc.Dialect interface for Snowflake.
// It also implements ConnectionAware, SchemaProvider, and StorageReader for optimized operations.
type Dialect struct {
	db         *sql.DB   // Set via ConnectionAware interface (ADBC connection)
	uri        string    // Set via SetURI for native ADBC streaming
	nativeDB   *sql.DB   // Cached native gosnowflake connection for reuse
	nativeOnce sync.Once // Ensures native connection is created only once
	nativeErr  error     // Error from native connection creation
}

// NewDialect creates a new Snowflake dialect.
func NewDialect() *Dialect {
	return &Dialect{}
}

func (d *Dialect) Name() string {
	return "SNOWFLAKE"
}

func (d *Dialect) Schemes() []string {
	return []string{"snowflake"}
}

func (d *Dialect) DriverName() string {
	return "snowflake"
}

func (d *Dialect) EnsureDriver(ctx context.Context) error {
	driverOnce.Do(func() {
		driverErr = ensureDriverInstalled(ctx)
	})
	return driverErr
}

func ensureDriverInstalled(ctx context.Context) error {
	config.Debug("[SNOWFLAKE] Checking if ADBC driver is available...")

	if tryLoadDriver() {
		config.Debug("[SNOWFLAKE] ADBC driver already available")
		return nil
	}

	config.Debug("[SNOWFLAKE] ADBC driver not found, installing...")

	if err := adbc.InstallDriver(ctx, "snowflake"); err != nil {
		return err
	}

	// Verify driver is now available
	if !tryLoadDriver() {
		return errors.New("snowflake ADBC driver still not available after installation")
	}

	config.Debug("[SNOWFLAKE] ADBC driver installed successfully")
	return nil
}

func tryLoadDriver() bool {
	// Test if snowflake driver can be loaded with minimal connection string
	// The connection will fail auth but confirms driver is loadable
	db, err := sql.Open(adbc.ADBCDriverName, "driver=snowflake;adbc.snowflake.sql.account=test")
	if err != nil {
		return false
	}
	defer func() { _ = db.Close() }()
	// Don't ping - auth will fail; just check driver loads
	return true
}

func (d *Dialect) BuildConnectionString(uri string) (string, error) {
	auth, err := sfauth.ParseURI(uri)
	if err != nil {
		return "", err
	}

	dsn, err := auth.ToDSN()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("driver=snowflake;uri=%s", dsn), nil
}

func (d *Dialect) DefaultSchema() string {
	return "PUBLIC" // Snowflake default schema is PUBLIC (uppercase)
}

func (d *Dialect) ParseTableName(table string) (string, string) {
	_, schemaName, tableName := d.ParseTableNameWithCatalog(table)
	return schemaName, tableName
}

// ParseTableNameWithCatalog implements adbc.CatalogAwareDialect. Snowflake
// tables live in a database.schema.table namespace.
func (d *Dialect) ParseTableNameWithCatalog(table string) (string, string, string) {
	tn, err := tablename.Snowflake.Parse(table, tablename.Defaults{Schema: d.DefaultSchema()})
	if err != nil {
		// Best-effort fallback preserving the legacy 2-part behavior.
		parts := strings.SplitN(table, ".", 2)
		if len(parts) == 2 {
			return "", strings.ToUpper(parts[0]), strings.ToUpper(parts[1])
		}
		return "", d.DefaultSchema(), strings.ToUpper(table)
	}
	// Snowflake treats unquoted identifiers as uppercase.
	tn = tn.Upper()
	return tn.Catalog, tn.Schema, tn.Table
}

func (d *Dialect) SchemaQuery() string {
	return schemaQuerySQL
}

func (d *Dialect) PrimaryKeyQuery() string {
	// Snowflake doesn't support KEY_COLUMN_USAGE in the same way as other databases.
	// Skip PK detection to avoid slow queries that will fail anyway.
	// Users can specify primary keys explicitly via --primary-key flag if needed.
	return ""
}

func (d *Dialect) MapDataType(dbType string) (schema.DataType, int, int, schema.DataType) {
	return MapSnowflakeToDataType(dbType)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(strings.ToUpper(name), `"`, `""`))
}

func (d *Dialect) QuoteCustomQueryIdentifier(name string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(name, `"`, `""`))
}

func (d *Dialect) ParsePrimaryKeyResult(rawValue interface{}) []string {
	if rawValue == nil {
		return nil
	}
	// Snowflake PK query returns individual column names (one per row)
	if pkStr, ok := rawValue.(string); ok && pkStr != "" {
		return []string{strings.TrimSpace(pkStr)}
	}
	return nil
}

// SetConnection implements adbc.ConnectionAware interface.
// This allows the dialect to use the connection for optimized schema fetching.
func (d *Dialect) SetConnection(db *sql.DB) {
	d.db = db
}

// GetSchema implements adbc.SchemaProvider interface.
// Uses DESCRIBE TABLE which is faster than INFORMATION_SCHEMA queries.
func (d *Dialect) GetSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	// Prefer native connection if available (faster, reused for data fetching)
	db := d.nativeDB
	if db == nil {
		db = d.db
	}
	if db == nil {
		return nil, errors.New("database connection not available")
	}

	catalog, schemaName, tableName := d.ParseTableNameWithCatalog(table)
	fullTable := tablename.TableName{Catalog: catalog, Schema: schemaName, Table: tableName}.String()

	config.Debug("[SNOWFLAKE] Using DESCRIBE TABLE for schema fetching: %s", fullTable)

	query := fmt.Sprintf(`DESCRIBE TABLE %s ->> SELECT "name", "type", "null?" FROM $1`, fullTable)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var name, dataType, nullable string

		if err := rows.Scan(&name, &dataType, &nullable); err != nil {
			return nil, fmt.Errorf("failed to scan describe row: %w", err)
		}

		dt, precision, scale, arrayType := d.MapDataType(dataType)

		col := schema.Column{
			Name:      adbc.CopyString(name),
			DataType:  dt,
			Nullable:  nullable == "Y",
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		}
		if dt == schema.TypeString {
			// DESCRIBE reports the full type (e.g. VARCHAR(50)); keep the length.
			// A bare VARCHAR reports as 16777216, which is treated as unbounded.
			col.MaxLength = parseSnowflakeStringLength(dataType)
		}
		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	return &schema.TableSchema{
		Name:    tableName,
		Schema:  schemaName,
		Columns: columns,
	}, nil
}
