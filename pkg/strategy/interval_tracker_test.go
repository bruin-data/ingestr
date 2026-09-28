package strategy

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func intervalRecord(t *testing.T, dt arrow.DataType, values string) arrow.RecordBatch {
	t.Helper()
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })
	arr, _, err := array.FromJSON(pool, dt, strings.NewReader(values), array.WithUseNumber())
	require.NoError(t, err)
	defer arr.Release()
	return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: dt, Nullable: true}}, nil), []arrow.Array{arr}, int64(arr.Len()))
}

func TestIntervalTracker_AdditionalTypes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		dt       arrow.DataType
		values   string
		min, max any
	}{
		{"int8", arrow.PrimitiveTypes.Int8, `[7,-3,2]`, int64(-3), int64(7)},
		{"uint8", arrow.PrimitiveTypes.Uint8, `[250,3,128]`, int64(3), int64(250)},
		{"uint16", arrow.PrimitiveTypes.Uint16, `[65535,256,4]`, int64(4), int64(65535)},
		{"uint64", arrow.PrimitiveTypes.Uint64, `[18446744073709551615,9223372036854775807,9223372036854775808]`, uint64(9223372036854775807), uint64(18446744073709551615)},
		{"time64", arrow.FixedWidthTypes.Time64us, `["13:04:05.000006","02:03:04.000005","13:04:05.000007"]`, "02:03:04.000005", "13:04:05.000007"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := intervalRecord(t, tc.dt, tc.values)
			defer rec.Release()
			tk := NewIntervalTracker("id")
			tk.updateBounds(rec)
			require.NoError(t, tk.err)
			require.Equal(t, tc.min, tk.Min)
			require.Equal(t, tc.max, tk.Max)
		})
	}
}

func TestDeleteInsertStrategy_RejectsInvalidKeys(t *testing.T) {
	for _, tc := range []struct {
		name            string
		dt              arrow.DataType
		values, message string
	}{
		{"all_null", arrow.PrimitiveTypes.Int64, `[null,null]`, "NULL"},
		{"mixed_null", arrow.PrimitiveTypes.Int64, `[3,null,7]`, "NULL"},
		{"unsupported", arrow.FixedWidthTypes.Boolean, `[true,false]`, "unsupported"},
	} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/explicit=%t", tc.name, explicit), func(t *testing.T) {
				job, src, dest := minimalJob()
				job.Config.IncrementalKey = "id"
				if explicit {
					start, end := time.Now(), time.Now().Add(time.Hour)
					job.Config.IntervalStart, job.Config.IntervalEnd = &start, &end
				}
				src.readCh = mustClosedRecords(source.RecordBatchResult{Batch: intervalRecord(t, tc.dt, tc.values)})
				err := (&DeleteInsertStrategy{}).Execute(t.Context(), job)
				require.ErrorContains(t, err, tc.message)
				require.Empty(t, dest.diCalls)
				require.Empty(t, dest.dropCalls)
			})
		}
	}
}

func TestIntervalTracker_ChangingTypes(t *testing.T) {
	// Call synchronously so the original panic is reported as a test failure.
	tk := NewIntervalTracker("id")
	first := intervalRecord(t, arrow.PrimitiveTypes.Int64, `[9007199254740993]`)
	second := intervalRecord(t, arrow.PrimitiveTypes.Float64, `[1.5]`)
	defer first.Release()
	defer second.Release()
	tk.updateBounds(first)
	require.NotPanics(t, func() { tk.updateBounds(second) })
	require.ErrorContains(t, tk.err, "changed bound type")

	job, src, dest := minimalJob()
	job.Config.IncrementalKey = "id"
	first.Retain()
	second.Retain()
	src.readCh = mustClosedRecords(source.RecordBatchResult{Batch: first}, source.RecordBatchResult{Batch: second})
	require.ErrorContains(t, (&DeleteInsertStrategy{}).Execute(t.Context(), job), "changed bound type")
	require.Empty(t, dest.diCalls)
	require.Empty(t, dest.dropCalls)
}

func TestDeleteInsertStrategy_EmptyInput(t *testing.T) {
	for _, bounds := range []int{0, 1, 2} {
		job, src, dest := minimalJob()
		job.Config.IncrementalKey = "id"
		if bounds > 0 {
			start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			job.Config.IntervalStart = &start
		}
		if bounds == 2 {
			end := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
			job.Config.IntervalEnd = &end
		}
		src.readCh = mustClosedRecords()
		require.NoError(t, (&DeleteInsertStrategy{}).Execute(t.Context(), job))
		if bounds == 2 {
			require.Len(t, dest.diCalls, 1)
		} else {
			require.Empty(t, dest.diCalls)
		}
		require.Len(t, dest.dropCalls, 1)
	}
}
