package onelake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

var deltaDecimalTypePattern = regexp.MustCompile(`(?i)^decimal\(\s*(\d+)\s*,\s*(\d+)\s*\)$`)

func deltaSchemaFromMetadata(metadata deltaMetadata) (*deltaStruct, error) {
	raw, ok := metadata["schemaString"]
	if !ok {
		return nil, errors.New("delta metadata is missing schemaString")
	}
	var schemaString string
	if err := json.Unmarshal(raw, &schemaString); err != nil {
		return nil, fmt.Errorf("failed to decode delta schemaString: %w", err)
	}

	var result deltaStruct
	if err := json.Unmarshal([]byte(schemaString), &result); err != nil {
		return nil, fmt.Errorf("failed to decode delta table schema: %w", err)
	}
	if !strings.EqualFold(result.Type, "struct") {
		return nil, fmt.Errorf("unsupported delta root schema type %q", result.Type)
	}
	for i := range result.Fields {
		// The Delta protocol requires every field to carry a metadata object.
		// Writers are free to omit it, and re-encoding a nil map would commit
		// the field back as "metadata":null, which readers reject.
		if result.Fields[i].Metadata == nil {
			result.Fields[i].Metadata = map[string]any{}
		}
	}
	return &result, nil
}

func deltaMetadataWithSchema(metadata deltaMetadata, tableSchema *deltaStruct) (deltaMetadata, error) {
	if len(metadata) == 0 {
		return nil, errors.New("delta metadata is empty")
	}
	schemaBytes, err := json.Marshal(tableSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to encode delta table schema: %w", err)
	}
	schemaString, err := json.Marshal(string(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to encode delta schemaString: %w", err)
	}

	result := make(deltaMetadata, len(metadata))
	for key, value := range metadata {
		result[key] = append(json.RawMessage(nil), value...)
	}
	result["schemaString"] = schemaString
	return result, nil
}

// deltaConfiguration returns the table's configuration map. It is optional in
// the Delta metaData action, and an absent one unambiguously means no table
// properties are set.
func deltaConfiguration(metadata deltaMetadata) (map[string]string, error) {
	raw, ok := metadata["configuration"]
	if !ok {
		return nil, nil
	}
	var configuration map[string]string
	if err := json.Unmarshal(raw, &configuration); err != nil {
		return nil, fmt.Errorf("failed to decode delta table configuration: %w", err)
	}
	return configuration, nil
}

// deltaColumnMappingMode reports the table's delta.columnMapping.mode.
func deltaColumnMappingMode(metadata deltaMetadata) (string, error) {
	configuration, err := deltaConfiguration(metadata)
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(configuration["delta.columnMapping.mode"])), nil
}

// deltaPartitionColumns returns the table's partition columns. Their values are
// stored in the log's partitionValues rather than in the Parquet files, so any
// code that projects a data file onto the table schema has to know about them.
func deltaPartitionColumns(metadata deltaMetadata) ([]string, error) {
	raw, ok := metadata["partitionColumns"]
	if !ok {
		return nil, nil
	}
	var columns []string
	if err := json.Unmarshal(raw, &columns); err != nil {
		return nil, fmt.Errorf("failed to decode delta partition columns: %w", err)
	}
	return columns, nil
}

const (
	maxSupportedDeltaReaderVersion = 3
	maxSupportedDeltaWriterVersion = 7
)

// Table features (lowercased) whose presence in the protocol action is
// compatible with how ingestr reads and rewrites a table. Features that only
// matter when actually in use — deletion vectors on active files, a column
// mapping mode other than none, constraints or generated columns declared in
// the metadata — are allowed here and rejected by the in-use checks below.
var (
	supportedDeltaReaderFeatures = map[string]struct{}{
		"columnmapping":       {},
		"deletionvectors":     {},
		"timestampntz":        {},
		"v2checkpoint":        {},
		"vacuumprotocolcheck": {},
	}
	supportedDeltaWriterFeatures = map[string]struct{}{
		"appendonly":          {},
		"changedatafeed":      {},
		"checkconstraints":    {},
		"clustering":          {},
		"columnmapping":       {},
		"deletionvectors":     {},
		"domainmetadata":      {},
		"generatedcolumns":    {},
		"identitycolumns":     {},
		"invariants":          {},
		"timestampntz":        {},
		"v2checkpoint":        {},
		"vacuumprotocolcheck": {},
	}
)

