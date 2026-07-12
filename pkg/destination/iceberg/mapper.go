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
	if err := validateIcebergTableSchema(s); err != nil {
		return nil, err
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

func icebergWriteArrowSchema(s *schema.TableSchema, tableSchema *iceberggo.Schema) *arrow.Schema {
	arrowSchema := icebergArrowSchema(s)
	if tableSchema == nil {
		return arrowSchema
	}
	fields := arrowSchema.Fields()
	for i := range fields {
		if field, ok := tableSchema.FindFieldByName(fields[i].Name); ok {
			fields[i].Nullable = !field.Required
			fields[i].Type = overlayIcebergRequiredness(fields[i].Type, field.Type)
		}
	}
	metadata := arrowSchema.Metadata()
	return arrow.NewSchema(fields, &metadata)
}

func overlayIcebergRequiredness(dataType arrow.DataType, iceType iceberggo.Type) arrow.DataType {
	switch typed := dataType.(type) {
	case *arrow.StructType:
		iceStruct, ok := iceType.(*iceberggo.StructType)
		if !ok {
			return dataType
		}
		fields := typed.Fields()
		for i := range fields {
			for _, iceField := range iceStruct.FieldList {
				if iceField.Name == fields[i].Name {
					fields[i].Nullable = !iceField.Required
					fields[i].Type = overlayIcebergRequiredness(fields[i].Type, iceField.Type)
					break
				}
			}
		}
		return arrow.StructOf(fields...)
	case *arrow.ListType:
		iceList, ok := iceType.(*iceberggo.ListType)
		if !ok {
			return dataType
		}
		elem := typed.ElemField()
		elem.Nullable = !iceList.ElementRequired
		elem.Type = overlayIcebergRequiredness(elem.Type, iceList.Element)
		return arrow.ListOfField(elem)
	case *arrow.MapType:
		iceMap, ok := iceType.(*iceberggo.MapType)
		if !ok {
			return dataType
		}
		key := typed.KeyField()
		key.Nullable = false
		key.Type = overlayIcebergRequiredness(key.Type, iceMap.KeyType)
		value := typed.ItemField()
		value.Nullable = !iceMap.ValueRequired
		value.Type = overlayIcebergRequiredness(value.Type, iceMap.ValueType)
		return arrow.MapOfFields(key, value)
	default:
		return dataType
	}
}

func icebergArrowType(col schema.Column) arrow.DataType {
	switch col.DataType {
	case schema.TypeJSON, schema.TypeUnknown:
		return arrow.BinaryTypes.String
	case schema.TypeBinary:
		return arrow.BinaryTypes.Binary
	case schema.TypeUUID:
		return extensions.NewUUIDType()
	case schema.TypeArray:
		elem := col.Element
		if elem == nil {
			elem = &schema.Column{DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale, MaxLength: col.MaxLength, FixedLength: col.FixedLength, Nullable: true}
		}
		return arrow.ListOfField(arrow.Field{Name: "element", Type: icebergArrowType(*elem), Nullable: elem.Nullable})
	case schema.TypeStruct:
		if col.StructFields == nil {
			return arrow.StructOf()
		}
		fields := make([]arrow.Field, len(col.StructFields.Columns))
		for i, field := range col.StructFields.Columns {
			fields[i] = arrow.Field{Name: field.Name, Type: icebergArrowType(field), Nullable: field.Nullable}
		}
		return arrow.StructOf(fields...)
	case schema.TypeMap:
		key := schema.Column{DataType: schema.TypeString, Nullable: false}
		if col.MapKey != nil {
			key = *col.MapKey
		}
		value := schema.Column{DataType: schema.TypeUnknown, Nullable: true}
		if col.MapValue != nil {
			value = *col.MapValue
		}
		return arrow.MapOfFields(
			arrow.Field{Name: "key", Type: icebergArrowType(key), Nullable: false},
			arrow.Field{Name: "value", Type: icebergArrowType(value), Nullable: value.Nullable},
		)
	default:
		dt := schema.DataTypeToArrowType(col)
		if ext, ok := dt.(arrow.ExtensionType); ok {
			return ext.StorageType()
		}
		return dt
	}
}

func icebergTypeForColumn(col schema.Column) (iceberggo.Type, error) {
	arrowSchema := arrow.NewSchema([]arrow.Field{{Name: col.Name, Type: icebergArrowType(col), Nullable: col.Nullable}}, nil)
	iceSchema, err := icebergtable.ArrowSchemaToIcebergWithFreshIDs(arrowSchema, false)
	if err != nil {
		return nil, err
	}
	field, ok := iceSchema.FindFieldByName(col.Name)
	if !ok {
		return nil, fmt.Errorf("converted Iceberg field %q not found", col.Name)
	}
	return field.Type, nil
}

func icebergTypesEquivalent(left, right iceberggo.Type) bool {
	switch l := left.(type) {
	case *iceberggo.ListType:
		r, ok := right.(*iceberggo.ListType)
		return ok && l.ElementRequired == r.ElementRequired && icebergTypesEquivalent(l.Element, r.Element)
	case *iceberggo.MapType:
		r, ok := right.(*iceberggo.MapType)
		return ok && l.ValueRequired == r.ValueRequired &&
			icebergTypesEquivalent(l.KeyType, r.KeyType) && icebergTypesEquivalent(l.ValueType, r.ValueType)
	case *iceberggo.StructType:
		r, ok := right.(*iceberggo.StructType)
		if !ok || len(l.FieldList) != len(r.FieldList) {
			return false
		}
		for i := range l.FieldList {
			lf, rf := l.FieldList[i], r.FieldList[i]
			if lf.Name != rf.Name || lf.Required != rf.Required || !icebergTypesEquivalent(lf.Type, rf.Type) {
				return false
			}
		}
		return true
	default:
		return left.Equals(right)
	}
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
	if err := applyIcebergType(&col, field.Type); err != nil {
		return schema.Column{}, fmt.Errorf("iceberg: column %q uses an unsupported type: %w", field.Name, err)
	}
	return col, nil
}

func applyIcebergType(col *schema.Column, typ iceberggo.Type) error {
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
		col.DataType = schema.TypeFixedBinary
		col.FixedLength = t.Len()
	case iceberggo.DateType:
		col.DataType = schema.TypeDate
	case iceberggo.TimeType:
		col.DataType = schema.TypeTime
	case iceberggo.TimestampType:
		col.DataType = schema.TypeTimestamp
	case iceberggo.TimestampTzType:
		col.DataType = schema.TypeTimestampTZ
	case iceberggo.TimestampNsType, iceberggo.TimestampTzNsType:
		return fmt.Errorf("nanosecond timestamps cannot be represented without precision loss; ingestr uses microseconds")
	case iceberggo.UnknownType:
		col.DataType = schema.TypeUnknown
	case iceberggo.VariantType:
		return fmt.Errorf("native Iceberg %s values cannot be represented by ingestr's flat schema; serialize or flatten the column at the source", typ.Type())
	case *iceberggo.StructType:
		fields := make([]schema.Column, 0, len(t.FieldList))
		for _, field := range t.FieldList {
			nested, err := columnFromIcebergField(field)
			if err != nil {
				return err
			}
			fields = append(fields, nested)
		}
		col.DataType = schema.TypeStruct
		col.StructFields = &schema.TableSchema{Columns: fields}
	case *iceberggo.MapType:
		key := schema.Column{Name: "key", Nullable: false}
		if err := applyIcebergType(&key, t.KeyType); err != nil {
			return fmt.Errorf("map key: %w", err)
		}
		value := schema.Column{Name: "value", Nullable: !t.ValueRequired}
		if err := applyIcebergType(&value, t.ValueType); err != nil {
			return fmt.Errorf("map value: %w", err)
		}
		col.DataType = schema.TypeMap
		col.MapKey = &key
		col.MapValue = &value
	case *iceberggo.ListType:
		elem := schema.Column{Name: "element", Nullable: !t.ElementRequired}
		if err := applyIcebergType(&elem, t.Element); err != nil {
			return fmt.Errorf("list element: %w", err)
		}
		col.DataType = schema.TypeArray
		col.ArrayType = elem.DataType
		col.Element = &elem
		col.Precision = elem.Precision
		col.Scale = elem.Scale
		col.MaxLength = elem.MaxLength
		col.FixedLength = elem.FixedLength
	default:
		return fmt.Errorf("iceberg type %s cannot be represented by ingestr", typ.Type())
	}
	return nil
}
