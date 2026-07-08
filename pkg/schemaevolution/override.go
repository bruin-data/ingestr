package schemaevolution

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// ColumnOverride represents a user-specified column type override.
type ColumnOverride struct {
	Name      string
	RenameTo  string
	DataType  schema.DataType
	Precision int
	Scale     int
	MaxLength int
}

// ColumnOverrides is a map of column name (lowercase) to its override.
type ColumnOverrides map[string]ColumnOverride

// Names returns all column names in the overrides.
func (c ColumnOverrides) Names() []string {
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	return names
}

// StandardTypeNames maps user-friendly type names to internal DataType.
// These are the canonical names users can specify in --columns flag.
var StandardTypeNames = map[string]schema.DataType{
	// Boolean
	"boolean": schema.TypeBoolean,
	"bool":    schema.TypeBoolean,

	// Integer types
	"int8":     schema.TypeInt8,
	"tinyint":  schema.TypeInt8,
	"int16":    schema.TypeInt16,
	"smallint": schema.TypeInt16,
	"int32":    schema.TypeInt32,
	"int":      schema.TypeInt64,
	"integer":  schema.TypeInt64,
	"int64":    schema.TypeInt64,
	"bigint":   schema.TypeInt64,
	"long":     schema.TypeInt64,

	// Floating point
	"float32": schema.TypeFloat32,
	"float":   schema.TypeFloat32,
	"real":    schema.TypeFloat32,
	"float64": schema.TypeFloat64,
	"double":  schema.TypeFloat64,
	"float4":  schema.TypeFloat32,
	"float8":  schema.TypeFloat64,

	// Decimal (supports precision/scale via decimal(p,s) syntax)
	"decimal": schema.TypeDecimal,
	"numeric": schema.TypeDecimal,

	// String types (sized forms like varchar(50) carry the length as MaxLength)
	"string":  schema.TypeString,
	"text":    schema.TypeString,
	"varchar": schema.TypeString,

	// Binary
	"binary": schema.TypeBinary,
	"bytes":  schema.TypeBinary,
	"blob":   schema.TypeBinary,

	// Date and time
	"date":          schema.TypeDate,
	"time":          schema.TypeTime,
	"timestamp":     schema.TypeTimestampTZ, // Default to timezone-aware
	"timestamptz":   schema.TypeTimestampTZ,
	"datetime":      schema.TypeTimestampTZ,
	"timestamp_ntz": schema.TypeTimestamp, // Explicit no timezone
	"timestampntz":  schema.TypeTimestamp,

	// Special types
	"json":     schema.TypeJSON,
	"jsonb":    schema.TypeJSON,
	"uuid":     schema.TypeUUID,
	"interval": schema.TypeInterval,
}

// ParseColumnOverrides parses a comma-separated list of column override
// entries. Each entry takes one of these shapes:
//
//	name:type            — apply type to source column "name"
//	dest:type:source     — rename source column "source" to "dest" and apply type
//	dest::source         — rename source column "source" to "dest" (no type change)
//
// Examples: "col1:type1,col2:type2", "col1:decimal(10,2),col2:bigint",
// "first_name:string:fname,email::eml".
func ParseColumnOverrides(input string) (ColumnOverrides, error) {
	if input == "" {
		return nil, nil
	}

	overrides := make(ColumnOverrides)
	pairs := SplitColumnPairs(input)

	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		override, err := parseColumnOverride(pair)
		if err != nil {
			return nil, err
		}

		if override.Name == "" {
			continue
		}

		overrides[strings.ToLower(override.Name)] = override
	}

	return overrides, nil
}

