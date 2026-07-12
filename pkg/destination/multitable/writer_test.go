package multitable

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type releaseCountingBatch struct{ releases *atomic.Int64 }

func (b *releaseCountingBatch) MarshalJSON() ([]byte, error) { return []byte("null"), nil }
func (b *releaseCountingBatch) Release()                     { b.releases.Add(1) }
func (b *releaseCountingBatch) Retain()                      {}
func (b *releaseCountingBatch) Schema() *arrow.Schema        { return nil }
func (b *releaseCountingBatch) NumRows() int64               { return 0 }
func (b *releaseCountingBatch) NumCols() int64               { return 0 }
func (b *releaseCountingBatch) Columns() []arrow.Array       { return nil }
func (b *releaseCountingBatch) Column(int) arrow.Array       { return nil }
func (b *releaseCountingBatch) ColumnName(int) string        { return "" }
func (b *releaseCountingBatch) SetColumn(int, arrow.Array) (arrow.RecordBatch, error) {
	return nil, nil
}
func (b *releaseCountingBatch) NewSlice(int64, int64) arrow.RecordBatch { return b }

type fakeDestination struct {
	writeFn func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error
}

type truncatingFakeDestination struct {
	*fakeDestination
	mu        sync.Mutex
	truncated []string
}

func (d *truncatingFakeDestination) TruncateTable(_ context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.truncated = append(d.truncated, table)
	return nil
}

func (d *fakeDestination) Schemes() []string                             { return nil }
func (d *fakeDestination) Connect(ctx context.Context, uri string) error { return nil }
func (d *fakeDestination) Close(ctx context.Context) error               { return nil }
func (d *fakeDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	return nil
}

