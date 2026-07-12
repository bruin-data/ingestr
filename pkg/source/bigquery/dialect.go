package bigquery

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"google.golang.org/api/option"
)

var (
	driverOnce sync.Once
	driverErr  error
)

// Dialect implements the adbc.Dialect interface for BigQuery.
type Dialect struct {
	projectID string
	location  string
	credPath  string // Path to credentials file
	credJSON  string // JSON credentials string
}

// NewDialect creates a new BigQuery dialect.
func NewDialect() *Dialect {
	return &Dialect{}
}

func (d *Dialect) Name() string {
	return "BIGQUERY"
}

func (d *Dialect) Schemes() []string {
	return []string{"bigquery"}
}

func (d *Dialect) DriverName() string {
	return "bigquery"
}

func (d *Dialect) EnsureDriver(ctx context.Context) error {
	driverOnce.Do(func() {
		driverErr = ensureDriverInstalled(ctx)
	})
	return driverErr
}

func ensureDriverInstalled(ctx context.Context) error {
	config.Debug("[BIGQUERY] Checking if ADBC driver is available...")

	if tryLoadDriver() {
		config.Debug("[BIGQUERY] ADBC driver already available")
		return nil
	}

	config.Debug("[BIGQUERY] ADBC driver not found, installing...")

	if err := adbc.InstallDriver(ctx, "bigquery"); err != nil {
		return err
	}

	// Verify driver is now available
	if !tryLoadDriver() {
		return errors.New("BigQuery ADBC driver still not available after installation")
	}

	config.Debug("[BIGQUERY] ADBC driver installed successfully")
	return nil
}

func tryLoadDriver() bool {
	db, err := sql.Open(adbc.ADBCDriverName, "driver=bigquery;adbc.bigquery.sql.project_id=test")
	if err != nil {
		return false
	}
	defer func() { _ = db.Close() }()
	return true
}

func (d *Dialect) BuildConnectionString(uri string) (string, error) {
	// Parse bigquery://<project-name>?credentials_path=/path/to/sa.json&location=<location>
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid BigQuery URI: %w", err)
	}

	projectID := u.Host
	if projectID == "" {
		return "", errors.New("BigQuery URI must include project_id as host (e.g., bigquery://my-project)")
	}

	d.projectID = projectID

	// Build connection string
	connParts := []string{
		"driver=bigquery",
		fmt.Sprintf("adbc.bigquery.sql.project_id=%s", projectID),
	}

	query := u.Query()

	// Handle credentials - BigQuery ADBC driver supports direct file path and JSON string
	if credPath := query.Get("credentials_path"); credPath != "" {
		// Use json_credential_file auth type with file path directly
		d.credPath = credPath
		connParts = append(connParts, "adbc.bigquery.sql.auth_type=adbc.bigquery.sql.auth_type.json_credential_file")
		connParts = append(connParts, fmt.Sprintf("adbc.bigquery.sql.auth_credentials=%s", credPath))
	} else if credBase64 := query.Get("credentials_base64"); credBase64 != "" {
		// Decode and pass as JSON string directly (no temp file needed)
		credContent, err := base64.StdEncoding.DecodeString(credBase64)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64 credentials: %w", err)
		}
		d.credJSON = string(credContent)
		connParts = append(connParts, "adbc.bigquery.sql.auth_type=adbc.bigquery.sql.auth_type.json_credential_string")
		connParts = append(connParts, fmt.Sprintf("adbc.bigquery.sql.auth_credentials=%s", string(credContent)))
	} else {
		// If no credentials specified, use Application Default Credentials (ADC)
		// The auth_bigquery auth type uses the default authentication method from Google Cloud SDK
		connParts = append(connParts, "adbc.bigquery.sql.auth_type=adbc.bigquery.sql.auth_type.auth_bigquery")
	}

	// Handle location parameter
	if location := query.Get("location"); location != "" {
		d.location = location
		connParts = append(connParts, fmt.Sprintf("adbc.bigquery.sql.location=%s", location))
	}

	return strings.Join(connParts, ";"), nil
}

func (d *Dialect) DefaultSchema() string {
	// BigQuery doesn't have schemas; datasets are the equivalent
	// Dataset must be specified in the table name (dataset.table)
	return ""
}

func (d *Dialect) ParseTableName(table string) (string, string) {
	_, schemaName, tableName := d.ParseTableNameWithCatalog(table)
	return schemaName, tableName
}

// ParseTableNameWithCatalog implements adbc.CatalogAwareDialect. BigQuery
// tables live in a project.dataset.table namespace; the project defaults to
// the connection project from the URI.
func (d *Dialect) ParseTableNameWithCatalog(table string) (string, string, string) {
	tn, err := tablename.BigQuery.Parse(table, tablename.Defaults{Catalog: d.projectID})
	if err != nil {
		// Best-effort fallback; ValidateTableName surfaces the real error.
		parts := strings.SplitN(table, ".", 2)
		if len(parts) == 2 {
			return d.projectID, parts[0], parts[1]
		}
		return d.projectID, "", table
	}
	return tn.Catalog, tn.Schema, tn.Table
}

func (d *Dialect) SchemaQuery() string {
	// BigQuery requires project.dataset in INFORMATION_SCHEMA path
	// This is a template - actual dataset will be substituted by SchemaQueryForDataset
	return ""
}

func (d *Dialect) SchemaQueryForDataset(dataset string) string {
	return fmt.Sprintf(`
		SELECT
			column_name,
			data_type,
			is_nullable
		FROM %s.%s.INFORMATION_SCHEMA.COLUMNS
		WHERE table_name = ?
		ORDER BY ordinal_position
	`, d.quoteIdentifier(d.projectID), d.quoteIdentifier(dataset))
}

