package bigquery

import (
	"testing"

	cloudbigquery "cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/stretchr/testify/require"
)

func TestMapBigQueryFieldDecimalDefaults(t *testing.T) {
	tests := []struct {
		name               string
		field              *cloudbigquery.FieldSchema
		precision, scale   int
		wantValidationFail bool
	}{
		{
			name:      "unconstrained bignumeric uses effective bounds",
			field:     &cloudbigquery.FieldSchema{Type: cloudbigquery.BigNumericFieldType},
			precision: 76, scale: 38, wantValidationFail: true,
		},
		{
			name:      "constrained bignumeric preserves metadata",
			field:     &cloudbigquery.FieldSchema{Type: cloudbigquery.BigNumericFieldType, Precision: 50, Scale: 12},
			precision: 50, scale: 12, wantValidationFail: true,
		},
		{
			name:      "numeric mapping remains unchanged",
			field:     &cloudbigquery.FieldSchema{Type: cloudbigquery.NumericFieldType},
			precision: 0, scale: 0,
		},
		{
			name:      "constrained numeric remains supported",
			field:     &cloudbigquery.FieldSchema{Type: cloudbigquery.NumericFieldType, Precision: 38, Scale: 9},
			precision: 38, scale: 9,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataType, precision, scale, arrayType := mapBigQueryFieldToDataType(tt.field)
			require.Equal(t, schema.TypeDecimal, dataType)
			require.Equal(t, tt.precision, precision)
			require.Equal(t, tt.scale, scale)
			require.Equal(t, schema.TypeUnknown, arrayType)

			err := schemainfer.ValidateSchema(&schema.TableSchema{Columns: []schema.Column{{
				Name: "amount", DataType: dataType, Precision: precision, Scale: scale,
			}}})
			if tt.wantValidationFail {
				require.ErrorContains(t, err, "maximum supported precision is 38")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