// checkSupportedDeltaProtocol rejects tables that require reader or writer
// capabilities ingestr does not have. Ignoring the protocol action and writing
// anyway is how deleted rows come back or table guarantees get broken, so an
// unknown version or feature has to fail the operation.
func checkSupportedDeltaProtocol(protocol *deltaProtocol, op string) error {
	if protocol == nil {
		return fmt.Errorf("the delta log carries no protocol action, so %s cannot verify the table features it requires; the log may have been truncated", op)
	}
	if protocol.MinReaderVersion > maxSupportedDeltaReaderVersion || protocol.MinWriterVersion > maxSupportedDeltaWriterVersion {
		return fmt.Errorf("the table requires delta reader version %d and writer version %d, which OneLake does not support for %s",
			protocol.MinReaderVersion, protocol.MinWriterVersion, op)
	}
	for _, feature := range protocol.ReaderFeatures {
		if _, ok := supportedDeltaReaderFeatures[strings.ToLower(feature)]; !ok {
			return fmt.Errorf("the table requires the delta reader feature %q, which OneLake does not support for %s", feature, op)
		}
	}
	for _, feature := range protocol.WriterFeatures {
		if _, ok := supportedDeltaWriterFeatures[strings.ToLower(feature)]; !ok {
			return fmt.Errorf("the table requires the delta writer feature %q, which OneLake does not support for %s", feature, op)
		}
	}
	return nil
}

// checkRewritableDeltaTable rejects tables a copy-on-write rewrite would
// corrupt rather than fail on: files with deletion vectors would have their
// deleted rows resurrected, column mapping renames the physical columns and
// partition columns are not stored in the files at all (both of which would
// replace real values with NULLs), and append-only, change-data-feed,
// constraint, invariant, generated and identity columns all promise readers
// guarantees a plain remove-and-add commit does not keep.
func checkRewritableDeltaTable(snap *deltaSnapshot, tableSchema *deltaStruct, op string) error {
	if err := checkSupportedDeltaProtocol(snap.protocol, op); err != nil {
		return err
	}
	if snap.firstVersion != 0 {
		// Log cleanup removes commits already covered by a checkpoint, which
		// ingestr does not read, so the replayed set of active data files is
		// incomplete and the rewrite would leave the checkpoint-era files live
		// next to its own output.
		return fmt.Errorf("%s rewrites the table from its transaction log, but the log has been truncated (its oldest commit is %d, not 0), so the active data files cannot be reconstructed", op, snap.firstVersion)
	}
	if len(snap.deletionVectorFiles) > 0 {
		return fmt.Errorf("%s rewrites the table, which OneLake does not support while data file %q carries a deletion vector (rewriting it would restore its deleted rows)",
			op, snap.deletionVectorFiles[0])
	}

	configuration, err := deltaConfiguration(snap.metadata)
	if err != nil {
		return fmt.Errorf("failed to parse %s table configuration: %w", op, err)
	}
	if mode := strings.ToLower(strings.TrimSpace(configuration["delta.columnMapping.mode"])); mode != "" && mode != "none" {
		return fmt.Errorf("%s rewrites the table, which OneLake does not support for Delta column mapping mode %q", op, mode)
	}
	partitionColumns, err := deltaPartitionColumns(snap.metadata)
	if err != nil {
		return fmt.Errorf("failed to parse %s table configuration: %w", op, err)
	}
	if len(partitionColumns) > 0 {
		return fmt.Errorf("%s rewrites the table, which OneLake does not support for the partitioned Delta table (partition columns %v)", op, partitionColumns)
	}

	if strings.EqualFold(configuration["delta.appendOnly"], "true") {
		return fmt.Errorf("%s rewrites the table, which OneLake does not support for an append-only Delta table (delta.appendOnly is true)", op)
	}
	if strings.EqualFold(configuration["delta.enableChangeDataFeed"], "true") {
		return fmt.Errorf("%s rewrites the table without writing the change data files delta.enableChangeDataFeed promises its readers, which OneLake does not support", op)
	}
	for key := range configuration {
		if strings.HasPrefix(strings.ToLower(key), "delta.constraints.") {
			return fmt.Errorf("%s writes rows without enforcing the table's CHECK constraint %q, which OneLake does not support", op, key)
		}
	}
	for _, field := range tableSchema.Fields {
		for key := range field.Metadata {
			switch lowered := strings.ToLower(key); {
			case lowered == "delta.invariants":
				return fmt.Errorf("%s writes rows without enforcing the invariant on column %q, which OneLake does not support", op, field.Name)
			case lowered == "delta.generationexpression":
				return fmt.Errorf("%s writes rows without computing the generated column %q, which OneLake does not support", op, field.Name)
			case strings.HasPrefix(lowered, "delta.identity."):
				return fmt.Errorf("%s writes rows without maintaining the identity column %q, which OneLake does not support", op, field.Name)
			}
		}
	}
	return nil
}