func (d *Dialect) PrimaryKeyQuery() string {
	// BigQuery PK constraints query - template
	return ""
}

func (d *Dialect) PrimaryKeyQueryForDataset(dataset string) string {
	return fmt.Sprintf(`
		SELECT kcu.column_name
		FROM %s.%s.INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		JOIN %s.%s.INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
			ON tc.constraint_name = kcu.constraint_name
		WHERE tc.constraint_type = 'PRIMARY KEY'
			AND tc.table_name = ?
		ORDER BY kcu.ordinal_position
	`, d.quoteIdentifier(d.projectID), d.quoteIdentifier(dataset),
		d.quoteIdentifier(d.projectID), d.quoteIdentifier(dataset))
}

func (d *Dialect) ValidateTableName(table string) error {
	return tablename.BigQuery.CheckName(table)
}

func (d *Dialect) quoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

func (d *Dialect) MapDataType(dbType string) (schema.DataType, int, int, schema.DataType) {
	return MapBigQueryToDataType(dbType)
}

func (d *Dialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

func (d *Dialect) ParsePrimaryKeyResult(rawValue interface{}) []string {
	if rawValue == nil {
		return nil
	}
	// BigQuery PK query returns individual column names (one per row)
	if pkStr, ok := rawValue.(string); ok && pkStr != "" {
		return []string{strings.TrimSpace(pkStr)}
	}
	return nil
}

// BuildConnectionStringWithDataset implements the adbc.DatasetConnector interface.
// BigQuery requires the dataset_id to be set in the connection string for queries.
func (d *Dialect) BuildConnectionStringWithDataset(uri string, dataset string) (string, error) {
	// Get the base connection string
	connStr, err := d.BuildConnectionString(uri)
	if err != nil {
		return "", err
	}

	// Add the dataset_id parameter
	connStr = connStr + fmt.Sprintf(";adbc.bigquery.sql.dataset_id=%s", dataset)
	return connStr, nil
}

// GetSchema implements the adbc.SchemaProvider interface.
// It uses the BigQuery Go SDK to fetch table metadata directly, which is much faster
// than querying INFORMATION_SCHEMA.
func (d *Dialect) GetSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	// Validate table name format
	if err := d.ValidateTableName(table); err != nil {
		return nil, err
	}

	project, dataset, tableName := d.ParseTableNameWithCatalog(table)

	// Create BigQuery client with appropriate credentials
	var client *bigquery.Client
	var err error
	var options []option.ClientOption

	if d.credPath != "" {
		options = append(options, option.WithAuthCredentialsFile(option.ServiceAccount, d.credPath))
	} else if d.credJSON != "" {
		options = append(options, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(d.credJSON)))
	}

	// The client uses the connection (billing) project; the table is read from
	// its own project, which may differ for a three-part name.
	client, err = bigquery.NewClient(ctx, d.projectID, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create BigQuery client: %w", err)
	}
	defer func() { _ = client.Close() }()

	// Get table metadata
	tableRef := client.DatasetInProject(project, dataset).Table(tableName)
	metadata, err := tableRef.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get table metadata: %w", err)
	}

	// Convert BigQuery schema to our schema format
	columns := make([]schema.Column, 0, len(metadata.Schema))
	for _, field := range metadata.Schema {
		dt, precision, scale, arrayType := mapBigQueryFieldToDataType(field)

		columns = append(columns, schema.Column{
			Name:      field.Name,
			DataType:  dt,
			Nullable:  !field.Required,
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		})
	}

	// Extract primary keys from table constraints
	var primaryKeys []string
	if metadata.TableConstraints != nil && metadata.TableConstraints.PrimaryKey != nil {
		primaryKeys = metadata.TableConstraints.PrimaryKey.Columns
	}

	config.Debug("[%s] Detected primary keys: %v", d.Name(), primaryKeys)

	// Mark primary key columns
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
		Schema:      dataset,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

// mapBigQueryFieldToDataType maps a BigQuery field schema to our internal data types.
func mapBigQueryFieldToDataType(field *bigquery.FieldSchema) (schema.DataType, int, int, schema.DataType) {
	switch field.Type {
	case bigquery.BooleanFieldType:
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown

	case bigquery.IntegerFieldType:
		return schema.TypeInt64, 0, 0, schema.TypeUnknown

	case bigquery.FloatFieldType:
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown

	case bigquery.NumericFieldType:
		return schema.TypeDecimal, int(field.Precision), int(field.Scale), schema.TypeUnknown

	case bigquery.BigNumericFieldType:
		precision, scale := int(field.Precision), int(field.Scale)
		if precision == 0 {
			precision, scale = 76, 38
		}
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown

	case bigquery.StringFieldType:
		return schema.TypeString, 0, 0, schema.TypeUnknown

	case bigquery.BytesFieldType:
		return schema.TypeBinary, 0, 0, schema.TypeUnknown

	case bigquery.DateFieldType:
		return schema.TypeDate, 0, 0, schema.TypeUnknown

	case bigquery.TimeFieldType:
		return schema.TypeTime, 0, 0, schema.TypeUnknown

	case bigquery.DateTimeFieldType:
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown

	case bigquery.TimestampFieldType:
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown

	case bigquery.RecordFieldType:
		// STRUCT/RECORD types are mapped to JSON
		return schema.TypeJSON, 0, 0, schema.TypeUnknown

	case bigquery.GeographyFieldType:
		return schema.TypeString, 0, 0, schema.TypeUnknown

	case bigquery.JSONFieldType:
		return schema.TypeJSON, 0, 0, schema.TypeUnknown

	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
