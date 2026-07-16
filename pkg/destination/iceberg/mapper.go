package iceberg

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func icebergSchemaFromTableSchema(s *schema.TableSchema) (*iceberggo.Schema, error) {
	if s == nil {
		return nil, fmt.Errorf("schema is required")
	}
	arrowSchema := icebergArrowSchema(s)
	iceSchema, err := icebergtable.ArrowSchemaToIcebergWithFreshIDs(arrowSchema, false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert arrow schema to iceberg schema: %w", err)
	}
	if len(s.PrimaryKeys) == 0 {
		return iceSchema, nil
	}

	ids, err := identifierFieldIDs(iceSchema, s.PrimaryKeys)
	if err != nil {
		return nil, err
	}
	return iceberggo.NewSchemaWithIdentifiers(iceSchema.ID, ids, iceSchema.Fields()...), nil
}

func identifierFieldIDs(iceSchema *iceberggo.Schema, primaryKeys []string) ([]int, error) {
	ids := make([]int, 0, len(primaryKeys))
	seen := make(map[string]struct{}, len(primaryKeys))
	fields := make(map[string]iceberggo.NestedField, iceSchema.NumFields())
	for _, field := range iceSchema.Fields() {
		fields[field.Name] = field
	}

	for _, pk := range primaryKeys {
		if _, ok := seen[pk]; ok {
			return nil, fmt.Errorf("primary key %q is specified more than once", pk)
		}
		seen[pk] = struct{}{}

		field, ok := fields[pk]
		if !ok {
			return nil, fmt.Errorf("primary key %q is not present in schema", pk)
		}
		if err := validateIdentifierField(pk, field); err != nil {
			return nil, err
		}
		ids = append(ids, field.ID)
	}
	return ids, nil
}

func validateIdentifierField(name string, field iceberggo.NestedField) error {
	if !field.Required {
		return fmt.Errorf("primary key %q must be non-nullable", name)
	}
	if _, ok := field.Type.(iceberggo.PrimitiveType); !ok {
		return fmt.Errorf("primary key %q must be a primitive type, got %s", name, field.Type.Type())
	}
	switch field.Type.(type) {
	case iceberggo.Float32Type, iceberggo.Float64Type:
		return fmt.Errorf("primary key %q cannot use floating-point type %s", name, field.Type.Type())
	default:
		return nil
	}
}

func icebergArrowSchema(s *schema.TableSchema) *arrow.Schema {
	fields := make([]arrow.Field, len(s.Columns))
	for i, col := range s.Columns {
		fields[i] = arrow.Field{
			Name:     col.Name,
			Type:     icebergArrowType(col),
			Nullable: col.Nullable,
		}
	}
	return arrow.NewSchema(fields, nil)
}

func icebergArrowType(col schema.Column) arrow.DataType {
	switch col.DataType {
	case schema.TypeJSON, schema.TypeUnknown:
		return arrow.BinaryTypes.String
	case schema.TypeUUID:
		return extensions.NewUUIDType()
	case schema.TypeArray:
		elem := icebergArrowType(schema.Column{
			DataType:  col.ArrayType,
			Precision: col.Precision,
			Scale:     col.Scale,
		})
		return arrow.ListOf(elem)
	default:
		dt := schema.DataTypeToArrowType(col)
		if ext, ok := dt.(arrow.ExtensionType); ok {
			return ext.StorageType()
		}
		return dt
	}
}

func icebergTypeForColumn(col schema.Column) (iceberggo.Type, error) {
	if col.DataType == schema.TypeArray {
		arrowSchema := arrow.NewSchema([]arrow.Field{{
			Name: col.Name, Type: icebergArrowType(col), Nullable: col.Nullable,
		}}, nil)
		iceSchema, err := icebergtable.ArrowSchemaToIcebergWithFreshIDs(arrowSchema, false)
		if err != nil {
			return nil, err
		}
		return iceSchema.Field(0).Type, nil
	}
	return icebergtable.ArrowTypeToIceberg(icebergArrowType(col), false)
}

func tableSchemaFromIceberg(name string, iceSchema *iceberggo.Schema) (*schema.TableSchema, error) {
	if iceSchema == nil {
		return nil, nil
	}

	cols := make([]schema.Column, 0, iceSchema.NumFields())
	for _, field := range iceSchema.Fields() {
		col, err := columnFromIcebergField(field)
		if err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}

	primaryKeys := make([]string, 0, len(iceSchema.IdentifierFieldIDs))
	for _, id := range iceSchema.IdentifierFieldIDs {
		if name, ok := iceSchema.FindColumnName(id); ok {
			primaryKeys = append(primaryKeys, name)
		}
	}

	return &schema.TableSchema{
		Name:        name,
		Columns:     cols,
		PrimaryKeys: primaryKeys,
	}, nil
}

func columnFromIcebergField(field iceberggo.NestedField) (schema.Column, error) {
	col := schema.Column{
		Name:     field.Name,
		Nullable: !field.Required,
	}
	applyIcebergType(&col, field.Type)
	return col, nil
}

func applyIcebergType(col *schema.Column, typ iceberggo.Type) {
	switch t := typ.(type) {
	case iceberggo.BooleanType:
		col.DataType = schema.TypeBoolean
	case iceberggo.Int32Type:
		col.DataType = schema.TypeInt32
	case iceberggo.Int64Type:
		col.DataType = schema.TypeInt64
	case iceberggo.Float32Type:
		col.DataType = schema.TypeFloat32
	case iceberggo.Float64Type:
		col.DataType = schema.TypeFloat64
	case iceberggo.DecimalType:
		col.DataType = schema.TypeDecimal
		col.Precision = t.Precision()
		col.Scale = t.Scale()
	case iceberggo.StringType:
		col.DataType = schema.TypeString
	case iceberggo.UUIDType:
		col.DataType = schema.TypeUUID
	case iceberggo.BinaryType:
		col.DataType = schema.TypeBinary
	case iceberggo.FixedType:
		col.DataType = schema.TypeBinary
		col.MaxLength = t.Len()
	case iceberggo.DateType:
		col.DataType = schema.TypeDate
	case iceberggo.TimeType:
		col.DataType = schema.TypeTime
	case iceberggo.TimestampType, iceberggo.TimestampNsType:
		col.DataType = schema.TypeTimestamp
	case iceberggo.TimestampTzType, iceberggo.TimestampTzNsType:
		col.DataType = schema.TypeTimestampTZ
	case iceberggo.UnknownType:
		col.DataType = schema.TypeUnknown
	case iceberggo.VariantType, *iceberggo.StructType, *iceberggo.MapType:
		col.DataType = schema.TypeJSON
	case *iceberggo.ListType:
		elem := schema.Column{}
		applyIcebergType(&elem, t.Element)
		if elem.DataType == schema.TypeJSON || elem.DataType == schema.TypeArray || elem.DataType == schema.TypeUnknown {
			col.DataType = schema.TypeJSON
			return
		}
		col.DataType = schema.TypeArray
		col.ArrayType = elem.DataType
		col.Precision = elem.Precision
		col.Scale = elem.Scale
	default:
		col.DataType = schema.TypeString
	}
}
