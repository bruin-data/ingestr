package schemainfer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// Builds a TableSchema purely from --columns when a schema-less source produced no rows.
func TableSchemaFromColumnOverrides(columnsSpec, tableName, schemaNaming string) (*schema.TableSchema, error) {
	if columnsSpec == "" {
		return nil, nil
	}

	schemaName := ""
	tblName := tableName
	if idx := strings.LastIndex(tableName, "."); idx > 0 {
		schemaName = tableName[:idx]
		tblName = tableName[idx+1:]
	}

	ts := &schema.TableSchema{Name: tblName, Schema: schemaName}
	if err := AppendMissingOverrideColumns(ts, columnsSpec, schemaNaming); err != nil {
		return nil, err
	}
	if len(ts.Columns) == 0 {
		return nil, nil
	}
	return ts, nil
}

// Marks columns as exempt from being dropped as all-null during inference.
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

func (i *SchemaInferrer) ProtectColumnOverrides(columnsSpec string) error {
	if columnsSpec == "" {
		return nil
	}
	overrides, err := schemaevolution.ParseColumnOverrides(columnsSpec)
	if err != nil {
		return fmt.Errorf("failed to parse column overrides: %w", err)
	}
	i.ProtectColumns(overrides.Names())
	return nil
}

// Ensures every primary key, the incremental key, and the partition column
// exist on the schema. Used when a synthetic schema is built from --columns
// without any data rows, so these key columns aren't guaranteed to be included
// in the overrides spec.
func AddKeyColumnsIfMissing(ts *schema.TableSchema, primaryKeys []string, incrementalKey, partitionBy, schemaNaming string) error {
	if ts == nil {
		return nil
	}

	conv, err := resolveOverrideConvention(schemaNaming)
	if err != nil {
		return err
	}
	normalize := func(s string) string {
		return strings.ToLower(conv.Normalize(s))
	}

	existing := make(map[string]bool, len(ts.Columns))
	for _, col := range ts.Columns {
		existing[normalize(col.Name)] = true
	}

	for _, pk := range primaryKeys {
		if pk == "" || existing[normalize(pk)] {
			continue
		}
		ts.Columns = append(ts.Columns, schema.Column{
			Name:     pk,
			DataType: schema.TypeString,
			Nullable: false,
		})
		existing[normalize(pk)] = true
		fmt.Printf("Warning: PK column %q created as STRING placeholder (no type info available); pass --columns %s:<type> for correct typing\n", pk, pk)
		config.Debug("[INFER] Added missing PK column to synthetic schema: %s (string)", pk)
	}

	if incrementalKey != "" && !existing[normalize(incrementalKey)] {
		ts.Columns = append(ts.Columns, schema.Column{
			Name:     incrementalKey,
			DataType: schema.TypeString,
			Nullable: true,
		})
		existing[normalize(incrementalKey)] = true
		fmt.Printf("Warning: incremental_key column %q created as STRING placeholder (no type info available); pass --columns %s:<type> for correct typing\n", incrementalKey, incrementalKey)
		config.Debug("[INFER] Added missing incremental-key column to synthetic schema: %s (string)", incrementalKey)
	}

	if partitionBy != "" && !existing[normalize(partitionBy)] {
		ts.Columns = append(ts.Columns, schema.Column{
			Name:     partitionBy,
			DataType: schema.TypeDate,
			Nullable: true,
		})
		fmt.Printf("Warning: partition_by column %q created as DATE placeholder (no type info available); pass --columns %s:<type> if a different type is expected\n", partitionBy, partitionBy)
		config.Debug("[INFER] Added missing partition_by column to synthetic schema: %s (date)", partitionBy)
	}
	return nil
}

func resolveOverrideConvention(schemaNaming string) (naming.NamingConvention, error) {
	parsed, err := naming.ParseConvention(schemaNaming)
	if err != nil {
		return nil, fmt.Errorf("invalid schema naming convention: %w", err)
	}
	if parsed == naming.Direct {
		return naming.Get(naming.Direct), nil
	}
	return naming.Get(naming.SnakeCase), nil
}

// Adds columns from --columns that are absent from
// the inferred schema (e.g. dropped because every value was empty/null).
func AppendMissingOverrideColumns(tableSchema *schema.TableSchema, columnsSpec, schemaNaming string) error {
	if tableSchema == nil || columnsSpec == "" {
		return nil
	}
	overrides, err := schemaevolution.ParseColumnOverrides(columnsSpec)
	if err != nil {
		return fmt.Errorf("failed to parse column overrides: %w", err)
	}
	if len(overrides) == 0 {
		return nil
	}

	conv, err := resolveOverrideConvention(schemaNaming)
	if err != nil {
		return err
	}
	normalize := func(s string) string {
		return strings.ToLower(conv.Normalize(s))
	}

	existing := make(map[string]bool, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		existing[normalize(col.Name)] = true
	}

	// Iterate in alphabetical order so the appended columns are deterministic
	// across runs.
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		ov := overrides[name]
		// If a rename was requested, the appended column should use the
		// destination name.
		finalName := ov.Name
		if ov.RenameTo != "" {
			finalName = ov.RenameTo
		}
		if existing[normalize(finalName)] {
			continue
		}

		dataType := ov.DataType
		precision := ov.Precision
		scale := ov.Scale
		// Rename-only overrides carry no type. Fall back to STRING
		if dataType == schema.TypeUnknown {
			dataType = schema.TypeString
			precision = 0
			scale = 0
			fmt.Printf("Warning: column %q created as STRING placeholder (no type in --columns); pass --columns %s:<type>:%s for correct typing\n", finalName, finalName, ov.Name)
		}

		tableSchema.Columns = append(tableSchema.Columns, schema.Column{
			Name:      finalName,
			DataType:  dataType,
			Precision: precision,
			Scale:     scale,
			Nullable:  true,
		})
	}

	return nil
}
