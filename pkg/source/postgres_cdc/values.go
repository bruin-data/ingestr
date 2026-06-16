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
	if v, ok := columnFilledFromBatch(change, colIdx); ok {
		return v
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

// columnFilledFromBatch reports whether the column's value was supplied from the
// within-commit fill (i.e. staging now holds an authoritative value for it).
func columnFilledFromBatch(change Change, colIdx int) (interface{}, bool) {
	if change.batchFill == nil || colIdx >= len(change.batchFill) {
		return nil, false
	}
	v := change.batchFill[colIdx]
	return v, v != nil
}

// applyIntraBatchFill coalesces unchanged TOAST columns within a single commit:
// an INSERT (or full-value row) followed by a partial UPDATE of the same primary
// key, where the UPDATE omits the unchanged TOAST value. compactPendingChanges
// later keeps only the latest row per key, so without this the earlier full
// value would be lost. State is local to the commit; cross-commit coalescing is
// handled later at the staging-batch level (see forwardFillUnchanged), which is
// where separate transactions are actually merged.
func applyIntraBatchFill(changes []Change, tableSchema *schema.TableSchema) {
	if len(changes) == 0 || tableSchema == nil {
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
		lookupKey, storeKey := fillStateKeys(*change, pkIndices, i)

		batchFill := make([]interface{}, nSource)
		hasFill := false
		for colIdx := 0; colIdx < nSource; colIdx++ {
			if !columnIsUnchanged(*change, colIdx) {
				continue
			}
			if resolveColumnValueBase(*change, colIdx) != nil {
				continue
			}
			if prior, ok := state[lookupKey]; ok && colIdx < len(prior) && prior[colIdx] != nil {
				batchFill[colIdx] = prior[colIdx]
				hasFill = true
			}
		}
		if hasFill {
			change.batchFill = batchFill
		}

		if change.Operation == "DELETE" {
			delete(state, storeKey)
			if lookupKey != storeKey {
				delete(state, lookupKey)
			}
			continue
		}

		resolved := make([]interface{}, nSource)
		for colIdx := 0; colIdx < nSource; colIdx++ {
			resolved[colIdx] = resolveColumnValue(*change, colIdx)
		}
		if lookupKey != storeKey {
			delete(state, lookupKey)
		}
		state[storeKey] = resolved
	}
}

func fillStateKeys(change Change, pkIndices []int, changeIndex int) (lookupKey, storeKey string) {
	storeKey = pkKeyFromRow(change.Values, change.OldValues, pkIndices, changeIndex)
	lookupKey = storeKey
	if change.Operation == "UPDATE" && pkValueChanged(change, pkIndices) {
		lookupKey = pkKeyFromRow(change.OldValues, change.OldValues, pkIndices, changeIndex)
	}
	return lookupKey, storeKey
}

func pkValueChanged(change Change, pkIndices []int) bool {
	if change.Operation != "UPDATE" {
		return false
	}
	for _, idx := range pkIndices {
		old := columnValueAt(change.OldValues, idx)
		cur := columnValueAt(change.Values, idx)
		if isTupleUnchanged(cur) {
			continue
		}
		if fmt.Sprintf("%v", old) != fmt.Sprintf("%v", cur) {
			return true
		}
	}
	return false
}

func columnValueAt(values []interface{}, idx int) interface{} {
	if idx < len(values) {
		return values[idx]
	}
	return nil
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

func pkKeyFromRow(values, oldValues []interface{}, pkIndices []int, changeIndex int) string {
	parts := make([]string, len(pkIndices))
	for i, idx := range pkIndices {
		val := columnValueAt(values, idx)
		if val == nil || isTupleUnchanged(val) {
			val = columnValueAt(oldValues, idx)
		}
		if val == nil || isTupleUnchanged(val) {
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
		if !columnIsUnchanged(change, i) {
			continue
		}
		// A column resolved from the cross-commit fill cache now has an
		// authoritative value in staging, so it must NOT be reported as
		// unchanged; otherwise the destination merge would keep the (possibly
		// stale) target value instead of the filled one on an existing row.
		if _, filled := columnFilledFromBatch(change, i); filled {
			continue
		}
		names = append(names, columns[i].Name)
	}
	b, err := json.Marshal(names)
	if err != nil {
		return "[]"
	}
	return string(b)
}
