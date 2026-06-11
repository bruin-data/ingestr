package onelake

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// deltaAddFile describes a single Parquet data file to be referenced by an
// "add" action in the Delta transaction log.
type deltaAddFile struct {
	Path string
	Size int64
}

type deltaField struct {
	Name     string         `json:"name"`
	Type     any            `json:"type"`
	Nullable bool           `json:"nullable"`
	Metadata map[string]any `json:"metadata"`
}

type deltaStruct struct {
	Type   string       `json:"type"`
	Fields []deltaField `json:"fields"`
}

// deltaTypeFor maps an ingestr column type to a Delta Lake schema type. Scalar
// types are returned as strings; arrays as nested type objects.
func deltaTypeFor(col schema.Column) any {
	switch col.DataType {
	case schema.TypeBoolean:
		return "boolean"
	case schema.TypeInt8:
		return "byte"
	case schema.TypeInt16:
		return "short"
	case schema.TypeInt32:
		return "integer"
	case schema.TypeInt64:
		return "long"
	case schema.TypeFloat32:
		return "float"
	case schema.TypeFloat64:
		return "double"
	case schema.TypeDecimal:
		precision := col.Precision
		if precision == 0 {
			precision = 38
		}
		return fmt.Sprintf("decimal(%d,%d)", precision, col.Scale)
	case schema.TypeString, schema.TypeUUID, schema.TypeJSON:
		return "string"
	case schema.TypeBinary:
		return "binary"
	case schema.TypeDate:
		return "date"
	case schema.TypeTimestamp, schema.TypeTimestampTZ:
		return "timestamp"
	case schema.TypeTime:
		// Delta has no TIME type; values are carried as microsecond longs.
		return "long"
	case schema.TypeArray:
		return map[string]any{
			"type":         "array",
			"elementType":  deltaTypeFor(schema.Column{DataType: col.ArrayType}),
			"containsNull": true,
		}
	default:
		return "string"
	}
}

// buildSchemaString returns the JSON-encoded Delta struct schema, which is itself
// stored as a string inside the metaData action.
func buildSchemaString(cols []schema.Column) (string, error) {
	fields := make([]deltaField, len(cols))
	for i, col := range cols {
		fields[i] = deltaField{
			Name:     col.Name,
			Type:     deltaTypeFor(col),
			Nullable: col.Nullable,
			Metadata: map[string]any{},
		}
	}
	b, err := json.Marshal(deltaStruct{Type: "struct", Fields: fields})
	if err != nil {
		return "", fmt.Errorf("failed to encode delta schema: %w", err)
	}
	return string(b), nil
}

func addActions(adds []deltaAddFile, nowMillis int64) []any {
	actions := make([]any, 0, len(adds))
	for _, f := range adds {
		actions = append(actions, map[string]any{
			"add": map[string]any{
				"path":             f.Path,
				"partitionValues":  map[string]string{},
				"size":             f.Size,
				"modificationTime": nowMillis,
				"dataChange":       true,
			},
		})
	}
	return actions
}

func removeActions(paths []string, nowMillis int64) []any {
	actions := make([]any, 0, len(paths))
	for _, p := range paths {
		actions = append(actions, map[string]any{
			"remove": map[string]any{
				"path":              p,
				"deletionTimestamp": nowMillis,
				"dataChange":        true,
			},
		})
	}
	return actions
}

func commitInfo(mode string, nowMillis int64) any {
	return map[string]any{
		"commitInfo": map[string]any{
			"timestamp": nowMillis,
			"operation": "WRITE",
			"operationParameters": map[string]string{
				"mode": mode,
			},
			"clientVersion": "ingestr",
		},
	}
}

func marshalActions(actions []any) ([]byte, error) {
	var buf bytes.Buffer
	for _, a := range actions {
		line, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("failed to encode delta action: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// buildInitialCommit produces the newline-delimited JSON for Delta commit
// version 0: protocol, metaData, one add per data file, and a commitInfo.
func buildInitialCommit(cols []schema.Column, adds []deltaAddFile, tableID string, nowMillis int64) ([]byte, error) {
	schemaString, err := buildSchemaString(cols)
	if err != nil {
		return nil, err
	}

	actions := []any{
		map[string]any{
			"protocol": map[string]any{
				"minReaderVersion": 1,
				"minWriterVersion": 2,
			},
		},
		map[string]any{
			"metaData": map[string]any{
				"id":               tableID,
				"format":           map[string]any{"provider": "parquet", "options": map[string]string{}},
				"schemaString":     schemaString,
				"partitionColumns": []string{},
				"configuration":    map[string]string{},
				"createdTime":      nowMillis,
			},
		},
	}
	actions = append(actions, addActions(adds, nowMillis)...)
	actions = append(actions, commitInfo("Overwrite", nowMillis))

	return marshalActions(actions)
}

// buildAppendCommit produces the newline-delimited JSON for an append commit
// (version > 0): add actions plus a commitInfo. The table's protocol and
// metaData are inherited from version 0.
func buildAppendCommit(adds []deltaAddFile, nowMillis int64) ([]byte, error) {
	actions := addActions(adds, nowMillis)
	actions = append(actions, commitInfo("Append", nowMillis))
	return marshalActions(actions)
}

// buildRewriteCommit produces a commit that removes existing data files and adds
// new ones — the copy-on-write pattern used by merge, delete+insert and scd2.
func buildRewriteCommit(removePaths []string, adds []deltaAddFile, operation string, nowMillis int64) ([]byte, error) {
	actions := removeActions(removePaths, nowMillis)
	actions = append(actions, addActions(adds, nowMillis)...)
	actions = append(actions, commitInfo(operation, nowMillis))
	return marshalActions(actions)
}

// commitFileName returns the zero-padded 20-digit Delta log file name for a
// commit version, e.g. 0 -> "00000000000000000000.json".
func commitFileName(version int64) string {
	return fmt.Sprintf("%020d.json", version)
}
