package destination

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/source"
)

type boundaryWriteDestination struct {
	Destination
	write func(context.Context, <-chan source.RecordBatchResult, WriteOptions) error
}

type conditionalBoundaryDestination struct {
	*boundaryWriteDestination
	currentIncarnation string
	unconditionalCalls atomic.Int64
	conditionalCalls   atomic.Int64
}

type unconditionalBoundaryDestination struct {
	*boundaryWriteDestination
	unconditionalCalls atomic.Int64
}

func (d *unconditionalBoundaryDestination) GetScheme() string { return "unconditional" }

func (d *unconditionalBoundaryDestination) TruncateCDCTable(context.Context, string) error {
	d.unconditionalCalls.Add(1)
	return nil
}

func (d *conditionalBoundaryDestination) TruncateCDCTable(context.Context, string) error {
	d.unconditionalCalls.Add(1)
	return nil
}

func (d *conditionalBoundaryDestination) TruncateCDCTableIfIncarnation(_ context.Context, _, expected string) error {
	d.conditionalCalls.Add(1)
	if expected != d.currentIncarnation {
		return errors.New("target incarnation changed")
	}
	return nil
}

func (d *boundaryWriteDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts WriteOptions) error {
	return d.write(ctx, records, opts)
}

func TestWriteWithTruncateBoundariesEarlyWriteErrorReleasesAllBatches(t *testing.T) {
	wantErr := errors.New("write failed before consuming segment")
	dest := &boundaryWriteDestination{
		write: func(context.Context, <-chan source.RecordBatchResult, WriteOptions) error {
			return wantErr
		},
	}

	releases := make([]atomic.Int64, 3)
	records := make(chan source.RecordBatchResult, len(releases))
	for i := range releases {
		records <- source.RecordBatchResult{Batch: &boundaryReleaseCountingBatch{releases: &releases[i]}}
	}
	close(records)

	_, err := WriteWithTruncateBoundaries(context.Background(), dest, records, WriteOptions{Table: "items"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WriteWithTruncateBoundaries() error = %v, want %v", err, wantErr)
	}
	assertBoundaryReleaseCounts(t, releases)
}

func TestWriteWithTruncateBoundariesWriteErrorDrainsProducerWithoutDoubleRelease(t *testing.T) {
	wantErr := errors.New("write failed after first batch")
	dest := &boundaryWriteDestination{
		write: func(_ context.Context, records <-chan source.RecordBatchResult, _ WriteOptions) error {
			result := <-records
			result.Batch.Release()
			return wantErr
		},
	}

	releases := make([]atomic.Int64, 4)
	records := make(chan source.RecordBatchResult)
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		defer close(records)
		for i := range releases {
			records <- source.RecordBatchResult{Batch: &boundaryReleaseCountingBatch{releases: &releases[i]}}
		}
	}()

	_, err := WriteWithTruncateBoundaries(context.Background(), dest, records, WriteOptions{Table: "items"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WriteWithTruncateBoundaries() error = %v, want %v", err, wantErr)
	}
	select {
	case <-producerDone:
	case <-time.After(time.Second):
		t.Fatal("source producer remained blocked after destination write failure")
	}
	assertBoundaryReleaseCounts(t, releases)
}

func TestWriteWithTruncateBoundariesCanceledWriteReleasesAllBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dest := &boundaryWriteDestination{
		write: func(ctx context.Context, _ <-chan source.RecordBatchResult, _ WriteOptions) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	releases := make([]atomic.Int64, 3)
	records := make(chan source.RecordBatchResult, len(releases))
	for i := range releases {
		records <- source.RecordBatchResult{Batch: &boundaryReleaseCountingBatch{releases: &releases[i]}}
	}
	close(records)

	_, err := WriteWithTruncateBoundaries(ctx, dest, records, WriteOptions{Table: "items"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WriteWithTruncateBoundaries() error = %v, want context.Canceled", err)
	}
	assertBoundaryReleaseCounts(t, releases)
}

func TestWriteWithTruncateBoundariesCancelsOpenInputBeforeDrain(t *testing.T) {
	wantErr := errors.New("write failed")
	dest := &boundaryWriteDestination{
		write: func(context.Context, <-chan source.RecordBatchResult, WriteOptions) error {
			return wantErr
		},
	}
	var releases atomic.Int64
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: &boundaryReleaseCountingBatch{releases: &releases}}

	done := make(chan error, 1)
	go func() {
		_, err := WriteWithTruncateBoundariesAfterCancel(
			context.Background(),
			dest,
			records,
			WriteOptions{Table: "items"},
			func() { close(records) },
		)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("WriteWithTruncateBoundariesAfterCancel() error = %v, want %v", err, wantErr)
		}
	case <-time.After(time.Second):
		t.Fatal("write cleanup drained the open input before canceling its owner")
	}
	if got := releases.Load(); got != 1 {
		t.Fatalf("batch release count = %d, want 1", got)
	}
}

