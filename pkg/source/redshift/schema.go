package redshift

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// getSchema reads a table's columns and primary key with SHOW COLUMNS and
// SHOW CONSTRAINTS rather than information_schema or pg_catalog.
//
// Redshift's information_schema leaves out tables whose owner is not a user on
// the warehouse being queried, such as tables a datashare consumer created with
// multi-warehouse writes, so the Postgres query finds no columns for them. AWS
// recommends SHOW commands for metadata discovery: they work the same for
// local, datashare and external tables.
// https://docs.aws.amazon.com/redshift/latest/mgmt/discovering-metadata-system-tables.html
func getSchema(ctx context.Context, conn *pgx.Conn, schemaName, tableName string) (*schema.TableSchema, error) {
	database, err := currentDatabase(ctx, conn)
	if err != nil {
		return nil, err
	}
	table := qualifiedTableName(database, schemaName, tableName)

	columns, err := showColumns(ctx, conn, table)
	if err != nil {
		return nil, err
	}
	primaryKeys, err := showPrimaryKeys(ctx, conn, table)
	if err != nil {
		return nil, err
	}

	return buildTableSchema(schemaName, tableName, columns, primaryKeys)
}

// showColumn is one row of SHOW COLUMNS, reduced to the fields a schema needs.
type showColumn struct {
	name             string
	ordinalPosition  int
	dataType         string
	isNullable       string
	charMaxLength    *int
	numericPrecision *int
	numericScale     *int
}

// showPrimaryKey is one row of SHOW CONSTRAINTS PRIMARY KEYS.
type showPrimaryKey struct {
	columnName string
	keySeq     int
}

func currentDatabase(ctx context.Context, conn *pgx.Conn) (string, error) {
	var database string
	if err := conn.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil {
		return "", fmt.Errorf("failed to get current database: %w", err)
	}
	return database, nil
}

func showColumns(ctx context.Context, conn *pgx.Conn, table string) ([]showColumn, error) {
	rows, err := queryShow(ctx, conn, "SHOW COLUMNS FROM TABLE "+table)
	if err != nil {
		return nil, fmt.Errorf("failed to show columns of %s: %w", table, err)
	}

	columns := make([]showColumn, 0, len(rows))
	for _, row := range rows {
		column, err := parseShowColumn(row)
		if err != nil {
			return nil, fmt.Errorf("failed to parse columns of %s: %w", table, err)
		}
		columns = append(columns, column)
	}
	return columns, nil
}

func showPrimaryKeys(ctx context.Context, conn *pgx.Conn, table string) ([]showPrimaryKey, error) {
	rows, err := queryShow(ctx, conn, "SHOW CONSTRAINTS PRIMARY KEYS FROM TABLE "+table)
	if err != nil {
		return nil, fmt.Errorf("failed to show primary keys of %s: %w", table, err)
	}

	primaryKeys := make([]showPrimaryKey, 0, len(rows))
	for _, row := range rows {
		primaryKey, err := parseShowPrimaryKey(row)
		if err != nil {
			return nil, fmt.Errorf("failed to parse primary keys of %s: %w", table, err)
		}
		primaryKeys = append(primaryKeys, primaryKey)
	}
	return primaryKeys, nil
}

// queryShow runs a SHOW command and returns each row keyed by column name.
//
// SHOW takes no bind parameters, so it is sent as a plain simple-protocol
// query, which returns every value as text. pgx's QueryExecModeSimpleProtocol
// can't be used: it refuses to run unless standard_conforming_strings is on,
// and Redshift turns it off. Rows are keyed by name because AWS adds output
// columns to SHOW commands over time. A NULL value is a nil pointer.
func queryShow(ctx context.Context, conn *pgx.Conn, sql string) ([]map[string]*string, error) {
	results, err := conn.PgConn().Exec(ctx, sql).ReadAll()
	if err != nil {
		return nil, err
	}

	var rows []map[string]*string
	for _, result := range results {
		for _, values := range result.Rows {
			rows = append(rows, rawRowByName(result.FieldDescriptions, values))
		}
	}
	return rows, nil
}

func rawRowByName(fields []pgconn.FieldDescription, values [][]byte) map[string]*string {
	row := make(map[string]*string, len(fields))
	for i, field := range fields {
		if values[i] == nil {
			row[field.Name] = nil
			continue
		}
		value := string(values[i])
		row[field.Name] = &value
	}
	return row
}

