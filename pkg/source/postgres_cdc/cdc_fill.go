package postgres_cdc

import (
	"encoding/json"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// forwardFillUnchanged coalesces unchanged TOAST columns across the rows of an
// already-assembled staging batch.
//
// PostgreSQL omits unchanged TOASTed values from UPDATE WAL tuples. When an
// INSERT (or any row carrying the full value) and a later partial UPDATE for the
// same primary key land in the same staging batch — e.g. two autocommit
// transactions accumulated together — the partial UPDATE's omitted column is
// filled from the most recent prior row of that key. Filled columns are dropped
// from _cdc_unchanged_cols so the destination merge uses the staging value
// instead of the target fallback.
//
// Rows are processed in array order, which the accumulator preserves as commit
// (LSN) order. The pass is stateless across batches and bounded by one batch:
// cross-batch coalescing is intentionally not handled because once a row is
// flushed the destination holds its data and the _cdc_unchanged_cols target
// fallback applies. When nothing needs filling the input batch is returned
// unchanged (no allocation, no extra reference).
//
// Rows are keyed by their primary key as it appears in the staging batch, which
// is always the new tuple's value. Limitation: a primary-key-changing UPDATE
// that omits an unchanged TOAST column is keyed by its new PK, so it cannot be
// linked back to the originating row under the old PK. When that INSERT and the
// PK-changing UPDATE land in different commits of the same accumulated batch the
// omitted value is not recovered here (it stays in _cdc_unchanged_cols and falls
// back to the target). The within-commit decoder pass (applyIntraBatchFill) does
// handle PK changes via the old tuple, so only the cross-commit case is affected.
func forwardFillUnchanged(batch arrow.RecordBatch, pkNames []string) arrow.RecordBatch {
	if batch == nil || batch.NumRows() < 2 || len(pkNames) == 0 {
		return batch
	}

	sc := batch.Schema()
	nCols := int(batch.NumCols())
	nRows := int(batch.NumRows())

	pkIdx := make([]int, 0, len(pkNames))
	for _, name := range pkNames {
		if idxs := sc.FieldIndices(name); len(idxs) > 0 {
			pkIdx = append(pkIdx, idxs[0])
		}
	}
	if len(pkIdx) != len(pkNames) {
		return batch
	}

	unchangedColIdx := firstFieldIndex(sc, CDCUnchangedColsColumn)
	if unchangedColIdx < 0 {
		return batch
	}
	unchangedArr, ok := batch.Column(unchangedColIdx).(*array.String)
	if !ok {
		return batch
	}

	var deletedArr *array.Boolean
	if di := firstFieldIndex(sc, CDCDeletedColumn); di >= 0 {
		deletedArr, _ = batch.Column(di).(*array.Boolean)
	}

	meta := map[string]bool{
		CDCLSNColumn:           true,
		CDCDeletedColumn:       true,
		CDCSyncedAtColumn:      true,
		CDCUnchangedColsColumn: true,
	}
	isSource := make([]bool, nCols)
	for c := 0; c < nCols; c++ {
		isSource[c] = !meta[sc.Field(c).Name]
	}

	// srcIndex[c][r] is the source row whose cell column c copies into output row
	// r (identity unless filled). colHasFill marks columns that need rebuilding.
	srcIndex := make([][]int, nCols)
	colHasFill := make([]bool, nCols)
	for c := 0; c < nCols; c++ {
		if !isSource[c] {
			continue
		}
		srcIndex[c] = make([]int, nRows)
		for r := 0; r < nRows; r++ {
			srcIndex[c][r] = r
		}
	}

	newUnchanged := make([]string, nRows)
	unchangedRewritten := false

	// lastRow[pk][col] = most recent row holding an authoritative non-null value.
	lastRow := make(map[string]map[int]int)

	for r := 0; r < nRows; r++ {
		newUnchanged[r] = stringAt(unchangedArr, r)

		key, ok := pkKeyAt(batch, pkIdx, r)
		if !ok {
			continue
		}

		if deletedArr != nil && !deletedArr.IsNull(r) && deletedArr.Value(r) {
			delete(lastRow, key)
			continue
		}

		unchangedSet := unchangedNameSet(unchangedArr, r)
		filledAny := false

		for c := 0; c < nCols; c++ {
			if !isSource[c] {
				continue
			}
			name := sc.Field(c).Name
			col := batch.Column(c)

			if unchangedSet[name] && col.IsNull(r) {
				// Omitted unchanged TOAST: fill from the most recent known row
				// for this key, even when that value is NULL (an explicit
				// SET col = NULL upstream). The zero-copy take copies the NULL,
				// and dropping the column from _cdc_unchanged_cols stops the
				// destination from resurrecting the stale target value.
				if priors, ok := lastRow[key]; ok {
					if sr, ok := priors[c]; ok {
						srcIndex[c][r] = sr
						colHasFill[c] = true
						delete(unchangedSet, name)
						filledAny = true
					}
				}
				continue
			}

			// Authoritative value for this row (a real value or an explicit
			// NULL). Record it regardless of nullness so a later unchanged row
			// can be filled with it.
			if lastRow[key] == nil {
				lastRow[key] = make(map[int]int)
			}
			lastRow[key][c] = r
		}

		if filledAny {
			if b, err := json.Marshal(remainingUnchanged(unchangedArr, r, unchangedSet)); err == nil {
				newUnchanged[r] = string(b)
				unchangedRewritten = true
			}
		}
	}

	anyFill := unchangedRewritten
	for c := 0; c < nCols && !anyFill; c++ {
		if colHasFill[c] {
			anyFill = true
		}
	}
	if !anyFill {
		return batch
	}

	mem := memory.NewGoAllocator()
	outCols := make([]arrow.Array, nCols)
	for c := 0; c < nCols; c++ {
		switch {
		case isSource[c] && colHasFill[c]:
			outCols[c] = takeArray(batch.Column(c), srcIndex[c], mem)
		case c == unchangedColIdx && unchangedRewritten:
			outCols[c] = buildStringArray(newUnchanged, mem)
		default:
			outCols[c] = batch.Column(c)
			outCols[c].Retain()
		}
	}

	out := array.NewRecordBatch(sc, outCols, int64(nRows))
	for _, a := range outCols {
		a.Release()
	}
	return out
}

func firstFieldIndex(sc *arrow.Schema, name string) int {
	if idxs := sc.FieldIndices(name); len(idxs) > 0 {
		return idxs[0]
	}
	return -1
}

func pkKeyAt(batch arrow.RecordBatch, pkIdx []int, row int) (string, bool) {
	parts := make([]string, len(pkIdx))
	for i, c := range pkIdx {
		col := batch.Column(c)
		if col.IsNull(row) {
			return "", false
		}
		parts[i] = col.ValueStr(row)
	}
	return encodeKeyParts(parts), true
}

func stringAt(arr *array.String, row int) string {
	if arr.IsNull(row) {
		return "[]"
	}
	return arr.Value(row)
}

func unchangedNameSet(arr *array.String, row int) map[string]bool {
	names := parseUnchangedNames(arr, row)
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// remainingUnchanged preserves the original column order minus the filled ones.
func remainingUnchanged(arr *array.String, row int, remaining map[string]bool) []string {
	out := make([]string, 0, len(remaining))
	for _, n := range parseUnchangedNames(arr, row) {
		if remaining[n] {
			out = append(out, n)
		}
	}
	return out
}

func parseUnchangedNames(arr *array.String, row int) []string {
	if arr.IsNull(row) {
		return nil
	}
	s := strings.TrimSpace(arr.Value(row))
	if s == "" || s == "[]" {
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(s), &names); err != nil {
		return nil
	}
	return names
}

// takeArray builds a new array by copying, for each output row, the element at
// srcIndex[row] of col. It groups maximal ascending-contiguous runs into
// zero-copy slices and concatenates them, so unfilled stretches stay cheap and
// values are preserved exactly (no string round-trip).
func takeArray(col arrow.Array, srcIndex []int, mem memory.Allocator) arrow.Array {
	runs := make([]arrow.Array, 0, 4)
	n := len(srcIndex)
	for i := 0; i < n; {
		start := srcIndex[i]
		j := i + 1
		for j < n && srcIndex[j] == srcIndex[j-1]+1 {
			j++
		}
		runs = append(runs, array.NewSlice(col, int64(start), int64(start+(j-i))))
		i = j
	}

	out, err := array.Concatenate(runs, mem)
	for _, r := range runs {
		r.Release()
	}
	if err != nil {
		col.Retain()
		return col
	}
	return out
}

func buildStringArray(vals []string, mem memory.Allocator) arrow.Array {
	b := array.NewStringBuilder(mem)
	defer b.Release()
	for _, v := range vals {
		b.Append(v)
	}
	return b.NewArray()
}
