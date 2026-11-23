package postgres_cdc

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func chunkWALTestTable() source.SourceTableInfo {
	return source.SourceTableInfo{
		Name: "public.items", PrimaryKeys: []string{"id"},
		Schema: addCDCColumns(&schema.TableSchema{PrimaryKeys: []string{"id"}, Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeString},
		}}),
	}
}

func TestChunkWALRangeRetainsDeletesAndKeyMoves(t *testing.T) {
	table := chunkWALTestTable()
	r := newChunkWALRange(table, snapshotChunk{LastKey: []string{"5"}, UpperKey: []string{"100"}})
	require.NotNil(t, r)
	for _, tc := range []struct {
		name    string
		change  Change
		skipped bool
	}{
		{name: "copied key", change: Change{Operation: "INSERT", Values: []any{int64(5)}}},
		{name: "future key", change: Change{Operation: "INSERT", Values: []any{int64(6)}}, skipped: true},
		{name: "upper key", change: Change{Operation: "INSERT", Values: []any{int64(100)}}, skipped: true},
		{name: "beyond fixed upper", change: Change{Operation: "INSERT", Values: []any{int64(101)}}},
		{name: "future stable update", change: Change{Operation: "UPDATE", Values: []any{int64(75), "new"}, OldValues: []any{int64(75), "old"}}, skipped: true},
		{name: "update without old tuple", change: Change{Operation: "UPDATE", Values: []any{int64(75), "new"}}, skipped: true},
		{name: "future delete", change: Change{Operation: "DELETE", Values: []any{int64(75)}}},
		{name: "copied delete", change: Change{Operation: "DELETE", Values: []any{int64(3)}}},
		{name: "move forward", change: Change{Operation: "UPDATE", Values: []any{int64(75)}, OldValues: []any{int64(3)}}},
		{name: "move backward", change: Change{Operation: "UPDATE", Values: []any{int64(3)}, OldValues: []any{int64(75)}}},
		{name: "move between future keys", change: Change{Operation: "UPDATE", Values: []any{int64(75)}, OldValues: []any{int64(80)}}},
		{name: "missing old key", change: Change{Operation: "UPDATE", Values: []any{int64(75)}, OldValues: []any{nil}}},
		{name: "unchanged key marker", change: Change{Operation: "UPDATE", Values: []any{tupleUnchangedMarker}, OldValues: []any{int64(75)}}},
		{name: "unknown key representation", change: Change{Operation: "INSERT", Values: []any{"75"}}},
		{name: "missing key", change: Change{Operation: "INSERT"}},
		{name: "truncate", change: Change{Operation: "TRUNCATE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := DecodedChanges{TableName: table.Name, Changes: []Change{tc.change}}
			filtered := r.filter(group)
			if tc.skipped {
				require.Empty(t, filtered)
			} else {
				require.Equal(t, group.Changes, filtered)
			}
			group.TableName = "public.other"
			require.Equal(t, group.Changes, r.filter(group), "other tables may already be complete or have an unknown upper bound")
			require.Equal(t, group.Changes, (*chunkWALRange)(nil).filter(group), "serial and continuous readers have no active range")
		})
	}
}

func TestChunkWALRangeMatchesCompositePostgresOrdering(t *testing.T) {
	table := source.SourceTableInfo{Name: "public.items", PrimaryKeys: []string{"tenant", "id"}, Schema: &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "tenant", DataType: schema.TypeInt32},
	}}}
	chunk := snapshotChunk{LastKey: []string{"-2", "50"}, UpperKey: []string{"3", "-10"}}
	r := newChunkWALRange(table, chunk)
	require.NotNil(t, r)
	for _, tc := range []struct {
		id      int64
		tenant  int32
		skipped bool
	}{
		{id: 50, tenant: -2},
		{id: 51, tenant: -2, skipped: true},
		{id: -100, tenant: -1, skipped: true},
		{id: 100, tenant: 2, skipped: true},
		{id: -10, tenant: 3, skipped: true},
		{id: -9, tenant: 3},
		{id: -100, tenant: 4},
	} {
		change := Change{Operation: "INSERT", Values: []any{tc.id, tc.tenant}}
		require.Equal(t, tc.skipped, r.coveredByLaterChunk(change), "%+v", tc)
	}
	chunk.Done = true
	require.Nil(t, newChunkWALRange(table, chunk), "no later snapshot will observe writes after the final chunk")
	chunk.Done, chunk.UpperKey = false, nil
	require.Nil(t, newChunkWALRange(table, chunk))
	chunk.UpperKey = []string{"3", "9223372036854775808"}
	require.Nil(t, newChunkWALRange(table, chunk))
	chunk.UpperKey = []string{"3", "-10"}
	table.Schema.Columns[0].DataType = schema.TypeString
	require.Nil(t, newChunkWALRange(table, chunk), "text ordering depends on the source collation")
}

func TestChunkWALFilterPreservesJournalReplayAndTOAST(t *testing.T) {
	table := chunkWALTestTable()
	groups := []DecodedChanges{{TableName: table.Name, LSN: 100, Changes: []Change{
		{Operation: "INSERT", LSN: 100, Sequence: 2, Values: []any{int64(75), "future insert"}},
		{Operation: "UPDATE", LSN: 100, Sequence: 4, Values: []any{int64(75), tupleUnchangedMarker}, OldValues: []any{int64(75), "future insert"}},
		{Operation: "UPDATE", LSN: 100, Sequence: 6, Values: []any{int64(80), tupleUnchangedMarker}, OldValues: []any{int64(3), "moved value"}},
		{Operation: "DELETE", LSN: 100, Sequence: 8, Values: []any{int64(80), "moved value"}},
		{Operation: "INSERT", LSN: 100, Sequence: 10, Values: []any{int64(101), "beyond upper"}},
	}}}
	store := newChunkTestStore()
	journal := &chunkJournal{store: store, epoch: "epoch"}
	require.NoError(t, journal.load(t.Context(), 0))
	require.NoError(t, journal.append(t.Context(), groups, 100))
	for range 2 {
		results := make(chan source.RecordBatchResult, 1)
		r := &chunkReader{
			state: chunkTestState(store.identity), multi: true, results: results,
			walRange: newChunkWALRange(table, snapshotChunk{LastKey: []string{"5"}, UpperKey: []string{"100"}}),
			accum:    newBatchAccumulator(1, map[string]*schema.TableSchema{table.Name: table.Schema}),
		}
		require.NoError(t, journal.replay(t.Context(), 0, func(replayed []DecodedChanges) error {
			require.Equal(t, groups, replayed, "capture persists every change before the current chunk establishes its frontier")
			return r.apply(t.Context(), replayed)
		}))
		result := <-results
		ids := result.Batch.Column(0).(*array.Int64)
		deleted := result.Batch.Column(result.Batch.Schema().FieldIndices(CDCDeletedColumn)[0]).(*array.Boolean)
		values := result.Batch.Column(1).(*array.String)
		require.Equal(t, []int64{3, 80, 80, 101}, ids.Int64Values())
		require.True(t, deleted.Value(0), "moving forward must remove the copied old key")
		require.Equal(t, "moved value", values.Value(1), "a retained key move must retain its complete row image")
		require.True(t, deleted.Value(2), "the later delete must remove the future row materialized by the key move")
		require.Equal(t, int64(2), r.walRange.skipped)
		result.Batch.Release()
		require.NoError(t, r.accum.toast.close())
	}
}