func parseShowColumn(row map[string]*string) (showColumn, error) {
	name, err := requiredString(row, "column_name")
	if err != nil {
		return showColumn{}, err
	}
	dataType, err := requiredString(row, "data_type")
	if err != nil {
		return showColumn{}, err
	}
	ordinalPosition, err := requiredInt(row, "ordinal_position")
	if err != nil {
		return showColumn{}, err
	}
	charMaxLength, err := optionalInt(row, "character_maximum_length")
	if err != nil {
		return showColumn{}, err
	}
	numericPrecision, err := optionalInt(row, "numeric_precision")
	if err != nil {
		return showColumn{}, err
	}
	numericScale, err := optionalInt(row, "numeric_scale")
	if err != nil {
		return showColumn{}, err
	}

	return showColumn{
		name:             name,
		ordinalPosition:  ordinalPosition,
		dataType:         dataType,
		isNullable:       stringOrEmpty(row["is_nullable"]),
		charMaxLength:    charMaxLength,
		numericPrecision: numericPrecision,
		numericScale:     numericScale,
	}, nil
}

func parseShowPrimaryKey(row map[string]*string) (showPrimaryKey, error) {
	columnName, err := requiredString(row, "column_name")
	if err != nil {
		return showPrimaryKey{}, err
	}
	keySeq, err := requiredInt(row, "key_seq")
	if err != nil {
		return showPrimaryKey{}, err
	}
	return showPrimaryKey{columnName: columnName, keySeq: keySeq}, nil
}

// buildTableSchema maps SHOW output onto a TableSchema, with the same type
// mapping as the Postgres source.
func buildTableSchema(schemaName, tableName string, columns []showColumn, primaryKeys []showPrimaryKey) (*schema.TableSchema, error) {
	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s.%s not found or has no columns", schemaName, tableName)
	}

	sort.Slice(columns, func(i, j int) bool {
		return columns[i].ordinalPosition < columns[j].ordinalPosition
	})
	sort.Slice(primaryKeys, func(i, j int) bool {
		return primaryKeys[i].keySeq < primaryKeys[j].keySeq
	})

	isPrimaryKey := make(map[string]bool, len(primaryKeys))
	primaryKeyNames := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		isPrimaryKey[pk.columnName] = true
		primaryKeyNames[i] = pk.columnName
	}

	schemaColumns := make([]schema.Column, len(columns))
	for i, column := range columns {
		schemaColumns[i] = toSchemaColumn(column, isPrimaryKey[column.name])
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     schemaColumns,
		PrimaryKeys: primaryKeyNames,
	}, nil
}

func toSchemaColumn(column showColumn, isPrimaryKey bool) schema.Column {
	dataType, precision, scale, arrayType := postgres.MapPostgresToDataType(column.dataType)

	result := schema.Column{
		Name:         column.name,
		DataType:     dataType,
		Nullable:     column.isNullable == "YES",
		ArrayType:    arrayType,
		IsPrimaryKey: isPrimaryKey,
		Precision:    precision,
		Scale:        scale,
	}
	if column.numericPrecision != nil {
		result.Precision = *column.numericPrecision
	}
	if column.numericScale != nil {
		result.Scale = *column.numericScale
	}
	if column.charMaxLength != nil {
		result.MaxLength = *column.charMaxLength
	}
	return result
}

// qualifiedTableName quotes database.schema.table for a SHOW command, which
// cannot take the names as bind parameters.
func qualifiedTableName(database, schemaName, tableName string) string {
	return quoteIdentifier(database) + "." + quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)
}

// quoteIdentifier matches the Postgres source: a name that is already quoted
// is used as it is.
func quoteIdentifier(name string) string {
	if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func requiredString(row map[string]*string, key string) (string, error) {
	value := row[key]
	if value == nil {
		return "", fmt.Errorf("%s is missing", key)
	}
	return *value, nil
}

func requiredInt(row map[string]*string, key string) (int, error) {
	value, err := optionalInt(row, key)
	if err != nil {
		return 0, err
	}
	if value == nil {
		return 0, fmt.Errorf("%s is missing", key)
	}
	return *value, nil
}

func optionalInt(row map[string]*string, key string) (*int, error) {
	value := row[key]
	if value == nil || *value == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(*value)
	if err != nil {
		return nil, fmt.Errorf("%s is not an integer: %q", key, *value)
	}
	return &parsed, nil
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
