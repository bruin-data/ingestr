package spanner

import (
	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func MapSpannerCodeToDataType(colType *sppb.Type) (schema.DataType, int, int, schema.DataType) {
	if colType == nil {
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}

	switch colType.Code {
	case sppb.TypeCode_BOOL:
		return schema.TypeBoolean, 0, 0, schema.TypeUnknown
	case sppb.TypeCode_INT64:
		return schema.TypeInt64, 0, 0, schema.TypeUnknown
	case sppb.TypeCode_FLOAT32:
		return schema.TypeFloat32, 0, 0, schema.TypeUnknown
	case sppb.TypeCode_FLOAT64:
		return schema.TypeFloat64, 0, 0, schema.TypeUnknown
	case sppb.TypeCode_NUMERIC:
		return schema.TypeDecimal, 38, 9, schema.TypeUnknown
	case sppb.TypeCode_STRING:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	case sppb.TypeCode_BYTES:
		return schema.TypeBinary, 0, 0, schema.TypeUnknown
	case sppb.TypeCode_DATE:
		return schema.TypeDate, 0, 0, schema.TypeUnknown
	case sppb.TypeCode_TIMESTAMP:
		return schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown
	case sppb.TypeCode_JSON:
		return schema.TypeJSON, 0, 0, schema.TypeUnknown
	case sppb.TypeCode_ARRAY:
		if colType.ArrayElementType != nil {
			elemType, _, _, _ := MapSpannerCodeToDataType(colType.ArrayElementType)
			return schema.TypeArray, 0, 0, elemType
		}
		return schema.TypeArray, 0, 0, schema.TypeString
	default:
		return schema.TypeString, 0, 0, schema.TypeUnknown
	}
}
