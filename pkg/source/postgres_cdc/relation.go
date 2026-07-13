package postgres_cdc

import (
	"encoding/binary"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

// SchemaChangedError reports mid-stream DDL on a target table that the current
// stream cannot represent: a column added after the stream started, or a
// column type change. Streaming readers catch it and rebuild the stream around
// the refreshed schema; batch runs surface it and heal on restart.
type SchemaChangedError struct {
	Table      string
	Column     string
	Reason     string
	Mismatches []SchemaMismatch
}

type SchemaMismatch struct {
	Column string
	Reason string
}

// TableReincarnatedError reports that a relation with the configured table
// name now has a different PostgreSQL OID. No row from the replacement table
// may be decoded until the destination has been replaced from a fresh snapshot.
type TableReincarnatedError struct {
	Table    string
	Previous string
	Current  string
}

func (e *TableReincarnatedError) Error() string {
	return fmt.Sprintf("table %s was dropped and recreated (incarnation %s -> %s)", e.Table, e.Previous, e.Current)
}

func (e *SchemaChangedError) Error() string {
	if len(e.Mismatches) > 1 {
		return fmt.Sprintf("table %s: %d columns changed; restart the pipeline to pick up the new schema", e.Table, len(e.Mismatches))
	}
	return fmt.Sprintf("table %s: column %q %s; restart the pipeline to pick up the new schema", e.Table, e.Column, e.Reason)
}

func newSchemaChangedError(table string, mismatches []SchemaMismatch) *SchemaChangedError {
	first := mismatches[0]
	return &SchemaChangedError{Table: table, Column: first.Column, Reason: first.Reason, Mismatches: mismatches}
}

func (e *SchemaChangedError) Columns() []string {
	if len(e.Mismatches) == 0 {
		return []string{e.Column}
	}
	columns := make([]string, 0, len(e.Mismatches))
	seen := make(map[string]struct{}, len(e.Mismatches))
	for _, mismatch := range e.Mismatches {
		if _, ok := seen[mismatch.Column]; ok {
			continue
		}
		seen[mismatch.Column] = struct{}{}
		columns = append(columns, mismatch.Column)
	}
	return columns
}

type RelationInfo struct {
	RelationID uint32
	Namespace  string
	Name       string
	Columns    []RelationColumn
	// SchemaIndex maps each relation column to its index in the connect-time
	// schema's source columns, resolved by name (-1 when the schema has no such
	// column). Nil means no mapping was built and tuples decode positionally.
	SchemaIndex []int
	// Stale marks historical pre-DDL relation metadata replayed after the table
	// was resnapshotted at a newer shape.
	Stale bool
}

type RelationColumn struct {
	Flags    uint8
	Name     string
	DataType uint32
	TypeMod  int32
}

func (r *RelationInfo) hasColumn(name string) bool {
	for _, c := range r.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (r *RelationInfo) column(name string) (RelationColumn, bool) {
	for _, c := range r.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return RelationColumn{}, false
}

func parseRelationMessage(data []byte) (*RelationInfo, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("relation message too short")
	}

	relID := binary.BigEndian.Uint32(data[:4])
	data = data[4:]

	namespace, n := readString(data)
	data = data[n:]

	name, n := readString(data)
	data = data[n:]

	// Skip replica identity
	if len(data) < 1 {
		return nil, fmt.Errorf("relation message missing replica identity")
	}
	data = data[1:]

	if len(data) < 2 {
		return nil, fmt.Errorf("relation message missing column count")
	}
	numCols := binary.BigEndian.Uint16(data[:2])
	data = data[2:]

	columns := make([]RelationColumn, numCols)
	for i := uint16(0); i < numCols; i++ {
		if len(data) < 1 {
			return nil, fmt.Errorf("relation message column flags truncated")
		}
		flags := data[0]
		data = data[1:]

		colName, n := readString(data)
		data = data[n:]

		if len(data) < 4 {
			return nil, fmt.Errorf("relation message column type truncated")
		}
		dataType := binary.BigEndian.Uint32(data[:4])
		data = data[4:]

		if len(data) < 4 {
			return nil, fmt.Errorf("relation message column typemod truncated")
		}
		typeMod := int32(binary.BigEndian.Uint32(data[:4]))
		data = data[4:]

		columns[i] = RelationColumn{
			Flags:    flags,
			Name:     colName,
			DataType: dataType,
			TypeMod:  typeMod,
		}
	}

	return &RelationInfo{
		RelationID: relID,
		Namespace:  namespace,
		Name:       name,
		Columns:    columns,
	}, nil
}

// mapRelationToSchema resolves each Relation-message column to its position in
// the connect-time schema by name and stores the mapping in rel.SchemaIndex.
// pgoutput resends a Relation message whenever a table's shape changes, so
// decoding through this mapping keeps values in the right columns even when the
// relation's column ordinals no longer line up with the schema (e.g. WAL
// replayed across a DROP COLUMN that happened while the pipeline was down).
//
// Mid-stream DDL the fixed destination schema cannot represent fails loudly
// instead of silently dropping or corrupting values: a column added after the
// stream started would have its values discarded, and a column type change
// would make the connect-time text conversion silently produce NULLs.
// Restarting the pipeline re-fetches the schema and replays from the last
// checkpoint, so the failed transaction's data is recovered. prev is the last
// Relation message seen for this relation (nil on the first one). A caller may
// allow a first-message column only after a live catalog refresh proves that
// column has already been dropped and the message is replayed pre-DDL WAL.
func mapRelationToSchema(rel, prev *RelationInfo, tableSchema *schema.TableSchema, tableName string, allowedUnknown ...map[string]struct{}) error {
	nSource := sourceColumnCount(tableSchema)
	schemaIdx := make(map[string]int, nSource)
	for i := 0; i < nSource; i++ {
		schemaIdx[tableSchema.Columns[i].Name] = i
	}

	rel.SchemaIndex = make([]int, len(rel.Columns))
	mapped := make([]bool, nSource)
	var mismatches []SchemaMismatch
	addMismatch := func(column, reason string) {
		mismatches = append(mismatches, SchemaMismatch{Column: column, Reason: reason})
	}
	for i, col := range rel.Columns {
		idx, ok := schemaIdx[col.Name]
		if !ok {
			if len(allowedUnknown) > 0 {
				if _, allowed := allowedUnknown[0][col.Name]; allowed {
					rel.SchemaIndex[i] = -1
					continue
				}
			}
			if prev != nil && !prev.hasColumn(col.Name) {
				addMismatch(col.Name, "was added mid-stream and is not part of the destination schema")
				continue
			}
			addMismatch(col.Name, "is present in the first replication relation but not the captured schema")
			continue
		}
		matches, known := oidMatchesColumn(col.DataType, tableSchema.Columns[idx])
		if prev == nil && known && !matches {
			if len(allowedUnknown) > 0 {
				if _, allowed := allowedUnknown[0][col.Name]; allowed {
					rel.Stale = true
					rel.SchemaIndex[i] = idx
					mapped[idx] = true
					continue
				}
			}
			addMismatch(col.Name, fmt.Sprintf("has historical type OID %d that does not match the live captured schema", col.DataType))
			rel.SchemaIndex[i] = idx
			mapped[idx] = true
			continue
		}
		typmodMatches, typmodKnown := typmodMatchesColumn(col, tableSchema.Columns[idx])
		if prev == nil && typmodKnown && !typmodMatches {
			if len(allowedUnknown) > 0 {
				if _, allowed := allowedUnknown[0][col.Name]; allowed {
					rel.Stale = true
					rel.SchemaIndex[i] = idx
					mapped[idx] = true
					continue
				}
			}
			addMismatch(col.Name, fmt.Sprintf("has historical type modifier %d that does not match the live captured schema", col.TypeMod))
			rel.SchemaIndex[i] = idx
			mapped[idx] = true
			continue
		}
		if prev != nil {
			if prevCol, ok := prev.column(col.Name); ok && prevCol.DataType != col.DataType {
				// A transition whose new type agrees with the connect-time schema
				// is a replay across a type change the restart already picked up;
				// it must pass or the error would recur on every restart.
				newMatches, newKnown := oidMatchesColumn(col.DataType, tableSchema.Columns[idx])
				_, prevKnown := oidMatchesColumn(prevCol.DataType, tableSchema.Columns[idx])
				if newKnown && !newMatches || !newKnown && prevKnown {
					addMismatch(col.Name, fmt.Sprintf("changed type mid-stream (OID %d -> %d)", prevCol.DataType, col.DataType))
				}
				if !newKnown && !prevKnown {
					config.Debug("[CDC] Table %s: column %q custom/unknown type transition (OID %d -> %d) cannot be verified; keeping schema mapping", tableName, col.Name, prevCol.DataType, col.DataType)
				} else {
					config.Debug("[CDC] Table %s: column %q type transition (OID %d -> %d) matches the connect-time schema", tableName, col.Name, prevCol.DataType, col.DataType)
				}
			}
			if prevCol, ok := prev.column(col.Name); ok && prevCol.DataType == col.DataType && prevCol.TypeMod != col.TypeMod {
				newMatches, relevant := typmodMatchesColumn(col, tableSchema.Columns[idx])
				if relevant && !newMatches {
					addMismatch(col.Name, fmt.Sprintf("changed type modifier mid-stream (%d -> %d)", prevCol.TypeMod, col.TypeMod))
				}
			}
		}
		rel.SchemaIndex[i] = idx
		mapped[idx] = true
	}
	if len(mismatches) > 0 {
		return newSchemaChangedError(tableName, mismatches)
	}

	if len(allowedUnknown) > 0 {
		reconcileAllowedRelationColumns(rel, tableSchema, schemaIdx, allowedUnknown[0])
	}

	for i := 0; i < nSource; i++ {
		if !mapped[i] {
			config.Debug("[CDC] Table %s: column %q is missing from the replicated relation; its values will be NULL", tableName, tableSchema.Columns[i].Name)
		}
	}

	return nil
}

func reconcileAllowedRelationColumns(rel *RelationInfo, tableSchema *schema.TableSchema, schemaIdx map[string]int, allowed map[string]struct{}) {
	if len(allowed) == 0 {
		return
	}

	currentShape := true
	for name := range allowed {
		idx, expected := schemaIdx[name]
		relCol, present := rel.column(name)
		if !expected {
			if present {
				currentShape = false
			}
			continue
		}
		if !present {
			currentShape = false
			rel.Stale = true
			continue
		}

		if matches, known := oidMatchesColumn(relCol.DataType, tableSchema.Columns[idx]); known && !matches {
			currentShape = false
			rel.Stale = true
			continue
		}
		if matches, known := typmodMatchesColumn(relCol, tableSchema.Columns[idx]); known && !matches {
			currentShape = false
			rel.Stale = true
		}
	}

	if currentShape && !rel.Stale {
		clear(allowed)
	}
}

func typmodMatchesColumn(rel RelationColumn, col schema.Column) (bool, bool) {
	switch rel.DataType {
	case 1700: // numeric
		if rel.TypeMod == -1 {
			return col.DataType == schema.TypeDecimal && col.Precision == 38 && col.Scale == 9, true
		}
		mod := rel.TypeMod - 4
		precision := int((mod >> 16) & 0xffff)
		scale := int(mod & 0x7ff)
		if scale&0x400 != 0 {
			scale |= ^0x7ff
		}
		return col.DataType == schema.TypeDecimal && col.Precision == precision && col.Scale == scale, true
	case 1042, 1043: // bpchar, varchar
		length := 0
		if rel.TypeMod >= 4 {
			length = int(rel.TypeMod - 4)
		}
		return col.DataType == schema.TypeString && col.MaxLength == length, true
	default:
		return true, false
	}
}

// oidMatchesColumn reports whether a relation column's pg type OID maps to the
// same internal data type as the connect-time schema column, and whether the OID
// was known to pgtype's built-in map. Unknown OIDs are custom/domain/enum types;
// when both sides of a transition are unknown, the decoder cannot verify a type
// mismatch and must not rebuild forever on the same WAL records.
func oidMatchesColumn(oid uint32, col schema.Column) (bool, bool) {
	t, ok := pgtype.NewMap().TypeForOID(oid)
	if !ok {
		return false, false
	}
	dt, _, _, arrayType := postgres.MapPostgresToDataType(t.Name)
	return dt == col.DataType && arrayType == col.ArrayType, true
}

// parseTupleData decodes a pgoutput TupleData block into a slice indexed by the
// connect-time schema's source columns, routing each wire value through the
// relation's name-resolved SchemaIndex rather than its ordinal position. Values
// for relation columns with no schema counterpart are discarded; schema columns
// absent from the relation stay nil (NULL).
func parseTupleData(data []byte, rel *RelationInfo, tableSchema *schema.TableSchema, typeMap *pgtype.Map) ([]interface{}, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("tuple data too short")
	}

	numCols := binary.BigEndian.Uint16(data[:2])
	data = data[2:]

	values := make([]interface{}, sourceColumnCount(tableSchema))

	for i := uint16(0); i < numCols; i++ {
		if len(data) < 1 {
			return nil, fmt.Errorf("tuple data truncated at column %d", i)
		}

		colType := data[0]
		data = data[1:]

		schemaIdx := int(i)
		if rel.SchemaIndex != nil {
			schemaIdx = -1
			if int(i) < len(rel.SchemaIndex) {
				schemaIdx = rel.SchemaIndex[i]
			}
		}
		if schemaIdx >= len(values) {
			schemaIdx = -1
		}

		switch colType {
		case tupleDataNull:
			// values[schemaIdx] is already nil
		case tupleDataUnchanged:
			if schemaIdx >= 0 {
				values[schemaIdx] = tupleUnchangedMarker
			}
		case tupleDataText:
			if len(data) < 4 {
				return nil, fmt.Errorf("text length truncated")
			}
			length := binary.BigEndian.Uint32(data[:4])
			data = data[4:]

			if len(data) < int(length) {
				return nil, fmt.Errorf("text data truncated")
			}
			textVal := string(data[:length])
			data = data[length:]

			if schemaIdx >= 0 {
				col := tableSchema.Columns[schemaIdx]
				value, err := convertTextValueWithMap(textVal, col, typeMap)
				if err != nil {
					return nil, fmt.Errorf("failed to decode text value for column %q: %w", col.Name, err)
				}
				values[schemaIdx] = value
			}
		case tupleDataBinary:
			if len(data) < 4 {
				return nil, fmt.Errorf("binary length truncated")
			}
			length := binary.BigEndian.Uint32(data[:4])
			data = data[4:]

			if len(data) < int(length) {
				return nil, fmt.Errorf("binary data truncated")
			}
			if schemaIdx >= 0 {
				if int(i) < len(rel.Columns) {
					v, err := convertBinaryValue(data[:length], tableSchema.Columns[schemaIdx], rel.Columns[i].DataType, typeMap)
					if err != nil {
						return nil, err
					}
					values[schemaIdx] = v
				} else {
					values[schemaIdx] = append([]byte(nil), data[:length]...)
				}
			}
			data = data[length:]
		default:
			return nil, fmt.Errorf("unknown tuple data type: %c", colType)
		}
	}

	return values, nil
}

func markMissingRelationColumnsUnchanged(values []interface{}, rel *RelationInfo) {
	if rel == nil || len(rel.SchemaIndex) == 0 {
		return
	}
	mapped := make([]bool, len(values))
	for _, schemaIndex := range rel.SchemaIndex {
		if schemaIndex >= 0 && schemaIndex < len(mapped) {
			mapped[schemaIndex] = true
		}
	}
	for i := range values {
		if !mapped[i] {
			values[i] = tupleUnchangedMarker
		}
	}
}
