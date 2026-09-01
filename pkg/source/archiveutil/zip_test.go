package archiveutil

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsZIP(t *testing.T) {
	tests := []struct {
		path  string
		isZIP bool
	}{
		{path: "release.zip", isZIP: true},
		{path: "release.ZIP", isZIP: true},
		{path: "regular.csv", isZIP: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.isZIP, IsZIP(tt.path))
		})
	}
}

func TestForwardBatchesIgnoresErrorsAfterLimit(t *testing.T) {
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.AppendValues([]int64{1, 2}, nil)
	values := builder.NewArray()
	builder.Release()
	record := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{values}, 2)
	values.Release()

	batches := make(chan source.RecordBatchResult, 2)
	batches <- source.RecordBatchResult{Batch: record}
	batches <- source.RecordBatchResult{Err: errors.New("malformed row after limit")}
	close(batches)
	destination := make(chan source.RecordBatchResult, 1)

	rows, err := ForwardBatches(context.Background(), destination, batches, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, rows)

	result := <-destination
	require.NoError(t, result.Err)
	assert.Equal(t, int64(1), result.Batch.NumRows())
	result.Batch.Release()
}