func tableSchemaFromDelta(table string, deltaSchema *deltaStruct) (*schema.TableSchema, error) {
	if deltaSchema == nil {
		return nil, errors.New("delta schema is nil")
	}
	columns := make([]schema.Column, 0, len(deltaSchema.Fields))
	seen := make(map[string]struct{}, len(deltaSchema.Fields))
	for _, field := range deltaSchema.Fields {
		name := strings.ToLower(field.Name)
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("delta schema contains duplicate column %q", field.Name)
		}
		seen[name] = struct{}{}

		column, err := columnFromDeltaField(field)
		if err != nil {
			// Tables authored by Fabric or Spark can contain Delta types ingestr
			// has no equivalent for (structs, maps, nested arrays). Omitting them
			// keeps the rest of the table evolvable without carrying the column
			// into the write schema, where it would be materialized with the
			// wrong physical type; appended files simply leave it out and Delta
			// readers fill NULL. A source column colliding with an omitted one
			// still fails loudly in evolveDeltaMetadata, which checks the raw
			// Delta fields.
			config.Debug("[ONELAKE] Skipping delta column %q ingestr cannot map: %v", field.Name, err)
			continue
		}
		columns = append(columns, column)
	}
	return &schema.TableSchema{Name: table, Columns: columns}, nil
}

func columnFromDeltaField(field deltaField) (schema.Column, error) {
	column := schema.Column{Name: field.Name, Nullable: field.Nullable}
	if err := applyDeltaType(&column, field.Type, false); err != nil {
		return schema.Column{}, err
	}
	return column, nil
}

func applyDeltaType(column *schema.Column, deltaType any, arrayElement bool) error {
	if typeName, ok := deltaType.(string); ok {
		dataType, precision, scale, err := primitiveDeltaType(typeName)
		if err != nil {
			return err
		}
		if arrayElement {
			column.ArrayType = dataType
		} else {
			column.DataType = dataType
		}
		column.Precision = precision
		column.Scale = scale
		return nil
	}

	typeBytes, err := json.Marshal(deltaType)
	if err != nil {
		return fmt.Errorf("failed to inspect delta type: %w", err)
	}
	var complexType struct {
		Type        string `json:"type"`
		ElementType any    `json:"elementType"`
	}
	if err := json.Unmarshal(typeBytes, &complexType); err != nil {
		return fmt.Errorf("failed to decode delta type: %w", err)
	}
	if !strings.EqualFold(complexType.Type, "array") || arrayElement {
		return fmt.Errorf("unsupported delta type %s", string(typeBytes))
	}
	column.DataType = schema.TypeArray
	if err := applyDeltaType(column, complexType.ElementType, true); err != nil {
		return fmt.Errorf("unsupported delta array element: %w", err)
	}
	return nil
}

