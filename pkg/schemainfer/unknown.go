package schemainfer

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func inferUnknownColumnType(arr arrow.Array) (arrow.DataType, bool) {
	ext, ok := arr.(array.ExtensionArray)
	if !ok {
		return schema.UnknownArrowType, false
	}

	storage := ext.Storage()
	var inferred arrow.DataType
	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			continue
		}

		raw, ok := StringValueAt(storage, i)
		if !ok {
			continue
		}

		decoded, err := DecodeUnknownValue(raw)
		if err != nil {
			decoded = raw
		}

		// TODO: corner cases
		decodedStr, ok := decoded.(string)
		if ok && strings.TrimSpace(decodedStr) == "" {
			continue
		}

		t := inferValueType(decoded)
		if inferred == nil {
			inferred = t
			continue
		}

		merged, err := MergeArrowTypes(inferred, t)
		if err != nil {
			inferred = arrow.BinaryTypes.String
			continue
		}
		inferred = merged
	}

	if inferred == nil {
		return schema.UnknownArrowType, false
	}
	return inferred, true
}

// DecodeUnknownValue decodes a JSON-encoded string from Unknown type storage.
func DecodeUnknownValue(raw string) (any, error) {
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// StringValueAt extracts a string value from an arrow array at the given index.
func StringValueAt(arr arrow.Array, idx int) (string, bool) {
	switch a := arr.(type) {
	case *array.String:
		return a.Value(idx), true
	case *array.LargeString:
		return a.Value(idx), true
	case *array.Binary:
		return string(a.Value(idx)), true
	case *array.LargeBinary:
		return string(a.Value(idx)), true
	case *array.Dictionary:
		return a.ValueStr(idx), true
	default:
		return "", false
	}
}
