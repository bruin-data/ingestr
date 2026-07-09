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
	Table  string
	Column string
	Reason string
}

func (e *SchemaChangedError) Error() string {
	return fmt.Sprintf("table %s: column %q %s; restart the pipeline to pick up the new schema", e.Table, e.Column, e.Reason)
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

func (r *RelationInfo) columnType(name string) (uint32, bool) {
	for _, c := range r.Columns {
		if c.Name == name {
			return c.DataType, true
		}
	}
	return 0, false
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
// Relation message seen for this relation (nil on the first one); divergence
// from the connect-time schema on the first message is tolerated because it can
// also mean the replayed WAL predates DDL that happened before we connected.
func mapRelationToSchema(rel, prev *RelationInfo, tableSchema *schema.TableSchema, tableName string) error {
	nSource := sourceColumnCount(tableSchema)
	schemaIdx := make(map[string]int, nSource)
	for i := 0; i < nSource; i++ {
		schemaIdx[tableSchema.Columns[i].Name] = i
	}

	rel.SchemaIndex = make([]int, len(rel.Columns))
	mapped := make([]bool, nSource)
	for i, col := range rel.Columns {
		idx, ok := schemaIdx[col.Name]
		if !ok {
			if prev != nil && !prev.hasColumn(col.Name) {
				return &SchemaChangedError{Table: tableName, Column: col.Name, Reason: "was added mid-stream and is not part of the destination schema"}
			}
			// First Relation message for this table: the unknown column is either
			// replayed WAL predating a DROP COLUMN (nothing to ingest) or a column
			// added between the connect-time schema fetch and the table's first
			// traffic (its values cannot be ingested this run). Warn visibly —
			// silently dropping a live column's values must not go unnoticed.
			fmt.Printf("Warning: table %s: ignoring replicated column %q not present in the schema captured at connect time; if the column still exists, restart the pipeline to pick it up\n", tableName, col.Name)
			rel.SchemaIndex[i] = -1
			continue
		}
		if prev != nil {
			if prevType, ok := prev.columnType(col.Name); ok && prevType != col.DataType {
				// A transition whose new type agrees with the connect-time schema
				// is a replay across a type change the restart already picked up;
				// it must pass or the error would recur on every restart.
				newMatches, newKnown := oidMatchesColumn(col.DataType, tableSchema.Columns[idx])
				_, prevKnown := oidMatchesColumn(prevType, tableSchema.Columns[idx])
				if newKnown && !newMatches || !newKnown && prevKnown {
					return &SchemaChangedError{Table: tableName, Column: col.Name, Reason: fmt.Sprintf("changed type mid-stream (OID %d -> %d)", prevType, col.DataType)}
				}
				if !newKnown && !prevKnown {
					config.Debug("[CDC] Table %s: column %q custom/unknown type transition (OID %d -> %d) cannot be verified; keeping schema mapping", tableName, col.Name, prevType, col.DataType)
				} else {
					config.Debug("[CDC] Table %s: column %q type transition (OID %d -> %d) matches the connect-time schema", tableName, col.Name, prevType, col.DataType)
				}
			}
		}
		rel.SchemaIndex[i] = idx
		mapped[idx] = true
	}

	for i := 0; i < nSource; i++ {
		if !mapped[i] {
			config.Debug("[CDC] Table %s: column %q is missing from the replicated relation; its values will be NULL", tableName, tableSchema.Columns[i].Name)
		}
	}

	return nil
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
func parseTupleData(data []byte, rel *RelationInfo, tableSchema *schema.TableSchema) ([]interface{}, error) {
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
				values[schemaIdx] = convertTextValue(textVal, tableSchema.Columns[schemaIdx])
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
				values[schemaIdx] = data[:length]
			}
			data = data[length:]
		default:
			return nil, fmt.Errorf("unknown tuple data type: %c", colType)
		}
	}

	return values, nil
}
