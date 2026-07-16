package schemaevolution

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestCompareWidensPrimitiveArrayElements(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "values", DataType: schema.TypeArray, ArrayType: schema.TypeInt64,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "Values", DataType: schema.TypeArray, ArrayType: schema.TypeInt32,
	}}}

	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	change := comparison.Changes[0]
	require.Equal(t, ChangeWidenType, change.Type)
	require.Equal(t, "Values", change.ColumnName)
	require.Equal(t, "Values", change.NewColumn.Name)
	require.Equal(t, schema.TypeArray, change.NewColumn.DataType)
	require.Equal(t, schema.TypeInt64, change.NewColumn.ArrayType)
	require.Equal(t, schema.TypeInt64, BuildFinalSchema(dest, comparison).Columns[0].ArrayType)
}

func TestCompareDoesNotNarrowPrimitiveArrayElements(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "values", DataType: schema.TypeArray, ArrayType: schema.TypeInt32,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "values", DataType: schema.TypeArray, ArrayType: schema.TypeInt64,
	}}}

	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.False(t, comparison.HasChanges)
}

func TestCompareMergesDecimalArrayElementPrecision(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amounts", DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 20, Scale: 8,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amounts", DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 18, Scale: 2,
	}}}

	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, 24, comparison.Changes[0].NewColumn.Precision)
	require.Equal(t, 8, comparison.Changes[0].NewColumn.Scale)

	source.Columns[0].Precision = 38
	source.Columns[0].Scale = 38
	dest.Columns[0].Precision = 38
	dest.Columns[0].Scale = 0
	_, err = Compare(source, dest, nil)
	require.ErrorContains(t, err, "maximum supported precision is 38")
}

func TestCompareWidensStringArrayElementLength(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "labels", DataType: schema.TypeArray, ArrayType: schema.TypeString, MaxLength: 120,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "labels", DataType: schema.TypeArray, ArrayType: schema.TypeString, MaxLength: 40,
	}}}

	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, 120, comparison.Changes[0].NewColumn.MaxLength)
}

func TestCompareRejectsUnrepresentableScalarDecimalWidening(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 38,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 0,
	}}}

	_, err := Compare(source, dest, nil)
	require.ErrorContains(t, err, "maximum supported precision is 38")
}

func TestComparePreservesDecimalIntegerDigits(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 18, Scale: 2,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 20, Scale: 8,
	}}}

	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, 24, comparison.Changes[0].NewColumn.Precision)
	require.Equal(t, 8, comparison.Changes[0].NewColumn.Scale)
}

func TestCompareDecimalWideningIncludesIntegerCapacity(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 2,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeInt64,
	}}}

	comparison, err := Compare(source, dest, nil)
	require.NoError(t, err)
	require.Len(t, comparison.Changes, 1)
	require.Equal(t, schema.TypeDecimal, comparison.Changes[0].NewColumn.DataType)
	require.Equal(t, 21, comparison.Changes[0].NewColumn.Precision)
	require.Equal(t, 2, comparison.Changes[0].NewColumn.Scale)
}

func TestCompareTreatsUnspecifiedDecimalPrecisionAsDecimal38(t *testing.T) {
	source := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal,
	}}}
	dest := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 2,
	}}}

	comparison, err := Compare(source, dest, nil)
	require.ErrorContains(t, err, "maximum supported precision is 38")
	require.Nil(t, comparison)
}

func TestCompareUsesUnboundedStringForCrossTypeWidening(t *testing.T) {
	tests := []struct {
		name   string
		source schema.Column
		dest   schema.Column
	}{
		{
			name:   "bounded string and integer",
			source: schema.Column{Name: "value", DataType: schema.TypeString, MaxLength: 5},
			dest:   schema.Column{Name: "value", DataType: schema.TypeInt64},
		},
		{
			name:   "decimal and float",
			source: schema.Column{Name: "value", DataType: schema.TypeDecimal, Precision: 18, Scale: 4},
			dest:   schema.Column{Name: "value", DataType: schema.TypeFloat64},
		},
		{
			name:   "array decimal and float",
			source: schema.Column{Name: "value", DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 18, Scale: 4},
			dest:   schema.Column{Name: "value", DataType: schema.TypeArray, ArrayType: schema.TypeFloat64},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comparison, err := Compare(
				&schema.TableSchema{Columns: []schema.Column{tt.source}},
				&schema.TableSchema{Columns: []schema.Column{tt.dest}}, nil,
			)
			require.NoError(t, err)
			require.Len(t, comparison.Changes, 1)
			newColumn := comparison.Changes[0].NewColumn
			if newColumn.DataType == schema.TypeArray {
				require.Equal(t, schema.TypeString, newColumn.ArrayType)
			} else {
				require.Equal(t, schema.TypeString, newColumn.DataType)
			}
			require.Zero(t, newColumn.MaxLength)
		})
	}
}
