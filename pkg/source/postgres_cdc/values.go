package postgres_cdc

import (
	"encoding/json"
	"fmt"
	"strconv"
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
	return resolveColumnValueBase(change, colIdx)
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

// knownValue is a column value tracked during within-commit fill. known
// distinguishes an authoritative NULL (an explicit SET col = NULL, which must be
// propagated) from a column we have no information about (which must stay
// unchanged so the destination uses its target value).
type knownValue struct {
	val   interface{}
	known bool
}

// applyIntraBatchFill coalesces unchanged TOAST columns within a single commit:
// an INSERT (or full-value row) followed by a partial UPDATE of the same primary
// key, where the UPDATE omits the unchanged TOAST value. compactPendingChanges
// later keeps only the latest row per key, so without this the earlier value
// would be lost. State is local to the commit; cross-commit coalescing is
// handled later at the staging-batch level (see forwardFillUnchanged), which is
// where separate transactions are actually merged.
//
// A filled column's value is written directly into change.Values, replacing the
// unchanged marker. That makes columnIsUnchanged report false for it, so it is
// emitted with its (possibly NULL) value and excluded from _cdc_unchanged_cols —
// which is exactly what we want, including when the known value is NULL.
func applyIntraBatchFill(changes []Change, tableSchema *schema.TableSchema) {
	if len(changes) == 0 || tableSchema == nil {
		return
	}

	pkIndices := pkColumnIndices(tableSchema.Columns, tableSchema.PrimaryKeys)
	if len(pkIndices) == 0 {
		return
	}

	nSource := sourceColumnCount(tableSchema)
	state := make(map[string][]knownValue)

	for i := range changes {
		change := &changes[i]
		lookupKey, storeKey := fillStateKeys(*change, pkIndices, i)
		prior := state[lookupKey]

		for colIdx := 0; colIdx < nSource; colIdx++ {
			if !columnIsUnchanged(*change, colIdx) {
				continue
			}
			if base := resolveColumnValueBase(*change, colIdx); base != nil {
				// Authoritative old-tuple value (REPLICA IDENTITY FULL). Clear
				// the marker so the column is emitted with its value and not
				// reported as unchanged; otherwise a matched merge falls back to
				// the target and discards a value set earlier in the same batch.
				setColumnValue(change, colIdx, base)
				continue
			}
			if prior != nil && colIdx < len(prior) && prior[colIdx].known {
				setColumnValue(change, colIdx, prior[colIdx].val)
			}
		}

		if change.Operation == "DELETE" {
			delete(state, storeKey)
			if lookupKey != storeKey {
				delete(state, lookupKey)
			}
			continue
		}

		// Carry forward prior known values, then overwrite with the columns this
		// change resolves authoritatively (a real value, an explicit NULL, or a
		// fill applied above).
		next := make([]knownValue, nSource)
		copy(next, prior)
		for colIdx := 0; colIdx < nSource; colIdx++ {
			if columnIsAuthoritative(*change, colIdx) {
				next[colIdx] = knownValue{val: resolveColumnValue(*change, colIdx), known: true}
			}
		}
		if lookupKey != storeKey {
			delete(state, lookupKey)
		}
		state[storeKey] = next
	}
}

// expandUpdates rewrites UPDATE changes whose row identity moved or is absent,
// so the emitted stream stays applicable at the destination. It runs after
// applyIntraBatchFill (which may still need the original UPDATE shape) and
// before compaction.
//
// Keyed tables: an UPDATE that changes a key column would merge under the new
// key and leave the old-key row behind in the destination forever. The old
// tuple (sent by Postgres precisely because the identity changed) becomes a
// DELETE for the old key, followed by the original UPDATE.
//
// Keyless tables (append-only change log, REPLICA IDENTITY FULL): an UPDATE's
// new image alone cannot identify which row changed. It becomes a
// DELETE(old image) + INSERT(new image) pair, making the log a self-contained
// retract stream; unchanged-TOAST markers in the new image resolve from the
// full old tuple.
func expandUpdates(changes []Change, tableSchema *schema.TableSchema) []Change {
	if len(changes) == 0 || tableSchema == nil {
		return changes
	}
	pkIndices := pkColumnIndices(tableSchema.Columns, tableSchema.PrimaryKeys)
	keyless := len(pkIndices) == 0

	needsExpand := false
	for i := range changes {
		if updateNeedsExpansion(changes[i], pkIndices, keyless) {
			needsExpand = true
			break
		}
	}
	if !needsExpand {
		return changes
	}

	out := make([]Change, 0, len(changes)+1)
	for _, c := range changes {
		if !updateNeedsExpansion(c, pkIndices, keyless) {
			out = append(out, c)
			continue
		}
		out = append(out, Change{Operation: "DELETE", LSN: c.LSN, Values: c.OldValues})
		if keyless {
			out = append(out, Change{Operation: "INSERT", LSN: c.LSN, Values: resolvedNewImage(c)})
		} else {
			out = append(out, c)
		}
	}
	return out
}

func updateNeedsExpansion(c Change, pkIndices []int, keyless bool) bool {
	if c.Operation != "UPDATE" || c.OldValues == nil {
		return false
	}
	if keyless {
		return true
	}
	return pkValueChanged(c, pkIndices)
}

// resolvedNewImage materializes an UPDATE's new tuple with unchanged-TOAST
// markers resolved from the old tuple, so it can be emitted as a plain INSERT.
func resolvedNewImage(c Change) []interface{} {
	vals := make([]interface{}, len(c.Values))
	for i := range c.Values {
		vals[i] = resolveColumnValueBase(c, i)
	}
	return vals
}

// setColumnValue overwrites a column's value in place, replacing an unchanged
// marker once we have resolved the value it stood for.
func setColumnValue(change *Change, colIdx int, val interface{}) {
	if colIdx < len(change.Values) {
		change.Values[colIdx] = val
	}
}

// columnIsAuthoritative reports whether the change carries a definite value for
// the column — a real value, an explicit NULL, or an old-tuple value — as
// opposed to an omitted unchanged-TOAST marker we cannot resolve.
func columnIsAuthoritative(change Change, colIdx int) bool {
	if !columnIsUnchanged(change, colIdx) {
		return true
	}
	return resolveColumnValueBase(change, colIdx) != nil
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
	return encodeKeyParts(parts)
}

// encodeKeyParts joins parts into a collision-free key by prefixing each with
// its byte length. A plain delimiter (e.g. "|") is not injective: a value that
// contains the delimiter could make two distinct composite keys collide and
// cause TOAST values to be coalesced across different rows.
func encodeKeyParts(parts []string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(strconv.Itoa(len(p)))
		b.WriteByte(':')
		b.WriteString(p)
	}
	return b.String()
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
		// applyIntraBatchFill overwrites the unchanged marker of any column it
		// resolves (including to NULL), so columnIsUnchanged already excludes
		// filled columns here; a column still marked unchanged is one we have no
		// staging value for and the destination must fall back to its target.
		if !columnIsUnchanged(change, i) {
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
