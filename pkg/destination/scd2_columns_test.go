package destination

import (
	"reflect"
	"testing"
)

func TestSCD2MetadataColumns(t *testing.T) {
	got := SCD2MetadataColumns()
	want := []string{"_scd_valid_from", "_scd_valid_to", "_scd_is_current"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SCD2MetadataColumns() = %v, want %v", got, want)
	}
}

// TestSCD2MetadataColumnsReturnsCopy verifies that SCD2MetadataColumns returns a new slice on each call, not a shared slice.
func TestSCD2MetadataColumnsReturnsCopy(t *testing.T) {
	a := SCD2MetadataColumns()
	a[0] = "mutated"
	b := SCD2MetadataColumns()
	if b[0] != "_scd_valid_from" {
		t.Errorf("SCD2MetadataColumns() returned a shared slice; got %q after mutation", b[0])
	}
}

func TestSCD2NonDataColumns(t *testing.T) {
	tests := []struct {
		name        string
		primaryKeys []string
		expected    []string
	}{
		{
			name:        "no primary keys",
			primaryKeys: nil,
			expected:    []string{"_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
		{
			name:        "empty primary keys",
			primaryKeys: []string{},
			expected:    []string{"_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
		{
			name:        "single primary key",
			primaryKeys: []string{"id"},
			expected:    []string{"id", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
		{
			name:        "multiple primary keys",
			primaryKeys: []string{"id", "tenant_id"},
			expected:    []string{"id", "tenant_id", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
		{
			name:        "primary key overlaps scd metadata",
			primaryKeys: []string{"_scd_valid_from"},
			expected:    []string{"_scd_valid_from", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SCD2NonDataColumns(tt.primaryKeys)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("SCD2NonDataColumns(%v) = %v, want %v", tt.primaryKeys, got, tt.expected)
			}
		})
	}
}

func TestSCD2NonDataColumnsDoesNotMutateInput(t *testing.T) {
	pks := []string{"id"}
	_ = SCD2NonDataColumns(pks)
	if !reflect.DeepEqual(pks, []string{"id"}) {
		t.Errorf("SCD2NonDataColumns mutated input slice: %v", pks)
	}
}

func TestAppendSCD2Columns(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: []string{"_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: []string{"_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
		{
			name:     "data columns only",
			input:    []string{"id", "name"},
			expected: []string{"id", "name", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
		{
			name:     "one scd column already present",
			input:    []string{"id", "_scd_valid_from"},
			expected: []string{"id", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
		{
			name:     "all scd columns already present",
			input:    []string{"id", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
			expected: []string{"id", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
		},
		{
			name:     "scd columns in different order",
			input:    []string{"_scd_is_current", "id", "_scd_valid_to"},
			expected: []string{"_scd_is_current", "id", "_scd_valid_to", "_scd_valid_from"},
		},
		{
			name:     "scd columns present but uppercased",
			input:    []string{"id", "_SCD_VALID_FROM", "_SCD_VALID_TO", "_SCD_IS_CURRENT"},
			expected: []string{"id", "_SCD_VALID_FROM", "_SCD_VALID_TO", "_SCD_IS_CURRENT"},
		},
		{
			name:     "scd columns present in mixed case",
			input:    []string{"id", "_Scd_Valid_From", "_scd_valid_to", "_SCD_IS_CURRENT"},
			expected: []string{"id", "_Scd_Valid_From", "_scd_valid_to", "_SCD_IS_CURRENT"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppendSCD2Columns(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("AppendSCD2Columns(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
