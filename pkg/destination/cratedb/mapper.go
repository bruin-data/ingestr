package cratedb

import (
	"fmt"

	"github.com/bruin-data/gong/pkg/schema"
)

func mapDataTypeToCrateDB(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32, schema.TypeFloat64:
		return "DOUBLE PRECISION"
	case schema.TypeDecimal:
		if col.Precision > 0 {
			return fmt.Sprintf("NUMERIC(%d,%d)", col.Precision, col.Scale)
		}
		return "NUMERIC"
	case schema.TypeString:
		return "TEXT"
	case schema.TypeBinary:
		return "TEXT"
	case schema.TypeDate:
		return "TIMESTAMP WITHOUT TIME ZONE"
	case schema.TypeTime:
		return "TEXT"
	case schema.TypeTimestamp:
		return "TIMESTAMP WITHOUT TIME ZONE"
	case schema.TypeTimestampTZ:
		return "TIMESTAMP WITH TIME ZONE"
	case schema.TypeInterval:
		return "TEXT"
	case schema.TypeJSON:
		return "TEXT"
	case schema.TypeUUID:
		return "TEXT"
	case schema.TypeArray:
		elemCol := schema.Column{DataType: col.ArrayType}
		return fmt.Sprintf("ARRAY(%s)", mapDataTypeToCrateDB(elemCol))
	default:
		return "TEXT"
	}
}
