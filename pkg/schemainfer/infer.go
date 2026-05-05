// Package schemainfer provides schema inference from Arrow record batches.
// It is used for sources with unknown schemas (e.g., MongoDB) where the schema
// must be determined by analyzing all the data.
package schemainfer

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/araddon/dateparse"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/schema"
)

// FieldInfo tracks information about a field observed during schema inference.
type FieldInfo struct {
	Name      string
	Type      arrow.DataType
	Nullable  bool
	SeenNull  bool             // Whether a null value was observed
	HasData   bool             // Whether at least one non-null value was observed
	SeenTypes []arrow.DataType // All types observed for type conflict detection
}

// SchemaInferrer collects Arrow schemas from record batches and produces
// a unified schema that encompasses all observed fields.
type SchemaInferrer struct {
	seenFields       map[string]*FieldInfo
	fieldOrder       []string // Preserve field order
	batchCount       int64
	rowCount         int64
	droppedColumns   map[string]bool
	protectedColumns map[string]bool
}

// NewSchemaInferrer creates a new schema inferrer.
func NewSchemaInferrer() *SchemaInferrer {
	return &SchemaInferrer{
		seenFields: make(map[string]*FieldInfo),
		fieldOrder: make([]string, 0),
	}
}

// AddBatch incorporates a record batch into schema inference.
// It analyzes the batch schema and merges it with previously seen schemas.
func (i *SchemaInferrer) AddBatch(batch arrow.RecordBatch) error {
	if batch == nil {
		return nil
	}

	i.batchCount++
	i.rowCount += batch.NumRows()

	batchSchema := batch.Schema()
	for fieldIdx := 0; fieldIdx < batchSchema.NumFields(); fieldIdx++ {
		field := batchSchema.Field(fieldIdx)
		col := batch.Column(fieldIdx)

		effectiveType := field.Type
		hasNulls := col.NullN() > 0
		hasNonNull := col.Len()-col.NullN() > 0
		if isUnknownType(field.Type) {
			inferred, ok := inferUnknownColumnType(col)
			if ok {
				effectiveType = inferred
			} else {
				// All non-null values were empty strings or JSON nulls —
				// treat this column as having no meaningful data for this batch.
				hasNonNull = false
			}
		}

		existing, exists := i.seenFields[field.Name]
		if !exists {
			// First time seeing this field
			i.seenFields[field.Name] = &FieldInfo{
				Name:      field.Name,
				Type:      effectiveType,
				Nullable:  field.Nullable || hasNulls,
				SeenNull:  hasNulls,
				HasData:   hasNonNull,
				SeenTypes: []arrow.DataType{effectiveType},
			}
			i.fieldOrder = append(i.fieldOrder, field.Name)
		} else {
			// Field seen before - check for type conflicts and merge
			existing.Nullable = existing.Nullable || field.Nullable || hasNulls
			existing.SeenNull = existing.SeenNull || hasNulls
			existing.HasData = existing.HasData || hasNonNull

			// Check if type is different
			if !arrow.TypeEqual(existing.Type, effectiveType) {
				// Track all seen types
				typeAlreadySeen := false
				for _, t := range existing.SeenTypes {
					if arrow.TypeEqual(t, effectiveType) {
						typeAlreadySeen = true
						break
					}
				}
				if !typeAlreadySeen {
					existing.SeenTypes = append(existing.SeenTypes, effectiveType)
				}

				// Merge types using promotion rules
				mergedType, err := MergeArrowTypes(existing.Type, effectiveType)
				if err != nil {
					config.Debug("[INFER] Type conflict for field %s: %v, promoting to string", field.Name, err)
					mergedType = arrow.BinaryTypes.String
				}
				existing.Type = mergedType
			}
		}
	}

	return nil
}

// InferSchema returns the unified Arrow schema after analyzing all batches.
func (i *SchemaInferrer) InferSchema() (*arrow.Schema, error) {
	if len(i.seenFields) == 0 {
		return nil, nil
	}

	i.droppedColumns = make(map[string]bool)
	fields := make([]arrow.Field, 0, len(i.fieldOrder))
	for _, name := range i.fieldOrder {
		info := i.seenFields[name]
		if !info.HasData && info.Nullable && isUnknownType(info.Type) && !i.protectedColumns[strings.ToLower(name)] {
			i.droppedColumns[name] = true
			continue
		}
		fields = append(fields, arrow.Field{
			Name:     info.Name,
			Type:     info.Type,
			Nullable: info.Nullable,
		})
	}

	if len(fields) == 0 {
		return nil, nil
	}

	return arrow.NewSchema(fields, nil), nil
}

// ToTableSchema converts the inferred schema to an internal TableSchema.
func (i *SchemaInferrer) ToTableSchema(tableName string) (*schema.TableSchema, error) {
	// Parse schema.table format
	schemaName := ""
	tblName := tableName
	if idx := strings.LastIndex(tableName, "."); idx > 0 {
		schemaName = tableName[:idx]
		tblName = tableName[idx+1:]
	}

	if len(i.seenFields) == 0 {
		return nil, nil
	}

	i.droppedColumns = make(map[string]bool)
	columns := make([]schema.Column, 0, len(i.fieldOrder))
	for _, name := range i.fieldOrder {
		info := i.seenFields[name]
		if !info.HasData && info.Nullable && isUnknownType(info.Type) && !i.protectedColumns[strings.ToLower(name)] {
			config.Debug("[INFER] Dropping all-null unknown-type column %q from schema", name)
			i.droppedColumns[name] = true
			continue
		}
		col := ArrowFieldToColumn(info.Name, info.Type, info.Nullable)
		columns = append(columns, col)
	}

	if len(columns) == 0 {
		return nil, nil
	}

	return &schema.TableSchema{
		Name:        tblName,
		Schema:      schemaName,
		Columns:     columns,
		PrimaryKeys: nil, // Cannot infer PKs from data
	}, nil
}

