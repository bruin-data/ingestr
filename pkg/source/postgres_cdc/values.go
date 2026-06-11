package postgres_cdc

import (
	"encoding/json"

	"github.com/bruin-data/ingestr/pkg/schema"
)

type tupleUnchanged struct{}

var tupleUnchangedMarker = tupleUnchanged{}

func isTupleUnchanged(v interface{}) bool {
	_, ok := v.(tupleUnchanged)
	return ok
}

func resolveColumnValue(change Change, colIdx int) interface{} {
	var val interface{}
	if colIdx < len(change.Values) {
		val = change.Values[colIdx]
	}
	if !isTupleUnchanged(val) {
		return val
	}
	if change.Operation == "UPDATE" && colIdx < len(change.OldValues) {
		old := change.OldValues[colIdx]
		if old != nil && !isTupleUnchanged(old) {
			return old
		}
	}
	return nil
}

func columnIsUnchanged(change Change, colIdx int) bool {
	if change.Operation != "UPDATE" {
		return false
	}
	if colIdx >= len(change.Values) {
		return false
	}
	return isTupleUnchanged(change.Values[colIdx])
}

func unchangedColumnsJSON(change Change, columns []schema.Column, nSourceCols int) string {
	if change.Operation != "UPDATE" {
		return "[]"
	}
	names := make([]string, 0)
	for i := 0; i < nSourceCols && i < len(columns); i++ {
		if columnIsUnchanged(change, i) {
			names = append(names, columns[i].Name)
		}
	}
	b, err := json.Marshal(names)
	if err != nil {
		return "[]"
	}
	return string(b)
}