func (d *fakeDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *fakeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if d.writeFn != nil {
		return d.writeFn(ctx, records, opts)
	}
	for result := range records {
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func (d *fakeDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	return nil
}

func (d *fakeDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return nil
}

func (d *fakeDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return nil
}

func (d *fakeDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return nil
}
func (d *fakeDestination) DropTable(ctx context.Context, table string) error { return nil }
func (d *fakeDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (d *fakeDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return nil, nil
}

func (d *fakeDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}
func (d *fakeDestination) GetScheme() string                  { return "fake" }
func (d *fakeDestination) SupportsReplaceStrategy() bool      { return true }
func (d *fakeDestination) SupportsAppendStrategy() bool       { return true }
func (d *fakeDestination) SupportsMergeStrategy() bool        { return true }
func (d *fakeDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *fakeDestination) SupportsSCD2Strategy() bool         { return true }
func (d *fakeDestination) SupportsAtomicSwap() bool           { return true }

func TestWriteCancelsOtherTablesOnFirstError(t *testing.T) {
	dest := &fakeDestination{
		writeFn: func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
			switch opts.Table {
			case "dataset.fail":
				result, ok := <-records
				if !ok {
					return nil
				}
				if result.Err != nil {
					return result.Err
				}
				return errors.New("boom")
			case "dataset.wait":
				<-ctx.Done()
				return ctx.Err()
			default:
				return nil
			}
		},
	}

	records := make(chan source.RecordBatchResult, 16)
	for i := 0; i < 16; i++ {
		records <- source.RecordBatchResult{TableName: "fail"}
	}
	close(records)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := Write(ctx, dest, records, destination.MultiTableWriteOptions{
		TableConfigs: map[string]destination.TableWriteConfig{
			"fail": {DestTable: "dataset.fail"},
			"wait": {DestTable: "dataset.wait"},
		},
		Parallelism: 4,
	})
	if err == nil {
		t.Fatal("Write returned nil error, want failure")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Write error = %v, want boom", err)
	}
}

func TestWritePropagatesSkipCDCResume(t *testing.T) {
	var got destination.WriteOptions
	dest := &fakeDestination{writeFn: func(_ context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
		got = opts
		for result := range records {
			if result.Batch != nil {
				result.Batch.Release()
			}
		}
		return nil
	}}
	records := make(chan source.RecordBatchResult)
	close(records)

	_, err := WriteWithResult(t.Context(), dest, records, destination.MultiTableWriteOptions{
		TableConfigs: map[string]destination.TableWriteConfig{
			"events": {DestTable: "raw.events", SkipCDCResume: true, CDCExpectedIncarnation: "bound-table-uuid"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.SkipCDCResume {
		t.Fatal("managed multi-table write did not propagate SkipCDCResume")
	}
	if got.CDCExpectedIncarnation != "bound-table-uuid" {
		t.Fatalf("managed multi-table expected incarnation = %q, want bound-table-uuid", got.CDCExpectedIncarnation)
	}
}

// TestWriteRouterDeadlock reproduces the exact deadlock pattern:
// interleaved records for two tables, one writer fails after first record.
// Without cancellation propagation, the router blocks sending to the failed
// table's full channel, starving the healthy table forever.
func TestWriteRouterDeadlock(t *testing.T) {
	dest := &fakeDestination{
		writeFn: func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
			if opts.Table == "dataset.fail" {
				// Consume one record then fail
				result, ok := <-records
				if !ok {
					return nil
				}
				if result.Err != nil {
					return result.Err
				}
				return errors.New("write failed")
			}
			// Healthy table: consume records normally until channel closes
			for result := range records {
				if result.Err != nil {
					return result.Err
				}
			}
			return nil
		},
	}

	// Feed interleaved records for both tables via a goroutine so the test
	// doesn't block if the router stops consuming.
	records := make(chan source.RecordBatchResult)
	go func() {
		defer close(records)
		for i := 0; i < 100; i++ {
			name := "fail"
			if i%2 == 1 {
				name = "healthy"
			}
			select {
			case records <- source.RecordBatchResult{TableName: name}:
			case <-time.After(5 * time.Second):
				return
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		done <- Write(context.Background(), dest, records, destination.MultiTableWriteOptions{
			TableConfigs: map[string]destination.TableWriteConfig{
				"fail":    {DestTable: "dataset.fail"},
				"healthy": {DestTable: "dataset.healthy"},
			},
			Parallelism: 4,
		})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Write returned nil error, want failure")
		}
		if !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("Write error = %v, want 'write failed'", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Write deadlocked: router blocked on failed table's channel, starving healthy table")
	}
}

func TestWriteFailureCancelsProducerAndReleasesEveryBatchOnce(t *testing.T) {
	dest := &fakeDestination{writeFn: func(context.Context, <-chan source.RecordBatchResult, destination.WriteOptions) error {
		return errors.New("early write failure")
	}}
	readCtx, cancelRead := context.WithCancel(context.Background())
	releases := make([]atomic.Int64, 32)
	records := make(chan source.RecordBatchResult)
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		defer close(records)
		for i := range releases {
			batch := &releaseCountingBatch{releases: &releases[i]}
			select {
			case records <- source.RecordBatchResult{TableName: "items", Batch: batch}:
			case <-readCtx.Done():
				batch.Release()
				for j := i + 1; j < len(releases); j++ {
					(&releaseCountingBatch{releases: &releases[j]}).Release()
				}
				return
			}
		}
	}()

	err := Write(context.Background(), dest, records, destination.MultiTableWriteOptions{
		TableConfigs: map[string]destination.TableWriteConfig{"items": {DestTable: "raw.items"}},
		CancelSource: cancelRead,
	})
	if err == nil || !strings.Contains(err.Error(), "early write failure") {
		t.Fatalf("Write() error = %v, want early write failure", err)
	}
	select {
	case <-producerDone:
	case <-time.After(time.Second):
		t.Fatal("source producer remained blocked after table writer failure")
	}
	for i := range releases {
		if got := releases[i].Load(); got != 1 {
			t.Fatalf("batch %d release count = %d, want 1", i, got)
		}
	}
}

func TestWriteFailureReturnsWhenCanceledProducerNeverCloses(t *testing.T) {
	dest := &fakeDestination{writeFn: func(context.Context, <-chan source.RecordBatchResult, destination.WriteOptions) error {
		return errors.New("early write failure")
	}}
	var releases atomic.Int64
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: "items", Batch: &releaseCountingBatch{releases: &releases}}

	started := time.Now()
	err := Write(context.Background(), dest, records, destination.MultiTableWriteOptions{
		TableConfigs: map[string]destination.TableWriteConfig{"items": {DestTable: "raw.items"}},
		CancelSource: func() {},
	})
	if err == nil || !strings.Contains(err.Error(), "early write failure") {
		t.Fatalf("Write() error = %v, want early write failure", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Write() waited %s for a canceled producer that never closed", elapsed)
	}
	if got := releases.Load(); got != 1 {
		t.Fatalf("batch release count = %d, want 1", got)
	}
}

func TestWriteDeliversSourceErrorToEveryFullTableChannel(t *testing.T) {
	sourceErr := errors.New("source failed")
	startReading := make(chan struct{})

	dest := &fakeDestination{
		writeFn: func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
			select {
			case <-startReading:
			case <-ctx.Done():
				return ctx.Err()
			}
			for result := range records {
				if result.Err != nil {
					return fmt.Errorf("source stream: %w", result.Err)
				}
			}
			return nil
		},
	}

	records := make(chan source.RecordBatchResult, 17)
	for range 8 {
		records <- source.RecordBatchResult{TableName: "users"}
		records <- source.RecordBatchResult{TableName: "orders"}
	}
	records <- source.RecordBatchResult{Err: sourceErr}
	close(records)

	done := make(chan error, 1)
	go func() {
		done <- Write(context.Background(), dest, records, destination.MultiTableWriteOptions{
			TableConfigs: map[string]destination.TableWriteConfig{
				"users":  {DestTable: "dataset.users"},
				"orders": {DestTable: "dataset.orders"},
			},
		})
	}()

	deadline := time.Now().Add(time.Second)
	for len(records) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(records) != 0 {
		t.Fatal("router did not reach the source error while table channels were full")
	}
	time.Sleep(20 * time.Millisecond)
	close(startReading)

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), sourceErr.Error()) {
			t.Fatalf("Write error = %v, want source error", err)
		}
		for _, table := range []string{"users", "orders"} {
			if !strings.Contains(err.Error(), "table "+table+":") {
				t.Errorf("Write error = %v, want source error from table %s", err, table)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Write deadlocked while delivering a source error to full table channels")
	}
}

// TestWriteDoesNotDeadlockWithScarceSharedResourceScopedPerBatch models the
// Snowflake connection-pool exhaustion deadlock generically: many tables
// each spawn several workers (numTables * parallelism exceeds the size of a
// shared bounded resource, e.g. a connection pool), and each worker acquires
// that resource only for the duration of processing a single batch,
// releasing it before waiting on the next one. This proves the router design
// doesn't deadlock as long as consumers never hold a scarce shared resource
// while idle - which is the property the Snowflake destination fix restores.
func TestWriteDoesNotDeadlockWithScarceSharedResourceScopedPerBatch(t *testing.T) {
	const numTables = 5
	const parallelism = 4 // numTables * parallelism (20) far exceeds the resource pool below
	const batchesPerTable = 20

	pool := make(chan struct{}, 3) // shared "connection pool" smaller than numTables*parallelism

	dest := &fakeDestination{
		writeFn: func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
			for result := range records {
				if result.Err != nil {
					return result.Err
				}
				select {
				case pool <- struct{}{}:
				case <-ctx.Done():
					return ctx.Err()
				}
				<-pool
			}
			return nil
		},
	}

	tableConfigs := make(map[string]destination.TableWriteConfig, numTables)
	tableNames := make([]string, 0, numTables)
	for i := 0; i < numTables; i++ {
		name := "table_" + strconv.Itoa(i)
		tableNames = append(tableNames, name)
		tableConfigs[name] = destination.TableWriteConfig{DestTable: "dataset." + name}
	}

	records := make(chan source.RecordBatchResult)
	go func() {
		defer close(records)
		for j := 0; j < batchesPerTable; j++ {
			for _, name := range tableNames {
				records <- source.RecordBatchResult{TableName: name}
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		done <- Write(context.Background(), dest, records, destination.MultiTableWriteOptions{
			TableConfigs: tableConfigs,
			Parallelism:  parallelism,
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Write returned error = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Write deadlocked: a worker likely held the shared resource while idle instead of releasing it between batches")
	}
}

func TestWriteReturnsParentContextError(t *testing.T) {
	dest := &fakeDestination{
		writeFn: func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	records := make(chan source.RecordBatchResult)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Write(ctx, dest, records, destination.MultiTableWriteOptions{
		TableConfigs: map[string]destination.TableWriteConfig{
			"users": {DestTable: "dataset.users"},
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Write error = %v, want context.Canceled", err)
	}
}

func TestWriteWithResultSplitsTableAtTruncate(t *testing.T) {
	var (
		mu            sync.Mutex
		segmentCounts []int
	)
	base := &fakeDestination{writeFn: func(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions) error {
		count := 0
		for range records {
			count++
		}
		mu.Lock()
		segmentCounts = append(segmentCounts, count)
		mu.Unlock()
		return nil
	}}
	dest := &truncatingFakeDestination{fakeDestination: base}
	records := make(chan source.RecordBatchResult, 4)
	records <- source.RecordBatchResult{TableName: "items"}
	records <- source.RecordBatchResult{TableName: "items"}
	records <- source.RecordBatchResult{TableName: "items", Truncate: true}
	records <- source.RecordBatchResult{TableName: "items"}
	close(records)

	result, err := WriteWithResult(context.Background(), dest, records, destination.MultiTableWriteOptions{
		TableConfigs: map[string]destination.TableWriteConfig{
			"items": {DestTable: "raw.items", CDCMode: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TruncatedTables["items"] {
		t.Fatal("truncate was not reported for items")
	}
	if got := dest.truncated; len(got) != 1 || got[0] != "raw.items" {
		t.Fatalf("truncate calls = %v, want [raw.items]", got)
	}
	if len(segmentCounts) != 2 || segmentCounts[0] != 2 || segmentCounts[1] != 1 {
		t.Fatalf("segment counts = %v, want [2 1]", segmentCounts)
	}
}