func primitiveDeltaType(typeName string) (schema.DataType, int, int, error) {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "boolean":
		return schema.TypeBoolean, 0, 0, nil
	case "byte":
		return schema.TypeInt8, 0, 0, nil
	case "short":
		return schema.TypeInt16, 0, 0, nil
	case "integer", "int":
		return schema.TypeInt32, 0, 0, nil
	case "long":
		return schema.TypeInt64, 0, 0, nil
	case "float":
		return schema.TypeFloat32, 0, 0, nil
	case "double":
		return schema.TypeFloat64, 0, 0, nil
	case "string":
		return schema.TypeString, 0, 0, nil
	case "binary":
		return schema.TypeBinary, 0, 0, nil
	case "date":
		return schema.TypeDate, 0, 0, nil
	case "timestamp":
		return schema.TypeTimestampTZ, 0, 0, nil
	case "timestamp_ntz":
		return schema.TypeTimestamp, 0, 0, nil
	}

	matches := deltaDecimalTypePattern.FindStringSubmatch(typeName)
	if len(matches) == 3 {
		precision, err := strconv.Atoi(matches[1])
		if err != nil {
			return schema.TypeUnknown, 0, 0, fmt.Errorf("invalid delta decimal precision %q", matches[1])
		}
		scale, err := strconv.Atoi(matches[2])
		if err != nil {
			return schema.TypeUnknown, 0, 0, fmt.Errorf("invalid delta decimal scale %q", matches[2])
		}
		return schema.TypeDecimal, precision, scale, nil
	}
	return schema.TypeUnknown, 0, 0, fmt.Errorf("unsupported delta type %q", typeName)
}

func (*OneLakeDestination) NormalizeSchemaEvolutionColumn(column schema.Column) schema.Column {
	return normalizeSchemaEvolutionColumn(column)
}

func normalizeSchemaEvolutionColumn(column schema.Column) schema.Column {
	column.MaxLength = 0
	if column.DataType == schema.TypeArray {
		column.ArrayType = normalizeOneLakeType(column.ArrayType)
		return column
	}
	column.DataType = normalizeOneLakeType(column.DataType)
	return column
}

func normalizeOneLakeType(dataType schema.DataType) schema.DataType {
	switch dataType {
	case schema.TypeUUID, schema.TypeJSON:
		return schema.TypeString
	case schema.TypeTime:
		return schema.TypeInt64
	case schema.TypeTimestamp:
		return schema.TypeTimestampTZ
	default:
		return dataType
	}
}

func evolveDeltaMetadata(metadata deltaMetadata, comparison *schemaevolution.SchemaComparison) (deltaMetadata, bool, error) {
	if comparison == nil || !comparison.HasChanges {
		return metadata, false, nil
	}
	mappingMode, err := deltaColumnMappingMode(metadata)
	if err != nil {
		return nil, false, err
	}
	if mappingMode != "" && mappingMode != "none" {
		return nil, false, fmt.Errorf("OneLake schema evolution does not support Delta column mapping mode %q", mappingMode)
	}

	deltaSchema, err := deltaSchemaFromMetadata(metadata)
	if err != nil {
		return nil, false, err
	}
	changed := false
	for _, change := range comparison.Changes {
		switch change.Type {
		case schemaevolution.ChangeAddColumn:
			index := deltaFieldIndex(deltaSchema.Fields, change.ColumnName)
			if index >= 0 {
				existing, err := columnFromDeltaField(deltaSchema.Fields[index])
				if err != nil {
					return nil, false, fmt.Errorf("column %q: %w", change.ColumnName, err)
				}
				if !sameOneLakePhysicalType(existing, change.NewColumn) {
					return nil, false, fmt.Errorf(
						"cannot add column %q because the table already has it as Delta %s, not %s",
						change.ColumnName, describeDeltaType(deltaSchema.Fields[index].Type), describeDeltaType(deltaTypeFor(change.NewColumn)),
					)
				}
				continue
			}
			newColumn := change.NewColumn
			newColumn.Nullable = true
			deltaSchema.Fields = append(deltaSchema.Fields, deltaField{
				Name: newColumn.Name, Type: deltaTypeFor(newColumn), Nullable: true, Metadata: map[string]any{},
			})
			changed = true

		case schemaevolution.ChangeRelaxNullability, schemaevolution.ChangeRemoveColumn:
			index := deltaFieldIndex(deltaSchema.Fields, change.ColumnName)
			if index >= 0 && !deltaSchema.Fields[index].Nullable {
				deltaSchema.Fields[index].Nullable = true
				changed = true
			}

		case schemaevolution.ChangeWidenType, schemaevolution.ChangeOverrideType:
			if change.OldColumn == nil || !sameOneLakePhysicalType(*change.OldColumn, change.NewColumn) {
				// Report the type the table actually declares: deltaTypeFor
				// renders columns ingestr could not map as "string", which would
				// make the message read "from string to string".
				oldType := "unknown"
				if index := deltaFieldIndex(deltaSchema.Fields, change.ColumnName); index >= 0 {
					oldType = describeDeltaType(deltaSchema.Fields[index].Type)
				} else if change.OldColumn != nil {
					oldType = describeDeltaType(deltaTypeFor(*change.OldColumn))
				}
				return nil, false, fmt.Errorf(
					"column %q requires changing its Delta type from %s to %s, which OneLake does not support; recreate the table or pin the column type with --columns",
					change.ColumnName, oldType, describeDeltaType(deltaTypeFor(change.NewColumn)),
				)
			}
		}
	}
	if !changed {
		return metadata, false, nil
	}

	updated, err := deltaMetadataWithSchema(metadata, deltaSchema)
	if err != nil {
		return nil, false, err
	}
	return updated, true, nil
}