func (i *SchemaInferrer) ProtectColumns(names []string) {
	if len(names) == 0 {
		return
	}
	if i.protectedColumns == nil {
		i.protectedColumns = make(map[string]bool, len(names))
	}
	for _, n := range names {
		i.protectedColumns[strings.ToLower(n)] = true
	}
}

// DroppedColumns returns columns that were dropped during inference (all-null nullable columns).
func (i *SchemaInferrer) DroppedColumns() map[string]bool {
	return i.droppedColumns
}

// Stats returns statistics about the inference process.
func (i *SchemaInferrer) Stats() InferStats {
	return InferStats{
		BatchCount: i.batchCount,
		RowCount:   i.rowCount,
		FieldCount: len(i.seenFields),
	}
}

// InferStats contains statistics about the schema inference process.
type InferStats struct {
	BatchCount int64
	RowCount   int64
	FieldCount int
}

// TODO (cagin): if table already existed, compare existing column types against opts.Schema
// and ALTER columns that were VARCHAR (from unknown inference) to their newly inferred type
func inferValueType(val interface{}) arrow.DataType {
	switch v := val.(type) {
	case bool:
		return arrow.FixedWidthTypes.Boolean
	case int:
		return arrow.PrimitiveTypes.Int64
	case int8:
		return arrow.PrimitiveTypes.Int64
	case int16:
		return arrow.PrimitiveTypes.Int64
	case int32:
		return arrow.PrimitiveTypes.Int64
	case int64:
		return arrow.PrimitiveTypes.Int64
	case uint:
		if uint64(v) > math.MaxInt64 {
			return arrow.PrimitiveTypes.Float64
		}
		return arrow.PrimitiveTypes.Int64
	case uint8:
		return arrow.PrimitiveTypes.Int64
	case uint16:
		return arrow.PrimitiveTypes.Int64
	case uint32:
		return arrow.PrimitiveTypes.Int64
	case uint64:
		if v > math.MaxInt64 {
			return arrow.PrimitiveTypes.Float64
		}
		return arrow.PrimitiveTypes.Int64
	case float32:
		if isIntegralFloatWithinInt64(float64(v)) {
			return arrow.PrimitiveTypes.Int64
		}
		return arrow.PrimitiveTypes.Float64
	case float64:
		if isIntegralFloatWithinInt64(v) {
			return arrow.PrimitiveTypes.Int64
		}
		return arrow.PrimitiveTypes.Float64
	case json.Number:
		if _, err := v.Int64(); err == nil && !numberHasDecimal(v.String()) {
			return arrow.PrimitiveTypes.Int64
		}
		return arrow.PrimitiveTypes.Float64
	case string:
		if looksLikeDate(v) {
			if _, err := time.Parse("2006-01-02", v); err == nil {
				return arrow.FixedWidthTypes.Date32
			}
		}
		if looksLikeTemporal(v) {
			if _, err := dateparse.ParseAny(v); err == nil {
				return arrow.FixedWidthTypes.Timestamp_us
			}
		}
		return arrow.BinaryTypes.String
	case time.Time:
		return arrow.FixedWidthTypes.Timestamp_us
	case *time.Time:
		return arrow.FixedWidthTypes.Timestamp_us
	case map[string]interface{}:
		return schema.JSONArrowType
	case []interface{}:
		return schema.JSONArrowType
	case nil:
		return schema.JSONArrowType
	default:
		return schema.JSONArrowType
	}
}

func looksLikeDate(s string) bool {
	if len(s) != len("2006-01-02") {
		return false
	}
	for i, ch := range s {
		switch i {
		case 4, 7:
			if ch != '-' {
				return false
			}
		default:
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}

func looksLikeTemporal(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}

	// Avoid running date parsing on obvious non-temporal strings.
	if !strings.ContainsAny(trimmed, "0123456789") {
		return false
	}

	// Date-like forms, e.g. YYYY-MM-DD or YYYY/MM/DD.
	if strings.Count(trimmed, "-") >= 2 || strings.Count(trimmed, "/") >= 2 {
		return true
	}

	// Time-like forms, e.g. HH:MM[:SS[.frac]].
	if strings.Count(trimmed, ":") >= 1 {
		return true
	}

	// ISO-like forms with explicit T/Z marker and digits.
	if strings.ContainsAny(trimmed, "Tt") && strings.ContainsAny(trimmed, "Zz") {
		return true
	}

	return false
}

func numberHasDecimal(s string) bool {
	for _, ch := range s {
		if ch == '.' || ch == 'e' || ch == 'E' {
			return true
		}
	}
	return false
}

func isIntegralFloatWithinInt64(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	if v < math.MinInt64 || v > math.MaxInt64 {
		return false
	}
	return math.Trunc(v) == v
}
