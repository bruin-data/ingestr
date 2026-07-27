package postgres_cdc

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestSendResultCancellationReleasesUnsentBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var releases atomic.Int64
	batch := &lifecycleReleaseCountingBatch{releases: &releases}
	results := make(chan source.RecordBatchResult)

	if err := sendResult(ctx, results, source.RecordBatchResult{Batch: batch}); err == nil {
		t.Fatal("sendResult() error = nil, want context cancellation")
	}
	if got := releases.Load(); got != 1 {
		t.Fatalf("release count = %d, want 1", got)
	}
}

type lifecycleReleaseCountingBatch struct{ releases *atomic.Int64 }

func (b *lifecycleReleaseCountingBatch) MarshalJSON() ([]byte, error) { return []byte("null"), nil }
func (b *lifecycleReleaseCountingBatch) Release()                     { b.releases.Add(1) }
func (b *lifecycleReleaseCountingBatch) Retain()                      {}
func (b *lifecycleReleaseCountingBatch) Schema() *arrow.Schema        { return nil }
func (b *lifecycleReleaseCountingBatch) NumRows() int64               { return 0 }
func (b *lifecycleReleaseCountingBatch) NumCols() int64               { return 0 }
func (b *lifecycleReleaseCountingBatch) Columns() []arrow.Array       { return nil }
func (b *lifecycleReleaseCountingBatch) Column(int) arrow.Array       { return nil }
func (b *lifecycleReleaseCountingBatch) ColumnName(int) string        { return "" }
func (b *lifecycleReleaseCountingBatch) SetColumn(int, arrow.Array) (arrow.RecordBatch, error) {
	return nil, nil
}
func (b *lifecycleReleaseCountingBatch) NewSlice(int64, int64) arrow.RecordBatch { return b }
