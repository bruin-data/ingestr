package bigquery

import (
	"fmt"
	"slices"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// bigQueryMaxPKColumns is BigQuery's hard limit on PRIMARY KEY column count.
const bigQueryMaxPKColumns = 16

// MapDataTypeToBigQuery maps internal DataType to BigQuery FieldType.
func MapDataTypeToBigQuery(col schema.Column) bigquery.FieldType {
	switch col.DataType {
	case schema.TypeBoolean:
		return bigquery.BooleanFieldType

	case schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
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
		// BigQuery rejects PRIMARY KEY constraints with more than 16 columns.
		// The constraint is NOT ENFORCED (informational only), and MERGE SQL
		// keys on the in-memory PK list independently, so skipping it here is safe.
		if len(primaryKeys) > bigQueryMaxPKColumns {
			output.Warnf("[WARNING] BigQuery supports at most %d primary key columns; "+
				"creating table without PRIMARY KEY constraint (got %d cols)\n",
				bigQueryMaxPKColumns, len(primaryKeys))
		} else {
			metadata.TableConstraints = &bigquery.TableConstraints{
				PrimaryKey: &bigquery.PrimaryKey{
					Columns: primaryKeys,
				},
			}
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

// ParseTableName splits a BigQuery table name into its project, dataset and
// table. It accepts dataset.table or project.dataset.table; project is "" when
// omitted.
func ParseTableName(tableName string) (project, dataset, table string, err error) {
	parts := splitTableName(tableName)

	switch len(parts) {
	case 2:
		return "", parts[0], parts[1], nil
	case 3:
		return parts[0], parts[1], parts[2], nil
	default:
		if len(parts) <= 1 {
			return "", "", "", fmt.Errorf("BigQuery table name must include dataset (format: [project.]dataset.table), got: %s", tableName)
		}
		return "", "", "", fmt.Errorf("invalid BigQuery table name format: %s (expected: [project.]dataset.table)", tableName)
	}
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