func TestWriteWithTruncateBoundariesReturnsWhenCanceledProducerNeverCloses(t *testing.T) {
	wantErr := errors.New("write failed")
	dest := &boundaryWriteDestination{write: func(context.Context, <-chan source.RecordBatchResult, WriteOptions) error {
		return wantErr
	}}
	var releases atomic.Int64
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: &boundaryReleaseCountingBatch{releases: &releases}}

	started := time.Now()
	_, err := WriteWithTruncateBoundariesAfterCancel(
		context.Background(), dest, records, WriteOptions{Table: "items"}, func() {},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("WriteWithTruncateBoundariesAfterCancel() error = %v, want %v", err, wantErr)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("write cleanup waited %s for a canceled producer that never closed", elapsed)
	}
	if got := releases.Load(); got != 1 {
		t.Fatalf("batch release count = %d, want 1", got)
	}
}

func TestWriteWithTruncateBoundariesRefusesRecreatedManagedTarget(t *testing.T) {
	dest := &conditionalBoundaryDestination{
		boundaryWriteDestination: &boundaryWriteDestination{write: func(_ context.Context, records <-chan source.RecordBatchResult, _ WriteOptions) error {
			for result := range records {
				releaseCDCResult(result)
			}
			return nil
		}},
		currentIncarnation: "replacement-uuid",
	}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Truncate: true, CDCWALTruncate: true}
	close(records)

	_, err := WriteWithTruncateBoundaries(context.Background(), dest, records, WriteOptions{
		Table: "items", CDCExpectedIncarnation: "bound-original-uuid",
	})
	if err == nil || !strings.Contains(err.Error(), "target incarnation changed") {
		t.Fatalf("WriteWithTruncateBoundaries() error = %v, want incarnation change", err)
	}
	if got := dest.unconditionalCalls.Load(); got != 0 {
		t.Fatalf("unconditional truncate calls = %d, want 0", got)
	}
	if got := dest.conditionalCalls.Load(); got != 1 {
		t.Fatalf("conditional truncate calls = %d, want 1", got)
	}
}

func TestWriteWithTruncateBoundariesRejectsUnsupportedManagedTruncateWithoutMutation(t *testing.T) {
	dest := &unconditionalBoundaryDestination{boundaryWriteDestination: &boundaryWriteDestination{
		write: func(_ context.Context, records <-chan source.RecordBatchResult, _ WriteOptions) error {
			for result := range records {
				releaseCDCResult(result)
			}
			return nil
		},
	}}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Truncate: true, CDCWALTruncate: true}
	close(records)

	_, err := WriteWithTruncateBoundaries(context.Background(), dest, records, WriteOptions{
		Table: "items", CDCExpectedIncarnation: "bound-original-uuid",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot conditionally truncate managed CDC targets") {
		t.Fatalf("WriteWithTruncateBoundaries() error = %v, want unsupported conditional truncate", err)
	}
	if got := dest.unconditionalCalls.Load(); got != 0 {
		t.Fatalf("unconditional truncate calls = %d, want 0", got)
	}
}

func assertBoundaryReleaseCounts(t *testing.T, releases []atomic.Int64) {
	t.Helper()
	for i := range releases {
		if got := releases[i].Load(); got != 1 {
			t.Fatalf("batch %d release count = %d, want 1", i, got)
		}
	}
}

type boundaryReleaseCountingBatch struct {
	releases *atomic.Int64
}

func (b *boundaryReleaseCountingBatch) MarshalJSON() ([]byte, error) { return []byte("null"), nil }
func (b *boundaryReleaseCountingBatch) Release()                     { b.releases.Add(1) }
func (b *boundaryReleaseCountingBatch) Retain()                      {}
func (b *boundaryReleaseCountingBatch) Schema() *arrow.Schema        { return nil }
func (b *boundaryReleaseCountingBatch) NumRows() int64               { return 0 }
func (b *boundaryReleaseCountingBatch) NumCols() int64               { return 0 }
func (b *boundaryReleaseCountingBatch) Columns() []arrow.Array       { return nil }
func (b *boundaryReleaseCountingBatch) Column(int) arrow.Array       { return nil }
func (b *boundaryReleaseCountingBatch) ColumnName(int) string        { return "" }
func (b *boundaryReleaseCountingBatch) SetColumn(int, arrow.Array) (arrow.RecordBatch, error) {
	return nil, nil
}
func (b *boundaryReleaseCountingBatch) NewSlice(int64, int64) arrow.RecordBatch { return b }