// describeDeltaType renders a Delta type the way the table's schemaString
// carries it. Complex types are decoded JSON objects, which %v would print as
// Go map syntax.
func describeDeltaType(deltaType any) string {
	if name, ok := deltaType.(string); ok {
		return name
	}
	encoded, err := json.Marshal(deltaType)
	if err != nil {
		return fmt.Sprintf("%v", deltaType)
	}
	return string(encoded)
}

func deltaFieldIndex(fields []deltaField, name string) int {
	for i := range fields {
		if strings.EqualFold(fields[i].Name, name) {
			return i
		}
	}
	return -1
}

func sameOneLakePhysicalType(left, right schema.Column) bool {
	// deltaTypeFor falls back to "string" for anything it does not recognize,
	// which would make an unknown column look compatible with every string
	// column, so refuse to judge unknown columns here.
	if left.DataType == schema.TypeUnknown || right.DataType == schema.TypeUnknown {
		return false
	}
	leftType, err := json.Marshal(deltaTypeFor(left))
	if err != nil {
		return false
	}
	rightType, err := json.Marshal(deltaTypeFor(right))
	if err != nil {
		return false
	}
	return string(leftType) == string(rightType)
}

func (d *OneLakeDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	if comparison == nil || !comparison.HasChanges {
		return nil, nil
	}
	if d.client == nil {
		return nil, errors.New("OneLake destination is not connected")
	}
	tableDir, err := d.tableDirForTables(table, "schema evolution")
	if err != nil {
		return nil, err
	}

	var lastConflict error
	for range maxDeltaCommitAttempts {
		snapshot, err := d.readTableMetadata(ctx, tableDir)
		if err != nil {
			return nil, fmt.Errorf("failed to read OneLake table schema: %w", err)
		}
		if !snapshot.exists || len(snapshot.metadata) == 0 {
			return nil, nil
		}
		if err := checkSupportedDeltaProtocol(snapshot.protocol, "schema evolution"); err != nil {
			return nil, err
		}

		metadata, changed, err := evolveDeltaMetadata(snapshot.metadata, comparison)
		if err != nil {
			return nil, err
		}
		if !changed {
			return nil, nil
		}
		commit, err := buildSchemaEvolutionCommit(metadata, time.Now().UnixMilli())
		if err != nil {
			return nil, err
		}
		version := snapshot.version + 1
		if err := d.uploadDeltaCommit(ctx, tableDir+"/_delta_log", version, commit); err != nil {
			if errors.Is(err, errDeltaCommitConflict) {
				lastConflict = err
				config.Debug("[ONELAKE] Delta commit conflict during schema evolution; retrying from latest snapshot")
				continue
			}
			return nil, err
		}
		config.Debug("[ONELAKE] Evolved schema in delta version %d at %s", version, tableDir)
		return nil, nil
	}

	return nil, fmt.Errorf("failed to commit OneLake schema evolution after %d attempts: %w", maxDeltaCommitAttempts, lastConflict)
}

func (d *OneLakeDestination) SupportsColumnTypeChanges() bool {
	return false
}
