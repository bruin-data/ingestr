package iceberg

import (
	"fmt"
	"strings"

	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/schema"
)

var reservedIcebergMetadataColumns = map[string]struct{}{
	"_file":                         {},
	"_pos":                          {},
	"_deleted":                      {},
	"_spec_id":                      {},
	"_partition":                    {},
	"_row_id":                       {},
	"_last_updated_sequence_number": {},
}

func validateIsolatedTableFilePaths(props iceberggo.Properties) error {
	for _, key := range []string{icebergtable.WriteDataPathKey, icebergtable.WriteMetadataPathKey} {
		if _, configured := props[key]; configured {
			return fmt.Errorf("table property %q is unsafe because files outside the table root cannot be isolated for orphan cleanup or purged reliably", key)
		}
	}
	return nil
}

func validateIcebergTableSchema(tableSchema *schema.TableSchema) error {
	if tableSchema == nil {
		return fmt.Errorf("iceberg: schema is required")
	}

	seen := make(map[string]string, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		if err := validateIcebergColumnName(col.Name); err != nil {
			return err
		}
		normalized := strings.ToLower(col.Name)
		if previous, ok := seen[normalized]; ok {
			return fmt.Errorf("iceberg: columns %q and %q differ only by case; use unique names for cross-engine compatibility", previous, col.Name)
		}
		seen[normalized] = col.Name
		if err := validateIcebergNestedColumn(col, col.Name); err != nil {
			return err
		}
	}
	return nil
}

func validateIcebergNestedColumn(col schema.Column, path string) error {
	switch col.DataType {
	case schema.TypeFixedBinary:
		if col.FixedLength <= 0 {
			return fmt.Errorf("iceberg: fixed binary column %q requires a positive length", path)
		}
	case schema.TypeArray:
		if col.ArrayLength > 0 {
			return fmt.Errorf("iceberg: fixed-size list column %q with length %d is unsupported because Iceberg list types do not preserve cardinality", path, col.ArrayLength)
		}
		if col.Element != nil {
			return validateIcebergNestedColumn(*col.Element, path+".element")
		}
	case schema.TypeStruct:
		if col.StructFields == nil || len(col.StructFields.Columns) == 0 {
			return fmt.Errorf("iceberg: struct column %q requires at least one field", path)
		}
		seen := make(map[string]struct{}, len(col.StructFields.Columns))
		for _, field := range col.StructFields.Columns {
			if err := validateIcebergColumnName(field.Name); err != nil {
				return err
			}
			name := strings.ToLower(field.Name)
			if _, ok := seen[name]; ok {
				return fmt.Errorf("iceberg: duplicate nested field %q", path+"."+field.Name)
			}
			seen[name] = struct{}{}
			if err := validateIcebergNestedColumn(field, path+"."+field.Name); err != nil {
				return err
			}
		}
	case schema.TypeMap:
		if col.MapKey == nil || col.MapValue == nil {
			return fmt.Errorf("iceberg: map column %q requires key and value types", path)
		}
		if col.MapKey.Nullable {
			return fmt.Errorf("iceberg: map key %q cannot be nullable", path)
		}
		if err := validateIcebergNestedColumn(*col.MapKey, path+".key"); err != nil {
			return err
		}
		return validateIcebergNestedColumn(*col.MapValue, path+".value")
	}
	return nil
}

func validateIcebergColumnName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("iceberg: column name is required")
	}
	normalized := strings.ToLower(name)
	if _, reserved := reservedIcebergMetadataColumns[normalized]; reserved {
		return fmt.Errorf("iceberg: column %q conflicts with reserved metadata column %q; rename it at the source because automatic remapping would not be reversible", name, normalized)
	}
	return nil
}
