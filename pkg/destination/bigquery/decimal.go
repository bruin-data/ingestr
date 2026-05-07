package bigquery

import (
	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/pkg/schema"
)

const (
	defaultBigQueryNumericPrecision    = 38
	defaultBigQueryNumericScale        = 9
	defaultBigQueryBigNumericPrecision = 76
	defaultBigQueryBigNumericScale     = 38
)

func isDefaultBigQueryNumeric(precision, scale int) bool {
	return precision == defaultBigQueryNumericPrecision && scale == defaultBigQueryNumericScale
}

func isDefaultBigQueryBigNumeric(precision, scale int) bool {
	return precision == defaultBigQueryBigNumericPrecision && scale == defaultBigQueryBigNumericScale
}

func normalizeBigQueryDecimalPrecisionScale(fieldType bigquery.FieldType, precision, scale int64) (int, int) {
	if precision == 0 && scale == 0 {
		switch fieldType {
		case bigquery.NumericFieldType:
			return defaultBigQueryNumericPrecision, defaultBigQueryNumericScale
		case bigquery.BigNumericFieldType:
			return defaultBigQueryBigNumericPrecision, defaultBigQueryBigNumericScale
		}
	}

	return int(precision), int(scale)
}

func applyBigQueryDecimalPrecisionScale(field *bigquery.FieldSchema, col schema.Column) {
	if field == nil || col.DataType != schema.TypeDecimal {
		return
	}

	if field.Type == bigquery.NumericFieldType && isDefaultBigQueryNumeric(col.Precision, col.Scale) {
		return
	}

	if field.Type == bigquery.BigNumericFieldType && isDefaultBigQueryBigNumeric(col.Precision, col.Scale) {
		return
	}

	if col.Precision > 0 {
		field.Precision = int64(col.Precision)
	}
	if col.Scale > 0 {
		field.Scale = int64(col.Scale)
	}
}