// SplitColumnPairs splits the input by commas, but respects parentheses.
// E.g., "a:decimal(10,2),b:int" -> ["a:decimal(10,2)", "b:int"]
func SplitColumnPairs(input string) []string {
	var pairs []string
	var current strings.Builder
	depth := 0

	for _, ch := range input {
		switch ch {
		case '(':
			depth++
			current.WriteRune(ch)
		case ')':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				pairs = append(pairs, current.String())
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		pairs = append(pairs, current.String())
	}

	return pairs
}

func parseColumnOverride(pair string) (ColumnOverride, error) {
	if !strings.Contains(pair, ":") {
		return ColumnOverride{}, fmt.Errorf("invalid column override format '%s': expected 'column:type'", pair)
	}

	// Parameterized type names like decimal(10,2) use ',' inside parens, not
	// ':' — so splitting the entry on ':' is safe.
	parts := strings.Split(pair, ":")

	var colName, typeSpec, renameTo string
	switch len(parts) {
	case 2:
		colName = strings.TrimSpace(parts[0])
		typeSpec = strings.TrimSpace(parts[1])
	case 3:
		renameTo = strings.TrimSpace(parts[0])
		typeSpec = strings.TrimSpace(parts[1])
		colName = strings.TrimSpace(parts[2])
	default:
		return ColumnOverride{}, fmt.Errorf("invalid column override format '%s': expected 'col:type', 'dest:type:source', or 'dest::source'", pair)
	}

	if colName == "" {
		return ColumnOverride{}, nil
	}
	if typeSpec == "" {
		if renameTo == "" {
			return ColumnOverride{}, fmt.Errorf("empty type in override '%s'", pair)
		}
		// Rename-only override: leave DataType at TypeUnknown.
		return ColumnOverride{Name: colName, RenameTo: renameTo}, nil
	}

	override := ColumnOverride{Name: colName, RenameTo: renameTo}

	// Check for parameterized types like decimal(10,2)
	if parenIdx := strings.Index(typeSpec, "("); parenIdx != -1 {
		baseName := strings.ToLower(strings.TrimSpace(typeSpec[:parenIdx]))
		paramsStr := typeSpec[parenIdx:]

		if !strings.HasSuffix(paramsStr, ")") {
			return ColumnOverride{}, fmt.Errorf("invalid type parameters in '%s': missing closing parenthesis", typeSpec)
		}

		paramsStr = paramsStr[1 : len(paramsStr)-1] // Remove ( and )
		params := strings.Split(paramsStr, ",")

		dataType, ok := StandardTypeNames[baseName]
		if !ok {
			return ColumnOverride{}, fmt.Errorf("unknown type '%s' in override '%s'", baseName, pair)
		}

		override.DataType = dataType

		switch dataType {
		case schema.TypeDecimal:
			// Handle precision/scale for decimal
			if len(params) >= 1 {
				p, err := strconv.Atoi(strings.TrimSpace(params[0]))
				if err != nil {
					return ColumnOverride{}, fmt.Errorf("invalid precision in '%s': %w", typeSpec, err)
				}
				override.Precision = p
			}
			if len(params) >= 2 {
				s, err := strconv.Atoi(strings.TrimSpace(params[1]))
				if err != nil {
					return ColumnOverride{}, fmt.Errorf("invalid scale in '%s': %w", typeSpec, err)
				}
				override.Scale = s
			}
		case schema.TypeString:
			// Sized string types take exactly one length parameter, e.g. varchar(50).
			if len(params) != 1 {
				return ColumnOverride{}, fmt.Errorf("invalid length in '%s': expected a single length, e.g. varchar(50)", typeSpec)
			}
			l, err := strconv.Atoi(strings.TrimSpace(params[0]))
			if err != nil {
				return ColumnOverride{}, fmt.Errorf("invalid length in '%s': %w", typeSpec, err)
			}
			if l <= 0 {
				return ColumnOverride{}, fmt.Errorf("invalid length in '%s': must be positive", typeSpec)
			}
			override.MaxLength = l
		}
	} else {
		// Simple type name
		typeName := strings.ToLower(typeSpec)
		dataType, ok := StandardTypeNames[typeName]
		if !ok {
			return ColumnOverride{}, fmt.Errorf("unknown type '%s' in override '%s'. Valid types: %s", typeName, pair, validTypesList())
		}
		override.DataType = dataType
	}

	return override, nil
}

func validTypesList() string {
	seen := make(map[schema.DataType]string)
	for name, dt := range StandardTypeNames {
		if _, ok := seen[dt]; !ok {
			seen[dt] = name
		}
	}

	types := make([]string, 0, len(seen))
	for _, name := range seen {
		types = append(types, name)
	}
	return strings.Join(types, ", ")
}

// Get returns the override for a column name, if one exists.
func (o ColumnOverrides) Get(columnName string) (ColumnOverride, bool) {
	if o == nil {
		return ColumnOverride{}, false
	}
	override, ok := o[strings.ToLower(columnName)]
	return override, ok
}

func (o ColumnOverrides) GetForColumn(columnName, schemaNaming string) (ColumnOverride, bool) {
	if o == nil {
		return ColumnOverride{}, false
	}
	conv := overrideMatchConvention(schemaNaming)
	key := strings.ToLower(conv.Normalize(columnName))
	for _, ov := range o {
		if strings.ToLower(conv.Normalize(ov.Name)) == key {
			return ov, true
		}
	}
	return ColumnOverride{}, false
}

func overrideMatchConvention(schemaNaming string) naming.NamingConvention {
	parsed, err := naming.ParseConvention(schemaNaming)
	if err != nil || parsed != naming.Direct {
		return naming.Get(naming.SnakeCase)
	}
	return naming.Get(naming.Direct)
}

// ApplyToColumn applies the override to a column, returning the modified column.
// A TypeUnknown override is treated as "no type change" (rename-only override),
// so the column's existing type is preserved.
func (o ColumnOverride) ApplyToColumn(col schema.Column) schema.Column {
	if o.DataType != schema.TypeUnknown {
		col.DataType = o.DataType
	}
	if o.Precision > 0 {
		col.Precision = o.Precision
	}
	if o.Scale > 0 {
		col.Scale = o.Scale
	}
	if o.MaxLength > 0 {
		col.MaxLength = o.MaxLength
	}
	return col
}
