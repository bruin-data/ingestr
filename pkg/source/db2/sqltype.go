package db2

import "github.com/bruin-data/ingestr/pkg/schema"

func MapDb2SQLTypeToDataType(sqlType int, length int, precision int, scale int) (schema.DataType, int, int, schema.DataType) {
	switch sqlType {
	case db2SQLTypeBoolean, db2SQLTypeNBoolean:
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown
	case db2SQLTypeSmall, db2SQLTypeNSmall:
		return schema.TypeInt16, 0, 0, schema.TypeUnknown
	case db2SQLTypeInteger, db2SQLTypeNInteger:
		return schema.TypeInt32, 0, 0, schema.TypeUnknown
	case db2SQLTypeBigInt, db2SQLTypeNBigInt:
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case db2SQLTypeFloat, db2SQLTypeNFloat:
		if length == 4 {
			return schema.TypeFloat32, 0, 0, schema.TypeUnknown
		}
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case db2SQLTypeDecimal, db2SQLTypeNDecimal, db2SQLTypeNumeric, db2SQLTypeNNumeric:
		return schema.TypeDecimal, precision, scale, schema.TypeUnknown
	case db2SQLTypeDecFloat, db2SQLTypeNDecFloat:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case db2SQLTypeVarchar, db2SQLTypeNVarchar, db2SQLTypeChar, db2SQLTypeNChar,
		db2SQLTypeLong, db2SQLTypeNLong, db2SQLTypeGraphic, db2SQLTypeNGraphic,
		db2SQLTypeVarGraph, db2SQLTypeNVarGraph, db2SQLTypeClob, db2SQLTypeNClob:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case db2SQLTypeBlob, db2SQLTypeNBlob, db2SQLTypeRowID, db2SQLTypeNRowID:
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case db2SQLTypeDate, db2SQLTypeNDate:
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case db2SQLTypeTime, db2SQLTypeNTime:
		return schema.TypeTime, 0, 0, schema.TypeUnknown
	case db2SQLTypeTimestamp, db2SQLTypeNTimestamp:
		return schema.TypeTimestamp, 0, 0, schema.TypeUnknown
	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}

func nullableSQLType(sqlType int) bool {
	return sqlType%2 == 1
}
