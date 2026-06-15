package postgres_cdc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/schema"
)

type tupleUnchanged struct{}

var tupleUnchangedMarker = tupleUnchanged{}

func isTupleUnchanged(v interface{}) bool {
	_, ok := v.(tupleUnchanged)
	return ok
}

func resolveColumnValue(change Change, colIdx int) interface{} {
	if v := resolveColumnValueBase(change, colIdx); v != nil || !columnIsUnchanged(change, colIdx) {
		return v
	}
	if change.batchFill != nil && colIdx < len(change.batchFill) {
		return change.batchFill[colIdx]
	}
	return nil
}

func resolveColumnValueBase(change Change, colIdx int) interface{} {
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

func applyIntraBatchFill(changes []Change, tableSchema *schema.TableSchema) {
	if len(changes) < 2 || tableSchema == nil {
		return
	}

	pkIndices := pkColumnIndices(tableSchema.Columns, tableSchema.PrimaryKeys)
	if len(pkIndices) == 0 {
		return
	}

	nSource := sourceColumnCount(tableSchema)
	state := make(map[string][]interface{})

	for i := range changes {
		change := &changes[i]
		key := changePKKey(*change, pkIndices, i)

		batchFill := make([]interface{}, nSource)
		hasFill := false
		for colIdx := 0; colIdx < nSource; colIdx++ {
			if !columnIsUnchanged(*change, colIdx) {
				continue
			}
			if resolveColumnValueBase(*change, colIdx) != nil {
				continue
			}
			if prior, ok := state[key]; ok && colIdx < len(prior) && prior[colIdx] != nil {
				batchFill[colIdx] = prior[colIdx]
				hasFill = true
			}
		}
		if hasFill {
			change.batchFill = batchFill
		}

		if change.Operation == "DELETE" {
			delete(state, key)
			continue
		}

		resolved := make([]interface{}, nSource)
		for colIdx := 0; colIdx < nSource; colIdx++ {
			resolved[colIdx] = resolveColumnValue(*change, colIdx)
		}
		state[key] = resolved
	}
}

func pkColumnIndices(columns []schema.Column, primaryKeys []string) []int {
	if len(primaryKeys) == 0 {
		return nil
	}
	indices := make([]int, 0, len(primaryKeys))
	for _, pk := range primaryKeys {
		idx := -1
		for colIdx, col := range columns {
			if col.Name == pk {
				idx = colIdx
				break
			}
		}
		if idx < 0 {
			return nil
		}
		indices = append(indices, idx)
	}
	return indices
}

func changePKKey(change Change, pkIndices []int, changeIndex int) string {
	parts := make([]string, len(pkIndices))
	for i, idx := range pkIndices {
		var val interface{}
		if idx < len(change.Values) {
			val = change.Values[idx]
		}
		if val == nil && idx < len(change.OldValues) {
			val = change.OldValues[idx]
		}
		if val == nil {
			return fmt.Sprintf("row-%d", changeIndex)
		}
		parts[i] = fmt.Sprintf("%T:%v", val, val)
	}
	return strings.Join(parts, "|")
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
