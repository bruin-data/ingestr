package onelake

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

const keySep = "\x1f"

// fieldIndex returns the index of a field by name (exact, then case-insensitive).
func fieldIndex(s *arrow.Schema, name string) (int, bool) {
	for i := 0; i < s.NumFields(); i++ {
		if s.Field(i).Name == name {
			return i, true
		}
	}
	for i := 0; i < s.NumFields(); i++ {
		if strings.EqualFold(s.Field(i).Name, name) {
			return i, true
		}
	}
	return -1, false
}

func fieldIndices(s *arrow.Schema, names []string) ([]int, error) {
	idxs := make([]int, len(names))
	for i, n := range names {
		idx, ok := fieldIndex(s, n)
		if !ok {
			return nil, fmt.Errorf("column %q not found in schema", n)
		}
		idxs[i] = idx
	}
	return idxs, nil
}

// cellString renders a single cell as a canonical, comparable string. NULLs map
// to a distinct sentinel so they never collide with real values.
func cellString(arr arrow.Array, idx int) string {
	if arr.IsNull(idx) {
		return "\x00NULL"
	}
	v := arrowutil.Value(arr, idx)
	switch val := v.(type) {
	case nil:
		return "\x00NULL"
	case []byte:
		return string(val)
	case time.Time:
		return val.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// rowKey builds a composite key for a row from the given column indices.
func rowKey(batch arrow.RecordBatch, colIdxs []int, row int) string {
	parts := make([]string, len(colIdxs))
	for i, c := range colIdxs {
		parts[i] = cellString(batch.Column(c), row)
	}
	return strings.Join(parts, keySep)
}

// buildKeySet collects composite keys for the given columns across all batches.
func buildKeySet(batches []arrow.RecordBatch, colIdxs []int) map[string]struct{} {
	set := make(map[string]struct{})
	for _, b := range batches {
		for r := 0; r < int(b.NumRows()); r++ {
			set[rowKey(b, colIdxs, r)] = struct{}{}
		}
	}
	return set
}

// filterByMask keeps the rows where keep[i] is true. Returns nil if no rows are
// kept. The returned batch is owned by the caller.
func filterByMask(ctx context.Context, batch arrow.RecordBatch, keep []bool) (arrow.RecordBatch, error) {
	b := array.NewBooleanBuilder(memory.DefaultAllocator)
	defer b.Release()
	b.Reserve(len(keep))
	kept := 0
	for _, k := range keep {
		b.Append(k)
		if k {
			kept++
		}
	}
	if kept == 0 {
		return nil, nil
	}
	mask := b.NewArray()
	defer mask.Release()

	return compute.FilterRecordBatch(ctx, batch, mask, compute.DefaultFilterOptions())
}

// retainAll returns the same batches with an extra reference each, so they can be
// returned to a caller that will release them independently of the inputs.
func retainAll(batches []arrow.RecordBatch) []arrow.RecordBatch {
	out := make([]arrow.RecordBatch, 0, len(batches))
	for _, b := range batches {
		b.Retain()
		out = append(out, b)
	}
	return out
}

// mergeBatches implements upsert-by-primary-key: target rows whose PK appears in
// staging are dropped, then all staging rows are kept.
func mergeBatches(ctx context.Context, target, staging []arrow.RecordBatch, primaryKeys []string) ([]arrow.RecordBatch, error) {
	if len(staging) == 0 {
		return retainAll(target), nil
	}
	stagingPKs, err := fieldIndices(staging[0].Schema(), primaryKeys)
	if err != nil {
		return nil, err
	}
	stagingSet := buildKeySet(staging, stagingPKs)

	var out []arrow.RecordBatch
	for _, tb := range target {
		tPKs, err := fieldIndices(tb.Schema(), primaryKeys)
		if err != nil {
			releaseBatches(out)
			return nil, err
		}
		keep := make([]bool, tb.NumRows())
		for r := 0; r < int(tb.NumRows()); r++ {
			_, collides := stagingSet[rowKey(tb, tPKs, r)]
			keep[r] = !collides
		}
		filtered, err := filterByMask(ctx, tb, keep)
		if err != nil {
			releaseBatches(out)
			return nil, err
		}
		if filtered != nil {
			out = append(out, filtered)
		}
	}

	out = append(out, retainAll(staging)...)
	return out, nil
}

// deleteInsertBatches implements range-based delete+insert: target rows whose
// incremental key falls within [start, end] are dropped, then all staging rows
// are kept.
func deleteInsertBatches(ctx context.Context, target, staging []arrow.RecordBatch, opts destination.DeleteInsertOptions) ([]arrow.RecordBatch, error) {
	startC, startOK := boundCanonical(opts.IntervalStart, opts.IncrementalKeyType)
	endC, endOK := boundCanonical(opts.IntervalEnd, opts.IncrementalKeyType)

	var out []arrow.RecordBatch
	for _, tb := range target {
		keyIdx, ok := fieldIndex(tb.Schema(), opts.IncrementalKey)
		if !ok {
			releaseBatches(out)
			return nil, fmt.Errorf("incremental key %q not found in target", opts.IncrementalKey)
		}
		col := tb.Column(keyIdx)
		keep := make([]bool, tb.NumRows())
		for r := 0; r < int(tb.NumRows()); r++ {
			cellC, cellOK := cellCanonical(col, r, opts.IncrementalKeyType)
			// Keep a row unless it falls inside the deletion interval.
			inRange := cellOK && startOK && endOK &&
				canonicalCmp(cellC, startC) >= 0 && canonicalCmp(cellC, endC) <= 0
			keep[r] = !inRange
		}
		filtered, err := filterByMask(ctx, tb, keep)
		if err != nil {
			releaseBatches(out)
			return nil, err
		}
		if filtered != nil {
			out = append(out, filtered)
		}
	}

	out = append(out, retainAll(staging)...)
	return out, nil
}

// scd2Batches implements SCD2 (type-2) using the same semantics as the SQL
// destinations: close changed current rows, optionally soft-delete missing
// current rows, then insert new versions and net-new records.
func scd2Batches(ctx context.Context, target, staging []arrow.RecordBatch, opts destination.SCD2Options) ([]arrow.RecordBatch, error) {
	if len(staging) == 0 {
		return retainAll(target), nil
	}
	stagingSchema := staging[0].Schema()
	pkIdxs, err := fieldIndices(stagingSchema, opts.PrimaryKeys)
	if err != nil {
		return nil, err
	}
	dataCols := filterStrings(opts.Columns, destination.SCD2NonDataColumns(opts.PrimaryKeys))
	dataIdxs, err := fieldIndices(stagingSchema, dataCols)
	if err != nil {
		return nil, err
	}

	// Index staging rows by PK -> data signature (assumes one row per PK).
	stagingData := make(map[string]string)
	for _, sb := range staging {
		for r := 0; r < int(sb.NumRows()); r++ {
			stagingData[rowKey(sb, pkIdxs, r)] = rowKey(sb, dataIdxs, r)
		}
	}

	validToIdx, ok := fieldIndex(stagingSchema, destination.SCD2ValidToColumn)
	if !ok {
		return nil, fmt.Errorf("%s column missing", destination.SCD2ValidToColumn)
	}
	isCurrentIdx, ok := fieldIndex(stagingSchema, destination.SCD2IsCurrentColumn)
	if !ok {
		return nil, fmt.Errorf("%s column missing", destination.SCD2IsCurrentColumn)
	}

	// Tracks which PKs remain current after applying step 1/2 to the target.
	stillCurrent := make(map[string]struct{})
	var out []arrow.RecordBatch

	for _, tb := range target {
		tPKs, err := fieldIndices(tb.Schema(), opts.PrimaryKeys)
		if err != nil {
			releaseBatches(out)
			return nil, err
		}
		tData, err := fieldIndices(tb.Schema(), dataCols)
		if err != nil {
			releaseBatches(out)
			return nil, err
		}
		tIsCurrent, ok := fieldIndex(tb.Schema(), destination.SCD2IsCurrentColumn)
		if !ok {
			releaseBatches(out)
			return nil, fmt.Errorf("%s column missing in target", destination.SCD2IsCurrentColumn)
		}

		keepAsIs := make([]bool, tb.NumRows()) // unchanged rows, written verbatim
		closeRow := make([]bool, tb.NumRows()) // current rows to be closed out
		for r := 0; r < int(tb.NumRows()); r++ {
			current := boolValue(tb.Column(tIsCurrent), r)
			if !current {
				keepAsIs[r] = true
				continue
			}
			pk := rowKey(tb, tPKs, r)
			incoming, matched := stagingData[pk]
			switch {
			case matched && incoming != rowKey(tb, tData, r):
				closeRow[r] = true // changed: close this version
			case !matched && opts.IncrementalKey == "":
				closeRow[r] = true // missing from snapshot: soft-delete
			default:
				keepAsIs[r] = true // unchanged (or kept because incremental load)
				stillCurrent[pk] = struct{}{}
			}
		}

		asIs, err := filterByMask(ctx, tb, keepAsIs)
		if err != nil {
			releaseBatches(out)
			return nil, err
		}
		if asIs != nil {
			out = append(out, asIs)
		}

		toClose, err := filterByMask(ctx, tb, closeRow)
		if err != nil {
			releaseBatches(out)
			return nil, err
		}
		if toClose != nil {
			closed, err := closeSCD2Rows(toClose, validToIdx, isCurrentIdx, opts.Timestamp)
			toClose.Release()
			if err != nil {
				releaseBatches(out)
				return nil, err
			}
			out = append(out, closed)
		}
	}

	// Step 3: insert staging rows whose PK is not currently active in the target.
	for _, sb := range staging {
		keep := make([]bool, sb.NumRows())
		for r := 0; r < int(sb.NumRows()); r++ {
			_, active := stillCurrent[rowKey(sb, pkIdxs, r)]
			keep[r] = !active
		}
		filtered, err := filterByMask(ctx, sb, keep)
		if err != nil {
			releaseBatches(out)
			return nil, err
		}
		if filtered != nil {
			out = append(out, filtered)
		}
	}

	return out, nil
}

// closeSCD2Rows rewrites the _scd_valid_to and _scd_is_current columns of every
// row in the batch to mark the versions as closed at ts. All other columns are
// preserved exactly.
func closeSCD2Rows(batch arrow.RecordBatch, validToIdx, isCurrentIdx int, ts time.Time) (arrow.RecordBatch, error) {
	n := int(batch.NumRows())

	validToType, ok := batch.Schema().Field(validToIdx).Type.(*arrow.TimestampType)
	if !ok {
		return nil, fmt.Errorf("%s is not a timestamp column", destination.SCD2ValidToColumn)
	}
	tsBuilder := array.NewTimestampBuilder(memory.DefaultAllocator, validToType)
	defer tsBuilder.Release()
	for i := 0; i < n; i++ {
		tsBuilder.Append(arrow.Timestamp(ts.UnixMicro()))
	}
	validToArr := tsBuilder.NewArray()

	boolBuilder := array.NewBooleanBuilder(memory.DefaultAllocator)
	defer boolBuilder.Release()
	for i := 0; i < n; i++ {
		boolBuilder.Append(false)
	}
	isCurrentArr := boolBuilder.NewArray()

	cols := make([]arrow.Array, batch.NumCols())
	for i := 0; i < int(batch.NumCols()); i++ {
		switch i {
		case validToIdx:
			cols[i] = validToArr
		case isCurrentIdx:
			cols[i] = isCurrentArr
		default:
			c := batch.Column(i)
			c.Retain()
			cols[i] = c
		}
	}
	out := array.NewRecordBatch(batch.Schema(), cols, batch.NumRows())
	for _, c := range cols {
		c.Release()
	}
	return out, nil
}

func boolValue(arr arrow.Array, idx int) bool {
	if b, ok := arr.(*array.Boolean); ok && !b.IsNull(idx) {
		return b.Value(idx)
	}
	return false
}

func filterStrings(all, exclude []string) []string {
	skip := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		skip[strings.ToLower(e)] = struct{}{}
	}
	out := make([]string, 0, len(all))
	for _, c := range all {
		if _, ok := skip[strings.ToLower(c)]; !ok {
			out = append(out, c)
		}
	}
	return out
}

// canonical comparison helpers ---------------------------------------------

// canonicalValue normalizes a value into a float64 or string for ordered
// comparison, based on the incremental key's logical type.
func canonicalValue(v interface{}, keyType schema.DataType) (interface{}, bool) {
	if v == nil {
		return nil, false
	}
	switch keyType {
	case schema.TypeString, schema.TypeUUID:
		return fmt.Sprintf("%v", v), true
	case schema.TypeDate:
		t, ok := toTime(v)
		if !ok {
			return nil, false
		}
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return float64(d.Unix()), true
	case schema.TypeTimestamp, schema.TypeTimestampTZ:
		t, ok := toTime(v)
		if !ok {
			return nil, false
		}
		return float64(t.UTC().UnixMicro()), true
	default: // numeric
		switch n := v.(type) {
		case int16:
			return float64(n), true
		case int32:
			return float64(n), true
		case int64:
			return float64(n), true
		case float32:
			return float64(n), true
		case float64:
			return n, true
		default:
			return nil, false
		}
	}
}

func cellCanonical(arr arrow.Array, idx int, keyType schema.DataType) (interface{}, bool) {
	if arr.IsNull(idx) {
		return nil, false
	}
	return canonicalValue(arrowutil.Value(arr, idx), keyType)
}

func boundCanonical(v interface{}, keyType schema.DataType) (interface{}, bool) {
	return canonicalValue(v, keyType)
}

// canonicalCmp compares two canonical values (both float64 or both string).
func canonicalCmp(a, b interface{}) int {
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return strings.Compare(fmt.Sprintf("%v", a), fmt.Sprintf("%v", b))
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		default:
			return 0
		}
	case string:
		return strings.Compare(av, fmt.Sprintf("%v", b))
	default:
		return strings.Compare(fmt.Sprintf("%v", a), fmt.Sprintf("%v", b))
	}
}

func toTime(v interface{}) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case *time.Time:
		if t == nil {
			return time.Time{}, false
		}
		return *t, true
	case string:
		for _, layout := range []string{"2006-01-02", time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, t); err == nil {
				return parsed, true
			}
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}
