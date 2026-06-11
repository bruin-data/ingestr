package fabric

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// MapDataTypeToFabric maps an internal schema type to a Fabric Warehouse column
// type. Fabric does not support several SQL Server types (NVARCHAR, DATETIME,
// DATETIMEOFFSET, MONEY, TINYINT, TEXT, JSON, XML), so this differs from the
// mssql mapper: strings use VARCHAR (UTF-8) and timestamps use DATETIME2.
func MapDataTypeToFabric(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "BIT"
	case schema.TypeInt8:
		return "SMALLINT"
	case schema.TypeInt16:
		return "SMALLINT"
	case schema.TypeInt32:
		return "INT"
	case schema.TypeInt64:
		return "BIGINT"
	case schema.TypeFloat32:
		return "REAL"
	case schema.TypeFloat64:
		return "FLOAT"
	case schema.TypeDecimal:
		precision := col.Precision
		scale := col.Scale
		if precision <= 0 {
			precision = 38
		}
		if scale < 0 {
			scale = 0
		}
		if precision > 38 {
			precision = 38
		}
		if scale > precision {
			scale = precision
		}
		return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
	case schema.TypeString:
		if col.MaxLength > 0 && col.MaxLength <= 8000 {
			return fmt.Sprintf("VARCHAR(%d)", col.MaxLength)
		}
		return "VARCHAR(MAX)"
	case schema.TypeBinary:
		if col.MaxLength > 0 && col.MaxLength <= 8000 {
			return fmt.Sprintf("VARBINARY(%d)", col.MaxLength)
		}
		return "VARBINARY(MAX)"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME(6)"
	case schema.TypeTimestamp:
		return "DATETIME2(6)"
	case schema.TypeTimestampTZ:
		// Fabric has no DATETIMEOFFSET; store the UTC instant as DATETIME2.
		return "DATETIME2(6)"
	case schema.TypeJSON:
		return "VARCHAR(MAX)"
	case schema.TypeUUID:
		return "UNIQUEIDENTIFIER"
	case schema.TypeArray:
		return "VARCHAR(MAX)"
	case schema.TypeInterval:
		return "VARCHAR(255)"
	default:
		return "VARCHAR(MAX)"
	}
}
