package cassandrautil

import (
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func MapCassandraToDataType(cqlType string) (schema.DataType, int, int, schema.DataType) {
	cqlType = normalizeCQLType(cqlType)

	if inner, ok := collectionInner(cqlType, "list"); ok {
		elemType, _, _, _ := MapCassandraToDataType(inner)
		if !isSupportedArrayElement(elemType) {
			return schema.TypeJSON, 0, 0, schema.TypeUnknown
		}
		return schema.TypeArray, 0, 0, elemType
	}
	if inner, ok := collectionInner(cqlType, "set"); ok {
		elemType, _, _, _ := MapCassandraToDataType(inner)
		if !isSupportedArrayElement(elemType) {
			return schema.TypeJSON, 0, 0, schema.TypeUnknown
		}
		return schema.TypeArray, 0, 0, elemType
	}
	if strings.HasPrefix(cqlType, "map<") || strings.HasPrefix(cqlType, "tuple<") {
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	}

	switch cqlType {
	case "boolean", "bool":
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown
	case "tinyint", "smallint":
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case "int":
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case "bigint", "counter":
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case "float":
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case "double":
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case "decimal", "varint":
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown
	case "ascii", "text", "varchar", "inet":
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case "blob":
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case "date":
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case "time":
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case "timestamp":
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	case "duration":
		return schema.TypeInterval, 0, 0, schema.TypeUnknown
	case "uuid", "timeuuid":
		return schema.TypeUUID, 0, 0, schema.TypeUnknown
	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}

func MapDataTypeToCassandra(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "boolean"
	case schema.TypeInt16:
		return "smallint"
	case schema.TypeInt32:
		return "int"
	case schema.TypeInt64:
		return "bigint"
	case schema.TypeFloat32:
		return "float"
	case schema.TypeFloat64:
		return "double"
	case schema.TypeDecimal:
		return "decimal"
	case schema.TypeString, schema.TypeUnknown:
		return "text"
	case schema.TypeBinary:
		return "blob"
	case schema.TypeDate:
		return "date"
	case schema.TypeTime:
		return "time"
	case schema.TypeTimestamp, schema.TypeTimestampTZ:
		return "timestamp"
	case schema.TypeInterval:
		return "duration"
	case schema.TypeJSON:
		return "text"
	case schema.TypeUUID:
		return "uuid"
	case schema.TypeArray:
		elemType := MapDataTypeToCassandra(schema.Column{DataType: col.ArrayType})
		return "list<" + elemType + ">"
	default:
		return "text"
	}
}

func normalizeCQLType(cqlType string) string {
	cqlType = strings.ToLower(strings.TrimSpace(cqlType))
	for {
		inner, ok := collectionInner(cqlType, "frozen")
		if !ok {
			return cqlType
		}
		cqlType = strings.TrimSpace(inner)
	}
}

func collectionInner(cqlType, name string) (string, bool) {
	prefix := name + "<"
	if !strings.HasPrefix(cqlType, prefix) || !strings.HasSuffix(cqlType, ">") {
		return "", false
	}
	return strings.TrimSpace(cqlType[len(prefix) : len(cqlType)-1]), true
}

func isSupportedArrayElement(dt schema.DataType) bool {
	switch dt {
	case schema.TypeUnknown, schema.TypeJSON, schema.TypeArray:
		return false
	default:
		return true
	}
}
