package databricks

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// databricksMaxVarcharLength is the maximum length Databricks allows for a
// bounded VARCHAR(n). A longer requested length is rejected at table creation.
const databricksMaxVarcharLength = 65535

func MapDataTypeToDatabricks(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt8:
		return "TINYINT"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INT"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "FLOAT"
	case schema.TypeFloat64:
		return "DOUBLE"
	case schema.TypeDecimal:
		precision := col.Precision
		scale := col.Scale
		if precision == 0 {
			precision = 38
		}
		return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
	case schema.TypeString:
		if col.MaxLength > 0 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "STRING"
	case schema.TypeBinary:
		return "BINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "STRING"
	case schema.TypeTimestamp:
		return "TIMESTAMP_NTZ"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP"
	case schema.TypeInterval:
		return "STRING"
	case schema.TypeJSON:
		return "STRING"
	case schema.TypeUUID:
		return "STRING"
	case schema.TypeArray:
		elemType := MapDataTypeToDatabricks(schema.Column{DataType: col.ArrayType})
		return fmt.Sprintf("ARRAY<%s>", elemType)
	default:
		return "STRING"
	}
}
