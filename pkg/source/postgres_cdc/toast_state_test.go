package postgres_cdc

import (
	"fmt"
	"os"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func TestToastAcrossSourceBatches(t *testing.T) {
	for _, spill := range []bool{false, true} {
		for _, initial := range []interface{}{"large payload", nil} {
			t.Run(fmtToastCase(spill, initial), func(t *testing.T) {
				a := newBatchAccumulator(1, map[string]*schema.TableSchema{"a": fillTestSchema(), "b": fillTestSchema()})
				if spill {
					a.toast.limit = 1
				}
				t.Cleanup(func() { require.NoError(t, a.toast.close()) })
				out := make(chan source.RecordBatchResult, 1)
				flush := func(table string, c Change) source.RecordBatchResult {
					t.Helper()
					a.add(table, []Change{c}, c.LSN)
					require.NoError(t, a.flushReadyContext(t.Context(), out, nil))
					res := <-out
					t.Cleanup(res.Batch.Release)
					return res
				}
				flush("a", Change{Operation: "INSERT", LSN: 1, Values: []interface{}{int64(1), initial, "first"}})
				// No old tuple is sent for a normal UPDATE under default replica identity.
				update := func(lsn pglogrepl.LSN) Change {
					return Change{Operation: "UPDATE", LSN: lsn, Values: []interface{}{int64(1), tupleUnchangedMarker, "later"}}
				}
				other := flush("b", update(2))
				require.Equal(t, `["config_data"]`, unchangedValueAt(t, other.Batch, 0), "table identities must not share cached rows")
				filled := flush("a", update(3))
				require.Equal(t, `[]`, unchangedValueAt(t, filled.Batch, 0))
				col := filled.Batch.Column(1).(*array.String)
				if initial == nil {
					require.True(t, col.IsNull(0))
				} else {
					require.Equal(t, initial, col.Value(0))
				}
				require.Equal(t, spill, a.toast.tx != nil)
				if spill {
					path := a.toast.path
					require.FileExists(t, path)
				}
				a.durable = func() pglogrepl.LSN { return 3 }
				afterAck := flush("a", update(4))
				require.Equal(t, `["config_data"]`, unchangedValueAt(t, afterAck.Batch, 0), "durable values can fall back to the destination")
			})
		}
	}
}

func fmtToastCase(spill bool, value interface{}) string {
	mode := "memory"
	if spill {
		mode = "disk"
	}
	if value == nil {
		return mode + "/null"
	}
	return mode + "/value"
}

func TestToastStateDeleteTruncateAndCleanup(t *testing.T) {
	for _, spill := range []bool{false, true} {
		t.Run(fmtToastCase(spill, "value"), func(t *testing.T) {
			s := newToastState()
			if spill {
				s.limit = 1
			}
			t.Cleanup(func() { require.NoError(t, s.close()) })
			ctx := t.Context()
			for _, table := range []string{"a", "b"} {
				for _, key := range []string{"1", "2"} {
					require.NoError(t, s.put(ctx, table, key, []interface{}{"payload", nil, tupleUnchangedMarker}, 7))
				}
			}
			require.NoError(t, s.delete(ctx, "a", "1"))
			values, err := s.get(ctx, "a", "1")
			require.NoError(t, err)
			require.Nil(t, values)
			require.NoError(t, s.truncate(ctx, "a"))
			values, err = s.get(ctx, "a", "2")
			require.NoError(t, err)
			require.Nil(t, values)
			values, err = s.get(ctx, "b", "2")
			require.NoError(t, err)
			require.Equal(t, []interface{}{"payload", nil, tupleUnchangedMarker}, values)
			require.NoError(t, s.prune(ctx, 6))
			values, err = s.get(ctx, "b", "2")
			require.NoError(t, err)
			require.NotEmpty(t, values, "unacknowledged changes must survive")
			path := s.path
			require.NoError(t, s.close())
			if path != "" {
				_, err := os.Stat(path)
				require.True(t, os.IsNotExist(err))
			}
		})
	}
}

func TestToastKeyMoveRequiresCompleteImage(t *testing.T) {
	for _, fullOld := range []bool{false, true} {
		for _, earlierBatch := range []bool{false, true} {
			t.Run(fmt.Sprintf("full_old=%v/earlier_batch=%v", fullOld, earlierBatch), func(t *testing.T) {
				a := newBatchAccumulator(1, map[string]*schema.TableSchema{"a": fillTestSchema()})
				t.Cleanup(func() { require.NoError(t, a.toast.close()) })
				out := make(chan source.RecordBatchResult, 1)
				if earlierBatch {
					a.add("a", []Change{{Operation: "INSERT", LSN: 1, Values: []interface{}{int64(1), "payload", "first"}}}, 1)
					require.NoError(t, a.flushAll(out, nil))
					(<-out).Batch.Release()
				}
				old := []interface{}{int64(1), nil, nil}
				if fullOld {
					old[1] = "payload"
				}
				a.add("a", []Change{{Operation: "UPDATE", LSN: 2, Sequence: 2, Values: []interface{}{int64(2), tupleUnchangedMarker, "moved"}, OldValues: old}}, 2)
				err := a.flushAll(out, nil)
				if !fullOld && !earlierBatch {
					require.ErrorContains(t, err, "REPLICA IDENTITY FULL")
					require.ErrorContains(t, err, "config_data")
					require.Empty(t, out, "neither the old-key delete nor the new-key row may escape")
					return
				}
				require.NoError(t, err)
				res := <-out
				defer res.Batch.Release()
				require.EqualValues(t, 2, res.Batch.NumRows())
				require.Equal(t, int64(2), res.Batch.Column(0).(*array.Int64).Value(1))
				require.Equal(t, "payload", res.Batch.Column(1).(*array.String).Value(1))
				require.Equal(t, "[]", unchangedValueAt(t, res.Batch, 1))
			})
		}
	}
}

func TestToastKeyMoveAfterRelationChangeRequestsSchemaRebuild(t *testing.T) {
	a := newBatchAccumulator(1, map[string]*schema.TableSchema{"public.t": fillTestSchema()})
	t.Cleanup(func() { require.NoError(t, a.toast.close()) })
	a.add("public.t", []Change{{
		Operation: "UPDATE",
		LSN:       2,
		Sequence:  2,
		Values:    []interface{}{int64(2), tupleRelationMissingMarker, "moved"},
		OldValues: []interface{}{int64(1), nil, nil},
	}}, 2)

	err := a.flushAll(make(chan source.RecordBatchResult, 1), nil)
	var schemaErr *SchemaChangedError
	require.ErrorAs(t, err, &schemaErr)
	require.Equal(t, "public.t", schemaErr.Table)
	require.Equal(t, "config_data", schemaErr.Column)
}
