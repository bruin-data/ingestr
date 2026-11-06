package postgres_cdc

import (
	"cmp"
	"strconv"

	"github.com/bruin-data/ingestr/pkg/source"
)

type chunkWALRange struct {
	table   string
	indices []int
	after   []int64
	upper   []int64
	skipped int64
}

func newChunkWALRange(table source.SourceTableInfo, chunk snapshotChunk) *chunkWALRange {
	if chunk.Done || !chunkKeysOrderedIntegers(table) || len(chunk.LastKey) != len(table.PrimaryKeys) || len(chunk.UpperKey) != len(table.PrimaryKeys) {
		return nil
	}
	r := &chunkWALRange{
		table: table.Name, indices: pkColumnIndices(table.Schema.Columns, table.PrimaryKeys),
		after: make([]int64, len(table.PrimaryKeys)), upper: make([]int64, len(table.PrimaryKeys)),
	}
	for i := range table.PrimaryKeys {
		var err error
		r.after[i], err = strconv.ParseInt(chunk.LastKey[i], 10, 64)
		if err != nil {
			return nil
		}
		r.upper[i], err = strconv.ParseInt(chunk.UpperKey[i], 10, 64)
		if err != nil {
			return nil
		}
	}
	return r
}

func (r *chunkWALRange) filter(group DecodedChanges) []Change {
	if r == nil || group.TableName != r.table {
		return group.Changes
	}
	var kept []Change
	for i, change := range group.Changes {
		if r.coveredByLaterChunk(change) {
			if kept == nil {
				kept = make([]Change, 0, len(group.Changes)-1)
				kept = append(kept, group.Changes[:i]...)
			}
			r.skipped++
		} else if kept != nil {
			kept = append(kept, change)
		}
	}
	if kept == nil {
		return group.Changes
	}
	return kept
}

// A later snapshot observes writes in the unread range. Deletes and key moves
// must still remove rows already materialized by WAL or an interrupted chunk.
func (r *chunkWALRange) coveredByLaterChunk(change Change) bool {
	if change.Operation != "INSERT" && change.Operation != "UPDATE" {
		return false
	}
	after, upper := 0, 0
	for i, index := range r.indices {
		value, ok := chunkWALInteger(columnValueAt(change.Values, index))
		if !ok {
			return false
		}
		if change.Operation == "UPDATE" && change.OldValues != nil {
			old, ok := chunkWALInteger(columnValueAt(change.OldValues, index))
			if !ok || old != value {
				return false
			}
		}
		if after == 0 {
			after = cmp.Compare(value, r.after[i])
		}
		if upper == 0 {
			upper = cmp.Compare(value, r.upper[i])
		}
	}
	return after > 0 && upper <= 0
}

func chunkWALInteger(value any) (int64, bool) {
	switch value := value.(type) {
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case int:
		return int64(value), true
	default:
		return 0, false
	}
}
