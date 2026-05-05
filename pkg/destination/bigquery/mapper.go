package bigquery

import (
	"fmt"
	"slices"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/gong/pkg/schema"
)

// MapDataTypeToBigQuery maps internal DataType to BigQuery FieldType.
func MapDataTypeToBigQuery(col schema.Column) bigquery.FieldType {
	switch col.DataType {
	case schema.TypeBoolean:
		return bigquery.BooleanFieldType

	case schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return bigquery.IntegerFieldType

	case schema.TypeFloat32, schema.TypeFloat64:
		return bigquery.FloatFieldType

	case schema.TypeDecimal:
		// BigQuery has NUMERIC (38,9) and BIGNUMERIC (76,38)
		if col.Precision > 38 {
			return bigquery.BigNumericFieldType
		}
		return bigquery.NumericFieldType

	case schema.TypeString, schema.TypeUUID:
		return bigquery.StringFieldType

	case schema.TypeBinary:
		return bigquery.BytesFieldType

	case schema.TypeDate:
		return bigquery.DateFieldType

	case schema.TypeTime:
		return bigquery.TimeFieldType

	case schema.TypeTimestamp:
		return bigquery.TimestampFieldType

	case schema.TypeTimestampTZ:
		// TIMESTAMP in BigQuery (always UTC)
		return bigquery.TimestampFieldType

	case schema.TypeJSON:
		return bigquery.JSONFieldType

	case schema.TypeArray:
		// Array types handled separately - this is just the marker
		return bigquery.StringFieldType

	default:
		// Default to STRING for unknown types
		return bigquery.StringFieldType
	}
}

func BuildBigQuerySchema(tableSchema *schema.TableSchema) bigquery.Schema {
	fields := make([]*bigquery.FieldSchema, 0, len(tableSchema.Columns))

	for _, col := range tableSchema.Columns {
		field := &bigquery.FieldSchema{
			Name:     col.Name,
			Type:     MapDataTypeToBigQuery(col),
			Required: !col.Nullable,
		}

		applyBigQueryDecimalPrecisionScale(field, col)

		if col.DataType == schema.TypeArray && col.ArrayType != schema.TypeUnknown {
			elemField := schema.Column{
				DataType:  col.ArrayType,
				Precision: col.Precision,
				Scale:     col.Scale,
			}
			field.Type = MapDataTypeToBigQuery(elemField)
			field.Repeated = true
		}

		fields = append(fields, field)
	}

	return fields
}

func BuildTableMetadata(tableSchema *schema.TableSchema, primaryKeys []string, location string, partitionBy string, clusterBy []string, expiresAfter time.Duration) *bigquery.TableMetadata {
	metadata := &bigquery.TableMetadata{
		Schema: BuildBigQuerySchema(tableSchema),
	}

	if location != "" {
		metadata.Location = location
	}

	if len(primaryKeys) > 0 {
		metadata.TableConstraints = &bigquery.TableConstraints{
			PrimaryKey: &bigquery.PrimaryKey{
				Columns: primaryKeys,
			},
		}
	}

	if partitionBy != "" {
		metadata.TimePartitioning = &bigquery.TimePartitioning{
			Field: partitionBy,
		}
	}

	if len(clusterBy) > 0 {
		metadata.Clustering = &bigquery.Clustering{
			Fields: clusterBy,
		}
	}

	if expiresAfter > 0 {
		metadata.ExpirationTime = time.Now().UTC().Add(expiresAfter)
	}

	return metadata
}

// ParseTableName splits a BigQuery table name into dataset and table.
// BigQuery requires dataset.table format.
func ParseTableName(tableName string) (dataset string, table string, err error) {
	// BigQuery table names must be in format: dataset.table
	// We don't support project.dataset.table in this implementation
	parts := splitTableName(tableName)

	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}

	if len(parts) <= 1 {
		return "", "", fmt.Errorf("BigQuery table name must include dataset (format: dataset.table), got: %s", tableName)
	}

	return "", "", fmt.Errorf("invalid BigQuery table name format: %s (expected: dataset.table)", tableName)
}

// splitTableName splits a table name by periods
func splitTableName(name string) []string {
	result := []string{}
	current := ""
	inBacktick := false

	for _, r := range name {
		if r == '`' {
			inBacktick = !inBacktick
			continue
		}
		if r == '.' && !inBacktick {
			if current != "" {
				result = append(result, current)
				current = ""
			}
			continue
		}
		current += string(r)
	}

	if current != "" {
		result = append(result, current)
	}

	return result
}

// makeNonPKColumnsNullable returns a copy of the schema where all non-primary-key
// columns are marked as nullable. This is needed for CDC staging tables because
// DELETE events only contain key column values — non-key columns are NULL.
func makeNonPKColumnsNullable(s *schema.TableSchema, primaryKeys []string) *schema.TableSchema {
	cp := *s
	cp.Columns = make([]schema.Column, len(s.Columns))
	for i, col := range s.Columns {
		cp.Columns[i] = col
		if !slices.Contains(primaryKeys, col.Name) {
			cp.Columns[i].Nullable = true
		}
	}
	return &cp
}
